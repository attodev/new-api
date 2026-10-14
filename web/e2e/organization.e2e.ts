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
import { expect, test, type BrowserContext, type Page } from '@playwright/test'

const adminUsername = process.env.E2E_ADMIN_USERNAME || 'root'
// Upstream's password policy needs 8-128 characters, and the created accounts
// reuse this password.
const adminPassword = process.env.E2E_ADMIN_PASSWORD || '12345678'

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
const planTitle = `E2E Plan ${runId}`

// Credentials per page so openAuthenticatedPage can sign in again when a page
// lands back on /sign-in. Upstream auth persists nothing to localStorage: the
// access token lives in memory and each page load restores it from the
// HttpOnly refresh cookie (plus the new_api_has_session hint cookie), so the
// browser context's cookies are the whole session.
const pageCredentials = new WeakMap<
  Page,
  { username: string; password: string }
>()

async function signInWithUi(
  page: Page,
  username = adminUsername,
  password = adminPassword
) {
  await page.goto('/sign-in')
  await expect(page.getByRole('heading', { name: /^sign in$/i })).toBeVisible()

  await page.getByPlaceholder('Enter your username or email').fill(username)
  await page.getByPlaceholder('Enter password').fill(password)
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

  await expect(page).toHaveURL(/\/dashboard/)
  pageCredentials.set(page, { username, password })
}

// Upstream serves a public landing page at `/`; the console with the sidebar
// starts at /dashboard, so that is the default authenticated page.
async function openAuthenticatedPage(page: Page, path = '/dashboard') {
  const creds = pageCredentials.get(page)
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto(path)
    // Wait for the network to settle so the auth guard's session refresh
    // completes before any assertions run.
    await page.waitForLoadState('networkidle').catch(() => {})
    if (!creds || !page.url().includes('/sign-in')) return page
    // The refresh cookie was rejected and the guard bounced the page to
    // /sign-in: sign in through the UI again to re-establish the session.
    await signInWithUi(page, creds.username, creds.password)
  }
  // Sign-in lands on /dashboard; end on the requested path even when the
  // last attempt had to sign in again.
  await page.goto(path)
  await page.waitForLoadState('networkidle').catch(() => {})
  return page
}

// Teardown is best-effort: log a failed step and keep cleaning up.
function reportCleanupFailure(message: string, error: unknown) {
  process.stderr.write(`${message} ${String(error)}\n`)
}

// Upstream asks for step-up verification before sensitive admin actions such
// as deleting a user. Without 2FA or a passkey, the password method applies.
async function completeSecureVerification(page: Page, password: string) {
  // The dialog title names the action (e.g. "Verify to delete user").
  const dialog = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('button', { name: /^verify$/i }) })
  await expect(dialog).toBeVisible()
  // The method tab panel is also labelled "Password"; target the input.
  await dialog.locator('input[type="password"]').fill(password)
  await dialog.getByRole('button', { name: /^verify$/i }).click()
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
  // Owner selector is a Base UI Select whose trigger shows the "Owner User"
  // placeholder until a user is picked. Filter by that text so we don't match
  // the root org-scope selector that also renders as a combobox on this page.
  await page.getByRole('combobox').filter({ hasText: 'Owner User' }).click()
  await page
    .getByRole('option', {
      name: new RegExp(`^${options.ownerUsername}\\s+#\\d+$`),
    })
    .click()
  await page.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByText(/organization created/i)).toBeVisible()
}

