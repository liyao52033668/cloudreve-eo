import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<div>登录页面（待实现）</div>} />
        <Route path="/register" element={<div>注册页面（待实现）</div>} />
        <Route path="/" element={<div>文件管理（待实现）</div>} />
        <Route path="/share/:code" element={<div>分享查看（待实现）</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
