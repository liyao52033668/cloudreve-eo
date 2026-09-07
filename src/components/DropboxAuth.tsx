import { useCallback, useEffect, useRef, useState } from 'react'
import { Modal, Button, Input, Space, Typography, message, Alert } from 'antd'
import { LinkOutlined } from '@ant-design/icons'
import { getDropboxAuthURL, dropboxAuthByCode, getDropboxAuthStatus } from '../api/policies'

const { Paragraph } = Typography

interface Props {
  policyId: number
  /** 策略的 App Key，用于生成 App 控制台直达链接 */
  appKey?: string
  open: boolean
  onClose: () => void
  onAuthorized: () => void
}

/** Dropbox OAuth 授权弹窗：网页授权（新窗口 + 轮询状态 + 手动兜底）。 */
export default function DropboxAuth({ policyId, appKey, open, onClose, onAuthorized }: Props) {
  const [authUrl, setAuthUrl] = useState('')
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const done = useRef(false)
  const pollTimer = useRef<ReturnType<typeof setInterval> | null>(null)

  const handleAuthorized = useCallback(() => {
    if (done.current) return
    done.current = true
    stopPolling()
    message.success('Dropbox 授权成功')
    onAuthorized()
  }, [onAuthorized])

  const stopPolling = useCallback(() => {
    if (pollTimer.current) {
      clearInterval(pollTimer.current)
      pollTimer.current = null
    }
  }, [])

  const startPolling = useCallback(() => {
    stopPolling()
    const deadline = Date.now() + 5 * 60 * 1000 // 5 分钟超时
    pollTimer.current = setInterval(async () => {
      if (Date.now() > deadline) {
        stopPolling()
        message.warning('授权等待超时，请手动提交授权码')
        return
      }
      try {
        const res = await getDropboxAuthStatus(policyId)
        if (res.data.status === 'authorized') {
          handleAuthorized()
        }
      } catch {
        // 网络抖动忽略，继续轮询
      }
    }, 2000)
  }, [policyId, handleAuthorized, stopPolling])

  // 获取网页授权地址（带 origin 让后端拼出浏览器真实域名的回调地址）
  const loadAuthUrl = useCallback(async () => {
    try {
      const { data } = await getDropboxAuthURL(policyId, window.location.origin)
      setAuthUrl(data.auth_url)
    } catch {
      setAuthUrl('')
    }
  }, [policyId])

  useEffect(() => {
    if (!open) return
    done.current = false
    setCode('')
    loadAuthUrl()
    return stopPolling
  }, [open, policyId, loadAuthUrl, stopPolling])

  const openAuthPage = () => {
    if (!authUrl) return
    window.open(authUrl, '_blank')
    startPolling()
  }

  const submitCode = async (authCode: string) => {
    if (!authCode.trim()) {
      message.warning('请输入授权码')
      return
    }
    setSubmitting(true)
    try {
      await dropboxAuthByCode(policyId, authCode.trim(), window.location.origin)
      handleAuthorized()
    } catch (err: any) {
      message.error(err.response?.data?.error || '授权失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title="Dropbox 授权"
      open={open}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
      width={520}
      destroyOnHidden
    >
      <div>
        <Alert
          type="info"
          showIcon
          icon={<LinkOutlined />}
          message="Dropbox OAuth 授权流程"
          description={
            <div>
              <div>
                1. 在 Dropbox App Console 的 Redirect URIs 中添加本站回调地址
                {appKey && (
                  <a
                    href={`https://www.dropbox.com/developers/apps/info?app_key=${appKey}`}
                    target="_blank"
                    rel="noreferrer"
                    style={{ marginLeft: 8 }}
                  >
                    打开 App 控制台
                  </a>
                )}
              </div>
              <div style={{ margin: '4px 0 8px' }}>
                <code
                  onClick={() => {
                    const url = `${window.location.origin}/api/oauth/dropbox/callback`
                    navigator.clipboard.writeText(url)
                    message.success('回调地址已复制')
                  }}
                  style={{
                    fontSize: 12,
                    color: '#1677ff',
                    cursor: 'pointer',
                    padding: '4px 8px',
                    background: '#f5f5f5',
                    borderRadius: 4,
                    userSelect: 'text',
                  }}
                  title="点击复制"
                >
                  {window.location.origin}/api/oauth/dropbox/callback
                </code>
              </div>
              <div>2. 点击下方按钮打开 Dropbox 授权页面</div>
              <div>3. 登录并授权后，系统将自动检测授权状态</div>
              <div>4. 若自动检测未生效，可从回调地址 URL 中复制 code 手动提交</div>
            </div>
          }
          style={{ marginBottom: 16 }}
        />

        {authUrl ? (
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Button type="primary" onClick={openAuthPage} block>
              打开 Dropbox 授权页面
            </Button>
            <Paragraph type="secondary" style={{ fontSize: 12, margin: 0 }}>
              授权后将自动检测完成；若自动流程未生效，请从回调地址 URL 中复制 code 手动提交
            </Paragraph>
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
    </Modal>
  )
}
