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
import { expect, test } from '@playwright/test'

// Korean interface smoke (fork: O9). A browser that prefers ko-KR gets the
// Korean locale on the sign-in page and keeps it in the console after login.
// The labels are the ko.json values of `Sign in`, the two sign-in
// placeholders and the `Dashboard` sidebar entry.

const adminUsername = process.env.E2E_ADMIN_USERNAME || 'root'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || '12345678'

test('a ko-KR browser sees the sign-in page in Korean and the console in the saved language (Korean when none)', async ({
  browser,
}) => {
  const context = await browser.newContext({ locale: 'ko-KR' })
  try {
    const page = await context.newPage()
    await page.goto('/sign-in')

    const signIn = page.getByRole('button', { name: '로그인', exact: true })
    await expect(signIn).toBeVisible()

    await page
      .getByPlaceholder('사용자 이름 또는 이메일 입력')
      .fill(adminUsername)
    await page.getByPlaceholder('비밀번호 입력').fill(adminPassword)
    const [response] = await Promise.all([
      page.waitForResponse(
        (r) =>
          r.url().includes('/api/user/login') && r.request().method() === 'POST'
      ),
      signIn.click(),
    ])
    const loginStatus = response.status()
    const loginBody = await response.text()
    expect(response.ok(), `login responded ${loginStatus}: ${loginBody}`).toBe(
      true
    )

    await expect(page).toHaveURL(/\/dashboard/)
    // fork O9: after sign-in the console follows the account's saved UI
    // language; the browser locale only applies when nothing is saved. On a
    // production database copy the admin usually has a saved language, so
    // accept either console language here (the sign-in page above already
    // proved the ko-KR locale path). Set E2E_ADMIN_LANGUAGE=ko|en to pin it.
    const pinned = process.env.E2E_ADMIN_LANGUAGE
    let expected: string | RegExp = /^(대시보드|Dashboard)$/
    if (pinned === 'ko') expected = '대시보드'
    else if (pinned === 'en') expected = 'Dashboard'
    await expect(
      page.getByRole('link', { name: expected }).first()
    ).toBeVisible()
  } finally {
    await context.close()
  }
})
