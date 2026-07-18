import { useEffect, useState } from 'react'
import { Form, Input, Button, Card, message, Result } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { useNavigate, Link } from 'react-router-dom'
import { register } from '../api/auth'
import { getSiteInfo } from '../api/settings'

export default function Register() {
  const [loading, setLoading] = useState(false)
  const [checking, setChecking] = useState(true)
  const [allowRegister, setAllowRegister] = useState(true)
  const navigate = useNavigate()

  useEffect(() => {
    let cancelled = false
    getSiteInfo()
      .then((info) => {
        if (!cancelled) setAllowRegister(info.allow_register)
      })
      .finally(() => {
        if (!cancelled) setChecking(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const res = await register(values)
      localStorage.setItem('token', res.data.token)
      localStorage.setItem('user', JSON.stringify(res.data.user))
      message.success('注册成功')
      navigate('/')
    } catch (err: any) {
      message.error(err.response?.data?.error || '注册失败')
    } finally {
      setLoading(false)
    }
  }

  if (checking) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
        <Card style={{ width: 400 }} loading />
      </div>
    )
  }

  // 关闭注册：直接不展示注册表单
  if (!allowRegister) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
        <Card style={{ width: 400 }}>
          <Result
            status="403"
            title="暂未开放注册"
            subTitle="当前站点已关闭新用户注册，请联系管理员。"
            extra={
              <Button type="primary" onClick={() => navigate('/login')}>
                去登录
              </Button>
            }
          />
        </Card>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="Cloudreve-EO 注册" style={{ width: 400 }}>
        <Form onFinish={onFinish}>
          <Form.Item name="username" rules={[{ required: true, min: 3, message: '用户名至少3个字符' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, min: 6, message: '密码至少6个字符' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              注册
            </Button>
          </Form.Item>
          <div style={{ textAlign: 'center' }}>
            已有账号？<Link to="/login">去登录</Link>
          </div>
        </Form>
      </Card>
    </div>
  )
}
