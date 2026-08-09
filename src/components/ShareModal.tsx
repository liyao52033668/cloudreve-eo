import { useState } from 'react'
import { Modal, Input, DatePicker, Button, message, Space } from 'antd'
import { ThunderboltOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { createShare } from '../api/shares'
import { copyText } from '../utils/clipboard'

interface Props {
  open: boolean
  /** 要分享的文件 ID 列表（≥1，多个时访问者看到文件列表） */
  fileIds: number[]
  onClose: () => void
}

function generateRandomPassword(length = 8): string {
  const chars = 'ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789'
  let result = ''
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length))
  }
  return result
}

export default function ShareModal({ open, fileIds, onClose }: Props) {
  const [password, setPassword] = useState('')
  const [expireAt, setExpireAt] = useState<string | undefined>()
  const [shareLink, setShareLink] = useState('')

  // 常用过期时长快捷项（后端要求 RFC3339，统一用 ISO 8601；
  // 用 presets 而非自定义按钮，rc-picker 内部接管点击，弹层内自定义按钮事件不可靠）
  const expirePresets = [
    { label: '1 天', value: () => dayjs().add(1, 'day') },
    { label: '7 天', value: () => dayjs().add(7, 'day') },
    { label: '30 天', value: () => dayjs().add(30, 'day') },
  ]

  const handleCreate = async () => {
    if (fileIds.length === 0) return
    try {
      const res = await createShare(fileIds, password || undefined, expireAt)
      const code = res.data.share.code
      const link = `${window.location.origin}/share/${code}`
      setShareLink(link)
      message.success('分享链接已生成')
    } catch (err: any) {
      message.error(err.response?.data?.error || '创建分享失败')
    }
  }

  const handleCopy = async () => {
    try {
      await copyText(shareLink)
      message.success('已复制到剪贴板')
    } catch {
      message.error('复制失败，请手动复制')
    }
  }

  const title = fileIds.length > 1 ? `分享 ${fileIds.length} 个文件` : '创建分享'

  return (
    <Modal title={title} open={open} onCancel={() => { onClose(); setShareLink(''); setPassword(''); setExpireAt(undefined) }} footer={null}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Space.Compact style={{ width: '100%' }}>
          <Input placeholder="提取码（可选）" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" style={{ flex: 1 }} />
          <Button icon={<ThunderboltOutlined />} onClick={() => setPassword(generateRandomPassword())}>
            随机
          </Button>
        </Space.Compact>
        <DatePicker
          showTime
          showNow={false}
          presets={expirePresets}
          popupClassName="share-expire-picker"
          placeholder="过期时间（可选）"
          value={expireAt ? dayjs(expireAt) : null}
          onChange={(date) => setExpireAt(date ? date.toISOString() : undefined)}
          style={{ width: '100%' }}
        />
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
