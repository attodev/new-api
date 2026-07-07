import { expect, test, type Page } from '@playwright/test'
import fs from 'node:fs/promises'
import path from 'node:path'

const backendURL = process.env.E2E_BACKEND_URL || 'http://127.0.0.1:3100'
const adminUsername = process.env.E2E_ADMIN_USERNAME || 'root'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || '12345678'
const screenshotDir = path.resolve(
  process.cwd(),
  '../../docs/manuals/images/wallet-auto-recharge'
)

type ApiResponse<T> = {
  success?: boolean
  message?: string
  data?: T
}

async function initializeSetupIfNeeded(page: Page) {
  const statusResponse = await page.request.get(`${backendURL}/api/setup`)
  const statusBody = (await statusResponse.json()) as ApiResponse<{
    status?: boolean
  }>
  expect(
    statusResponse.ok(),
    statusBody.message || 'setup status request failed'
  ).toBe(true)
  if (statusBody.data?.status) return

  const setupResponse = await page.request.post(`${backendURL}/api/setup`, {
    data: {
      username: adminUsername,
      password: adminPassword,
      confirmPassword: adminPassword,
      SelfUseModeEnabled: false,
      DemoSiteEnabled: false,
    },
  })
  const setupBody = (await setupResponse.json()) as ApiResponse<unknown>
  expect(setupResponse.ok(), setupBody.message || 'setup request failed').toBe(
    true
  )
  expect(setupBody.success, setupBody.message || 'setup failed').toBe(true)
}

async function signInWithUi(page: Page) {
  await initializeSetupIfNeeded(page)
  await page.goto('/sign-in')
  await expect(page.getByRole('heading', { name: /sign in/i })).toBeVisible()

  await page.getByPlaceholder(/username|email/i).fill(adminUsername)
  await page.getByPlaceholder(/password/i).fill(adminPassword)
  const loginResponse = page.waitForResponse(
    (response) =>
      response.url().includes('/api/user/login') &&
      response.request().method() === 'POST'
  )
  const [response] = await Promise.all([
    loginResponse,
    page.getByRole('button', { name: /^sign in$/i }).click(),
  ])

  const body = (await response.json()) as ApiResponse<{
    id: number
    username: string
  }>
  expect(response.ok(), body.message || 'login request failed').toBe(true)
  expect(body.success, body.message || 'login failed').toBe(true)

  await expect
    .poll(async () =>
      page.evaluate(() => {
        const raw = window.localStorage.getItem('user')
        if (!raw) return ''
        try {
          return JSON.parse(raw).username as string
        } catch {
          return ''
        }
      })
    )
    .toBe(adminUsername)
}

async function createPreset(
  page: Page,
  payload: Record<string, string | number | boolean>
) {
  const uid = await page.evaluate(() => window.localStorage.getItem('uid') ?? '')
  expect(uid, 'logged-in uid is required for authenticated API calls').not.toBe(
    ''
  )
  const response = await page.request.post(
    `${backendURL}/api/admin/wallet/auto-recharge/presets`,
    { data: payload, headers: { 'New-Api-User': uid } }
  )
  const body = (await response.json()) as ApiResponse<unknown>
  expect(response.ok(), body.message || 'preset request failed').toBe(true)
  expect(body.success, body.message || 'preset create failed').toBe(true)
}

async function seedPresets(page: Page) {
  const runId = Date.now()
  const base = {
    target_scope: 'all',
    description: 'Documentation screenshot preset',
    enabled: true,
  }
  const scheduled = [
    { label: 'Daily', interval_unit: 'day', interval_value: 1, amount: 1000 },
    { label: 'Daily', interval_unit: 'day', interval_value: 1, amount: 2000 },
    { label: 'Weekly', interval_unit: 'day', interval_value: 7, amount: 5000 },
    { label: 'Monthly', interval_unit: 'month', interval_value: 1, amount: 1000 },
    { label: 'Monthly', interval_unit: 'month', interval_value: 1, amount: 2000 },
    { label: 'Monthly', interval_unit: 'month', interval_value: 1, amount: 3000 },
    { label: 'Custom', interval_unit: 'custom', interval_value: 1, custom_seconds: 86400, amount: 10000 },
  ]

  for (const [index, item] of scheduled.entries()) {
    await createPreset(page, {
      ...base,
      type: 'scheduled',
      name: `${item.label} ${item.amount} ${runId}`,
      amount: item.amount,
      threshold_amount: 0,
      interval_unit: item.interval_unit,
      interval_value: item.interval_value,
      custom_seconds: item.custom_seconds ?? 0,
      charge_immediately: true,
      sort_order: index,
    })
  }

  const thresholdAmounts = [10000, 30000]
  const thresholdBalances = [1000, 3000, 5000]
  let sortOrder = 100
  for (const amount of thresholdAmounts) {
    for (const thresholdAmount of thresholdBalances) {
      await createPreset(page, {
        ...base,
        type: 'threshold',
        name: `Auto ${amount} below ${thresholdAmount} ${runId}`,
        amount,
        threshold_amount: thresholdAmount,
        interval_unit: 'month',
        interval_value: 1,
        custom_seconds: 0,
        charge_immediately: false,
        sort_order: sortOrder++,
      })
    }
  }
}

test('captures wallet auto recharge admin and user manuals', async ({ page }) => {
  await fs.mkdir(screenshotDir, { recursive: true })
  await signInWithUi(page)
  await seedPresets(page)

  await page.goto('/system-settings/billing/wallet-auto-recharge-presets')
  await expect(page.getByText('Scheduled recharge options')).toBeVisible()
  await expect(page.getByText('Auto recharge options')).toBeVisible()
  await page.screenshot({
    path: path.join(screenshotDir, 'admin-auto-recharge-options.png'),
    fullPage: true,
  })
  await page.getByText('Auto recharge options').scrollIntoViewIfNeeded()
  await page.screenshot({
    path: path.join(screenshotDir, 'admin-auto-recharge-threshold-options.png'),
    fullPage: true,
  })

  await page.goto('/wallet')
  await expect(page.getByText('Scheduled recharge')).toBeVisible()
  await expect(page.getByText('Auto recharge')).toBeVisible()
  await page.getByRole('tab', { name: 'Scheduled recharge' }).click()
  await expect(page.getByText('Choose recharge amount').first()).toBeVisible()
  await page.screenshot({
    path: path.join(screenshotDir, 'user-wallet-scheduled-recharge.png'),
    fullPage: true,
  })

  await page.getByRole('tab', { name: 'Auto recharge' }).click()
  await expect(page.getByText('Choose recharge amount').first()).toBeVisible()
  await page.screenshot({
    path: path.join(screenshotDir, 'user-wallet-auto-recharge.png'),
    fullPage: true,
  })
  await page.getByRole('button', { name: '10000' }).click()
  await expect(page.getByText('Choose threshold balance')).toBeVisible()
  await page.screenshot({
    path: path.join(screenshotDir, 'user-wallet-auto-recharge-threshold.png'),
    fullPage: true,
  })
})
