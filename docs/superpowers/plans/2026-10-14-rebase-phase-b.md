# upstream 재기반 단계 B(O3 조직 관리) 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `develop`(upstream 7de10f2c6 + 단계 A overlay) 위에 team의 조직 관리(조직/멤버/역할, 조직 지갑과 과금 귀속, 조직 구독, 대시보드·로그, 조직 지갑 결제 6종, 멤버 Excel 내보내기·가져오기)를 올려 `docs/manuals/organization-*.md` 시나리오를 통과시킨다.

**Architecture:** team의 조직 코드(모델 3파일, 컨트롤러 4파일, 서비스 2파일 분량, 프런트 16파일 + 라우트 7개)를 fork 전용 파일로 복사하고, upstream 파일에는 "훅 한 줄 + `// fork: O3`" 또는 구조체 필드만 둔다. 과금은 upstream `BillingSession`의 선택 지점에 `newOrganizationBillingSession` 훅 하나를 넣고 조직 전용 `FundingSource` 두 종을 fork 파일에 둔다. 충전 정산은 `creditTopUpTarget`이 `TopUp.TargetType`으로 사용자/조직 지갑을 가른다. 지갑 상한은 upstream의 `common.MaxWalletQuota`(2^53-1)다. 플랫폼 관리자용 `/api/organizations/*`는 casbin `organization` 리소스로 감싸고, `/api/organization/*`는 team의 조직 역할 검사를 유지한다.

**Tech Stack:** Go 1.26 (`GOWORK=off`), Gin, GORM 1.25.12, excelize, casbin(upstream authz), React 19 + Base UI + TanStack Router/Query + vitest, Playwright(e2e, 새로 추가).

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §2.1 O3, §3, §4.4, §5, §7, §9 결정 5. 조직 기능 자체의 의미는 team의 `docs/superpowers/specs/2026-06-09-organization-management-integrated-design.md`(§5 권한, §7 과금, §8 자동 갱신)를 따른다. 단계 A 기록 `docs/2026-10-14-rebase-phase-a.md` §7 "단계 B로 넘기는 것"도 이 계획이 소화한다.

## Global Constraints

- 브랜치 `develop`, worktree `/Users/molla/work/ai/new-api-rebase`. `team`(메인 체크아웃 `/Users/molla/work/ai/new-api`)은 읽기 전용 원본이다. team 파일은 `git show team:<path>`로 가져온다.
- upstream 파일 수정은 훅 한 줄 또는 구조체 필드 + `// fork: O3` 주석으로 제한하고, 모든 훅을 `docs/fork-overlays.md`에 적는다 (spec §3). 로직은 fork 전용 파일에 둔다.
- 지갑 수치: upstream 도메인. 사용자 지갑 API는 Go `int`(`model.IncreaseUserQuota(id, quota int, db bool)`, `DecreaseUserQuota`, `TryReserveUserQuota(id, quota int)`, `GetUserQuota`). 조직 모델은 team처럼 `int64`를 유지하고 경계에서 `int(...)`/`int64(...)`로 변환한다. 상한 상수는 `common.MaxWalletQuota`(team의 `common.MaxQuota` 5e14를 전부 교체; develop의 `common.MaxQuota`는 MaxInt32라 지갑에 쓰면 안 된다).
- 행 잠금은 `lockForUpdate(tx)`(`model/locking.go`)로 한다. `tx.Clauses(clause.Locking{...})`를 그대로 두지 않는다.
- DB 종류 판별은 `common.UsingMainDatabase(common.DatabaseTypePostgreSQL)` 등 upstream 함수로 한다(`common.UsingSQLite` 류 전역은 없다). 테스트는 `common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)`.
- JSON은 `common.Marshal/Unmarshal/DecodeJson`만. 웹 콘솔 오류는 `common.ApiErrorT(c, "English text", params...)` / `common.NewMessage(...)`(upstream `i18n/keys.go`에 조직 키를 추가하지 않는다; team의 `i18n.MsgOrg*`는 아래 표의 영문으로 바꾼다).
- Toss, 지갑 자동충전(`wallet_auto_recharge*`, 프리셋, cron), `UserBillingKey`, `web/classic`은 가져오지 않는다. 조직 코드에서 이들을 호출하는 줄은 제거한다.
- Go 파일에는 라이선스 헤더를 넣지 않는다(upstream 관례). `web/src`의 새 TS/TSX 파일은 upstream AGPL 헤더 블록을 붙인다.
- 프런트 게이트: `bun run typecheck`, `bun run test`(vitest), 건드린 파일에 새 lint/format 오류 없음(`bunx oxlint -c .oxlintrc.json <files>`, `oxfmt --check <files>`); upstream 기준선(`bun run lint/format:check/copyright:check/knip`)은 이미 실패한다. 새 영문 키는 `web/src/i18n/locales/en.json`에 넣고 `bun run i18n:sync`.
- 접근 토큰 라우트 레지스트리: 모든 `/api` 라우트는 `middleware/fork_access_token_routes.go`의 fork 맵에 정확히 한 번 선언하거나(권한 라우트는 `handlePermissionRoute`가 자동 선언) `router/access_token_scope_test.go`의 exempt/helper 목록에 있어야 한다(`TestAccessTokenRouteRulesCoverEveryDashboardRoute`).
- 커밋 메시지 끝: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. team에서 옮긴 코드는 `Ported-From: team <path>`.
- Go 테스트: `GOWORK=off go test ./<pkg>/ -run <Name>`; 컨트롤러 패키지 전체는 약 2분.

## 계획 단계에서 내린 결정 (spec §4.4를 구체화)

1. **조직 지갑 결제 6종 유지**: Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake(spec은 5종을 적었지만 team과 develop 모두 Waffo Pancake가 있다).
2. **환급은 상한을 검사하지 않는다**(team 정책 유지): `IncreaseOrganizationQuota`는 단순 가산. 새 잔액이 들어오는 길(충전 `creditOrganizationQuota`, root 수정, 가져오기)만 `MaxWalletQuota`를 지킨다.
3. **조직 필드는 사용자 캐시(UserBase/Lua)에 넣지 않는다**: 어떤 과금 경로도 캐시의 조직 필드를 읽지 않는다. `GetSelfUserById`의 컬럼 목록과 `buildSelfUserData`에만 추가한다.
4. **접근 토큰 스코프**: `/api/organization/*`는 기본 `accessTokenSessionRule`(대시보드 세션 전용). 예외: 조직 지갑 조회·충전은 사용자 지갑과 같은 `wallet:read`/`wallet:write`, `GET /api/organization/subscription/self`는 `profile:read`. `/api/organizations/*`는 casbin 권한 라우트라 `organization:read`/`organization:write` 스코프가 자동 선언된다.
5. **감사 로그**: `/api/organization/*`의 멤버·플랜·구독 변경 9개 핸들러와 `/api/organizations/*` 4개 핸들러는 `recordOrganizationAudit`로 `organization.<action>` 항목을 남긴다(upstream `auditContentTemplates`는 건드리지 않고 영문 content를 직접 만든다 — 단계 A의 `recordUserImportAudit` 패턴).
6. **충전 캐시 동기화**: upstream `syncCreditUserQuotaCache`는 사용자 target일 때만 호출한다(`syncCreditTopUpCache`). 조직 지갑에는 캐시가 없다.
7. **조직 로그 조회는 ClickHouse 안전하게** upstream `GetUserLogs`를 거울처럼 따른다(`logs.user_id IN ?` + `LogFilter.apply` + `clickHouseLogOrder`).
8. **조직 Excel 가져오기**는 단계 A의 사용자 가져오기 규칙을 따른다: 글로벌 역할은 절대 바꾸지 않고(조직 역할만), 새 사용자 행은 `common.Validate.Struct`, 잔액은 `MaxWalletQuota` 안에서, 요청당 감사 항목 1건.
9. **대시보드 세션 응답**: 로그인/`/api/user/self`에 `organization_id`, `organization_role`을 추가한다(사이드바·지갑 분기용).
10. **Playwright**는 `web/`에 devDependency로 추가하고 team의 조직 e2e를 이식하되, 이 장비에서 Chromium을 받을 수 없으면 실행은 기록만 남긴다.

## Review Focus

1. **조직 구독 사용자의 요청인데 조직 지갑이 비어 있음** — 플랜 한도가 남아 있어도 요청은 403으로 실패하고 플랜·지갑·개인 잔액 어느 것도 줄지 않아야 한다. → Task 3 `TestOrganizationSubscriptionBillingRejectsWhenOrganizationWalletInsufficient`.
2. **조직 지갑 충전이 상한(2^53-1)을 넘기는 콜백 두 건이 동시에 도착** — 하나만 성공하고 다른 하나는 주문을 pending으로 남긴 채 상한 오류를 내야 한다. → Task 2 `TestCreditTopUpTargetOrganizationStopsAtWalletCeiling`.
3. **비동기 작업 결제 중 멤버가 조직에서 빠짐** — 환급은 작업 제출 시점의 조직(`PrivateData.OrganizationId`)으로 간다. → Task 3 `TestOrganizationWalletFundingPinsOrganizationAcrossMembershipChange`, Task 3 `TestTaskRefundUsesPinnedOrganization`.
4. **root가 `organization_id` 없이 조직 사용자 목록을 조회** — 500이 아니라 "organization_id is required"를 받아야 한다(team 7bdac0374 회귀). → Task 6 `TestListOrganizationUsersRootRequiresOrganizationId`.
5. **조직 관리자가 xlsx로 멤버를 올리면서 `organization_role=owner`나 글로벌 역할 컬럼을 넣음** — 소유자는 하나뿐이고 글로벌 역할은 바뀌지 않아야 한다. → Task 4 `TestImportOrgUsersNeverEscalatesRoles`.

---

## 파일 구조

새 fork 파일(모두 team에서 복사·적응하거나 새로 작성):

| 파일 | 책임 |
|---|---|
| `model/organization.go`, `model/organization_subscription.go`, `model/organization_dashboard.go` | 조직·플랜·구독·대시보드 모델과 쿼리 (Task 1) |
| `model/organization_migrate.go` | `MigrateForkOrganizationTables(db)` — 조직 4테이블 AutoMigrate (Task 1) |
| `model/subscription_fork.go` | `CalcSubscriptionPlanEndTime`, `CalcSubscriptionNextResetTime` 래퍼 (Task 1) |
| `model/user_cache_fork.go` | `InvalidateUserCache` 공개 래퍼 (Task 1) |
| `model/topup_target.go` | target 상수, `EffectiveTarget*`, 조직 지갑 상한 검증·적립, 조직 충전 내역 (Task 2) |
| `model/log_organization.go` | `GetLogsByUserIDs` (Task 4) |
| `service/funding_source_organization.go`, `service/billing_session_organization.go`, `service/task_billing_organization.go` | 조직 과금 (Task 3) |
| `service/org_user_export.go` | 조직 멤버 xlsx (Task 4) |
| `common/json_strict.go` | `DecodeJsonStrict` (Task 5) |
| `controller/topup_target.go`, `controller/organization_validation.go`, `controller/organization.go`, `controller/organization_subscription.go`, `controller/organization_audit.go` | 핸들러 (Task 5) |
| `router/organization-router.go`, `service/authz/resources_organization.go` | 라우트·권한 (Task 5) |
| `web/src/lib/organization-roles.ts`, `web/src/features/organizations/**`, `web/src/routes/_authenticated/organization/**` | 프런트 (Task 8–11) |
| `web/e2e/organization.e2e.ts`, `web/playwright.config.ts` | e2e (Task 12) |

upstream 파일 훅(각 Task에 정확한 위치): `model/user.go`, `model/topup.go`, `model/topup_paypal.go`, `model/task.go`, `model/main.go`, `model/midjourney.go`, `relay/common/relay_info.go`, `service/billing.go`, `service/billing_session.go`, `service/quota.go`, `service/text_quota.go`, `service/task_billing.go`, `service/violation_fee.go`, `service/log_info_generate.go`, `service/midjourney.go`, `relay/mjproxy_handler.go`, `controller/user.go`, `controller/topup.go`, `controller/topup_{stripe,creem,waffo,waffo_pancake,paypal}.go`, `router/api-router.go`, `router/access_token_scope_test.go`, `service/task_billing_test.go`(TestMain), `model/task_cas_test.go`(TestMain), `web/src/{stores/auth-store.ts, features/users/types.ts, features/profile/types.ts, features/wallet/**, features/usage-logs/**, features/dashboard/lib/charts.ts, hooks/use-sidebar-*.ts, features/system-settings/maintenance/*, routes/_authenticated/wallet/index.tsx, i18n/locales/*}`.

## team i18n 키 → develop 영문 변환표 (Task 5·6에서 사용)

| team 키 | develop 텍스트 |
|---|---|
| `MsgOrgNotFound` | `organization not found` |
| `MsgOrgRootRequired` | `root permission required` |
| `MsgOrgOwnerRequired` | `organization owner permission required` |
| `MsgOrgAdminRequired` | `organization admin permission required` |
| `MsgOrgNoFieldsToUpdate` | `no organization fields to update` |
| `MsgOrgNoUserFieldsToUpdate` | `no organization user fields to update` |
| `MsgOrgInvalidStartTs` | `invalid start_timestamp` |
| `MsgOrgInvalidEndTs` | `invalid end_timestamp` |
| `MsgOrgInvalidPreset` | `invalid preset` |
| `MsgOrgTimestampOrder` | `start_timestamp must be before end_timestamp` |
| `MsgOrgIdRequired` | `organization_id is required` |
| `MsgOrgSubNoFieldsToUpdate` | `no subscription plan fields to update` |
| `MsgOrgTargetPermDenied` | `organization target permission denied` |
| `MsgOrgGlobalAdminUnmanageable` | `global admin users cannot be managed by organization admins` |
| `MsgOrgCannotReassignSelf` | `organization admins cannot reassign themselves` |
| `MsgOrgInvalidRole` | `invalid organization role` |
| `MsgOrgOnlyOwnerCanAssignOwner` | `only the organization owner can assign the owner role` |
| `MsgOrgUserNotInOrg` | `user does not belong to your organization` |
| `MsgOrgOwnerCannotBeRemoved` | `organization owner cannot be removed` |
| `MsgInvalidParams` | `Invalid parameters` |
| `MsgTopupQuotaCapacityExceeded` | upstream `model.ErrTopUpQuotaLimitExceeded`(`Top-up quota limit exceeded`) 사용 |

변환 규칙: `common.ApiErrorI18n(c, i18n.MsgX)` → `common.ApiErrorT(c, "텍스트")`; `common.ApiErrorI18n(c, i18n.MsgX, map[string]any{...})` → `common.ApiErrorT(c, "텍스트 {{param}}", map[string]any{...})`(파라미터 이름은 소문자); `common.TranslateMessage(c, i18n.MsgX)` → 문자열 리터럴; `errors.New(common.TranslateMessage(c, i18n.MsgX))` → `common.NewMessage("텍스트")`. 테스트의 `i18n.Translate(i18n.LangEn, i18n.MsgX)`는 같은 텍스트 리터럴로.

---

