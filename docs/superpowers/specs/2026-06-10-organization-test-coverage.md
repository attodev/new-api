# 조직 관리 기능 테스트 코드 설명 및 커버리지 분석

작성일: 2026-06-10
최종 업데이트: 2026-06-15

## 1. 목적

이 문서는 조직 관리 및 조직 구독 기능의 설계서와 구현 계획서를 기준으로, 현재 작성된 자동 테스트 코드가 무엇을 검증하는지 정리한다. 또한 통합 설계서의 테스트 요구사항을 기준으로 자동 테스트 커버 범위를 계산한다.

기준 문서:

- `docs/superpowers/specs/2026-06-09-organization-management-integrated-design.md`
- `docs/superpowers/specs/2026-06-09-organization-subscription-management-design.md`
- `docs/superpowers/plans/2026-06-09-organization-subscription-management.md`
- `docs/superpowers/plans/2026-06-03-organization-management.md`
- `docs/superpowers/plans/2026-06-04-organization-usage-dashboard.md`
- `docs/superpowers/plans/2026-06-07-organization-dashboard-charts.md`

검증 대상 테스트:

- `model/organization_test.go`
- `model/organization_subscription_test.go`
- `service/organization_billing_test.go`
- `controller/organization_test.go`
- `controller/organization_subscription_test.go`
- `web/default/src/features/organizations/lib/organization-subscription-utils.test.ts`
- `web/default/src/features/organizations/lib/organization-selection.test.ts`
- `web/default/src/i18n/languages.test.ts`
- `web/default/src/features/organizations/components/organization-member-wallet-summary.test.ts`
- `web/default/src/features/wallet/components/wallet-stats-card.test.tsx`
- `web/default/e2e/organization.e2e.ts`

## 2. 테스트 구성 요약

현재 조직 관련 자동 테스트는 크게 다섯 계층으로 나뉜다.

| 계층 | 파일 | 주요 검증 범위 |
| --- | --- | --- |
| 모델 | `model/organization_test.go` | 조직 역할 검증, 조직 생성 시 owner 지정, 이미 조직에 속한 사용자 차단, 조직 대시보드 집계 범위 |
| 모델 | `model/organization_subscription_test.go` | 조직 구독 생성/교체/취소, 조직 밖 사용자 차단, 구독 자동 갱신, 주기 리셋 |
| 서비스 | `service/organization_billing_test.go` | 조직 quota 방식 과금, 조직 구독 방식 과금, quota 부족/plan 한도 부족 실패 |
| 컨트롤러 | `controller/organization_test.go` | 조직 생성/수정 API, 대시보드/로그 API 권한과 범위, 조직 사용자 관리 권한, 로그 필터, 조직 쓰기 API 입력값 검증 |
| 컨트롤러 | `controller/organization_subscription_test.go` | 조직 구독 plan CRUD, 전체 관리자 조직 선택, 사용자 plan 할당/취소/재할당, 권한 차단, plan 입력값 검증 |
| 프론트 유틸 | `web/default/src/features/organizations/lib/organization-subscription-utils.test.ts` | active 조직 구독 사용자 식별, plan 할당 가능 사용자 필터, quota 입력 잠금 판단 |
| 프론트 유틸 | `web/default/src/features/organizations/lib/organization-selection.test.ts` | 마지막 선택 조직 저장/삭제, 사용 가능 조직 fallback |
| 프론트 i18n | `web/default/src/i18n/languages.test.ts` | UI 언어 선택지가 영어/한국어만 노출되는지, 제거된 언어가 영어로 fallback되는지 |
| 프론트 유틸/컴포넌트 | `web/default/src/features/organizations/components/organization-member-wallet-summary.test.ts`, `web/default/src/features/wallet/components/wallet-stats-card.test.tsx` | 조직 plan 사용자 지갑 잔액 대체 문구와 사용량/요청 수 유지 |
| 프론트 E2E | `web/default/e2e/organization.e2e.ts` | 실제 브라우저 로그인, 역할별 조직 메뉴 노출/비노출, 조직 화면 접근, 조직 선택 유지, 조직 사용 로그 화면 렌더링 |

## 3. 파일별 상세 설명

### 3.1 `model/organization_test.go`

이 파일은 조직 도메인의 가장 기본적인 모델 정책을 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `TestOrganizationRoleValidation` | `member`, `admin`, `owner`만 유효한 조직 역할로 인정하고 빈 값 또는 `root` 같은 잘못된 역할을 거부한다. | 조직 역할은 `member`, `admin`, `owner` 세 가지라는 요구사항 |
| `TestCreateOrganizationAssignsOwner` | 조직 생성 시 지정된 사용자의 `OrganizationId`와 `OrganizationRole`이 owner로 갱신되는지 확인한다. | 조직 생성과 조직 소유자 지정 |
| `TestCreateOrganizationRejectsUserAlreadyInOrganization` | 이미 다른 조직에 속한 사용자를 새 조직의 소유자로 지정할 수 없음을 검증한다. | 사용자는 하나의 조직에만 소속 가능 |
| `TestCanManageOrganizationTargetRejectsGlobalAdmins` | 조직 관리자/소유자가 전역 관리자 또는 다른 조직 사용자를 관리할 수 없음을 검증한다. | 조직 역할과 전역 권한 분리, 전역 관리자 관리 금지 |
| `TestGetOrganizationDashboardScopesQuotaDataToOrganization` | 대시보드 집계가 해당 조직 사용자만 포함하는지 확인한다. 일별 사용량, Top 사용자, Top 모델, 사용자별/모델별 시계열까지 검증한다. | 조직 대시보드가 조직 내부 로그만 집계 |

