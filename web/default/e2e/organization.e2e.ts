import { expect, test, type BrowserContext, type Page } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME || 'admin'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'atto1234'
const organizationOwnerUsername =
  process.env.E2E_ORGANIZATION_OWNER_USERNAME || 'atto.o'
const organizationAdminUsername =
  process.env.E2E_ORGANIZATION_ADMIN_USERNAME || 'atto.a'
const organizationMemberUsername =
  process.env.E2E_ORGANIZATION_MEMBER_USERNAME || 'atto.1'
const organizationPassword =
  process.env.E2E_ORGANIZATION_PASSWORD || adminPassword

type SavedAuthStorage = {
  user: string
  uid: string
}

const authStorage = new WeakMap<Page, SavedAuthStorage>()

async function signInWithUi(
  page: Page,
  username = adminUsername,
  password = adminPassword
) {
  await page.goto('/sign-in')
  await expect(page.getByRole('heading', { name: /sign in/i })).toBeVisible()

  await page.getByPlaceholder(/username|email/i).fill(username)
  await page.getByPlaceholder(/password/i).fill(password)
  const loginResponse = page.waitForResponse(
    (response) =>
      response.url().includes('/api/user/login') &&
      response.request().method() === 'POST'
  )
  const [response] = await Promise.all([
    loginResponse,
    page.getByRole('button', { name: /^sign in$/i }).click(),
  ])
  const responseText = await response.text()
  expect(
    response.ok(),
    `${username} login failed with HTTP ${response.status()}: ${responseText}`
  ).toBe(true)

  const body = (responseText ? JSON.parse(responseText) : {}) as {
    success?: boolean
    message?: string
  }
  expect(
    body.success,
    body.message || `${username} login failed with HTTP ${response.status()}`
  ).toBe(true)

  await expect(page).toHaveURL(/\/dashboard|\/playground|\/$/)
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
    .toBe(username)

  const savedStorage = await page.evaluate(() => ({
    user: window.localStorage.getItem('user') ?? '',
    uid: window.localStorage.getItem('uid') ?? '',
  }))
  authStorage.set(page, savedStorage)
  await page.context().addInitScript((storage: SavedAuthStorage) => {
    if (storage.user) window.localStorage.setItem('user', storage.user)
    if (storage.uid) window.localStorage.setItem('uid', storage.uid)
  }, savedStorage)
}

async function openAuthenticatedPage(page: Page, path = '/') {
  const saved = authStorage.get(page)
  if (saved) {
    await page.evaluate((storage) => {
      if (storage.user) window.localStorage.setItem('user', storage.user)
      if (storage.uid) window.localStorage.setItem('uid', storage.uid)
    }, saved)
  }
  await page.goto(path)
  return page
}

test.describe('organization browser smoke tests', () => {
  test.describe.configure({ mode: 'serial' })

  let rootPage: Page
  let ownerPage: Page
  let organizationAdminPage: Page
  let memberPage: Page
  let contexts: BrowserContext[]

  test.beforeAll(async ({ browser }) => {
    contexts = await Promise.all([
      browser.newContext(),
      browser.newContext(),
      browser.newContext(),
      browser.newContext(),
    ])

    rootPage = await contexts[0].newPage()
    await signInWithUi(rootPage)

    ownerPage = await contexts[1].newPage()
    await signInWithUi(
      ownerPage,
      organizationOwnerUsername,
      organizationPassword
    )

    organizationAdminPage = await contexts[2].newPage()
    await signInWithUi(
      organizationAdminPage,
      organizationAdminUsername,
      organizationPassword
    )

    memberPage = await contexts[3].newPage()
    await signInWithUi(
      memberPage,
      organizationMemberUsername,
      organizationPassword
    )
  })

  test.afterAll(async () => {
    await Promise.all(contexts.filter(Boolean).map((context) => context.close()))
  })

  test('root admin can see organization management navigation and pages', async () => {
    const page = await openAuthenticatedPage(rootPage)

    await expect(
      page.getByRole('link', { name: /organization users/i })
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: /organization dashboard/i })
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: /organization usage logs/i })
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: /organization task logs/i })
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: /organization subscription/i })
    ).toBeVisible()

    await page.getByRole('link', { name: /organization dashboard/i }).click()
    await expect(page).toHaveURL(/\/organization\/dashboard/)
    await expect(
      page.getByRole('heading', { name: /organization dashboard/i })
    ).toBeVisible()

    await page.getByRole('link', { name: /organization subscription/i }).click()
    await expect(page).toHaveURL(/\/organization\/subscriptions/)
    await expect(
      page.getByRole('heading', { name: /organization subscription/i })
    ).toBeVisible()
  })

  test('organization selection survives navigation between organization pages', async () => {
    const page = await openAuthenticatedPage(rootPage)
    await page.goto('/organization/dashboard')

    const selector = page.getByRole('combobox').first()
    await expect(selector).toBeVisible()
    const selectedText = (await selector.textContent())?.trim()
    test.skip(!selectedText, 'No organization is available for selection.')

    await page.goto('/organization/usage-logs/common')
    await page.goto('/organization/dashboard')

    await expect(selector).toContainText(selectedText!)
  })

  test('organization owner and admin can access organization management pages', async () => {
    for (const page of [ownerPage, organizationAdminPage]) {
      await openAuthenticatedPage(page)
      await expect(
        page.getByRole('link', { name: /organization users/i })
      ).toBeVisible()
      await expect(
        page.getByRole('link', { name: /organization subscription/i })
      ).toBeVisible()

      await page.goto('/organization')
      await expect(page).toHaveURL(/\/organization\/?$/)
      await expect(
        page.getByRole('heading', { name: /organization users/i })
      ).toBeVisible()

      await page.goto('/organization/subscriptions')
      await expect(page).toHaveURL(/\/organization\/subscriptions/)
      await expect(
        page.getByRole('heading', { name: /organization subscription/i })
      ).toBeVisible()
    }
  })

  test('organization usage logs render for admins', async () => {
    const page = await openAuthenticatedPage(rootPage)

    await page.goto('/organization/usage-logs/common')
    await expect(page).toHaveURL(/\/organization\/usage-logs\/common/)
    await expect(page.getByText(/usage logs/i).first()).toBeVisible()
    await expect(
      page.locator('button').filter({ hasText: /^Search$/ })
    ).toBeVisible()
    await expect(page.getByText(/rows per page/i)).toBeVisible()
  })

  test('organization member cannot open management pages directly', async () => {
    const page = await openAuthenticatedPage(memberPage)

    await expect(
      page.getByRole('link', { name: /organization users/i })
    ).toHaveCount(0)
    await expect(
      page.getByRole('link', { name: /organization dashboard/i })
    ).toHaveCount(0)
    await expect(
      page.getByRole('link', { name: /organization subscription/i })
    ).toHaveCount(0)

    await page.goto('/organization/dashboard')
    await expect(page).toHaveURL(/\/403|\/sign-in/)

    await page.goto('/organization/usage-logs/common')
    await expect(page).toHaveURL(/\/403|\/sign-in/)

    await page.goto('/organization/subscriptions')
    await expect(page).toHaveURL(/\/403|\/sign-in/)
  })
})