### Task 1: 조직 데이터 모델과 사용자·충전·작업 필드

**Files:**
- Create: `model/organization.go`, `model/organization_subscription.go`, `model/organization_dashboard.go` (team 복사+적응), `model/organization_migrate.go`, `model/subscription_fork.go`, `model/user_cache_fork.go`, `model/organization_test.go`, `model/organization_subscription_test.go`, `model/organization_quota_reserve_test.go` (team 복사+적응)
- Modify: `model/user.go`(User 구조체, `GetSelfUserById` Select 목록), `model/topup.go`(TopUp 구조체), `model/task.go`(`TaskPrivateData`, `InitTask`), `relay/common/relay_info.go`, `model/main.go`(`migrateDB` AutoMigrate 오류 검사 뒤), `model/task_cas_test.go`(TestMain), `controller/user.go`(`buildSelfUserData`)

**Interfaces:**
- Consumes: `lockForUpdate(tx *gorm.DB) *gorm.DB`, `calcPlanEndTime(start time.Time, plan *SubscriptionPlan) (int64, error)`, `calcNextResetTime(base time.Time, plan *SubscriptionPlan, endUnix int64) int64`, `RefreshUserGroupCache(userId int) error`, `invalidateUserCache(userId int) error`, `common.MaxWalletQuota`.
- Produces: `model.Organization{Id, Name, Description, OwnerUserId, Quota int64, UsedQuota int64, Status, CreatedAt, UpdatedAt}`; 상수 `OrganizationStatusEnabled=1/Disabled=2`, `OrganizationRoleMember/Admin/Owner`; `IsValidOrganizationRole`, `HasOrganizationAdminRole`, `HasOrganizationOwnerRole`, `GetOrganizationOwnerUserId`, `GetOrganizationQuota(int) (int64, error)`, `IncreaseOrganizationQuota(int, int64) error`, `DecreaseOrganizationQuota`, `TryReserveOrganizationQuota(int, int64) (bool, error)`, `UpdateOrganizationUsedQuota`, `CreateOrganization(name, description string, ownerUserId int, quota ...int64) (*Organization, error)`, `AssignUserToOrganization(userID, organizationID int, organizationRole string) error`, `CanManageOrganizationTarget(actor, target User) bool`, `UpdateOrganizationFields(id int, updates map[string]interface{}) error`, `DeleteOrganization(id int) error`; 구독 모델 3종과 team과 같은 이름의 함수들(`PreConsumeOrganizationUserSubscription(requestId string, organizationId, userId int, modelName string, amount int64)`, `PostConsumeOrganizationUserSubscriptionDelta(id int, delta int64)`, `RefundOrganizationSubscriptionPreConsume(requestId string)`, `HasActiveOrganizationUserSubscription`, `GetActiveOrganizationUserSubscription`, `CreateOrganizationUserSubscriptionFromPlan`, `CancelActiveOrganizationUserSubscriptionForUser`, `ListOrganizationSubscriptionPlans`, `ListOrganizationUserSubscriptions`, `GetOrganizationSubscriptionPlanById`, `GetOrganizationSubscriptionPlanInfoByUserSubscriptionId`); `GetOrganizationDashboard(organizationId int, startTime, endTime int64) (*OrganizationDashboard, error)`; `MigrateForkOrganizationTables(db *gorm.DB) error`; `InvalidateUserCache(userId int) error`; `User.OrganizationId int`, `User.OrganizationRole string`; `TopUp.TargetType string`, `TopUp.TargetId int`; `TaskPrivateData.OrganizationId int`; `relaycommon.RelayInfo.BillingOrganizationId int`; 로그인·`/api/user/self` 응답의 `organization_id`, `organization_role`.

- [ ] **Step 1: team 테스트 3개 복사 후 적응**

```bash
for f in model/organization_test.go model/organization_subscription_test.go model/organization_quota_reserve_test.go; do git show team:$f > $f; done
```

적응:
- 두 `setup*TestDB` 헬퍼에서 `common.UsingSQLite = true` 류 3줄을 `common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)` 한 줄로 바꾼다.
- `model/task_cas_test.go`의 `TestMain` AutoMigrate 목록에 `&Organization{}, &OrganizationSubscriptionPlan{}, &OrganizationUserSubscription{}, &OrganizationSubscriptionPreConsumeRecord{}, // fork: O3`를 추가하고, `truncateTables`에 `"organizations"`를 추가한다(`organization_quota_reserve_test.go`가 쓴다).
- `int64(...)` 캐스팅: 테스트가 `User{Quota: int64(...)}`를 만들면 `int`로 바꾼다. 조직 쪽 `Quota`는 int64 그대로.
- `TestOrganizationRoleValidation` 등 Toss와 무관한 테스트는 그대로. `model/organization_toss_lifecycle_test.go`는 복사하지 않는다.

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./model/ -run 'Organization' 2>&1 | head -5
```

Expected: 컴파일 실패 (`undefined: Organization`).

- [ ] **Step 3: 모델 3파일 복사 후 적응**

```bash
for f in model/organization.go model/organization_subscription.go model/organization_dashboard.go; do git show team:$f > $f; done
```

`model/organization.go`:
- `CreateOrganization` 안의 `DeactivatePersonalTossBillingForOrganizationJoin(...)` 호출 블록과 import를 삭제한다.
- `AssignUserToOrganizationWithBillingLifecycle` → 이름을 `AssignUserToOrganization`으로 바꾸고 Toss 호출을 삭제한다. `UpdateOrganizationFieldsWithBillingLifecycle` → `UpdateOrganizationFields`, Toss 호출 삭제.
- `tx.Clauses(clause.Locking{Strength: "UPDATE"})` → `lockForUpdate(tx)`; `gorm.io/gorm/clause` import 제거.
- `common.MaxQuota` → `int64(common.MaxWalletQuota)` (주석 포함).
- 파일 머리에 `// fork: O3 — organizations. Hooks: model/user.go (OrganizationId/OrganizationRole), model/main.go (MigrateForkOrganizationTables).` 주석.

`model/organization_subscription.go`:
- `UpdateUserGroupCache(userId, group)` → DB 갱신 뒤 `RefreshUserGroupCache(userId)`.
- `tx.Clauses(clause.Locking{...})` → `lockForUpdate(tx)`.
- `CalcSubscriptionPlanEndTime`/`CalcSubscriptionNextResetTime` 호출은 Step 4의 래퍼가 제공한다.

`model/organization_dashboard.go`: 변경 없음(순수 Go 집계). `QuotaData` 필드명이 develop과 같은지 `go build`로 확인.

- [ ] **Step 4: 보조 fork 파일**

`model/organization_migrate.go`:

```go
package model

import "gorm.io/gorm"

// fork: O3 — organization tables. Called from migrateDB (model/main.go) and
// from package TestMains that need the organization schema.
func MigrateForkOrganizationTables(db *gorm.DB) error {
	return db.AutoMigrate(
		&Organization{},
		&OrganizationSubscriptionPlan{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPreConsumeRecord{},
	)
}
```

`model/subscription_fork.go`:

```go
package model

import "time"

// fork: O3 — organization plans reuse upstream's personal-plan period math.
func CalcSubscriptionPlanEndTime(start time.Time, durationUnit string, durationValue int, customSeconds int64) (int64, error) {
	return calcPlanEndTime(start, &SubscriptionPlan{DurationUnit: durationUnit, DurationValue: durationValue, CustomSeconds: customSeconds})
}

func CalcSubscriptionNextResetTime(base time.Time, resetPeriod string, resetCustomSeconds int64, endUnix int64) int64 {
	return calcNextResetTime(base, &SubscriptionPlan{QuotaResetPeriod: resetPeriod, QuotaResetCustomSeconds: resetCustomSeconds}, endUnix)
}
```

`model/user_cache_fork.go`:

```go
package model

// fork: O3 — organization handlers evict a member's cached profile after
// changing membership or quota.
func InvalidateUserCache(userId int) error { return invalidateUserCache(userId) }
```

- [ ] **Step 5: upstream 구조체 필드와 훅**

`model/user.go` `User` 구조체의 `Group` 필드 바로 뒤에:

```go
	OrganizationId   int    `json:"organization_id" gorm:"type:int;default:0;column:organization_id;index"`      // fork: O3
	OrganizationRole string `json:"organization_role" gorm:"type:varchar(16);default:'';column:organization_role"` // fork: O3
```

`GetSelfUserById`의 `Select([]string{...})`에 `"organization_id", "organization_role", // fork: O3` 한 줄 추가(`"group"` 뒤).

`model/topup.go` `TopUp` 구조체 끝(`ProviderOrderId` 뒤):

```go
	TargetType      string  `json:"target_type" gorm:"type:varchar(32);default:'user'"` // fork: O3 wallet target
	TargetId        int     `json:"target_id" gorm:"default:0;index"`                   // fork: O3
```

`model/task.go` `TaskPrivateData`에 `OrganizationId int \`json:"organization_id,omitempty"\` // fork: O3 pinned billing organization`; `InitTask`에서 `privateData`를 채우는 기존 `if relayInfo != nil` 블록 안(또는 바로 뒤)에 `privateData.OrganizationId = relayInfo.BillingOrganizationId // fork: O3` (nil 검사 안에 둔다).

`relay/common/relay_info.go` `SubscriptionId` 필드 뒤: `BillingOrganizationId int // fork: O3 organization charged for this request (0 = none)`.

`model/main.go` `migrateDB`: AutoMigrate의 `if err != nil { return err }` 바로 뒤에

```go
	if err := MigrateForkOrganizationTables(DB); err != nil { // fork: O3
		return err
	}
```

`controller/user.go` `buildSelfUserData`의 맵에 `"group"` 뒤 두 줄:

```go
		"organization_id":   user.OrganizationId,   // fork: O3
		"organization_role": user.OrganizationRole, // fork: O3
```

- [ ] **Step 6: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go vet ./model/ ./controller/ && GOWORK=off go test ./model/ -run 'Organization|TryReserveOrganization' -v 2>&1 | tail -20 && GOWORK=off go test ./model/ 2>&1 | tail -3
```

Expected: 조직 테스트 전부 PASS(team 12개), 패키지 ok. `TestUserQuotaColumnsDeclareBigint`도 그대로 통과.

- [ ] **Step 7: 커밋**

`docs/fork-overlays.md`에 O3 행: user.go 필드 2 + Select 1, topup.go 필드 2, task.go 필드 1 + InitTask 1, relay_info.go 1, main.go 1, controller/user.go 2, task_cas_test.go 1.

```bash
git add model/ relay/common/relay_info.go controller/user.go docs/fork-overlays.md
git commit -m "feat(organization): port the organization, plan and subscription models onto upstream

Ported-From: team model/organization.go model/organization_subscription.go model/organization_dashboard.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: 충전 target 정산(사용자/조직 지갑)과 조직 충전 내역

**Files:**
- Create: `model/topup_target.go`, `model/topup_target_test.go`
- Modify: `model/topup.go`(`Recharge`, Stripe `Recharge`, `ManualCompleteTopUp`, `RechargeCreem`, `RechargeWaffo`, `RechargeWaffoPancake`의 `creditTopUpQuota`/`syncCreditUserQuotaCache` 호출 12줄), `model/topup_paypal.go`(2줄)

**Interfaces:**
- Consumes: `topUpQuotaMaxCurrent(creditedQuota int) (int, error)`, `creditTopUpQuota(tx, userId, creditedQuota int, updates map[string]any) error`, `syncCreditUserQuotaCache(userId, quota int, operation string)`, `ErrTopUpQuotaLimitExceeded`, `sanitizeLikePattern`, `searchTopUpCountHardLimit`.
- Produces: `TopUpTargetTypeUser = "user"`, `TopUpTargetTypeOrganization = "organization"`; `(*TopUp).EffectiveTargetType() string`, `(*TopUp).EffectiveTargetId() int`; `ValidateOrganizationWalletCapacity(orgId int, creditedQuota int) error`; `ValidateTopUpTargetCapacity(targetType string, targetId int, creditedQuota int) error`; `creditTopUpTarget(tx *gorm.DB, topUp *TopUp, creditedQuota int, updates map[string]any) error`; `syncCreditTopUpCache(topUp *TopUp, creditedQuota int, operation string)`; `GetOrganizationTopUps(organizationId int, pageInfo *common.PageInfo) ([]*TopUp, int64, error)`; `SearchOrganizationTopUps(organizationId int, keyword string, pageInfo *common.PageInfo) ([]*TopUp, int64, error)`.

- [ ] **Step 1: 테스트 작성**

`model/topup_target_test.go`:

```go
package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func cleanupTopUpTargetTest(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM organizations")
	})
}

func TestCreditTopUpTargetCreditsOrganizationNotPayer(t *testing.T) {
	cleanupTopUpTargetTest(t)
	owner := &User{Id: 701, Username: "topup_target_owner", Status: common.UserStatusEnabled, Quota: 10}
	require.NoError(t, DB.Create(owner).Error)
	org := &Organization{Name: "topup-target-org", OwnerUserId: owner.Id, Quota: 100, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(org).Error)
	topUp := &TopUp{UserId: owner.Id, TargetType: TopUpTargetTypeOrganization, TargetId: org.Id, TradeNo: "tt-org-1", Status: common.TopUpStatusPending}

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 50, nil) }))

	var gotOrg Organization
	require.NoError(t, DB.First(&gotOrg, org.Id).Error)
	require.EqualValues(t, 150, gotOrg.Quota)
	var gotOwner User
	require.NoError(t, DB.First(&gotOwner, owner.Id).Error)
	require.Equal(t, 10, gotOwner.Quota, "payer wallet must not change for an organization target")
}

func TestCreditTopUpTargetOrganizationStopsAtWalletCeiling(t *testing.T) {
	cleanupTopUpTargetTest(t)
	org := &Organization{Name: "topup-ceiling-org", OwnerUserId: 1, Quota: int64(common.MaxWalletQuota) - 40, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(org).Error)
	topUp := &TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: org.Id}

	// First credit fits exactly, the second would overflow the ceiling.
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 40, nil) }))
	err := DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 1, nil) })
	require.True(t, errors.Is(err, ErrTopUpQuotaLimitExceeded), "got %v", err)
	var gotOrg Organization
	require.NoError(t, DB.First(&gotOrg, org.Id).Error)
	require.EqualValues(t, int64(common.MaxWalletQuota), gotOrg.Quota)

	require.True(t, errors.Is(ValidateOrganizationWalletCapacity(org.Id, 1), ErrTopUpQuotaLimitExceeded))
	require.Error(t, ValidateTopUpTargetCapacity(TopUpTargetTypeOrganization, org.Id, 0), "a zero credit is invalid")
}

func TestCreditTopUpTargetUserPathUnchanged(t *testing.T) {
	cleanupTopUpTargetTest(t)
	user := &User{Id: 702, Username: "topup_target_user", Status: common.UserStatusEnabled, Quota: 5}
	require.NoError(t, DB.Create(user).Error)
	topUp := &TopUp{UserId: user.Id} // no target fields: legacy rows
	require.Equal(t, TopUpTargetTypeUser, topUp.EffectiveTargetType())
	require.Equal(t, user.Id, topUp.EffectiveTargetId())
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error { return creditTopUpTarget(tx, topUp, 7, nil) }))
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 12, got.Quota)
}

func TestOrganizationTopUpsListOnlyOrganizationOrders(t *testing.T) {
	cleanupTopUpTargetTest(t)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: 9, TradeNo: "org-9-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TargetType: TopUpTargetTypeOrganization, TargetId: 10, TradeNo: "org-10-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	require.NoError(t, DB.Create(&TopUp{UserId: 1, TradeNo: "user-1-a", Status: common.TopUpStatusSuccess, CreateTime: 100}).Error)
	topups, total, err := GetOrganizationTopUps(9, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "org-9-a", topups[0].TradeNo)
	found, total, err := SearchOrganizationTopUps(9, "org-9", &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, found, 1)
}
```

