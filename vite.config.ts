import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 本地联调：
// - 推荐经 edgeone makers 网关：http://127.0.0.1:8088/
// - WSL + VS Code 端口转发时，Windows 往往只能访问 Vite 端口（如 6699），
//   此时由 Vite 将 /api 代理到 makers 网关（WSL 内 8088 可达）。
export default defineConfig({
  plugins: [react()],
  server: {
    // 与 makers 并行时可用 `vite --port 6699`；代理目标为 WSL 内 makers 网关
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8088',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
