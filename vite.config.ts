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
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        // 拆分第三方依赖，避免单个 chunk 超过 500 kB 警告，并利用浏览器缓存；
        // antd 体积过大，由 maxSize 进一步切成多个子 chunk
        codeSplitting: {
          groups: [
            { name: 'icons', test: /[\\/]@ant-design[\\/]icons[\\/]/, priority: 30 },
            { name: 'antd', test: /[\\/](antd|@ant-design)[\\/]/, priority: 20, maxSize: 400 * 1024 },
            { name: 'react', test: /[\\/](react|react-dom|scheduler)[\\/]/, priority: 20 },
            { name: 'vendor', test: /node_modules/, priority: 10 },
          ],
        },
      },
    },
  },
})
