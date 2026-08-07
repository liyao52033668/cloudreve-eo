const pad = (n: number) => String(n).padStart(2, '0')

// 固定为 YYYY-MM-DD HH:mm:ss（与后端 Go "2006-01-02 15:04:05" 格式一致），
// 不依赖浏览器 locale 的 toLocaleString（部分环境会显示 12 小时制或斜杠日期）。
export const formatDateTime = (value: string | Date): string => {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return typeof value === 'string' ? value : ''
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  )
}
