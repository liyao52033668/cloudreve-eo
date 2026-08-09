import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, Input, Button, message, Space, Typography, Table, Breadcrumb, Spin } from 'antd'
import { DownloadOutlined, FolderOutlined, FileOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { getShare, getShareDownload, getShareFiles, getShareZipURL, getShareZipSelectedURL, getShareChildDownload, type ShareInfo } from '../api/shares'
import type { FileItem } from '../api/files'
import { formatDateTime } from '../utils/time'

const { Title, Text } = Typography

const formatSize = (bytes: number) => {
  if (bytes === 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return `${size.toFixed(1)} ${units[i]}`
}

export default function ShareView() {
  const { code } = useParams<{ code: string }>()
  const [password, setPassword] = useState('')
  const [roots, setRoots] = useState<FileItem[]>([])
  const [share, setShare] = useState<ShareInfo | null>(null)
  const [error, setError] = useState('')
  const [needPassword, setNeedPassword] = useState(false)

  // 列表浏览状态：breadcrumb 中 id 为 0 表示顶层（分享的根文件列表）
  const [breadcrumb, setBreadcrumb] = useState<{ id: number; title: string }[]>([])
  const [children, setChildren] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [downloading, setDownloading] = useState(false)

  // 勾选下载：选中的文件 ID
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  useEffect(() => {
    if (code) loadShare('')
  }, [code])

  // 切换目录时清空选中（选中项属于上一目录，跨目录无意义）
  useEffect(() => {
    setSelectedIds([])
  }, [children])

  const loadChildren = async (parentId: number, pwd: string) => {
    if (!code) return
    setLoading(true)
    try {
      const res = await getShareFiles(code, parentId, pwd || undefined)
      setChildren(res.data.files)
    } catch (err: any) {
      message.error(err.response?.data?.error || '加载目录失败')
      setChildren([])
    } finally {
      setLoading(false)
    }
  }

  const loadShare = async (pwd: string) => {
    if (!code) return
    try {
      const res = await getShare(code, pwd)
      setRoots(res.data.files)
      setShare(res.data.share)
      setError('')
      const files = res.data.files
      if (files.length === 1 && files[0].is_dir) {
        // 单个文件夹分享：直接进入文件夹浏览
        setBreadcrumb([{ id: files[0].id, title: files[0].name }])
        loadChildren(files[0].id, pwd)
      } else {
        // 单文件或多文件分享：顶层展示根文件列表
        setBreadcrumb([{ id: 0, title: files.length > 1 ? `分享的 ${files.length} 个文件` : '分享文件' }])
        setChildren(files)
      }
    } catch (err: any) {
      const msg = err.response?.data?.error || '加载失败'
      if (msg.includes('提取码')) {
        setNeedPassword(true)
        setError('')
      } else {
        setError(msg)
      }
    }
  }

  // 当前是否浏览单个普通文件（非文件夹）分享
  const singleFile = roots.length === 1 && !roots[0].is_dir ? roots[0] : null

  const handleDownload = async () => {
    if (!code || !singleFile || downloading) return
    setDownloading(true)
    message.loading({ content: '正在获取下载链接...', key: 'download', duration: 0 })
    try {
      const res = await getShareDownload(code, password || undefined)
      // 直接交给浏览器下载管理器：流式落盘、原生支持 Range 断点续传
      const a = document.createElement('a')
      a.href = res.data.download_url
      a.download = singleFile.name
      document.body.appendChild(a)
      a.click()
      a.remove()
      message.success({ content: '下载已开始，进度见浏览器下载列表', key: 'download' })
    } catch {
      message.error({ content: '下载失败', key: 'download' })
    } finally {
      setDownloading(false)
    }
  }

  const openDir = (dir: FileItem) => {
    setBreadcrumb(b => [...b, { id: dir.id, title: dir.name }])
    loadChildren(dir.id, password)
  }

  const goBreadcrumb = (index: number) => {
    const target = breadcrumb[index]
    setBreadcrumb(breadcrumb.slice(0, index + 1))
    if (target.id === 0) {
      // 回到顶层：直接展示分享的根文件列表
      setChildren(roots)
    } else {
      loadChildren(target.id, password)
    }
  }

  // zip 下载：浏览器直接导航到流式 URL，原生下载管理器接管
  //（附件响应不会触发页面跳转，进度可见于 chrome://downloads）
  const handleDownloadZip = () => {
    if (!code) return
    window.location.href = getShareZipURL(code, password || undefined)
    message.success({ content: '打包下载已开始，进度见浏览器下载列表', key: 'download' })
  }

  // 下载勾选的文件（打包 zip；单选普通文件走直链更快）
  const handleDownloadSelected = () => {
    if (!code || selectedIds.length === 0) return
    const selected = children.filter(f => selectedIds.includes(f.id))
    if (selected.length === 1 && !selected[0].is_dir) {
      handleChildDownload(selected[0])
      return
    }
    window.location.href = getShareZipSelectedURL(code, selectedIds, password || undefined)
    message.success({ content: '打包下载已开始，进度见浏览器下载列表', key: 'download' })
  }

  const handleChildDownload = async (f: FileItem) => {
    if (!code || downloading) return
    setDownloading(true)
    message.loading({ content: `正在获取下载链接 ${f.name}...`, key: 'download', duration: 0 })
    try {
      const res = await getShareChildDownload(code, f.id, password || undefined)
      // 直接交给浏览器下载管理器：流式落盘、原生支持 Range 断点续传
      const a = document.createElement('a')
      a.href = res.data.download_url
      a.download = f.name
      document.body.appendChild(a)
      a.click()
      a.remove()
      message.success({ content: '下载已开始，进度见浏览器下载列表', key: 'download' })
    } catch (err: any) {
      message.error({ content: err.response?.data?.error || '下载失败', key: 'download' })
    } finally {
      setDownloading(false)
    }
  }

  const someSelected = selectedIds.length > 0

  const columns: ColumnsType<FileItem> = [
    {
      title: '名称', dataIndex: 'name',
      render: (name: string, r) => (
        <Space>
          {r.is_dir ? <FolderOutlined style={{ color: '#faad14' }} /> : <FileOutlined />}
          {name}
        </Space>
      ),
    },
    { title: '大小', width: 120, render: (_, r) => (r.is_dir ? '-' : formatSize(r.size)) },
    { title: '修改时间', width: 180, render: (_, r) => formatDateTime(r.updated_at) },
    {
      title: '操作', width: 100,
      render: (_, r) => r.is_dir ? (
        <a onClick={() => openDir(r)}>打开</a>
      ) : (
        <a onClick={() => handleChildDownload(r)}>下载</a>
      ),
    },
  ]

  if (error) return <div style={{ textAlign: 'center', marginTop: 100 }}><Text type="danger">{error}</Text></div>

  if (needPassword && roots.length === 0) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Card title="输入提取码" style={{ width: 360 }}>
          <Space direction="vertical" style={{ width: '100%' }}>
            <Input.Password value={password} onChange={(e) => setPassword(e.target.value)} placeholder="提取码" />
            <Button type="primary" block onClick={() => loadShare(password)}>确认</Button>
          </Space>
        </Card>
      </div>
    )
  }

  if (roots.length === 0) return <div style={{ textAlign: 'center', marginTop: 100 }}>加载中...</div>

  const expireText = share?.expire_at
    ? `过期时间：${formatDateTime(share.expire_at)}`
    : '永久有效'

  // 单个普通文件分享：简洁卡片 + 下载按钮
  if (singleFile) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
        <Card title="分享文件" style={{ width: 400 }}>
          <Title level={4}>{singleFile.name}</Title>
          <Text type="secondary">大小: {formatSize(singleFile.size)}</Text>
          <div style={{ marginTop: 8 }}>
            <Text type="secondary">{expireText}</Text>
          </div>
          <div style={{ marginTop: 24 }}>
            <Button type="primary" icon={<DownloadOutlined />} block onClick={handleDownload} loading={downloading}>下载文件</Button>
          </div>
        </Card>
      </div>
    )
  }

  // 文件夹或多文件分享：文件列表 + 勾选下载 + 全部打包下载
  const listTitle = roots.length === 1 ? `分享文件夹：${roots[0].name}` : `分享了 ${roots.length} 个文件`
  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-start', minHeight: '100vh', background: '#f0f2f5', padding: '48px 16px' }}>
      <Card title={listTitle} style={{ width: '100%', maxWidth: 760 }}>
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
            <Space wrap align="center">
              <Button type="primary" icon={<DownloadOutlined />} onClick={handleDownloadZip} loading={downloading}>下载全部(zip)</Button>
              {someSelected && (
                <>
                  <Button icon={<DownloadOutlined />} onClick={handleDownloadSelected} loading={downloading}>
                    下载选中{selectedIds.length === 1 && children.find(f => f.id === selectedIds[0] && !f.is_dir) ? '' : '(zip)'}
                  </Button>
                  <span>已选 {selectedIds.length} 项</span>
                </>
              )}
            </Space>
            <Text type="secondary">{expireText}</Text>
          </div>
          {breadcrumb.length > 1 && (
            <Breadcrumb
              items={breadcrumb.map((b, index) => ({
                key: `${b.id}-${index}`,
                title: index === breadcrumb.length - 1 ? b.title : (
                  <a onClick={() => goBreadcrumb(index)}>{b.title}</a>
                ),
              }))}
            />
          )}
          <Spin spinning={loading}>
            <Table
              columns={columns}
              dataSource={children}
              rowKey="id"
              pagination={false}
              size="small"
              rowSelection={{
                selectedRowKeys: selectedIds,
                onChange: (keys) => setSelectedIds(keys.map(Number)),
              }}
            />
          </Spin>
        </Space>
      </Card>
    </div>
  )
}
