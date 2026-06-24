# E2E Organization 자체 프로비저닝 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `organization.e2e.ts`에서 admin(root) 계정만 시드 기본값에 의존하고, owner/org-admin/member 계정과 테스트 조직은 UI를 통해 setup 테스트에서 생성하고 teardown 테스트에서 삭제하도록 변경한다.

**Architecture:** 단일 파일 Playwright serial 스위트. 첫 테스트 = UI 프로비저닝(사용자 3명 + 조직 생성, 멤버 배정, 역할 승격), 기존 14개 테스트 = 본문 그대로(인증 페이지만 setup에서 준비), 마지막 테스트 = UI 삭제(best-effort). 모든 셋업/티어다운은 UI 경유.

**Tech Stack:** Playwright (`@playwright/test`), React 19 프론트엔드(Base UI Dialog/Select), TanStack Router.

---

## 사전 사실 (코드 확인 완료)

- 사용자 생성 폼 기본 role = `Common User`(1) → role Select 조작 불필요. (`user-form.ts:48`)
- 토스트 문구: 사용자 생성 `User created successfully`, 사용자 삭제 `User deleted successfully`, 조직 생성 `Organization created`, 조직 삭제 `Organization deleted`, 멤버 배정 `Organization member assigned`, 역할 저장 `Changes saved`.
- 사용자 생성 drawer = Base UI Dialog → `getByRole('dialog')`. drawer 제목 `Create User`.
- `/organization` 라우트 가드: root 또는 org-admin/owner만 통과, member는 `/403`. (`organization/index.tsx:26-37`)
- 조직 생성/삭제 = root 전용. org-admin 역할 부여 = owner 전용(멤버 배정은 항상 `member`로 들어가고, owner가 Role Select로 `admin` 변경 후 Save).
- 추가 버튼 텍스트는 하드코딩 한글 `추가`. 사용자 테이블 검색 placeholder `Filter by username, name or email...`. 행 액션 트리거 sr-only `Open menu`.

## File Structure

- Modify: `web/default/e2e/organization.e2e.ts` — 식별자/헬퍼/setup·teardown 테스트 추가, beforeAll 축소. (유일한 코드 변경 파일)
- Modify: `web/default/e2e/organization.e2e.md` — 문서 갱신(16개 테스트, 자체 프로비저닝).
- Modify: `web/default/e2e/E2E_TEST_ANALYSIS.md` — 환경변수/픽스처/테스트 목록 갱신.

> 참고: TDD가 그대로 적용되지 않는다(우리가 작성하는 산출물이 곧 테스트다). 각 태스크의 검증은 `npx playwright test e2e/organization.e2e.ts --list`(TS 파싱 + 테스트 목록 확인)로 하고, 최종 태스크에서 라이브 백엔드 대상 전체 실행으로 행위를 검증한다.

---

### Task 1: 런 스코프 식별자 도입 및 조직 env 변수 제거

**Files:**
- Modify: `web/default/e2e/organization.e2e.ts:3-12`

- [ ] **Step 1: 상단 상수 블록 교체**

기존 (3–12행):

```ts
const adminUsername = process.env.E2E_ADMIN_USERNAME || 'admin'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'atto1234'
const organizationOwnerUsername =
  process.env.E2E_ORGANIZATION_OWNER_USERNAME || 'atto.o'
const organizationAdminUsername =
  process.env.E2E_ORGANIZATION_ADMIN_USERNAME || 'ato.a'
const organizationMemberUsername =
  process.env.E2E_ORGANIZATION_MEMBER_USERNAME || 'atto.1'
const organizationPassword =
  process.env.E2E_ORGANIZATION_PASSWORD || adminPassword
```

교체:

```ts
const adminUsername = process.env.E2E_ADMIN_USERNAME || 'admin'
const adminPassword = process.env.E2E_ADMIN_PASSWORD || 'atto1234'

// Only the admin account relies on seeded defaults. The organization owner,
// org-admin, and member accounts plus the test organization are created and
// destroyed through the UI within this suite (see setup/teardown tests).
// A run-scoped id keeps names unique so a crashed run never collides with the next.
const runId = `${Date.now()}`
const ownerUsername = `e2e_owner_${runId}`
const orgAdminUsername = `e2e_admin_${runId}`
const memberUsername = `e2e_member_${runId}`
const createdUserPassword = adminPassword
const organizationName = `E2E Org ${runId}`
const createdUsernames = [ownerUsername, orgAdminUsername, memberUsername]
```