특히 대시보드 테스트는 단순 합계만 보는 것이 아니라, 다른 조직의 사용량이 섞이지 않는지와 `(unknown)` 모델 처리까지 함께 확인한다.

### 3.2 `model/organization_subscription_test.go`

이 파일은 조직 전용 subscription의 핵심 상태 전이를 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `TestCreateOrganizationSubscriptionFromPlanCancelsExistingActiveSubscription` | 같은 사용자에게 새 plan을 배정하면 기존 active 구독이 cancelled로 바뀌고 새 구독만 active가 되는지 확인한다. | 동일 사용자에게 여러 active 조직 구독이 남지 않음 |
| `TestCreateOrganizationSubscriptionFromPlanCancelsExpiredActiveSubscription` | 만료 시간이 지났지만 status가 active인 구독도 새 plan 배정 시 취소되는지 확인한다. | 취소는 만료 시간과 무관하게 active 구독을 취소 |
| `TestCreateOrganizationUserSubscriptionRejectsOutsideOrganizationUser` | 다른 조직 사용자를 현재 조직 plan에 배정할 수 없음을 검증한다. | 조직 밖 사용자 배정 거부 |
| `TestCancelOrganizationUserSubscriptionCancelsExpiredActiveSubscription` | 만료된 active 구독도 취소 API에서 cancelled 처리되는지 확인한다. | 만료 시간과 무관한 active 구독 취소 |
| `TestPreConsumeOrganizationUserSubscriptionResetsPeriodicQuota` | 리셋 시점이 지난 구독은 pre-consume 전에 사용량을 새 기간 기준으로 리셋하는지 확인한다. | 기간과 리셋 주기 계산 |
| `TestHasActiveOrganizationUserSubscriptionRenewsExpiredSubscription` | active 구독 조회 시 만료된 active 구독이 자동 갱신되고 사용량이 0으로 초기화되는지 확인한다. | active 구독 자동 갱신 |
| `TestPreConsumeOrganizationUserSubscriptionRenewsExpiredSubscription` | pre-consume 시점에도 만료된 active 구독이 자동 갱신된 뒤 사용량이 차감되는지 확인한다. | 다음 요청 또는 활성 구독 조회 시 자동 갱신 |

이 테스트들은 조직 구독을 "일회성 할당"이 아니라 "active 상태이면 자동 갱신되는 운영 정책"으로 다루는 설계 의도를 직접 검증한다.

### 3.3 `service/organization_billing_test.go`

이 파일은 실제 요청 과금 흐름에서 어떤 funding source가 사용되는지 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `TestOrganizationWalletBillingPreConsumesMemberLimitAndOrganizationWallet` | 활성 조직 구독이 없는 조직 사용자는 사용자 quota와 조직 quota가 함께 선차감되는지 확인한다. | 일반 quota 방식 |
| `TestOrganizationWalletBillingSettlesRefundToMemberLimitAndOrganizationWallet` | 선차감 25 후 실제 사용량 10으로 정산하면 차액이 사용자 quota와 조직 quota에 되돌아가는지 확인한다. | 정산 시 차액 반영 |
| `TestOrganizationWalletBillingOwnerRequestUsesOrganizationWallet` | 조직 소유자 요청도 조직 quota를 함께 사용하는지 확인한다. | 조직 전체 사용 금액은 조직 quota에서 최종 차감 |
| `TestOrganizationWalletBillingRejectsWhenOrganizationWalletInsufficient` | 조직 quota가 부족하면 사용자 quota가 충분해도 요청이 실패하고 잔액이 변하지 않는지 확인한다. | 조직 quota 부족 시 요청 실패 |
| `TestOrganizationSubscriptionBillingPreConsumesSubscriptionAndOrganizationWallet` | 활성 조직 구독이 있으면 사용자 개인 quota는 유지되고, 구독 사용량과 조직 quota가 함께 차감되는지 확인한다. | 조직 subscription 방식 |
| `TestOrganizationSubscriptionBillingRejectsWhenPlanLimitInsufficient` | 조직 구독 한도가 부족하면 사용자 quota와 조직 quota가 충분해도 fallback 없이 실패하는지 확인한다. | 조직 구독 한도 부족 시 사용자 quota fallback 없음 |
| `TestOrganizationSubscriptionBillingSettlesRefundToSubscriptionAndOrganizationWallet` | 조직 구독 방식에서 선차감 25 후 실제 사용량 10으로 정산하면 구독 사용량과 조직 quota가 함께 차액 복구되는지 확인한다. | 조직 구독 방식 정산 |
| `TestOrganizationSubscriptionBillingRefundRestoresSubscriptionAndOrganizationWallet` | 요청 실패 환불 흐름에서 조직 구독 사용량과 조직 quota가 모두 원복되는지 확인한다. | 조직 구독 방식 환불 |

현재 서비스 테스트는 pre-consume, settle 차액 복구, 비동기 refund 복구, 한도 부족 실패를 모두 검증한다.

### 3.4 `controller/organization_test.go`

