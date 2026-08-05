import client from './client'
import type { FileItem } from './files'

export interface ShareInfo {
  id: number
  code: string
  expire_at: string | null
  views: number
  created_at: string
}

export const createShare = (fileId: number, password?: string, expireAt?: string) =>
  client.post('/shares', { file_id: fileId, password, expire_at: expireAt })

export const getShare = (code: string, password?: string) =>
  client.get(`/shares/${code}`, { params: { password } })

export const getShareDownload = (code: string, password?: string) =>
  client.get<{ download_url: string }>(`/shares/${code}/download`, { params: { password } })

export const getShareFiles = (code: string, parentId: number, password?: string) =>
  client.get<{ files: FileItem[] }>(`/shares/${code}/files`, {
    params: { parent_id: parentId, password },
  })

/** 分享目录内单个文件下载 */
export const getShareChildDownload = (code: string, fileId: number, password?: string) =>
  client.get<{ download_url: string }>(`/shares/${code}/files/${fileId}/download`, {
    params: { password },
  })

/** 文件夹打包 zip 下载；大目录耗时较长，放宽超时 */
export const getShareZip = (code: string, password?: string) =>
  client.get<Blob>(`/shares/${code}/zip`, {
    params: { password },
    responseType: 'blob',
    timeout: 0,
  })