- [ ] **Step 2: 파싱/목록 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list`
Expected: 컴파일 에러 없이 기존 테스트 목록 출력(아직 14개 + describe). 미사용 변수 경고가 나도 무방(다음 태스크에서 사용).

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): introduce run-scoped org identifiers, drop seeded org env vars"
```

---

### Task 2: beforeAll 축소 — root만 로그인

**Files:**
- Modify: `web/default/e2e/organization.e2e.ts:127-159` (beforeAll 블록)

- [ ] **Step 1: beforeAll 본문 교체**

기존 beforeAll(127–159행)을 아래로 교체:

```ts
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

    // Only the admin (root) account exists at the start. The other accounts are
    // created in the first test ("admin provisions ...") and signed in there.
    await signInWithUi(rootPage)
  })
```

- [ ] **Step 2: 파싱/목록 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list`
Expected: 컴파일 에러 없음.

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): sign in only root in beforeAll; defer org accounts to setup test"
```

---

### Task 3: UI 프로비저닝/정리 헬퍼 추가

**Files:**
- Modify: `web/default/e2e/organization.e2e.ts` — `openAuthenticatedPage` 함수 정의 직후(115행 부근, `test.describe` 시작 전)에 헬퍼 추가.

- [ ] **Step 1: 헬퍼 함수들 추가**

`openAuthenticatedPage`의 닫는 `}` 다음, `test.describe(...)` 앞에 삽입:

```ts
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
  // Owner selector is a Base UI Select; its trigger shows the "Owner User"
  // placeholder until a user is picked.
  await page.getByText('Owner User', { exact: true }).click()
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
  await page.getByRole('option', { name: /^admin$/i }).click()
  await page.getByRole('button', { name: /^save$/i }).click()
  await expect(page.getByText(/changes saved/i)).toBeVisible()
}

async function deleteOrganizationViaUi(page: Page, name: string) {
  await openAuthenticatedPage(page, '/organization')
  const card = page
    .locator('div.rounded-md.border')
    .filter({ hasText: name })
    .filter({ has: page.getByRole('button', { name: /^delete$/i }) })
    .first()
  await card.getByRole('button', { name: /^delete$/i }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog.getByText(/delete organization/i)).toBeVisible()
  await dialog.getByRole('button', { name: /^delete$/i }).click()
  await expect(page.getByText(/organization deleted/i)).toBeVisible()
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
```

- [ ] **Step 2: 파싱/목록 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list`
Expected: 컴파일 에러 없음.

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): add UI helpers for creating/deleting users and organizations"
```

---

### Task 4: setup 테스트 추가 (serial 첫 케이스)

**Files:**
- Modify: `web/default/e2e/organization.e2e.ts` — `test.afterAll(...)` 정의 바로 다음, 기존 첫 테스트(`'root admin can see ...'`) **앞**에 삽입.

- [ ] **Step 1: 프로비저닝 테스트 삽입**

`afterAll` 블록 다음 줄에 추가(기존 테스트들보다 먼저 위치해야 serial 첫 케이스가 됨):

```ts
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
```

- [ ] **Step 2: 파싱/목록 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list`
Expected: 새 테스트 `admin provisions organization, users, and roles via UI`가 목록 첫 항목으로 표시.

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): provision org accounts and roles via UI in setup test"
```

---

### Task 5: 기존 14개 테스트 검증 (변경 없음 확인)

**Files:**
- Inspect: `web/default/e2e/organization.e2e.ts` (테스트 본문 165–374행 부근)

기존 14개 테스트는 페이지 핸들(rootPage/ownerPage/organizationAdminPage/memberPage)에만 의존하고 시드 username/조직명을 단언에 사용하지 않으므로 **본문 수정이 필요 없다**. 이 태스크는 그 사실을 확인만 한다.

