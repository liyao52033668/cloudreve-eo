import { useEffect, useState, useSyncExternalStore } from 'react'
import { Button, Progress, Tooltip } from 'antd'
import { DownloadOutlined, MinusOutlined, CloseOutlined } from '@ant-design/icons'
import { getDownloadTasks, subscribeDownload, cancelDownload, type DownloadTask } from '../store/downloadManager'

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

/** 单个下载任务的进度行。 */
function TaskRow({ task }: { task: DownloadTask }) {
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <span style={{ fontWeight: 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 13, flex: 1 }}>
          {task.name}
        </span>
        <span style={{ color: 'rgba(0,0,0,0.45)', fontSize: 12, whiteSpace: 'nowrap' }}>
          {formatSize(task.received)} / {formatSize(task.total)}
        </span>
        <Tooltip title="取消该下载">
          <Button size="small" type="text" icon={<CloseOutlined />} onClick={() => cancelDownload(task.id)} />
        </Tooltip>
      </div>
      <Progress percent={task.percent} status="active" size="small" />
    </div>
  )
}

/** 可最小化的下载进度浮窗：挂在 App 根，跨路由常驻，固定右下角、不遮罩页面，支持多任务并发。 */
export default function DownloadProgress() {
  const tasks = useSyncExternalStore(subscribeDownload, getDownloadTasks)
  const [minimized, setMinimized] = useState(false)
  // 任务数变化（新任务开始 / 全部结束）时重置为展开态；下载进行中（进度变化）保持当前态
  useEffect(() => {
    setMinimized(false)
  }, [tasks.length])

  if (tasks.length === 0) return null

  if (minimized) {
    return (
      <Tooltip title="点击展开下载列表">
        <Button
          type="primary"
          shape="round"
          icon={<DownloadOutlined />}
          style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1000 }}
          onClick={() => setMinimized(false)}
        >
          下载 {tasks.length} 项
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
        maxHeight: 420,
        overflowY: 'auto',
        background: '#fff',
        borderRadius: 8,
        padding: '10px 12px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
        border: '1px solid rgba(0,0,0,0.06)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
        <span style={{ fontWeight: 600, fontSize: 13 }}>
          {tasks.length > 1 ? `正在下载 ${tasks.length} 项` : '正在下载'}
        </span>
        <Tooltip title="最小化">
          <Button size="small" type="text" icon={<MinusOutlined />} onClick={() => setMinimized(true)} />
        </Tooltip>
      </div>
      {tasks.map(t => <TaskRow key={t.id} task={t} />)}
    </div>
  )
}
