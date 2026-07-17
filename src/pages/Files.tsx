import { useState, useEffect, useCallback } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space } from 'antd'
import { UploadOutlined, FolderAddOutlined, LogoutOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import { listFiles, mkdir, getUploadURL, uploadCallback, type FileItem } from '../api/files'
import { useNavigate } from 'react-router-dom'

const { Header, Content } = Layout

interface BreadcrumbItem { title: string; id: number }

export default function Files() {
  const [files, setFiles] = useState<FileItem[]>([])
  const [currentDir, setCurrentDir] = useState(0)
  const [breadcrumb, setBreadcrumb] = useState<BreadcrumbItem[]>([{ title: '根目录', id: 0 }])
  const [mkdirModal, setMkdirModal] = useState(false)
  const [dirName, setDirName] = useState('')
  const navigate = useNavigate()

  const loadFiles = useCallback(async () => {
    try {
      const res = await listFiles(currentDir)
      setFiles(res.data.files)
    } catch {
      message.error('加载文件列表失败')
    }
  }, [currentDir])

  useEffect(() => { loadFiles() }, [loadFiles])

  const handleOpenDir = async (dirId: number) => {
    setCurrentDir(dirId)
    if (dirId === 0) {
      setBreadcrumb([{ title: '根目录', id: 0 }])
    } else {
      setBreadcrumb(prev => [...prev, { title: files.find(f => f.id === dirId)?.name || '', id: dirId }])
    }
  }

  const handleMkdir = async () => {
    if (!dirName) return
    try {
      await mkdir(currentDir, dirName)
      message.success('创建成功')
      setMkdirModal(false)
      setDirName('')
      loadFiles()
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建失败')
    }
  }

  const handleUpload = async (file: File) => {
    try {
      const { data } = await getUploadURL(file.name, file.type, currentDir)
      await fetch(data.upload_url, { method: 'PUT', body: file, headers: { 'Content-Type': file.type } })
      await uploadCallback(file.name, data.storage_key, file.size, file.type, currentDir)
      message.success(`${file.name} 上传成功`)
      loadFiles()
    } catch {
      message.error(`${file.name} 上传失败`)
    }
    return false
  }

  const handleLogout = () => {
    localStorage.removeItem('token')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <span style={{ color: '#fff', fontSize: 18 }}>Cloudreve-EO</span>
        <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>退出</Button>
      </Header>
      <Content style={{ padding: '24px', maxWidth: 1200, margin: '0 auto', width: '100%' }}>
        <Breadcrumb style={{ marginBottom: 16 }} items={breadcrumb.map(b => ({ title: b.title, key: b.id }))} />
        <Space style={{ marginBottom: 16 }}>
          <Upload beforeUpload={handleUpload} showUploadList={false}>
            <Button icon={<UploadOutlined />} type="primary">上传文件</Button>
          </Upload>
          <Button icon={<FolderAddOutlined />} onClick={() => setMkdirModal(true)}>新建文件夹</Button>
        </Space>
        <FileList files={files} onRefresh={loadFiles} onOpenDir={handleOpenDir} />
      </Content>
      <Modal title="新建文件夹" open={mkdirModal} onOk={handleMkdir} onCancel={() => setMkdirModal(false)}>
        <Input value={dirName} onChange={(e) => setDirName(e.target.value)} placeholder="文件夹名称" />
      </Modal>
    </Layout>
  )
}
