import { Navigate, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'

/**
 * 路由守卫：无 token 时跳转登录页，避免未登录直接进入业务页。
 */
export default function RequireAuth({ children }: { children: ReactNode }) {
  const location = useLocation()
  const token = localStorage.getItem('token')

  if (!token) {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }

  return <>{children}</>
}
