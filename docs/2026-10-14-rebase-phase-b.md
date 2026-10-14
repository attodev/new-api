# upstream 재기반 단계 B(O3 조직 관리) 기록

브랜치: develop. 계획: docs/superpowers/plans/2026-10-14-rebase-phase-b.md

## 1. 백엔드 (Task 1–7)

`git log --format='%h %s' 149c567a1..HEAD` 기준(오래된 순). Task 번호는 제목으로 추정했다.

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | 37d4659f7 | feat(organization): port the organization, plan and subscription models onto upstream |
| 2 | aad37212b | feat(organization): settle top-ups into the organization wallet by target |
| 2 | 0b4bfee8e | fix(organization): refuse to settle organization orders without a target |
| 3 | 5c2bb1301 | feat(organization): charge organization members against the member limit, organization wallet and organization plans |
| 3 | a16f417b7 | fix(organization): reserve organization funds atomically and bill legacy Midjourney against the organization |
| 3 | d1886beea | fix(organization): reject legacy Midjourney requests from plan members and empty organization wallets before the upstream call |
| 4 | 9a664cd4a | feat(organization): add member-scoped log queries and the organization member Excel service |
| 4 | 6f1d15b89 | fix(organization): keep the member group read-only on import and protect platform admins from remove-absent |
| 5 | b09a802ab | feat(organization): port the organization handlers, routes, casbin resource and audit entries |
| 5 | 4b8456c8a | fix(organization): close self-service quota, owner-targeting, hash-disclosure and order-by holes in the organization API |
| 5 | dd71f5dce | fix(organization): let root manage the owner's plan and scope membership clears to the organization |
| 6 | 5dfb77f71 | test(organization): port the organization controller and payment capacity tests |
| 7 | 99540d545 | docs: record the organization backend port (phase B part 1) |

- 검증: `GOWORK=off go vet ./...` 종료 코드 0(출력 없음). `make test` 종료 코드 0, 패키지 100개(루트 + relaykit + tokenkit 합산) — `ok` 54, `no test files` 46, FAIL 0. 단계 A 기준선(54/46/0)과 같은 패키지 구성이며, 조직 테스트는 기존 패키지(model, service, controller, router, service/authz)에 추가됐다.
- upstream 훅 수: `docs/fork-overlays.md`의 O3 행 54개(`grep -c "O3" docs/fork-overlays.md`). 모든 O3 행의 줄 번호를 트리와 대조했고 어긋난 행은 없었다.
- MySQL/PostgreSQL 실행: 미실행(컨테이너 없음). 실행 명령: `TEST_MYSQL_DSN=… TEST_POSTGRES_DSN=… GOWORK=off go test ./service/ ./model/ ./controller/ -run Organization`

## 2. 프런트엔드 (Task 8–12)와 수동 시나리오 (Task 13)

`git log --format='%h %s' 99540d545..HEAD` 기준(오래된 순).

| Task | 커밋 | 내용 |
|---|---|---|
| 8 | 7af2d429e | feat(web): add the organization API client, types, helpers and translations |
| 8 | aef0e7d8c | fix(web): surface organization API failures and narrow the assignable roles |
| 9 | bb7d4fab1 | feat(web): extend wallet, billing history and usage-logs components for organization scope |
| 10 | ade000359 | feat(web): add the organization users, dashboard and subscription pages with routes and sidebar entries |
| 10 | adc4f9acc | fix(web): fix organization page headers, root import/export and owner plan targets |
| 11 | b65e7895b | feat(web): add the organization wallet and member wallet summary |
| 11 | 66900791b | fix(web): tighten the organization Waffo flow and drop an unused key |
| 12 | 57005448f | test(web): port the organization Playwright smoke suite |
| 12 | 971c18089 | test(web): keep the e2e sign-in on the target page and fence the backend env |
| 13 | (이 커밋) | docs: record phase B of the upstream rebase and port the organization manuals |

훅 위치는 모두 `docs/fork-overlays.md`의 "O3 조직 (프런트)"·"O3 조직 사이드바"·"O3 조직 e2e" 행에 있다. 아래 줄 번호는 HEAD 971c18089 기준 `// fork: O3` 표시 줄이다.

