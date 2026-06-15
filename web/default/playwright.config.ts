import { defineConfig, devices } from '@playwright/test'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:3101'
const backendURL = process.env.E2E_BACKEND_URL || 'http://127.0.0.1:3100'
const backendPort = new URL(backendURL).port || '3000'
const frontendPort = new URL(baseURL).port || '3001'

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  timeout: 30_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: [['list']],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  webServer: [
    {
      command: `CRITICAL_RATE_LIMIT_ENABLE=false GLOBAL_WEB_RATE_LIMIT_ENABLE=false CRITICAL_RATE_LIMIT=1000 go run . --port ${backendPort}`,
      cwd: path.resolve(__dirname, '../..'),
      env: {
        ...process.env,
        CRITICAL_RATE_LIMIT_ENABLE: 'false',
        GLOBAL_WEB_RATE_LIMIT_ENABLE: 'false',
      },
      url: backendURL,
      reuseExistingServer: true,
      timeout: 60_000,
    },
    {
      command: `bun run dev -- --host 127.0.0.1 --port ${frontendPort}`,
      cwd: __dirname,
      env: {
        ...process.env,
        VITE_REACT_APP_SERVER_URL: backendURL,
      },
      url: baseURL,
      reuseExistingServer: true,
      timeout: 60_000,
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // Ubuntu 26.04+ does not support Playwright's bundled Chromium — use system browser
        ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
          ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH }
          : {}),
      },
    },
  ],
})
