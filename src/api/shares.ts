import client from './client'
import type { FileItem } from './files'

export interface ShareInfo {
  id: number
  code: string
  expire_at: string | null
  views: number
  created_at: string
}

/** 创建分享：支持单个或多个文件（多文件分享访问者看到文件列表） */
export const createShare = (fileIds: number[], password?: string, expireAt?: string) =>
  client.post('/shares', { file_ids: fileIds, password, expire_at: expireAt })

/** 获取分享信息：返回根文件列表（单文件分享长度为 1，多文件分享为多个） */
export const getShare = (code: string, password?: string) =>
  client.get<{ share: ShareInfo; files: FileItem[] }>(`/shares/${code}`, { params: { password } })

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

/** 分享全部文件打包 zip 下载；大目录耗时较长，放宽超时 */
export const getShareZip = (code: string, password?: string) =>
  client.get<Blob>(`/shares/${code}/zip`, {
    params: { password },
    responseType: 'blob',
    timeout: 0,
  })

/** 分享内选中文件打包 zip 下载 */
export const getShareZipSelected = (code: string, ids: number[], password?: string) =>
  client.post<Blob>(`/shares/${code}/zip`, { ids }, {
    params: { password },
    responseType: 'blob',
    timeout: 0,
  })
