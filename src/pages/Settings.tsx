import { useCallback, useEffect, useState } from 'react'
import { Layout, Card, Button, Typography, Space, message, Modal, Input, Alert, Switch } from 'antd'
import { ArrowLeftOutlined, ReloadOutlined, CopyOutlined, LogoutOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import {
  getSecuritySettings,
  rotateJWTSecret,
  updateAllowRegister,
} from '../api/settings'
import { getProfile } from '../api/user'

const { Header, Content } = Layout
const { Text, Paragraph } = Typography

export default function Settings() {
  const navigate = useNavigate()
  const [secret, setSecret] = useState('')
  const [allowRegister, setAllowRegister] = useState(true)
  const [loading, setLoading] = useState(false)
  const [rotating, setRotating] = useState(false)
  const [registerSaving, setRegisterSaving] = useState(false)

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
      await navigator.clipboard.writeText(secret)
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

  const handleLogout = () => {
    localStorage.removeItem('token')
    localStorage.removeItem('user')
    navigate('/login')
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: '#001529' }}>
        <Space>
          <Button type="text" icon={<ArrowLeftOutlined />} style={{ color: '#fff' }} onClick={() => navigate('/')}>
            返回
          </Button>
          <span style={{ color: '#fff', fontSize: 18 }}>参数设置</span>
        </Space>
        <Button icon={<LogoutOutlined />} type="text" style={{ color: '#fff' }} onClick={handleLogout}>
          退出
        </Button>
      </Header>
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
        </Space>
      </Content>
    </Layout>
  )
}
