import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false, // 串行执行，避免 WebSocket 冲突
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1, // 单 worker，避免并发问题
  reporter: 'html',
  timeout: 180000, // 单个测试 180s 超时（HITL 测试需要重试时间）
  use: {
    baseURL: 'http://localhost:8851',
    trace: 'on-first-retry',
    viewport: { width: 1280, height: 720 },
    headless: !!process.env.CI, // CI 环境 headless，本地调试有头
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'pnpm dev',
    url: 'http://localhost:8851',
    reuseExistingServer: true, // 复用已启动的 dev server
  },
})
