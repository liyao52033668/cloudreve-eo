import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 本地联调请使用 `npm run dev`（edgeone makers dev，默认 http://127.0.0.1:8088/）。
// makers 会统一托管前端与 cloud-functions，一般不需要在此配置 /api 代理。
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
