import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 60000,
  retries: 0,
  use: {
    baseURL: 'http://localhost:8851',
    headless: false,
    screenshot: 'only-on-failure',
    trace: 'on-first-retry',
  },
  webServer: {
    command: 'pnpm dev',
    port: 8851,
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },
  reporter: [
    ['list'],
    ['html', { open: 'never' }],
  ],
})
