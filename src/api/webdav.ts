import client from './client'

export interface WebDAVSettings {
  enabled: boolean
}

export const getWebDAVSettings = () =>
  client.get<WebDAVSettings>('/settings/webdav')

export const updateWebDAVEnabled = (enabled: boolean) =>
  client.put<{ enabled: boolean; message: string }>('/settings/webdav', { enabled })
