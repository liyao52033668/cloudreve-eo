import { useCallback, useEffect, useRef, useState } from 'react'
import { Modal, Button, Input, Space, Typography, message, Alert } from 'antd'
import { getBaiduAuthURL, baiduAuthByCode } from '../api/policies'

const { Paragraph } = Typography

interface Props {
  policyId: number
  open: boolean
  onClose: () => void
  onAuthorized: () => void
}

/** 百度网盘 OAuth 授权弹窗。
 * - redirect 模式（策略配置了回调地址）：popup 打开授权页，授权后跳回本站回调路由
 *   自动换 token，落地页 postMessage 通知本弹窗，全程零手动复制。
 * - oob 模式（回调地址留空）：授权后百度页面直接展示授权码，手动粘贴提交。
 */
export default function BaiduAuth({ policyId, open, onClose, onAuthorized }: Props) {
  const [authUrl, setAuthUrl] = useState('')
  const [mode, setMode] = useState<'oob' | 'redirect'>('oob')
  const [code, setCode] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [loadingUrl, setLoadingUrl] = useState(false)
  const [autoPending, setAutoPending] = useState(false)
  const doneRef = useRef(false)

  const loadAuthUrl = useCallback(async () => {
    setLoadingUrl(true)
    try {
      const { data } = await getBaiduAuthURL(policyId)
      setAuthUrl(data.auth_url)
      setMode(data.mode || 'oob')
    } catch {
      setAuthUrl('')
    } finally {
      setLoadingUrl(false)
    }
  }, [policyId])

  useEffect(() => {
    if (!open) return
    doneRef.current = false
    setCode('')
    setAutoPending(false)
    loadAuthUrl()
  }, [open, policyId, loadAuthUrl])

  // 监听回调落地页的 postMessage（redirect 模式自动完成信号）
  useEffect(() => {
    if (!open) return
    const handler = (e: MessageEvent) => {
      try {
        const data = typeof e.data === 'string' ? JSON.parse(e.data) : e.data
        if (data?.event !== 'baiduOauthDone') return
        setAutoPending(false)
        if (doneRef.current) return
        doneRef.current = true
        if (data.ok) {
          message.success('百度网盘授权成功')
          onAuthorized()
        } else {
          message.error(decodeURIComponent(data.error || '') || '授权失败')
        }
      } catch {
        // 非 JSON 消息忽略
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [open, onAuthorized])

  const openAuthPage = () => {
    if (!authUrl) return
    // 不带窗口特性参数：按浏览器默认行为开新标签页（而非独立新窗口）；
    // 保留 window.opener，授权完成后回调落地页可 postMessage 自动通知本弹窗。
    window.open(authUrl, '_blank')
    if (mode === 'redirect') setAutoPending(true)
  }

  const submitCode = async (authCode: string) => {
    if (!authCode.trim()) {
      message.warning('请输入授权码')
      return
    }
    setSubmitting(true)
    try {
      await baiduAuthByCode(policyId, authCode.trim())
      doneRef.current = true
      message.success('百度网盘授权成功')
      onAuthorized()
    } catch (err: any) {
      message.error(err.response?.data?.error || '授权失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      title="百度网盘授权"
      open={open}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
      width={560}
      destroyOnClose
    >
      <Paragraph type="secondary">
        {mode === 'redirect'
          ? '点击下方按钮打开百度授权页，用百度账号登录或扫码完成授权。授权成功后将自动跳回本站并完成授权，无需手动操作；若自动跳转未生效，也可在下方手动粘贴授权码。'
          : '在新标签页打开百度授权页，使用百度账号登录或扫码完成授权；授权成功后页面会直接展示授权码（code），将其复制粘贴到下方提交。'}
      </Paragraph>
      <Paragraph type="secondary">
        当前为 {mode === 'redirect' ? '回调地址模式（自动）' : 'oob 模式（手动回填授权码）'}，由策略的「回调地址」是否填写决定。
      </Paragraph>

      {authUrl ? (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Button type="primary" onClick={openAuthPage}>
            新标签页打开授权页
          </Button>
          {autoPending && (
            <Alert type="info" showIcon message="等待授权完成…授权后会自动跳回本站，请勿关闭本弹窗" />
          )}
        </Space>
      ) : loadingUrl ? (
        <Alert type="info" showIcon message="获取授权地址中…" />
      ) : (
        <Alert type="warning" showIcon message="获取授权地址失败，请重试" />
      )}

      <div style={{ marginTop: 16 }}>
        <Space.Compact style={{ width: '100%' }}>
          <Input
            placeholder={mode === 'oob' ? '粘贴授权码（code）' : '自动授权未生效时，粘贴授权码（code）'}
            value={code}
            onChange={(e) => setCode(e.target.value)}
            onPressEnter={() => submitCode(code)}
          />
          <Button loading={submitting} onClick={() => submitCode(code)}>
            提交授权码
          </Button>
        </Space.Compact>
      </div>
    </Modal>
  )
}
