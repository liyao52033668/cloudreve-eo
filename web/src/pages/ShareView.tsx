import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import { Card, Input, Button, message, Space, Typography } from 'antd'
import { DownloadOutlined } from '@ant-design/icons'
import { getShare, getShareDownload } from '../api/shares'

const { Title, Text } = Typography

export default function ShareView() {
  const { code } = useParams<{ code: string }>()
  const [password, setPassword] = useState('')
  const [file, setFile] = useState<any>(null)
  const [error, setError] = useState('')
  const [needPassword, setNeedPassword] = useState(false)

  useEffect(() => {
    if (code) loadShare('')
  }, [code])

  const loadShare = async (pwd: string) => {
    if (!code) return
    try {
      const res = await getShare(code, pwd)
      setFile(res.data.file)
      setError('')
    } catch (err: any) {
      const msg = err.response?.data?.error || '加载失败'
      if (msg.includes('提取码')) {
        setNeedPassword(true)
        setError('')
      } else {
        setError(msg)
      }
    }
  }

  const handleDownload = async () => {
    if (!code) return
    try {
      const res = await getShareDownload(code, password || undefined)
      window.open(res.data.download_url, '_blank')
    } catch {
      message.error('获取下载链接失败')
    }
  }

  if (error) return <div style={{ textAlign: 'center', marginTop: 100 }}><Text type="danger">{error}</Text></div>

  if (needPassword && !file) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh' }}>
        <Card title="输入提取码" style={{ width: 360 }}>
          <Space direction="vertical" style={{ width: '100%' }}>
            <Input.Password value={password} onChange={(e) => setPassword(e.target.value)} placeholder="提取码" />
            <Button type="primary" block onClick={() => loadShare(password)}>确认</Button>
          </Space>
        </Card>
      </div>
    )
  }

  if (!file) return <div style={{ textAlign: 'center', marginTop: 100 }}>加载中...</div>

  return (
    <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <Card title="分享文件" style={{ width: 400 }}>
        <Title level={4}>{file.name}</Title>
        <Text type="secondary">大小: {(file.size / 1024 / 1024).toFixed(2)} MB</Text>
        <div style={{ marginTop: 24 }}>
          <Button type="primary" icon={<DownloadOutlined />} block onClick={handleDownload}>下载文件</Button>
        </div>
      </Card>
    </div>
  )
}
