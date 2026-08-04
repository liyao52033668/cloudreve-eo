import client from './client'

export interface AdminUser {
  id: number
  username: string
  is_admin: boolean
  group_id: number
  storage_quota: number
  storage_used: number
  created_at: string
  group_name?: string
}

export const listAdminUsers = () =>
  client.get<{ users: AdminUser[] }>('/admin/users')

export const getAdminUser = (id: number) =>
  client.get<{ user: AdminUser }>(`/admin/users/${id}`)

export const createUser = (data: {
  username: string
  password: string
  group_id: number
  is_admin: boolean
}) =>
  client.post<{ user: AdminUser }>('/admin/users', data)

export const updateUser = (id: number, data: {
  username?: string
  password?: string
  group_id?: number
  is_admin?: boolean
}) =>
  client.put<{ user: AdminUser }>(`/admin/users/${id}`, data)

export const deleteUser = (id: number) =>
  client.delete(`/admin/users/${id}`)
