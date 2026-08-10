import JSZip from 'jszip'
import { listFiles, type FileItem } from '../api/files'
import { proxySegmentDownload, saveBlob } from './proxyDownload'

/** 待打包条目：zip 内相对路径 + 文件 ID + 原始大小（用于进度条总进度）。 */
export interface ZipEntry {
  name: string
  id: number
  size: number
}

/** 当前用户目录列表：listFiles 的 FileItem[] 包装（collectEntries 的 listDir 签名）。 */
const listFilesDir = (pid: number) => listFiles(pid).then(r => r.data.files)

/** 通用：把一组文件/文件夹展开为 zip 条目（listDir 用于递归列出子目录，保持 zip 目录结构）。 */
export async function collectEntries(
  items: FileItem[],
  listDir: (parentId: number) => Promise<FileItem[]>,
): Promise<ZipEntry[]> {
  const out: ZipEntry[] = []
  const walk = async (list: FileItem[], prefix: string) => {
    for (const f of list) {
      if (f.is_dir) {
        const children = await listDir(f.id)
        await walk(children, `${prefix}${f.name}/`)
      } else {
        out.push({ name: `${prefix}${f.name}`, id: f.id, size: f.size })
      }
    }
  }
  await walk(items, '')
  return out
}

/** 递归收集当前用户某文件夹下的所有文件（zip 内保留目录结构）。 */
export async function collectDirFiles(dirId: number): Promise<ZipEntry[]> {
  const res = await listFiles(dirId)
  return collectEntries(res.data.files, listFilesDir)
}

/** 批量选中的文件（含选中的文件夹递归展开）展开为 zip 条目。 */
export function collectSelectedEntries(items: FileItem[]): Promise<ZipEntry[]> {
  return collectEntries(items, listFilesDir)
}

/**
 * 逐个下载文件并用 JSZip 打包为 zip，最终触发浏览器保存。
 * 绕开 EdgeOne 云函数 6MB 响应缓冲：每个文件走分段/直链拉取（单请求 < 6MB），
 * 打包在本机内存完成。zip 不压缩（STORE），体积约等于文件之和，速度快。
 *
 * @param fetchUrl 由文件 ID 获取下载 URL（登录态/分享态 API 不同，参数化注入）
 */
export async function downloadEntriesAsZip(
  entries: ZipEntry[],
  zipName: string,
  fetchUrl: (id: number) => Promise<string>,
  onProgress?: (received: number, total: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  const total = entries.reduce((sum, e) => sum + e.size, 0)
  const zip = new JSZip()
  let received = 0

  for (const entry of entries) {
    if (signal?.aborted) throw new DOMException('aborted', 'AbortError')
    const url = await fetchUrl(entry.id)
    let blob: Blob
    if (url.startsWith('/api/files/stream') || url.startsWith('/api/files/proxy')) {
      blob = await proxySegmentDownload(url, undefined, signal)
    } else {
      const resp = await fetch(url, { signal })
      if (!resp.ok) throw new Error(`下载 ${entry.name} 失败（HTTP ${resp.status}）`)
      blob = await resp.blob()
    }
    zip.file(entry.name, blob)
    received += blob.size
    onProgress?.(received, total)
  }

  // STORE 不压缩，生成阶段极快，进度以"已下载字节/总字节"为准
  const zipBlob = await zip.generateAsync({ type: 'blob', compression: 'STORE' })
  saveBlob(zipBlob, zipName)
}
