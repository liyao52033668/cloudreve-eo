import { Table, Button, Dropdown, Modal, Input, message, Space, Image, Breadcrumb, Spin, Empty, Checkbox, Alert } from 'antd'
import { FolderOutlined, FileOutlined, FileImageOutlined, VideoCameraOutlined, DownloadOutlined, DeleteOutlined, EditOutlined, MoreOutlined, ShareAltOutlined, EyeOutlined, DragOutlined, FileTextOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { FileItem } from '../api/files'
import { deleteFile, renameFile, getDownloadURL, listFiles, moveFile, batchDeleteFiles, batchMoveFiles, getFileContent } from '../api/files'
import { useEffect, useMemo, useRef, useState } from 'react'
import { isImage, isVideo, isText, isMarkdown, isJson } from '../utils/fileType'
import { formatDateTime } from '../utils/time'
import ShareModal from './ShareModal'
import DownloadProgress from './DownloadProgress'
import { proxySegmentDownload, saveBlob, toProxyUrl } from '../utils/proxyDownload'
import { collectDirFiles, collectSelectedEntries, downloadEntriesAsZip } from '../utils/zipDownload'
import { marked } from 'marked'

// markdown 渲染：raw HTML 一律转义为纯文本，防 XSS（文件内容可能来自他人分享）
marked.use({
  renderer: {
    html({ text }) {
      return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    },
  },
})

/** 文本预览内容格式化：JSON 文件格式化缩进展示，解析失败则回退原文；其余文本原样显示 */
function renderTextContent(file: FileItem, content?: string): string {
  if (content == null) return ''
  if (isJson(file)) {
    try {
      return JSON.stringify(JSON.parse(content), null, 2)
    } catch {
      return content
    }
  }
  return content
}

interface Props {
  files: FileItem[]
  onRefresh: () => void
  onOpenDir: (dirId: number) => void
  viewMode: 'list' | 'grid'
}

interface MoveBreadcrumbItem {
  title: string
  id: number
}

/** 网格模式下图片缩略图：按需拉取内联预览 URL */
function GridThumb({ file }: { file: FileItem }) {
  const [url, setUrl] = useState('')
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let cancelled = false
    setUrl('')
    setFailed(false)
    if (!file.is_dir && isImage(file)) {
      getDownloadURL(file.id, true)
        .then((res) => {
          // 代理存储：缩略图直接请求 proxy（云函数单段 < 6MB），绕开边缘函数
          if (!cancelled) setUrl(toProxyUrl(res.data.download_url))
        })
        .catch(() => { if (!cancelled) setFailed(true) })
    }
    return () => { cancelled = true }
  }, [file.id])

  if (file.is_dir) return <FolderOutlined style={{ fontSize: 44, color: '#faad14' }} />
  if (isVideo(file)) return <VideoCameraOutlined style={{ fontSize: 44, color: '#13c2c2' }} />
  if (isText(file)) return <FileTextOutlined style={{ fontSize: 44, color: '#1677ff' }} />
  if (isImage(file)) {
    if (url) {
      return (
        <img
          src={url}
          alt={file.name}
          className="file-grid__thumb-img"
          onError={() => setFailed(true)}
        />
      )
    }
    if (failed) return <FileImageOutlined style={{ fontSize: 44, color: '#52c41a' }} />
    return <Spin size="small" />
  }
  return <FileOutlined style={{ fontSize: 44, color: 'rgba(0,0,0,0.45)' }} />
}

