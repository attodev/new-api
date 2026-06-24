import { expect, test, type BrowserContext, type Page } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME || 'admin'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'atto1234'

// Only the admin account relies on seeded defaults. The organization owner,
// org-admin, and member accounts plus the test organization are created and
// destroyed through the UI within this suite (see setup/teardown tests).
// A run-scoped id keeps names unique so a crashed run never collides with the next.
// Base-36 keeps it short (~8 chars) so the longest username (`e2e_member_<id>`)
// stays within the backend's 20-char `max` validation on Username/DisplayName.
const runId = Date.now().toString(36)
const ownerUsername = `e2e_owner_${runId}`
const orgAdminUsername = `e2e_admin_${runId}`
const memberUsername = `e2e_member_${runId}`
const createdUserPassword = adminPassword
const organizationName = `E2E Org ${runId}`

type SavedAuthStorage = {
  user: string
  uid: string
}

const authStorage = new WeakMap<Page, SavedAuthStorage>()
// Credentials per page so openAuthenticatedPage can fully re-login (re-establishing
// the session cookie, not just localStorage) when a page lands back on /sign-in.
const pageCredentials = new WeakMap<Page, { username: string; password: string }>()

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
  pageCredentials.set(page, { username, password })
  await page.context().addInitScript((storage: SavedAuthStorage) => {
    if (storage.user) window.localStorage.setItem('user', storage.user)
    if (storage.uid) window.localStorage.setItem('uid', storage.uid)
  }, savedStorage)
}

async function restoreAuth(page: Page, saved: SavedAuthStorage) {
  await page.evaluate((storage: SavedAuthStorage) => {
    if (storage.user) window.localStorage.setItem('user', storage.user)
    if (storage.uid) window.localStorage.setItem('uid', storage.uid)
  }, saved)
}

async function openAuthenticatedPage(page: Page, path = '/') {
  const saved = authStorage.get(page)
  const creds = pageCredentials.get(page)
  for (let attempt = 0; attempt < 3; attempt++) {
    // Set localStorage before navigation (same-origin persistence).
    if (saved) {
      await restoreAuth(page, saved).catch(() => {
        // Ignore: page might be at about:blank on the first attempt.
      })
    }
    await page.goto(path)
    // Wait for the network to settle so the auth guard's getSelf() call completes
    // before any assertions run.
    await page.waitForLoadState('networkidle').catch(() => {})
    if (!saved) return page
    // Verify auth survived. On snap Chromium the context-level initScript can lose
    // the race against the app's auth guard, and a restored session cookie can be
    // rejected (getSelf -> 401), both of which bounce the page to /sign-in.
    const hasUser = await page.evaluate(
      () => !!window.localStorage.getItem('user')
    )
    if (hasUser && !page.url().includes('/sign-in')) {
      return page
    }
    // Recovery: a full UI re-login deterministically re-establishes both the
    // session cookie and localStorage. Falls back to a localStorage restore if
    // we never captured credentials for this page.
    if (creds) {
      await signInWithUi(page, creds.username, creds.password)
    } else {
      await restoreAuth(page, saved).catch(() => {})
    }
  }
  await page.goto(path)
  await page.waitForLoadState('networkidle').catch(() => {})
  return page
}

async function createUserViaUi(
  page: Page,
  user: { username: string; password: string; displayName?: string }
) {
  await openAuthenticatedPage(page, '/users')
  await page.getByRole('button', { name: /^add user$/i }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toBeVisible()
  // Role defaults to "Common User" (role=1) — no need to touch the Role select.
  await drawer.getByPlaceholder('Enter username').fill(user.username)
  if (user.displayName) {
    await drawer.getByPlaceholder('Enter display name').fill(user.displayName)
  }
  await drawer.getByPlaceholder(/Enter password/i).fill(user.password)
  await page.getByRole('button', { name: /^save changes$/i }).click()
  await expect(page.getByText(/user created successfully/i)).toBeVisible()
}

async function createOrganizationViaUi(
  page: Page,
  options: { name: string; ownerUsername: string }
) {
  await openAuthenticatedPage(page, '/organization')
  await page.getByPlaceholder('Organization Name').fill(options.name)
  // Owner selector is a Base UI Select; root sees exactly one combobox here
  // (the Create Organization form). Open it and pick the owner by "username #id".
  await page.getByRole('combobox').click()
  await page
    .getByRole('option', {
      name: new RegExp(`^${options.ownerUsername}\\s+#\\d+$`),
    })
    .click()
  await page.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByText(/organization created/i)).toBeVisible()
}