이 파일은 API handler 레벨에서 권한, 범위, 요청 파라미터 검증을 확인한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `TestOrganizationRootCanCreateOrganization` | 전체 관리자가 조직을 만들고 소유자를 owner로 지정할 수 있음을 확인한다. | 전체 관리자 조직 생성 |
| `TestOrganizationRootAcceptsCreateBoundaryInput` | 조직 생성 시 최소 이름/0 quota와 최대 이름/설명/owner ID/quota 경계값이 정상 저장되는지 확인한다. | 조직 생성 입력값 경계값 허용 |
| `TestOrganizationRootRejectsInvalidCreateInput` | 조직 생성 시 빈 이름, 이름 64자 초과, 설명 255자 초과, owner ID 하한/상한 초과, quota 하한/상한 초과, 허용되지 않은 body 필드를 정확한 메시지로 거부한다. | 조직 생성 입력값 범위와 메시지 강제 |
| `TestOrganizationRootCanUpdateOrganizationQuota` | 전체 관리자가 조직 quota를 수정할 수 있음을 확인한다. | 전체 관리자 조직 quota 조정 |
| `TestOrganizationRootAcceptsUpdateBoundaryInput` | 조직 수정 시 설명 0/255자, quota 0/1,000,000,000, status 1/2 경계값이 정상 저장되는지 확인한다. | 조직 수정 입력값 경계값 허용 |
| `TestOrganizationRootRejectsInvalidUpdateInput` | 조직 수정 시 설명 255자 초과, quota 하한/상한 초과, 허용되지 않은 status 값과 body 필드를 정확한 메시지로 거부한다. | 조직 수정 입력값 범위, enum, 메시지 강제 |
| `TestOrganizationAdminCanReadOrganizationProfile` | 조직 소유자/관리자 권한으로 조직 profile에서 quota와 사용량을 볼 수 있음을 확인한다. | 조직 지갑/profile 조회 |
| `TestOrganizationOwnerCanReadDashboard` | 조직 소유자의 대시보드 조회가 자기 조직 데이터만 포함하는지 확인한다. | 조직 소유자 대시보드 |
| `TestOrganizationAdminCanReadDashboard` | 조직 관리자도 대시보드 접근이 가능함을 확인한다. | 조직 관리자 대시보드 |
| `TestOrganizationRootCanReadSelectedOrganizationDashboard` | 전체 관리자가 `organization_id`로 선택한 조직의 대시보드를 조회할 수 있음을 확인한다. | 전체 관리자 조직 선택 조회 |
| `TestOrganizationRootDashboardRequiresOrganizationId` | 전체 관리자는 조직 선택 없이 조직 대시보드를 볼 수 없음을 확인한다. | 전체 관리자 조직 선택 필수 |
| `TestOrganizationRootCanReadSelectedOrganizationLogs` | 전체 관리자가 선택한 조직의 사용 로그만 조회하는지 확인한다. | 조직 로그 범위 제한 |
| `TestOrganizationAdminLogsStayWithinOrganizationAndApplyFilters` | 조직 관리자가 사용 로그를 조회할 때 자기 조직 사용자 로그만 보이며, 모델명 필터가 적용되는지 확인한다. | 조직 로그 범위 제한과 검색 필터 |
| `TestOrganizationDashboardAcceptsPresetRanges` | `today`, `30d` preset이 허용되는지 확인한다. | 조직 대시보드 기간 필터 |
| `TestOrganizationDashboardRejectsInvalidPreset` | 잘못된 preset을 거부한다. | 입력 검증 |
| `TestOrganizationDashboardRejectsMalformedStartTimestamp` | 잘못된 시작 timestamp를 거부한다. | 입력 검증 |
| `TestOrganizationDashboardRejectsMalformedEndTimestamp` | 잘못된 종료 timestamp를 거부한다. | 입력 검증 |
| `TestOrganizationMemberCannotReadDashboard` | 조직 일반 사용자가 조직 대시보드에 접근할 수 없음을 확인한다. | 조직 일반 사용자 관리 기능 접근 금지 |
| `TestOrganizationAdminCannotUpdateOutsideOrganization` | 조직 관리자가 다른 조직 사용자를 수정할 수 없음을 확인한다. | 조직 밖 사용자 관리 차단 |
| `TestOrganizationAdminCanUpdateQuotaForMember` | 조직 관리자가 자기 조직 member의 quota, status, remark를 수정할 수 있음을 확인한다. | 조직 관리자 사용자 quota 관리 |
| `TestOrganizationAdminAcceptsUserUpdateBoundaryInput` | 조직 사용자 수정 시 quota 0/1,000,000,000, status 1/2, remark 0/255자 경계값이 정상 저장되는지 확인한다. | 조직 사용자 수정 입력값 경계값 허용 |
| `TestOrganizationAdminRejectsInvalidUserUpdateInput` | 조직 사용자 수정 시 quota 하한/상한 초과, 허용되지 않은 status/body 필드, remark 255자 초과를 정확한 메시지로 거부한다. | 조직 사용자 수정 입력값 범위, enum, 메시지 강제 |
| `TestOrganizationAdminCannotUpdateGlobalAdmin` | 조직 관리자가 전역 관리자 사용자를 수정할 수 없음을 확인한다. | 전역 관리자 관리 금지 |
| `TestOrganizationOwnerCanAssignMemberRole` | 조직 소유자가 조직 밖 일반 사용자를 자기 조직 member로 배정할 수 있음을 확인한다. | 조직 소유자의 사용자 추가 |
| `TestOrganizationOwnerCanListAssignableUsers` | 할당 가능 사용자 목록에서 자기 자신, 다른 조직 사용자, 전역 관리자가 제외되는지 확인한다. | 조직과 무관한 사용자만 선택 가능 |
| `TestOrganizationOwnerCannotAssignSelf` | 소유자가 자기 자신의 조직 역할을 낮추는 배정을 할 수 없음을 확인한다. | owner 보호 |
| `TestOrganizationAdminCannotListAssignableUsers` | 조직 관리자는 할당 가능 사용자 목록을 볼 수 없음을 확인한다. | 조직 소유자 전용 구성원 추가 |
| `TestOrganizationAdminCannotAssignMembership` | 조직 관리자는 새 멤버십 배정을 할 수 없음을 확인한다. | 조직 소유자 전용 구성원 추가 |
| `TestOrganizationOwnerCannotAssignGlobalAdmin` | 조직 소유자가 전역 관리자를 조직 사용자로 배정할 수 없음을 확인한다. | 전역 관리자 관리 금지 |
| `TestOrganizationOwnerCannotAssignInvalidRole` | `root` 같은 유효하지 않은 조직 역할 배정을 거부한다. | 조직 역할 값 제한 |

