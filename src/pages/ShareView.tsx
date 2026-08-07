import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, Input, Button, message, Space, Typography, Table, Breadcrumb, Spin } from 'antd'
import { DownloadOutlined, FolderOutlined, FileOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { getShare, getShareDownload, getShareFiles, getShareZip, getShareChildDownload } from '../api/shares'
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
  const [file, setFile] = useState<FileItem | null>(null)
  const [error, setError] = useState('')
  const [needPassword, setNeedPassword] = useState(false)

  // 目录浏览状态
  const [breadcrumb, setBreadcrumb] = useState<{ id: number; title: string }[]>([])
  const [children, setChildren] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [downloading, setDownloading] = useState(false)

  useEffect(() => {
    if (code) loadShare('')
  }, [code])

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
      setFile(res.data.file)
      setError('')
      if (res.data.file.is_dir) {
        setBreadcrumb([{ id: res.data.file.id, title: res.data.file.name }])
        loadChildren(res.data.file.id, pwd)
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

  const handleDownload = async () => {
    if (!code || downloading) return
    setDownloading(true)
    message.loading({ content: '正在准备下载...', key: 'download', duration: 0 })
    try {
      const res = await getShareDownload(code, password || undefined)
      const response = await fetch(res.data.download_url)
      const blob = await response.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = file?.name || ''
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(blobUrl)
      message.success({ content: '下载成功', key: 'download' })
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
    loadChildren(target.id, password)
  }

  const handleDownloadZip = async () => {
    if (!code || downloading) return
    setDownloading(true)
    message.loading({ content: '正在打包下载...', key: 'download', duration: 0 })
    try {
      const res = await getShareZip(code, password || undefined)
      const url = URL.createObjectURL(res.data)
      const a = document.createElement('a')
      a.href = url
      a.download = `${file?.name || '分享'}.zip`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      message.success({ content: '下载成功', key: 'download' })
    } catch (err: any) {
      message.error({ content: err.response?.data?.error || '打包下载失败', key: 'download' })
    } finally {
      setDownloading(false)
    }
  }

  const handleChildDownload = async (f: FileItem) => {
    if (!code || downloading) return
    setDownloading(true)
    message.loading({ content: `正在下载 ${f.name}...`, key: 'download', duration: 0 })
    try {
      const res = await getShareChildDownload(code, f.id, password || undefined)
      const response = await fetch(res.data.download_url)
      const blob = await response.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = f.name
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(blobUrl)
      message.success({ content: '下载成功', key: 'download' })
    } catch (err: any) {
      message.error({ content: err.response?.data?.error || '下载失败', key: 'download' })
    } finally {
      setDownloading(false)
    }
  }

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

  if (needPassword && !file) {
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

  if (!file) return <div style={{ textAlign: 'center', marginTop: 100 }}>加载中...</div>

  if (file.is_dir) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'flex-start', minHeight: '100vh', background: '#f0f2f5', padding: '48px 16px' }}>
        <Card title="分享文件夹" style={{ width: '100%', maxWidth: 760 }}>
          <Space direction="vertical" style={{ width: '100%' }} size="middle">
            <Space wrap align="center">
              <Title level={4} style={{ margin: 0 }}>{file.name}</Title>
              <Button type="primary" icon={<DownloadOutlined />} onClick={handleDownloadZip} loading={downloading}>下载全部(zip)</Button>
            </Space>
            <Breadcrumb
              items={breadcrumb.map((b, index) => ({
                key: b.id,
                title: index === breadcrumb.length - 1 ? b.title : (
                  <a onClick={() => goBreadcrumb(index)}>{b.title}</a>
                ),
              }))}
            />
            <Spin spinning={loading}>
              <Table columns={columns} dataSource={children} rowKey="id" pagination={false} size="small" />
            </Spin>
          </Space>
        </Card>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="分享文件" style={{ width: 400 }}>
        <Title level={4}>{file.name}</Title>
        <Text type="secondary">大小: {(file.size / 1024 / 1024).toFixed(2)} MB</Text>
        <div style={{ marginTop: 24 }}>
          <Button type="primary" icon={<DownloadOutlined />} block onClick={handleDownload} loading={downloading}>下载文件</Button>
        </div>
      </Card>
    </div>
  )
}
