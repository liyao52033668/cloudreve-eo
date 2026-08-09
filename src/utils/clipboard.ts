/** 复制文本到剪贴板。
 * navigator.clipboard 仅在安全上下文（https/localhost）可用，
 * http 或 IP 访问时降级为 execCommand 方案。 */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }
  const textarea = document.createElement('textarea')
  textarea.value = text
  // 避免弹出键盘/页面跳动
  textarea.style.position = 'fixed'
  textarea.style.opacity = '0'
  document.body.appendChild(textarea)
  textarea.select()
  try {
    if (!document.execCommand('copy')) {
      throw new Error('execCommand copy failed')
    }
  } finally {
    document.body.removeChild(textarea)
  }
}
