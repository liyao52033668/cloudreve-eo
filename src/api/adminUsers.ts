import client from './client'

export interface AdminUser {
  id: string
  username: string
  is_admin: boolean
  group_id: number
  storage_quota: number
  storage_used: number
  banned: boolean
  created_at: string
  group_name?: string
}

export const listAdminUsers = () =>
  client.get<{ users: AdminUser[] }>('/admin/users')

export const getAdminUser = (id: string) =>
  client.get<{ user: AdminUser }>(`/admin/users/${id}`)

export const createUser = (data: {
  username: string
  password: string
  group_id: number
  is_admin: boolean
}) =>
  client.post<{ user: AdminUser }>('/admin/users', data)

export const updateUser = (id: string, data: {
  username?: string
  password?: string
  group_id?: number
  is_admin?: boolean
}) =>
  client.put<{ user: AdminUser }>(`/admin/users/${id}`, data)

export const deleteUser = (id: string) =>
  client.delete(`/admin/users/${id}`)

export const toggleBanUser = (id: string) =>
  client.put<{ message: string; banned: boolean }>(`/admin/users/${id}/ban`)
