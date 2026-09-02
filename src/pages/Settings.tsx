import { useCallback, useEffect, useState } from 'react'
import { Layout, Card, Button, Typography, Space, message, Modal, Input, Alert, Switch, Divider } from 'antd'
import { ReloadOutlined, CopyOutlined, LinkOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  getSecuritySettings,
  rotateJWTSecret,
  updateAllowRegister,
} from '../api/settings'
import { getProfile } from '../api/user'
import { getWebDAVSettings, updateWebDAVEnabled } from '../api/webdav'
import { getWebDAVStatus, setWebDAVPassword } from '../api/user'
import AppHeader from '../components/AppHeader'
import { copyText } from '../utils/clipboard'

const { Content } = Layout
const { Text, Paragraph } = Typography

export default function Settings() {
  const navigate = useNavigate()
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

  const ensureAdmin = useCallback(async () => {
    try {
      const res = await getProfile()
      if (!res.data.user?.is_admin) {
        message.error('需要管理员权限')
        navigate('/')
        return false
      }
      return true
    } catch {
      navigate('/login')
      return false
    }
  }, [navigate])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const ok = await ensureAdmin()
      if (!ok) return
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

  const handleSetWebDAVPassword = async () => {
    if (webdavPassword.length < 6) {
      message.error('密码长度至少 6 位')
      return
    }
    setWebdavPasswordSaving(true)
    try {
      await setWebDAVPassword(webdavPassword)
      setWebdavHasPassword(true)
      setWebdavPassword('')
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

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppHeader title="参数设置" />
      <Content style={{ padding: 24, maxWidth: 800, margin: '0 auto', width: '100%' }}>
        <Space direction="vertical" size={16} style={{ width: '100%' }}>
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
                WebDAV 访问地址
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
                WebDAV 密码
              </Paragraph>
              {webdavHasPassword ? (
                <Alert
                  type="success"
                  showIcon
                  message="已设置 WebDAV 密码"
                  description="使用用户名和此密码登录 WebDAV 客户端"
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
                  onClick={handleSetWebDAVPassword}
                  disabled={webdavPassword.length < 6}
                >
                  设置密码
                </Button>
              </Space.Compact>
            </div>

            <Alert
              type="info"
              showIcon
              message="使用说明"
              description={
                <div>
                  <div>1. 启用 WebDAV 服务并设置密码</div>
                  <div>2. 在 WebDAV 客户端中填入上方地址</div>
                  <div>3. 使用您的用户名和 WebDAV 密码登录</div>
                  <div style={{ marginTop: 8, fontSize: 12, color: '#999' }}>
                    注意：WebDAV 密码与登录密码独立，互不影响
                  </div>
                </div>
              }
            />
          </Card>
        </Space>
      </Content>
    </Layout>
  )
}