- [ ] **Step 1: 하드코딩 의존성 부재 확인**

Run: `cd web/default && grep -nE "atto|ato\.a|organizationOwnerUsername|organizationAdminUsername|organizationMemberUsername|organizationPassword" e2e/organization.e2e.ts`
Expected: 출력 없음(Task 1에서 모두 제거됨). 출력이 있으면 해당 라인을 새 동적 변수로 교체.

- [ ] **Step 2: 목록상 테스트 수 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list | grep -c "›"`
Expected: 15 (setup 1 + 기존 14). teardown은 Task 6에서 추가되어 16이 됨.

- [ ] **Step 3: (변경 없으면 커밋 생략)**

변경이 있었던 경우에만:

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): point existing org tests at dynamic fixtures"
```

---

### Task 6: teardown 테스트 추가 (serial 마지막 케이스)

**Files:**
- Modify: `web/default/e2e/organization.e2e.ts` — 마지막 테스트(`'subscription page create plan validates empty title'`) **다음**, `test.describe` 닫는 `})` **앞**에 삽입.

- [ ] **Step 1: 정리 테스트 삽입**

```ts
  test('admin removes organization and users via UI', async () => {
    // Best-effort cleanup: keep going even if an earlier step left things partial.
    try {
      await deleteOrganizationViaUi(rootPage, organizationName)
    } catch (error) {
      console.error(`Failed to delete organization ${organizationName}:`, error)
    }

    for (const username of createdUsernames) {
      try {
        await deleteUserViaUi(rootPage, username)
      } catch (error) {
        console.error(`Failed to delete user ${username}:`, error)
      }
    }
  })
```

- [ ] **Step 2: 파싱/목록 확인**

Run: `cd web/default && npx playwright test e2e/organization.e2e.ts --list | grep -c "›"`
Expected: 16 (setup 1 + 기존 14 + teardown 1). 마지막 항목이 `admin removes organization and users via UI`.

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): tear down org accounts and organization via UI"
```

---

### Task 7: 라이브 백엔드 대상 전체 스위트 실행 (행위 검증)

**Files:**
- (없음 — 실행 검증)

라이브 백엔드(rate limit 3종 비활성화)와 프론트가 필요하다. `playwright.config.ts`의 `webServer`가 자동 기동하거나, 이미 떠 있으면 재사용한다.

- [ ] **Step 1: 전체 스위트 실행**

Run:
```bash
cd web/default && E2E_BACKEND_URL=http://127.0.0.1:3000 npx playwright test e2e/organization.e2e.ts
```
Expected: 16개 테스트 전부 PASS. 첫 테스트가 계정/조직을 만들고, 중간 14개가 통과하며, 마지막 테스트가 정리.

- [ ] **Step 2: 잔여 데이터 없음 확인**

`admin`으로 로그인 후 `/users`에서 `e2e_owner_`, `e2e_admin_`, `e2e_member_` 검색 시 결과가 없어야 하고, `/organization`의 "Manage organization quota"에 `E2E Org ` 카드가 없어야 한다.

Run(수동/스크립트): 위 UI 확인. 잔여가 있으면 teardown 셀렉터를 점검(특히 `deleteOrganizationViaUi`의 카드 매칭, `deleteUserViaUi`의 검색 결과 대기).

- [ ] **Step 3: 실패 시 셀렉터 보정**

흔한 보정 포인트:
- Owner Select 옵션 미선택 → `getByText('Owner User')` 대신 `page.getByRole('combobox')` 중 Create 카드 내부로 스코프.
- 역할 옵션 `^admin$` 미매칭 → `t('admin')` 번역값 확인 후 정규식 조정.
- 조직 카드 `div.rounded-md.border` 매칭 과다/부족 → `.filter({ has: ... Delete 버튼 })`로 좁히거나 `#id` 텍스트로 추가 필터.

(보정 후 Step 1 재실행)

- [ ] **Step 4: Commit (보정이 있었던 경우)**

```bash
git add web/default/e2e/organization.e2e.ts
git commit -m "test(e2e): fix selectors after live run"
```

