# Organization E2E Test Documentation

`web/e2e/organization.e2e.ts` (team `web/default/e2e/organization.e2e.ts`에서 이식)

## 개요

조직(Organization) 기능의 브라우저 스모크 테스트. 총 **19개 (프로비저닝 setup 1 + 기능 17 + 정리 teardown 1)** 테스트를 serial 모드로 실행한다. `beforeAll`에서 4개의 브라우저 컨텍스트를 분리 생성하지만 **root(루트 관리자)만** 로그인한다. owner / org admin / member 계정과 테스트 조직은 첫 번째 setup 테스트에서 **UI를 통해** 런타임에 생성되며, 실행마다 충돌하지 않도록 run-scoped 고유 이름을 사용한다(`runId = Date.now().toString(36)`, 사용자명 `e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`, 조직명 `E2E Org <runId>`). 생성된 계정은 admin 비밀번호를 그대로 재사용한다.

## 실행 방법

```bash
cd web
bun install
bun run e2e:install   # Playwright Chromium (최초 1회)
E2E_ADMIN_USERNAME=root E2E_ADMIN_PASSWORD=12345678 bun run e2e
```

`playwright.config.ts`의 `webServer`가 두 서버를 새로 띄운다. 이미 떠 있는 서버를 재사용하려면 `E2E_REUSE_SERVER=1`을 준다(기본은 재사용하지 않으므로 3100/3101 포트가 비어 있어야 한다).

- 백엔드: 저장소 루트에서 `go run . --port 3100` (rate limit 3종 비활성화). `SQLITE_PATH`가 없으면 SQLite 파일 `$TMPDIR/new-api-e2e.db`를 쓴다(저장소 밖). 테스트가 사용자를 만들고 지우므로, 상속된 `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING`은 `E2E_ALLOW_EXTERNAL_DB=1`이 아니면 백엔드 env에서 빈 값으로 덮어쓴다(Playwright가 `process.env`를 항상 합치므로).
- 프런트엔드: `web/`에서 `bun run dev -- --host 127.0.0.1 --port 3101 --strict-port` (rsbuild). `VITE_REACT_APP_SERVER_URL`을 백엔드 URL로 넘겨 `/api` 프록시 대상을 맞춘다.

빈 DB에는 root 계정이 없으므로 **최초 1회 `POST /api/setup`** 으로 root를 만들어야 한다. upstream 비밀번호 정책은 8–128자이므로 `123456` 같은 6자리 비밀번호는 setup에서 거부된다.

### 격리된 임시 백엔드로 실행 (권장: 공용 서버에 영향 없음)

```bash
# 1) 빈 SQLite로 rate limit 끈 백엔드 기동 (포트 3100). 바이너리는 저장소 루트에서 go build로 만든다.
rm -f "$TMPDIR/new-api-e2e.db"*
env -u SQL_DSN -u LOG_SQL_DSN \
  SQLITE_PATH="$TMPDIR/new-api-e2e.db" \
  CRITICAL_RATE_LIMIT_ENABLE=false \
  GLOBAL_API_RATE_LIMIT_ENABLE=false \
  GLOBAL_WEB_RATE_LIMIT_ENABLE=false \
  ./new-api --port 3100 &

# 2) root 계정 1개만 시드 (initial setup)
curl -s -X POST http://127.0.0.1:3100/api/setup \
  -H 'Content-Type: application/json' \
  -d '{"username":"root","password":"12345678","confirmPassword":"12345678"}'

# 3) 테스트 실행 (root/12345678은 E2E_ADMIN_* 기본값과 일치). 떠 있는 백엔드를 재사용한다.
cd web
E2E_REUSE_SERVER=1 bun run e2e
```

> setup만 끝낸 뒤 백엔드를 내리고 `bun run e2e`를 실행해도 된다. 그러면 webServer가 같은 `$TMPDIR/new-api-e2e.db`로 `go run .`을 띄운다.

