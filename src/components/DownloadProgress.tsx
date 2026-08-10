import { useEffect, useRef, useState, useSyncExternalStore, type CSSProperties } from 'react'
import { createPortal } from 'react-dom'
import { Button, Progress, Tooltip } from 'antd'
import { DownloadOutlined, MinusOutlined, CloseOutlined } from '@ant-design/icons'
import { getDownloadTasks, subscribeDownload, cancelDownload, type DownloadTask } from '../store/downloadManager'

const CARD_WIDTH = 320
const CARD_MAX_HEIGHT = 300
const POS_STORAGE_KEY = 'download-progress-pos'

/** 读取卡片拖动过的位置（localStorage），没有则返回 null（默认贴右下角）。 */
function loadPos(): { left: number; top: number } | null {
  try {
    const raw = localStorage.getItem(POS_STORAGE_KEY)
    return raw ? JSON.parse(raw) : null
  } catch {
    return null
  }
}

function savePos(p: { left: number; top: number }) {
  try {
    localStorage.setItem(POS_STORAGE_KEY, JSON.stringify(p))
  } catch {
    /* 忽略写入失败 */
  }
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

/** 单个下载任务的进度行。 */
function TaskRow({ task }: { task: DownloadTask }) {
  return (
    <div style={{ marginBottom: 6 }}>
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

/** 可最小化、可拖动的下载进度浮窗。Portal 挂到 body（避开 #root> * 全局 min-height），
 * 拖动标题栏可把它移到不遮挡页面的位置。固定高度，任务多时内部滚动。 */
export default function DownloadProgress() {
  const tasks = useSyncExternalStore(subscribeDownload, getDownloadTasks)
  const [minimized, setMinimized] = useState(false)
  // 拖动后的位置；null 表示默认贴右下角。初始从 localStorage 恢复，拖动结束保存。
  const [pos, setPos] = useState<{ left: number; top: number } | null>(loadPos)
  const posRef = useRef<{ left: number; top: number } | null>(pos)
  const dragRef = useRef<{ startX: number; startY: number; origLeft: number; origTop: number } | null>(null)

  // 任务数变化（新任务开始 / 全部结束）时重置为展开态；下载进行中（进度变化）保持当前态
  useEffect(() => {
    setMinimized(false)
  }, [tasks.length])

  if (tasks.length === 0) return null

  const dragHandlers = {
    onPointerDown: (e: React.PointerEvent<HTMLDivElement>) => {
      // 点击按钮不触发拖动（避免与最小化/取消冲突）
      if ((e.target as HTMLElement).closest('button')) return
      e.preventDefault()
      // 捕获指针：即使鼠标移出标题栏（向上拖时很快会移出）也能持续收到 move/up，
      // 否则向上拖会因事件丢失而卡住
      e.currentTarget.setPointerCapture(e.pointerId)
      const rect = e.currentTarget.getBoundingClientRect()
      dragRef.current = {
        startX: e.clientX,
        startY: e.clientY,
        origLeft: rect.left,
        origTop: rect.top,
      }
    },
    onPointerMove: (e: React.PointerEvent<HTMLDivElement>) => {
      const d = dragRef.current
      if (!d) return
      // 边界 clamp：卡片不能拖出屏幕（顶部/左侧 ≥0，底部/右侧留一点可见）
      const next = {
        left: Math.max(0, Math.min(d.origLeft + (e.clientX - d.startX), window.innerWidth - CARD_WIDTH)),
        top: Math.max(0, Math.min(d.origTop + (e.clientY - d.startY), window.innerHeight - 40)),
      }
      posRef.current = next
      setPos(next)
    },
    onPointerUp: () => {
      if (posRef.current) savePos(posRef.current)
      dragRef.current = null
    },
    onPointerCancel: () => {
      dragRef.current = null
    },
  }

  const baseStyle: CSSProperties = {
    position: 'fixed',
    zIndex: 1000,
    ...(pos ? { left: pos.left, top: pos.top } : { right: 24, bottom: 24 }),
  }

  const el = minimized ? (
    <Tooltip title="点击展开下载列表">
      <Button
        type="primary"
        shape="round"
        icon={<DownloadOutlined />}
        style={{ ...baseStyle, minHeight: 0 }}
        onClick={() => setMinimized(false)}
      >
        下载 {tasks.length} 项
      </Button>
    </Tooltip>
  ) : (
    <div
      style={{
        ...baseStyle,
        width: CARD_WIDTH,
        minHeight: 0,
        maxHeight: CARD_MAX_HEIGHT,
        overflowY: 'auto',
        background: '#fff',
        borderRadius: 8,
        padding: '10px 12px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
        border: '1px solid rgba(0,0,0,0.06)',
      }}
    >
      <div
        {...dragHandlers}
        style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6, cursor: 'move', userSelect: 'none' }}
      >
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

  return createPortal(el, document.body)
}