async function assignMemberViaUi(page: Page, username: string) {
  await openAuthenticatedPage(page, '/organization')
  await page.getByPlaceholder('Username', { exact: true }).fill(username)
  // Add button label is hard-coded Korean "추가" in organization-users-table.tsx.
  await page.getByRole('button', { name: '추가' }).click()
  await expect(page.getByText(/organization member assigned/i)).toBeVisible()
}

async function promoteMemberToAdminViaUi(page: Page, username: string) {
  await openAuthenticatedPage(page, '/organization')
  const row = page.getByRole('row').filter({ hasText: username })
  await expect(row).toBeVisible()
  // Only the owner sees the editable Role select in the members table.
  await row.getByRole('combobox').click()
  // Assumes the English locale: t('admin') falls back to the literal key.
  await page.getByRole('option', { name: /^admin$/i }).click()
  await page.getByRole('button', { name: /^save$/i }).click()
  await expect(page.getByText(/changes saved/i)).toBeVisible()
}

async function deleteOrganizationViaUi(page: Page, name: string) {
  await openAuthenticatedPage(page, '/organization')
  // The org cards and their section wrapper share the same classes, but only
  // the wrapper and the one leaf card matching `name` survive the hasText
  // filter; .last() picks the leaf (it appears later in the DOM than the wrapper).
  const card = page
    .locator('div.rounded-md.border.p-3')
    .filter({ hasText: name })
    .filter({ has: page.getByRole('button', { name: /^delete$/i }) })
    .last()
  // The trigger and (once open) the dialog's confirm button both expose the
  // "Delete" accessible name, so target the trigger by its data-slot to avoid
  // ambiguity.
  await card.locator('[data-slot="alert-dialog-trigger"]').click()
  // Scope to the dialog whose description names this organization.
  const dialog = page.getByRole('alertdialog').filter({ hasText: name })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: /^delete$/i }).click()
  // Assert the card disappears from the manage list rather than trusting the
  // toast, so a backend rejection fails the test loudly.
  await expect(card).toHaveCount(0)
}

async function deleteUserViaUi(page: Page, username: string) {
  await openAuthenticatedPage(page, '/users')
  await page.getByPlaceholder(/filter by username/i).fill(username)
  const row = page.getByRole('row').filter({ hasText: username })
  await expect(row).toBeVisible()
  await row.getByRole('button', { name: /open menu/i }).click()
  await page.getByRole('menuitem', { name: /^delete$/i }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog.getByText(/are you sure/i)).toBeVisible()
  await dialog.getByRole('button', { name: /^delete$/i }).click()
  await expect(page.getByText(/user deleted successfully/i)).toBeVisible()
}

