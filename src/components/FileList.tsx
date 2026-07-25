import { Table, Button, Dropdown, Modal, Input, message, Space, Image, Breadcrumb, Spin, Empty } from 'antd'
import { FolderOutlined, FileOutlined, FileImageOutlined, DownloadOutlined, DeleteOutlined, EditOutlined, MoreOutlined, ShareAltOutlined, EyeOutlined, DragOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { FileItem } from '../api/files'
import { deleteFile, renameFile, getDownloadURL, listFiles, moveFile } from '../api/files'
import { useMemo, useState } from 'react'
import ShareModal from './ShareModal'

interface Props {
  files: FileItem[]
  onRefresh: () => void
  onOpenDir: (dirId: number) => void
}

interface MoveBreadcrumbItem {
  title: string
  id: number
}

const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico', 'avif']

const isImage = (f: FileItem) =>
  f.mime_type?.startsWith('image/') ||
  IMAGE_EXTS.includes(f.name.split('.').pop()?.toLowerCase() || '')

export default function FileList({ files, onRefresh, onOpenDir }: Props) {
  const [renameModal, setRenameModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [newName, setNewName] = useState('')
  const [shareFile, setShareFile] = useState<FileItem | null>(null)
  const [preview, setPreview] = useState<{ open: boolean; url: string }>({ open: false, url: '' })
  const [moveModal, setMoveModal] = useState<{ visible: boolean; file?: FileItem }>({ visible: false })
  const [moveDirs, setMoveDirs] = useState<FileItem[]>([])
  const [moveLoading, setMoveLoading] = useState(false)
  const [moveSubmitting, setMoveSubmitting] = useState(false)
  const [moveCurrentId, setMoveCurrentId] = useState(0)
  const [moveBreadcrumb, setMoveBreadcrumb] = useState<MoveBreadcrumbItem[]>([{ title: '根目录', id: 0 }])

  const formatSize = (bytes: number) => {
    if (bytes === 0) return '-'
    const units = ['B', 'KB', 'MB', 'GB']
    let i = 0
    let size = bytes
    while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
    return `${size.toFixed(1)} ${units[i]}`
  }

  const handleDownload = async (file: FileItem) => {
    try {
      const res = await getDownloadURL(file.id)
      // 后端已带 Content-Disposition: attachment，直接跳转即触发下载
      const a = document.createElement('a')
      a.href = res.data.download_url
      a.download = file.name
      document.body.appendChild(a)
      a.click()
      a.remove()
    } catch {
      message.error('获取下载链接失败')
    }
  }

  const handlePreview = async (file: FileItem) => {
    try {
      const res = await getDownloadURL(file.id, true)
      setPreview({ open: true, url: res.data.download_url })
    } catch {
      message.error('获取预览链接失败')
    }
  }

  const handleDelete = (file: FileItem) => {
    Modal.confirm({
      title: '确认删除',
      content: `确定删除 "${file.name}" 吗？`,
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

  const loadMoveDirs = async (parentId: number, excludeId?: number) => {
    setMoveLoading(true)
    try {
      const res = await listFiles(parentId)
      const dirs = (res.data.files || []).filter(
        f => f.is_dir && f.id !== excludeId,
      )
      setMoveDirs(dirs)
    } catch (err: any) {
      message.error(err?.response?.data?.error || '加载目录失败')
      setMoveDirs([])
    } finally {
      setMoveLoading(false)
    }
  }

  const openMoveModal = (file: FileItem) => {
    setMoveModal({ visible: true, file })
    setMoveCurrentId(0)
    setMoveBreadcrumb([{ title: '根目录', id: 0 }])
    loadMoveDirs(0, file.is_dir ? file.id : undefined)
  }

  const closeMoveModal = () => {
    setMoveModal({ visible: false })
    setMoveDirs([])
    setMoveCurrentId(0)
    setMoveBreadcrumb([{ title: '根目录', id: 0 }])
  }

  const handleMoveEnter = (dir: FileItem) => {
    if (moveModal.file?.is_dir && dir.id === moveModal.file.id) return
    setMoveCurrentId(dir.id)
    setMoveBreadcrumb(prev => [...prev, { title: dir.name, id: dir.id }])
    loadMoveDirs(dir.id, moveModal.file?.is_dir ? moveModal.file.id : undefined)
  }

  const handleMoveBreadcrumb = (index: number) => {
    const target = moveBreadcrumb[index]
    if (!target || target.id === moveCurrentId) return
    setMoveCurrentId(target.id)
    setMoveBreadcrumb(moveBreadcrumb.slice(0, index + 1))
    loadMoveDirs(target.id, moveModal.file?.is_dir ? moveModal.file.id : undefined)
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

  const policyFilters = useMemo(() => {
    const names = new Set<string>()
    for (const f of files) {
      if (f.storage_policy) names.add(f.storage_policy)
    }
    return Array.from(names).sort().map((p) => ({ text: p, value: p }))
  }, [files])

  const columns: ColumnsType<FileItem> = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (name: string, record) => (
        <Space>
          {record.is_dir
            ? <FolderOutlined style={{ color: '#faad14' }} />
            : isImage(record)
              ? <FileImageOutlined style={{ color: '#52c41a' }} />
              : <FileOutlined />}
          <a
            onClick={() => {
              if (record.is_dir) onOpenDir(record.id)
              else if (isImage(record)) handlePreview(record)
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
    { title: '修改时间', dataIndex: 'updated_at', width: 180, render: (v: string) => new Date(v).toLocaleString() },
    {
      title: '操作', width: 120,
      render: (_, record) => (
        <Dropdown menu={{
          items: [
            ...(!record.is_dir && isImage(record) ? [{ key: 'preview', label: '预览', icon: <EyeOutlined /> }] : []),
            ...(!record.is_dir ? [{ key: 'download', label: '下载', icon: <DownloadOutlined /> }] : []),
            ...(!record.is_dir ? [{ key: 'share', label: '分享', icon: <ShareAltOutlined /> }] : []),
            { key: 'rename', label: '重命名', icon: <EditOutlined /> },
            { key: 'move', label: '移动', icon: <DragOutlined /> },
            { key: 'delete', label: '删除', icon: <DeleteOutlined />, danger: true },
          ],
          onClick: ({ key }) => {
            if (key === 'preview') handlePreview(record)
            else if (key === 'download') handleDownload(record)
            else if (key === 'share') { setShareFile(record) }
            else if (key === 'rename') { setRenameModal({ visible: true, file: record }); setNewName(record.name) }
            else if (key === 'move') openMoveModal(record)
            else if (key === 'delete') handleDelete(record)
          },
        }}>
          <Button type="text" icon={<MoreOutlined />} />
        </Dropdown>
      ),
    },
  ]

  return (
    <>
      <Table columns={columns} dataSource={files} rowKey="id" pagination={false} />
      <Modal title="重命名" open={renameModal.visible} onOk={handleRename} onCancel={() => setRenameModal({ visible: false })}>
        <Input value={newName} onChange={(e) => setNewName(e.target.value)} />
      </Modal>
      <Modal
        title={`移动「${moveModal.file?.name || ''}」`}
        open={moveModal.visible}
        onCancel={closeMoveModal}
        onOk={handleMoveConfirm}
        okText="移动到这里"
        confirmLoading={moveSubmitting}
        destroyOnClose
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
        open={!!shareFile}
        fileId={shareFile?.id ?? null}
        onClose={() => setShareFile(null)}
      />
      <Image
        style={{ display: 'none' }}
        preview={{
          visible: preview.open,
          src: preview.url,
          onVisibleChange: (v) => { if (!v) setPreview({ open: false, url: '' }) },
        }}
      />
    </>
  )
}