> 19개 테스트 통과 후 DB에는 root 1명만 남고 조직·조직 플랜·조직 구독은 0개다(teardown 검증 완료).

## 환경 변수

| 변수                                  | 기본값                     | 설명                                                                                                                            |
| ------------------------------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `E2E_ADMIN_USERNAME`                  | `root`                     | 루트 관리자 계정 (DB에 선존재해야 함)                                                                                           |
| `E2E_ADMIN_PASSWORD`                  | `12345678`                 | 공통 비밀번호 (생성되는 모든 계정에 재사용, 8–128자)                                                                            |
| `E2E_BACKEND_URL`                     | `http://127.0.0.1:3100`    | 백엔드 URL                                                                                                                      |
| `E2E_BASE_URL`                        | `http://127.0.0.1:3101`    | 프런트엔드 URL (Playwright webServer)                                                                                           |
| `SQLITE_PATH`                         | `$TMPDIR/new-api-e2e.db?…` | Playwright가 띄우는 백엔드의 SQLite 경로 (`E2E_ALLOW_EXTERNAL_DB=1`과 `SQL_DSN`이 함께 있으면 그쪽이 우선)                      |
| `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` | (없음)                     | 시스템 Chromium 경로. 없으면 Playwright 번들 Chromium                                                                           |
| `E2E_REUSE_SERVER`                    | (없음)                     | `1`이면 이미 떠 있는 백엔드/프런트엔드 서버를 재사용. 기본은 3100/3101에 새로 띄운다                                            |
| `E2E_ALLOW_EXTERNAL_DB`               | (없음)                     | `1`이면 상속된 `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING`을 백엔드에 넘긴다. 기본은 빈 값으로 덮어써 실제 DB를 건드리지 않는다 |

## 픽스처 구조

```
beforeAll  (root만 로그인)
  ├── contexts[0] → rootPage               (root 계정, 루트 관리자)  ← 여기서 로그인
  ├── contexts[1] → ownerPage              (조직 owner)   ← 컨텍스트만 생성
  ├── contexts[2] → organizationAdminPage  (조직 admin)   ← 컨텍스트만 생성
  └── contexts[3] → memberPage             (조직 member)  ← 컨텍스트만 생성

setup 테스트 (#1)  (UI로 런타임 프로비저닝)
  ├── root가 e2e_owner_<runId> / e2e_admin_<runId> / e2e_member_<runId> 3계정 + E2E Org <runId> 조직 생성
  ├── owner 로그인 → 나머지 두 계정을 멤버로 추가 → 한 명을 org admin으로 승격
  └── org admin / member 각각 로그인
```

각 컨텍스트는 별도 쿠키를 가지며 서로 간섭하지 않는다.

## 핵심 헬퍼

### `signInWithUi(page, username, password)`

`/sign-in`에서 "Enter your username or email" / "Enter password" 입력란을 채우고 "Sign in"을 누른 뒤 `POST /api/user/login` 응답(`success: true`)과 `/dashboard` 이동을 기다린다. 페이지별 자격 증명을 저장해 재로그인에 쓴다.

upstream 인증은 localStorage에 아무것도 남기지 않는다. access token은 메모리에만 있고, 페이지를 새로 열 때마다 HttpOnly refresh 쿠키(`new_api_refresh`, `Path=/api/user/auth`)와 세션 힌트 쿠키(`new_api_has_session`)로 세션을 복원한다. 그래서 team 스펙의 `user`/`uid` localStorage 저장·복원 경로는 없앴고, 브라우저 컨텍스트의 쿠키가 세션 전부다.

### `openAuthenticatedPage(page, path = '/dashboard')`

지정 경로로 이동하고 `networkidle`까지 기다린다. `/sign-in`으로 튕기면 저장된 자격 증명으로 **UI 재로그인** 후 다시 이동한다(최대 3회). upstream의 `/`는 사이드바가 없는 공개 랜딩 페이지라 기본 경로는 콘솔 첫 화면인 `/dashboard`다.

### `completeSecureVerification(page, password)`