(`gorm` import: `"gorm.io/gorm"`. `common.PageInfo` 필드명이 다르면 `common.GetPageQuery`가 만드는 구조체를 따른다.)

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./model/ -run 'TopUpTarget|OrganizationTopUps' 2>&1 | head -5
```

Expected: 컴파일 실패.

- [ ] **Step 3: `model/topup_target.go`**

```go
package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O3 — a top-up settles into the payer's wallet or the organization
// wallet the order was created for. Hooks: the creditTopUpQuota /
// syncCreditUserQuotaCache pairs in model/topup.go and model/topup_paypal.go
// call creditTopUpTarget / syncCreditTopUpCache instead.

const (
	TopUpTargetTypeUser         = "user"
	TopUpTargetTypeOrganization = "organization"
)

func (topUp *TopUp) EffectiveTargetType() string {
	if topUp != nil && topUp.TargetType == TopUpTargetTypeOrganization {
		return TopUpTargetTypeOrganization
	}
	return TopUpTargetTypeUser
}

func (topUp *TopUp) EffectiveTargetId() int {
	if topUp.EffectiveTargetType() == TopUpTargetTypeOrganization && topUp.TargetId > 0 {
		return topUp.TargetId
	}
	return topUp.UserId
}

// ValidateOrganizationWalletCapacity is the pre-payment check for an
// organization wallet, mirroring ValidateTopUpQuotaCapacity.
func ValidateOrganizationWalletCapacity(orgId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	var org Organization
	if err := DB.Select("quota").Where("id = ?", orgId).First(&org).Error; err != nil {
		return err
	}
	if org.Quota > int64(maxCurrentQuota) {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

func ValidateTopUpTargetCapacity(targetType string, targetId int, creditedQuota int) error {
	if targetType == TopUpTargetTypeOrganization {
		return ValidateOrganizationWalletCapacity(targetId, creditedQuota)
	}
	return ValidateTopUpQuotaCapacity(targetId, creditedQuota)
}

// creditOrganizationQuota adds a top-up to an organization wallet with the
// same atomic ceiling predicate creditTopUpQuota uses for users. A negative
// balance (debt) can still be reduced; only the upper bound matters.
func creditOrganizationQuota(tx *gorm.DB, orgId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	result := tx.Model(&Organization{}).
		Where("id = ? AND quota <= ?", orgId, maxCurrentQuota).
		Update("quota", gorm.Expr("quota + ?", creditedQuota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := tx.Model(&Organization{}).Where("id = ?", orgId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return errors.New("organization not found")
	}
	return ErrTopUpQuotaLimitExceeded
}

// creditTopUpTarget routes a settled order to its wallet. User-row updates
// (Stripe customer id, Creem email) only apply to user targets.
func creditTopUpTarget(tx *gorm.DB, topUp *TopUp, creditedQuota int, updates map[string]any) error {
	if topUp.EffectiveTargetType() == TopUpTargetTypeOrganization {
		return creditOrganizationQuota(tx, topUp.EffectiveTargetId(), creditedQuota)
	}
	return creditTopUpQuota(tx, topUp.UserId, creditedQuota, updates)
}

// syncCreditTopUpCache refreshes the payer's cached balance only when the
// payer's own wallet was credited; organization wallets are not cached.
func syncCreditTopUpCache(topUp *TopUp, creditedQuota int, operation string) {
	if topUp.EffectiveTargetType() == TopUpTargetTypeUser {
		syncCreditUserQuotaCache(topUp.UserId, creditedQuota, operation)
	}
}

func GetOrganizationTopUps(organizationId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	query := DB.Model(&TopUp{}).Where("target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationId)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	return topups, total, err
}

func SearchOrganizationTopUps(organizationId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	pattern := "%" + sanitizeLikePattern(keyword) + "%"
	query := DB.Model(&TopUp{}).
		Where("target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationId).
		Where("trade_no LIKE ? ESCAPE '!'", pattern)
	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	return topups, total, err
}
```

`common.PageInfo`의 메서드 이름(`GetPageSize`/`GetStartIdx`)과 `sanitizeLikePattern`의 escape 문자는 develop `model/topup.go`의 `SearchTopUps`를 보고 맞춘다(team `SearchOrganizationTopUps`가 같은 패턴을 쓴다).

- [ ] **Step 4: 정산 호출 교체(훅)**

`model/topup.go`에서 아래 7곳의 두 줄씩을 바꾼다(각각 `// fork: O3` 주석):

| 함수 | 전 | 후 |
|---|---|---|
| `Recharge`(epay) | `creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)` / `syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, …)` | `creditTopUpTarget(tx, topUp, quotaToAdd, nil)` / `syncCreditTopUpCache(topUp, quotaToAdd, …)` |
| Stripe `Recharge` | `creditTopUpQuota(tx, topUp.UserId, quota, map[string]any{…})` / sync | `creditTopUpTarget(tx, topUp, quota, map[string]any{…})` / `syncCreditTopUpCache(topUp, quota, …)` |
| `ManualCompleteTopUp` | `creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)` / `syncCreditUserQuotaCache(userId, …)` | `creditTopUpTarget(tx, topUp, quotaToAdd, nil)` / `syncCreditTopUpCache(topUp, quotaToAdd, …)` |
| `RechargeCreem` | `creditTopUpQuota(tx, topUp.UserId, quota, updateFields)` / sync | `creditTopUpTarget(tx, topUp, quota, updateFields)` / `syncCreditTopUpCache(topUp, quota, …)` |
| `RechargeWaffo`, `RechargeWaffoPancake` | 같은 패턴 | 같은 패턴 |
| `model/topup_paypal.go` `RechargePayPal` | 같은 패턴 | 같은 패턴 |

`model/redemption.go`는 건드리지 않는다(환급 코드는 항상 사용자).

- [ ] **Step 5: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./model/ -run 'TopUpTarget|OrganizationTopUps|Recharge|PaymentMethodGuard' -v 2>&1 | tail -15 && GOWORK=off go test ./model/ 2>&1 | tail -2
```

Expected: 새 테스트 4개 PASS, 기존 충전 테스트 변화 없음.

- [ ] **Step 6: 커밋**

`docs/fork-overlays.md`: topup.go 12줄, topup_paypal.go 2줄 행 추가.

```bash
git add model/topup_target.go model/topup_target_test.go model/topup.go model/topup_paypal.go docs/fork-overlays.md
git commit -m "feat(organization): settle top-ups into the organization wallet by target

Ported-From: team model/topup.go (CreditTopUpTarget, organization top-up lists)
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: 조직 과금(지갑·구독 FundingSource, BillingSession 훅, 사용량·작업 환급)

**Files:**
- Create: `service/funding_source_organization.go`, `service/billing_session_organization.go`, `service/task_billing_organization.go`, `service/organization_billing_test.go`(team 복사+적응), `service/funding_source_organization_test.go`(team `funding_source_test.go`의 조직 2개 + 신규 1개)
- Modify: `service/billing.go`(상수 1, 알림 1), `service/billing_session.go`(훅 8곳), `service/quota.go`(`postConsumeQuotaWithResult` 1, used-quota 2), `service/text_quota.go`(1), `service/violation_fee.go`(1), `service/task_billing.go`(4), `service/log_info_generate.go`(1), `service/midjourney.go`(1), `relay/mjproxy_handler.go`(2), `service/task_billing_test.go`(TestMain 1)

**Interfaces:**
- Consumes: Task 1 모델 함수들; upstream `FundingSource` 인터페이스(`Source() string; PreConsume(amount int) error; Settle(delta int) error; Refund() error`), `WalletFunding`, `SubscriptionFunding`, `refundWithRetry(fn func() error) error`, `ErrInsufficientWalletQuota`, `BillingSession{relayInfo, funding, lang, extraReserved, preConsumedQuota, …}`, `(*BillingSession).preConsume(c, quota)`, `i18n.StatedLang(c)`, `i18n.MsgQuotaUserInsufficient`/`MsgQuotaPreConsumeInsufficient`, `logger.FormatQuota(int)`.
- Produces: `BillingSourceOrganizationSubscription = "organization_subscription"`; `OrganizationWalletFunding{memberId, organizationId int; consumed int}`; `OrganizationSubscriptionFunding{requestId string; organizationId, userId int; modelName string; amount int64; organizationUserSubscriptionId int; preConsumed, organizationWalletConsumed, AmountTotal, AmountUsedAfter int64; PlanId int; PlanTitle string}`; `AdjustWalletQuotaForUser(userId int, delta int) error`; `adjustWalletQuotaForOrganization(userId, organizationId int, delta int) error`; `UpdateOrganizationUsedQuotaForUser(userId int, quota int)`; `newOrganizationBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError, bool)`; `reserveOrganizationFunding(funding FundingSource, delta int, lang string) error`, `rollbackOrganizationFundingReserve(funding FundingSource, delta int)`, `refundOrganizationExtraReserved(funding FundingSource, subscriptionId, extraReserved int)`, `organizationFundingNeedsRefund(funding FundingSource) bool`, `syncOrganizationRelayInfo(funding FundingSource, info *relaycommon.RelayInfo, extraReserved int)`, `isOrganizationSubscriptionSource(source string) bool`; `postConsumeOrganizationQuota(relayInfo, quota int) (handled bool, err error)`; `taskIsOrganizationSubscription(task) bool`, `taskAdjustOrganizationFunding(task, delta int) (handled bool, err error)`, `taskAdjustOrganizationUsedQuota(task, delta int)`.

- [ ] **Step 1: 테스트 복사·적응**

```bash
git show team:service/organization_billing_test.go > service/organization_billing_test.go
git show team:service/funding_source_test.go > service/funding_source_organization_test.go
```

- `service/task_billing_test.go` `TestMain`: AutoMigrate 호출 바로 뒤에 `if err := model.MigrateForkOrganizationTables(db); err != nil { panic("failed to migrate organization tables: " + err.Error()) } // fork: O3`; `truncate(t)`에 `DELETE FROM organizations`, `organization_subscription_plans`, `organization_user_subscriptions`, `organization_subscription_pre_consume_records` 4줄 추가.
- 헬퍼 `seedOrganizationUser`의 `quota int64` → `int`; 단언의 `int64(...)` 캐스팅을 사용자 쪽은 `int`로, 조직 쪽은 `int64` 유지.
- `funding_source_organization_test.go`에는 team 파일의 조직 테스트 2개(`TestUpdateOrganizationUsedQuotaForUser_*`)만 남긴다.
- 새 테스트 하나를 `funding_source_organization_test.go`에 추가:

```go
func TestTaskRefundUsesPinnedOrganization(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 1000) // helper from organization_billing_test.go: org id 1
	seedOrganization(t, 2, 1000)
	seedOrganizationUser(t, 10, 1, model.OrganizationRoleMember, 500)
	task := &model.Task{UserId: 10, Quota: 100, PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet, OrganizationId: 1}}
	// Member moved to organization 2 after the task was charged.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 10).Update("organization_id", 2).Error)
	handled, err := taskAdjustOrganizationFunding(task, -100)
	require.True(t, handled)
	require.NoError(t, err)
	q1, _ := model.GetOrganizationQuota(1)
	q2, _ := model.GetOrganizationQuota(2)
	require.EqualValues(t, 1100, q1, "refund goes to the pinned organization")
	require.EqualValues(t, 1000, q2)
}
```

그리고 `organization_billing_test.go` 끝에 Review Focus 1을 고정하는 테스트를 추가한다:

```go
func TestOrganizationSubscriptionBillingRejectsWhenOrganizationWalletInsufficient(t *testing.T) {
	truncate(t)
	org := seedOrganization(t, 1, 0) // plan limit remains, organization wallet is empty
	seedOrganizationUser(t, 30, org.Id, model.OrganizationRoleMember, 5000)
	created, err := createOrganizationPlanAndAssign(t, org.Id, 30, 100000) // helper: plan total 100000, assigned to user 30
	require.NoError(t, err)
	require.NotNil(t, created)
	c, relayInfo := newOrganizationBillingContext(t, 30, "gpt-x") // helper from the ported test file
	session, apiErr := NewBillingSession(c, relayInfo, 100)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "organization quota insufficient")
	var member model.User
	require.NoError(t, model.DB.First(&member, 30).Error)
	require.Equal(t, 5000, member.Quota, "member limit untouched")
	sub, err := model.GetActiveOrganizationUserSubscription(org.Id, 30)
	require.NoError(t, err)
	require.EqualValues(t, 0, sub.AmountUsed, "plan untouched")
}
```

`createOrganizationPlanAndAssign`이 team 테스트 파일에 없으면 `model.OrganizationSubscriptionPlan{OrganizationId, Title:"p", DurationUnit:"month", DurationValue:1, Enabled:true, TotalAmount: total}`를 만들고 `model.CreateOrganizationUserSubscriptionFromPlan(orgId, userId, plan.Id, 0)`을 부르는 10줄 헬퍼로 같은 파일에 정의한다. `apiErr`의 상태 코드 접근자는 `types.NewAPIError`의 실제 필드/메서드명(`StatusCode`)을 따른다.

(`seedOrganization`/`seedOrganizationUser`의 실제 시그니처는 복사한 team 테스트 파일을 따른다.)

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./service/ -run 'Organization' 2>&1 | head -5
```

Expected: 컴파일 실패.

- [ ] **Step 3: `service/funding_source_organization.go`**

team `service/funding_source.go` 73-197줄(`resolveOrganizationForWallet`, `AdjustWalletQuotaForUser`, `adjustWalletQuotaForOrganization`, `reserveWalletQuotaForOrganization`, `UpdateOrganizationUsedQuotaForUser`, `OrganizationWalletFunding`)과 256-344줄(`OrganizationSubscriptionFunding`)을 이 파일로 옮긴다. 적응:
- 사용자 쪽 수량은 `int`: `model.DecreaseUserQuota(userId, delta, false)`, `IncreaseUserQuota(userId, refund, false)`, `TryReserveUserQuota(userId, amount)`에 `int`를 넘기고, 조직 쪽은 `int64(delta)`.
- 시그니처: `AdjustWalletQuotaForUser(userId int, delta int) error`, `adjustWalletQuotaForOrganization(userId, organizationId int, delta int) error`, `reserveWalletQuotaForOrganization(userId, organizationId int, amount int) error`, `UpdateOrganizationUsedQuotaForUser(userId int, quota int)`.
- 파일 머리 주석에 team의 "member limit + owner wallet" 오기(실제로는 member limit + organization wallet)를 바로잡는다.
- `OrganizationSubscriptionFunding.Source()`는 `BillingSourceOrganizationSubscription`(Step 5에서 상수 추가).

- [ ] **Step 4: `service/billing_session_organization.go`**

```go
package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// fork: O3 — organization members never use the personal billing preference:
// an active organization plan charges plan + organization wallet, otherwise
// the member limit (users.quota) + organization wallet are charged together.
// Hook: NewBillingSession calls newOrganizationBillingSession first.

func isOrganizationSubscriptionSource(source string) bool {
	return source == BillingSourceOrganizationSubscription
}

func orgQuotaError(c *gin.Context, msg string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(msg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func requireOrganizationQuota(c *gin.Context, organizationId int, need int64) *types.NewAPIError {
	organizationQuota, err := model.GetOrganizationQuota(organizationId)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if organizationQuota <= 0 {
		return orgQuotaError(c, fmt.Sprintf("organization quota insufficient, remaining: %s", logger.FormatQuota(int(organizationQuota))))
	}
	if organizationQuota-need < 0 {
		return orgQuotaError(c, fmt.Sprintf("organization pre-charge failed, remaining quota: %s, required pre-charge quota: %s",
			logger.FormatQuota(int(organizationQuota)), logger.FormatQuota(int(need))))
	}
	return nil
}

// newOrganizationBillingSession returns handled=false for users without an
// organization so NewBillingSession continues with upstream's preference logic.
func newOrganizationBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError, bool) {
	user, err := model.GetUserById(relayInfo.UserId, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if user.OrganizationId <= 0 {
		return nil, nil, false
	}
	hasOrgSub, err := model.HasActiveOrganizationUserSubscription(user.OrganizationId, relayInfo.UserId)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if hasOrgSub {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		if apiErr := requireOrganizationQuota(c, user.OrganizationId, subConsume); apiErr != nil {
			return nil, apiErr, true
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &OrganizationSubscriptionFunding{
				requestId: relayInfo.RequestId, organizationId: user.OrganizationId, userId: relayInfo.UserId,
				modelName: relayInfo.OriginModelName, amount: subConsume,
			},
			lang: i18n.StatedLang(c),
		}
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr, true
		}
		return session, nil, true
	}

	// Wallet mode: the member limit is checked exactly like upstream's tryWallet.
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if userQuota <= 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota)})),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog()), true
	}
	if userQuota-preConsumedQuota < 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(i18n.T(c, i18n.MsgQuotaPreConsumeInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota), "Required": logger.FormatQuota(preConsumedQuota)})),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog()), true
	}
	relayInfo.UserQuota = userQuota
	if apiErr := requireOrganizationQuota(c, user.OrganizationId, int64(preConsumedQuota)); apiErr != nil {
		return nil, apiErr, true
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &OrganizationWalletFunding{memberId: relayInfo.UserId, organizationId: user.OrganizationId},
		lang:      i18n.StatedLang(c),
	}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		return nil, apiErr, true
	}
	return session, nil, true
}

