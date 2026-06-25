# E2E Organization 테스트 자체 프로비저닝 설계

> 작성일: 2026-06-24
> 대상: `web/default/e2e/organization.e2e.ts`
> 관련 문서: `web/default/e2e/organization.e2e.md`, `web/default/e2e/E2E_TEST_ANALYSIS.md`

## 1. 배경 / 문제

현재 E2E 테스트는 DB에 미리 시드된 고정 계정·조직에 의존한다.

- `E2E_ADMIN_USERNAME` (admin / root)
- `E2E_ORGANIZATION_OWNER_USERNAME` (atto.o)
- `E2E_ORGANIZATION_ADMIN_USERNAME` (ato.a, org_id=2)
- `E2E_ORGANIZATION_MEMBER_USERNAME` (atto.1)

이 시드 계정·조직이 환경마다 달라지면 테스트가 곧바로 깨진다. 재현성과 이식성이 없다.

## 2. 목표

- **root(admin) 계정만 기본값(시드)에 의존**한다.
- 테스트에 필요한 나머지 계정(owner / org-admin / member)과 조직은 **테스트 실행 중 UI를 통해 직접 생성**한다.
- 테스트 종료 전 생성한 모든 리소스를 **UI를 통해 삭제**한다.
- 생성/삭제는 **명시적인 setup/teardown 테스트 케이스**로 두어, 생성·삭제 UI 플로우 자체도 검증 대상에 포함한다.
- 기존 14개 테스트의 검증 로직(역할별 권한·렌더링)은 그대로 유지하고, 픽스처(계정/조직)만 동적 값으로 교체한다.

### 비목표 (YAGNI)

- API 직접 호출을 통한 셋업/티어다운 — 금지. **모든 작업은 UI로** 수행한다.
- Playwright global-setup/teardown, storageState 파일 분리 — 사용하지 않는다(생성/삭제를 가시적 테스트로 두라는 요구와 상충).
- 멤버 추가/역할변경 외의 신규 기능 검증 추가 — 범위 밖.

## 3. 선택한 접근

단일 파일(`organization.e2e.ts`) 내 serial 모드 유지. 첫 테스트 = 생성, 마지막 테스트 = 삭제, 중간 14개는 픽스처만 교체.

대안으로 검토한 Playwright global-setup + storageState 분리 방식은 생성/삭제를 테스트에서 감추게 되어 요구사항과 어긋나므로 채택하지 않았다.

## 4. UI 플로우 근거 (코드 확인 결과)

검증한 소스 파일과 핵심 사실:

| 작업 | 위치 | 권한 | 핵심 셀렉터/레이블 |
|---|---|---|---|
| 사용자 생성 | `/users` → "Add User" → drawer | root/admin | placeholder `Enter username`, `Enter display name`, `Enter password (min 8 characters)`; role Select `Common User`/`Admin`; 제출 `Save changes` |
| 사용자 삭제 | `/users` 행 액션 메뉴 | root | 트리거 sr-only `Open menu`; 항목 `Delete`; 확인 다이얼로그 제목 `Are you sure?`, 확인 버튼 `Delete` |
| 조직 생성 | `/organization` "Create Organization" 폼 | **root만** | placeholder `Organization Name`/`Description`/`Initial organization quota`; Owner Select placeholder `Owner User`(항목 `username #id`); 제출 `Create`; 성공 토스트 `Organization created` |
| 조직 삭제 | `/organization` "Manage organization quota" 카드 | **root만** | 카드 `Delete` 버튼 → AlertDialog 제목 `Delete Organization`, 확인 `Delete`; 성공 토스트 `Organization deleted` |
| 멤버 배정 | `/organization` "Assign Organization Member" | org-admin/owner | placeholder `Username`; 추가 버튼 텍스트는 **하드코딩 한글 `추가`**(`organization-users-table.tsx:710`); 성공 토스트 `Organization member assigned` |
| 역할 변경 | 조직 사용자 테이블 Role 컬럼 Select | **owner만** | 옵션 `member`/`admin`(ASSIGNABLE_ROLES); 변경 후 상단 `Save`; 성공 토스트 `Changes saved` |

