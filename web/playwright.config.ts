/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { defineConfig, devices } from '@playwright/test'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const baseURL = process.env.E2E_BASE_URL || 'http://127.0.0.1:3101'
const backendURL = process.env.E2E_BACKEND_URL || 'http://127.0.0.1:3100'
const backendPort = new URL(backendURL).port || '3000'
const frontendPort = new URL(baseURL).port || '3001'

// A backend started by Playwright uses a scratch SQLite file outside the repo
// unless SQL_DSN or SQLITE_PATH is already set. The pragmas mirror the
// backend's default common.SQLitePath.
const sqlitePath =
  process.env.SQLITE_PATH ||
  `${path.join(os.tmpdir(), 'new-api-e2e.db')}?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate`

const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH

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
    video: 'off',
  },
  webServer: [
    {
      command: `go run . --port ${backendPort}`,
      cwd: path.resolve(__dirname, '..'),
      env: {
        ...process.env,
        SQLITE_PATH: sqlitePath,
        CRITICAL_RATE_LIMIT_ENABLE: 'false',
        GLOBAL_WEB_RATE_LIMIT_ENABLE: 'false',
        GLOBAL_API_RATE_LIMIT_ENABLE: 'false',
      },
      url: backendURL,
      reuseExistingServer: true,
      timeout: 180_000,
    },
    {
      command: `bun run dev -- --host 127.0.0.1 --port ${frontendPort} --strict-port`,
      cwd: __dirname,
      env: {
        ...process.env,
        VITE_REACT_APP_SERVER_URL: backendURL,
      },
      url: baseURL,
      reuseExistingServer: true,
      timeout: 120_000,
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // Set PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH to use a system Chromium
        // (e.g. snap Chromium on Ubuntu 26.04+); otherwise Playwright's bundle.
        ...(chromiumExecutablePath
          ? { launchOptions: { executablePath: chromiumExecutablePath } }
          : {}),
      },
    },
  ],
})
