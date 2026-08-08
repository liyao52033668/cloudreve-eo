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
  custom_host: string
  base_path: string
  branch: string
  chunk_size: number
  cors_enabled: boolean
  is_default: boolean
  default_quota: number
  created_at?: string
  /** 仅 TeraBox：是否已完成 OAuth 授权 */
  authorized?: boolean
}

/** 管理端编辑详情（含密钥） */
export interface StoragePolicyDetail {
  id: number
  name: string
  type: 's3' | 'github' | 'terabox' | 'filen' | 'dropbox'
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key: string
  force_path_style: boolean
  custom_host: string
  base_path: string
  branch: string
  chunk_size: number
  is_default: boolean
  default_quota: number
}

export interface PolicyForm {
  name: string
  type: 's3' | 'github' | 'terabox' | 'filen' | 'dropbox'
  endpoint: string
  region: string
  bucket: string
  access_key: string
  secret_key: string
  /** 强制 path-style（MinIO / 部分私有 S3 需要） */
  force_path_style: boolean
  /** 自定义下载/预览域名（COS / 七牛的 CDN 加速域名），空表示使用 Endpoint */
  custom_host: string
  /** 上传目录前缀，对象键 = base_path/userID/uuid */
  base_path: string
  /** GitHub 分支名（仅 GitHub 类型使用） */
  branch: string
  /** 分片大小（字节）；0 表示默认 25MB，非 0 时最小 5MB */
  chunk_size: number
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

export const setPolicyCORS = (id: number) =>
  client.post(`/admin/storage/policies/${id}/cors`)

// ============ TeraBox OAuth 授权 ============

/** 获取网页授权（iframe）地址 */
export const getTeraBoxAuthURL = (id: number) =>
  client.get<{ auth_url: string }>(`/admin/storage/policies/${id}/terabox/auth-url`)

/** 用网页授权回调的 code 换取 token */
export const teraboxAuthByCode = (id: number, code: string) =>
  client.post(`/admin/storage/policies/${id}/terabox/auth-code`, { code })

/** 获取扫码授权二维码（base64 图片）与会话信息 */
export const getTeraBoxDeviceCode = (id: number) =>
  client.post<{ qrcode: string; policy_name: string; expires_in: number; interval: number }>(
    `/admin/storage/policies/${id}/terabox/devicecode`,
  )

/** 轮询扫码授权结果 */
export const getTeraBoxAuthStatus = (id: number) =>
  client.post<{ status: 'pending' | 'authorized' | 'expired' | 'error' | 'no_session'; error?: string; message?: string }>(
    `/admin/storage/policies/${id}/terabox/auth-status`,
  )
