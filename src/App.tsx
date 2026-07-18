import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import Files from './pages/Files'
import ShareView from './pages/ShareView'
import Settings from './pages/Settings'
import StoragePolicies from './pages/StoragePolicies'
import RequireAuth from './components/RequireAuth'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route path="/share/:code" element={<ShareView />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <Files />
            </RequireAuth>
          }
        />
        <Route
          path="/settings"
          element={
            <RequireAuth>
              <Settings />
            </RequireAuth>
          }
        />
        <Route
          path="/storage-policies"
          element={
            <RequireAuth>
              <StoragePolicies />
            </RequireAuth>
          }
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
