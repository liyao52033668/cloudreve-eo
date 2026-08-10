import type { FileItem } from '../api/files'

export const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico', 'avif']
export const VIDEO_EXTS = ['mp4', 'webm', 'mkv', 'ogg', 'ogv', 'mov', 'avi', 'm4v', 'ts', 'flv', 'wmv', '3gp']
/** 可文本预览的常见文件格式（txt/md/json 及常见代码/配置文本） */
export const TEXT_EXTS = [
  'txt', 'md', 'markdown', 'json', 'log', 'csv', 'tsv',
  'js', 'jsx', 'ts', 'tsx', 'vue', 'css', 'scss', 'less', 'html', 'htm', 'xml',
  'yaml', 'yml', 'toml', 'ini', 'conf', 'cfg', 'env', 'sh', 'bash', 'zsh',
  'py', 'go', 'java', 'c', 'h', 'cpp', 'hpp', 'cs', 'rb', 'php', 'swift',
  'kt', 'rs', 'sql', 'graphql', 'dockerfile', 'makefile', 'sqlite', 'properties',
]

const ext = (name: string) => name.split('.').pop()?.toLowerCase() || ''

export const isImage = (f: FileItem) =>
  f.mime_type?.startsWith('image/') || IMAGE_EXTS.includes(ext(f.name))

export const isVideo = (f: FileItem) =>
  f.mime_type?.startsWith('video/') || VIDEO_EXTS.includes(ext(f.name))

/** 文本类文件：mime 以 text/ 开头，或扩展名在可预览文本列表中 */
export const isText = (f: FileItem) =>
  f.mime_type?.startsWith('text/') || TEXT_EXTS.includes(ext(f.name))

/** Markdown 文件 */
export const isMarkdown = (f: FileItem) => {
  const e = ext(f.name)
  return e === 'md' || e === 'markdown'
}

/** JSON 文件 */
export const isJson = (f: FileItem) => ext(f.name) === 'json' || f.mime_type === 'application/json'