upstream은 사용자 삭제 같은 민감한 관리 작업 전에 step-up 인증 대화상자("Verify to delete user")를 띄운다. 2FA·passkey가 없으면 비밀번호 방식이므로 비밀번호를 넣고 "Verify"를 누른다.

### UI 프로비저닝/정리 헬퍼

setup·teardown 테스트가 사용하는 UI 기반 헬퍼다. 모두 시드 데이터 대신 실제 화면 조작으로 계정/조직을 생성·삭제한다.

- `createUserViaUi(page, user)` — `/users`의 "Add User" 드로어에서 신규 사용자를 생성한다.
- `createOrganizationViaUi(page, options)` — `/organization`에서 조직을 신규 생성한다(`E2E Org <runId>`).
- `assignMemberViaUi(page, username)` — `/organization` 멤버 추가 섹션("Add")으로 사용자를 조직 멤버로 등록한다.
- `promoteMemberToAdminViaUi(page, username)` — 조직 사용자 테이블의 Role 셀렉터로 멤버를 org admin으로 승격한다.
- `removeMemberFromOrganizationViaUi(page, username)` — 행 선택 → "Remove from organization" → "Save".
- `deleteOrganizationViaUi(page, name)` — 조직을 삭제한다.
- `deleteUserViaUi(page, username)` — 사용자를 삭제한다(step-up 인증 포함).

### devtools 오버레이 숨김 init script

`beforeAll`은 `addInitScript`로 TanStack Router·Query devtools 오버레이 버튼을 숨긴다. dev 서버가 화면 하단 모서리에 렌더링하는 이 버튼들이 푸터/드로어 버튼 클릭을 가로채기 때문이다.

## 테스트 목록

### 1. admin provisions organization, users, and roles via UI

**페이지:** 사용자 관리 → `/organization`
**대상:** `rootPage`, `ownerPage`, `organizationAdminPage`, `memberPage` (setup)

setup 테스트. root가 UI로 3개 계정(`e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`)과 조직(`E2E Org <runId>`)을 생성한다. 이후 owner가 로그인해 나머지 두 계정을 조직 멤버로 추가하고 한 명을 org admin으로 승격한다. 마지막으로 org admin과 member가 각각 로그인하여 이후 테스트에서 쓸 인증 상태를 갖춘다.

---

### 2. root admin can see organization management navigation and pages

**페이지:** `/dashboard` → 사이드바 확인 → `/organization/dashboard` → `/organization/subscriptions`
**대상:** `rootPage` (루트 관리자)

사이드바에 Organization 섹션의 5개 메뉴(Users, Dashboard, Usage Logs, Task Logs, Subscription)가 모두 노출되는지 확인한다. 이후 Dashboard, Subscription 링크를 클릭하여 URL과 페이지 제목(heading)이 올바른지 검증한다.

---

### 3. organization selection survives navigation between organization pages

**페이지:** `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/dashboard`
**대상:** `rootPage`

조직 선택 combobox의 선택값이 다른 조직 페이지로 이동한 뒤에도 유지되는지 확인한다. 조직이 없으면 `test.skip`으로 건너뛴다.

---

### 4. organization owner and admin can access organization management pages

**페이지:** `/dashboard` → `/organization` → `/organization/subscriptions`
**대상:** `ownerPage`, `organizationAdminPage` (각각 순회)

owner와 admin 역할 모두 사이드바에 Organization Users, Organization Subscription 링크가 보이고, `/organization`과 `/organization/subscriptions` 페이지에 직접 접근할 수 있는지 검증한다.

---

### 5. organization usage logs render for admins

**페이지:** `/organization/usage-logs/common`
**대상:** `rootPage`

Usage Logs 페이지가 올바르게 렌더링되는지 확인한다. Usage logs 텍스트, Search 버튼, Rows per page 텍스트가 모두 노출되어야 한다.

---

### 6. organization member cannot open management pages directly

**페이지:** `/dashboard` → `/organization/dashboard` → `/organization/usage-logs/common` → `/organization/subscriptions`
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

