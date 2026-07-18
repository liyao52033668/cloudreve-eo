import client from './client'

export interface UserProfile {
  id: number
  username: string
  is_admin: boolean
  storage_quota: number
  storage_used: number
  created_at?: string
}

export const getProfile = () =>
  client.get<{ user: UserProfile }>('/user/profile')
