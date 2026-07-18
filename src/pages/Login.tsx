import { useEffect, useState } from 'react'
import { Form, Input, Button, Card, message } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { useNavigate, Link } from 'react-router-dom'
import { login } from '../api/auth'
import { getSiteInfo } from '../api/settings'

export default function Login() {
  const [loading, setLoading] = useState(false)
  // null = 加载中；true = 显示注册入口；false = 隐藏
  const [allowRegister, setAllowRegister] = useState<boolean | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    // 已登录则直接进首页
    if (localStorage.getItem('token')) {
      navigate('/', { replace: true })
      return
    }
    let cancelled = false
    getSiteInfo().then((info) => {
      if (!cancelled) setAllowRegister(info.allow_register)
    })
    return () => {
      cancelled = true
    }
  }, [navigate])

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const res = await login(values)
      localStorage.setItem('token', res.data.token)
      localStorage.setItem('user', JSON.stringify(res.data.user))
      message.success('登录成功')
      navigate('/')
    } catch (err: any) {
      message.error(err.response?.data?.error || '登录失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="Cloudreve-EO 登录" style={{ width: 400 }}>
        <Form onFinish={onFinish}>
          <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input prefix={<UserOutlined />} placeholder="用户名" />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading} block>
              登录
            </Button>
          </Form.Item>
          {/* 仅当明确允许注册时显示入口；关闭注册时直接隐藏 */}
          {allowRegister === true && (
            <div style={{ textAlign: 'center' }}>
              没有账号？<Link to="/register">去注册</Link>
            </div>
          )}
        </Form>
      </Card>
    </div>
  )
}
