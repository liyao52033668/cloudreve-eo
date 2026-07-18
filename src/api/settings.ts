import axios from 'axios'
import client from './client'

export interface SecuritySettings {
  jwt_secret: string
  allow_register: boolean
}

export interface RotateJWTResponse {
  jwt_secret: string
  message: string
}

export interface SiteInfo {
  allow_register: boolean
}

export const getSecuritySettings = () =>
  client.get<SecuritySettings>('/settings/security')

export const rotateJWTSecret = () =>
  client.post<RotateJWTResponse>('/settings/security/rotate-jwt')

export const updateAllowRegister = (allow_register: boolean) =>
  client.put<{ allow_register: boolean; message: string }>('/settings/register', {
    allow_register,
  })

function parseAllowRegister(value: unknown): boolean {
  // 仅明确关闭时返回 false；缺省 / 异常默认开放，避免误藏注册入口
  if (value === false || value === 0 || value === 'false' || value === '0') {
    return false
  }
  return true
}

/**
 * 公开接口，无需登录。
 * 使用独立 axios 实例，避免带上失效 token 触发全局 401 跳转。
 * 关闭注册 → allow_register=false → 登录页隐藏「去注册」。
 */
export async function getSiteInfo(): Promise<SiteInfo> {
  try {
    const res = await axios.get('/api/site', { timeout: 10000 })
    const data = res.data
    if (data && typeof data === 'object' && !Array.isArray(data) && 'allow_register' in data) {
      return { allow_register: parseAllowRegister((data as { allow_register: unknown }).allow_register) }
    }
  } catch {
    // 接口不可用时默认开放
  }
  return { allow_register: true }
}
