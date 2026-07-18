import client from './client'

/** 用户上传可选策略（无密钥） */
export interface StoragePolicyPublic {
  id?: number
  name: string
  type: string
  bucket?: string
  endpoint?: string
  region?: string
  is_default: boolean
  default_quota: number
}

/** 管理端列表项（密钥脱敏） */
export interface StoragePolicyAdmin {
  id: number
  name: string
  type: string
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key_hint: string
  force_path_style: boolean
  base_path: string
  is_default: boolean
  default_quota: number
  created_at?: string
}

/** 管理端编辑详情（含密钥） */
export interface StoragePolicyDetail {
  id: number
  name: string
  type: string
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key: string
  force_path_style: boolean
  base_path: string
  is_default: boolean
  default_quota: number
}

export interface PolicyForm {
  name: string
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key: string
  /** 强制 path-style（MinIO / 部分私有 S3 需要） */
  force_path_style: boolean
  /** 上传目录前缀，对象键 = base_path/userID/uuid */
  base_path: string
  is_default: boolean
  /** 该策略下每用户默认配额（字节） */
  default_quota: number
}

export const listPublicPolicies = () =>
  client.get<{ policies: StoragePolicyPublic[]; default: string }>('/storage/policies')

export const listAdminPolicies = () =>
  client.get<{ policies: StoragePolicyAdmin[] }>('/admin/storage/policies')

export const getAdminPolicy = (id: number) =>
  client.get<{ policy: StoragePolicyDetail }>(`/admin/storage/policies/${id}`)

export const createPolicy = (data: PolicyForm) =>
  client.post<{ policy: StoragePolicyAdmin }>('/admin/storage/policies', data)

export const updatePolicy = (id: number, data: PolicyForm) =>
  client.put<{ policy: StoragePolicyAdmin }>(`/admin/storage/policies/${id}`, data)

export const deletePolicy = (id: number) =>
  client.delete(`/admin/storage/policies/${id}`)

export const setDefaultPolicy = (id: number) =>
  client.post(`/admin/storage/policies/${id}/default`)