근거 파일:
- `web/default/src/features/users/components/users-mutate-drawer.tsx`
- `web/default/src/features/users/components/users-primary-buttons.tsx`
- `web/default/src/features/users/components/users-delete-dialog.tsx`
- `web/default/src/features/users/components/data-table-row-actions.tsx`
- `web/default/src/features/organizations/components/organization-users-table.tsx`
- `web/default/src/lib/organization-roles.ts`

### 제약 사항 (반드시 준수)

1. **조직 생성/삭제는 root만** 가능 → 해당 단계는 `rootPage`로 수행.
2. **org-admin 역할은 owner만 부여 가능** → owner가 로그인하여 멤버를 admin으로 승격. 신규 사용자는 배정 시 항상 `member`로 들어가며, 이후 owner가 Role Select로 `admin`으로 변경 후 Save.
3. **멤버 배정은 username 정확 매칭 + 후보 목록 의존**: `handleAssignUser`는 `getAssignableOrganizationUsers` 후보 중 username이 정확히 일치하는 사용자만 배정한다. 신규 Common User(org_id=0, role<admin)는 후보에 포함되어야 한다. 미포함 시 `User not found` 토스트로 실패.
4. **조직 owner 후보 필터**: root의 Owner Select 후보는 `role < ADMIN && organization_id === 0`인 사용자. 신규 Common User는 충족.
5. 비밀번호는 8~20자 (`Enter password (min 8 characters)`); 기본 adminPassword(`atto1234`, 8자)를 재사용.

## 5. 상세 설계

### 5.1 환경 변수 변경

| 변수 | 변경 |
|---|---|
| `E2E_ADMIN_USERNAME` | 유지 (기본 `admin`) |
| `E2E_ADMIN_PASSWORD` | 유지 (기본 `atto1234`) |
| `E2E_ORGANIZATION_OWNER_USERNAME` | **제거** |
| `E2E_ORGANIZATION_ADMIN_USERNAME` | **제거** |
| `E2E_ORGANIZATION_MEMBER_USERNAME` | **제거** |
| `E2E_ORGANIZATION_PASSWORD` | **제거** (생성 계정은 adminPassword 재사용) |

### 5.2 런 스코프 유니크 식별자

테스트 파일 상단에서 1회 생성(테스트 파일이므로 `Date.now()` 사용 가능):

```ts
const runId = `${Date.now()}`
const ownerUsername = `e2e_owner_${runId}`
const orgAdminUsername = `e2e_admin_${runId}`
const memberUsername = `e2e_member_${runId}`
const createdUserPassword = adminPassword
const organizationName = `E2E Org ${runId}`
const createdUsernames = [ownerUsername, orgAdminUsername, memberUsername]
```

충돌·잔여 데이터 방지 목적. 셋업 실패로 잔여가 생겨도 다음 런은 새 식별자를 쓴다.

### 5.3 beforeAll (축소)

- 4개 BrowserContext + Page 생성 (root / owner / orgAdmin / member).
- **root(admin)만 `signInWithUi`로 로그인.**
- owner/orgAdmin/member 페이지는 계정 생성 전이므로 로그인하지 않는다(첫 테스트 이후 로그인).

### 5.4 테스트 1 — `admin provisions organization, users, and roles via UI` (setup)

serial 모드의 첫 케이스. 단계:

1. `rootPage` `/users` 이동 → 3명 생성:
   - "Add User" 클릭 → drawer 오픈
   - Username(`Enter username`), Display Name(선택), Role=`Common User`, Password(`createdUserPassword`) 입력
   - `Save changes` → 성공 토스트(`User created` 계열) 또는 drawer 닫힘 확인
   - 3회 반복 (owner, orgAdmin, member)
2. `rootPage` `/organization` → "Create Organization":
   - `Organization Name` = organizationName
   - Owner Select에서 `${ownerUsername} #id` 항목 선택
   - `Create` → `Organization created` 토스트
3. `ownerPage` 로그인(`signInWithUi(ownerPage, ownerUsername, createdUserPassword)`).
4. `ownerPage` `/organization` "Assign Organization Member":
   - `Username` 입력 = orgAdminUsername → 추가(`추가`) → `Organization member assigned`
   - `Username` 입력 = memberUsername → 추가 → `Organization member assigned`
