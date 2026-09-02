import { useCallback, useEffect, useState } from 'react'
import { Layout, Card, Button, Typography, Space, message, Modal, Input, Alert, Switch, Divider, Menu } from 'antd'
import { ReloadOutlined, CopyOutlined, LinkOutlined, UserOutlined, SafetyOutlined, CloudOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  getSecuritySettings,
  rotateJWTSecret,
  updateAllowRegister,
} from '../api/settings'
import { getProfile } from '../api/user'
import { getWebDAVSettings, updateWebDAVEnabled } from '../api/webdav'
import { getWebDAVStatus, setWebDAVPassword, getWebDAVPassword } from '../api/user'
import AppHeader from '../components/AppHeader'
import { copyText } from '../utils/clipboard'

const { Content, Sider } = Layout
const { Text, Paragraph } = Typography

export default function Settings() {
  const navigate = useNavigate()
  const [activeKey, setActiveKey] = useState('register')
  const [secret, setSecret] = useState('')
  const [allowRegister, setAllowRegister] = useState(true)
  const [loading, setLoading] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [registerSaving, setRegisterSaving] = useState(false)
  const [webdavEnabled, setWebdavEnabled] = useState(false)
  const [webdavHasPassword, setWebdavHasPassword] = useState(false)
  const [webdavSaving, setWebdavSaving] = useState(false)
  const [webdavPassword, setWebdavPassword] = useState('')
  const [webdavPasswordSaving, setWebdavPasswordSaving] = useState(false)
  // WebDAV 连接信息：用户名 = 当前登录用户名；密码明文仅在本会话设置后保留一次，刷新即失。
  const [webdavUsername, setWebdavUsername] = useState('')
  const [webdavPasswordPlain, setWebdavPasswordPlain] = useState('')
  const [connInfoOpen, setConnInfoOpen] = useState(false)

  const ensureAdmin = useCallback(async () => {
    try {
      const res = await getProfile()
      if (!res.data.user?.is_admin) {
        message.error('需要管理员权限')
        navigate('/')
        return null
      }
      // WebDAV 用户名即登录用户名
      setWebdavUsername(res.data.user?.username || '')
      return res.data.user
    } catch {
      navigate('/login')
      return null
    }
  }, [navigate])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const profile = await ensureAdmin()
      if (!profile) return
      const res = await getSecuritySettings()
      setSecret(res.data.jwt_secret || '')
      // 仅明确 false 时视为关闭，避免字段缺失时误显示为关
      setAllowRegister(res.data.allow_register !== false)

      // 加载 WebDAV 设置
      const webdavRes = await getWebDAVSettings()
      setWebdavEnabled(webdavRes.data.enabled)

      // 加载当前用户 WebDAV 密码状态
      const statusRes = await getWebDAVStatus()
      setWebdavHasPassword(statusRes.data.has_password)
    } catch (err: any) {
      if (err.response?.status === 403) {
        message.error('需要管理员权限')
        navigate('/')
      } else {
        message.error(err.response?.data?.error || '加载设置失败')
      }
    } finally {
      setLoading(false)
    }
  }, [ensureAdmin, navigate])

  useEffect(() => {
    load()
  }, [load])

  const handleCopy = async () => {
    try {
      await copyText(secret)
      message.success('已复制到剪贴板')
    } catch {
      message.error('复制失败')
    }
  }

  const handleRotate = () => {
    Modal.confirm({
      title: '确认轮转 JWT 主密钥？',
      content: '轮转后所有用户的登录令牌将立即失效，需要重新登录。',
      okText: '确认轮转',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        setRotating(true)
        try {
          const res = await rotateJWTSecret()
          setSecret(res.data.jwt_secret)
          message.success(res.data.message || '主密钥已轮转')
          localStorage.removeItem('token')
          localStorage.removeItem('user')
          setTimeout(() => navigate('/login'), 1500)
        } catch (err: any) {
          message.error(err.response?.data?.error || '轮转失败')
        } finally {
          setRotating(false)
        }
      },
    })
  }

  const handleAllowRegisterChange = async (checked: boolean) => {
    setRegisterSaving(true)
    try {
      await updateAllowRegister(checked)
      setAllowRegister(checked)
      message.success(checked ? '已开放注册' : '已关闭注册')
    } catch (err: any) {
      message.error(err.response?.data?.error || '更新失败')
    } finally {
      setRegisterSaving(false)
    }
  }

  const handleWebDAVChange = async (checked: boolean) => {
    setWebdavSaving(true)
    try {
      await updateWebDAVEnabled(checked)
      setWebdavEnabled(checked)
      message.success(checked ? '已启用 WebDAV 服务' : '已禁用 WebDAV 服务')
    } catch (err: any) {
      message.error(err.response?.data?.error || '更新失败')
    } finally {
      setWebdavSaving(false)
    }
  }

  const handleSetWebDAVPassword = async (password: string = webdavPassword) => {
    const pwd = password
    if (pwd.length < 6) {
      message.error('密码长度至少 6 位')
      return
    }
    setWebdavPasswordSaving(true)
    try {
      await setWebDAVPassword(pwd)
      setWebdavHasPassword(true)
      setWebdavPassword('')
      // 保留明文，供查看连接信息；仅本会话有效，刷新即失
      setWebdavPasswordPlain(pwd)
      message.success('WebDAV 密码已设置')
    } catch (err: any) {
      message.error(err.response?.data?.error || '设置失败')
    } finally {
      setWebdavPasswordSaving(false)
    }
  }

  const getWebDAVURL = () => {
    const baseUrl = window.location.origin
    return `${baseUrl}/api/dav/`
  }

  const handleCopyConnInfo = async () => {
    const url = getWebDAVURL()
    const info =
      `地址：${url}\n` +
      `用户名：${webdavUsername || '（未获取）'}\n` +
      `密码：${webdavPasswordPlain || '（未设置或已刷新）'}`
    try {
      await copyText(info)
      message.success('连接信息已复制')
    } catch {
      message.error('复制失败')
    }
  }

  const handleOpenConnInfo = async () => {
    setConnInfoOpen(true)
    // 如果本会话没有明文，从后端获取
    if (!webdavPasswordPlain && webdavHasPassword) {
      try {
        const res = await getWebDAVPassword()
        setWebdavPasswordPlain(res.data.password)
      } catch (err: any) {
        if (err.response?.status !== 404) {
          message.error('获取密码失败')
        }
      }
    }
  }

  const menuItems = [
    {
      key: 'register',
      icon: <UserOutlined />,
      label: '注册与登录',
    },
    {
      key: 'security',
      icon: <SafetyOutlined />,
      label: '安全设置',
    },
    {
      key: 'webdav',
      icon: <CloudOutlined />,
      label: 'WebDAV 服务',
    },
  ]

  const renderContent = () => {
    switch (activeKey) {
      case 'register':
        return (
          <Card title="注册与登录" loading={loading}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
              <div>
                <div style={{ fontWeight: 500, marginBottom: 4 }}>允许新用户注册</div>
                <Text type="secondary">关闭后，无法再通过前台注册新的用户。系统尚无用户时仍允许注册首个管理员。</Text>
              </div>
              <Switch
                checked={allowRegister}
                loading={registerSaving}
                onChange={handleAllowRegisterChange}
              />
            </div>
          </Card>
        )

      case 'security':
        return (
          <Card title="安全设置" loading={loading}>
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 16 }}
              message="JWT 主密钥用于签发登录令牌"
              description="首次启动时会自动生成并写入数据库。请勿泄露主密钥；轮转后所有用户需重新登录。"
            />
            <Paragraph type="secondary" style={{ marginBottom: 8 }}>
              当前 JWT 主密钥
            </Paragraph>
            <Space.Compact style={{ width: '100%', marginBottom: 16 }}>
              <Input.Password value={secret} readOnly visibilityToggle />
              <Button icon={<CopyOutlined />} onClick={handleCopy} disabled={!secret}>
                复制
              </Button>
            </Space.Compact>
            <Space>
              <Button
                type="primary"
                danger
                icon={<ReloadOutlined />}
                loading={rotating}
                onClick={handleRotate}
                disabled={!secret}
              >
                轮转主密钥
              </Button>
              <Text type="secondary">轮转后所有用户需重新登录</Text>
            </Space>
          </Card>
        )

      case 'webdav':
        return (
          <Card title="WebDAV 服务" loading={loading}>
            <Alert
              type="info"
              showIcon
              icon={<LinkOutlined />}
              style={{ marginBottom: 16 }}
              message="WebDAV 服务允许第三方客户端（如 Rclone、Windows 资源管理器、macOS Finder）挂载访问您的云盘文件"
            />

            <div style={{ marginBottom: 16 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 16 }}>
                <div>
                  <div style={{ fontWeight: 500, marginBottom: 4 }}>启用 WebDAV 服务</div>
                  <Text type="secondary">开启后，用户可通过 WebDAV 协议访问云盘文件</Text>
                </div>
                <Switch
                  checked={webdavEnabled}
                  loading={webdavSaving}
                  onChange={handleWebDAVChange}
                />
              </div>
            </div>

            <Divider />

            <div style={{ marginBottom: 16 }}>
              <Paragraph type="secondary" style={{ marginBottom: 8 }}>
                WebDAV 用户名
              </Paragraph>
              <Space.Compact style={{ width: '100%' }}>
                <Input value={webdavUsername} readOnly placeholder="加载中..." />
                <Button
                  icon={<CopyOutlined />}
                  onClick={async () => {
                    if (!webdavUsername) return
                    try {
                      await copyText(webdavUsername)
                      message.success('已复制')
                    } catch {
                      message.error('复制失败')
                    }
                  }}
                  disabled={!webdavUsername}
                >
                  复制
                </Button>
              </Space.Compact>
              <Text type="secondary" style={{ fontSize: 12, marginTop: 4, display: 'block' }}>
                WebDAV 用户名即您的登录用户名
              </Text>
            </div>

            <div style={{ marginBottom: 16 }}>
              <Paragraph type="secondary" style={{ marginBottom: 8 }}>
                WebDAV 密码
              </Paragraph>
              {webdavHasPassword ? (
                <Alert
                  type="success"
                  showIcon
                  message="已设置 WebDAV 密码"
                  description="使用上方用户名和此密码登录 WebDAV 客户端"
                  style={{ marginBottom: 12 }}
                />
              ) : (
                <Alert
                  type="warning"
                  showIcon
                  message="尚未设置 WebDAV 密码"
                  description="请先设置密码才能使用 WebDAV 服务"
                  style={{ marginBottom: 12 }}
                />
              )}
              <Space.Compact style={{ width: '100%' }}>
                <Input.Password
                  value={webdavPassword}
                  onChange={(e) => setWebdavPassword(e.target.value)}
                  placeholder="输入新密码（至少 6 位）"
                  visibilityToggle
                />
                <Button
                  type="primary"
                  loading={webdavPasswordSaving}
                  onClick={() => handleSetWebDAVPassword()}
                  disabled={webdavPassword.length < 6}
                >
                  设置密码
                </Button>
              </Space.Compact>
            </div>

            <div style={{ marginBottom: 16 }}>
              <Button
                type="primary"
                icon={<LinkOutlined />}
                onClick={handleOpenConnInfo}
                disabled={!webdavEnabled || !webdavHasPassword}
                style={{ width: '100%' }}
              >
                查看连接信息
              </Button>
              <Text type="secondary" style={{ fontSize: 12, marginTop: 4, display: 'block' }}>
                点击按钮查看 WebDAV 客户端所需的地址、用户名、密码
              </Text>
            </div>

            <Alert
              type="info"
              showIcon
              message="使用说明"
              description={
                <div>
                  <div>1. 启用 WebDAV 服务并设置密码</div>
                  <div>2. 点击「查看连接信息」获取地址、用户名、密码</div>
                  <div>3. 在 WebDAV 客户端中填入上述信息</div>
                  <div style={{ marginTop: 8, fontSize: 12, color: '#999' }}>
                    注意：WebDAV 密码与登录密码独立，互不影响
                  </div>
                </div>
              }
            />
          </Card>
        )

      default:
        return null
    }
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppHeader title="参数设置" />
      <Layout style={{ background: '#fff' }}>
        <Sider width={200} style={{ background: '#fff', borderRight: '1px solid #f0f0f0' }}>
          <Menu
            mode="inline"
            selectedKeys={[activeKey]}
            items={menuItems}
            onClick={({ key }) => setActiveKey(key)}
            style={{ height: '100%', borderRight: 0 }}
          />
        </Sider>
        <Content style={{ padding: 24, maxWidth: 900, margin: '0 auto', width: '100%' }}>
          {renderContent()}
        </Content>
      </Layout>

      {/* WebDAV 连接信息 Modal */}
      <Modal
        title="WebDAV 连接信息"
        open={connInfoOpen}
        onCancel={() => setConnInfoOpen(false)}
        footer={[
          <Button key="copy" type="primary" onClick={handleCopyConnInfo}>
            复制全部
          </Button>,
          <Button key="close" onClick={() => setConnInfoOpen(false)}>
            关闭
          </Button>,
        ]}
      >
        <div style={{ marginBottom: 16 }}>
          <Paragraph type="secondary" style={{ marginBottom: 8 }}>
            地址
          </Paragraph>
          <Space.Compact style={{ width: '100%' }}>
            <Input value={getWebDAVURL()} readOnly />
            <Button
              icon={<CopyOutlined />}
              onClick={async () => {
                try {
                  await copyText(getWebDAVURL())
                  message.success('已复制')
                } catch {
                  message.error('复制失败')
                }
              }}
            >
              复制
            </Button>
          </Space.Compact>
        </div>

        <div style={{ marginBottom: 16 }}>
          <Paragraph type="secondary" style={{ marginBottom: 8 }}>
            用户名
          </Paragraph>
          <Space.Compact style={{ width: '100%' }}>
            <Input value={webdavUsername} readOnly />
            <Button
              icon={<CopyOutlined />}
              onClick={async () => {
                if (!webdavUsername) return
                try {
                  await copyText(webdavUsername)
                  message.success('已复制')
                } catch {
                  message.error('复制失败')
                }
              }}
              disabled={!webdavUsername}
            >
              复制
            </Button>
          </Space.Compact>
        </div>

        <div style={{ marginBottom: 16 }}>
          <Paragraph type="secondary" style={{ marginBottom: 8 }}>
            密码
          </Paragraph>
          {webdavPasswordPlain ? (
            <Space.Compact style={{ width: '100%' }}>
              <Input.Password value={webdavPasswordPlain} readOnly visibilityToggle />
              <Button
                icon={<CopyOutlined />}
                onClick={async () => {
                  try {
                    await copyText(webdavPasswordPlain)
                    message.success('已复制')
                  } catch {
                    message.error('复制失败')
                  }
                }}
              >
                复制
              </Button>
            </Space.Compact>
          ) : (
            <Alert
              type="info"
              showIcon
              message="加载中..."
              description="正在获取密码"
            />
          )}
        </div>
      </Modal>
    </Layout>
  )
}
