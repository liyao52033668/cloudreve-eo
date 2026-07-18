import client from './client'

export interface UserProfile {
  id: number
  username: string
  is_admin: boolean
  storage_quota: number
  storage_used: number
  created_at?: string
}

export interface UserStoragePolicyUsage {
  name: string
  is_default: boolean
  default_quota: number
  used: number
}

export interface ProfileResponse {
  user: UserProfile
  storage_policies?: UserStoragePolicyUsage[]
}

export const getProfile = () =>
  client.get<ProfileResponse>('/user/profile')
