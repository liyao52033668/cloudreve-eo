/** 复制文本到剪贴板。
 * navigator.clipboard 仅在安全上下文（https/localhost）可用，
 * http、IP 访问、低版本浏览器或页面未聚焦时可能不可用或抛异常，
 * 统一 try-catch 后降级为 execCommand 方案。 */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return
    } catch {
      // Clipboard API 失败（非安全上下文、权限拒绝、页面未聚焦等），走降级方案
    }
  }

  // 降级方案：创建隐藏 textarea，使用 execCommand('copy')
  const textarea = document.createElement('textarea')
  textarea.value = text
  // 避免弹出键盘/页面跳动
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  textarea.style.top = '0'
  textarea.style.left = '-9999px'
  // iOS 需要可编辑且未禁用才能 select
  textarea.setAttribute('readonly', '')
  document.body.appendChild(textarea)

  try {
    // iOS 特殊处理：使用 setSelectionRange 代替 select，兼容性更好
    if (/iPhone|iPad|iPod|Macintosh/i.test(navigator.userAgent)) {
      textarea.setSelectionRange(0, textarea.value.length)
    } else {
      textarea.select()
    }
    if (!document.execCommand('copy')) {
      throw new Error('execCommand copy failed')
    }
  } finally {
    document.body.removeChild(textarea)
  }
}