컨트롤러 테스트는 조직 관리 API의 권한 경계를 가장 넓게 커버한다.

### 3.5 `controller/organization_subscription_test.go`

이 파일은 조직 구독 API handler 레벨의 권한과 범위를 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `TestOrganizationOwnerCanManageSubscriptionPlan` | 조직 소유자가 자기 조직의 구독 plan을 생성, 수정, 비활성화할 수 있음을 확인한다. 생성 시 quota reset은 UI 정책처럼 `never`로 고정되는지도 검증한다. | 조직 소유자/관리자의 plan 생성/수정/비활성화 |
| `TestOrganizationSubscriptionPlanAcceptsBoundaryInput` | 조직 구독 plan 생성 시 제목/부제목/금액/기간/사용자 지정 초/정렬값의 최소 및 최대 경계값이 정상 저장되는지 확인한다. | 조직 구독 plan 입력값 경계값 허용 |
| `TestOrganizationSubscriptionPlanRejectsInvalidInput` | 조직 구독 plan 생성 시 빈 제목, 제목 128자 초과, 부제목 255자 초과, 잘못된 `duration_unit`, 기간/사용자 지정 초/금액/정렬값의 하한/상한 초과, 허용되지 않은 body 필드를 정확한 메시지로 거부한다. | 조직 구독 plan 입력값 범위, enum, 메시지 강제 |
| `TestOrganizationMemberCannotManageSubscriptionPlan` | 조직 일반 사용자가 조직 구독 plan을 생성할 수 없음을 확인한다. | 조직 일반 사용자 접근 거부 |
| `TestOrganizationRootCanManageSelectedOrganizationSubscription` | 전체 관리자가 `organization_id`로 선택한 조직에 plan을 만들고 해당 조직 사용자에게 plan을 할당할 수 있음을 확인한다. | 전체 관리자 조직 선택 관리 |
| `TestOrganizationRootSubscriptionPlanRequiresOrganizationId` | 전체 관리자는 조직 구독 API 사용 시 대상 `organization_id`가 필요함을 확인한다. | 전체 관리자 조직 선택 필수 |
| `TestOrganizationAdminCannotAssignSubscriptionOutsideOrganization` | 조직 관리자가 다른 조직 사용자에게 자기 조직 plan을 할당할 수 없음을 확인한다. | 조직 밖 사용자 배정 거부 |
| `TestOrganizationSubscriptionAssignmentAcceptsBoundaryPlanId` | 조직 구독 할당 시 `plan_id` 1과 1,000,000,000 경계값이 실제 plan이면 정상 할당되는지 확인한다. | 조직 구독 할당 참조 ID 경계값 허용 |
| `TestOrganizationSubscriptionAssignmentRejectsInvalidPlanId` | 조직 구독 할당 시 `plan_id`가 0이거나 1,000,000,000을 초과하거나 허용되지 않은 body 필드가 있으면 정확한 메시지로 거부한다. | 조직 구독 할당 참조 ID 범위와 메시지 강제 |
| `TestOrganizationAdminCanCancelActiveSubscription` | 조직 관리자가 자기 조직 사용자의 active 구독을 취소할 수 있음을 확인한다. | 사용자 plan 취소 |
| `TestOrganizationAdminCanCancelAndReassignSubscription` | active 구독을 취소한 뒤 같은 사용자에게 다시 plan을 할당하면 새 active 구독 하나만 생기고 이전 구독은 cancelled로 남는지 확인한다. | 취소 후 재할당 상태 전이 |

이 테스트들은 기존 모델 테스트가 다루던 조직 구독 상태 전이를 API handler 경계에서도 확인한다. 따라서 route handler가 권한을 잘못 풀거나 조직 범위를 잘못 해석하는 회귀를 잡을 수 있다.

### 3.6 `web/default/src/features/organizations/lib/organization-subscription-utils.test.ts`

이 파일은 프론트엔드 UI에서 active 조직 구독 사용자를 구분하기 위한 순수 유틸을 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `detects only active organization plan assignments` | `active`, `cancelled`, `expired` 구독 중 active 사용자 ID만 Set으로 반환하고, 특정 사용자가 active 구독을 갖는지 판별한다. | active 플랜 사용자는 quota 입력/할당 UI에서 별도 처리 |
| `filters active plan users out of assignable plan users` | active plan 사용자를 새 plan 할당 후보 목록에서 제외하는 필터를 검증한다. | active 플랜 사용자는 플랜 할당 드롭다운에서 제외 |
| `marks quota controls disabled only for active organization plan users` | active plan 사용자만 quota 입력 잠금 대상으로 판정하는지 검증한다. | 조직 구독 사용자는 조직 사용자 화면에서 quota 입력 비활성화 |

이 테스트는 UI 렌더링 테스트가 아니라 로직 유틸 테스트다. 다만 조직 구독 페이지의 할당 후보 목록이 이 공통 유틸을 사용하도록 연결되어 있어, active plan 사용자를 할당 후보에서 제외하는 데이터 흐름은 자동 테스트로 보호된다.