### 19. admin removes organization members, organization, and users via UI

**페이지:** `/organization` → 사용자 관리
**대상:** `ownerPage`(멤버 제거), `rootPage`(조직·계정 삭제) (teardown, best-effort)

teardown 테스트. setup에서 생성한 리소스를 UI로 정리한다. **순서가 중요하다:**

1. **owner가 조직에서 멤버 제거** — 조직에서의 멤버 추가/제거는 owner/admin만 가능하므로 `ownerPage`로 수행한다. owner가 아닌 두 멤버(`e2e_admin_<runId>` / `e2e_member_<runId>`)의 행 체크박스를 선택하고 "Remove from organization" → "Save"로 멤버십을 해제한다(`organization_id`를 0으로 초기화). 백엔드는 owner가 아닌 멤버가 남아 있는 조직의 삭제를 거부하므로 이 단계가 선행돼야 한다.
2. **root가 조직 삭제** — `E2E Org <runId>`. 이때 백엔드(`model.DeleteOrganization`)가 해당 조직의 구독 플랜·사용자 구독을 cascade 삭제하고 owner의 멤버십을 정리한다.
3. **root가 계정 삭제** — `e2e_admin_<runId>` / `e2e_member_<runId>` / `e2e_owner_<runId>` 3계정을 모두 삭제한다(삭제마다 step-up 비밀번호 인증).

각 단계는 best-effort(try/catch)로 동작하며, 멤버 제거는 멤버에 활성 구독이 있어도 가능하다(구독은 조직 삭제 cascade로 정리됨).

## 자원 생명주기 및 정리 (Cleanup)

테스트가 만드는 모든 자원은 **오직 UI를 통해서만** 생성·삭제된다. 테스트 코드에는 DB·SQL 직접 접근이나 API 직접 호출이 일절 없다(`afterAll`도 브라우저 컨텍스트만 닫고 DB는 건드리지 않는다).

### 생성 → 사용 → 삭제 흐름

```
setup 테스트(#1)            기능 테스트(#2~#18)            teardown 테스트(#19)
─────────────────         ───────────────────          ──────────────────────────
root가 UI로                 setup이 만든 계정·조직을        ① owner가 조직에서 멤버 제거
 · 사용자 3명 생성            그대로 재사용해 권한·렌더링         (e2e_admin, e2e_member)
 · 조직 1개 생성              ·상호작용 검증                 ② root가 조직 삭제 (cascade)
owner가 UI로                                              ③ root가 계정 3개 삭제
 · 멤버 2명 추가                                              (admin, member, owner)
 · 1명 org admin 승격
```

### 삭제 순서가 중요한 이유 (권한 + 백엔드 제약)

- 조직에서의 **멤버 추가/제거는 owner/admin만** 할 수 있다(`RemoveOrganizationUserMembership`는 org admin 권한을 요구). 따라서 멤버 제거 단계는 root가 아니라 **`ownerPage`(조직 소유자)** 로 수행한다.
- `model.DeleteOrganization`은 **owner가 아닌 활성 멤버가 남아 있는 조직의 삭제를 거부**한다(`"organization has N non-owner members, remove them first"`).

그래서 teardown은 반드시 다음 순서를 지킨다.

1. **owner가 조직에서 멤버 제거** (`ownerPage`) — `e2e_admin_<runId>`, `e2e_member_<runId>`의 행을 선택하고 "Remove from organization" → "Save". 멤버의 `organization_id`만 0으로 초기화되며, 멤버에 활성 구독이 있어도 제거된다.
2. **root가 조직 삭제** (`rootPage`) — `E2E Org <runId>`. 이제 non-owner 멤버 0명 → owner는 멤버 카운트에서 제외되므로 삭제 가능. 이때 백엔드가 해당 조직의 구독 플랜·사용자 구독을 cascade 삭제하고 owner 멤버십을 정리한다.
3. **root가 계정 삭제** (`rootPage`) — `e2e_admin_<runId>` / `e2e_member_<runId>` / `e2e_owner_<runId>` 3계정.

