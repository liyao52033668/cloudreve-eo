import { useNavigate, useLocation } from 'react-router-dom'
import { Button, Layout, Space } from 'antd'
import {
  ArrowLeftOutlined,
  CloudServerOutlined,
  LogoutOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from '@ant-design/icons'

const { Header } = Layout

const navItems = [
  { key: '/storage-policies', icon: <CloudServerOutlined />, label: '存储策略' },
  { key: '/user-groups', icon: <TeamOutlined />, label: '用户组' },
  { key: '/users', icon: <UserOutlined />, label: '用户' },
  { key: '/settings', icon: <SettingOutlined />, label: '参数设置' },
]

export default function AppHeader({ title, onHome }: { title?: string; onHome?: () => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const isAdmin = (() => {
    try {
      return JSON.parse(localStorage.getItem('user') || '{}')?.is_admin === true
    } catch {
      return false
    }
  })()

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  return (
    <Header
      style={{
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        background: '#001529',
        padding: '0 24px',
      }}
    >
      {title ? (
        <Space>
          <Button type="text" icon={<ArrowLeftOutlined />} style={{ color: '#fff' }} onClick={() => navigate('/')}>
            返回
          </Button>
          <span style={{ color: '#fff', fontSize: 18 }}>{title}</span>
        </Space>
      ) : (
        <span
          style={{ color: '#fff', fontSize: 18, cursor: 'pointer', userSelect: 'none' }}
          onClick={onHome ?? (() => navigate('/'))}
        >
          Cloudreve-EO
        </span>
      )}
      <Space>
        {isAdmin &&
          navItems.map((item) => (
            <Button
              key={item.key}
              type="text"
              icon={item.icon}
              style={{ color: '#fff' }}
              disabled={location.pathname === item.key}
              onClick={() => navigate(item.key)}
            >
              {item.label}
            </Button>
          ))}
        <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>
          退出
        </Button>
      </Space>
    </Header>
  )
}