test.describe('organization browser smoke tests', () => {
  test.describe.configure({ mode: 'serial' })
  test.setTimeout(60_000)

  let rootPage: Page
  let ownerPage: Page
  let organizationAdminPage: Page
  let memberPage: Page
  let contexts: BrowserContext[]

  test.beforeAll(async ({ browser }, testInfo) => {
    testInfo.setTimeout(120_000)
    contexts = await Promise.all([
      browser.newContext(),
      browser.newContext(),
      browser.newContext(),
      browser.newContext(),
    ])

    rootPage = await contexts[0].newPage()
    ownerPage = await contexts[1].newPage()
    organizationAdminPage = await contexts[2].newPage()
    memberPage = await contexts[3].newPage()

    // The dev server renders a fixed-position "Open TanStack Router Devtools"
    // button in the bottom corner that intercepts clicks on drawer/footer
    // buttons (e.g. "Save changes"). Hide it on every page in every context.
    await Promise.all(
      contexts.map((context) =>
        context.addInitScript(() => {
          const inject = () => {
            const id = 'e2e-hide-devtools'
            if (document.getElementById(id)) return
            const style = document.createElement('style')
            style.id = id
            style.textContent =
              '[aria-label="Open TanStack Router Devtools"],[aria-label="Open Tanstack query devtools"]{display:none !important;}'
            ;(document.head || document.documentElement)?.appendChild(style)
          }
          if (document.head || document.documentElement) inject()
          document.addEventListener('DOMContentLoaded', inject)
        })
      )
    )

    // Only the admin (root) account exists at the start. The other accounts are
    // created in the first test ("admin provisions ...") and signed in there.
    await signInWithUi(rootPage)
  })

  test.afterAll(async () => {
    await Promise.all(contexts.filter(Boolean).map((context) => context.close()))
  })

  test('admin provisions organization, users, and roles via UI', async () => {
    // 1) root creates three Common Users.
    await createUserViaUi(rootPage, {
      username: ownerUsername,
      password: createdUserPassword,
      displayName: ownerUsername,
    })
    await createUserViaUi(rootPage, {
      username: orgAdminUsername,
      password: createdUserPassword,
      displayName: orgAdminUsername,
    })
    await createUserViaUi(rootPage, {
      username: memberUsername,
      password: createdUserPassword,
      displayName: memberUsername,
    })

    // 2) root creates the organization, owned by the owner user.
    await createOrganizationViaUi(rootPage, {
      name: organizationName,
      ownerUsername,
    })

    // 3) owner signs in (account now exists) and assigns the other two as members.
    await signInWithUi(ownerPage, ownerUsername, createdUserPassword)
    await assignMemberViaUi(ownerPage, orgAdminUsername)
    await assignMemberViaUi(ownerPage, memberUsername)

    // 4) owner promotes the org-admin user from member -> admin.
    await promoteMemberToAdminViaUi(ownerPage, orgAdminUsername)

    // 5) org-admin and member sign in for the downstream tests.
    await signInWithUi(
      organizationAdminPage,
      orgAdminUsername,
      createdUserPassword
    )
    await signInWithUi(memberPage, memberUsername, createdUserPassword)
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
    const page = await openAuthenticatedPage(rootPage, '/organization/dashboard')

    const selector = page.getByRole('combobox').first()
    await expect(selector).toBeVisible()
    const selectedText = (await selector.textContent())?.trim()
    test.skip(!selectedText, 'No organization is available for selection.')

    await openAuthenticatedPage(page, '/organization/usage-logs/common')
    await openAuthenticatedPage(page, '/organization/dashboard')

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

      await openAuthenticatedPage(page, '/organization')
      await expect(page).toHaveURL(/\/organization\/?$/)
      await expect(
        page.getByRole('heading', { name: /organization users/i })
      ).toBeVisible()

      await openAuthenticatedPage(page, '/organization/subscriptions')
      await expect(page).toHaveURL(/\/organization\/subscriptions/)
      await expect(
        page.getByRole('heading', { name: /organization subscription/i })
      ).toBeVisible()
    }
  })

  test('organization usage logs render for admins', async () => {
    const page = await openAuthenticatedPage(rootPage, '/organization/usage-logs/common')
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

  test('organization users table renders members for admin', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization')
    await expect(page).toHaveURL(/\/organization\/?$/)
    await expect(
      page.getByRole('heading', { name: /organization users/i })
    ).toBeVisible()
    // table header columns
    await expect(page.getByRole('columnheader', { name: /username/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /role/i })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: /status/i })).toBeVisible()
  })

  test('organization users search filters table results', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization')

    const searchInput = page.getByPlaceholder(/search by username/i)
    await expect(searchInput).toBeVisible()

    // type a query that should not match any user
    await searchInput.fill('__no_match_xyzzy__')
    // wait for debounce + API response then check empty state
    await expect(page.getByText(/no data/i)).toBeVisible()
  })

  test('export button is visible for admin and triggers download', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization')

    const exportBtn = page.getByRole('button', { name: /^export$/i })
    await expect(exportBtn).toBeVisible()

    const [download] = await Promise.all([
      page.waitForEvent('download'),
      exportBtn.click(),
    ])
    expect(download.suggestedFilename()).toMatch(/org-users.*\.xlsx$/)
  })

  test('import button is visible for admin and opens dialog', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization')

    const importBtn = page.getByRole('button', { name: /^import$/i })
    await expect(importBtn).toBeVisible()
    await importBtn.click()

    // dialog / modal should open
    await expect(page.getByRole('dialog')).toBeVisible()
    // close dialog
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })

  test('add member section is visible for admin but not for member', async () => {
    const adminPage2 = await openAuthenticatedPage(organizationAdminPage, '/organization')
    await expect(adminPage2.getByPlaceholder('Username', { exact: true })).toBeVisible()

    const memberPage2 = await openAuthenticatedPage(memberPage, '/organization')
    await expect(memberPage2).toHaveURL(/\/403|\/sign-in/)
  })

  test('owner sees all controls including role selector in table', async () => {
    const page = await openAuthenticatedPage(ownerPage, '/organization')
    await expect(
      page.getByRole('heading', { name: /organization users/i })
    ).toBeVisible()
    // role column should exist
    await expect(page.getByRole('columnheader', { name: /role/i })).toBeVisible()
  })

  test('dashboard preset buttons change the chart range', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization/dashboard')
    await expect(
      page.getByRole('heading', { name: /organization dashboard/i })
    ).toBeVisible()

    // preset buttons: Today / 7d / 30d
    for (const label of ['Today', '7d', '30d']) {
      const btn = page.getByRole('button', { name: new RegExp(`^${label}$`, 'i') })
      if (await btn.isVisible()) {
        await btn.click()
        // no error toast should appear
        await expect(page.getByText(/error/i)).toHaveCount(0)
      }
    }
  })

  test('subscription page renders plan configuration form for admin', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization/subscriptions')
    await expect(
      page.getByRole('heading', { name: /organization subscription/i })
    ).toBeVisible()

    await expect(page.getByText(/plan configuration/i)).toBeVisible()
    await expect(page.getByText(/plan title/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible()
  })

  test('subscription page create plan validates empty title', async () => {
    const page = await openAuthenticatedPage(organizationAdminPage, '/organization/subscriptions')

    // click Create without filling title
    const createBtn = page.getByRole('button', { name: /^create$/i })
    await expect(createBtn).toBeVisible()
    await createBtn.click()

    // toast with validation error should appear
    await expect(page.getByText(/plan title is required/i)).toBeVisible()
  })

  test('admin removes organization and users via UI', async () => {
    // Best-effort cleanup: keep going even if an earlier step left things partial.
    // The backend refuses to delete an organization that still has non-owner
    // members, so order matters: remove the non-owner members, then the
    // organization (the owner is excluded from the member check), then the owner.
    for (const username of [orgAdminUsername, memberUsername]) {
      try {
        await deleteUserViaUi(rootPage, username)
      } catch (error) {
        console.error(`Failed to delete user ${username}:`, error)
      }
    }

    try {
      await deleteOrganizationViaUi(rootPage, organizationName)
    } catch (error) {
      console.error(`Failed to delete organization ${organizationName}:`, error)
    }

    try {
      await deleteUserViaUi(rootPage, ownerUsername)
    } catch (error) {
      console.error(`Failed to delete user ${ownerUsername}:`, error)
    }
  })
})