### Task 8 — API 클라이언트·타입·헬퍼·번역
- 그대로 이식: `features/organizations/{api.ts,types.ts}`, `lib/organization-roles.ts`, `features/organizations/lib/{organization-selection,organization-subscription-utils}.ts`. 다시 만든 것: `organization-wallet-payment.ts`(Toss flow 삭제, stripe/paypal/waffo_pancake/generic), `api.ts`의 Toss 함수 2개 삭제, 삭제·가져오기 응답의 `requireServerSuccess`, 내보내기 JSON blob 오류 처리, 할당 payload에서 `owner` 제외.
- upstream 훅: `stores/auth-store.ts:39-40`, `features/profile/types.ts:52-53`, `features/users/types.ts:49-50` (`organization_id`, `organization_role`).
- 번역: 영문 키 126개를 `en.json`에 넣고 `bun run i18n:sync`(다른 locale은 영문 fallback). `조직에서 제거` → `Remove from organization`, `was` → `(was {{amount}})`.
- 테스트: 조직 4파일 18개. 전체 vitest 192파일 / 2276개(수정 전 기준).
- 미룬 minor: 다른 locale 번역(단계 D), 조직 `/waffo/amount` 클라이언트 없음, `Organization.description` 타입 optional화.

### Task 9 — 공용 컴포넌트의 조직 범위 확장
- 지갑: `wallet-stats-card.tsx:31,60-61`(`balanceDisplay`), `use-billing-history.ts:33,44,58-62,81,107`(`getBillingHistory`, `forceSelfHistory`), `billing-history-dialog.tsx:57,62,75-77,93,116`(`description`), `recharge-form-card.tsx:84,115,520,568`(`showRedemption`), `dashboard/lib/charts.ts:709,744,749,767,782,810`(`themeKey`).
- 사용 로그: `types.ts:35,282,444,488,502,519-520`, `api.ts:173`, `lib/utils.ts:29-31,44,241,268,285-286,310,313,325`, `log-cost-display.tsx:85`, `dialogs/details-dialog.tsx:493,1286`, `usage-logs-provider.tsx:25,28,51,61,96-98,125`, `index.tsx`(16줄), `usage-logs-table.tsx`(11줄), `common-logs-filter-bar.tsx`(15줄), `task-logs-filter-bar.tsx`(7줄), `common-logs-stats.tsx:31,52`, `dialogs/export-logs-dialog.tsx:40,52`.
- 다시 만든 것: team의 prop 전달 대신 `UsageLogsProvider` 컨텍스트(`logsScope`, `organizationId`, `waitForOrganization`)로 범위를 넘기고, 경로에 묶인 `getRouteApi` 6곳을 fork 파일 `usage-logs-route.ts`의 non-strict search로 바꿨다(§3 결정). 새 fork 파일: `usage-logs-route.ts`, `lib/organization-logs-selection.ts`, `organization-logs-selector.tsx`.
- 테스트: 새 3파일 8개. 전체 vitest 196파일 / 2289개.
- 미룬 minor: provider 테스트에 관리자+조직 범위 경우 없음, `balanceDisplay` 테스트가 값 치환을 확인하지 않음, 조직 전환 직후 이전 조직 행이 잠깐 보임(`placeholderData`).