각 단계는 `try/catch`로 감싼 **best-effort**다. 앞 단계가 실패해도 나머지 정리를 계속 진행하며, 실패는 stderr로 로그를 남긴다.

> 이전에는 멤버의 **계정 자체를 삭제**해 멤버십을 간접적으로 없앴으나, 실제 운영 흐름(멤버 제거는 owner/admin가 수행)에 맞춰 **명시적 멤버 제거 → 조직 삭제 → 계정 삭제** 순으로 변경했다.

### 조직 삭제는 토스트가 아니라 "카드 사라짐"으로 검증

`deleteOrganizationViaUi`는 성공 토스트 대신 **조직 카드가 목록에서 사라지는지**(`expect(card).toHaveCount(0)`)로 삭제 성공을 단언한다. 백엔드 거부가 있으면 테스트가 분명히 실패하게 하기 위해서다.

### 충돌 방지: run-scoped 고유 이름

모든 자원은 `runId = Date.now().toString(36)` 기반 고유 이름을 쓴다. 따라서 **중간 테스트 실패로 teardown이 건너뛰어져 잔여물이 남더라도, 다음 실행은 새 `runId`를 쓰므로 충돌하지 않는다.** (serial 모드에서는 한 테스트가 실패하면 이후 테스트가 모두 "did not run"으로 건너뛰어지므로, teardown(#19)도 실행되지 않을 수 있다는 점에 유의.)

### 사용자명 길이 제약

백엔드 `User.Username`은 `max=20` 검증이 있다. 따라서 `runId`는 13자리 ms 타임스탬프 대신 `Date.now().toString(36)`(약 8자)로 줄여, 가장 긴 사용자명 `e2e_member_<runId>`가 20자를 넘지 않도록 했다.

## 알려진 제약사항

### Rate Limit 비활성화 필수

백엔드는 반드시 아래 세 플래그를 모두 비활성화한 상태로 기동해야 한다. 미설정 시 전역 API rate limit(`GLOBAL_API_RATE_LIMIT`)에 걸려 테스트 도중 sign-in 리디렉트가 발생할 수 있다.

```
CRITICAL_RATE_LIMIT_ENABLE=false
GLOBAL_API_RATE_LIMIT_ENABLE=false
GLOBAL_WEB_RATE_LIMIT_ENABLE=false
```

`playwright.config.ts`의 `webServer.env`에도 이미 설정되어 있으나, `E2E_REUSE_SERVER=1`로 이미 기동된 서버를 재사용할 경우 해당 서버의 환경 변수가 적용된다.

### Chromium

기본은 Playwright 번들 Chromium이다(`bun run e2e:install`). 번들 Chromium을 쓸 수 없는 환경(예: Ubuntu 26.04+ snap Chromium)에서는 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH`로 시스템 Chromium을 지정한다. `video: 'off'`로 설정되어 있다.

### 세션 복원

`page.goto()`는 매번 전체 페이지를 다시 로드하므로, 매 이동마다 refresh 쿠키로 세션을 복원하는 요청이 나간다. 테스트에서 인증이 필요한 페이지 이동은 `openAuthenticatedPage()`를 통한다.

### admin 계정 선존재 필수

조직·역할 계정은 setup 테스트에서 UI로 생성하지만, 그 작업을 수행할 root 계정만큼은 백엔드 DB에 미리 존재해야 한다(`E2E_ADMIN_USERNAME` / `E2E_ADMIN_PASSWORD`, `POST /api/setup`). 이 계정이 없으면 setup 테스트가 로그인 단계에서 실패한다.

### teardown 삭제 순서 (백엔드 제약)

teardown 테스트는 반드시 **owner가 아닌 멤버 → 조직 → 계정** 순으로 삭제해야 한다. 백엔드는 owner가 아닌 멤버가 남아 있는 조직의 삭제를 거부하기 때문이다.
