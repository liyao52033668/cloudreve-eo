import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { Button, Layout, Space, Drawer, Menu } from 'antd'
import {
  ArrowLeftOutlined,
  CloudServerOutlined,
  LogoutOutlined,
  MenuOutlined,
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
  const [drawerOpen, setDrawerOpen] = useState(false)

  const user = (() => {
    try {
      return JSON.parse(localStorage.getItem('user') || '{}')
    } catch {
      return {}
    }
  })()
  const isAdmin = user?.is_admin === true
  const username = user?.username || ''

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  const handleMenuClick = (key: string) => {
    setDrawerOpen(false)
    if (key === 'logout') {
      handleLogout()
    } else {
      navigate(key)
    }
  }

  return (
    <>
      <Header
        className="app-header"
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          background: '#001529',
          padding: '0 16px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', minWidth: 0, overflow: 'hidden' }}>
          {title ? (
            <Space style={{ minWidth: 0 }}>
              <Button
                type="text"
                size="small"
                icon={<ArrowLeftOutlined />}
                style={{ color: '#fff' }}
                onClick={() => navigate('/')}
              >
                返回
              </Button>
              <span
                style={{
                  color: '#fff',
                  fontSize: 17,
                  fontWeight: 500,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {title}
              </span>
            </Space>
          ) : (
            <span
              style={{
                color: '#fff',
                fontSize: 18,
                fontWeight: 600,
                cursor: 'pointer',
                userSelect: 'none',
                letterSpacing: '0.5px',
              }}
              onClick={onHome ?? (() => navigate('/'))}
            >
              Cloudreve-EO
            </span>
          )}
        </div>

        {/* 桌面端导航 */}
        <div className="app-header__desktop-nav">
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
        </div>

        {/* 移动端汉堡菜单按钮 */}
        <div className="app-header__mobile-nav">
          <Button
            type="text"
            icon={<MenuOutlined style={{ fontSize: 18 }} />}
            style={{ color: '#fff' }}
            onClick={() => setDrawerOpen(true)}
            aria-label="打开导航菜单"
          />
        </div>
      </Header>

      {/* 移动端导航抽屉 */}
      <Drawer
        title="菜单导航"
        placement="right"
        onClose={() => setDrawerOpen(false)}
        open={drawerOpen}
        styles={{ body: { padding: '12px 0' } }}
        width={260}
      >
        {username && (
          <div style={{ padding: '0 20px 12px', borderBottom: '1px solid #f0f0f0', marginBottom: 8 }}>
            <div style={{ fontSize: 13, color: '#8c8c8c' }}>当前登录</div>
            <div style={{ fontSize: 15, fontWeight: 600, color: '#262626', marginTop: 2 }}>{username}</div>
          </div>
        )}
        <Menu
          mode="inline"
          selectedKeys={[location.pathname]}
          items={[
            {
              key: '/',
              icon: <ArrowLeftOutlined style={{ transform: 'rotate(45deg)' }} />,
              label: '我的文件',
            },
            ...(isAdmin
              ? navItems.map((item) => ({
                  key: item.key,
                  icon: item.icon,
                  label: item.label,
                }))
              : []),
            {
              type: 'divider',
            },
            {
              key: 'logout',
              icon: <LogoutOutlined />,
              label: '退出登录',
              danger: true,
            },
          ]}
          onClick={({ key }) => handleMenuClick(key)}
          style={{ borderRight: 0 }}
        />
      </Drawer>
    </>
  )
}