### 3.7 `web/default/src/features/organizations/lib/organization-selection.test.ts`

이 파일은 전체 관리자의 조직 선택 유지에 쓰이는 localStorage helper를 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `saves, reads, and clears the last selected organization id` | 선택한 조직 ID를 저장하고 다시 읽으며, 빈 값 저장 시 삭제되는지 확인한다. | 조직 선택 값 유지 |
| `falls back to the first available organization when saved id is unavailable` | 저장된 조직이 목록에 없으면 첫 번째 사용 가능 조직으로 fallback하는지 확인한다. | 조직 선택 복구 |
| `returns an empty id when storage is unavailable` | 브라우저 storage가 없는 환경에서도 안전하게 빈 값을 반환하는지 확인한다. | 제한 환경 방어 |

이 유틸 테스트만 보면 route 이동은 포함하지 않지만, `organization.e2e.ts`가 실제 브라우저에서 조직 대시보드와 조직 사용 로그 사이를 이동한 뒤 선택 조직이 유지되는지 검증하므로 F4는 직접 커버로 계산한다.

### 3.8 조직 지갑 프론트 테스트

조직 구독 사용자의 지갑 표시를 위해 두 종류의 프론트 테스트를 추가했다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `organization member wallet balance display > replaces current balance copy for active organization plan users` | active 조직 plan이 있으면 지갑 잔액 표시가 `Organization plan in use`와 설명 문구로 대체되는지 확인한다. | 조직 구독 사용자는 지갑에서 잔액 대신 조직 plan 사용 문구 표시 |
| `organization member wallet balance display > keeps regular balance display when there is no active organization plan` | cancelled 구독 또는 구독 없음 상태에서는 기본 잔액 표시를 유지하도록 `undefined`를 반환하는지 확인한다. | 조직 구독이 없으면 기존 quota 방식 유지 |
| `WalletStatsCard > renders organization plan balance message while keeping usage and request stats` | 지갑 카드 컴포넌트를 서버 렌더링해 잔액 대체 문구가 나오면서 사용량과 요청 수 영역은 유지되는지 확인한다. | 사용량과 요청 수는 그대로 표시 |

이 테스트들은 전체 페이지 E2E는 아니지만, 실제 지갑 카드 컴포넌트와 조직 지갑 표시 helper를 함께 검증하므로 F5 요구사항은 직접 커버로 본다.

### 3.9 `web/default/src/i18n/languages.test.ts`

이 파일은 UI 언어 지원 범위를 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `only exposes English and Korean in the UI language list` | 언어 선택 옵션이 `en`, `kr` 두 개만 노출되는지 확인한다. | UI 지원 언어를 영어/한국어로 제한 |
| `normalizes Korean browser language variants to kr` | `ko`, `ko-KR`, `kr` 입력이 모두 한국어 locale 코드 `kr`로 정규화되는지 확인한다. | 한국어 브라우저 언어 감지 |
| `falls back removed or unknown languages to English` | 제거된 `zh-CN`, `fr` 또는 알 수 없는 언어 값이 영어로 fallback되는지 확인한다. | 제거된 locale 안전 처리 |

이 테스트는 번역 품질 자체를 검증하지는 않는다. 다만 언어 선택 UI와 저장된 언어 값 정규화가 영어/한국어 체계로 제한되는지는 자동으로 보호한다.

### 3.10 `web/default/e2e/organization.e2e.ts`

이 파일은 Playwright 기반 브라우저 E2E 테스트다. 단위 테스트와 달리 실제 백엔드, 프론트 dev server, Chromium 브라우저를 사용해 사용자가 보는 화면과 라우팅을 검증한다.

| 테스트 | 설명 | 설계서 연결 |
| --- | --- | --- |
| `root admin can see organization management navigation and pages` | 전체 관리자로 로그인한 뒤 조직 사용자, 조직 대시보드, 조직 사용 로그, 조직 작업 로그, 조직 구독 메뉴가 보이는지 확인하고, 조직 대시보드와 조직 구독 화면에 실제로 진입한다. | 전체 관리자 조직 메뉴와 조직 화면 접근 |
| `organization selection survives navigation between organization pages` | 전체 관리자 화면에서 선택된 조직이 조직 사용 로그로 이동했다가 조직 대시보드로 돌아온 뒤에도 유지되는지 확인한다. | 조직 선택 유지 |
| `organization owner and admin can access organization management pages` | 조직 소유자와 조직 관리자로 로그인해 조직 사용자/조직 구독 메뉴가 보이고, 조직 사용자 화면과 조직 구독 화면에 접근 가능한지 확인한다. | 조직 소유자/관리자 메뉴와 접근 권한 |
| `organization usage logs render for admins` | 전체 관리자가 조직 사용 로그 화면에 진입했을 때 검색 버튼과 페이지네이션 UI가 렌더링되는지 확인한다. | 조직 사용 로그 화면 접근 |
| `organization member cannot open management pages directly` | 조직 일반 사용자에게 조직 관리 메뉴가 보이지 않는지 확인하고, 조직 대시보드/사용 로그/조직 구독 URL 직접 접근이 403 또는 로그인 리다이렉트로 차단되는지 확인한다. | 조직 일반 사용자 관리 기능 차단 |

이 E2E 테스트는 프론트의 메뉴/라우팅/조직 선택/사용 로그 화면 회귀를 잡기 위한 smoke test다. 조직 생성, plan 생성, plan 할당/취소처럼 DB 상태를 적극적으로 변경하는 흐름과 지갑 전체 페이지 이동은 아직 자동화하지 않았다.

## 4. 설계 요구사항 대비 커버리지