// reserveOrganizationFunding is the reserveFunding case for both organization fundings.
func reserveOrganizationFunding(funding FundingSource, delta int, lang string) error {
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("organization wallet quota insufficient: %s", err.Error()),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		f.consumed += delta
		return nil
	case *OrganizationSubscriptionFunding:
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(
				errors.New(i18n.Translate(lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return nil
	}
	return types.NewError(fmt.Errorf("unsupported funding source: %s", funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

func rollbackOrganizationFundingReserve(funding FundingSource, delta int) {
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		if err := f.Settle(-delta); err != nil {
			common.SysLog(common.LogText("error rolling back organization wallet funding reserve: %s", err.Error()))
		} else {
			f.consumed -= delta
		}
	case *OrganizationSubscriptionFunding:
		if err := f.Settle(-delta); err != nil {
			common.SysLog(common.LogText("error rolling back organization subscription funding reserve: %s", err.Error()))
		}
	}
}

func refundOrganizationExtraReserved(funding FundingSource, subscriptionId int, extraReserved int) {
	if extraReserved <= 0 || subscriptionId <= 0 || !isOrganizationSubscriptionSource(funding.Source()) {
		return
	}
	if err := model.PostConsumeOrganizationUserSubscriptionDelta(subscriptionId, -int64(extraReserved)); err != nil {
		common.SysLog(common.LogText("error refunding organization subscription extra reserved quota: %s", err.Error()))
	}
}

func organizationFundingNeedsRefund(funding FundingSource) bool {
	if sub, ok := funding.(*OrganizationSubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	if wallet, ok := funding.(*OrganizationWalletFunding); ok && wallet.consumed > 0 {
		return true
	}
	return false
}

func syncOrganizationRelayInfo(funding FundingSource, info *relaycommon.RelayInfo, extraReserved int) {
	info.BillingOrganizationId = 0
	switch f := funding.(type) {
	case *OrganizationSubscriptionFunding:
		info.BillingOrganizationId = f.organizationId
		info.SubscriptionId = f.organizationUserSubscriptionId
		info.SubscriptionPreConsumed = f.preConsumed + int64(extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = f.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = f.AmountUsedAfter + int64(extraReserved)
		info.SubscriptionPlanId = f.PlanId
		info.SubscriptionPlanTitle = f.PlanTitle
	case *OrganizationWalletFunding:
		info.BillingOrganizationId = f.organizationId
	}
}

func isOrganizationSubscriptionPreConsumeError(errMsg string) bool {
	return strings.Contains(errMsg, "no active organization subscription")
}

// postConsumeOrganizationQuota is the legacy post-consume path (no BillingSession).
func postConsumeOrganizationQuota(relayInfo *relaycommon.RelayInfo, quota int) (bool, error) {
	if relayInfo == nil {
		return false, nil
	}
	if isOrganizationSubscriptionSource(relayInfo.BillingSource) {
		if relayInfo.SubscriptionId == 0 {
			return true, errors.New("organization subscription id is missing")
		}
		if quota != 0 {
			if err := model.PostConsumeOrganizationUserSubscriptionDelta(relayInfo.SubscriptionId, int64(quota)); err != nil {
				return true, err
			}
			if quota > 0 {
				if err := model.DecreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(quota)); err != nil {
					return true, err
				}
			} else if err := model.IncreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(-quota)); err != nil {
				return true, err
			}
			relayInfo.SubscriptionPostDelta += int64(quota)
		}
		return true, nil
	}
	if relayInfo.BillingOrganizationId > 0 {
		return true, adjustWalletQuotaForOrganization(relayInfo.UserId, relayInfo.BillingOrganizationId, quota)
	}
	return false, nil
}
```

- [ ] **Step 5: `service/billing.go`·`service/billing_session.go` 훅**

`service/billing.go` const 블록: `BillingSourceOrganizationSubscription = "organization_subscription" // fork: O3`. 알림 분기 `if relayInfo.BillingSource == BillingSourceSubscription {` → `if relayInfo.BillingSource == BillingSourceSubscription || isOrganizationSubscriptionSource(relayInfo.BillingSource) { // fork: O3`.

`service/billing_session.go`(위치는 함수명으로 찾는다; 각 줄 `// fork: O3`):
1. `NewBillingSession`: `pref := …` 다음에
   ```go
   	if session, apiErr, handled := newOrganizationBillingSession(c, relayInfo, preConsumedQuota); handled { // fork: O3
   		return session, apiErr
   	}
   ```
2. `Settle`: `if s.funding.Source() == BillingSourceSubscription {` → `if s.funding.Source() == BillingSourceSubscription || isOrganizationSubscriptionSource(s.funding.Source()) { // fork: O3`.
3. `Refund` goroutine: 기존 `if extraReserved > 0 && funding.Source() == BillingSourceSubscription …` 블록 뒤에 `refundOrganizationExtraReserved(funding, subscriptionId, extraReserved) // fork: O3`.
4. `needsRefundLocked`: `return false` 앞에 `if organizationFundingNeedsRefund(s.funding) { return true } // fork: O3`.
5. `reserveFunding`: `default:` 앞에 `case *OrganizationWalletFunding, *OrganizationSubscriptionFunding: // fork: O3` + `return reserveOrganizationFunding(s.funding, delta, s.lang)`.
6. `rollbackFundingReserve`: 마지막 case 뒤에 `case *OrganizationWalletFunding, *OrganizationSubscriptionFunding: // fork: O3` + `rollbackOrganizationFundingReserve(s.funding, delta)`.
7. `shouldTrust`: `case BillingSourceWallet:` 안 첫 줄에 `if _, ok := s.funding.(*OrganizationWalletFunding); ok { return false } // fork: O3`; `case BillingSourceSubscription:` 뒤에 `case BillingSourceOrganizationSubscription: // fork: O3` + `return false`.
8. `syncRelayInfo` 끝에 `syncOrganizationRelayInfo(s.funding, info, s.extraReserved) // fork: O3`.
9. `preConsume` 오류 매핑: `strings.Contains(errMsg, "no active subscription") || …` 조건에 `|| isOrganizationSubscriptionPreConsumeError(errMsg) // fork: O3` 추가.

- [ ] **Step 6: 사용량·작업·로그 훅**

- `service/quota.go` `postConsumeQuotaWithResult`: `if relayInfo != nil && relayInfo.BillingSource == BillingSourceSubscription {` 앞에
  ```go
  	if handled, orgErr := postConsumeOrganizationQuota(relayInfo, quota); handled { // fork: O3
  		if orgErr != nil {
  			return result, orgErr
  		}
  	} else
  ```
  (기존 `if … else { wallet }` 체인에 `else`로 이어 붙인다.)
- used-quota: `model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)` 바로 뒤에 `UpdateOrganizationUsedQuotaForUser(relayInfo.UserId, quota) // fork: O3` — `service/quota.go`(2곳), `service/text_quota.go`, `service/violation_fee.go`, `service/task_billing.go` `LogTaskConsumption`; `relay/mjproxy_handler.go` 2곳은 `service.UpdateOrganizationUsedQuotaForUser(...)`.
- `service/task_billing_organization.go`:
  ```go
  package service

  import "github.com/QuantumNous/new-api/model"

  // fork: O3 — async tasks settle against the organization recorded at submission.
  func taskIsOrganizationSubscription(task *model.Task) bool {
  	return isOrganizationSubscriptionSource(task.PrivateData.BillingSource) && task.PrivateData.SubscriptionId > 0
  }

  // taskAdjustOrganizationFunding returns handled=false for tasks without an organization.
  func taskAdjustOrganizationFunding(task *model.Task, delta int) (bool, error) {
  	orgId := task.PrivateData.OrganizationId
  	if taskIsOrganizationSubscription(task) {
  		if err := model.PostConsumeOrganizationUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta)); err != nil {
  			return true, err
  		}
  		if orgId <= 0 {
  			return true, nil
  		}
  		if delta > 0 {
  			return true, model.DecreaseOrganizationQuota(orgId, int64(delta))
  		}
  		return true, model.IncreaseOrganizationQuota(orgId, int64(-delta))
  	}
  	if orgId > 0 {
  		return true, adjustWalletQuotaForOrganization(task.UserId, orgId, delta)
  	}
  	return false, nil
  }

  func taskAdjustOrganizationUsedQuota(task *model.Task, delta int) {
  	if task.PrivateData.OrganizationId > 0 && delta != 0 {
  		_ = model.UpdateOrganizationUsedQuota(task.PrivateData.OrganizationId, int64(delta))
  	}
  }
  ```
- `service/task_billing.go`: `taskAdjustFunding` 첫 줄에 `if handled, err := taskAdjustOrganizationFunding(task, delta); handled { return err } // fork: O3`; `RefundTaskQuota`의 `model.UpdateUserUsedQuota(task.UserId, -quota)` 뒤에 `taskAdjustOrganizationUsedQuota(task, -quota) // fork: O3`; `RecalculateTaskQuota`의 `UpdateUserUsedQuota(task.UserId, quotaDelta)` 뒤에 `taskAdjustOrganizationUsedQuota(task, quotaDelta) // fork: O3`.
- `service/log_info_generate.go`: `if relayInfo.BillingSource == "subscription" {` → `if relayInfo.BillingSource == "subscription" || isOrganizationSubscriptionSource(relayInfo.BillingSource) { // fork: O3`, 그 블록 안 `subscription_id` 설정 뒤에 `if isOrganizationSubscriptionSource(relayInfo.BillingSource) { other.SetPublic("organization_user_subscription_id", relayInfo.SubscriptionId) } // fork: O3`.
- `service/midjourney.go`: `if relayInfo.BillingSource == BillingSourceSubscription {` → `|| isOrganizationSubscriptionSource(relayInfo.BillingSource) // fork: O3`.

- [ ] **Step 7: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go vet ./service/ ./relay/ && GOWORK=off go test ./service/ -run 'Organization|TaskRefundUsesPinned' -v 2>&1 | tail -25 && GOWORK=off go test ./service/ 2>&1 | tail -2
```

Expected: team 11 + 2 + 신규 1 = 14 PASS; 패키지 ok(upstream 과금 테스트 변화 없음).

- [ ] **Step 8: 커밋**

`docs/fork-overlays.md`에 billing.go 2, billing_session.go 9, quota.go 3, text_quota.go 1, violation_fee.go 1, task_billing.go 4, log_info_generate.go 2, midjourney.go 1, mjproxy_handler.go 2, task_billing_test.go 1 행.

```bash
git add service/ relay/mjproxy_handler.go docs/fork-overlays.md
git commit -m "feat(organization): charge organization members against the member limit, organization wallet and organization plans

Ported-From: team service/funding_source.go service/billing_session.go service/task_billing.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: 조직 로그·작업 조회와 멤버 Excel 서비스

**Files:**
- Create: `model/log_organization.go`, `model/log_organization_test.go`, `service/org_user_export.go`(team 복사+적응), `service/org_user_export_test.go`(team `user_import_quota_test.go`의 조직 2개 + 신규)
- Modify: `model/midjourney.go`(`TaskQueryParams` 필드 1, `GetAllTasks`/`CountAllTasks` 필터 2)

**Interfaces:**
- Consumes: `LogFilter`, `(LogFilter).apply`, `logSearchCountLimit`, `clickHouseLogOrder("logs.")`, `common.UsingLogDatabase(common.DatabaseTypeClickHouse)`, `formatUserLogs`; `setImportedUserQuota(userId, current, target int) error`(단계 A), `common.ValidateWalletQuota(int)`, `common.Validate.Struct`.
- Produces: `GetLogsByUserIDs(userIDs []int, filter LogFilter, startIdx, num int) ([]*Log, int64, error)`; `TaskQueryParams.UserIDs []int`; `service.OrgExcelHeaders`, `BuildOrgExportFile(organizationId int) (*excelize.File, error)`, `OrgImportResult{Assigned, Created, Updated, Removed int; Errors []string; …}`, `ImportOrgUsersFromFile(f *excelize.File, organizationId int, removeAbsent bool) (*OrgImportResult, error)`.

- [ ] **Step 1: 테스트**

`model/log_organization_test.go`:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetLogsByUserIDsScopesToMembers(t *testing.T) {
	t.Cleanup(func() { DB.Exec("DELETE FROM logs") })
	for _, l := range []*Log{
		{UserId: 11, Username: "m1", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 100},
		{UserId: 12, Username: "m2", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 101},
		{UserId: 99, Username: "outsider", ModelName: "gpt-x", Type: LogTypeConsume, CreatedAt: 102},
	} {
		require.NoError(t, LOG_DB.Create(l).Error)
	}
	logs, total, err := GetLogsByUserIDs([]int{11, 12}, LogFilter{ModelName: "gpt-x"}, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	require.Equal(t, "m2", logs[0].Username, "newest first")

	logs, total, err = GetLogsByUserIDs(nil, LogFilter{}, 0, 10)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, logs)
}
```

`service/org_user_export_test.go`: team `service/user_import_quota_test.go`에서 `TestImportOrgUsersRejectsQuotaAboveWalletCeiling`, `TestImportedWalletQuota`를 옮기고(`common.MaxQuota` → `common.MaxWalletQuota`, int 적응, `newImportWorkbook`은 단계 A의 `user_export_test.go`에 이미 있음) 다음을 추가:

```go
func TestImportOrgUsersNeverEscalatesRoles(t *testing.T) {
	truncate(t)
	org := seedOrganization(t, 1, 1000)
	seedOrganizationUser(t, 20, org.Id, model.OrganizationRoleOwner, 0) // owner
	seedOrganizationUser(t, 21, org.Id, model.OrganizationRoleMember, 0)
	// Column names come from OrgExcelHeaders; a global "role" column is not part of the format.
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "quota", "role"},
		{"member_21", "owner", "1", "root"},
		{"brand_new", "admin", "1", "admin"},
	})
	// username of user 21 must match what seedOrganizationUser creates; adjust if the helper names differ.
	result, err := ImportOrgUsersFromFile(f, org.Id, false)
	require.NoError(t, err)
	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Equal(t, model.OrganizationRoleMember, member.OrganizationRole, "owner role cannot be granted by import")
	require.Equal(t, common.RoleCommonUser, member.Role, "global role untouched")
	var created model.User
	if err := model.DB.Where("username = ?", "brand_new").First(&created).Error; err == nil {
		require.Equal(t, common.RoleCommonUser, created.Role)
		require.NotEqual(t, model.OrganizationRoleOwner, created.OrganizationRole)
	}
	require.NotEmpty(t, result.Errors, "the owner row must be reported, not silently applied")
}
```

(`seedOrganization`/`seedOrganizationUser`는 Task 3의 테스트 헬퍼; 같은 패키지라 재사용된다. 새 사용자 행에는 `initial_password`가 필요하면 워크북에 컬럼을 추가한다.)

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./model/ -run TestGetLogsByUserIDs 2>&1 | head -3; GOWORK=off go test ./service/ -run 'ImportOrgUsers|ImportedWalletQuota' 2>&1 | head -3
```

Expected: 둘 다 컴파일 실패.

- [ ] **Step 3: `model/log_organization.go`와 midjourney 훅**

```go
package model

import "github.com/QuantumNous/new-api/common"

// fork: O3 — organization admins list their members' logs. Mirrors GetUserLogs
// (same filter, count limit, ClickHouse order and user-visibility formatting).
func GetLogsByUserIDs(userIDs []int, filter LogFilter, startIdx int, num int) (logs []*Log, total int64, err error) {
	if len(userIDs) == 0 {
		return []*Log{}, 0, nil
	}
	tx, err := filter.apply(LOG_DB.Where("logs.user_id IN ?", userIDs))
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error; err != nil {
		common.SysError(common.LogText("failed to count organization logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	if err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error; err != nil {
		common.SysError(common.LogText("failed to search organization logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}
	formatUserLogs(logs, startIdx)
	return logs, total, nil
}
```

`model/midjourney.go`: `TaskQueryParams`에 `UserIDs []int // fork: O3`; `GetAllTasks`와 `CountAllTasks`의 필터 체인에 `if len(queryParams.UserIDs) > 0 { query = query.Where("user_id IN ?", queryParams.UserIDs) } // fork: O3`.

- [ ] **Step 4: `service/org_user_export.go`**

`git show team:service/org_user_export.go > service/org_user_export.go` 후:
- `importedWalletQuota`는 `int`를 반환하고 `common.WalletQuotaFromDecimalStrict`(이미 int)를 그대로 쓴다; 오류 문구의 `common.MaxQuota` → `common.MaxWalletQuota`.
- team의 `setImportedUserQuota`(52-61줄)는 삭제한다 — develop `service/user_export.go`의 `setImportedUserQuota(userId, current, target int)`를 쓴다(`GrantUserQuota` 호출 제거).
- 가져오기 행 처리에서 `organization_role` 값은 `member`/`admin`만 허용하고 `owner`는 오류 행으로 남긴다(소유자는 조직 생성/양도로만). 글로벌 `role` 컬럼은 읽지 않는다(헤더에 없음).
- 새 사용자 생성 전 `common.Validate.Struct(user)`; 멤버 배정은 `model.AssignUserToOrganization(userId, organizationId, role)`; 잔액은 `setImportedUserQuota`.
- `int64` → `int` 캐스팅을 사용자 쪽에 적용.

- [ ] **Step 5: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./model/ -run TestGetLogsByUserIDs -v 2>&1 | tail -3 && GOWORK=off go test ./service/ -run 'ImportOrgUsers|ImportedWalletQuota' -v 2>&1 | tail -8
```

Expected: PASS.

- [ ] **Step 6: 커밋**

```bash
git add model/log_organization.go model/log_organization_test.go model/midjourney.go service/org_user_export.go service/org_user_export_test.go docs/fork-overlays.md
git commit -m "feat(organization): add member-scoped log queries and the organization member Excel service

Ported-From: team model/log.go model/midjourney.go service/org_user_export.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: 조직 컨트롤러·라우트·권한·접근 토큰·감사

**Files:**
- Create: `common/json_strict.go`, `controller/topup_target.go`, `controller/organization_validation.go`, `controller/organization.go`, `controller/organization_subscription.go`, `controller/organization_audit.go`, `router/organization-router.go`, `service/authz/resources_organization.go`, `router/organization_router_test.go`
- Modify: `router/api-router.go`(1), `middleware/fork_access_token_routes.go`(맵 항목), `router/access_token_scope_test.go`(`accessTokenHelperRoutes` 4), `controller/topup.go`(`rejectInvalidCreditedQuota`·`rejectInvalidTopUpQuota` 각 1, `&model.TopUp{` 1), `controller/topup_stripe.go`, `controller/topup_creem.go`, `controller/topup_waffo.go`, `controller/topup_waffo_pancake.go`, `controller/topup_paypal.go`(`&model.TopUp{` 각 1), `service/access_token_scope.go`(카탈로그 테스트가 요구할 때만 1)

**Interfaces:**
- Consumes: Task 1–4 전부; upstream `handlePermissionRoute(group, method, path, authz.Permission, handlers...)`, `permissionRoute`, `middleware.RootAuth/UserAuth/CriticalRateLimit`, `authz.RegisterResource`, `model.RecordOperationAuditLog(logUserId, actorRole int, content, ip, action string, params map[string]any, adminInfo *model.AuditAdminInfo, auditInfo *model.AuditRequestInfo, request ...*gin.Context)`, `auditOperatorInfo(c)`, `markAuditLogged(c)`, `tasksToDto(tasks, fillUser bool, viewerRole int)`, `logFilterFromQuery(c *gin.Context, admin bool) model.LogFilter`(`controller/log.go`).
- Produces: 핸들러 전부(team과 같은 이름: `CreateOrganization`, `ListOrganizations`, `UpdateOrganization`, `DeleteOrganization`, `GetOrganizationProfile`, `GetOrganizationDashboard`, `GetOrganizationLogs`, `GetOrganizationMidjourney`, `GetOrganizationTask`, `ListOrganizationUsers`, `ExportOrganizationUsers`, `ImportOrganizationUsers`, `ListAssignableOrganizationUsers`, `GetOrganizationUser`, `UpdateOrganizationUser`, `AssignOrganizationUser`, `RemoveOrganizationUserMembership`, `GetOrganizationWallet`, `GetOrganizationTopUps`, `RequestOrganization{Amount,Epay,StripeAmount,StripePay,PayPalAmount,PayPalPay,CreemPay,WaffoAmount,WaffoPay,WaffoPancakeAmount,WaffoPancakePay}`, 구독 8개); `authz.ResourceOrganization`, `authz.OrganizationRead`, `authz.OrganizationWrite`; `registerForkOrganizationRoutes(apiRouter *gin.RouterGroup)`; `recordOrganizationAudit(c *gin.Context, action string, content string, params map[string]any)`; `common.DecodeJsonStrict(r io.Reader, v any) error`; `validateTopUpTargetCapacity(c, userId, creditedQuota int) error`.
- 라우트(38 + 4): team `router/api-router.go` 228-282줄에서 Toss 2개와 auto-recharge 6개를 뺀 것.

- [ ] **Step 1: 라우트·권한 테스트**

`router/organization_router_test.go`:

```go
package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOrganizationRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerForkOrganizationRoutes(engine.Group("/api"))
	registered := map[string]bool{}
	for _, r := range engine.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for _, want := range []string{
		"GET /api/organizations/", "POST /api/organizations/", "PATCH /api/organizations/:id", "DELETE /api/organizations/:id",
		"GET /api/organization/", "GET /api/organization/dashboard", "GET /api/organization/logs", "GET /api/organization/mj", "GET /api/organization/task",
		"GET /api/organization/users", "GET /api/organization/users/export", "POST /api/organization/users/import",
		"GET /api/organization/assignable-users", "GET /api/organization/users/:id", "PATCH /api/organization/users/:id",
		"PUT /api/organization/users/:id/membership", "DELETE /api/organization/users/:id/membership",
		"GET /api/organization/subscription/self", "GET /api/organization/subscription/plans", "POST /api/organization/subscription/plans",
		"PATCH /api/organization/subscription/plans/:id", "DELETE /api/organization/subscription/plans/:id",
		"GET /api/organization/subscription/users", "PUT /api/organization/subscription/users/:id", "DELETE /api/organization/subscription/users/:id",
		"GET /api/organization/wallet", "GET /api/organization/topup/self",
		"POST /api/organization/pay", "POST /api/organization/amount",
		"POST /api/organization/stripe/pay", "POST /api/organization/stripe/amount",
		"POST /api/organization/paypal/pay", "POST /api/organization/paypal/amount",
		"POST /api/organization/creem/pay",
		"POST /api/organization/waffo/amount", "POST /api/organization/waffo/pay",
		"POST /api/organization/waffo-pancake/amount", "POST /api/organization/waffo-pancake/pay",
	} {
		require.True(t, registered[want], "missing route %s", want)
	}
	for _, gone := range []string{"POST /api/organization/toss/pay", "GET /api/organization/wallet/auto-recharge"} {
		require.False(t, registered[gone], "excluded route %s is registered", gone)
	}
	require.Equal(t, 42, len(registered))
	require.Equal(t, authz.Permission{Resource: "organization", Action: "write"}, authz.OrganizationWrite)
	_ = http.MethodGet
}
```

(`require.Equal(t, 42, …)`는 라우트 수 고정; 숫자가 다르면 누락/초과를 확인한다.)

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./router/ -run TestOrganizationRoutesRegistered 2>&1 | head -3
```

Expected: 컴파일 실패.

- [ ] **Step 3: 공통 보조 파일**

`common/json_strict.go`: team `common/json.go`의 `DecodeJsonStrict` 함수(23줄 부근)를 그대로 옮긴다(`// fork: O3` 주석; `encoding/json`의 `Decoder.DisallowUnknownFields`를 쓰는 래퍼이므로 Rule 1의 래퍼 범주).

`service/authz/resources_organization.go`:

```go
package authz

// fork: O3 — platform-level organization administration (root by default; no
// DefaultRoles means the admin built-in role does not receive it).
const ResourceOrganization = "organization"

var (
	OrganizationRead  = Permission{Resource: ResourceOrganization, Action: ActionRead}
	OrganizationWrite = Permission{Resource: ResourceOrganization, Action: ActionWrite}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceOrganization,
		LabelKey: "Organization Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read organizations", DescriptionKey: "List organizations, their owners and wallet balances."},
			{Action: ActionWrite, LabelKey: "Manage organizations", DescriptionKey: "Create, edit, fund and delete organizations."},
		},
	})
}
```

`controller/organization_audit.go`:

```go
package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — one operation audit entry per organization mutation. Content is
// English (upstream's auditContentTemplates is not edited); action names are
// "organization.<verb>". markAuditLogged suppresses the generic RootAuth entry
// for /api/organizations and is harmless on UserAuth routes.
func recordOrganizationAudit(c *gin.Context, action string, content string, params map[string]any) {
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), action, params, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
}
```

- [ ] **Step 4: 컨트롤러 복사·변환**

```bash
for f in controller/topup_target.go controller/organization_validation.go controller/organization.go controller/organization_subscription.go; do git show team:$f > $f; done
```

변환(모든 파일 공통):
- `i18n` import 제거; 위 변환표로 `ApiErrorI18n`/`TranslateMessage`를 바꾼다.
- `common.DecodeJsonStrict` 유지(Step 3).
- `organization_validation.go`: `MaxOrganizationQuota = common.MaxQuota` → `MaxOrganizationQuota int64 = int64(common.MaxWalletQuota)`.
- `int64`/`int` 경계: `user.Quota`(int)와 비교·대입하는 곳은 캐스팅. `go build`가 알려준다.
- `model.AssignUserToOrganizationWithBillingLifecycle` → `model.AssignUserToOrganization`; `UpdateOrganizationFieldsWithBillingLifecycle` → `UpdateOrganizationFields`; `model.UpdateUserFieldsWithBillingLifecycle(id, updates)` → `model.DB.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error` 뒤 `model.InvalidateUserCache(id)`.
- `GetOrganizationLogs`: `model.GetLogsByUserIDs(userIDs, logFilterFromQuery(c, false), pageInfo.GetStartIdx(), pageInfo.GetPageSize())`(develop `controller/log.go` `GetUserLogs`의 파라미터 파싱을 그대로 재사용). `GetOrganizationMidjourney`: `TaskQueryParams{…, UserIDs: userIDs}`; `GetOrganizationTask`: `tasksToDto(items, true, c.GetInt("role"))`.
- `GetOrganizationTopUps`: `model.GetOrganizationTopUps`/`SearchOrganizationTopUps` (Task 2).
- 결제 wrapper 11개(`RequestOrganization*`)는 그대로(각각 `prepareOrganizationTopUpTarget` 후 사용자 핸들러 호출). Toss·auto-recharge wrapper가 파일에 있으면 삭제.
- `ImportOrganizationUsers` 끝에 `recordOrganizationAudit(c, "organization.user.import", fmt.Sprintf("Imported organization %d members from Excel (assigned %d, created %d, updated %d, removed %d, errors %d)", …), map[string]any{…})`.
- 감사: `CreateOrganization`("organization.create", content `Created organization "<name>" (owner %d, quota %d)`), `UpdateOrganization`("organization.update"), `DeleteOrganization`("organization.delete"), `UpdateOrganizationUser`("organization.user.update", params에 user_id와 바뀐 필드), `AssignOrganizationUser`("organization.user.assign"), `RemoveOrganizationUserMembership`("organization.user.remove"), `CreateOrganizationSubscriptionPlan`/`Update`/`Delete`("organization.plan.create/update/disable"), `AssignOrganizationUserSubscription`("organization.subscription.assign"), `CancelOrganizationUserSubscription`("organization.subscription.cancel") — 성공 응답 직전 한 줄씩.
- `controller/topup_target.go`: `errors.New(common.TranslateMessage(c, i18n.MsgOrgOwnerRequired))` → `common.NewMessage("organization owner permission required")`. 파일 끝에 추가:
  ```go
  // validateTopUpTargetCapacity checks the wallet this order settles into.
  func validateTopUpTargetCapacity(c *gin.Context, userId int, creditedQuota int) error {
  	targetType := getTopUpTargetType(c)
  	targetId := userId
  	if targetType == model.TopUpTargetTypeOrganization {
  		targetId = getTopUpTargetId(c)
  	}
  	return model.ValidateTopUpTargetCapacity(targetType, targetId, creditedQuota)
  }
  ```

- [ ] **Step 5: upstream 훅(충전 target)**

- `controller/topup.go` `rejectInvalidCreditedQuota`와 `rejectInvalidTopUpQuota`: `err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)` → `err = validateTopUpTargetCapacity(c, userId, creditedQuota) // fork: O3`.
- `&model.TopUp{` 생성 6곳(`controller/topup.go` `RequestEpay`, `topup_stripe.go`, `topup_creem.go`, `topup_waffo.go`, `topup_waffo_pancake.go`, `topup_paypal.go`)에 `TargetType: getTopUpTargetType(c), TargetId: getTopUpTargetId(c), // fork: O3` 두 필드.

- [ ] **Step 6: 라우트·접근 토큰**

`router/organization-router.go`:

```go
package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// fork: O3 — organization routes. Platform administration (/api/organizations)
// is casbin-guarded; member/organization-admin routes (/api/organization) keep
// the fork's organization-role checks inside the handlers.
var organizationPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/", permission: authz.OrganizationRead, handler: controller.ListOrganizations},
	{method: http.MethodPost, path: "/", permission: authz.OrganizationWrite, handler: controller.CreateOrganization},
	{method: http.MethodPatch, path: "/:id", permission: authz.OrganizationWrite, handler: controller.UpdateOrganization},
	{method: http.MethodDelete, path: "/:id", permission: authz.OrganizationWrite, handler: controller.DeleteOrganization},
}

func registerForkOrganizationRoutes(apiRouter *gin.RouterGroup) {
	organizationsRoute := apiRouter.Group("/organizations")
	organizationsRoute.Use(middleware.RootAuth())
	for _, route := range organizationPermissionRoutes {
		handlePermissionRoute(organizationsRoute, route.method, route.path, route.permission, route.handler)
	}

	organizationRoute := apiRouter.Group("/organization")
	organizationRoute.Use(middleware.UserAuth())
	{
		organizationRoute.GET("/", controller.GetOrganizationProfile)
		organizationRoute.GET("/dashboard", controller.GetOrganizationDashboard)
		organizationRoute.GET("/logs", controller.GetOrganizationLogs)
		organizationRoute.GET("/mj", controller.GetOrganizationMidjourney)
		organizationRoute.GET("/task", controller.GetOrganizationTask)
		organizationRoute.GET("/users", controller.ListOrganizationUsers)
		organizationRoute.GET("/users/export", controller.ExportOrganizationUsers)
		organizationRoute.POST("/users/import", controller.ImportOrganizationUsers)
		organizationRoute.GET("/assignable-users", controller.ListAssignableOrganizationUsers)
		organizationRoute.GET("/users/:id", controller.GetOrganizationUser)
		organizationRoute.PATCH("/users/:id", controller.UpdateOrganizationUser)
		organizationRoute.PUT("/users/:id/membership", controller.AssignOrganizationUser)
		organizationRoute.DELETE("/users/:id/membership", controller.RemoveOrganizationUserMembership)
		organizationRoute.GET("/subscription/self", controller.GetMyOrganizationSubscription)
		organizationRoute.GET("/subscription/plans", controller.ListOrganizationSubscriptionPlans)
		organizationRoute.POST("/subscription/plans", controller.CreateOrganizationSubscriptionPlan)
		organizationRoute.PATCH("/subscription/plans/:id", controller.UpdateOrganizationSubscriptionPlan)
		organizationRoute.DELETE("/subscription/plans/:id", controller.DeleteOrganizationSubscriptionPlan)
		organizationRoute.GET("/subscription/users", controller.ListOrganizationUserSubscriptions)
		organizationRoute.PUT("/subscription/users/:id", controller.AssignOrganizationUserSubscription)
		organizationRoute.DELETE("/subscription/users/:id", controller.CancelOrganizationUserSubscription)
		organizationRoute.GET("/wallet", controller.GetOrganizationWallet)
		organizationRoute.GET("/topup/self", controller.GetOrganizationTopUps)
		organizationRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationEpay)
		organizationRoute.POST("/amount", controller.RequestOrganizationAmount)
		organizationRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationStripePay)
		organizationRoute.POST("/stripe/amount", controller.RequestOrganizationStripeAmount)
		organizationRoute.POST("/paypal/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationPayPalPay)
		organizationRoute.POST("/paypal/amount", controller.RequestOrganizationPayPalAmount)
		organizationRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationCreemPay)
		organizationRoute.POST("/waffo/amount", controller.RequestOrganizationWaffoAmount)
		organizationRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationWaffoPay)
		organizationRoute.POST("/waffo-pancake/amount", controller.RequestOrganizationWaffoPancakeAmount)
		organizationRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.RequestOrganizationWaffoPancakePay)
	}
}
```

`router/api-router.go`: `registerForkPublicRoutes(apiRouter)` 다음 줄에 `registerForkOrganizationRoutes(apiRouter) // fork: O3 router/organization-router.go`.

`middleware/fork_access_token_routes.go` 맵에 `/api/organization/*` 38개를 추가: 기본 `accessTokenSessionRule`; 예외 `"GET /api/organization/wallet"`, `"GET /api/organization/topup/self"`, `"POST /api/organization/amount"`, `"POST /api/organization/stripe/amount"`, `"POST /api/organization/paypal/amount"`, `"POST /api/organization/waffo/amount"`, `"POST /api/organization/waffo-pancake/amount"` → `accessTokenScopeRule("wallet:read")`; 모든 `.../pay` 6개 → `accessTokenScopeRule("wallet:write")`; `"GET /api/organization/subscription/self"` → `accessTokenScopeRule("profile:read")`. 키는 gin FullPath(끝 슬래시 포함: `"GET /api/organization/"`).

`router/access_token_scope_test.go` `accessTokenHelperRoutes`에 `/api/organizations/` 4개를 `(method, path, permission)` 형식으로 추가(기존 항목 형식을 따른다). `TestAccessTokenCatalogCoversEveryPermission`이 `organization` 리소스를 요구하면 `service/access_token_scope.go`의 카탈로그에 그 항목을 한 줄 추가하고 `// fork: O3` 주석.

- [ ] **Step 7: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go vet ./controller/ ./router/ ./middleware/ ./service/authz/ && GOWORK=off go test ./router/ -run 'TestOrganizationRoutesRegistered|TestAccessToken' -v 2>&1 | tail -12 && GOWORK=off go test ./service/authz/ ./middleware/ 2>&1 | tail -3
```

Expected: 라우트 테스트 PASS; `TestAccessTokenRouteRulesCoverEveryDashboardRoute`·카탈로그 테스트 PASS.

- [ ] **Step 8: 커밋**

`docs/fork-overlays.md`: api-router.go 1, controller/topup.go 3, topup_{stripe,creem,waffo,waffo_pancake,paypal}.go 각 1, access_token_scope_test.go 1, (필요 시) access_token_scope.go 1.

```bash
git add common/json_strict.go controller/ router/ middleware/fork_access_token_routes.go service/authz/resources_organization.go service/access_token_scope.go docs/fork-overlays.md
git commit -m "feat(organization): port the organization handlers, routes, casbin resource and audit entries

Ported-From: team controller/organization.go controller/organization_subscription.go controller/topup_target.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: 조직 컨트롤러 테스트 이식

**Files:**
- Create: `controller/organization_test.go`, `controller/organization_subscription_test.go`(team 복사+적응), `controller/organization_payment_test.go`(team `topup_paypal_capacity_test.go`의 조직 테스트 + `createPaymentCapacityOrganization` + `topup_quota_limit_test.go`의 `TestRequestEpayRejectsTopUpThatWouldOverflowOrganizationWallet`)

**Interfaces:**
- Consumes: Task 5 핸들러; develop `controller/topup_paypal_test.go`의 `setupPayPalCapacityTest`/`postPayPal` 패턴.

- [ ] **Step 1: 복사**

```bash
git show team:controller/organization_test.go > controller/organization_test.go
git show team:controller/organization_subscription_test.go > controller/organization_subscription_test.go
```

- [ ] **Step 2: 적응**

- `setupOrganizationControllerTestDB`: `setting.GetTossConfigSnapshot`, `ApplyTossOptionValuesWithRevision`, `restoreTossConfigForOptionTest` 호출과 `UserBillingKey`/`WalletAutoRecharge` 마이그레이션을 삭제; `common.TranslateMessage` override 삭제(메시지가 영문 리터럴); `common.UsingSQLite…` → `common.SetDatabaseTypes(...)`.
- 모든 `i18n.Translate(i18n.LangEn, i18n.MsgX[, args])` 단언을 변환표의 영문 리터럴로 바꾼다. 충전 상한 메시지는 `model.ErrTopUpQuotaLimitExceeded.Error()`.
- 삭제: `TestOrganizationDisableCancelsWalletBillingAndQueuesKeyRevocation`, `TestOrganizationAdminDisablingUserStopsTossRecurringBilling`.
- `int64` 사용자 잔액 → `int`.
- `performOrganizationRequest`가 `c.Set("id")`/`c.Set("role")`만 하는 구조는 유지.
- `controller/organization_payment_test.go`: `createPaymentCapacityOrganization(t, ownerId, orgId int, ownerQuota int, orgQuota int64) (model.User, model.Organization)`를 옮기고, `TestRequestOrganizationPayPalPayChecksOrganizationWalletCapacity`와 `TestRequestEpayRejectsTopUpThatWouldOverflowOrganizationWallet`을 develop의 sqlite 셋업(`setupPayPalCapacityTest` + `MigrateForkOrganizationTables`)으로 다시 쓴다. 단언은 `paymentError` 형식: `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`. `roundTripFunc`가 필요하면 이 파일에 정의.
- 추가 테스트(Review Focus 4):

```go
func TestListOrganizationUsersRootRequiresOrganizationId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	res := performOrganizationRequest(ListOrganizationUsers, root, http.MethodGet, "/api/organization/users", "")
	requireOrganizationApiError(t, res, "organization_id is required")
}
```

- [ ] **Step 3: 실행**

```bash
GOWORK=off go test ./controller/ -run 'Organization' -v 2>&1 | grep -E "^(=== RUN|--- FAIL|FAIL|ok|PASS)" | grep -c "FAIL"; GOWORK=off go test ./controller/ 2>&1 | tail -3
```

Expected: FAIL 0 (team 약 150개 + 신규); 패키지 ok. 실패하는 단언이 메시지 문구 차이면 리터럴을 고치고, 동작 차이면 Task 5 코드를 의심한다(team 동작이 기준).

- [ ] **Step 4: 커밋**

```bash
git add controller/organization_test.go controller/organization_subscription_test.go controller/organization_payment_test.go
git commit -m "test(organization): port the organization controller and payment capacity tests

Ported-From: team controller/organization_test.go controller/organization_subscription_test.go controller/topup_paypal_capacity_test.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: 백엔드 검증·기록

**Files:**
- Modify: `docs/fork-overlays.md`(정리), `docs/2026-10-14-rebase-phase-b.md`(새 기록, §1 백엔드)

- [ ] **Step 1: 전체 검증**

```bash
GOWORK=off go vet ./... && make test 2>&1 | grep -E "^(FAIL|ok)" | sort | uniq -c | sort -rn | head -3
```

Expected: FAIL 0.

- [ ] **Step 2: 기록 문서 시작**

`docs/2026-10-14-rebase-phase-b.md`:

```markdown
# upstream 재기반 단계 B(O3 조직 관리) 기록

브랜치: develop. 계획: docs/superpowers/plans/2026-10-14-rebase-phase-b.md

## 1. 백엔드 (Task 1–7)
- 커밋 표 (Task | 커밋 | 내용)
- 검증: go vet / make test 결과
- upstream 훅 수: docs/fork-overlays.md O3 행 수
- MySQL/PostgreSQL 실행: 미실행(컨테이너 없음). 실행 명령: `TEST_MYSQL_DSN=… TEST_POSTGRES_DSN=… GOWORK=off go test ./service/ ./model/ ./controller/ -run Organization`

## 2. 프런트엔드 (Task 8–12)
## 3. 결정 사항
## 4. 단계 C로 넘기는 것
```

§1을 실제 값으로 채운다.

- [ ] **Step 3: 커밋**

```bash
git add docs/2026-10-14-rebase-phase-b.md docs/fork-overlays.md
git commit -m "docs: record the organization backend port (phase B part 1)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: 프런트 기초 — 역할 헬퍼, 타입, API, 유틸, i18n

**Files:**
- Create: `web/src/lib/organization-roles.ts`, `web/src/features/organizations/api.ts`, `web/src/features/organizations/types.ts`, `web/src/features/organizations/lib/organization-selection.ts`, `web/src/features/organizations/lib/organization-subscription-utils.ts`, `web/src/features/organizations/lib/organization-wallet-payment.ts`, `web/src/features/organizations/lib/__tests__/organization-selection.test.ts`, `web/src/features/organizations/lib/__tests__/organization-subscription-utils.test.ts`, `web/src/features/organizations/lib/__tests__/organization-wallet-payment.test.ts`
- Modify: `web/src/stores/auth-store.ts`(`AuthUser`), `web/src/features/users/types.ts`(zod 스키마), `web/src/features/profile/types.ts`, `web/src/i18n/locales/en.json`(+sync)

**Interfaces:**
- Produces: `ORGANIZATION_ROLE = {MEMBER:'member', ADMIN:'admin', OWNER:'owner'}`, `hasOrganizationAdminRole(role?: string): boolean`, `hasOrganizationOwnerRole(role?: string): boolean`; `AuthUser.organization_id?: number`, `AuthUser.organization_role?: string`; team `api.ts`의 함수 전부(Toss 2개 제외: `calculateOrganizationTossAmount`, `requestOrganizationTossPayment`)와 `types.ts`의 타입 전부(Toss/auto-recharge 타입 제외); `getOrganizationWalletPaymentFlow(type): 'stripe'|'paypal'|'waffo_pancake'|'generic'`.

- [ ] **Step 1: 테스트 이식(vitest)**

```bash
mkdir -p web/src/features/organizations/lib/__tests__
git show team:web/default/src/features/organizations/lib/organization-selection.test.ts > web/src/features/organizations/lib/__tests__/organization-selection.test.ts
git show team:web/default/src/features/organizations/lib/organization-subscription-utils.test.ts > web/src/features/organizations/lib/__tests__/organization-subscription-utils.test.ts
git show team:web/default/src/features/organizations/lib/organization-wallet-payment.test.ts > web/src/features/organizations/lib/__tests__/organization-wallet-payment.test.ts
```

적응: `node:test`/`bun:test` import를 `import { describe, expect, test, beforeEach } from 'vitest'`로, `assert.equal(a,b)` → `expect(a).toBe(b)`, `assert.deepEqual` → `toEqual`; import 경로 `./x` → `../x`; wallet-payment 테스트의 Toss 케이스는 삭제하고 generic/stripe/paypal 케이스를 남긴다; 헤더 블록 추가.

- [ ] **Step 2: 실패 확인**

```bash
cd web && bun run test -- organizations 2>&1 | tail -5; cd ..
```

Expected: 모듈 없음으로 실패.

- [ ] **Step 3: 구현**

```bash
git show team:web/default/src/lib/organization-roles.ts > web/src/lib/organization-roles.ts
for f in api.ts types.ts lib/organization-selection.ts lib/organization-subscription-utils.ts lib/organization-wallet-payment.ts; do git show team:web/default/src/features/organizations/$f > web/src/features/organizations/$f; done
```

- `api.ts`: `/api/organization/toss/amount`, `/api/organization/toss/pay` 함수와 Toss 타입 import 삭제; `walletAutoRecharge` 관련이 있으면 삭제.
- `types.ts`: `TossAmountResponse`, `TossPaymentResponse`, `TopupAmountMode`, `TossTopupQuote`, `WalletAutoRecharge*` 참조 삭제.
- `organization-wallet-payment.ts`: `'toss'` 분기 삭제.
- `auth-store.ts` `AuthUser`에 `organization_id?: number` / `organization_role?: string` (`group?` 뒤, `// fork: O3`).
- `features/users/types.ts` 스키마 `group: z.string()` 뒤에 `organization_id: z.number().optional(), organization_role: z.string().optional(), // fork: O3`.
- `features/profile/types.ts` `UserProfile`에 같은 두 필드.
- 헤더 블록을 새 파일에 붙인다; import 그룹 공백 규칙.

- [ ] **Step 4: i18n**

team `features/organizations/**`(테스트 제외)와 사이드바·maintenance에서 쓰는 영문 키 중 `en.json`에 없는 것을 전부 추가한다. 추출 명령:

```bash
cd web && grep -rhoE "t\('([^']+)'" ../../new-api/web/default/src/features/organizations ../../new-api/web/default/src/hooks/use-sidebar-data.ts ../../new-api/web/default/src/features/system-settings/maintenance | sed -E "s/^t\('//; s/'$//" | sort -u > /tmp/org-keys.txt
node -e "const en=require('./src/i18n/locales/en.json'); const fs=require('fs'); const miss=fs.readFileSync('/tmp/org-keys.txt','utf8').split('\n').filter(k=>k && !(k in en)); console.log(miss.length); fs.writeFileSync('/tmp/org-missing.txt', miss.join('\n'))"
```

(`require` 결과가 `translation` 아래 중첩이면 `en.translation`으로.) `/tmp/org-missing.txt`의 키를 값=키로 `en.json`에 넣고(한글 리터럴 키 `조직에서 제거`는 `Remove from organization`으로 바꿔 컴포넌트 쪽도 Task 10에서 수정; `was` 조각 키는 Task 10에서 문장 키로 교체), 동적 키 `Last 7 days`, `Last 30 days`, `member`, `admin`, `owner`, `year`, `month`, `day`, `hour`, `custom`, `active`, `cancelled`, `expired`도 추가. `bun run i18n:sync`.

- [ ] **Step 5: 검증·커밋**

```bash
cd web && bun run test -- organizations 2>&1 | tail -5 && bun run typecheck && bunx oxlint -c .oxlintrc.json src/lib/organization-roles.ts src/features/organizations src/stores/auth-store.ts && cd ..
git add web/src/lib/organization-roles.ts web/src/features/organizations web/src/stores/auth-store.ts web/src/features/users/types.ts web/src/features/profile/types.ts web/src/i18n/locales
git commit -m "feat(web): add the organization API client, types, helpers and translations

Ported-From: team web/default/src/features/organizations
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: 공용 컴포넌트 확장(지갑 카드·결제 내역·충전 폼·차트·usage-logs 조직 범위)

**Files:**
- Modify: `web/src/features/wallet/components/wallet-stats-card.tsx`(+`balanceDisplay` prop), `web/src/features/wallet/components/dialogs/billing-history-dialog.tsx`(+`getBillingHistory`, `forceSelfHistory`, `description`), `web/src/features/wallet/hooks/use-billing-history.ts`(+옵션), `web/src/features/wallet/components/recharge-form-card.tsx`(+`showRedemption?: boolean`, 기본 true), `web/src/features/dashboard/lib/charts.ts`(`processUserChartData` 5번째 인자 `themeKey`), `web/src/features/usage-logs/{types.ts, api.ts, lib/utils.ts, index.tsx, components/usage-logs-table.tsx, components/common-logs-filter-bar.tsx, components/task-logs-filter-bar.tsx, columns/common-logs-columns.tsx, dialogs/details-dialog.tsx}`
- Create: `web/src/features/wallet/components/__tests__/wallet-stats-card-balance.test.tsx`(team `wallet-stats-card.test.tsx`의 `balanceDisplay` 케이스), `web/src/features/usage-logs/__tests__/organization-scope.test.ts`

**Interfaces:**
- Consumes: Task 8 `api.ts`(`getOrganizationLogs`, `getOrganizationMidjourneyLogs`, `getOrganizationTaskLogs`는 이 Task에서 `features/usage-logs/api.ts`에 추가), `organization-selection.ts`.
- Produces: `WalletStatsCard` props `{user, loading, balanceDisplay?: {value: string; description: string}}`; `BillingHistoryDialog` props `{open, onOpenChange, getBillingHistory?, forceSelfHistory?, description?}`; `useBillingHistory({initialPage?, initialPageSize?, getBillingHistory?, forceSelfHistory?})`; `RechargeFormCard.showRedemption`; `processUserChartData(…, themeKey?)`; `UsageLogs({logsScope?: 'user'|'organization'})`; `UsageLogsScope`; `UsageLogsTable` props `{logCategory, logsScope?, routeTo?, organizationId?, waitForOrganization?}`; usage-logs API `getOrganizationLogs/getOrganizationMidjourneyLogs/getOrganizationTaskLogs(params, organizationId)`; `billing_source === 'organization_subscription'`을 구독으로 표시.

- [ ] **Step 1: 테스트**

`wallet-stats-card-balance.test.tsx`: team `web/default/src/features/wallet/components/wallet-stats-card.test.tsx` 51-73줄의 `balanceDisplay` 케이스를 vitest/@testing-library로 옮긴다(렌더 후 `screen.getByText('Organization plan in use')` 확인; `balanceDisplay` 없을 때는 잔액 숫자 표시).

`organization-scope.test.ts`: `lib/utils.ts`에 새로 두는 순수 함수 `buildOrganizationLogsParams(params, logsScope, organizationId)`가 `logsScope:'organization'`, `organizationId: 7`일 때 결과에 `organization_id: 7`이 있고 `logsScope:'user'`일 때는 없음을 단언한다(기존 파라미터 빌더가 이 함수를 호출하도록 연결).

- [ ] **Step 2: 실패 확인**

```bash
cd web && bun run test -- wallet-stats-card-balance organization-scope 2>&1 | tail -5; cd ..
```

- [ ] **Step 3: 지갑·차트 확장**

- `wallet-stats-card.tsx`: team 26-59줄을 참고해 `balanceDisplay?: { value: string; description: string }` prop을 추가하고, 있으면 잔액 숫자 대신 `value`와 `description`을 렌더.
- `billing-history-dialog.tsx` + `use-billing-history.ts`: team 64-81줄 — `getBillingHistory?: (page, pageSize, keyword) => Promise<…>`(기본은 upstream의 사용자/관리자 선택), `forceSelfHistory?: boolean`(관리자여도 전체 내역이 아닌 전달된 함수 사용), `description?: string`(다이얼로그 설명 문구).
- `recharge-form-card.tsx`: `showRedemption?: boolean`(기본 `true`); `false`면 환급 코드 블록을 렌더하지 않는다.
- `charts.ts` `processUserChartData`: team 서명을 보고 5번째 인자 `themeKey?: string`을 추가(색상 프리셋 선택에만 쓰임; 생략 시 기존 동작).

- [ ] **Step 4: usage-logs 조직 범위**

team 파일의 조직 관련 hunk(§5 목록)를 develop 파일에 옮긴다. 각 변경에 `// fork: O3` 주석(또는 JSX 주석):
- `types.ts`: `UsageLogsScope = 'user' | 'organization'`, 파라미터 타입에 `organization_id?`, `scope?`, `organizationId?`, 로그 타입에 `organization_user_subscription_id?`.
- `api.ts`: `getOrganizationLogs(params)` → `/api/organization/logs`, `getOrganizationMidjourneyLogs` → `/api/organization/mj`, `getOrganizationTaskLogs` → `/api/organization/task`(upstream `fetchLogs` 헬퍼 재사용).
- `lib/utils.ts`: `organizationId` → `organization_id` 쿼리; 범위 dispatch에서 `useAdminParams = isAdmin || isOrganizationScope`.
- `index.tsx`: `UsageLogs({ logsScope = 'user' })` — upstream에 이미 `LogsViewScope`(`all|self`)와 `scope`라는 이름이 있으므로 prop 이름은 **`logsScope`**로 한다(Task 10의 라우트도 `<UsageLogs logsScope='organization' />`); 조직 범위면 제목 `Organization Usage Logs`/`Organization Task Logs`, `routeTo='/organization/usage-logs/$section'`, root에게만 조직 선택 셀렉터(`useQuery(['organizations','usage-logs-selector'])` + `organization-selection.ts`), 테이블에 `logsScope/routeTo/organizationId/waitForOrganization` 전달.
- `usage-logs-table.tsx`: props 확장, `isAdmin = userIsAdmin || logsScope === 'organization'`, 쿼리 `enabled`를 `!waitForOrganization`으로, `showStats={logsScope !== 'organization'}`.
- 두 filter-bar: `routeTo` 유니온에 `/organization/usage-logs/$section`.
- `common-logs-columns.tsx`, `dialogs/details-dialog.tsx`: `billing_source === 'organization_subscription'`을 구독으로 취급하고 `organization_user_subscription_id`를 상세에 표시.

- [ ] **Step 5: 검증·커밋**

```bash
cd web && bun run test -- wallet usage-logs dashboard 2>&1 | tail -6 && bun run typecheck && bunx oxlint -c .oxlintrc.json src/features/wallet src/features/usage-logs src/features/dashboard/lib/charts.ts && cd ..
git add web/src/features/wallet web/src/features/usage-logs web/src/features/dashboard/lib/charts.ts
git commit -m "feat(web): extend wallet, billing history and usage-logs components for organization scope

Ported-From: team web/default/src/features/wallet web/default/src/features/usage-logs
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: 조직 화면·라우트·사이드바

**Files:**
- Create: `web/src/features/organizations/index.ts`, `web/src/features/organizations/components/{organization-users-table.tsx, organization-dashboard.tsx, organization-subscriptions-page.tsx, org-users-import-dialog.tsx}`, `web/src/routes/_authenticated/organization/{index.tsx, dashboard.tsx, subscriptions.tsx, usage-logs/index.tsx, usage-logs/$section.tsx}`, `web/src/features/organizations/components/__tests__/organization-users-table.test.tsx`
- Modify: `web/src/hooks/use-sidebar-data.ts`(조직 그룹), `web/src/hooks/use-sidebar-config.ts`(`DEFAULT_SIDEBAR_MODULES.organization`, `URL_TO_CONFIG_MAP` 6), `web/src/hooks/use-sidebar-view.ts`(조직 그룹 가시성), `web/src/components/layout/types.ts`(없음 — 그룹 id로 판단), `web/src/features/system-settings/maintenance/config.ts`, `web/src/features/system-settings/maintenance/sidebar-modules-section.tsx`, `web/src/i18n/locales/en.json`(사이드바·maintenance 키)

**Interfaces:**
- Consumes: Task 8·9 전부; upstream 라우트 가드 패턴(`routes/_authenticated/users/index.tsx`의 `beforeLoad` + `useAuthStore.getState()`), `SectionPageLayout`, `AlertDialog`(`render=` 패턴; `asChild` 없음).
- Produces: 라우트 `/organization`, `/organization/dashboard`, `/organization/subscriptions`, `/organization/usage-logs/`(→ `/organization/usage-logs/common`), `/organization/usage-logs/$section`; 사이드바 그룹 `organization`(root 또는 조직 admin/owner에게만).

- [ ] **Step 1: 테이블 테스트**

`organization-users-table.test.tsx`(vitest + testing-library): `api`를 `vi.mock`해 `getOrganizationProfile`이 멤버 3명을 돌려주게 하고, (a) owner로 렌더하면 "Role" 컬럼과 역할 셀렉터가 보이고, (b) member 사용자(`organization_role:'member'`)로 렌더하면 add-member 섹션이 없음을 단언한다(auth-store를 `useAuthStore.setState`로 세팅). 셀렉터/텍스트 이름은 team 컴포넌트의 실제 라벨(`t('Role')`, `t('Assign Organization Member')`)을 쓴다.

- [ ] **Step 2: 컴포넌트·라우트 복사·적응**

```bash
for f in organization-users-table.tsx organization-dashboard.tsx organization-subscriptions-page.tsx org-users-import-dialog.tsx; do git show team:web/default/src/features/organizations/components/$f > web/src/features/organizations/components/$f; done
for f in index.tsx dashboard.tsx subscriptions.tsx usage-logs/index.tsx 'usage-logs/$section.tsx'; do mkdir -p "web/src/routes/_authenticated/organization/$(dirname "$f")"; git show "team:web/default/src/routes/_authenticated/organization/$f" > "web/src/routes/_authenticated/organization/$f"; done
```

적응:
- `organization-users-table.tsx`: `AlertDialogTrigger asChild` → upstream `render=` 패턴(`features/security/components/passkey-card.tsx` 참고); `t('조직에서 제거')` → `t('Remove from organization')`; `t('was')` 조각을 완전한 문장 키로 교체(예: `t('{{count}} changes were saved')` 형태로 바꾸고 키 추가); import 경로를 `@/features/organizations/...`로.
- `organization-dashboard.tsx`: `processUserChartData` 5번째 인자(Task 9); 차트 컴포넌트 props 동일.
- `organization-subscriptions-page.tsx`: 그대로(Toss 없음).
- `org-users-import-dialog.tsx`: 단계 A의 `users-import-dialog.tsx`처럼 `requireServerSuccess` 경로로 오류를 표시하고 파일 입력에 `aria-label`.
- 라우트: `beforeLoad`를 upstream 패턴(`useAuthStore.getState()` 동기 사용)으로; `usage-logs/$section.tsx`는 upstream의 `usage-logs/$section.tsx` 검색 스키마(타입 `0-7`)를 복사하고 `<UsageLogs logsScope='organization' />`.
- `features/organizations/index.ts`: 세 페이지 컴포넌트 export.

- [ ] **Step 3: 사이드바·설정**

- `use-sidebar-data.ts`: team 116-149줄의 `organization` 그룹(5 항목, `Building2`·`LayoutDashboard`·`FileText`·`ListTodo`·`CreditCard` 아이콘)을 `personal` 그룹 뒤에 추가(`// fork: O3`).
- `use-sidebar-config.ts`: `DEFAULT_SIDEBAR_MODULES.organization = { enabled, users, dashboard, subscription, log, task }`(모두 true), `URL_TO_CONFIG_MAP`에 team 126-137줄 6개.
- `use-sidebar-view.ts`: `userRole` 외에 `organization_role`을 선택하고, `group.id === 'organization'`이면 `isRoot || hasOrganizationAdminRole(organizationRole)`일 때만 표시(`// fork: O3`).
- `maintenance/config.ts` `SIDEBAR_MODULES_DEFAULT`와 `sidebar-modules-section.tsx`의 `sectionMeta`/`moduleMeta`에 team 80-91, 158-178줄의 `organization` 항목.
- 백엔드 기본값: Task 1에서 `generateDefaultSidebarConfig`에 `organization` 블록을 넣지 않았다면 `controller/user.go`와 `model/user.go`의 기본 사이드바 설정에 team 492-499줄 블록을 추가(`// fork: O3`) — 프런트 `DEFAULT_SIDEBAR_MODULES`와 키가 같아야 한다.
- i18n: `Organization`, `Organization Users`, `Organization Dashboard`, `Organization Usage Logs`, `Organization Task Logs`, `Organization Subscription`, `Organization area`, `Organization users, billing, and usage monitoring.`, `Manage organization members and roles.`, `Manage organization-only plans and assignments.`, `Review organization usage and quota trends.`, `Review organization API usage records.`, `Review organization task records.` + `bun run i18n:sync`.

- [ ] **Step 4: 검증**

```bash
cd web && bun run build 2>&1 | tail -3 && git status --short src/routeTree.gen.ts && bun run test -- organizations sidebar 2>&1 | tail -6 && bun run typecheck && bunx oxlint -c .oxlintrc.json src/features/organizations src/routes/_authenticated/organization src/hooks && cd ..
```

Expected: 빌드가 `routeTree.gen.ts`를 재생성(커밋 대상), 테스트 PASS, typecheck 0.

- [ ] **Step 5: 커밋**

```bash
git add web/src/features/organizations web/src/routes/_authenticated/organization web/src/routeTree.gen.ts web/src/hooks web/src/features/system-settings/maintenance web/src/i18n/locales controller/user.go model/user.go docs/fork-overlays.md
git commit -m "feat(web): add the organization users, dashboard and subscription pages with routes and sidebar entries

Ported-From: team web/default/src/features/organizations web/default/src/routes/_authenticated/organization
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: 조직 지갑·멤버 지갑 요약·지갑 라우트 분기

**Files:**
- Create: `web/src/features/organizations/components/organization-wallet.tsx`, `web/src/features/organizations/components/organization-member-wallet-summary.tsx`, `web/src/features/organizations/components/__tests__/organization-member-wallet-summary.test.tsx`
- Modify: `web/src/routes/_authenticated/wallet/index.tsx`(지갑 분기), `web/src/features/wallet/lib/ui.tsx`(`getPaymentIcon` PayPal case — 단계 A 이월)

**Interfaces:**
- Consumes: Task 8 API(`getOrganizationWallet`, `getOrganizationBillingHistory`, amount/pay 함수들), Task 9 props(`balanceDisplay`, `getBillingHistory`, `forceSelfHistory`, `showRedemption`), `useTopupInfo`, `requestPaymentAmount(topupAmount, type, calculators)`(주입 가능 계산기), `PaymentConfirmDialog`, `CreemConfirmDialog`, `useCreemPayment`/`useWaffoPayment`/`useWaffoPancakePayment`의 공개 API.
- Produces: `OrganizationWallet({initialShowHistory?})`, `OrganizationMemberWalletSummary()`, `getOrganizationMemberWalletBalanceDisplay(...)`; `/wallet`에서 owner → `OrganizationWallet`, 조직 비-owner → `OrganizationMemberWalletSummary`, 그 외 → `Wallet`.

- [ ] **Step 1: 테스트**

team `organization-member-wallet-summary.test.ts`(2 케이스: 활성 플랜이면 잔액 문구를 `Organization plan in use`로 대체, 없으면 기본)를 vitest로 옮긴다(`getOrganizationMemberWalletBalanceDisplay` 순수 함수 테스트).

- [ ] **Step 2: 복사·적응**

```bash
git show team:web/default/src/features/organizations/components/organization-wallet.tsx > web/src/features/organizations/components/organization-wallet.tsx
git show team:web/default/src/features/organizations/components/organization-member-wallet-summary.tsx > web/src/features/organizations/components/organization-member-wallet-summary.tsx
```

`organization-wallet.tsx`에서 제거(team 줄 번호): `loadTossPayments` import(20)와 `useTossPaymentLifecycle`(216), `topupAmountMode`/`tossQuote`/`tossPaymentConfirmation` 상태(196, 201-205, 215), `getOrganizationAmountModeRequest`(147-155), `getAmountForMinimumCheck`(282-291), `handleTopupAmountModeChange`(366), Toss 분기(408-421, 433-450, 470-475, 486-545, 588-591), `RechargeFormCard`/`PaymentConfirmDialog`의 Toss props(732-734, 861-869); auto-recharge: `canManageOrganizationAutoRecharge`(136-145), `buildOrganizationWalletPaymentSettingTabs`(169-181), `useWalletAutoRecharge`(226-229), `visibleAutoRechargeModes`/`paymentSettingTabs`/잠금 문구(661-700), "Payment settings" `TitledCard`(761-851). `shouldBlockOrganizationPaymentMethodBeforeQuote`는 `topupAmount < getMinTopupAmount(topupInfo)`로 대체. 금액 계산은 `requestPaymentAmount(amount, type, { regular: calculateOrganizationAmount, stripe: calculateOrganizationStripeAmount, paypal: calculateOrganizationPayPalAmount, waffo: …, waffoPancake: calculateOrganizationWaffoPancakeAmount })`로 주입. 결제 요청은 team처럼 조직 엔드포인트를 직접 호출(`requestOrganizationPayment` 등)하고 `getOrganizationWalletPaymentFlow`로 리다이렉트 방식을 고른다(PayPal은 `window.location.href`). `RechargeFormCard showRedemption={false}`. 결제 내역은 `BillingHistoryDialog getBillingHistory={getOrganizationBillingHistory} forceSelfHistory description={t('Organization wallet top-up history')}`.

`wallet/index.tsx`: team 79-86줄의 분기를 추가하되 검색 스키마는 `show_history`만 유지:

```tsx
const user = useAuthStore((s) => s.auth.user)
if (user?.organization_id && hasOrganizationOwnerRole(user.organization_role)) return <OrganizationWallet initialShowHistory={showHistory} />
if (user?.organization_id) return <OrganizationMemberWalletSummary />
return <Wallet initialShowHistory={showHistory} />
```

`features/wallet/lib/ui.tsx` `getPaymentIcon`: `case PAYMENT_TYPES.PAYPAL:` → `CreditCard` 아이콘 + `PAYMENT_ICON_COLORS[PAYMENT_TYPES.PAYPAL]`(Creem/Waffo case와 같은 형태).

- [ ] **Step 3: 검증·커밋**

```bash
cd web && bun run test -- organizations wallet 2>&1 | tail -6 && bun run typecheck && bun run build 2>&1 | tail -2 && bunx oxlint -c .oxlintrc.json src/features/organizations src/routes/_authenticated/wallet src/features/wallet/lib/ui.tsx && cd ..
git add web/src/features/organizations web/src/routes/_authenticated/wallet web/src/features/wallet/lib/ui.tsx web/src/i18n/locales web/src/routeTree.gen.ts
git commit -m "feat(web): add the organization wallet and member wallet summary

Ported-From: team web/default/src/features/organizations/components/organization-wallet.tsx
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 12: Playwright e2e 이식

**Files:**
- Create: `web/playwright.config.ts`, `web/e2e/organization.e2e.ts`, `web/e2e/README.md`
- Modify: `web/package.json`(devDependency `@playwright/test`, scripts `e2e`, `e2e:headed`, `e2e:install`), `web/bun.lock`, `.gitignore`(`web/test-results/`, `web/playwright-report/`)

**Interfaces:**
- Consumes: Task 10·11 화면; 백엔드 `go run . --port 3100`.
- Produces: `cd web && bun run e2e`로 조직 smoke 19개 실행.

- [ ] **Step 1: 설정 복사·적응**

```bash
git show team:web/default/playwright.config.ts > web/playwright.config.ts
mkdir -p web/e2e && git show team:web/default/e2e/organization.e2e.ts > web/e2e/organization.e2e.ts && git show team:web/default/e2e/organization.e2e.md > web/e2e/README.md
```

`playwright.config.ts`: 프런트 webServer `cwd`를 `web/`(현재 디렉터리), 백엔드 `cwd`를 `..`로; `bun run dev -- --host 127.0.0.1 --port <port>`가 rsbuild에서 통하는지 확인(`rsbuild dev --port`); 환경변수 이름 `VITE_REACT_APP_SERVER_URL`은 upstream rsbuild 설정의 서버 URL 변수명으로 바꾼다(`web/rsbuild.config.ts`의 `define`/`source.define` 확인); Chromium `executablePath`는 `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH`가 없으면 생략(Playwright 번들 사용). `organization.e2e.ts`: 라우트 경로는 Task 10과 동일(변경 없음); `localStorage` 키와 로그인 흐름이 upstream 인증(세션 쿠키 + 메모리 access token)과 맞는지 `signInWithUi`를 upstream 로그인 폼에 맞게 고친다(`POST /api/user/login` 응답 대기는 유지). 스크린샷 테스트(auto-recharge)는 가져오지 않는다.

- [ ] **Step 2: 의존성·스크립트**

```bash
cd web && bun add -d @playwright/test@^1.60.0 && bunx playwright install chromium 2>&1 | tail -2; cd ..
```

`package.json` scripts: `"e2e": "playwright test"`, `"e2e:headed": "playwright test --headed"`, `"e2e:install": "playwright install chromium"`.

- [ ] **Step 3: 실행**

```bash
cd web && E2E_ADMIN_USERNAME=root E2E_ADMIN_PASSWORD=123456 bun run e2e 2>&1 | tail -15; cd ..
```

(새 SQLite 백엔드가 `POST /api/setup`으로 초기화되는지 README의 절차를 따른다.) Expected: 19 passed. Chromium을 받을 수 없거나 백엔드 기동이 막히면 실패 로그를 기록 문서에 남기고 이 Task를 "미실행"으로 표시한다 — 설정 파일과 스펙은 그대로 커밋한다.

- [ ] **Step 4: 커밋**

```bash
git add web/playwright.config.ts web/e2e web/package.json web/bun.lock .gitignore
git commit -m "test(web): port the organization Playwright smoke suite

Ported-From: team web/default/e2e/organization.e2e.ts web/default/playwright.config.ts
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 13: 매뉴얼 이식·기록·병합 리허설

**Files:**
- Create: `docs/manuals/organization-management-manual.md`, `docs/manuals/organization-user-manual.md`, `docs/manuals/organization-test-guide.md`, `docs/images/organization-manual/*`(team 복사), `docs/openapi/organization-rest-api.md`
- Modify: `docs/2026-10-14-rebase-phase-b.md`, `docs/fork-overlays.md`, `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §4.4

- [ ] **Step 1: 전체 검증**

```bash
GOWORK=off go vet ./... && make test 2>&1 | grep -c "^FAIL"; cd web && bun run typecheck && bun run test 2>&1 | grep -E "Test Files|Tests " && bun run build 2>&1 | tail -1 && cd ..
```

Expected: FAIL 0, 테스트 전부 통과, 빌드 성공.

- [ ] **Step 2: 매뉴얼 복사·정정**

```bash
git checkout team -- docs/manuals/organization-management-manual.md docs/manuals/organization-user-manual.md docs/manuals/organization-test-guide.md docs/images/organization-manual docs/openapi/organization-rest-api.md
```

정정: Toss·자동충전·정기결제 절과 스크린샷 참조 삭제; `organization-test-guide.md` 시나리오 C("조직 관리자는 멤버 추가 불가")를 코드 동작(관리자도 member로 추가 가능, member↔admin 전환 가능)에 맞게 고침; 상한 표기 5e14 → 2^53-1; 환급 코드/주소 등 upstream 경로 변경 사항 반영.

- [ ] **Step 3: 수동 시나리오 실행 기록**

`docs/2026-10-14-rebase-phase-b.md` §2에 이 계획의 "Review Focus"와 매뉴얼의 핵심 시나리오(조직 생성→멤버 추가→지갑 충전→멤버 요청→대시보드/로그→플랜 할당→플랜 과금→취소→삭제)를 로컬 SQLite 서버(`make dev-api` 또는 `go run .` + `cd web && bun run dev`)에서 직접 수행한 결과를 표로 남긴다. 결제 provider 호출은 설정 미완료 상태에서 "비활성 표시"까지만 확인한다.

- [ ] **Step 4: upstream 병합 리허설**

```bash
git fetch qn && git merge --no-commit --no-ff qn/main 2>&1 | tail -3; git diff --name-only --diff-filter=U; git merge --abort 2>/dev/null || true; git status --short | wc -l
```

충돌 파일 목록이 `docs/fork-overlays.md`의 upstream 파일 집합 안에 있어야 한다. 결과를 기록 §4에.

- [ ] **Step 5: spec·기록 마무리**

- spec §4.4에 결정 1–10 중 spec과 다른 것(결제 6종, 스코프 매핑, 감사 항목, 캐시 미포함)을 한 줄씩 반영.
- 기록 §3 "결정 사항"(이 계획 상단 1–10 + 실행 중 추가), §4 "단계 C로 넘기는 것"(조직 할인 표시, 조직 로그 상세의 타이밍 등 C 범위; MySQL/PostgreSQL 실행; e2e 미실행 시 그 항목).
- `docs/fork-overlays.md` 줄 번호 재검증(`grep -n "fork: O3"`).

- [ ] **Step 6: 커밋**

```bash
git add docs/
git commit -m "docs: record phase B of the upstream rebase and port the organization manuals

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```