### Task 10 — 조직 화면·라우트·사이드바
- 이식(fork 파일): `organization-users-table.tsx`, `organization-dashboard.tsx`, `organization-subscriptions-page.tsx`, `org-users-import-dialog.tsx`, `lib/organization-access.ts`, 라우트 `routes/_authenticated/organization/{index,dashboard,subscriptions}.tsx`, `usage-logs/{index,$section}.tsx`(5개, `routeTree.gen.ts` 재생성).
- 다시 만든 것: owner 행·자기 행 잠금(`isRowEditable`), Role 선택지 member/admin만(소유자에게만 표시), root는 구성원 행 읽기 전용, 대기 배너에 역할 변경 수, root Export/Import(선택 조직), 소유자가 아닌 관리자에게 owner의 플랜 할당·취소 숨김, AGPL 헤더 1행.
- upstream 훅: `hooks/use-sidebar-data.ts:22,132-168`, `use-sidebar-config.ts:69-77,132-144`, `use-sidebar-view.ts:25,52,61-68,75`, `system-settings/maintenance/config.ts:86-94`, `sidebar-modules-section.tsx:84-88,171-193`, `usage-logs/components/columns/common-logs-columns.tsx:396,403-407,561,570`, `task-logs-columns.tsx:131,139,260-266`, `common-log-mobile-card.tsx:67,377`; Go 사이드바 기본값 `controller/user.go:585`, `model/user.go:253`.
- 테스트: vitest 조직 7파일 27개, 전체 200파일 / 2305개. Go `TestDefaultSidebarConfigIncludesOrganizationSection`, `TestGenerateDefaultSidebarConfigIncludesOrganizationSection`.
- 미룬 minor: Role 헤더 단언이 변별력 없음, 선택지에 owner가 없다는 테스트 없음, 라우트 `beforeLoad` 직접 테스트 없음, 대시보드 기간 Select가 Base UI에서 원값을 보일 수 있음(브라우저 확인 필요).

### Task 11 — 조직 지갑·구성원 지갑 요약·`/wallet` 전환
- 이식: `organization-wallet.tsx`(Toss·자동충전·결제 설정 탭 삭제), `organization-member-wallet-summary.tsx`, `lib/wallet-view.ts`, `lib/member-wallet-balance.ts`.
- 다시 만든 것: provider별 결제 흐름을 `getOrganizationWalletPaymentFlow` switch로 정리(6종), 모든 provider에 확인 창(Waffo 제외), Stripe/PayPal 링크 http(s) 검사, 리다이렉트 대상 없으면 실패 토스트, 백엔드 `data` 문구 표시, Waffo 최소 금액 검사.
- upstream 훅: `routes/_authenticated/wallet/index.tsx:22-24,26,39-47`(O3), `features/wallet/lib/ui.tsx:129-135`(PayPal 아이콘, O2).
- 테스트: 새 3파일(+수정 1건) — 조직·지갑 15파일 51개. 전체 vitest 203파일 / 2314개.
- 미룬 minor: Stripe 새 탭·Waffo Pancake 리다이렉트·generic epay 폼·리다이렉트 없음 토스트 분기 테스트 없음, `/wallet` 라우트 컴포넌트 렌더 테스트 없음.

### Task 12 — Playwright 스모크
- 이식: `web/playwright.config.ts`, `web/e2e/organization.e2e.ts`, `web/e2e/README.md`.
- 다시 만든 것: upstream 쿠키 세션에 맞춘 로그인(localStorage 복원 삭제), 기본 페이지 `/dashboard`, 사용자 삭제의 보안 확인(`completeSecureVerification`), 기본 계정 root/12345678, 백엔드 env 차단(`SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING`), `E2E_REUSE_SERVER`.
- upstream 파일: `web/package.json:19-21,93`, `web/bun.lock`, `.gitignore:40-41` (Task 13에서 `docs/fork-overlays.md`에 행 추가).
- 테스트: 19 passed(48.3s, 가짜 DSN으로 env 차단 확인).
- 미룬 minor: `/wallet` 스모크 없음, member의 "조직 링크 없음" 검사에 긍정 기준점 없음·프리셋 테스트가 버튼이 없으면 skip, teardown 실패가 통과를 막지 않음, 빈 DB는 `POST /api/setup` 수동 1회 필요.

### 수동 시나리오 실행 (Task 13, 2026-10-14)

환경: HEAD 971c18089를 `go build`한 바이너리, `$TMPDIR/new-api-e2e.db` 새 SQLite, `POST /api/setup`(root/12345678), rate limit 3종 끔, `bun run dev`(3101). 먼저 `E2E_REUSE_SERVER=1 bun run e2e`로 19개를 돌리고(**19 passed, 44.4s**), 같은 서버에서 계정 `s_owner`/`s_admin`/`s_member`/`s_plan`과 조직 `Scenario Org`를 API(`curl`, Bearer 세션 토큰)로 만들어 나머지를 확인했다. relay 확인용으로 로컬 OpenAI 호환 mock(usage 1000/1000 토큰 고정)을 채널로 등록했다. 이 장비의 디스크 사용률이 97.8%라 upstream 성능 보호가 relay를 `system_disk_overloaded`(503)로 막아서, 테스트 DB에서만 `performance_setting.monitor_enabled=false`로 껐다. 페이지 확인 2건은 저장소에 넣지 않은 일회용 Playwright 스크립트로 했다. 결제 provider는 설정하지 않았다(비활성 표시까지만).

