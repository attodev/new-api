# Organization E2E Test Documentation

`web/default/e2e/organization.e2e.ts`

## 개요

조직(Organization) 기능의 브라우저 스모크 테스트. 총 **14개** 테스트를 serial 모드로 실행한다. `beforeAll`에서 4개의 브라우저 컨텍스트를 분리 생성하여 각 역할(root admin, org owner, org admin, member)로 미리 로그인해 두고, 각 테스트에서 해당 페이지를 재사용한다.

## 실행 방법

```bash
# 백엔드 수동 기동 (rate limit 비활성화 필수)
SQL_DSN="postgresql://..." \
  CRITICAL_RATE_LIMIT_ENABLE=false \
  GLOBAL_API_RATE_LIMIT_ENABLE=false \
  GLOBAL_WEB_RATE_LIMIT_ENABLE=false \
  ./new-api --port 3000

# 테스트 실행
cd web/default
E2E_BACKEND_URL=http://127.0.0.1:3000 npx playwright test e2e/organization.e2e.ts
```

## 환경 변수

| 변수 | 기본값 | 설명 |
|---|---|---|
| `E2E_ADMIN_USERNAME` | `admin` | 루트 관리자 계정 |
| `E2E_ADMIN_PASSWORD` | `atto1234` | 공통 비밀번호 |
| `E2E_ORGANIZATION_OWNER_USERNAME` | `atto.o` | 조직 owner 계정 |
| `E2E_ORGANIZATION_ADMIN_USERNAME` | `ato.a` | 조직 admin 계정 (org_id=2, org_role=admin) |
| `E2E_ORGANIZATION_MEMBER_USERNAME` | `atto.1` | 조직 member 계정 |
| `E2E_ORGANIZATION_PASSWORD` | `(adminPassword 동일)` | 조직 계정 비밀번호 |
| `E2E_BACKEND_URL` | `http://127.0.0.1:3100` | 백엔드 URL |
| `E2E_BASE_URL` | `http://127.0.0.1:3101` | 프론트엔드 URL (Playwright webServer) |

## 픽스처 구조

```
beforeAll
  ├── contexts[0] → rootPage         (admin 계정, 루트 관리자)
  ├── contexts[1] → ownerPage        (atto.o 계정, 조직 owner)
  ├── contexts[2] → organizationAdminPage  (ato.a 계정, 조직 admin)
  └── contexts[3] → memberPage       (atto.1 계정, 조직 member)
```

각 컨텍스트는 별도 쿠키·localStorage를 가지며 서로 간섭하지 않는다.

## 핵심 헬퍼

### `signInWithUi(page, username, password)`
UI로 로그인 후 localStorage의 `user`·`uid`를 `authStorage` WeakMap에 저장하고, `context.addInitScript`로 이후 모든 페이지 로드 전에 localStorage를 자동 복원한다.

### `openAuthenticatedPage(page, path = '/')`
지정 경로로 이동하면서 인증 상태를 보장한다.
1. 이동 전 `restoreAuth`로 localStorage 설정
2. `page.goto(path)` + `waitForLoadState('networkidle')`
3. `/sign-in`으로 리디렉트됐거나 localStorage가 비었으면 localStorage 재설정 후 재시도

> **주의:** 모든 페이지 이동은 `page.goto()` 직접 호출 대신 `openAuthenticatedPage`를 사용해야 한다. TanStack Router의 `beforeLoad` 가드가 매 페이지 로드마다 `getSelf()` API를 호출하며, 이 요청이 rate limit에 걸리면 sign-in으로 리디렉트된다.

## 테스트 목록

### 1. root admin can see organization management navigation and pages
**페이지:** `/` → 사이드바 확인 → `/organization/dashboard` → `/organization/subscriptions`
**대상:** `rootPage` (루트 관리자)

사이드바에 Organization 섹션의 5개 메뉴(Users, Dashboard, Usage Logs, Task Logs, Subscription)가 모두 노출되는지 확인한다. 이후 Dashboard, Subscription 링크를 클릭하여 URL과 페이지 제목(heading)이 올바른지 검증한다.

---

### 2. organization selection survives navigation between organization pages
**페이지:** `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/dashboard`
**대상:** `rootPage`

조직 선택 combobox의 선택값이 다른 조직 페이지로 이동한 뒤에도 유지되는지 확인한다. 조직이 없으면 `test.skip`으로 건너뛴다.

---

### 3. organization owner and admin can access organization management pages
**페이지:** `/` → `/organization` → `/organization/subscriptions`
**대상:** `ownerPage`, `organizationAdminPage` (각각 순회)

owner와 admin 역할 모두 사이드바에 Organization Users, Organization Subscription 링크가 보이고, `/organization`과 `/organization/subscriptions` 페이지에 직접 접근할 수 있는지 검증한다.

---

### 4. organization usage logs render for admins
**페이지:** `/organization/usage-logs/common`
**대상:** `rootPage`