커버리지 계산은 통합 설계서 `15. 테스트 요구사항`의 항목을 기준으로 했다.

판정 기준:

- 직접: 자동 테스트가 해당 요구사항을 명시적으로 검증한다.
- 부분: 요구사항의 하위 로직 또는 백엔드 일부만 검증한다.
- 미커버: 현재 자동 테스트로 검증되지 않는다.

### 4.1 백엔드 요구사항

| 번호 | 요구사항 | 상태 | 근거 |
| --- | --- | --- | --- |
| B1 | 조직 생성과 조직 소유자 지정 | 직접 | `TestCreateOrganizationAssignsOwner`, `TestOrganizationRootCanCreateOrganization` |
| B2 | 조직과 무관한 사용자만 조직 소유자로 선택 가능 | 직접 | `TestCreateOrganizationRejectsUserAlreadyInOrganization`, `TestOrganizationOwnerCanListAssignableUsers` |
| B3 | 조직 admin/owner 권한 검증 | 직접 | `TestOrganizationAdminCanReadDashboard`, `TestOrganizationOwnerCanReadDashboard`, `TestOrganizationMemberCannotReadDashboard` |
| B4 | 조직 밖 사용자 관리 차단 | 직접 | `TestOrganizationAdminCannotUpdateOutsideOrganization`, `TestCreateOrganizationUserSubscriptionRejectsOutsideOrganizationUser` |
| B5 | 조직 quota 선차감, 정산, 환불 | 부분 | 선차감과 settle 차액 복구는 검증. 독립 refund API/실패 환불은 별도 직접 테스트 없음 |
| B6 | 조직 quota 부족 시 요청 실패 | 직접 | `TestOrganizationWalletBillingRejectsWhenOrganizationWalletInsufficient` |
| B7 | 조직 구독 한도 선차감, 정산, 환불 | 직접 | `TestOrganizationSubscriptionBillingPreConsumesSubscriptionAndOrganizationWallet`, `TestOrganizationSubscriptionBillingSettlesRefundToSubscriptionAndOrganizationWallet`, `TestOrganizationSubscriptionBillingRefundRestoresSubscriptionAndOrganizationWallet` |
| B8 | 조직 구독 한도 부족 시 요청 실패 | 직접 | `TestOrganizationSubscriptionBillingRejectsWhenPlanLimitInsufficient` |
| B9 | 조직 구독 사용자는 사용자 quota로 fallback하지 않음 | 직접 | plan 한도 부족 실패 시 사용자 quota와 조직 quota가 그대로 유지됨 |
| B10 | active 구독 자동 갱신 | 직접 | `TestHasActiveOrganizationUserSubscriptionRenewsExpiredSubscription`, `TestPreConsumeOrganizationUserSubscriptionRenewsExpiredSubscription` |
| B11 | 구독 취소 시 만료 시간과 무관하게 active 구독 취소 | 직접 | `TestCancelOrganizationUserSubscriptionCancelsExpiredActiveSubscription`, `TestOrganizationAdminCanCancelActiveSubscription` |
| B12 | 동일 사용자에게 여러 active 조직 구독이 남지 않음 | 직접 | `TestCreateOrganizationSubscriptionFromPlanCancelsExistingActiveSubscription`, `TestCreateOrganizationSubscriptionFromPlanCancelsExpiredActiveSubscription`, `TestOrganizationAdminCanCancelAndReassignSubscription` |
| B13 | 조직 대시보드와 로그가 조직 내부 데이터만 집계/조회 | 직접 | `TestGetOrganizationDashboardScopesQuotaDataToOrganization`, `TestOrganizationRootCanReadSelectedOrganizationDashboard`, `TestOrganizationRootCanReadSelectedOrganizationLogs`, `TestOrganizationAdminLogsStayWithinOrganizationAndApplyFilters` |
| B14 | 조직 쓰기 API 입력값 범위와 enum 검증 | 직접 | `TestOrganizationRootAcceptsCreateBoundaryInput`, `TestOrganizationRootRejectsInvalidCreateInput`, `TestOrganizationRootAcceptsUpdateBoundaryInput`, `TestOrganizationRootRejectsInvalidUpdateInput`, `TestOrganizationAdminAcceptsUserUpdateBoundaryInput`, `TestOrganizationAdminRejectsInvalidUserUpdateInput`, `TestOrganizationSubscriptionPlanAcceptsBoundaryInput`, `TestOrganizationSubscriptionPlanRejectsInvalidInput`, `TestOrganizationSubscriptionAssignmentAcceptsBoundaryPlanId`, `TestOrganizationSubscriptionAssignmentRejectsInvalidPlanId` |

백엔드 결과:

- 직접 커버: 13개
- 부분 커버: 1개
- 미커버: 0개
- 직접 커버율: 13 / 14 = 92.9%
- 가중 커버율: `(직접 13 + 부분 1 * 0.5) / 14 = 96.4%`
- 최소 커버율: 직접 또는 부분 포함 14 / 14 = 100%

### 4.2 프론트엔드 요구사항