| # | 시나리오 | 방법 | 결과 |
|---|---|---|---|
| 1 | root가 조직 생성(owner 지정) | E2E #1(UI) + `POST /api/organizations/` | 통과 |
| 2 | 멤버 추가 — owner가 admin·member 추가, **admin도 member 추가** | E2E #1 + `PUT .../membership` | 통과 |
| 3 | 역할 변경 member↔admin(admin이 API로), `owner` 부여 거부, member의 추가 거부 | E2E #1(승격) + API | 통과 (`ownership is assigned when the organization is created`, `organization admin permission required`) |
| 4 | 자기 자신·owner 보호 | API: admin 자기 quota, admin→owner quota, admin→owner 제거 | 통과 (각각 `...cannot modify their own membership`, `only the organization owner can manage the owner`, `organization owner cannot be removed`) |
| 5 | 조직 지갑 충전(관리자 API) | `PATCH /api/organizations/2 {"quota":5000000}` → owner `GET /api/organization/wallet` | 통과 (0 → 5,000,000) |
| 6 | 결제 provider 미설정 = 비활성 표시 | `GET /api/user/topup/info`(enable_* 전부 false), owner `/wallet` 화면, `POST /api/organization/{pay,stripe/pay,paypal/pay}` | 통과 (화면 `Online topup is not enabled...`, API는 각각 `Payment information has not been configured by administrator` / `Failed to start payment` / `Unsupported payment channel`, member는 `organization owner permission required`) |
| 7 | owner `/wallet` = 조직 지갑 | 일회용 Playwright | 통과 (`Organization wallet` / `Scenario Org` / `Current Balance $10`, 교환 코드 없음) |
| 8 | member·admin `/wallet` = 구성원 요약 | 일회용 Playwright | 통과 (`Organization allocated quota`, `Topups are managed by your organization`, `No organization plan is assigned.`) |
| 9 | 멤버 요청이 회원 한도 + 조직 지갑에 과금 | mock 채널 relay 1회 | 통과 (s_member quota 1,000,000 → 999,625, 조직 5,000,000 → 4,999,625, used_quota 375) |
| 10 | 조직 지갑 0이면 멤버 요청 거부 | 조직 quota 0으로 바꾼 뒤 relay | 통과 (HTTP 403 `organization quota insufficient, remaining: ＄0.000000`, 회원 quota 변화 없음) |
| 11 | 조직 사용 로그 | E2E #5(UI) + owner `GET /api/organization/logs`, root `?organization_id=2`, member | 통과 (owner·root 1건 `s_member gpt-4o-mini 375`, member 거부) |
| 12 | 조직 대시보드 | E2E #13(UI) + `GET /api/organization/dashboard` | 통과 — 요청 직후 `period_quota` 0, 약 5분 뒤(upstream `quota_data` 집계 주기) `period_quota` 1125·요청 3·top user 2명 |
| 13 | 플랜 생성·할당 | E2E #16, #17 + admin `POST .../subscription/plans`, `PUT .../subscription/users/8` | 통과 |
| 14 | 플랜 과금 | 플랜 회원 relay 1회 | 통과 (개인 quota 1,000,000 그대로, 구독 `amount_used` 375, 조직 지갑 −375) |
| 15 | 플랜 취소 | admin `DELETE .../subscription/users/8` 후 relay | 통과 (status `cancelled`, `subscription/self` null, 다음 요청은 회원 quota·조직 지갑에서 375씩) |
| 16 | 멤버 제거 | E2E #19(UI) + API | 통과 (제거된 회원 quota 999,625 → 0) |
| 17 | 조직 삭제 | E2E #19(UI) + `DELETE /api/organizations/2` | 통과 (구성원이 남으면 `organization has 2 non-owner members, remove them first`, 제거 후 삭제 성공, 4명 모두 조직 0·quota 0) |
| 18 | member의 조직 관리 화면 차단 | E2E #6 | 통과 |
| 19 | 감사 로그 | root `GET /api/audit` | 통과 (`organization.create/update/delete`, `organization.user.assign/update/remove`, `organization.plan.create`, `organization.subscription.assign/cancel`) |
| 20 | Review Focus 1–5 | 자동 테스트 6개 재실행(`-count=1`) | 통과 (`TestOrganizationSubscriptionBillingRejectsWhenOrganizationWalletInsufficient`, `TestCreditTopUpTargetOrganizationStopsAtWalletCeiling`, `TestOrganizationWalletFundingPinsOrganizationAcrossMembershipChange`, `TestTaskRefundUsesPinnedOrganization`, `TestListOrganizationUsersRootRequiresOrganizationId`, `TestImportOrgUsersNeverEscalatesRoles`) |
| 21 | 실제 결제 provider 결제·콜백 정산 | — | 미실행 — provider 계정·sandbox 없음. 정산 경로는 Task 2 테스트로만 확인(§4) |
| 22 | 레거시 Midjourney·비동기 작업 과금 | — | 미실행 — MJ/작업 upstream이 없음. Task 3 테스트로만 확인 |
| 23 | MySQL/PostgreSQL에서 같은 흐름 | — | 미실행 — 컨테이너 런타임 없음(§4) |

