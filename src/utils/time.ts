// 强制 24 小时制（00–23）：toLocaleString 在部分语言环境下默认显示 12 小时制 AM/PM；
// hour12:false 在部分浏览器零点会显示为 24，故用 hourCycle:'h23'。
export const formatDateTime = (value: string | Date): string =>
  new Date(value).toLocaleString(undefined, { hourCycle: 'h23' })
