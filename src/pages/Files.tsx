import { useState, useEffect, useCallback } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space, Select } from 'antd'
import { UploadOutlined, FolderAddOutlined, LogoutOutlined, SettingOutlined, CloudServerOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import {
  listFiles,
  mkdir,
  getUploadURL,
  uploadCallback,
  listStoragePolicies,
  type FileItem,
  type StoragePolicy,
} from '../api/files'
import { getProfile } from '../api/user'
import { useNavigate } from 'react-router-dom'

const { Header, Content } = Layout

interface BreadcrumbItem { title: string; id: number }

export default function Files() {
  const [files, setFiles] = useState<FileItem[]>([])
  const [currentDir, setCurrentDir] = useState(0)
  const [breadcrumb, setBreadcrumb] = useState<BreadcrumbItem[]>([{ title: '根目录', id: 0 }])
  const [mkdirModal, setMkdirModal] = useState(false)
  const [dirName, setDirName] = useState('')
  const [policies, setPolicies] = useState<StoragePolicy[]>([])
  const [selectedPolicy, setSelectedPolicy] = useState<string>('')
  const [isAdmin, setIsAdmin] = useState(false)
  const navigate = useNavigate()

  const loadFiles = useCallback(async () => {
    try {
      const res = await listFiles(currentDir)
      setFiles(res.data.files)
    } catch {
      message.error('加载文件列表失败')
    }
  }, [currentDir])

  const loadPolicies = useCallback(async () => {
    try {
      const res = await listStoragePolicies()
      setPolicies(res.data.policies || [])
      setSelectedPolicy(res.data.default || res.data.policies?.[0]?.name || '')
    } catch {
      // 单策略或旧后端时列表接口失败可忽略，上传仍走默认
    }
  }, [])

  const loadProfile = useCallback(async () => {
    try {
      const res = await getProfile()
      setIsAdmin(!!res.data.user?.is_admin)
      localStorage.setItem('user', JSON.stringify(res.data.user))
    } catch {
      // 未登录时由 client 拦截器跳转
    }
  }, [])

  useEffect(() => { loadFiles() }, [loadFiles])
  useEffect(() => { loadPolicies() }, [loadPolicies])
  useEffect(() => { loadProfile() }, [loadProfile])

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
      const { data } = await getUploadURL(file.name, file.type || 'application/octet-stream', currentDir, selectedPolicy)
      await fetch(data.upload_url, {
        method: 'PUT',
        body: file,
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
      })
      await uploadCallback(
        file.name,
        data.storage_key,
        file.size,
        file.type || 'application/octet-stream',
        currentDir,
        data.storage_policy || selectedPolicy,
      )
      message.success(`${file.name} 上传成功`)
      loadFiles()
    } catch {
      message.error(`${file.name} 上传失败`)
    }
    return false
  }

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  const policyOptions = policies.map((p) => ({
    value: p.name,
    label: p.is_default ? `${p.name}（默认）` : p.name,
  }))

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <span style={{ color: '#fff', fontSize: 18 }}>Cloudreve-EO</span>
        <Space>
          {isAdmin && (
            <>
              <Button
                icon={<CloudServerOutlined />}
                type="text"
                style={{ color: '#fff' }}
                onClick={() => navigate('/storage-policies')}
              >
                存储策略
              </Button>
              <Button
                icon={<SettingOutlined />}
                type="text"
                style={{ color: '#fff' }}
                onClick={() => navigate('/settings')}
              >
                参数设置
              </Button>
            </>
          )}
          <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>
            退出
          </Button>
        </Space>
      </Header>
      <Content style={{ padding: '24px', maxWidth: 1200, margin: '0 auto', width: '100%' }}>
        <Breadcrumb style={{ marginBottom: 16 }} items={breadcrumb.map(b => ({ title: b.title, key: b.id }))} />
        <Space style={{ marginBottom: 16 }} wrap>
          {policyOptions.length > 0 && (
            <Select
              style={{ minWidth: 180 }}
              value={selectedPolicy || undefined}
              onChange={setSelectedPolicy}
              options={policyOptions}
              placeholder="存储策略"
            />
          )}
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