결함 1건: 시나리오 13 중 `GET /api/organization/subscription/users` 응답의 `user`가 `users` 행 전체여서 **구성원의 비밀번호 해시(`$argon2id$...`)가 조직 관리자에게 노출된다**(team에서 물려받음, `model.OrganizationUserSubscriptionSummary.User *User`). Task 5가 고친 목록 API 두 개와 같은 종류다. §4 첫 항목.

## 3. 결정 사항

계획 단계(계획 상단 1–10):
- 조직 지갑 결제는 6종(Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake)이다. spec의 5종 표기는 Waffo Pancake가 빠진 것이었다.
- 환급은 지갑 상한을 검사하지 않는다(team 정책). 새 잔액이 들어오는 길(충전, root 수정, 가져오기)만 `MaxWalletQuota`(2^53-1)를 지킨다.
- 조직 필드는 사용자 캐시에 넣지 않는다. `GetSelfUserById` 컬럼과 `buildSelfUserData`에만 추가한다.
- `/api/organization/*` 접근 토큰 규칙: 기본은 세션 전용이고, 지갑 조회·충전만 wallet:read/write, subscription/self는 profile:read다. `/api/organizations/*`는 casbin `organization` 리소스를 쓴다.
- 조직 변경 핸들러 12개에 `organization.*` 감사 항목을 남긴다(upstream `auditContentTemplates`는 건드리지 않는다).
- 충전 캐시 동기화(`syncCreditUserQuotaCache`)는 사용자 target일 때만 한다. 조직 지갑에는 캐시가 없다.
- 조직 로그 조회는 upstream `GetUserLogs`를 따라 ClickHouse에서도 안전하게 만든다(`user_id IN ?` + `LogFilter.apply`).
- 조직 Excel 가져오기는 단계 A 사용자 가져오기 규칙을 따른다: 전역 역할은 바꾸지 않고, 새 행은 검증하며, 잔액은 지갑 상한 안, 요청당 감사 1건.
- 로그인·`/api/user/self` 응답에 `organization_id`, `organization_role`을 넣는다(사이드바·지갑 분기용).
- Playwright는 `web/` devDependency로 넣고 team 조직 e2e를 이식한다. 이 장비에서 Chromium을 받아 실행했다.

