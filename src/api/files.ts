import client from './client'

export interface FileItem {
  id: number
  user_id: number
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
}

export const listFiles = (parentId: number = 0) =>
  client.get<{ files: FileItem[] }>('/files', { params: { parent_id: parentId } })

export const mkdir = (parentId: number, name: string) =>
  client.post('/files/mkdir', { parent_id: parentId, name })

export const listStoragePolicies = () =>
  client.get<{ policies: StoragePolicy[]; default: string }>('/storage/policies')

export const getUploadURL = (
  fileName: string,
  contentType: string,
  parentId: number = 0,
  storagePolicy?: string,
) =>
  client.post<{ upload_url: string; storage_key: string; storage_policy: string }>('/files/upload', {
    file_name: fileName,
    content_type: contentType,
    parent_id: parentId,
    storage_policy: storagePolicy || undefined,
  })

export const uploadCallback = (
  fileName: string,
  storageKey: string,
  size: number,
  mimeType: string,
  parentId: number = 0,
  storagePolicy?: string,
) =>
  client.post('/files/upload/callback', {
    file_name: fileName,
    storage_key: storageKey,
    size,
    mime_type: mimeType,
    parent_id: parentId,
    storage_policy: storagePolicy || undefined,
  })

export const getDownloadURL = (fileId: number) =>
  client.get<{ download_url: string }>(`/files/${fileId}/download`)

export const deleteFile = (fileId: number) =>
  client.delete(`/files/${fileId}`)

export const renameFile = (fileId: number, name: string) =>
  client.put(`/files/${fileId}/rename`, { name })

export const moveFile = (fileId: number, parentId: number) =>
  client.put(`/files/${fileId}/move`, { parent_id: parentId })