5. `ownerPage` 테이블에서 orgAdmin 행의 Role Select → `admin` 선택 → 상단 `Save` → `Changes saved`.
6. `orgAdminPage`, `memberPage` 로그인(이제 계정 존재).

각 단계는 토스트/상태로 성공을 단언한다. 이 테스트가 실패하면 후속 테스트가 의미 없으므로 serial 모드에서 자연히 차단된다.

### 5.5 테스트 2–15 — 기존 14개 (픽스처만 교체)

기존 검증 로직 유지. 변경점:
- 하드코딩된 username/조직 기대값을 동적 변수로 교체.
- 페이지 핸들(`ownerPage`/`organizationAdminPage`/`memberPage`)은 테스트 1에서 로그인된 동일 객체 재사용.
- "organization users table renders members" 등에서 기대하는 멤버 username은 `createdUsernames` 기반으로 확인 가능(필요 시).
- 조직 선택 combobox 테스트(기존 #2)는 동적 organizationName으로 대응.

### 5.6 테스트 16 — `admin removes organization and users via UI` (teardown)

serial 모드 마지막 케이스. **best-effort**(각 단계 try/catch):

1. `rootPage` `/organization` "Manage organization quota" → organizationName 카드의 `Delete` → AlertDialog `Delete Organization` 확인 `Delete` → `Organization deleted`.
2. `rootPage` `/users` → 각 username에 대해:
   - 검색(`Filter by username, name or email...`)으로 행 필터
   - 행 액션 메뉴(`Open menu`) → `Delete` → `Are you sure?` 다이얼로그 확인 `Delete`
3. 일부 단계 실패해도 나머지 정리를 계속 진행하도록 개별 try/catch로 감싼다.

> afterAll에서 컨텍스트를 닫기 전에 이 테스트가 실행되어야 하므로 teardown은 afterAll이 아닌 **마지막 테스트 케이스**로 둔다(요구사항과 일치).

### 5.7 헬퍼 추가/변경

기존 `signInWithUi`, `restoreAuth`, `openAuthenticatedPage` 유지. 추가할 UI 헬퍼(파일 내부 함수):

- `createUserViaUi(page, { username, password, displayName? })`
- `createOrganizationViaUi(page, { name, ownerUsername })`
- `assignMemberViaUi(page, username)`
- `promoteMemberToAdminViaUi(page, username)`
- `deleteOrganizationViaUi(page, name)`
- `deleteUserViaUi(page, username)`

각 헬퍼는 단일 책임을 가지며 토스트/상태로 완료를 확인한다.

## 6. 테스트/검증 방법

```bash
cd web/default
E2E_BACKEND_URL=http://127.0.0.1:3000 npx playwright test e2e/organization.e2e.ts
```

- rate limit 3종 비활성화 백엔드 필요(기존과 동일).
- 성공 기준: 16개 테스트 전부 통과, 그리고 종료 후 DB에 잔여 `e2e_*` 계정/조직이 없어야 한다(teardown 검증).

## 7. 위험 및 완화

| 위험 | 완화 |
|---|---|
| owner 멤버 배정 시 후보 목록에 신규 유저 미포함 → `User not found` | 신규 유저는 Common User + org_id=0이라 후보 조건 충족. 실패 시 토스트 단언에서 즉시 드러남 |
| 한글 하드코딩 버튼(`추가`) 텍스트 변경 가능성 | 코드 기준 정확 매칭. 추후 i18n화되면 셀렉터 갱신 필요(문서에 명시) |
| 셋업 일부 실패 후 잔여 데이터 | 런 스코프 유니크 식별자로 다음 런 충돌 회피 + best-effort teardown |
| Base UI Select(역할/Owner) 상호작용 | 트리거 클릭 후 옵션 텍스트로 선택. 옵션 텍스트는 role 값(`admin`/`member`) 또는 `username #id` |
| serial 의존성: 테스트 1 실패 시 연쇄 실패 | 의도된 동작(셋업 없이는 검증 무의미). 실패 지점이 명확해짐 |

## 8. 문서 갱신

구현 후 `web/default/e2e/organization.e2e.md` 및 `E2E_TEST_ANALYSIS.md`의 환경 변수/픽스처/테스트 목록 섹션을 새 구조(16개 테스트, 자체 프로비저닝)에 맞게 갱신한다.
