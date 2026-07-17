import { useState } from 'react'
import { Form, Input, Button, Card, message } from 'antd'
import { UserOutlined, LockOutlined } from '@ant-design/icons'
import { useNavigate, Link } from 'react-router-dom'
import { register } from '../api/auth'

export default function Register() {
  const [loading, setLoading] = useState(false)
  const navigate = useNavigate()

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const res = await register(values)
      localStorage.setItem('token', res.data.token)
      message.success('注册成功')
      navigate('/')
    } catch (err: any) {
      message.error(err.response?.data?.error || '注册失败')
    } finally {
      setLoading(false)
    }
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
