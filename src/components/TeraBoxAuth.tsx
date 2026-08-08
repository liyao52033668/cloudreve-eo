import { useCallback, useEffect, useRef, useState } from 'react'
import { Modal, Tabs, Button, Input, Space, Typography, message, Spin, Alert } from 'antd'
import { QrcodeOutlined, LinkOutlined, ReloadOutlined } from '@ant-design/icons'
import {
  getTeraBoxDeviceCode,
  getTeraBoxAuthURL,
  getTeraBoxAuthStatus,
  teraboxAuthByCode,
} from '../api/policies'

const { Text, Paragraph } = Typography

interface Props {
  policyId: number
  open: boolean
  onClose: () => void
  onAuthorized: () => void
}

/** TeraBox OAuth 授权弹窗：扫码授权 / 网页授权（iframe + 手动粘贴 code）。 */
export default function TeraBoxAuth({ policyId, open, onClose, onAuthorized }: Props) {
  const [qrData, setQrData] = useState('')
  const [loading, setLoading] = useState(false)
  const [expiresLeft, setExpiresLeft] = useState(0)
  const [statusText, setStatusText] = useState('')
  const [authUrl, setAuthUrl] = useState('')
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  const done = useRef(false)

  const stopPolling = useCallback(() => {
    if (pollTimer.current) {
      clearInterval(pollTimer.current)
      pollTimer.current = null
    }
  }, [])

  const handleAuthorized = useCallback(() => {
    if (done.current) return
    done.current = true
    stopPolling()
    message.success('TeraBox 授权成功')
    onAuthorized()
  }, [onAuthorized, stopPolling])

  // 获取二维码并开始轮询
  const startQrFlow = useCallback(async () => {
    setLoading(true)
    setStatusText('')
    stopPolling()
    try {
      const { data } = await getTeraBoxDeviceCode(policyId)
      setQrData(data.qrcode || '')
      setExpiresLeft(data.expires_in || 0)
      if (!data.qrcode) {
        setStatusText('未获取到二维码，请改用网页授权')
        return
      }
      done.current = false
      const interval = Math.max(2, data.interval || 2)
      pollTimer.current = setInterval(async () => {
        setExpiresLeft((s) => Math.max(0, s - interval))
        try {
          const res = await getTeraBoxAuthStatus(policyId)
          if (res.data.status === 'authorized') {
            handleAuthorized()
          } else if (res.data.status === 'expired') {
            stopPolling()
            setStatusText('二维码已过期，请点击刷新')
          }
        } catch {
          // 网络抖动忽略，继续轮询
        }
      }, interval * 1000)
    } catch (err: any) {
      setStatusText(err.response?.data?.error || '获取二维码失败')
    } finally {
      setLoading(false)
    }
  }, [policyId, handleAuthorized, stopPolling])

  // 获取网页授权地址
  const loadAuthUrl = useCallback(async () => {
    try {
      const { data } = await getTeraBoxAuthURL(policyId)
      setAuthUrl(data.auth_url)
    } catch {
      setAuthUrl('')
    }
  }, [policyId])

  useEffect(() => {
    if (!open) return
    done.current = false
    setCode('')
    startQrFlow()
    loadAuthUrl()
    return stopPolling
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, policyId])

  const submitCode = async (authCode: string) => {
    if (!authCode.trim()) {
      message.warning('请输入授权码')
      return
    }
    setSubmitting(true)
    try {
      await teraboxAuthByCode(policyId, authCode.trim())
      handleAuthorized()
    } catch (err: any) {
      message.error(err.response?.data?.error || '授权失败')
    } finally {
      setSubmitting(false)
    }
  }

  // 监听 iframe postMessage（网页授权码模式）
  useEffect(() => {
    if (!open) return
    const handler = (e: MessageEvent) => {
      try {
        const data = typeof e.data === 'string' ? JSON.parse(e.data) : e.data
        if (data?.event !== 'teraboxOauth') return
        const authCode = data.code || data.data?.code || data.result?.code
        if (authCode) {
          void submitCode(String(authCode))
        } else {
          message.info('已收到授权事件，请在下方粘贴授权码完成授权')
        }
      } catch {
        // 非 JSON 消息忽略
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, policyId])

  return (
    <Modal
      title="TeraBox 授权"
      open={open}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
      width={520}
      destroyOnClose
    >
      <Tabs
        items={[
          {
            key: 'qr',
            label: (
              <span>
                <QrcodeOutlined /> 扫码授权
              </span>
            ),
            children: (
              <div style={{ textAlign: 'center' }}>
                <Paragraph type="secondary">
                  使用 TeraBox App 登录账号后扫描下方二维码完成授权（约 5 分钟内有效）。
                </Paragraph>
                {loading ? (
                  <Spin tip="获取二维码中…" style={{ margin: '32px 0' }} />
                ) : qrData ? (
                  <>
                    <img
                      src={qrData}
                      alt="TeraBox 授权二维码"
                      style={{ width: 240, maxWidth: '100%', border: '1px solid #f0f0f0', borderRadius: 8 }}
                    />
                    <div style={{ marginTop: 8 }}>
                      {expiresLeft > 0 ? (
                        <Text type="secondary">剩余 {Math.floor(expiresLeft / 60)}:{String(expiresLeft % 60).padStart(2, '0')}，等待扫码…</Text>
                      ) : null}
                    </div>
                  </>
                ) : (
                  <Alert type="warning" showIcon message={statusText || '未获取到二维码'} style={{ margin: '16px 0' }} />
                )}
                {statusText && <Alert type="info" showIcon message={statusText} style={{ marginTop: 12 }} />}
                <div style={{ marginTop: 16 }}>
                  <Button icon={<ReloadOutlined />} onClick={startQrFlow} loading={loading}>
                    刷新二维码
                  </Button>
                </div>
              </div>
            ),
          },
          {
            key: 'code',
            label: (
              <span>
                <LinkOutlined /> 网页授权
              </span>
            ),
            children: (
              <div>
                <Paragraph type="secondary">
                  方式一：下方内嵌授权页（需在 TeraBox 开放平台为本站点域名配置回调）。
                  方式二：新窗口打开授权页，授权成功后将获得的授权码（code）粘贴到下方提交。
                </Paragraph>
                {authUrl ? (
                  <Space direction="vertical" style={{ width: '100%' }}>
                    <iframe
                      src={authUrl}
                      title="TeraBox 授权页"
                      style={{ width: '100%', height: 320, border: '1px solid #f0f0f0', borderRadius: 8 }}
                    />
                    <Button type="link" href={authUrl} target="_blank" rel="noreferrer">
                      新窗口打开授权页
                    </Button>
                  </Space>
                ) : (
                  <Alert type="warning" showIcon message="获取授权地址失败，请重试" />
                )}
                <div style={{ marginTop: 16 }}>
                  <Space.Compact style={{ width: '100%' }}>
                    <Input
                      placeholder="粘贴授权码（code）"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      onPressEnter={() => submitCode(code)}
                    />
                    <Button type="primary" loading={submitting} onClick={() => submitCode(code)}>
                      提交授权码
                    </Button>
                  </Space.Compact>
                </div>
              </div>
            ),
          },
        ]}
      />
    </Modal>
  )
}