| 번호 | 요구사항 | 상태 | 근거 |
| --- | --- | --- | --- |
| F1 | 조직 일반 사용자는 조직 관리 메뉴를 볼 수 없음 | 직접 | `organization.e2e.ts`에서 조직 일반 사용자에게 조직 사용자/대시보드/구독 링크가 없는지 확인하고 직접 URL 접근도 403으로 검증 |
| F2 | 조직 소유자/관리자는 조직 관리 메뉴를 볼 수 있음 | 직접 | `organization.e2e.ts`에서 조직 소유자/관리자에게 조직 사용자/구독 메뉴가 보이고 화면 접근이 가능한지 검증 |
| F3 | 전체 관리자는 조직 선택 후 조직 대시보드/로그/구독을 조회할 수 있음 | 부분 | E2E에서 전체 관리자 조직 메뉴와 대시보드/구독 진입, 조직 선택 유지 검증. 사용 로그 화면 렌더링과 백엔드 로그 데이터 범위/필터는 검증하지만, 작업 로그 실제 데이터 렌더링은 미검증 |
| F4 | 조직 선택 값이 메뉴 이동 후에도 유지됨 | 직접 | `organization-selection.test.ts` helper와 `organization.e2e.ts`의 대시보드 -> 사용 로그 -> 대시보드 이동 검증 |
| F5 | 조직 구독 사용자는 지갑에서 잔액 대신 `조직 플랜 사용 중`을 봄 | 직접 | `getOrganizationMemberWalletBalanceDisplay`, `WalletStatsCard` 렌더링 테스트 |
| F6 | 조직 구독 사용자는 조직 사용자 화면에서 quota 입력이 비활성화됨 | 직접 | `getOrganizationQuotaControlState`가 active plan 사용자의 quota 입력 disabled 상태와 표시 quota 0을 반환하고, `OrganizationUsersTable`이 이 값을 input/button disabled와 잔액 표시 계산에 사용 |
| F7 | active 플랜 사용자는 플랜 할당 드롭다운에서 제외됨 | 직접 | active plan 사용자 제외 helper 테스트 및 `OrganizationSubscriptionsPage` 할당 후보 목록에 연결 |
| F8 | 플랜 취소 후 목록에서 사라지고 다시 할당 가능해짐 | 부분 | `getVisibleActiveOrganizationSubscriptionRecords`가 cancelled/expired 구독을 active 목록에서 제외하고, cancelled 사용자가 다시 할당 후보로 돌아오는 유틸 테스트와 컨트롤러의 취소 후 재할당 상태 전이 테스트 추가. 실제 버튼 클릭 후 API refresh E2E는 없음 |
| F9 | 한국어 UI에서 지갑 화면의 주요 문구가 번역됨 | 미커버 | i18n key 존재/렌더링 테스트 없음 |
| F10 | UI 언어 선택지는 영어와 한국어만 제공하고 제거된 언어 값은 영어로 fallback됨 | 직접 | `languages.test.ts`에서 `INTERFACE_LANGUAGE_OPTIONS`가 `en`, `kr`만 포함하고 `zh-CN`, `fr`, unknown 값이 `en`으로 정규화되는지 검증 |

프론트엔드 결과:

- 직접 커버: 7개
- 부분 커버: 2개
- 미커버: 1개
- 직접 커버율: 7 / 10 = 70.0%
- 가중 커버율: `(직접 7 + 부분 2 * 0.5) / 10 = 80.0%`
- 최소 커버율: 직접 또는 부분 포함 9 / 10 = 90.0%

### 4.3 전체 요구사항 기준 커버리지

전체 요구사항 수는 백엔드 14개와 프론트엔드 10개를 합쳐 24개다.

| 구분 | 직접 | 부분 | 미커버 | 직접 커버율 | 가중 커버율 | 직접/부분 포함 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 백엔드 | 13 | 1 | 0 | 92.9% | 96.4% | 100% |
| 프론트엔드 | 7 | 2 | 1 | 70.0% | 80.0% | 90.0% |
| 전체 | 20 | 3 | 1 | 83.3% | 89.6% | 95.8% |

계산식:

- 직접 커버율: `직접 / 전체`
- 가중 커버율: `(직접 + 부분 * 0.5) / 전체`
- 직접/부분 포함: `(직접 + 부분) / 전체`

현재 자동 테스트는 백엔드 핵심 정책을 상당히 잘 검증한다. 프론트엔드는 기존 유틸/컴포넌트 테스트에 더해 Playwright E2E가 추가되어 역할별 조직 메뉴, 실제 라우팅, 조직 선택 유지, 조직 사용 로그 화면 렌더링까지 자동화되었다. 이번 보강으로 조직 쓰기 API 입력값 검증, active plan 사용자의 quota 입력 잠금, cancelled plan 사용자의 재할당 후보 복귀, UI 지원 언어를 영어/한국어로 제한하는 정책도 자동 테스트에 포함되었다. 다만 실제 취소 버튼 클릭 후 API refresh E2E, 한국어 번역 품질, 실제 차트/로그 데이터 화면은 아직 수동 테스트에 의존한다. 따라서 전체 가중 커버율은 89.6%로 계산된다.

이번에 추가한 `TestOrganizationAdminCanCancelAndReassignSubscription`과 `TestOrganizationAdminLogsStayWithinOrganizationAndApplyFilters`는 기존 부분 커버 항목의 근거를 강화한다. 구독 취소 후 재할당은 API 상태 전이까지 자동 검증하며, 조직 사용 로그는 백엔드 API 레벨에서 조직 범위와 모델 필터를 검증한다. `languages.test.ts`는 영어/한국어만 남긴 UI 언어 정책을 직접 검증한다. 다만 실제 브라우저에서 취소 버튼을 누른 뒤 목록이 갱신되는지, 로그 테이블에 특정 행이 렌더링되는지는 아직 E2E로 고정하지 않았으므로 프론트엔드 F3/F8은 부분 커버로 유지한다.

## 5. 현재 검증 명령 결과

다음 명령으로 현재 조직 관련 테스트를 확인했다.

```bash
go test ./model ./service ./controller -run 'TestOrganization' -count=1 -timeout=30s
```

결과:

