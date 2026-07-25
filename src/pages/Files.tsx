import { useState, useEffect, useCallback, useRef } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space, Select, Alert } from 'antd'
import { UploadOutlined, FolderAddOutlined, LogoutOutlined, SettingOutlined, CloudServerOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import {
  listFiles,
  mkdir,
  getUploadURL,
  uploadCallback,
  listStoragePolicies,
  initMultipartUpload,
  completeMultipartUpload,
  abortMultipartUpload,
  listMultipartSessions,
  resumeMultipartUpload,
  type FileItem,
  type StoragePolicy,
  type CompletedPart,
  type MultipartSession,
  type UploadSessionInfo,
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
  const [pendingSessions, setPendingSessions] = useState<UploadSessionInfo[]>([])
  const resumeInputRef = useRef<HTMLInputElement>(null)
  const resumeTargetRef = useRef<UploadSessionInfo | null>(null)
  const navigate = useNavigate()

  const loadFiles = useCallback(async () => {
    try {
      const res = await listFiles(currentDir)
      setFiles(res.data.files || [])
    } catch (err: any) {
      if (err?.response?.status === 401) return // client 拦截器会跳登录
      message.error(err?.response?.data?.error || '加载文件列表失败')
    }
  }, [currentDir])

  const loadPolicies = useCallback(async () => {
    try {
      const res = await listStoragePolicies()
      setPolicies(res.data.policies || [])
      setSelectedPolicy(res.data.default || res.data.policies?.[0]?.name || '')
    } catch {
      // 尚未配置策略时接口可能为空/失败，上传前再提示
    }
  }, [])

  const loadProfile = useCallback(async () => {
    try {
      const res = await getProfile()
      setIsAdmin(!!res.data.user?.is_admin)
      localStorage.setItem('user', JSON.stringify(res.data.user))
    } catch (err: any) {
      if (err?.response?.status === 401) return
      // token 无效或用户不存在时清掉本地状态
      if (err?.response?.status === 404) {
        localStorage.removeItem('token')
        localStorage.removeItem('user')
        navigate('/login', { replace: true })
      }
    }
  }, [navigate])

  const loadSessions = useCallback(async () => {
    try {
      const res = await listMultipartSessions()
      setPendingSessions(res.data.sessions || [])
    } catch {
      // 未登录或后端不可用时忽略
    }
  }, [])

  useEffect(() => { loadFiles() }, [loadFiles])
  useEffect(() => { loadPolicies() }, [loadPolicies])
  useEffect(() => { loadProfile() }, [loadProfile])
  useEffect(() => { loadSessions() }, [loadSessions])

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

  // 超过该大小走 S3 分片上传（阈值固定 25MB；实际分片大小以策略配置为准）
  const MULTIPART_THRESHOLD = 25 * 1024 * 1024
  const MAX_CONCURRENT_PARTS = 3
  const PART_RETRY = 3

  const uploadSimple = async (file: File) => {
    const contentType = file.type || 'application/octet-stream'
    const { data } = await getUploadURL(file.name, contentType, currentDir, selectedPolicy)
    const resp = await fetch(data.upload_url, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': contentType },
    })
    if (!resp.ok) throw new Error(`上传失败: HTTP ${resp.status}`)
    await uploadCallback(
      file.name,
      data.storage_key,
      file.size,
      contentType,
      currentDir,
      data.storage_policy || selectedPolicy,
    )
  }

  const uploadPartWithRetry = async (url: string, chunk: Blob, partNumber: number): Promise<string> => {
    let lastErr: Error | null = null
    for (let attempt = 1; attempt <= PART_RETRY; attempt++) {
      try {
        const resp = await fetch(url, { method: 'PUT', body: chunk })
        if (!resp.ok) throw new Error(`分片 ${partNumber} 上传失败: HTTP ${resp.status}`)
        const etag = resp.headers.get('ETag') || resp.headers.get('etag')
        if (!etag) throw new Error(`分片 ${partNumber} 未返回 ETag，请检查存储桶 CORS 是否暴露 ETag`)
        return etag.replace(/"/g, '')
      } catch (err: any) {
        lastErr = err
        if (attempt < PART_RETRY) await new Promise(r => setTimeout(r, 1000 * attempt))
      }
    }
    throw lastErr
  }

  const runMultipart = async (
    file: File,
    session: MultipartSession,
    parentId: number,
    onProgress: (percent: number) => void,
  ) => {
    const contentType = file.type || 'application/octet-stream'
    const totalParts = session.part_urls.length
    const parts: CompletedPart[] = new Array(totalParts)
    const skip = new Set<number>()
    for (const p of session.uploaded_parts || []) {
      parts[p.part_number - 1] = { part_number: p.part_number, etag: p.etag.replace(/"/g, '') }
      skip.add(p.part_number - 1)
    }
    let done = skip.size
    onProgress(Math.round((done / totalParts) * 100))

    let next = 0
    const worker = async () => {
      while (next < totalParts) {
        const idx = next++
        if (skip.has(idx)) continue
        const start = idx * session.chunk_size
        const chunk = file.slice(start, Math.min(start + session.chunk_size, file.size))
        const etag = await uploadPartWithRetry(session.part_urls[idx], chunk, idx + 1)
        parts[idx] = { part_number: idx + 1, etag }
        done++
        onProgress(Math.round((done / totalParts) * 100))
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(MAX_CONCURRENT_PARTS, totalParts) }, () => worker()),
    )
    await completeMultipartUpload(
      file.name,
      session.storage_key,
      session.upload_id,
      file.size,
      contentType,
      parts,
      parentId,
      session.storage_policy,
    )
  }

  const uploadMultipart = async (file: File, onProgress: (percent: number) => void) => {
    const contentType = file.type || 'application/octet-stream'
    const { data } = await initMultipartUpload(file.name, contentType, file.size, currentDir, selectedPolicy)
    try {
      await runMultipart(file, data.session, currentDir, onProgress)
    } catch (err) {
      // 不 abort：会话保留在服务端，可稍后从「未完成的上传」恢复
      loadSessions()
      throw err
    }
  }

  const handleUpload = async (file: File) => {
    const key = `upload-${file.name}`
    try {
      if (file.size > MULTIPART_THRESHOLD) {
        message.loading({ content: `${file.name} 上传中 0%`, key, duration: 0 })
        await uploadMultipart(file, (percent) => {
          message.loading({ content: `${file.name} 上传中 ${percent}%`, key, duration: 0 })
        })
      } else {
        await uploadSimple(file)
      }
      message.success({ content: `${file.name} 上传成功`, key })
      loadFiles()
      loadSessions()
    } catch (err: any) {
      message.error({
        content: err?.response?.data?.error || err?.message || `${file.name} 上传失败`,
        key,
      })
    }
    return false
  }

  const handleResumeClick = (session: UploadSessionInfo) => {
    resumeTargetRef.current = session
    resumeInputRef.current?.click()
  }

  const handleResumeFileSelected = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    const target = resumeTargetRef.current
    resumeTargetRef.current = null
    if (!file || !target) return
    if (file.size !== target.size) {
      message.error(`文件大小不匹配：需要 ${formatSize(target.size)} 的「${target.file_name}」`)
      return
    }
    const key = `resume-${target.storage_key}`
    try {
      message.loading({ content: `${target.file_name} 恢复上传中…`, key, duration: 0 })
      const { data } = await resumeMultipartUpload(target.storage_key)
      await runMultipart(file, data.session, target.parent_id, (percent) => {
        message.loading({ content: `${target.file_name} 上传中 ${percent}%`, key, duration: 0 })
      })
      message.success({ content: `${target.file_name} 上传成功`, key })
      loadFiles()
      loadSessions()
    } catch (err: any) {
      message.error({
        content: err?.response?.data?.error || err?.message || '恢复上传失败',
        key,
      })
      loadSessions()
    }
  }

  const handleDiscardSession = async (session: UploadSessionInfo) => {
    try {
      await abortMultipartUpload(session.storage_key, session.upload_id, session.storage_policy)
      message.success('已放弃该上传')
    } catch (err: any) {
      message.error(err?.response?.data?.error || '放弃失败')
    }
    loadSessions()
  }

  const formatSize = (n: number) => {
    if (n >= 1 << 30) return (n / (1 << 30)).toFixed(2) + ' GB'
    if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB'
    if (n >= 1 << 10) return (n / (1 << 10)).toFixed(0) + ' KB'
    return n + ' B'
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
    <Layout style={{ minHeight: '100vh', width: '100%' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529', padding: '0 24px' }}>
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
      <Content style={{ padding: 24, width: '100%', maxWidth: 1400, margin: '0 auto', flex: 1 }}>
        <Breadcrumb style={{ marginBottom: 16 }} items={breadcrumb.map(b => ({ title: b.title, key: b.id }))} />
        {pendingSessions.length > 0 && (
          <Alert
            type="info"
            showIcon
            style={{ marginBottom: 16 }}
            message={`有 ${pendingSessions.length} 个未完成的上传，可断点续传`}
            description={
              <Space direction="vertical" style={{ width: '100%' }}>
                {pendingSessions.map(s => (
                  <Space key={s.storage_key} wrap>
                    <span>{s.file_name}（{formatSize(s.size)}，策略 {s.storage_policy}）</span>
                    <Button size="small" type="link" onClick={() => handleResumeClick(s)}>
                      选择原文件继续上传
                    </Button>
                    <Button size="small" type="link" danger onClick={() => handleDiscardSession(s)}>
                      放弃
                    </Button>
                  </Space>
                ))}
              </Space>
            }
          />
        )}
        <input
          ref={resumeInputRef}
          type="file"
          style={{ display: 'none' }}
          onChange={handleResumeFileSelected}
        />
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
