/** 全局下载任务管理（支持并发多个任务）。
 *
 * 下载进度提升为全局状态，进度卡片挂在 App 根（跨路由常驻）：
 * 切页/组件卸载不丢失进度，下载本身也不受页面生命周期影响。
 * 每次 runDownload 启动一个独立任务，多个任务可同时进行。
 */

export interface DownloadTask {
  id: number
  name: string
  percent: number
  received: number
  total: number
}

export interface DownloadCtx {
  /** 更新已下载字节/总字节（0 表示未知），内部会换算百分比并通知订阅者 */
  onProgress: (received: number, total: number) => void
  signal: AbortSignal
}

export type DownloadExecutor = (ctx: DownloadCtx) => Promise<void>

let tasks: DownloadTask[] = []
const aborts = new Map<number, AbortController>()
let nextId = 1
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

export function getDownloadTasks(): DownloadTask[] {
  return tasks
}

/** 订阅下载状态变化（返回退订函数）。 */
export function subscribeDownload(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

/** 取消指定任务（传入任务 id）。 */
export function cancelDownload(id: number) {
  aborts.get(id)?.abort()
}

/** 启动一个下载任务（并发：不会取消其他进行中的任务），返回任务 id。 */
export async function runDownload(name: string, executor: DownloadExecutor): Promise<void> {
  const controller = new AbortController()
  const id = nextId++
  aborts.set(id, controller)
  tasks = [...tasks, { id, name, percent: 0, received: 0, total: 0 }]
  emit()
  try {
    await executor({
      onProgress: (received, total) => {
        tasks = tasks.map(t =>
          t.id === id
            ? {
                id,
                name,
                percent: total > 0 ? Math.round((received / total) * 100) : 0,
                received,
                total,
              }
            : t,
        )
        emit()
      },
      signal: controller.signal,
    })
  } finally {
    tasks = tasks.filter(t => t.id !== id)
    aborts.delete(id)
    emit()
  }
}
