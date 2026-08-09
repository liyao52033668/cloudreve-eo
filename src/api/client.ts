import axios from 'axios'

const client = axios.create({
  baseURL: '/api',
  // 不设全局超时：大文件上传/zip 打包下载耗时可能远超 30s，
  // 超时会导致传输中断前功尽弃；网络异常由浏览器/axios 自身错误处理
})

client.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      const url = String(error.config?.url ?? '')
      // 登录/注册接口的 401 表示凭据错误，交由页面提示，不触发全局登出跳转
      const isAuthCredentialRequest =
        url.includes('/auth/login') || url.includes('/auth/register')
      if (!isAuthCredentialRequest) {
        localStorage.removeItem('token')
        localStorage.removeItem('user')
        if (!window.location.pathname.startsWith('/login')) {
          window.location.href = '/login'
        }
      }
    }
    return Promise.reject(error)
  }
)

export default client
