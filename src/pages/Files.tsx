import { useState, useEffect, useCallback, useRef } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space, Select, Alert, Card, Progress, Typography } from 'antd'
import { UploadOutlined, FolderAddOutlined, LogoutOutlined, SettingOutlined, CloudServerOutlined, CloseOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import {
  listFiles,
  listFilesByPolicy,
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

interface UploadTask {
  name: string
  percent: number
  status: 'uploading' | 'done' | 'error'
  error?: string
}

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
  const [uploadTasks, setUploadTasks] = useState<Record<string, UploadTask>>({})
  // 非空时进入「按策略查看」模式：跨目录展示该策略下全部文件
  const [viewPolicy, setViewPolicy] = useState<string>('')
  const resumeInputRef = useRef<HTMLInputElement>(null)
  const resumeTargetRef = useRef<UploadSessionInfo | null>(null)
  const navigate = useNavigate()

  const loadFiles = useCallback(async () => {
    try {
      const res = viewPolicy ? await listFilesByPolicy(viewPolicy) : await listFiles(currentDir)
      setFiles(res.data.files || [])
    } catch (err: any) {
      if (err?.response?.status === 401) return // client 拦截器会跳登录
      message.error(err?.response?.data?.error || '加载文件列表失败')
    }
  }, [currentDir, viewPolicy])

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

  const setTask = (key: string, task: Partial<UploadTask> & { name: string }) => {
    setUploadTasks(prev => {
      const base: UploadTask = prev[key] ?? { name: task.name, percent: 0, status: 'uploading' }
      return { ...prev, [key]: { ...base, ...task } }
    })
  }
  const removeTask = (key: string) => {
    setUploadTasks(prev => {
      const next = { ...prev }
      delete next[key]
      return next
    })
  }

  /** XHR PUT，带字节级进度回调，返回 ETag（可能为 null） */
  const putWithProgress = (
    url: string,
    body: Blob,
    contentType: string | null,
    onBytes: (loaded: number) => void,
  ): Promise<string | null> =>
    new Promise((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('PUT', url)
      if (contentType) xhr.setRequestHeader('Content-Type', contentType)
      xhr.upload.onprogress = (e) => { if (e.lengthComputable) onBytes(e.loaded) }
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(xhr.getResponseHeader('ETag'))
        } else {
          reject(new Error(`HTTP ${xhr.status}`))
        }
      }
      xhr.onerror = () => reject(new Error('网络错误'))
      xhr.send(body)
    })

  const uploadSimple = async (file: File, onProgress: (percent: number) => void) => {
    const contentType = file.type || 'application/octet-stream'
    const { data } = await getUploadURL(file.name, contentType, currentDir, selectedPolicy)
    await putWithProgress(data.upload_url, file, contentType, (loaded) => {
      onProgress(Math.round((loaded / file.size) * 100))
    })
    await uploadCallback(
      file.name,
      data.storage_key,
      file.size,
      contentType,
      currentDir,
      data.storage_policy || selectedPolicy,
    )
  }

  const uploadPartWithRetry = async (
    url: string,
    chunk: Blob,
    partNumber: number,
    onBytes: (loaded: number) => void,
  ): Promise<string> => {
    let lastErr: Error | null = null
    for (let attempt = 1; attempt <= PART_RETRY; attempt++) {
      try {
        const etag = await putWithProgress(url, chunk, null, onBytes)
        if (!etag) throw new Error(`分片 ${partNumber} 未返回 ETag，请检查存储桶 CORS 是否暴露 ETag`)
        return etag.replace(/"/g, '')
      } catch (err: any) {
        lastErr = new Error(`分片 ${partNumber} 上传失败: ${err?.message || err}`)
        onBytes(0)
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
    const partSize = (idx: number) =>
      Math.min((idx + 1) * session.chunk_size, file.size) - idx * session.chunk_size
    let baseBytes = 0
    for (const p of session.uploaded_parts || []) {
      parts[p.part_number - 1] = { part_number: p.part_number, etag: p.etag.replace(/"/g, '') }
      skip.add(p.part_number - 1)
      baseBytes += partSize(p.part_number - 1)
    }
    const partLoaded: Record<number, number> = {}
    const report = () => {
      const inFlight = Object.values(partLoaded).reduce((a, b) => a + b, 0)
      onProgress(Math.min(99, Math.round(((baseBytes + inFlight) / file.size) * 100)))
    }
    report()

    let next = 0
    const worker = async () => {
      while (next < totalParts) {
        const idx = next++
        if (skip.has(idx)) continue
        const start = idx * session.chunk_size
        const chunk = file.slice(start, Math.min(start + session.chunk_size, file.size))
        const etag = await uploadPartWithRetry(session.part_urls[idx], chunk, idx + 1, (loaded) => {
          partLoaded[idx] = loaded
          report()
        })
        parts[idx] = { part_number: idx + 1, etag }
        delete partLoaded[idx]
        baseBytes += partSize(idx)
        report()
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
    onProgress(100)
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
    const key = `upload-${file.name}-${Date.now()}`
    setTask(key, { name: file.name, percent: 0, status: 'uploading' })
    const onProgress = (percent: number) => setTask(key, { name: file.name, percent })
    try {
      if (file.size > MULTIPART_THRESHOLD) {
        await uploadMultipart(file, onProgress)
      } else {
        await uploadSimple(file, onProgress)
      }
      setTask(key, { name: file.name, percent: 100, status: 'done' })
      message.success(`${file.name} 上传成功`)
      setTimeout(() => removeTask(key), 3000)
      loadFiles()
      loadSessions()
    } catch (err: any) {
      const errMsg = err?.response?.data?.error || err?.message || '上传失败'
      setTask(key, { name: file.name, status: 'error', error: errMsg })
      message.error(`${file.name} ${errMsg}`)
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
    setTask(key, { name: target.file_name, percent: 0, status: 'uploading' })
    try {
      const { data } = await resumeMultipartUpload(target.storage_key)
      await runMultipart(file, data.session, target.parent_id, (percent) => {
        setTask(key, { name: target.file_name, percent })
      })
      setTask(key, { name: target.file_name, percent: 100, status: 'done' })
      message.success(`${target.file_name} 上传成功`)
      setTimeout(() => removeTask(key), 3000)
      loadFiles()
      loadSessions()
    } catch (err: any) {
      const errMsg = err?.response?.data?.error || err?.message || '恢复上传失败'
      setTask(key, { name: target.file_name, status: 'error', error: errMsg })
      message.error(errMsg)
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
        {viewPolicy ? (
          <Space style={{ marginBottom: 16 }}>
            <Typography.Text strong>存储策略「{viewPolicy}」的全部文件（跨目录）</Typography.Text>
            <Button size="small" onClick={() => setViewPolicy('')}>返回目录浏览</Button>
          </Space>
        ) : (
          <Breadcrumb style={{ marginBottom: 16 }} items={breadcrumb.map(b => ({ title: b.title, key: b.id }))} />
        )}
        {Object.keys(uploadTasks).length > 0 && (
          <Card size="small" title="上传任务" style={{ marginBottom: 16 }}>
            <Space direction="vertical" style={{ width: '100%' }}>
              {Object.entries(uploadTasks).map(([key, t]) => (
                <div key={key}>
                  <Space style={{ width: '100%', justifyContent: 'space-between', display: 'flex' }}>
                    <Typography.Text ellipsis style={{ maxWidth: 400 }}>{t.name}</Typography.Text>
                    {t.status !== 'uploading' && (
                      <Button
                        type="text"
                        size="small"
                        icon={<CloseOutlined />}
                        onClick={() => removeTask(key)}
                      />
                    )}
                  </Space>
                  <Progress
                    percent={t.percent}
                    size="small"
                    status={t.status === 'error' ? 'exception' : t.status === 'done' ? 'success' : 'active'}
                  />
                  {t.status === 'error' && t.error && (
                    <Typography.Text type="danger" style={{ fontSize: 12 }}>{t.error}</Typography.Text>
                  )}
                </div>
              ))}
            </Space>
          </Card>
        )}
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
          <Select
            style={{ minWidth: 200 }}
            value={viewPolicy}
            onChange={(v) => setViewPolicy(v || '')}
            options={[
              { value: '', label: '按目录浏览' },
              ...policies.map((p) => ({ value: p.name, label: p.name })),
            ]}
            placeholder="按策略查看"
          />
          {!viewPolicy && (
            <>
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
            </>
          )}
        </Space>
        <FileList files={files} onRefresh={loadFiles} onOpenDir={handleOpenDir} />
      </Content>
      <Modal title="新建文件夹" open={mkdirModal} onOk={handleMkdir} onCancel={() => setMkdirModal(false)}>
        <Input value={dirName} onChange={(e) => setDirName(e.target.value)} placeholder="文件夹名称" />
      </Modal>
    </Layout>
  )
}
