import client from './client'

export interface FileItem {
  id: number
  user_id: string
  parent_id: number
  name: string
  is_dir: boolean
  size: number
  mime_type: string
  storage_key: string
  storage_policy: string
  created_at: string
  updated_at: string
}

export interface StoragePolicy {
  name: string
  type: string
  bucket?: string
  endpoint?: string
  is_default: boolean
  default_quota?: number
}

export const listFiles = (parentId: number = 0) =>
  client.get<{ files: FileItem[] }>('/files', { params: { parent_id: parentId } })

/** 跨目录列出某存储策略下的全部文件 */
export const listFilesByPolicy = (policy: string) =>
  client.get<{ files: FileItem[] }>('/files', { params: { policy } })

/** 跨目录列出指定 mime 类型前缀的全部文件（如 image/ 或 video/） */
export const listFilesByMimePrefix = (mimePrefix: string) =>
  client.get<{ files: FileItem[] }>('/files', { params: { mime_prefix: mimePrefix } })

/** 全局搜索文件名；可选限定存储策略 */
export const searchFiles = (keyword: string, policy?: string) =>
  client.get<{ files: FileItem[] }>('/files', {
    params: { search: keyword, ...(policy ? { policy } : {}) },
  })

export const mkdir = (parentId: number, name: string) =>
  client.post<{ file: FileItem }>('/files/mkdir', { parent_id: parentId, name })

export const listStoragePolicies = () =>
  client.get<{ policies: StoragePolicy[]; default: string }>('/storage/policies')

export const getUploadURL = (
  fileName: string,
  contentType: string,
  parentId: number = 0,
) =>
  client.post<{
    upload_url?: string
    storage_key: string
    storage_policy: string
    server_upload?: boolean
    /** 驱动支持分块中转时为 true：大文件需切块走 /upload/chunked 通道 */
    chunked?: boolean
    chunk_size?: number
  }>('/files/upload', {
    file_name: fileName,
    content_type: contentType,
    parent_id: parentId,
  })

export const uploadServer = (
  file: File,
  storageKey: string,
  contentType: string,
  parentId: number = 0,
  storagePolicy: string = '',
) => {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('storage_key', storageKey)
  formData.append('content_type', contentType)
  formData.append('parent_id', parentId.toString())
  if (storagePolicy) formData.append('storage_policy', storagePolicy)
  return client.post('/files/upload/server', formData, {
    headers: {
      'Content-Type': 'multipart/form-data',
    },
    // 服务端中转上传（GitHub / Dropbox）耗时可能超过全局 30s，
    // 取消前端超时，等后端转传完成，避免"假超时"后误以为失败。
    timeout: 0,
  })
}

export const uploadCallback = (
  fileName: string,
  storageKey: string,
  size: number,
  mimeType: string,
  parentId: number = 0,
  storagePolicy: string = '',
) =>
  client.post('/files/upload/callback', {
    file_name: fileName,
    storage_key: storageKey,
    size,
    mime_type: mimeType,
    parent_id: parentId,
    storage_policy: storagePolicy,
  })

/** 服务端中转分块上传会话（百度/TeraBox，网关单请求 body ≤6MB） */
export interface ChunkedSession {
  upload_id: string
  storage_key: string
  chunk_size: number
  /** 秒传命中：上传已完成，无需再传块 */
  fast_upload: boolean
}

export const chunkedInit = (
  fileName: string,
  contentType: string,
  storageKey: string,
  storagePolicy: string,
  size: number,
  parentId: number,
  blockMd5s: string[],
) =>
  client.post<{ session: ChunkedSession }>('/files/upload/chunked', {
    file_name: fileName,
    content_type: contentType,
    storage_key: storageKey,
    storage_policy: storagePolicy,
    size,
    parent_id: parentId,
    block_md5s: blockMd5s,
  })

/** 提交单个块（multipart，单块 ≤5MB 满足网关 body 上限）。
 * 无状态：upload_id/policy 由客户端携带。
 * 返回 upload_id：Dropbox 首块创建会话后经此返回真实会话 ID，后续块须沿用。 */