```text
ok  	github.com/QuantumNous/new-api/model
ok  	github.com/QuantumNous/new-api/service
ok  	github.com/QuantumNous/new-api/controller
```

프론트 테스트는 `web/default`에서 다음 명령으로 확인했다.

```bash
bun run test
```

결과:

```text
17 pass
0 fail
```

브라우저 E2E 테스트는 다음 명령으로 확인했다.

```bash
bun run e2e
```

결과:

```text
5 passed
```

새 환경에서는 Playwright Chromium 바이너리가 없을 수 있으므로 최초 1회 다음 명령을 실행한다.

```bash
bun run e2e:install
```

추가로 프론트 타입체크와 lint도 시도했다.

```bash
bun run typecheck
./node_modules/.bin/eslint <변경 파일 목록>
```

`bun run typecheck`는 이번 변경과 무관한 기존 타입 오류로 실패했다. 확인된 대표 오류는 `organization-dashboard.tsx`, `usage-logs/*`, `system-settings/billing/index.tsx` 쪽이다. `eslint`는 현재 설치된 `@eslint/config-array`와 `minimatch/brace-expansion` 조합에서 `(0, brace_expansion_1.expand) is not a function` 오류로 실행 자체가 실패한다.

## 6. 주요 미커버 영역

Playwright E2E와 언어 설정 단위 테스트가 추가되면서 기존에 부족했던 역할별 메뉴, 조직 선택 유지, UI 지원 언어 범위 검증은 자동화되었다. 남은 미커버 영역은 주로 상태 변경형 UI와 번역 품질/시각화 검증이다.

우선순위가 높은 미커버 항목:

1. 조직 구독 화면 상태 전이
   - plan 취소 후 목록에서 사라지고 다시 할당 가능해야 한다.
   - 현재는 cancelled record가 active 목록에서 제외되고 재할당 후보로 돌아오는 유틸과, 컨트롤러에서 취소 후 재할당 시 active 구독이 하나만 남는 상태 전이를 검증한다. 취소 버튼 클릭, API refresh, 화면 목록 갱신은 아직 E2E로 검증하지 않는다.
2. 한국어 i18n 키와 실제 번역 품질
   - 영어/한국어만 언어 선택지에 노출되는지는 자동 검증한다.
   - 지갑/조직 구독 관련 주요 문구가 한국어 locale에 존재하고 화면에서 자연스럽게 번역되는지는 자동 검증하지 않는다.
3. 조직 로그/작업 로그 화면 데이터 검증
   - 백엔드 사용 로그 API는 조직 범위와 모델명 필터를 검증한다. E2E는 메뉴 노출과 일부 화면 이동을 확인하지만, 사용 로그/작업 로그의 실제 데이터 행 렌더링과 작업 로그 필터까지는 검증하지 않는다.
4. 대시보드 차트 시각 검증
   - 백엔드 집계와 프론트 진입은 검증하지만, 차트가 모든 데이터 상태에서 올바르게 그려지는지는 자동 검증하지 않는다.

## 7. 권장 추가 테스트

### 7.1 백엔드

- `service/organization_subscription_billing_test.go`
  - 조직 quota 부족 시 조직 구독 사용량도 증가하지 않는지 확인
  - 일반 조직 quota 방식의 명시적 `Refund` 호출 복구 확인

### 7.2 프론트엔드

현재 `web/default`에는 `bun run test` 기반 단위 테스트와 `bun run e2e` 기반 Playwright 테스트가 함께 있다. 다음 단계는 smoke E2E를 넘어서 상태 변경형 E2E와 DOM 수준 컴포넌트 테스트를 늘리는 것이다.

선택지:

- 컴포넌트 단위: React Testing Library 또는 현재 도구 체계에 맞는 테스트 runner 추가
- E2E 단위: Playwright로 실제 조직 생성/구독 할당/취소 화면 검증
- 경량 단위: 순수 유틸과 route/menu builder 함수를 분리해 `bun test`로 검증

우선 추가하면 좋은 테스트:

- 조직 구독 active 사용자 판별 결과가 quota 입력 비활성화 props로 전달되는지 DOM 수준에서 확인
- 조직 구독 취소 후 실제 화면 목록이 갱신되고 같은 사용자를 다시 할당할 수 있는지 E2E로 확인
- 조직 사용 로그/작업 로그 화면에서 선택 조직의 실제 데이터 행과 필터 결과가 맞는지 E2E로 확인
- 한국어 locale에 지갑/조직 구독 관련 key가 존재하고 영어 fallback 없이 렌더링되는지

## 8. 결론

현재 테스트는 조직 관리 기능의 서버 측 정책과 주요 프론트 smoke flow를 함께 커버한다. 조직 생성, 권한 경계, 조직 범위 집계, 조직 quota 과금, 조직 구독 자동 갱신과 중복 active 구독 방지는 백엔드 테스트로 잘 보호된다. 프론트에서는 Playwright E2E로 역할별 메뉴, 조직 선택 유지, 조직 사용 로그 화면 렌더링을 검증한다.

반면 조직 구독 화면에서 실제 취소 버튼을 누른 뒤 목록이 갱신되는 상태 변경형 E2E, 한국어 번역 품질, 로그/차트의 시각적 정확성은 아직 충분히 자동화되어 있지 않다. 조직 쓰기 API 입력값 검증과 영어/한국어만 남기는 UI 언어 정책 테스트가 추가되면서 현재 전체 요구사항 기준 가중 커버리지는 89.6%다. 다음 상승폭은 상태 변경형 Playwright E2E와 DOM 수준 프론트 컴포넌트 테스트를 보강할 때 가장 크다.