---

### Task 8: 문서 갱신

**Files:**
- Modify: `web/default/e2e/organization.e2e.md`
- Modify: `web/default/e2e/E2E_TEST_ANALYSIS.md`

- [ ] **Step 1: `organization.e2e.md` 갱신**

다음을 반영:
- 개요: "총 14개" → "총 16개(프로비저닝 setup 1 + 기능 14 + 정리 teardown 1)". 4개 컨텍스트 중 root만 beforeAll에서 로그인, 나머지는 setup 테스트에서 생성·로그인.
- 환경 변수 표: `E2E_ORGANIZATION_OWNER_USERNAME`/`..._ADMIN_USERNAME`/`..._MEMBER_USERNAME`/`E2E_ORGANIZATION_PASSWORD` 행 삭제. `E2E_ADMIN_USERNAME`/`E2E_ADMIN_PASSWORD`만 유지. "테스트가 owner/admin/member 계정과 조직을 런 스코프 유니크 이름(`e2e_*_<ts>`, `E2E Org <ts>`)으로 UI에서 생성·삭제" 문장 추가.
- 픽스처 구조: beforeAll은 root만 로그인하도록 설명 수정.
- 테스트 목록: 맨 앞에 `0. admin provisions organization, users, and roles via UI`, 맨 뒤에 `15. admin removes organization and users via UI` 항목 추가.

- [ ] **Step 2: `E2E_TEST_ANALYSIS.md` 갱신**

다음을 반영:
- "한눈에 보기" 표: 테스트 케이스 수 14 → 16. "테스트 대상" 설명에 자체 프로비저닝 추가.
- 실행 아키텍처: beforeAll이 root만 로그인하고 setup 테스트가 나머지 계정/조직을 UI로 생성한다는 흐름 추가.
- 커버리지 매트릭스: setup/teardown 두 행 추가(생성/삭제 UI 플로우 검증).
- 환경 변수: 조직 계정 env 제거 반영.
- 갭 분석: "멤버 추가/플랜 생성 성공 경로 미검증" 항목 중 멤버 추가·역할변경·삭제가 이제 setup/teardown로 커버됨을 반영(나머지 갭은 유지).

- [ ] **Step 3: Commit**

```bash
git add web/default/e2e/organization.e2e.md web/default/e2e/E2E_TEST_ANALYSIS.md
git commit -m "docs(e2e): document self-provisioning org test (16 cases)"
```

---

## Self-Review

**Spec coverage:**
- "admin만 시드 의존" → Task 1 (env 제거, runId 도입).
- "owner/admin/member·조직 UI로 생성" → Task 3 헬퍼 + Task 4 setup.
- "종료 전 UI로 삭제" → Task 3 헬퍼 + Task 6 teardown.
- "생성/삭제를 명시적 테스트로" → Task 4(첫), Task 6(마지막).
- "기존 14개 검증 유지" → Task 5 확인(본문 무변경).
- org-admin은 owner만 부여 → Task 4 step 4(promote).
- best-effort teardown → Task 6 try/catch.
- 문서 갱신 → Task 8.
- 라이브 검증 + 잔여 없음 → Task 7.

모든 spec 요구가 태스크에 매핑됨. 갭 없음.

**Placeholder scan:** 코드 단계는 모두 실제 코드 포함. Task 8만 서술형(문서 편집 지시)이며 변경 항목을 구체적으로 열거함.

**Type/이름 일관성:** 헬퍼 시그니처(`createUserViaUi`/`createOrganizationViaUi`/`assignMemberViaUi`/`promoteMemberToAdminViaUi`/`deleteOrganizationViaUi`/`deleteUserViaUi`)가 Task 4·6 호출과 일치. 변수명(`ownerUsername`/`orgAdminUsername`/`memberUsername`/`createdUserPassword`/`organizationName`/`createdUsernames`)이 Task 1 정의와 Task 4·6 사용에서 일치. 페이지 핸들(`rootPage`/`ownerPage`/`organizationAdminPage`/`memberPage`)은 기존 선언 재사용.
