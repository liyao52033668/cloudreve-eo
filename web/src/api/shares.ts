import client from './client'

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
