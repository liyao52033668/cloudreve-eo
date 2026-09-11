/** 单文件下载统一入口（文件列表与分享页共用，保证两处行为一致）。
 *
 * 按 URL 形态选择下载方式：
 * - /api/files/stream|proxy：代理存储（Filen/百度等无外链直链）。云函数响应有约 6MB 缓冲上限、
 *   边缘函数流式拼流又有执行时长限制，改由前端 JS 按 Range 分段拉取并拼接 Blob，避开平台限制。
 * - 跨域 URL 且没有 response-content-disposition 参数：<a download> 对跨域无效，
 *   浏览器会用 URL 路径作为文件名（随机无后缀），改用 fetch → blob 下载保证文件名正确。
 *   若 URL 包含 response-content-disposition 参数（如 Cloudreve/S3），
 *   浏览器会使用响应头里的文件名，可以直接下载。
 * - 其余（同源外链或带 Content-Disposition 的跨域直链）：交给浏览器下载管理器。
 */
import { message } from 'antd'
import { runDownload } from '../store/downloadManager'
import { proxySegmentDownload, saveBlob } from './proxyDownload'

/** 判断 URL 是否跨域 */
const isCrossOrigin = (url: string): boolean => {
  try {
    return new URL(url).origin !== window.location.origin
  } catch {
    return false
  }
}

/** 按下载 URL 形态选择下载方式并给出结果提示；下载过程的错误在此统一提示，不再向上抛出。 */
export async function downloadFileByURL(fileName: string, url: string): Promise<void> {
  try {
    if (url.startsWith('/api/files/stream') || url.startsWith('/api/files/proxy')) {
      // 代理存储分段下载：走全局下载管理器，切页不丢进度
      await runDownload(fileName, async ({ onProgress, signal }) => {
        const blob = await proxySegmentDownload(url, onProgress, signal)
        saveBlob(blob, fileName)
      })
      message.success({ content: `${fileName} 下载完成`, key: 'download' })
      return
    }

    if (isCrossOrigin(url) && !url.includes('response-content-disposition')) {
      // 跨域下载：fetch → blob，保证文件名正确并展示进度
      await runDownload(fileName, async ({ onProgress, signal }) => {
        const response = await fetch(url, { signal })
        if (!response.ok) throw new Error(`下载失败: HTTP ${response.status}`)

        const contentLength = response.headers.get('content-length')
        const total = contentLength ? parseInt(contentLength, 10) : 0
        const reader = response.body?.getReader()
        if (!reader) throw new Error('无法读取响应流')

        const chunks: BlobPart[] = []
        let loaded = 0
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          chunks.push(value)
          loaded += value.length
          if (total > 0) {
            onProgress(loaded, total)
          }
        }

        const blob = new Blob(chunks, { type: response.headers.get('content-type') || 'application/octet-stream' })
        saveBlob(blob, fileName)
      })
      message.success({ content: `${fileName} 下载完成`, key: 'download' })
      return
    }

    // 同源外链或带 Content-Disposition 的跨域 URL：直接交给浏览器下载管理器
    const a = document.createElement('a')
    a.href = url
    a.download = fileName
    document.body.appendChild(a)
    a.click()
    a.remove()
    message.success({ content: '下载已开始，进度见浏览器下载列表', key: 'download' })
  } catch (err: any) {
    if (err?.name === 'AbortError') {
      message.info({ content: '下载已取消', key: 'download' })
    } else {
      message.error({ content: err?.message || '下载失败', key: 'download' })
    }
  }
}
