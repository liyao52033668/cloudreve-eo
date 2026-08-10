import { useEffect, useState } from 'react'
import { Button, Progress, Space, Tooltip } from 'antd'
import { DownloadOutlined, MinusOutlined, CloseOutlined } from '@ant-design/icons'

/** 下载进度任务状态。 */
export interface DownloadTask {
  name: string
  percent: number
  received: number
  total: number
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '-'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) {
    size /= 1024
    i++
  }
  return `${size.toFixed(1)} ${units[i]}`
}

/** 可最小化的下载进度浮窗：固定右下角、不遮罩页面，可收起为小按钮，可取消。 */
export default function DownloadProgress({
  task,
  onCancel,
}: {
  task: DownloadTask | null
  onCancel: () => void
}) {
  const [minimized, setMinimized] = useState(false)
  // 新任务开始（task 从 null → 有值）时重置为展开态；下载进行中（percent 变化）保持当前态
  useEffect(() => {
    if (task) setMinimized(false)
  }, [!!task])

  if (!task) return null

  if (minimized) {
    return (
      <Tooltip title={`${task.name}（${task.percent}%）`}>
        <Button
          type="primary"
          shape="round"
          icon={<DownloadOutlined />}
          style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1000 }}
          onClick={() => setMinimized(false)}
        >
          {task.percent}%
        </Button>
      </Tooltip>
    )
  }

  return (
    <div
      style={{
        position: 'fixed',
        right: 24,
        bottom: 24,
        zIndex: 1000,
        width: 320,
        background: '#fff',
        borderRadius: 8,
        padding: '10px 12px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
        border: '1px solid rgba(0,0,0,0.06)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
        <span style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 13 }}>
          {task.name}
        </span>
        <Space size={2}>
          <Tooltip title="最小化">
            <Button size="small" type="text" icon={<MinusOutlined />} onClick={() => setMinimized(true)} />
          </Tooltip>
          <Tooltip title="取消下载">
            <Button size="small" type="text" icon={<CloseOutlined />} onClick={onCancel} />
          </Tooltip>
        </Space>
      </div>
      <Progress percent={task.percent} status="active" size="small" />
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 4, color: 'rgba(0,0,0,0.45)', fontSize: 12 }}>
        <span>{formatSize(task.received)} / {formatSize(task.total)}</span>
        <span>{task.percent}%</span>
      </div>
    </div>
  )
}
