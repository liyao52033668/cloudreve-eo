/** 代理存储（Filen/百度等无外链直链）文件的浏览器分段下载。
 *
 * 背景：EdgeOne 云函数响应有约 6MB 缓冲上限，边缘函数流式拼流又有执行时长限制，
 * 大文件无法完整送达。这里改由浏览器 JS 按 Range 分段拉取 /api/files/proxy 并拼接 Blob，
 * 完全绕开平台限制。
 *
 * 签名只包含 policy/key/name/exp/sig，与请求路径无关，因此 stream URL 可直接换用
 * proxy 路径，无需重新签名。
 */
export async function proxySegmentDownload(
  streamUrl: string,
  onProgress?: (received: number, total: number) => void,
  signal?: AbortSignal,
): Promise<Blob> {
  const proxyUrl = new URL(streamUrl, window.location.origin)
  proxyUrl.pathname = '/api/files/proxy'
  const SEG = 1024 * 1024 // 单段 1MB，稳妥低于云函数约 6MB 响应上限

  // 首段请求：顺带从 Content-Range 解析文件总大小
  const first = await fetch(proxyUrl.toString(), {
    headers: { Range: `bytes=0-${SEG - 1}` },
    signal,
  })
  if (first.status !== 206 && first.status !== 200) {
    const body = await first.json().catch(() => null)
    throw new Error(body?.error || `下载失败（HTTP ${first.status}）`)
  }
  let total = 0
  if (first.status === 206) {
    const m = first.headers.get('Content-Range')?.match(/\/(\d+)\s*$/)
    if (m) total = parseInt(m[1], 10)
  } else {
    // 上游忽略 Range 返回 200 整文件（小文件）：直接透传其内容
    total = parseInt(first.headers.get('Content-Length') || '0', 10)
  }

  const chunks: BlobPart[] = []
  const firstBuf = await first.arrayBuffer()
  chunks.push(firstBuf)
  let received = firstBuf.byteLength
  onProgress?.(received, total)

  // 首段校验：上游应返回 min(SEG, total) 字节；不足说明段流不完整，直接报错避免拼错位文件
  const firstExpect = total > 0 ? Math.min(SEG, total) : SEG
  if (firstBuf.byteLength < firstExpect) {
    throw new Error(`下载数据不完整（${firstBuf.byteLength}/${firstExpect} 字节）`)
  }

  let offset = SEG
  while (total === 0 || offset < total) {
    const end = total > 0 ? Math.min(offset + SEG - 1, total - 1) : offset + SEG - 1
    const expect = end - offset + 1
    const resp = await fetch(proxyUrl.toString(), {
      headers: { Range: `bytes=${offset}-${end}` },
      signal,
    })
    if (resp.status !== 206 && resp.status !== 200) {
      throw new Error(`分段下载失败（HTTP ${resp.status}）`)
    }
    const buf = await resp.arrayBuffer()
    if (total > 0 && buf.byteLength < expect) {
      throw new Error(`分段数据不完整（${buf.byteLength}/${expect} 字节）`)
    }
    chunks.push(buf)
    received += buf.byteLength
    onProgress?.(received, total)
    if (total === 0 && buf.byteLength < SEG) break // 总大小未知时按 EOF 收尾
    offset = end + 1
  }
  return new Blob(chunks)
}

/** 触发浏览器保存文件：由 Blob 生成临时对象 URL 并模拟点击下载。 */
export function saveBlob(blob: Blob, fileName: string) {
  const objectUrl = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = objectUrl
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(objectUrl)
}

/** 把代理下载的 stream 签名 URL 换成 proxy 路径（签名与路径无关，可复用）。
 * 非 stream 路径（S3 直链）原样返回。 */
export function toProxyUrl(url: string): string {
  if (!url.startsWith('/api/files/stream')) return url
  const u = new URL(url, window.location.origin)
  u.pathname = '/api/files/proxy'
  return u.toString()
}