실행 중 추가(ledger Ruling, 오래된 순):
- 컨테이너 런타임이 없어 MySQL/PostgreSQL DSN 테스트는 운영자 작업으로 기록한다(§4). 조직 SQL은 모두 GORM이라 dialect 위험은 낮게 본다.
- 조직 target이 없는 조직형 충전 주문은 정산을 거부한다. `EffectiveTargetId`가 결제자 id로 되돌아가지 않는다.
- 이미지 수량 예약과 조직 구독 선차감은 조직 지갑을 원자적으로 예약(`TryReserveOrganizationQuota`)하고, 부족하면 403을 돌려준다(team은 음수 잔액을 허용했다). 선차감 실패 시 선차감 기록을 되돌리고, 지갑 환급은 플랜 환급 실패와 무관하게 하며, 실패한 보정은 로그로 남긴다.
- 레거시 Midjourney는 upstream 호출 전에 조직 플랜 회원과 빈 조직 지갑을 거부한다(`relay/mjproxy_handler.go` 훅 2줄). 처음의 "upstream과 같이 호출 후 거부" 결정은 회원 한도 과금이 빠지면서 무제한 호출이 되므로 이 결정으로 대체했다. 과금은 회원 한도 + 조직 지갑, 환급은 현재 소속 조직 기준이다.
- 조직 Excel 가져오기는 `group`을 쓰지 않고(내보내기 전용), `owner` 행은 오류로 처리하며, 없는 멤버 제거(remove-absent)는 플랫폼 관리자를 건드리지 않는다.
- 라우트 수는 38이다(계획의 42는 계산 오류: team 46개 − 제외 8개).
- 조직 API 보안 수정("team 검사 유지"의 예외): 자기 자신 수정·제거 금지, owner 대상 변경은 owner만, `owner` 역할은 membership PUT으로 부여할 수 없음(소유권 이전은 root 작업으로 미룸), 멤버 제거·조직 삭제 시 떠나는 사용자의 `users.quota`를 0으로 만들고(감사 로그에 previous_quota), 목록 API는 제한된 컬럼만 조회하며, `ORDER BY`는 dialect별 quoting을 쓴다. 결제 래퍼는 `paymentError` 형식, 조직 로그는 관리자 필터(`logFilterFromQuery(c, true)`), 멤버십 PUT은 strict JSON.
- root는 owner의 플랜을 할당·취소할 수 있다(owner 대상 규칙의 예외는 구독 할당·취소만). `ClearOrganizationMembership`은 요청한 조직 범위로 제한한다. 관리자가 Excel로 자기 한도를 올리는 경로는 남겨 둔다(§4).
- 프런트 조직 사용 로그는 team의 prop 전달 대신 `UsageLogsProvider` 컨텍스트(`logsScope`, `organizationId`)로 범위를 넘기고 `getRouteApi`를 non-strict search로 바꾼다. upstream hunk가 더 적다.
- Task 10 리뷰 minor 3건은 게이트·UX에 영향이 있어 바로 고쳤다: 새 파일 AGPL 헤더 1행, root Export/Import를 선택 조직으로 연결, 소유자가 아닌 관리자에게 owner의 플랜 대상 숨김.
- 조직 Waffo는 확인 창 없이 `calculateOrganizationAmount`로 금액을 계산한다(team과 같음, `/api/organization/waffo/amount` 클라이언트 없음). 단계 E 결제 작업에서 다시 본다.
- e2e 기본 관리자 비밀번호는 `12345678`이다(upstream setup이 8–128자를 요구해 계획의 `123456`은 거부됨).
- e2e는 `E2E_ALLOW_EXTERNAL_DB=1`이 아니면 백엔드의 `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING`을 빈 값으로 덮어쓰고, `E2E_REUSE_SERVER=1`일 때만 기존 서버를 재사용한다.

Task 13에서 추가:
- team 매뉴얼 3개와 REST API 문서는 develop 동작에 맞게 고쳐 옮겼다(라벨은 영문 화면 그대로, 상한 2^53-1, 관리자 멤버 추가 가능, owner 역할 부여 불가, 자기/owner 보호, 제거·삭제 시 quota 0, 대시보드 집계 지연). 스크린샷 7장은 Toss·자동충전이 없어 그대로 두었다(team 한국어 화면이라는 표기를 붙임).
- `docs/fork-overlays.md`에 빠져 있던 upstream 파일 행을 추가했다: `.gitignore:40-41`, `web/package.json`·`web/bun.lock`(e2e), `web/src/i18n/locales/*.json`(공통).

## 4. 단계 C로 넘기는 것

단계 A §7에서 넘어온 조직 내보내기, PayPal 조직 target, `TopUp.TargetType/TargetId`, `getPaymentIcon` PayPal case는 단계 B에서 처리했다.

