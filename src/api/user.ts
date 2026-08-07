import client from './client'

export interface UserProfile {
  id: string
  username: string
  is_admin: boolean
  storage_quota: number
  storage_used: number
  created_at?: string
  group_id: number
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
  group?: {
    id: number
    name: string
    storage_policy?: string
    storage_policies?: string[]
    max_storage: number
    /** 展示用有效总额度：组容量，或 max_storage=0 时绑定策略默认配额总和 */
    effective_max_storage?: number
    is_default: boolean
    created_at?: string
    updated_at?: string
    user_count?: number
    storage_used?: number
  }
}

export const getProfile = () =>
  client.get<ProfileResponse>('/user/profile')