// Members are added by exact lookup (no listing): look the user up, confirm
// the single result row, then add.
async function assignMemberViaUi(page: Page, username: string) {
  await openAuthenticatedPage(page, '/organization')
  await page
    .getByPlaceholder('Username or email', { exact: true })
    .fill(username)
  await page.getByRole('button', { name: 'Look up user', exact: true }).click()
  await expect(page.getByText(username, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Add', exact: true }).click()
  await expect(page.getByText(/organization member assigned/i)).toBeVisible()
}

async function promoteMemberToAdminViaUi(page: Page, username: string) {
  await openAuthenticatedPage(page, '/organization')
  const row = page.getByRole('row').filter({ hasText: username })
  await expect(row).toBeVisible()
  // Only the owner sees the editable Role select in the members table.
  await row.getByRole('combobox', { name: 'Role' }).click()
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
  await completeSecureVerification(page, adminPassword)
  await expect(page.getByText(/user deleted successfully/i)).toBeVisible()
}

async function removeMemberFromOrganizationViaUi(page: Page, username: string) {
  // Only an org owner/admin can remove members, so this must run on the owner's
  // (or an org-admin's) page. Select the member's row, remove it from the org,
  // then save the pending change.
  await openAuthenticatedPage(page, '/organization')
  const row = page.getByRole('row').filter({ hasText: username })
  await expect(row).toBeVisible()
  await row.locator('input[type="checkbox"]').check()
  // "Remove from organization" only shows for org admins/owners.
  await page.getByRole('button', { name: 'Remove from organization' }).click()
  await page.getByRole('button', { name: /^save$/i }).click()
  await expect(page.getByText(/changes saved/i)).toBeVisible()
}

async function createOrganizationPlanViaUi(
  page: Page,
  plan: { title: string; quotaAmount: string }
) {
  await openAuthenticatedPage(page, '/organization/subscriptions')
  // The form labels are not associated with their inputs, so scope by the
  // "Plan configuration" card: the first textbox is Plan title and the first
  // number input is Quota amount. Duration keeps its default (1 month).
  const card = page
    .locator('[data-slot="card"]')
    .filter({ hasText: 'Plan configuration' })
  await card.getByRole('textbox').first().fill(plan.title)
  await card.locator('input[type="number"]').first().fill(plan.quotaAmount)
  await card.getByRole('button', { name: /^create$/i }).click()
  await expect(page.getByText(/organization plan saved/i)).toBeVisible()
  // The new plan appears in the "Organization plans" table.
  const plansCard = page
    .locator('[data-slot="card"]')
    .filter({ hasText: 'Organization plans' })
  await expect(plansCard.getByText(plan.title, { exact: true })).toBeVisible()
}

async function assignPlanToMemberViaUi(
  page: Page,
  options: { memberLabel: string; planTitle: string }
) {
  await openAuthenticatedPage(page, '/organization/subscriptions')
  // "Member plan assignment" uses native <select> elements: first = user,
  // second = plan.
  const card = page
    .locator('[data-slot="card"]')
    .filter({ hasText: 'Member plan assignment' })
  await card
    .locator('select')
    .first()
    .selectOption({ label: options.memberLabel })
  await card.locator('select').nth(1).selectOption({ label: options.planTitle })
  await card.getByRole('button', { name: /^assign$/i }).click()
  await expect(page.getByText(/organization plan assigned/i)).toBeVisible()
  // The assignment shows up as a row (Plan column cell) in the member table.
  await expect(
    card.getByRole('cell', { name: options.planTitle })
  ).toBeVisible()
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
            if (document.querySelector(`#${id}`)) return
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
    await Promise.all(
      contexts.filter(Boolean).map((context) => context.close())
    )
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
    const page = await openAuthenticatedPage(
      rootPage,
      '/organization/dashboard'
    )

    const selector = page.getByRole('combobox').first()
    await expect(selector).toBeVisible()
    const selectedText = (await selector.textContent())?.trim()
    test.skip(!selectedText, 'No organization is available for selection.')

    await openAuthenticatedPage(page, '/organization/usage-logs/common')
    await openAuthenticatedPage(page, '/organization/dashboard')

    await expect(selector).toContainText(selectedText ?? '')
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
    const page = await openAuthenticatedPage(
      rootPage,
      '/organization/usage-logs/common'
    )
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
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )
    await expect(page).toHaveURL(/\/organization\/?$/)
    await expect(
      page.getByRole('heading', { name: /organization users/i })
    ).toBeVisible()
    // table header columns
    await expect(
      page.getByRole('columnheader', { name: /username/i })
    ).toBeVisible()
    await expect(
      page.getByRole('columnheader', { name: /role/i })
    ).toBeVisible()
    await expect(
      page.getByRole('columnheader', { name: /status/i })
    ).toBeVisible()
    await expect(
      page.getByRole('columnheader', { name: /organization note/i })
    ).toBeVisible()
  })

  test('admin suspends and resumes a member (organization member status)', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )
    const memberRow = page.getByRole('row', {
      name: new RegExp(memberUsername),
    })
    await expect(memberRow.getByText('Active', { exact: true })).toBeVisible()

    await memberRow.getByRole('checkbox').check()
    await page.getByRole('button', { name: 'Suspend', exact: true }).click()
    await expect(memberRow.getByText('Pending suspension')).toBeVisible()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(
      memberRow.getByText('Suspended', { exact: true })
    ).toBeVisible()

    await memberRow.getByRole('checkbox').check()
    await page.getByRole('button', { name: 'Resume', exact: true }).click()
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(memberRow.getByText('Active', { exact: true })).toBeVisible()
  })

  test('organization users search filters table results', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )

    const searchInput = page.getByPlaceholder(/search by username/i)
    await expect(searchInput).toBeVisible()

    // type a query that should not match any user
    await searchInput.fill('__no_match_xyzzy__')
    // wait for debounce + API response then check empty state
    await expect(page.getByText(/no data/i)).toBeVisible()
  })

  test('export button is visible for admin and triggers download', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )

    const exportBtn = page.getByRole('button', { name: /^export$/i })
    await expect(exportBtn).toBeVisible()

    const [download] = await Promise.all([
      page.waitForEvent('download'),
      exportBtn.click(),
    ])
    expect(download.suggestedFilename()).toMatch(/org-users.*\.xlsx$/)
  })

  test('import button is visible for admin and opens dialog', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )

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
    const adminPage2 = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization'
    )
    await expect(
      adminPage2.getByPlaceholder('Username or email', { exact: true })
    ).toBeVisible()
    // There is no list of assignable users: an unknown name finds nothing.
    await adminPage2
      .getByPlaceholder('Username or email', { exact: true })
      .fill(`e2e_nobody_${runId}`)
    await adminPage2
      .getByRole('button', { name: 'Look up user', exact: true })
      .click()
    await expect(adminPage2.getByText('User not found')).toBeVisible()

    const memberPage2 = await openAuthenticatedPage(memberPage, '/organization')
    await expect(memberPage2).toHaveURL(/\/403|\/sign-in/)
  })

  test('owner sees all controls including role selector in table', async () => {
    const page = await openAuthenticatedPage(ownerPage, '/organization')
    await expect(
      page.getByRole('heading', { name: /organization users/i })
    ).toBeVisible()
    // role column should exist
    await expect(
      page.getByRole('columnheader', { name: /role/i })
    ).toBeVisible()
  })

  test('dashboard preset buttons change the chart range', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization/dashboard'
    )
    await expect(
      page.getByRole('heading', { name: /organization dashboard/i })
    ).toBeVisible()

    // preset buttons: Today / 7d / 30d
    for (const label of ['Today', '7d', '30d']) {
      const btn = page.getByRole('button', {
        name: new RegExp(`^${label}$`, 'i'),
      })
      if (await btn.isVisible()) {
        await btn.click()
        // no error toast should appear
        await expect(page.getByText(/error/i)).toHaveCount(0)
      }
    }
  })

  test('subscription page renders plan configuration form for admin', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization/subscriptions'
    )
    await expect(
      page.getByRole('heading', { name: /organization subscription/i })
    ).toBeVisible()

    await expect(page.getByText(/plan configuration/i)).toBeVisible()
    await expect(page.getByText(/plan title/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /^create$/i })).toBeVisible()
  })

  test('subscription page create plan validates empty title', async () => {
    const page = await openAuthenticatedPage(
      organizationAdminPage,
      '/organization/subscriptions'
    )

    // click Create without filling title
    const createBtn = page.getByRole('button', { name: /^create$/i })
    await expect(createBtn).toBeVisible()
    await createBtn.click()

    // toast with validation error should appear
    await expect(page.getByText(/plan title is required/i)).toBeVisible()
  })

  test('admin creates an organization subscription plan', async () => {
    await createOrganizationPlanViaUi(organizationAdminPage, {
      title: planTitle,
      quotaAmount: '10',
    })
  })

  test('admin assigns the organization subscription plan to a member', async () => {
    await assignPlanToMemberViaUi(organizationAdminPage, {
      memberLabel: memberUsername,
      planTitle,
    })
  })

  test('root admin can open organization users page without org_id error', async () => {
    const page = await openAuthenticatedPage(rootPage, '/organization')
    await expect(
      page.getByRole('heading', { name: /organization users/i })
    ).toBeVisible()
    // Root is not bound to one org, so it must scope the users list to an
    // organization; select the one created in setup. Without this scoping the
    // backend rejected the request with "organization_id is required".
    await page.locator('select').selectOption({ label: organizationName })
    await expect(page.getByText(/organization_id is required/i)).toHaveCount(0)
    // The selected org's members load (owner row is visible).
    await expect(
      page.getByText(ownerUsername, { exact: true }).first()
    ).toBeVisible()
  })

  test('admin removes organization members, organization, and users via UI', async () => {
    // Best-effort cleanup. Order matters:
    // 1) The owner removes the non-owner members from the organization — only an
    //    org owner/admin may remove members. (The backend also refuses to delete
    //    an org that still has non-owner members.)
    // 2) Root deletes the organization, which cascades subscription plans and
    //    user subscriptions and clears the owner's membership.
    // 3) Root deletes all the test accounts.
    for (const username of [orgAdminUsername, memberUsername]) {
      try {
        await removeMemberFromOrganizationViaUi(ownerPage, username)
      } catch (error) {
        reportCleanupFailure(
          `Failed to remove ${username} from organization:`,
          error
        )
      }
    }

    try {
      await deleteOrganizationViaUi(rootPage, organizationName)
    } catch (error) {
      reportCleanupFailure(
        `Failed to delete organization ${organizationName}:`,
        error
      )
    }

    for (const username of [orgAdminUsername, memberUsername, ownerUsername]) {
      try {
        await deleteUserViaUi(rootPage, username)
      } catch (error) {
        reportCleanupFailure(`Failed to delete user ${username}:`, error)
      }
    }
  })
})
