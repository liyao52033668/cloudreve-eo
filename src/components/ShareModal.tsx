import { useState } from 'react'
import { Modal, Input, DatePicker, Button, message, Space } from 'antd'
import { createShare } from '../api/shares'

interface Props {
  open: boolean
  fileId: number | null
  onClose: () => void
}

export default function ShareModal({ open, fileId, onClose }: Props) {
  const [password, setPassword] = useState('')
  const [expireAt, setExpireAt] = useState<string | undefined>()
  const [shareLink, setShareLink] = useState('')

  const handleCreate = async () => {
    if (!fileId) return
    try {
      const res = await createShare(fileId, password || undefined, expireAt)
      const code = res.data.share.code
      const link = `${window.location.origin}/share/${code}`
      setShareLink(link)
      message.success('分享链接已生成')
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建分享失败')
    }
  }

  const handleCopy = () => {
    navigator.clipboard.writeText(shareLink)
    message.success('已复制到剪贴板')
  }

  return (
    <Modal title="创建分享" open={open} onCancel={() => { onClose(); setShareLink(''); setPassword('') }} footer={null}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Input.Password placeholder="提取码（可选）" value={password} onChange={(e) => setPassword(e.target.value)} />
        <DatePicker showTime placeholder="过期时间（可选）" onChange={(_, dateStr) => setExpireAt(dateStr as string)} style={{ width: '100%' }} />
        <Button type="primary" onClick={handleCreate} block>生成链接</Button>
        {shareLink && (
          <Space.Compact style={{ width: '100%' }}>
            <Input value={shareLink} readOnly />
            <Button type="primary" onClick={handleCopy}>复制</Button>
          </Space.Compact>
        )}
      </Space>
    </Modal>
  )
}