export const chunkedUploadChunk = (
  storageKey: string,
  storagePolicy: string,
  uploadId: string,
  partSeq: number,
  chunk: Blob,
  onProgress?: (loaded: number) => void,
) => {
  const formData = new FormData()
  formData.append('chunk', chunk)
  formData.append('storage_key', storageKey)
  formData.append('storage_policy', storagePolicy)
  formData.append('upload_id', uploadId)
  formData.append('part_seq', partSeq.toString())
  return client.post<{ ok: boolean; upload_id: string }>('/files/upload/chunked/chunk', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 0,
    onUploadProgress: (e) => {
      if (onProgress && e.loaded !== undefined) onProgress(e.loaded)
    },
  })
}

export const chunkedComplete = (
  storageKey: string,
  storagePolicy: string,
  uploadId: string,
  fileName: string,
  contentType: string,
  size: number,
  parentId: number,
  blockMd5s: string[],
) =>
  client.post('/files/upload/chunked/complete', {
    storage_key: storageKey,
    storage_policy: storagePolicy,
    upload_id: uploadId,
    file_name: fileName,
    content_type: contentType,
    size,
    parent_id: parentId,
    block_md5s: blockMd5s,
  })

export interface MultipartSession {
  upload_id: string
  storage_key: string
  storage_policy: string
  chunk_size: number
  part_urls: string[]
  uploaded_parts?: CompletedPart[]
  file_name?: string
  size?: number
  parent_id?: number
}

export interface CompletedPart {
  part_number: number
  etag: string
}

/** 数据库中的未完成上传会话（用于断点续传列表） */
export interface UploadSessionInfo {
  id: number
  upload_id: string
  storage_key: string
  storage_policy: string
  file_name: string
  content_type: string
  size: number
  chunk_size: number
  parent_id: number
  expires_at: string
  created_at: string
}

export const initMultipartUpload = (
  fileName: string,
  contentType: string,
  size: number,
  parentId: number = 0,
) =>
  client.post<{ session: MultipartSession }>('/files/upload/multipart', {
    file_name: fileName,
    content_type: contentType,
    size,
    parent_id: parentId,
  })

export const completeMultipartUpload = (
  fileName: string,
  storageKey: string,
  uploadId: string,
  size: number,
  mimeType: string,
  parts: CompletedPart[],
  parentId: number = 0,
) =>
  client.post('/files/upload/multipart/complete', {
    file_name: fileName,
    storage_key: storageKey,
    upload_id: uploadId,
    size,
    mime_type: mimeType,
    parts,
    parent_id: parentId,
  })

export const abortMultipartUpload = (
  storageKey: string,
  uploadId: string,
) =>
  client.post('/files/upload/multipart/abort', {
    storage_key: storageKey,
    upload_id: uploadId,
  })

export const listMultipartSessions = () =>
  client.get<{ sessions: UploadSessionInfo[] }>('/files/upload/multipart/sessions')

export const resumeMultipartUpload = (storageKey: string) =>
  client.post<{ session: MultipartSession }>('/files/upload/multipart/resume', {
    storage_key: storageKey,
  })

export const getDownloadURL = (fileId: number, preview = false) =>
  client.get<{ download_url: string }>(`/files/${fileId}/download`, {
    params: preview ? { preview: 1 } : undefined,
  })

/** 文件夹打包 zip 下载；大目录耗时较长，放宽超时 */
export const getDownloadZip = (fileId: number) =>
  client.get<Blob>(`/files/${fileId}/zip`, {
    responseType: 'blob',
    timeout: 0,
  })

export const deleteFile = (fileId: number) =>
  client.delete(`/files/${fileId}`)

/** 批量删除文件/文件夹（含后代） */
export const batchDeleteFiles = (ids: number[]) =>
  client.post('/files/batch/delete', { ids })

/** 批量移动文件/文件夹到同一目标文件夹（0 表示根目录） */
export const batchMoveFiles = (ids: number[], parentId: number) =>
  client.post('/files/batch/move', { ids, parent_id: parentId })

/** 批量打包下载（zip 流）；大目录耗时较长，放宽超时 */
export const batchDownloadZip = (ids: number[]) =>
  client.post<Blob>('/files/batch/download', { ids }, {
    responseType: 'blob',
    timeout: 0,
  })

export const renameFile = (fileId: number, name: string) =>
  client.put(`/files/${fileId}/rename`, { name })

export const moveFile = (fileId: number, parentId: number) =>
  client.put(`/files/${fileId}/move`, { parent_id: parentId })