먼저 고칠 것:
- **구독 할당 목록의 민감 컬럼 노출**: `GET /api/organization/subscription/users`(`model.ListOrganizationUserSubscriptions`)가 `user`로 `users` 행 전체(비밀번호 해시 포함)를 돌려준다. `organizationUserView`/제한 컬럼으로 바꾸고 테스트를 추가한다. `GET /api/organization/subscription/self`도 호출자 본인의 `User` 전체를 싣는다(본인 데이터라 위험은 낮다). 단계 C 전에 처리하기를 권한다.

범위·환경:
- 조직 할인 표시(spec §4.6 O6, 단계 C).
- 조직 로그 상세의 타이밍 표시(spec §4.7 O7, 단계 C).
- MySQL/PostgreSQL: `TEST_MYSQL_DSN=… TEST_POSTGRES_DSN=… GOWORK=off go test ./service/ ./model/ ./controller/ -run Organization`와 스키마 리허설 `./scripts/schema-rehearsal/run.sh all` 미실행(이 장비에 컨테이너 런타임 없음). 운영 DB 사본 리허설도 운영자 작업으로 남는다.
- upstream 병합 리허설: §5 참고. 충돌 집합 검사(충돌 파일 ⊆ `docs/fork-overlays.md`의 upstream 파일)는 실제로 해 보지 못했다. 첫 실제 upstream 동기화 때 다시 한다.
- 실제 결제 provider로 조직 지갑 결제·콜백 정산을 sandbox에서 확인(단계 E 결제 작업과 함께).
- Toss 결제와 지갑 자동충전·정기결제는 가져오지 않았다(spec §9 결정). Toss 데이터 테이블만 첫 릴리스에 남는다.

