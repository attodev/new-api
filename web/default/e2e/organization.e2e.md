# Organization E2E Test Documentation

`web/default/e2e/organization.e2e.ts`

## 개요

조직(Organization) 기능의 브라우저 스모크 테스트. 총 **19개 (프로비저닝 setup 1 + 기능 17 + 정리 teardown 1)** 테스트를 serial 모드로 실행한다. `beforeAll`에서 4개의 브라우저 컨텍스트를 분리 생성하지만 **root(루트 관리자)만** 로그인한다. owner / org admin / member 계정과 테스트 조직은 첫 번째 setup 테스트에서 **UI를 통해** 런타임에 생성되며, 실행마다 충돌하지 않도록 run-scoped 고유 이름을 사용한다(`runId = Date.now().toString(36)`, 사용자명 `e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`, 조직명 `E2E Org <runId>`). 생성된 계정은 admin 비밀번호를 그대로 재사용한다.

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

### 격리된 임시 백엔드로 실행 (권장: 공용 서버에 영향 없음)

공용 개발 서버(예: rate limit이 켜진 :3000)에 영향을 주지 않으려면, **빈 SQLite DB로 일회용 백엔드**를 띄워 검증하는 방식을 권장한다. 빈 DB로 기동하면 setup 화면(`POST /api/setup`)으로 admin 계정만 만들어 두면 되고, 나머지 계정·조직은 테스트가 UI로 생성·삭제하므로 격리·재현성이 보장된다.

```bash
# 1) 빈 SQLite로 rate limit 끈 백엔드 기동 (포트 3100)
rm -f /tmp/e2e/one-api.db
env -u SQL_DSN -u LOG_SQL_DSN \
  SQLITE_PATH=/tmp/e2e/one-api.db \
  CRITICAL_RATE_LIMIT_ENABLE=false \
  GLOBAL_API_RATE_LIMIT_ENABLE=false \
  GLOBAL_WEB_RATE_LIMIT_ENABLE=false \
  ./new-api --port 3100 &

# 2) admin 계정 1개만 시드 (initial setup)
curl -s -X POST http://127.0.0.1:3100/api/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"atto1234","confirmPassword":"atto1234"}'

# 3) 테스트 실행 (admin/atto1234는 E2E_ADMIN_* 기본값과 일치)
cd web/default
E2E_BACKEND_URL=http://127.0.0.1:3100 npx playwright test e2e/organization.e2e.ts
```

> 19개 테스트 통과 후 백엔드 API로 확인하면 `e2e_*` 사용자 0개, 조직 0개로 **DB가 깨끗하게 남는다**(teardown 검증 완료). 생성한 구독 플랜·사용자 구독도 조직 삭제 cascade로 함께 정리된다.

## 환경 변수

| 변수 | 기본값 | 설명 |
|---|---|---|
| `E2E_ADMIN_USERNAME` | `admin` | 루트 관리자 계정 (DB에 선존재해야 함) |
| `E2E_ADMIN_PASSWORD` | `atto1234` | 공통 비밀번호 (생성되는 모든 계정에 재사용) |
| `E2E_BACKEND_URL` | `http://127.0.0.1:3100` | 백엔드 URL |
| `E2E_BASE_URL` | `http://127.0.0.1:3101` | 프론트엔드 URL (Playwright webServer) |

> **참고:** 이전의 `E2E_ORGANIZATION_OWNER_USERNAME` / `E2E_ORGANIZATION_ADMIN_USERNAME` / `E2E_ORGANIZATION_MEMBER_USERNAME` / `E2E_ORGANIZATION_PASSWORD` 환경 변수는 **제거되었다**. owner / admin / member 계정과 테스트 조직은 더 이상 시드에 의존하지 않고, setup 테스트에서 UI로 런타임에 생성된다. 이름은 run-scoped 고유값(`e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`, 조직명 `E2E Org <runId>`)이며 admin 비밀번호를 공유한다.