export default function FileList({ files, onRefresh, onOpenDir, viewMode }: Props) {
  const [renameModal, setRenameModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [newName, setNewName] = useState('')
  const [shareFile, setShareFile] = useState<FileItem | null>(null)
  // objectUrl=true 表示 url 是分段拼接 Blob 生成的对象 URL，关闭预览时需 revoke 释放内存
  const [preview, setPreview] = useState<{ open: boolean; url: string; objectUrl?: boolean }>({ open: false, url: '' })
  const [videoPreview, setVideoPreview] = useState<{ open: boolean; url: string; objectUrl?: boolean }>({ open: false, url: '' })
  const [downloading, setDownloading] = useState(false)
  // 前端分段下载进度（仅代理存储：Filen/百度等无外链直链）
  const [downloadTask, setDownloadTask] = useState<{
    name: string
    percent: number
    received: number
    total: number
  } | null>(null)
  const downloadAbortRef = useRef<AbortController | null>(null)
  const [moveModal, setMoveModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [moveDirs, setMoveDirs] = useState<FileItem[]>([])
  const [moveLoading, setMoveLoading] = useState(false)
  const [moveSubmitting, setMoveSubmitting] = useState(false)
  const [moveCurrentId, setMoveCurrentId] = useState(0)
  const [moveBreadcrumb, setMoveBreadcrumb] = useState<MoveBreadcrumbItem[]>([{ title: '根目录', id: 0 }])

  // 批量操作：选中的文件 ID 集合
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [batchBusy, setBatchBusy] = useState(false)
  // 批量移动弹窗（复用移动目录浏览逻辑，但作用于多选）
  const [batchMoveOpen, setBatchMoveOpen] = useState(false)
  const [batchShareOpen, setBatchShareOpen] = useState(false)

  // 切换目录/刷新时清空选中（选中项属于上一目录，跨目录无意义）
  useEffect(() => {
    setSelectedIds(new Set())
  }, [files])

  const toggleSelect = (id: number) => {
    setSelectedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const allSelected = files.length > 0 && files.every(f => selectedIds.has(f.id))
  const someSelected = selectedIds.size > 0

  const toggleSelectAll = () => {
    setSelectedIds(allSelected ? new Set() : new Set(files.map(f => f.id)))
  }

  const clearSelection = () => setSelectedIds(new Set())

  const formatSize = (bytes: number) => {
    if (bytes === 0) return '-'
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let size = bytes
    while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
    return `${size.toFixed(1)} ${units[i]}`
  }

  const handleDownload = async (file: FileItem) => {
    if (downloading) return
    setDownloading(true)
    message.loading({ content: `正在获取下载链接 ${file.name}...`, key: 'download', duration: 0 })
    try {
      const res = await getDownloadURL(file.id)
      const url = res.data.download_url
      if (url.startsWith('/api/files/stream') || url.startsWith('/api/files/proxy')) {
        // 代理存储（Filen/百度等）：云函数响应有 6MB 缓冲上限、边缘函数有时长限制，
        // 改由前端 JS 按 Range 分段拉取并拼接 Blob，避开平台限制。
        await handleProxyDownload(file, url)
      } else {
        // S3 等有外链直链：直接交给浏览器下载管理器，原生流式落盘
        const a = document.createElement('a')
        a.href = url
        a.download = file.name
        document.body.appendChild(a)
        a.click()
        a.remove()
        message.success({ content: '下载已开始，进度见浏览器下载列表', key: 'download' })
      }
    } catch {
      message.error({ content: '下载失败', key: 'download' })
    } finally {
      setDownloading(false)
    }
  }

  /** 代理存储分段下载：浏览器 JS 按 Range 分段拉取并拼接 Blob（绕开云函数/边缘函数限制）。 */
  const handleProxyDownload = async (file: FileItem, streamUrl: string) => {
    const controller = new AbortController()
    downloadAbortRef.current = controller
    setDownloadTask({ name: file.name, percent: 0, received: 0, total: 0 })

    try {
      const blob = await proxySegmentDownload(
        streamUrl,
        (received, total) => setDownloadTask({
          name: file.name,
          percent: total > 0 ? Math.round((received / total) * 100) : 0,
          received,
          total,
        }),
        controller.signal,
      )
      saveBlob(blob, file.name)
      message.success({ content: `${file.name} 下载完成`, key: 'download' })
    } catch (err: any) {
      if (err?.name === 'AbortError') {
        message.info({ content: '下载已取消', key: 'download' })
      } else {
        message.error({ content: err?.message || '下载失败', key: 'download' })
      }
    } finally {
      setDownloadTask(null)
      downloadAbortRef.current = null
    }
  }

  const cancelDownload = () => {
    downloadAbortRef.current?.abort()
  }

  const handleDownloadDir = async (file: FileItem) => {
    if (downloading) return
    setDownloading(true)
    const controller = new AbortController()
    downloadAbortRef.current = controller
    setDownloadTask({ name: `${file.name}.zip`, percent: 0, received: 0, total: 0 })
    try {
      message.loading({ content: '正在收集文件列表...', key: 'download', duration: 0 })
      const entries = await collectDirFiles(file.id)
      message.destroy('download')
      if (entries.length === 0) throw new Error('文件夹为空')
      // 前端逐个分段下载并打包 zip（绕开云函数 6MB 响应缓冲，zip 无法后端分段）
      await downloadEntriesAsZip(
        entries,
        `${file.name}.zip`,
        (id) => getDownloadURL(id).then(r => r.data.download_url),
        (received, total) => setDownloadTask({
          name: `${file.name}.zip`,
          percent: total > 0 ? Math.round((received / total) * 100) : 0,
          received,
          total,
        }),
        controller.signal,
      )
      message.success({ content: '打包下载完成', key: 'download' })
    } catch (err: any) {
      if (err?.name === 'AbortError') message.info({ content: '打包下载已取消', key: 'download' })
      else message.error({ content: err?.message || '打包下载失败', key: 'download' })
    } finally {
      message.destroy('download')
      setDownloadTask(null)
      downloadAbortRef.current = null
      setDownloading(false)
    }
  }

  const handlePreview = async (file: FileItem) => {
    // 文本类文件走内容读取接口，弹窗渲染；图片/视频走内联 URL
    if (isText(file)) {
      handleTextPreview(file)
      return
    }
    try {
      const res = await getDownloadURL(file.id, true)
      const url = res.data.download_url
      // 视频：直接用 proxy 直链，浏览器原生 Range 流式播放（边下边播、可 seek），
      // 云函数每段 ≤1MB，无需前端下载完整再播
      if (isVideo(file)) {
        setVideoPreview({ open: true, url: toProxyUrl(url) })
        return
      }
      // 图片：<img> 不带 Range，代理存储大图需分段拼接后展示
      if (url.startsWith('/api/files/stream') || url.startsWith('/api/files/proxy')) {
        message.loading({ content: `正在加载 ${file.name}...`, key: 'preview', duration: 0 })
        try {
          const blob = await proxySegmentDownload(url)
          const objectUrl = URL.createObjectURL(blob)
          setPreview({ open: true, url: objectUrl, objectUrl: true })
        } finally {
          message.destroy('preview')
        }
      } else {
        setPreview({ open: true, url })
      }
    } catch (err: any) {
      message.destroy('preview')
      message.error(err?.message || '获取预览链接失败')
    }
  }

  const [textPreview, setTextPreview] = useState<{
    open: boolean
    file?: FileItem
    content?: string
    truncated?: boolean
    loading?: boolean
  }>({ open: false })

  const handleTextPreview = async (file: FileItem) => {
    setTextPreview({ open: true, file, loading: true })
    try {
      const res = await getFileContent(file.id)
      setTextPreview({ open: true, file, content: res.data.content, truncated: res.data.truncated, loading: false })
    } catch (err: any) {
      message.error(err.response?.data?.error || '读取文件内容失败')
      setTextPreview({ open: false })
    }
  }

  const handleDelete = (file: FileItem) => {
    Modal.confirm({
      title: '确认删除',
      content: file.is_dir
        ? `确定删除文件夹 "${file.name}" 及其中的所有内容吗？此操作不可恢复。`
        : `确定删除 "${file.name}" 吗？`,
      okText: '删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await deleteFile(file.id)
          message.success('删除成功')
          onRefresh()
        } catch (err: any) {
          message.error(err.response?.data?.error || '删除失败')
        }
      },
    })
  }

  const handleRename = async () => {
    if (!renameModal.file || !newName) return
    try {
      await renameFile(renameModal.file.id, newName)
      message.success('重命名成功')
      setRenameModal({ visible: false })
      onRefresh()
    } catch (err: any) {
      message.error(err.response?.data?.error || '重命名失败')
    }
  }

  const loadMoveDirs = async (parentId: number, excludeIds: Set<number> = new Set()) => {
    setMoveLoading(true)
    try {
      const res = await listFiles(parentId)
      // 排除待排除的文件夹（单选为被移动项自身，批量为全部选中的文件夹，避免移入自身/子树）
      const dirs = (res.data.files || []).filter(
        f => f.is_dir && !excludeIds.has(f.id),
      )
      setMoveDirs(dirs)
    } catch (err: any) {
      message.error(err?.response?.data?.error || '加载目录失败')
      setMoveDirs([])
    } finally {
      setMoveLoading(false)
    }
  }

  // 当前移动上下文需排除的文件夹集合：单选=被移动目录自身；批量=全部选中的文件夹
  const moveExcludeIds = (): Set<number> => {
    if (batchMoveOpen) {
      return new Set(files.filter(f => selectedIds.has(f.id) && f.is_dir).map(f => f.id))
    }
    return moveModal.file?.is_dir ? new Set([moveModal.file.id]) : new Set()
  }

  const openMoveModal = (file: FileItem) => {
    setMoveModal({ visible: true, file })
    setMoveCurrentId(0)
    setMoveBreadcrumb([{ title: '根目录', id: 0 }])
    // 显式传排除集合：moveModal 状态尚未提交，闭包中读到的仍是旧值
    loadMoveDirs(0, file.is_dir ? new Set([file.id]) : new Set())
  }

  const closeMoveModal = () => {
    setMoveModal({ visible: false })
    setBatchMoveOpen(false)
    setMoveDirs([])
    setMoveCurrentId(0)
    setMoveBreadcrumb([{ title: '根目录', id: 0 }])
  }

  const handleMoveEnter = (dir: FileItem) => {
    setMoveCurrentId(dir.id)
    setMoveBreadcrumb(prev => [...prev, { title: dir.name, id: dir.id }])
    loadMoveDirs(dir.id, moveExcludeIds())
  }

  const handleMoveBreadcrumb = (index: number) => {
    const target = moveBreadcrumb[index]
    if (!target || target.id === moveCurrentId) return
    setMoveCurrentId(target.id)
    setMoveBreadcrumb(moveBreadcrumb.slice(0, index + 1))
    loadMoveDirs(target.id, moveExcludeIds())
  }

  const handleMoveConfirm = async () => {
    if (!moveModal.file) return
    if (moveModal.file.parent_id === moveCurrentId) {
      message.info('已在当前位置')
      return
    }
    if (moveModal.file.is_dir && moveModal.file.id === moveCurrentId) {
      message.error('不能移动到自身')
      return
    }
    setMoveSubmitting(true)
    try {
      await moveFile(moveModal.file.id, moveCurrentId)
      message.success('移动成功')
      closeMoveModal()
      onRefresh()
    } catch (err: any) {
      message.error(err?.response?.data?.error || '移动失败')
    } finally {
      setMoveSubmitting(false)
    }
  }

  // ===== 批量操作 =====

  const batchIds = useMemo(() => Array.from(selectedIds), [selectedIds])

  const handleBatchDelete = () => {
    if (!someSelected) return
    const hasDir = files.some(f => selectedIds.has(f.id) && f.is_dir)
    Modal.confirm({
      title: `确认删除 ${selectedIds.size} 项`,
      content: hasDir
        ? '选中项包含文件夹，将连同其中所有内容一并删除，此操作不可恢复。'
        : `确定删除选中的 ${selectedIds.size} 个文件吗？`,
      okText: '删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        setBatchBusy(true)
        try {
          await batchDeleteFiles(batchIds)
          message.success(`已删除 ${selectedIds.size} 项`)
          clearSelection()
          onRefresh()
        } catch (err: any) {
          message.error(err.response?.data?.error || '删除失败')
        } finally {
          setBatchBusy(false)
        }
      },
    })
  }

  const openBatchMoveModal = () => {
    if (!someSelected) return
    setBatchMoveOpen(true)
    setMoveCurrentId(0)
    setMoveBreadcrumb([{ title: '根目录', id: 0 }])
    // 显式传排除集合：batchMoveOpen 状态尚未提交，闭包中读到的仍是旧值
    const excludeIds = new Set(files.filter(f => selectedIds.has(f.id) && f.is_dir).map(f => f.id))
    loadMoveDirs(0, excludeIds)
  }

  const handleBatchMoveConfirm = async () => {
    if (!someSelected) return
    setMoveSubmitting(true)
    try {
      await batchMoveFiles(batchIds, moveCurrentId)
      message.success(`已移动 ${selectedIds.size} 项`)
      setBatchMoveOpen(false)
      clearSelection()
      onRefresh()
    } catch (err: any) {
      message.error(err?.response?.data?.error || '移动失败')
    } finally {
      setMoveSubmitting(false)
    }
  }

  const handleBatchDownload = async () => {
    if (!someSelected || downloading) return
    setDownloading(true)
    const controller = new AbortController()
    downloadAbortRef.current = controller
    setDownloadTask({ name: '批量下载.zip', percent: 0, received: 0, total: 0 })
    try {
      message.loading({ content: '正在收集文件列表...', key: 'batch-download', duration: 0 })
      const selected = files.filter(f => selectedIds.has(f.id))
      const entries = await collectSelectedEntries(selected)
      message.destroy('batch-download')
      if (entries.length === 0) throw new Error('没有可下载的文件')
      await downloadEntriesAsZip(
        entries,
        '批量下载.zip',
        (id) => getDownloadURL(id).then(r => r.data.download_url),
        (received, total) => setDownloadTask({
          name: '批量下载.zip',
          percent: total > 0 ? Math.round((received / total) * 100) : 0,
          received,
          total,
        }),
        controller.signal,
      )
      message.success({ content: '打包下载完成', key: 'batch-download' })
    } catch (err: any) {
      if (err?.name === 'AbortError') message.info({ content: '打包下载已取消', key: 'batch-download' })
      else message.error({ content: err?.message || '打包下载失败', key: 'batch-download' })
    } finally {
      message.destroy('batch-download')
      setDownloadTask(null)
      downloadAbortRef.current = null
      setDownloading(false)
    }
  }

  const policyFilters = useMemo(() => {
    const names = new Set<string>()
    for (const f of files) {
      if (f.storage_policy) names.add(f.storage_policy)
    }
    return Array.from(names).sort().map((p) => ({ text: p, value: p }))
  }, [files])

  const menuItemsFor = (record: FileItem) => [
    ...(!record.is_dir && (isImage(record) || isVideo(record) || isText(record)) ? [{ key: 'preview', label: '预览', icon: <EyeOutlined /> }] : []),
    { key: 'download', label: record.is_dir ? '下载(zip)' : '下载', icon: <DownloadOutlined /> },
    { key: 'share', label: '分享', icon: <ShareAltOutlined /> },
    { key: 'rename', label: '重命名', icon: <EditOutlined /> },
    { key: 'move', label: '移动', icon: <DragOutlined /> },
    { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
  ]

  const onMenuClick = (key: string, record: FileItem) => {
    if (key === 'preview') handlePreview(record)
    else if (key === 'download') { if (record.is_dir) handleDownloadDir(record); else handleDownload(record) }
    else if (key === 'share') setShareFile(record)
    else if (key === 'rename') { setRenameModal({ visible: true, file: record }); setNewName(record.name) }
    else if (key === 'move') openMoveModal(record)
    else if (key === 'delete') handleDelete(record)
  }

  const gridView = (
    <div className="file-grid">
      {files.map((f) => (
        <div className="file-grid__item" key={f.id}>
          <Dropdown
            trigger={['contextMenu']}
            menu={{ items: menuItemsFor(f), onClick: ({ key }) => onMenuClick(key, f) }}
          >
            <div
              className="file-grid__card"
              onClick={() => {
                if (f.is_dir) onOpenDir(f.id)
                else if (isImage(f) || isVideo(f) || isText(f)) handlePreview(f)
                else handleDownload(f)
              }}
            >
              <div className="file-grid__thumb">
                <GridThumb file={f} />
              </div>
              <div className="file-grid__name" title={f.name}>{f.name}</div>
            </div>
          </Dropdown>
          {/* 网格卡片左上角勾选框：批量选择 */}
          <div
            className="file-grid__select"
            onClick={(e) => { e.stopPropagation(); toggleSelect(f.id) }}
          >
            <Checkbox checked={selectedIds.has(f.id)} />
          </div>
          <Dropdown menu={{ items: menuItemsFor(f), onClick: ({ key }) => onMenuClick(key, f) }}>
            <Button className="file-grid__more" type="text" size="small" icon={<MoreOutlined />} />
          </Dropdown>
        </div>
      ))}
    </div>
  )

  const columns: ColumnsType<FileItem> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record) => (
        <Space>
          {record.is_dir
            ? <FolderOutlined style={{ color: '#faad14' }} />
            : isVideo(record)
              ? <VideoCameraOutlined style={{ color: '#13c2c2' }} />
              : isImage(record)
                ? <FileImageOutlined style={{ color: '#52c41a' }} />
                : isText(record)
                  ? <FileTextOutlined style={{ color: '#1677ff' }} />
                  : <FileOutlined />}
          <a
            onClick={() => {
              if (record.is_dir) onOpenDir(record.id)
              else if (isImage(record) || isVideo(record) || isText(record)) handlePreview(record)
            }}
          >
            {name}
          </a>
        </Space>
      ),
    },
    { title: '大小', dataIndex: 'size', width: 120, render: formatSize },
    {
      title: '存储策略',
      dataIndex: 'storage_policy',
      width: 140,
      filters: policyFilters,
      onFilter: (value, record) => record.storage_policy === value,
      render: (policy: string, record) => (record.is_dir ? '-' : (policy || '-')),
    },
    { title: '修改时间', dataIndex: 'updated_at', width: 180, render: (v: string) => formatDateTime(v) },
    {
      title: '操作', width: 120,
      render: (_, record) => (
        <Dropdown menu={{ items: menuItemsFor(record), onClick: ({ key }) => onMenuClick(key, record) }}>
          <Button type="text" icon={<MoreOutlined />} />
        </Dropdown>
      ),
    },
  ]

  const rowSelection = {
    selectedRowKeys: batchIds,
    onChange: (keys: React.Key[]) => setSelectedIds(new Set(keys.map(Number))),
  }

  // 批量操作工具栏（选中任意项时显示）
  const batchBar = someSelected && (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        padding: '8px 12px',
        marginBottom: 12,
        background: 'rgba(22,119,255,0.06)',
        borderRadius: 8,
      }}
    >
      <Checkbox checked={allSelected} indeterminate={someSelected && !allSelected} onChange={toggleSelectAll} />
      <span>已选 {selectedIds.size} 项</span>
      <Space>
        <Button size="small" icon={<DownloadOutlined />} onClick={handleBatchDownload} disabled={batchBusy}>
          下载(zip)
        </Button>
        <Button size="small" icon={<ShareAltOutlined />} onClick={() => setBatchShareOpen(true)} disabled={batchBusy}>
          分享
        </Button>
        <Button size="small" icon={<DragOutlined />} onClick={openBatchMoveModal} disabled={batchBusy}>
          移动
        </Button>
        <Button size="small" danger icon={<DeleteOutlined />} onClick={handleBatchDelete} disabled={batchBusy}>
          删除
        </Button>
        <Button size="small" type="link" onClick={clearSelection} disabled={batchBusy}>
          取消选择
        </Button>
      </Space>
    </div>
  )

  return (
    <>
      {batchBar}
      {files.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无文件" style={{ margin: '48px 0' }} />
      ) : viewMode === 'grid' ? (
        gridView
      ) : (
        <Table columns={columns} dataSource={files} rowKey="id" pagination={false} rowSelection={rowSelection} />
      )}
      <Modal title="重命名" open={renameModal.visible} onOk={handleRename} onCancel={() => setRenameModal({ visible: false })}>
        <Input value={newName} onChange={(e) => setNewName(e.target.value)} />
      </Modal>
      <DownloadProgress task={downloadTask} onCancel={cancelDownload} />
      <Modal
        title={batchMoveOpen ? `批量移动 ${selectedIds.size} 项` : `移动「${moveModal.file?.name || ''}」`}
        open={moveModal.visible || batchMoveOpen}
        onCancel={closeMoveModal}
        onOk={batchMoveOpen ? handleBatchMoveConfirm : handleMoveConfirm}
        okText="移动到这里"
        confirmLoading={moveSubmitting}
        destroyOnHidden
      >
        <Breadcrumb
          style={{ marginBottom: 12 }}
          items={moveBreadcrumb.map((b, index) => ({
            key: b.id,
            title: index === moveBreadcrumb.length - 1 ? (
              b.title
            ) : (
              <a onClick={() => handleMoveBreadcrumb(index)}>{b.title}</a>
            ),
          }))}
        />
        <Spin spinning={moveLoading}>
          {moveDirs.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前目录无子文件夹" />
          ) : (
            <div style={{ maxHeight: 320, overflowY: 'auto' }}>
              {moveDirs.map(dir => (
                <div
                  key={dir.id}
                  style={{
                    padding: '8px 12px',
                    cursor: 'pointer',
                    borderRadius: 6,
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                  }}
                  onClick={() => handleMoveEnter(dir)}
                  onMouseEnter={(e) => { e.currentTarget.style.background = 'rgba(0,0,0,0.04)' }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent' }}
                >
                  <FolderOutlined style={{ color: '#faad14' }} />
                  <span>{dir.name}</span>
                </div>
              ))}
            </div>
          )}
        </Spin>
      </Modal>
      <ShareModal
        open={batchShareOpen || !!shareFile}
        fileIds={batchShareOpen ? batchIds : shareFile ? [shareFile.id] : []}
        onClose={() => { setShareFile(null); setBatchShareOpen(false) }}
      />
      <Image
        style={{ display: 'none' }}
        preview={{
          open: preview.open,
          src: preview.url,
          onOpenChange: (v) => {
            if (!v) {
              if (preview.objectUrl) URL.revokeObjectURL(preview.url)
              setPreview({ open: false, url: '' })
            }
          },
        }}
      />
      <Modal
        title="视频预览"
        open={videoPreview.open}
        onCancel={() => {
          if (videoPreview.objectUrl) URL.revokeObjectURL(videoPreview.url)
          setVideoPreview({ open: false, url: '' })
        }}
        footer={null}
        width={800}
        destroyOnHidden
      >
        <video
          src={videoPreview.url}
          controls
          autoPlay
          style={{ width: '100%', maxHeight: '70vh' }}
        />
      </Modal>
      <Modal
        title={textPreview.file ? `预览：${textPreview.file.name}` : '预览'}
        open={textPreview.open}
        onCancel={() => setTextPreview({ open: false })}
        footer={null}
        width={820}
        destroyOnHidden
      >
        {textPreview.loading ? (
          <div style={{ textAlign: 'center', padding: '48px 0' }}><Spin /></div>
        ) : textPreview.file ? (
          <>
            {textPreview.truncated && (
              <Alert
                type="warning"
                showIcon
                message="文件较大，仅显示前 1MB 内容"
                style={{ marginBottom: 12 }}
              />
            )}
            {isMarkdown(textPreview.file) ? (
              <div
                className="text-preview-md"
                dangerouslySetInnerHTML={{ __html: marked.parse(textPreview.content || '', { gfm: true }) }}
              />
            ) : (
              <pre className="text-preview-plain">{renderTextContent(textPreview.file, textPreview.content)}</pre>
            )}
          </>
        ) : null}
      </Modal>
    </>
  )
}
