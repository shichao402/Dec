import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    host: '127.0.0.1',
    // 避开 Vite 默认 5173；与 tauri.conf.json devUrl、src-tauri frontend_guard 同步。
    port: 59124,
    strictPort: true,
    watch: {
      ignored: ['**/src-tauri/**'],
    },
  },
})