## 픽스처 구조

```
beforeAll  (root만 로그인)
  ├── contexts[0] → rootPage               (admin 계정, 루트 관리자)  ← 여기서 로그인
  ├── contexts[1] → ownerPage              (조직 owner)   ← 컨텍스트만 생성
  ├── contexts[2] → organizationAdminPage  (조직 admin)   ← 컨텍스트만 생성
  └── contexts[3] → memberPage             (조직 member)  ← 컨텍스트만 생성

setup 테스트 (#1)  (UI로 런타임 프로비저닝)
  ├── root가 e2e_owner_<runId> / e2e_admin_<runId> / e2e_member_<runId> 3계정 + E2E Org <runId> 조직 생성
  ├── owner 로그인 → 나머지 두 계정을 멤버로 추가 → 한 명을 org admin으로 승격
  └── org admin / member 각각 로그인
```

각 컨텍스트는 별도 쿠키·localStorage를 가지며 서로 간섭하지 않는다. `beforeAll`에서는 root만 로그인하고, 나머지 세 역할 계정·조직은 setup 테스트(#1)에서 UI로 생성·로그인된다.

## 핵심 헬퍼

### `signInWithUi(page, username, password)`
UI로 로그인 후 localStorage의 `user`·`uid`를 `authStorage` WeakMap에 저장하고, `context.addInitScript`로 이후 모든 페이지 로드 전에 localStorage를 자동 복원한다.

### `openAuthenticatedPage(page, path = '/')`
지정 경로로 이동하면서 인증 상태를 보장한다.
1. 이동 전 `restoreAuth`로 localStorage 설정
2. `page.goto(path)` + `waitForLoadState('networkidle')`
3. `/sign-in`으로 리디렉트됐거나 localStorage가 비었으면 재시도하며, 그래도 `/sign-in`에 머무르면 페이지별로 저장된 인증 정보(per-page stored credentials)로 **UI 전체 재로그인**을 수행한다.

> **주의:** 모든 페이지 이동은 `page.goto()` 직접 호출 대신 `openAuthenticatedPage`를 사용해야 한다. TanStack Router의 `beforeLoad` 가드가 매 페이지 로드마다 `getSelf()` API를 호출하며, 이 요청이 rate limit에 걸리면 sign-in으로 리디렉트된다.

### UI 프로비저닝/정리 헬퍼

setup·teardown 테스트가 사용하는 UI 기반 헬퍼다. 모두 시드 데이터 대신 실제 화면 조작으로 계정/조직을 생성·삭제한다.

- `createUserViaUi(page, username, password)` — 관리자 사용자 관리 화면에서 신규 사용자를 생성한다.
- `createOrganizationViaUi(page, name)` — 조직을 신규 생성한다(`E2E Org <runId>`).
- `assignMemberViaUi(page, username)` — `/organization` 멤버 추가 섹션으로 사용자를 조직 멤버로 등록한다.
- `promoteMemberToAdminViaUi(page, username)` — 조직 사용자 테이블의 Role 셀렉터로 멤버를 org admin으로 승격한다.
- `deleteOrganizationViaUi(page, name)` — 조직을 삭제한다.
- `deleteUserViaUi(page, username)` — 사용자를 삭제한다.

### devtools 오버레이 숨김 init script

`beforeAll`은 `addInitScript`로 TanStack Router·Query devtools 오버레이 버튼을 숨긴다. dev 서버가 화면 하단 모서리에 렌더링하는 이 버튼들이 푸터/드로어 버튼 클릭을 가로채기 때문이다.

## 테스트 목록

### 1. admin provisions organization, users, and roles via UI
**페이지:** 사용자 관리 → `/organization`
**대상:** `rootPage`, `ownerPage`, `organizationAdminPage`, `memberPage` (setup)

setup 테스트. root가 UI로 3개 계정(`e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`)과 조직(`E2E Org <runId>`)을 생성한다. 이후 owner가 로그인해 나머지 두 계정을 조직 멤버로 추가하고 한 명을 org admin으로 승격한다. 마지막으로 org admin과 member가 각각 로그인하여 이후 테스트에서 쓸 인증 상태를 갖춘다.

---

### 2. root admin can see organization management navigation and pages
**페이지:** `/` → 사이드바 확인 → `/organization/dashboard` → `/organization/subscriptions`
**대상:** `rootPage` (루트 관리자)

사이드바에 Organization 섹션의 5개 메뉴(Users, Dashboard, Usage Logs, Task Logs, Subscription)가 모두 노출되는지 확인한다. 이후 Dashboard, Subscription 링크를 클릭하여 URL과 페이지 제목(heading)이 올바른지 검증한다.

---

### 3. organization selection survives navigation between organization pages
**페이지:** `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/dashboard`
**대상:** `rootPage`

조직 선택 combobox의 선택값이 다른 조직 페이지로 이동한 뒤에도 유지되는지 확인한다. 조직이 없으면 `test.skip`으로 건너뛴다.

---

### 4. organization owner and admin can access organization management pages
**페이지:** `/` → `/organization` → `/organization/subscriptions`
**대상:** `ownerPage`, `organizationAdminPage` (각각 순회)

owner와 admin 역할 모두 사이드바에 Organization Users, Organization Subscription 링크가 보이고, `/organization`과 `/organization/subscriptions` 페이지에 직접 접근할 수 있는지 검증한다.

---

### 5. organization usage logs render for admins
**페이지:** `/organization/usage-logs/common`
**대상:** `rootPage`

Usage Logs 페이지가 올바르게 렌더링되는지 확인한다. Usage logs 텍스트, Search 버튼, Rows per page 텍스트가 모두 노출되어야 한다.

---

### 6. organization member cannot open management pages directly
**페이지:** `/` → `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/subscriptions`
**대상:** `memberPage`

member 역할은 사이드바에 Organization 관리 메뉴가 전혀 없고, 각 관리 페이지 URL로 직접 접근하면 `/403` 또는 `/sign-in`으로 리디렉트됨을 확인한다.

---

### 7. organization users table renders members for admin
**페이지:** `/organization`
**대상:** `organizationAdminPage`

조직 사용자 테이블 페이지 구조를 확인한다. "Organization Users" heading과 Username, Role, Status 컬럼 헤더가 모두 노출되어야 한다.

---

### 8. organization users search filters table results
**페이지:** `/organization`
**대상:** `organizationAdminPage`

검색 입력란에 `__no_match_xyzzy__`를 입력했을 때 "No data" empty state가 표시되는지 확인한다. (debounce + API 응답을 기다린다)

---

### 9. export button is visible for admin and triggers download
**페이지:** `/organization`
**대상:** `organizationAdminPage`

Export 버튼이 보이고, 클릭하면 `org-users*.xlsx` 파일 다운로드가 시작되는지 확인한다.

---

### 10. import button is visible for admin and opens dialog
**페이지:** `/organization`
**대상:** `organizationAdminPage`

Import 버튼이 보이고, 클릭하면 모달 dialog가 열리며, Escape 키로 닫히는지 확인한다.

---

### 11. add member section is visible for admin but not for member
**페이지:** `/organization`
**대상:** `organizationAdminPage`, `memberPage`

admin은 멤버 추가 섹션의 Username 입력란이 보이고, member는 `/organization` 접근 시 `/403` 또는 `/sign-in`으로 리디렉트됨을 동시에 검증한다.

---

### 12. owner sees all controls including role selector in table
**페이지:** `/organization`
**대상:** `ownerPage`

owner 역할로 접근했을 때 "Organization Users" heading과 Role 컬럼 헤더가 표시되는지 확인한다.

---

### 13. dashboard preset buttons change the chart range
**페이지:** `/organization/dashboard`
**대상:** `organizationAdminPage`

"Today", "7d", "30d" 프리셋 버튼을 차례로 클릭했을 때 에러 토스트가 나타나지 않는지 확인한다. (버튼이 화면에 없으면 건너뜀)

---

### 14. subscription page renders plan configuration form for admin
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

Subscription 페이지에 Plan configuration 폼이 올바르게 렌더링되는지 확인한다. "Plan configuration" 텍스트, "Plan title" 레이블, Create 버튼이 모두 노출되어야 한다.

---

### 15. subscription page create plan validates empty title
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

Plan title을 비운 채 Create 버튼을 클릭했을 때 "Plan title is required" 유효성 검사 토스트가 표시되는지 확인한다.

---

### 16. admin creates an organization subscription plan
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

조직 구독 플랜 **생성 성공 경로**를 검증한다. "Plan configuration" 폼에 Plan title(`E2E Plan <runId>`)과 Quota amount(10)를 입력하고(Duration은 기본 1 month) Create를 클릭하면 "Organization plan saved" 토스트가 뜨고, "Organization plans" 테이블에 해당 플랜이 나타나는지 확인한다.

---

### 17. admin assigns the organization subscription plan to a member
**페이지:** `/organization/subscriptions`
**대상:** `organizationAdminPage`

"Member plan assignment" 섹션에서 #16에서 만든 플랜을 멤버(`e2e_member_<runId>`)에게 **할당**한다. User/Plan 네이티브 셀렉트를 선택하고 Assign을 클릭하면 "Organization plan assigned" 토스트가 뜨고, 하단 멤버 구독 테이블에 해당 플랜 행이 나타나는지 확인한다.

> 정리: 플랜·구독은 조직에 종속되며, teardown(#19)에서 조직을 삭제하면 백엔드가 해당 조직의 구독 플랜·사용자 구독을 **함께 삭제(cascade)** 하므로 별도 정리가 필요 없다(`model.DeleteOrganization`).

---

### 18. root admin can open organization users page without org_id error
**페이지:** `/organization`
**대상:** `rootPage` (root/super admin)

**회귀 테스트.** root는 특정 조직에 소속되지 않으므로, 조직 사용자 목록을 보려면 대상 조직을 지정해야 한다. 이 테스트는 root로 `/organization`에 진입해 조직 선택기(native `<select>`)에서 setup 조직(`E2E Org <runId>`)을 선택한 뒤, **"organization_id is required" 오류가 나타나지 않고** 해당 조직의 멤버(owner 행)가 표시되는지 확인한다.

> 배경: 이전에는 `OrganizationUsersTable`이 root용 조직 선택기 없이 `organization_id` 없는 요청을 보내, 백엔드 `resolveOrganizationAdminTarget`가 root에게 요구하는 `organization_id`가 빠져 "organization_id is required" 오류가 발생했다. 컴포넌트에 root용 조직 선택기를 추가하고 모든 사용자 목록 요청에 `organization_id`를 전달하도록 수정했다.

---

### 19. admin removes organization and users via UI
**페이지:** `/organization` → 사용자 관리
**대상:** `rootPage` (teardown, best-effort)

teardown 테스트. setup에서 생성한 리소스를 UI로 정리한다. **삭제 순서가 중요하다** — 백엔드는 owner가 아닌 멤버가 남아 있는 조직의 삭제를 거부하므로, 먼저 owner가 아닌 두 멤버(`e2e_admin_<runId>` / `e2e_member_<runId>`)를 제거하고, 다음으로 조직(`E2E Org <runId>`)을 삭제한 뒤, 마지막으로 owner(`e2e_owner_<runId>`) 계정을 삭제한다. best-effort로 동작한다.

## 자원 생명주기 및 정리 (Cleanup)

테스트가 만드는 모든 자원은 **오직 UI를 통해서만** 생성·삭제된다. 테스트 코드에는 DB·SQL 직접 접근이나 API 직접 호출이 일절 없다(`afterAll`도 브라우저 컨텍스트만 닫고 DB는 건드리지 않는다).

### 생성 → 사용 → 삭제 흐름

```
setup 테스트(#1)            기능 테스트(#2~#18)            teardown 테스트(#19)
─────────────────         ───────────────────          ───────────────────
root가 UI로                 setup이 만든 계정·조직을        UI로 삭제 (best-effort)
 · 사용자 3명 생성            그대로 재사용해 권한·렌더링       · non-owner 멤버 2명
 · 조직 1개 생성              ·상호작용 검증                 · 조직
owner가 UI로                                              · owner
 · 멤버 2명 추가
 · 1명 org admin 승격
```

### 삭제 순서가 중요한 이유 (백엔드 제약)

`model.DeleteOrganization`은 **owner가 아닌 활성 멤버가 남아 있는 조직의 삭제를 거부**한다(`"organization has N non-owner members, remove them first"`). 따라서 teardown은 반드시 다음 순서를 지킨다.

1. **non-owner 멤버 삭제** — `e2e_admin_<runId>`, `e2e_member_<runId>` (사용자 자체를 삭제 → 멤버십도 사라짐)
2. **조직 삭제** — `E2E Org <runId>` (이제 non-owner 멤버 0명 → owner는 멤버 카운트에서 제외되므로 삭제 가능)
3. **owner 삭제** — `e2e_owner_<runId>`

각 단계는 `try/catch`로 감싼 **best-effort**다. 앞 단계가 실패해도 나머지 정리를 계속 진행하며, 실패는 `console.error`로 로그를 남긴다.

### 조직 삭제는 토스트가 아니라 "카드 사라짐"으로 검증

`deleteOrganizationViaUi`는 성공 토스트 대신 **조직 카드가 목록에서 사라지는지**(`expect(card).toHaveCount(0)`)로 삭제 성공을 단언한다. 현재 프론트엔드의 `handleDeleteOrganization`은 API가 `success:false`(예: 멤버가 남아 삭제 거부)를 200으로 반환해도 "Organization deleted" 성공 토스트를 띄우는 결함이 있어, 토스트만 믿으면 삭제 실패를 놓치기 때문이다. (이 프론트엔드 결함 수정은 별도 작업으로 분리됨.)

### 충돌 방지: run-scoped 고유 이름

모든 자원은 `runId = Date.now().toString(36)` 기반 고유 이름을 쓴다. 따라서 **중간 테스트 실패로 teardown이 건너뛰어져 잔여물이 남더라도, 다음 실행은 새 `runId`를 쓰므로 충돌하지 않는다.** (serial 모드에서는 한 테스트가 실패하면 이후 테스트가 모두 "did not run"으로 건너뛰어지므로, teardown(#19)도 실행되지 않을 수 있다는 점에 유의.)

### 사용자명 길이 제약

백엔드 `User.Username`은 `max=20` 검증이 있다. 따라서 `runId`는 13자리 ms 타임스탬프 대신 `Date.now().toString(36)`(약 8자)로 줄여, 가장 긴 사용자명 `e2e_member_<runId>`가 20자를 넘지 않도록 했다.

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

### admin 계정 선존재 필수
조직·역할 계정은 setup 테스트에서 UI로 생성하지만, 그 작업을 수행할 root(admin) 계정만큼은 백엔드 DB에 미리 존재해야 한다(`E2E_ADMIN_USERNAME` / `E2E_ADMIN_PASSWORD`). 이 계정이 없으면 setup 테스트가 로그인 단계에서 실패한다.

### teardown 삭제 순서 (백엔드 제약)
teardown 테스트는 반드시 **owner가 아닌 멤버 → 조직 → owner** 순으로 삭제해야 한다. 백엔드는 owner가 아닌 멤버가 남아 있는 조직의 삭제를 거부하기 때문이다.
