import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { Layout, Breadcrumb, Button, Upload, Modal, Input, message, Space, Select, Segmented, Alert, Card, Progress, Typography, Divider } from 'antd'
import { UploadOutlined, FolderAddOutlined, FileAddOutlined, FolderOpenOutlined, LogoutOutlined, SettingOutlined, CloudServerOutlined, CloseOutlined, ArrowLeftOutlined, SearchOutlined, TeamOutlined, UserOutlined, BarsOutlined, AppstoreOutlined } from '@ant-design/icons'
import FileList from '../components/FileList'
import {
  listFiles,
  listFilesByPolicy,
  listFilesByMimePrefix,
  searchFiles,
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
import { isImage, isVideo } from '../utils/fileType'
import { useNavigate } from 'react-router-dom'

const { Header, Content } = Layout

interface BreadcrumbItem { title: string; id: number }

interface UploadTask {
  name: string
  percent: number
  status: 'uploading' | 'done' | 'error'
  error?: string
}

/** 目录选择 input 的非标准属性，用类型安全的 spread 方式挂载 */
const folderDirectoryInputProps: React.InputHTMLAttributes<HTMLInputElement> & {
  webkitdirectory?: string
  directory?: string
} = {
  webkitdirectory: '',
  directory: '',
}

export default function Files() {
  const [files, setFiles] = useState<FileItem[]>([])
  const [currentDir, setCurrentDir] = useState(0)
  const [breadcrumb, setBreadcrumb] = useState<BreadcrumbItem[]>([{ title: '根目录', id: 0 }])
  const [mkdirModal, setMkdirModal] = useState(false)
  const [dirName, setDirName] = useState('')
  const [newFileModal, setNewFileModal] = useState(false)
  const [newFileName, setNewFileName] = useState('')
  const [newFileContent, setNewFileContent] = useState('')
  const [newFileSubmitting, setNewFileSubmitting] = useState(false)
  const [policies, setPolicies] = useState<StoragePolicy[]>([])
  const [isAdmin, setIsAdmin] = useState(false)
  const [pendingSessions, setPendingSessions] = useState<UploadSessionInfo[]>([])
  const [uploadTasks, setUploadTasks] = useState<Record<string, UploadTask>>({})
  // 非空时进入「按策略查看」模式：跨目录展示该策略下全部文件
  const [viewPolicy, setViewPolicy] = useState<string>('')
  // 图片/视频分类筛选：在当前浏览范围内只展示对应类型的文件
  const [category, setCategory] = useState<'all' | 'image' | 'video'>('all')
  const [viewMode, setViewMode] = useState<'list' | 'grid'>('list')
  const [searchKeyword, setSearchKeyword] = useState('')
  const [debouncedSearch, setDebouncedSearch] = useState('')
  const resumeInputRef = useRef<HTMLInputElement>(null)
  const resumeTargetRef = useRef<UploadSessionInfo | null>(null)
  const folderInputRef = useRef<HTMLInputElement>(null)
  const loadRequestIdRef = useRef(0)
  const navigate = useNavigate()

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedSearch(searchKeyword.trim())
    }, 300)
    return () => window.clearTimeout(timer)
  }, [searchKeyword])

  const loadFiles = useCallback(async () => {
    const requestId = ++loadRequestIdRef.current
    try {
      const res = debouncedSearch
        ? await searchFiles(debouncedSearch, viewPolicy || undefined)
        : category !== 'all'
          ? await listFilesByMimePrefix(category === 'image' ? 'image/' : 'video/')
          : viewPolicy
            ? await listFilesByPolicy(viewPolicy)
            : await listFiles(currentDir)
      if (requestId !== loadRequestIdRef.current) return
      setFiles(res.data.files || [])
    } catch (err: any) {
      if (requestId !== loadRequestIdRef.current) return
      if (err?.response?.status === 401) return // client 拦截器会跳登录
      message.error(err?.response?.data?.error || '加载文件列表失败')
    }
  }, [currentDir, viewPolicy, debouncedSearch, category])

  const loadPolicies = useCallback(async () => {
    try {
      const res = await listStoragePolicies()
      setPolicies(res.data.policies || [])
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

  const handleOpenDir = (dirId: number) => {
    setSearchKeyword('')
    setViewPolicy('')
    setCurrentDir(dirId)
    if (dirId === 0) {
      setBreadcrumb([{ title: '根目录', id: 0 }])
      return
    }
    const existingIdx = breadcrumb.findIndex(b => b.id === dirId)
    if (existingIdx >= 0) {
      setBreadcrumb(breadcrumb.slice(0, existingIdx + 1))
      return
    }
    const name = files.find(f => f.id === dirId)?.name || ''
    setBreadcrumb(prev => [...prev, { title: name, id: dirId }])
  }

  const handleBreadcrumbClick = (index: number) => {
    const target = breadcrumb[index]
    if (!target || target.id === currentDir) return
    setSearchKeyword('')
    setViewPolicy('')
    setCurrentDir(target.id)
    setBreadcrumb(breadcrumb.slice(0, index + 1))
  }

  const handleGoUp = () => {
    if (breadcrumb.length <= 1) return
    const parent = breadcrumb[breadcrumb.length - 2]
    setSearchKeyword('')
    setViewPolicy('')
    setCurrentDir(parent.id)
    setBreadcrumb(breadcrumb.slice(0, -1))
  }

  const handleGoHome = () => {
    setViewPolicy('')
    setCurrentDir(0)
    setBreadcrumb([{ title: '根目录', id: 0 }])
    setSearchKeyword('')
    navigate('/')
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

  const uploadSimple = async (
    file: File,
    onProgress: (percent: number) => void,
    parentId: number = currentDir,
  ) => {
    const contentType = file.type || 'application/octet-stream'
    const { data } = await getUploadURL(file.name, contentType, parentId)
    await putWithProgress(data.upload_url, file, contentType, (loaded) => {
      onProgress(file.size === 0 ? 100 : Math.round((loaded / file.size) * 100))
    })
    await uploadCallback(
      file.name,
      data.storage_key,
      file.size,
      contentType,
      parentId,
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
    )
    onProgress(100)
  }

  const uploadMultipart = async (
    file: File,
    onProgress: (percent: number) => void,
    parentId: number = currentDir,
  ) => {
    const contentType = file.type || 'application/octet-stream'
    const { data } = await initMultipartUpload(file.name, contentType, file.size, parentId)
    try {
      await runMultipart(file, data.session, parentId, onProgress)
    } catch (err) {
      // 不 abort：会话保留在服务端，可稍后从「未完成的上传」恢复
      loadSessions()
      throw err
    }
  }

  /** 通用上传：可指定 parentId 与任务展示名；返回是否成功 */
  const handleUpload = async (
    file: File,
    parentId: number = currentDir,
    displayName?: string,
    options: { refresh?: boolean; showSuccess?: boolean } = {},
  ): Promise<boolean> => {
    const taskName = displayName || file.name
    const { refresh = true, showSuccess = true } = options
    const key = `upload-${taskName}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    setTask(key, { name: taskName, percent: 0, status: 'uploading' })
    const onProgress = (percent: number) => setTask(key, { name: taskName, percent })
    try {
      if (file.size > MULTIPART_THRESHOLD) {
        await uploadMultipart(file, onProgress, parentId)
      } else {
        await uploadSimple(file, onProgress, parentId)
      }
      setTask(key, { name: taskName, percent: 100, status: 'done' })
      if (showSuccess) message.success(`${taskName} 上传成功`)
      setTimeout(() => removeTask(key), 3000)
      if (refresh) {
        loadFiles()
        loadSessions()
      }
      return true
    } catch (err: any) {
      const errMsg = err?.response?.data?.error || err?.message || '上传失败'
      setTask(key, { name: taskName, status: 'error', error: errMsg })
      message.error(`${taskName} ${errMsg}`)
      return false
    }
  }

  /** Ant Design Upload beforeUpload：多选时每个文件独立上传并各自显示进度 */
  const beforeUpload = (file: File) => {
    void handleUpload(file)
    return false
  }

  const handleFolderSelected = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const filesArr = Array.from(e.target.files || [])
    // 先复制 FileList 再清空，允许再次选择同一目录
    e.target.value = ''
    if (filesArr.length === 0) return
    // Promise 缓存：同一相对路径只创建一次，避免并发 mkdir 竞态
    const dirPromiseCache = new Map<string, Promise<number>>()
    dirPromiseCache.set('', Promise.resolve(currentDir))

    const ensureChildDir = (parentId: number, name: string, pathKey: string): Promise<number> => {
      const cached = dirPromiseCache.get(pathKey)
      if (cached) return cached
      const promise = (async () => {
        const res = await listFiles(parentId)
        const existing = (res.data.files || []).find((f) => f.is_dir && f.name === name)
        if (existing) return existing.id
        const created = await mkdir(parentId, name)
        return created.data.file.id
      })()
      dirPromiseCache.set(pathKey, promise)
      return promise
    }

    const resolveParentId = async (relativePath: string): Promise<number> => {
      const parts = relativePath.split('/').filter(Boolean)
      // webkitRelativePath 形如 "folder/sub/file.txt"，最后一段是文件名
      const dirParts = parts.slice(0, -1)
      let pathKey = ''
      let parentId = currentDir
      for (const seg of dirParts) {
        pathKey = pathKey ? `${pathKey}/${seg}` : seg
        parentId = await ensureChildDir(parentId, seg, pathKey)
      }
      return parentId
    }

    // 路径创建通过 Promise 缓存串行化，文件上传并发限制为 2
    const FOLDER_UPLOAD_CONCURRENCY = 2
    let cursor = 0
    let failCount = 0
    const worker = async () => {
      while (cursor < filesArr.length) {
        const idx = cursor++
        const file = filesArr[idx]
        const relativePath = file.webkitRelativePath || file.name
        try {
          const parentId = await resolveParentId(relativePath)
          const ok = await handleUpload(file, parentId, relativePath, {
            refresh: false,
            showSuccess: false,
          })
          if (!ok) failCount++
        } catch (err: any) {
          failCount++
          const errMsg = err?.response?.data?.error || err?.message || '处理失败'
          message.error(`${relativePath} ${errMsg}`)
        }
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(FOLDER_UPLOAD_CONCURRENCY, filesArr.length) }, () => worker()),
    )
    if (failCount === 0) {
      message.success(`文件夹上传完成（${filesArr.length} 个文件）`)
    } else {
      message.warning(`文件夹上传结束：${filesArr.length - failCount} 成功，${failCount} 失败`)
    }
    loadFiles()
    loadSessions()
  }

  const handleCreateTextFile = async () => {
    const fileName = newFileName.trim()
    if (!fileName) {
      message.warning('文件名不能为空')
      return
    }
    setNewFileSubmitting(true)
    try {
      const file = new File([newFileContent], fileName, { type: 'text/plain;charset=utf-8' })
      const ok = await handleUpload(file, currentDir, fileName)
      if (ok) {
        setNewFileModal(false)
        setNewFileName('')
        setNewFileContent('')
      }
    } finally {
      setNewFileSubmitting(false)
    }
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
      await abortMultipartUpload(session.storage_key, session.upload_id)
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

  const activeSearch = searchKeyword.trim()
  const canGoUp = !viewPolicy && !activeSearch && breadcrumb.length > 1

  const visibleFiles = useMemo(() => {
    if (category === 'image') return files.filter((f) => !f.is_dir && isImage(f))
    if (category === 'video') return files.filter((f) => !f.is_dir && isVideo(f))
    return files
  }, [files, category])

  const browseValue = category === 'all' ? viewPolicy : `__cat__${category}`

  return (
    <Layout style={{ minHeight: '100vh', width: '100%' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529', padding: '0 24px' }}>
        <span
          style={{ color: '#fff', fontSize: 18, cursor: 'pointer', userSelect: 'none' }}
          onClick={handleGoHome}
        >
          Cloudreve-EO
        </span>
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
                icon={<TeamOutlined />}
                type="text"
                style={{ color: '#fff' }}
                onClick={() => navigate('/user-groups')}
              >
                用户组
              </Button>
              <Button
                icon={<UserOutlined />}
                type="text"
                style={{ color: '#fff' }}
                onClick={() => navigate('/users')}
              >
                用户
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
        <section className="files-toolbar" aria-label="文件浏览工具栏">
          <div className="files-toolbar__top">
            <div className="files-toolbar__context">
              {activeSearch ? (
                <>
                  <Typography.Text strong className="files-toolbar__context-text" ellipsis={{ tooltip: true }}>
                    {viewPolicy
                      ? `存储策略「${viewPolicy}」中全局搜索「${activeSearch}」的结果`
                      : `全局搜索「${activeSearch}」的结果`}
                  </Typography.Text>
                  <Button size="small" onClick={() => setSearchKeyword('')}>
                    清除搜索
                  </Button>
                </>
              ) : viewPolicy ? (
                <>
                  <div className="files-toolbar__policy-path" aria-label="当前存储策略范围">
                    <Typography.Text type="secondary">存储策略</Typography.Text>
                    <span className="files-toolbar__policy-sep" aria-hidden>/</span>
                    <Typography.Text strong ellipsis={{ tooltip: true }} className="files-toolbar__policy-name">
                      {viewPolicy}
                    </Typography.Text>
                    <span className="files-toolbar__policy-sep" aria-hidden>/</span>
                    <Typography.Text>全部文件</Typography.Text>
                  </div>
                  <Button
                    size="small"
                    onClick={() => {
                      setViewPolicy('')
                      setSearchKeyword('')
                    }}
                  >
                    返回目录浏览
                  </Button>
                </>
              ) : category !== 'all' ? (
                <>
                  <div className="files-toolbar__policy-path" aria-label="当前分类范围">
                    <Typography.Text type="secondary">分类</Typography.Text>
                    <span className="files-toolbar__policy-sep" aria-hidden>/</span>
                    <Typography.Text strong className="files-toolbar__policy-name">
                      {category === 'image' ? '图片' : '视频'}
                    </Typography.Text>
                    <span className="files-toolbar__policy-sep" aria-hidden>/</span>
                    <Typography.Text>当前目录</Typography.Text>
                  </div>
                  <Button size="small" onClick={() => setCategory('all')}>
                    返回全部文件
                  </Button>
                </>
              ) : (
                <>
                  {canGoUp && (
                    <Button icon={<ArrowLeftOutlined />} onClick={handleGoUp}>
                      返回上一级
                    </Button>
                  )}
                  <div className="files-toolbar__breadcrumb">
                    <Typography.Text type="secondary" className="files-toolbar__location-label">
                      当前位置
                    </Typography.Text>
                    <Breadcrumb
                      items={breadcrumb.map((b, index) => ({
                        key: b.id,
                        title: index === breadcrumb.length - 1 ? (
                          <span className="files-toolbar__crumb-current">{b.title}</span>
                        ) : (
                          <a onClick={() => handleBreadcrumbClick(index)}>{b.title}</a>
                        ),
                      }))}
                    />
                  </div>
                </>
              )}
            </div>

            {!viewPolicy && (
              <div className="files-toolbar__actions">
                <div className="files-toolbar__action-group" role="group" aria-label="上传">
                  <Upload beforeUpload={beforeUpload} showUploadList={false} multiple>
                    <Button icon={<UploadOutlined />} type="primary">
                      上传文件
                    </Button>
                  </Upload>
                  <Button icon={<FolderOpenOutlined />} onClick={() => folderInputRef.current?.click()}>
                    上传文件夹
                  </Button>
                </div>
                <Divider type="vertical" className="files-toolbar__action-divider" />
                <div className="files-toolbar__action-group" role="group" aria-label="新建">
                  <Button icon={<FileAddOutlined />} onClick={() => setNewFileModal(true)}>
                    新建文件
                  </Button>
                  <Button icon={<FolderAddOutlined />} onClick={() => setMkdirModal(true)}>
                    新建文件夹
                  </Button>
                </div>
              </div>
            )}
          </div>

          <div className="files-toolbar__bottom">
            <Select
              className="files-toolbar__select-browse"
              value={browseValue}
              onChange={(v) => {
                if (v === '__cat__image' || v === '__cat__video') {
                  setCategory(v === '__cat__image' ? 'image' : 'video')
                  setViewPolicy('')
                } else {
                  setCategory('all')
                  setViewPolicy(v || '')
                }
                setSearchKeyword('')
              }}
              options={[
                { value: '', label: '按目录浏览' },
                { value: '__cat__image', label: '图片分类' },
                { value: '__cat__video', label: '视频分类' },
                ...policies.map((p) => ({ value: p.name, label: p.name })),
              ]}
              placeholder="浏览方式"
              aria-label="浏览方式"
            />
            <Input
              className="files-toolbar__search"
              allowClear
              prefix={<SearchOutlined />}
              placeholder="全局搜索文件名"
              value={searchKeyword}
              onChange={(e) => setSearchKeyword(e.target.value)}
              aria-label="全局搜索文件名"
              autoComplete="off"
            />
            <Segmented
              value={viewMode}
              onChange={(v) => setViewMode(v as 'list' | 'grid')}
              options={[
                { value: 'list', icon: <BarsOutlined /> },
                { value: 'grid', icon: <AppstoreOutlined /> },
              ]}
              aria-label="视图切换"
            />
          </div>
        </section>

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
        <input
          ref={folderInputRef}
          type="file"
          style={{ display: 'none' }}
          multiple
          {...folderDirectoryInputProps}
          onChange={handleFolderSelected}
        />
        <FileList files={visibleFiles} viewMode={viewMode} onRefresh={loadFiles} onOpenDir={handleOpenDir} />
      </Content>
      <Modal title="新建文件夹" open={mkdirModal} onOk={handleMkdir} onCancel={() => setMkdirModal(false)}>
        <Input value={dirName} onChange={(e) => setDirName(e.target.value)} placeholder="文件夹名称" />
      </Modal>
      <Modal
        title="新建文本文件"
        open={newFileModal}
        onOk={handleCreateTextFile}
        confirmLoading={newFileSubmitting}
        onCancel={() => {
          if (newFileSubmitting) return
          setNewFileModal(false)
        }}
        destroyOnClose={false}
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Input
            value={newFileName}
            onChange={(e) => setNewFileName(e.target.value)}
            placeholder="文件名，例如 notes.txt"
          />
          <Input.TextArea
            value={newFileContent}
            onChange={(e) => setNewFileContent(e.target.value)}
            placeholder="文件内容（可为空）"
            rows={8}
          />
        </Space>
      </Modal>
    </Layout>
  )
}
