import { defineConfig } from '@playwright/test'

// 布局回归跑在浏览器里而不是 Tauri 窗口里：布局只由 DOM 与 CSS 决定，
// IPC 由 tests/fixtures 注入，这样一次跑完「页面 × 数据形态 × 视口」的矩阵。
const port = 5178

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? [['github'], ['list']] : [['list']],
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `npx vite --port ${port} --strictPort --host 127.0.0.1`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: !process.env.CI,
    stdout: 'ignore',
    timeout: 60_000,
  },
})
