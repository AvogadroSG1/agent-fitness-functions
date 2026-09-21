import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/browser',
  timeout: 30_000,
  use: { baseURL: process.env.ARCHITECTURE_BROWSER_URL ?? 'http://127.0.0.1:7890', ...devices['Desktop Chrome'], channel: process.env.BROWSER_CHANNEL ?? 'chrome', launchOptions: { args: ['--headless=new'] } },
  forbidOnly: !!process.env.CI,
  reporter: [['list']],
})