테스트 공백:
- E2E에 `/wallet`(owner 조직 지갑, 구성원 요약)과 구독 취소가 없다. 이번 수동 실행(§2 #7, #8, #15)으로 한 번 확인했다.
- 조직 Waffo는 금액 확인 창이 없고 `/api/organization/waffo/amount` 클라이언트도 없다. Waffo 가격이 epay와 다르면 결제 후에야 차이가 보인다.
- 프런트: Stripe 새 탭·Waffo Pancake 리다이렉트·generic epay 폼·리다이렉트 없음 분기, `/wallet` 라우트 렌더, 조직 라우트 `beforeLoad`, Role 선택지에 owner가 없다는 단언, provider의 관리자+조직 범위, `balanceDisplay` 값 치환 단언이 없다. member의 "조직 링크 없음" E2E에 긍정 기준점이 없고 프리셋 테스트는 버튼이 없으면 skip한다. E2E teardown 실패는 통과를 막지 않는다. 빈 DB는 `POST /api/setup` 수동 1회가 필요하다.
- 백엔드: Toss 제거 뒤 Assign/UpdateFields/Delete·선차감 멱등·취소 구독 비갱신 테스트 없음, 규칙 2–7은 `organization_security_test.go`에서만 확인, 이미지 예약의 조직 지갑 부족 분기 테스트 없음, owner가 이미 떠난 조직 삭제 테스트 없음.

동작상 남은 것(ledger minor 중 표시만의 문제가 아닌 것):
- 레거시 MJ 사전 가드는 조직 지갑 `> 0`만 보고 가격 이상인지는 보지 않는다(요청 1건만큼 초과 가능). MJ 환급과 `UpdateOrganizationUsedQuotaForUser`는 현재 소속 기준이다. `OrganizationId==0`인 작업은 사용자만 환급한다. `service/midjourney.go:50` 훅은 레거시 MJ에서 `BillingSource`가 비어 실제로 발동하지 않는다. 조직 지갑이 예약 경쟁에서 지면 오류 문구가 "user quota insufficient"로 나온다.
- 기간 래퍼가 team의 범위·overflow 검사를 버렸다(컨트롤러 기간 검증과 `quota_reset_period=never` 고정이 막고 있음).
- Excel: 관리자가 자기 행으로 자기 한도를 올릴 수 있다(가져오기에 actor id 필요), 내보낸 owner 행은 다시 가져오면 항상 오류, MySQL의 대소문자 무시 username 비교, 기존 회원의 quota 칸 파싱 실패는 무시, 사용자 생성 뒤 조직 배정 실패 시 고아 사용자, quota 차이를 오래된 값 기준으로 계산.
- 조직 사용자 목록 `keyword`가 LIKE 이스케이프를 하지 않는다. plan 수정·비활성화는 바뀐 행이 없어도 감사 항목을 남긴다. `/api/organizations` 앞의 `RootAuth` 때문에 casbin `organization` 리소스는 지금은 장식이다.
- `controller/organization_validation.go`의 `MaxTopUpAmount`·`rejectTopUpAmountTooLarge`는 쓰는 곳이 없다(충전 상한은 upstream `getMaxTopUpAmount`).
- 화면: 관리자에게도 `Top up organization wallet` 버튼이 보이지만 `/wallet`은 구성원 요약을 연다(충전은 owner 전용). 조직 지갑의 `API Requests`는 항상 0이고, 결제 비활성 문구가 숨겨진 교환 코드를 안내한다. root의 조직 선택은 대시보드·로그 화면에서만 유지되고 조직 사용자·구독 화면은 첫 조직에서 시작한다(team과 같음). 조직 전환 직후 이전 조직 로그가 잠깐 보인다. 대시보드 기간 Select가 Base UI에서 원값을 보일 수 있다(브라우저 확인 필요). 대시보드 수치는 `quota_data` 집계 주기(기본 5분) 뒤에 반영된다.
- 번역: 조직 키 126개 이상이 다른 locale에서 영문 fallback이다(단계 D). `Organization.description` 타입 optional화.

## 5. 최종 검증과 병합 리허설 (Task 13, 2026-10-14)

HEAD 971c18089(문서 커밋 직전)에서 실행.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `make test`: 종료 코드 0, `ok` 54 / `no test files` 46 / FAIL 0 (100 패키지, 이 중 27개는 캐시 결과).
- `bun run typecheck`(`tsgo -b`): 종료 코드 0. `bun run test`: 203 파일 / 2315 테스트 pass (단계 A 끝 189 / 2262에서 +14 / +53). `bun run build`: 성공, `routeTree.gen.ts` 변경 없음.
- `bun run e2e`: 19 passed (44.4s).
- Review Focus 테스트 6개 `-count=1` 재실행: 전부 PASS.
- `docs/fork-overlays.md`: O3 행과 O2 PayPal 행의 줄 번호를 `git grep -n "fork: O"`와 대조했다. 고친 것: `model/user.go:537` → 538, O2 `model/topup.go:35,45` → 37,47, `:488` → 491(O3 필드 2줄 추가로 밀림), `task-logs-columns.tsx:259-269` → 260-266, 프런트 타입 3행에 줄 번호 추가, 접근 토큰 행의 스코프 개수 문구. 추가한 행: `.gitignore:40-41`, `web/package.json:19-21,93`·`web/bun.lock`, `web/src/i18n/locales/*.json`. `git diff --diff-filter=M --name-only qn/main..HEAD`의 upstream 파일 105개가 모두 표에 있다.
- `docs/openapi/organization-rest-api.md`: 문서의 엔드포인트 38개 = `router/organization-router.go` 38개(빠진 것·남는 것 없음). 정정 전에는 `DELETE /api/organizations/:id`, `GET /api/organization/users/export`, `POST /api/organization/users/import` 3개가 빠져 있었다.

upstream 병합 리허설 (`git fetch qn`은 앞서 실행됨):
- `qn/main` = 7de10f2c6 = develop의 베이스(조상). `git merge --no-commit --no-ff qn/main` 출력: `Already up to date.` 충돌 파일 없음(`git diff --name-only --diff-filter=U` 빈 출력), 병합이 시작되지 않아 `git merge --abort`는 `There is no merge to abort`, MERGE_HEAD 없음.
- 그래서 충돌 집합이 `docs/fork-overlays.md`의 upstream 파일 안에 드는지는 이번에 검사하지 못했다. upstream에 새 커밋이 들어오는 첫 동기화 때 이 리허설을 다시 하고 충돌 파일 목록을 표와 대조한다(spec §8).