Usage Logs 페이지가 올바르게 렌더링되는지 확인한다. Usage logs 텍스트, Search 버튼, Rows per page 텍스트가 모두 노출되어야 한다.

---

### 5. organization member cannot open management pages directly
**페이지:** `/` → `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/subscriptions`
**대상:** `memberPage`

member 역할은 사이드바에 Organization 관리 메뉴가 전혀 없고, 각 관리 페이지 URL로 직접 접근하면 `/403` 또는 `/sign-in`으로 리디렉트됨을 확인한다.

---

### 6. organization users table renders members for admin
**페이지:** `/organization`
**대상:** `organizationAdminPage`

조직 사용자 테이블 페이지 구조를 확인한다. "Organization Users" heading과 Username, Role, Status 컬럼 헤더가 모두 노출되어야 한다.

---

### 7. organization users search filters table results
**페이지:** `/organization`
**대상:** `organizationAdminPage`

검색 입력란에 `__no_match_xyzzy__`를 입력했을 때 "No data" empty state가 표시되는지 확인한다. (debounce + API 응답을 기다린다)

---

### 8. export button is visible for admin and triggers download
**페이지:** `/organization`
**대상:** `organizationAdminPage`

Export 버튼이 보이고, 클릭하면 `org-users*.xlsx` 파일 다운로드가 시작되는지 확인한다.

---

### 9. import button is visible for admin and opens dialog
**페이지:** `/organization`
**대상:** `organizationAdminPage`

Import 버튼이 보이고, 클릭하면 모달 dialog가 열리며, Escape 키로 닫히는지 확인한다.

---

### 10. add member section is visible for admin but not for member
**페이지:** `/organization`
**대상:** `organizationAdminPage`, `memberPage`

admin은 멤버 추가 섹션의 Username 입력란이 보이고, member는 `/organization` 접근 시 `/403` 또는 `/sign-in`으로 리디렉트됨을 동시에 검증한다.

---

### 11. owner sees all controls including role selector in table
**페이지:** `/organization`
**대상:** `ownerPage`

owner 역할로 접근했을 때 "Organization Users" heading과 Role 컬럼 헤더가 표시되는지 확인한다.

---

### 12. dashboard preset buttons change the chart range
**페이지:** `/organization/dashboard`
**대상:** `organizationAdminPage`

"Today", "7d", "30d" 프리셋 버튼을 차례로 클릭했을 때 에러 토스트가 나타나지 않는지 확인한다. (버튼이 화면에 없으면 건너뜀)

---

### 13. subscription page renders plan configuration form for admin
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

Subscription 페이지에 Plan configuration 폼이 올바르게 렌더링되는지 확인한다. "Plan configuration" 텍스트, "Plan title" 레이블, Create 버튼이 모두 노출되어야 한다.

---

### 14. subscription page create plan validates empty title
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

Plan title을 비운 채 Create 버튼을 클릭했을 때 "Plan title is required" 유효성 검사 토스트가 표시되는지 확인한다.

## 알려진 제약사항

### Rate Limit 비활성화 필수
백엔드는 반드시 아래 세 플래그를 모두 비활성화한 상태로 기동해야 한다. 미설정 시 `/api/user/self` 엔드포인트가 `GLOBAL_API_RATE_LIMIT`(기본 180 req/3분)에 걸려 테스트 도중 sign-in 리디렉트가 발생한다. 이 제한은 모든 `/api/*` 라우트에 적용되는 전역 미들웨어(`middleware.GlobalAPIRateLimit()`)에서 온다.

```
CRITICAL_RATE_LIMIT_ENABLE=false
GLOBAL_API_RATE_LIMIT_ENABLE=false
GLOBAL_WEB_RATE_LIMIT_ENABLE=false
```

`playwright.config.ts`의 `webServer.env`에도 이미 설정되어 있으나, `reuseExistingServer: true`로 인해 이미 기동된 서버를 재사용할 경우 해당 서버의 환경 변수가 적용된다.

### Snap Chromium (Ubuntu 26.04+)
Playwright 내장 Chromium 대신 시스템 Chromium(`/usr/bin/chromium-browser`)을 사용한다. `playwright.config.ts`의 `executablePath`에 설정되어 있으며, 환경 변수 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH`로 재정의할 수 있다.

ffmpeg를 지원하지 않으므로 `video: 'off'`로 설정되어 있다.

### TanStack Router 세션 재검증
TanStack Router의 `_authenticated` 레이아웃 라우트는 모듈 수준 변수 `sessionVerified`로 세션 검증을 1회만 수행한다. 그러나 `page.goto()`는 항상 완전한 페이지 리로드를 유발하여 모듈이 재초기화되므로, 모든 페이지 이동마다 `getSelf()` API가 한 번씩 호출된다. 테스트에서 페이지 이동은 반드시 `openAuthenticatedPage()`를 통해야 한다.
