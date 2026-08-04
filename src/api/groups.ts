import client from './client'

export interface GroupView {
  id: number
  name: string
  storage_policy: string
  max_storage: number
  is_default: boolean
  created_at: string
  updated_at: string
  user_count: number
  storage_used: number
}

export interface GroupForm {
  name: string
  storage_policy: string
  max_storage: number
  is_default: boolean
}

export interface GroupDetail extends GroupView {}

export const listAdminGroups = () =>
  client.get<{ groups: GroupView[] }>('/admin/groups')

export const getAdminGroup = (id: number) =>
  client.get<{ group: GroupDetail }>(`/admin/groups/${id}`)

export const createGroup = (data: GroupForm) =>
  client.post<{ group: GroupView }>('/admin/groups', data)

export const updateGroup = (id: number, data: GroupForm) =>
  client.put<{ group: GroupView }>(`/admin/groups/${id}`, data)

export const deleteGroup = (id: number) =>
  client.delete(`/admin/groups/${id}`)

export const setDefaultGroup = (id: number) =>
  client.post(`/admin/groups/${id}/default`)
