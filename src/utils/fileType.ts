import type { FileItem } from '../api/files'

export const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico', 'avif']
export const VIDEO_EXTS = ['mp4', 'webm', 'mkv', 'ogg', 'ogv', 'mov', 'avi', 'm4v', 'ts', 'flv', 'wmv', '3gp']

const ext = (name: string) => name.split('.').pop()?.toLowerCase() || ''

export const isImage = (f: FileItem) =>
  f.mime_type?.startsWith('image/') || IMAGE_EXTS.includes(ext(f.name))

export const isVideo = (f: FileItem) =>
  f.mime_type?.startsWith('video/') || VIDEO_EXTS.includes(ext(f.name))
