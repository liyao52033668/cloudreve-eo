import client from './client'

export interface LoginParams {
  username: string
  password: string
}

export interface RegisterParams {
  username: string
  password: string
}

export interface AuthResponse {
  token: string
  user: {
    id: number
    username: string
    storage_quota: number
    storage_used: number
  }
}

export const login = (params: LoginParams) =>
  client.post<AuthResponse>('/auth/login', params)

export const register = (params: RegisterParams) =>
  client.post<AuthResponse>('/auth/register', params)
