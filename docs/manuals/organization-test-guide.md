# 조직 관리 기능 테스트 가이드

작성일: 2026-06-12

## 1. 목적

이 문서는 조직 관리 기능을 최종 검수할 때 어떤 순서로 테스트하면 좋은지 정리한다.

다루는 범위:

- 조직 생성과 소유자 지정
- 조직 역할과 권한
- 조직 사용자 관리
- 조직 quota 지갑
- 조직 사용자 일반 quota 방식
- 조직 전용 구독 플랜 방식
- 조직 구독 할당, 취소, 자동 갱신
- 조직 대시보드, 사용 로그, 작업 로그
- 전체 관리자 조직 선택 화면
- 조직 사용자 지갑 표시
- 조직 쓰기 API 입력값 검증
- 자동 테스트 커버 범위와 수동 확인 필요 범위

관련 문서:

- `docs/superpowers/specs/2026-06-09-organization-management-integrated-design.md`
- `docs/superpowers/specs/2026-06-10-organization-test-coverage.md`
- `docs/manuals/organization-management-manual.md`

## 2. 테스트 전 준비

### 2.1 서버 실행

백엔드와 프론트엔드를 모두 실행한다.

일반적인 확인 주소:

- 백엔드: `http://localhost:3000`
- 프론트엔드: `http://localhost:3001`

프로젝트 환경에 따라 포트는 달라질 수 있다.

### 2.2 테스트 계정

최소한 다음 역할의 계정이 필요하다.

| 역할 | 필요 이유 |
| --- | --- |
| 전체 관리자 | 조직 생성, 조직 quota 조정, 조직 선택 조회 검증 |
| 조직 소유자 | 조직 사용자 추가, 조직 지갑, 조직 구독 관리 검증 |
| 조직 관리자 | 조직 사용자 quota, 조직 구독, 모니터링 권한 검증 |
| 조직 일반 사용자 | API 사용, 지갑 표시, 관리 메뉴 비노출 검증 |
| 조직 밖 일반 사용자 | 조직 소유자 후보, 조직 멤버 할당 후보 검증 |
| 다른 조직 사용자 | 조직 범위 격리 검증 |

테스트 환경에서 이미 알려진 계정이 있다면 그대로 사용한다. 예를 들어 `atto.o`, `atto.1` 같은 계정을 사용할 수 있다. 비밀번호가 공통으로 설정된 환경이라면 해당 값으로 로그인한다.

### 2.3 테스트 데이터 기준

권장 초기 상태:

- 조직 A: 소유자 1명, 관리자 1명, 일반 사용자 1명
- 조직 B: 다른 조직 범위 검증용 사용자 1명
- 조직 밖 일반 사용자 1명
- 조직 A quota: 충분한 값
- 조직 일반 사용자 quota: 충분한 값
- 조직 구독 플랜: 아직 없음

## 3. 자동 테스트 실행 방법

### 3.1 백엔드 조직 관련 테스트

```bash
go test ./model ./service ./controller -run 'TestOrganization' -count=1 -timeout=30s
```

검증하는 내용:

- 조직 생성과 owner 지정
- 이미 조직에 속한 사용자 차단
- 조직 역할 검증
- 조직 관리자/소유자 권한
- 조직 밖 사용자 관리 차단
- 조직 대시보드 집계 범위
- 조직 quota 방식 과금
- 조직 구독 방식 과금
- 조직 구독 정산과 환불
- 조직 구독 자동 갱신
- 조직 구독 중복 active 방지
- 조직 구독 controller 권한과 범위
- 조직 생성/수정, 조직 사용자 수정, 조직 구독 플랜 생성/수정, 구독 할당 API의 길이/숫자/enum 검증

### 3.2 프론트 테스트

`web/default`에서 실행한다.

```bash
bun run test
```

검증하는 내용:

- UI 언어 선택지가 영어/한국어만 남는지
- 제거된 언어 값이 영어로 fallback되는지
- active 조직 구독 사용자 식별
- active plan 사용자를 할당 후보에서 제외하는 유틸
- active plan 사용자의 quota 입력 잠금 판단
- 마지막 선택 조직 저장/삭제/복구 helper
- 조직 plan 사용자의 지갑 잔액 대체 문구
- 지갑 카드에서 사용량과 요청 수 유지

### 3.3 브라우저 E2E 테스트

`web/default`에서 실행한다.

```bash
bun run e2e
```

새 환경에서 Playwright Chromium이 설치되어 있지 않다면 최초 1회 다음 명령을 먼저 실행한다.

```bash
bun run e2e:install
```

검증하는 내용:

- 전체 관리자 로그인과 조직 메뉴 노출
- 조직 대시보드/조직 구독 화면 진입
- 전체 관리자 조직 선택 값 유지
- 조직 소유자/관리자 메뉴 노출과 조직 관리 화면 접근
- 조직 사용 로그 화면 렌더링
- 조직 일반 사용자 조직 관리 메뉴 비노출
- 조직 일반 사용자 조직 관리 URL 직접 접근 차단

### 3.4 현재 자동 테스트 커버 요약

현재 설계 요구사항 기준 커버리지는 다음과 같다.

| 구분 | 직접 커버 | 부분 커버 | 미커버 | 가중 커버율 |
| --- | ---: | ---: | ---: | ---: |
| 백엔드 | 13 | 1 | 0 | 96.4% |
| 프론트엔드 | 7 | 2 | 1 | 80.0% |
| 전체 | 20 | 3 | 1 | 89.6% |

주의:

- 이 수치는 설계서의 요구사항 항목 기준이다.
- 실제 UI 화면 클릭 중 역할별 메뉴, 조직 선택 유지, 조직 사용 로그 화면 렌더링은 E2E로 자동 검증한다.
- UI 언어 선택지가 영어/한국어만 제공되고 제거된 언어 값이 영어로 fallback되는지는 단위 테스트로 자동 검증한다.
- 지갑의 조직 plan 문구는 컴포넌트/유틸 테스트로 자동 검증한다.
- active plan 사용자의 quota 입력 잠금과 cancelled plan 사용자의 재할당 후보 복귀는 유틸 테스트로 자동 검증한다.
- 차트 렌더링의 시각적 정확성, 로그 행 데이터, 실제 구독 취소 버튼 클릭 후 화면 refresh, 한국어 번역 품질은 아직 수동 검증이 필요하다.

## 4. 최종 수동 테스트 권장 시나리오

아래 순서대로 진행하면 조직 생성부터 quota, 구독, 모니터링까지 한 흐름으로 확인할 수 있다.

## 4.1 시나리오 A: 전체 관리자 조직 생성

목적:

- 전체 관리자가 조직을 만들고 조직 소유자를 지정할 수 있는지 확인한다.
- 조직 소유자 후보 목록에 조직과 무관한 일반 사용자만 나오는지 확인한다.

절차:

1. 전체 관리자 계정으로 로그인한다.
2. `조직 사용자` 메뉴로 이동한다.
3. `조직 생성` 영역에서 조직 이름을 입력한다.
4. 설명을 입력한다.
5. 초기 조직 quota를 입력한다.
6. 소유자 사용자를 선택한다.
7. `생성` 버튼을 누른다.
8. 생성된 조직이 조직 목록에 표시되는지 확인한다.
9. 소유자 계정으로 로그인해 조직 메뉴가 보이는지 확인한다.

기대 결과:

- 조직이 생성된다.
- 선택한 사용자가 조직 `owner`가 된다.
- 이미 조직에 속한 사용자, 다른 조직 사용자, 전역 관리자 계정은 소유자 후보에서 제외된다.

자동 테스트 커버:

- `TestOrganizationRootCanCreateOrganization`
- `TestOrganizationRootAcceptsCreateBoundaryInput`
- `TestOrganizationRootRejectsInvalidCreateInput`
- `TestCreateOrganizationAssignsOwner`
- `TestCreateOrganizationRejectsUserAlreadyInOrganization`
- `TestOrganizationOwnerCanListAssignableUsers`

직접 확인 필요:

- 실제 UI의 소유자 드롭다운 목록
- 생성 폼의 표시와 validation 메시지
- 생성 후 화면 refresh와 목록 반영
- 소유자 계정에서 메뉴가 올바르게 보이는지

## 4.2 시나리오 B: 조직 역할별 메뉴와 권한

목적:

- 전체 관리자, 조직 소유자, 조직 관리자, 조직 일반 사용자의 접근 범위가 맞는지 확인한다.

절차:

1. 전체 관리자로 로그인한다.
2. 조직 대시보드, 조직 사용 로그, 조직 작업 로그, 조직 구독 메뉴가 보이는지 확인한다.
3. 조직 소유자로 로그인한다.
4. 조직 사용자, 조직 대시보드, 조직 사용 로그, 조직 작업 로그, 조직 구독 메뉴가 보이는지 확인한다.
5. 조직 관리자로 로그인한다.
6. 조직 대시보드, 로그, 구독 관리가 가능한지 확인한다.
7. 조직 일반 사용자로 로그인한다.
8. 조직 관리 메뉴가 보이지 않는지 확인한다.
9. 조직 일반 사용자가 조직 관리 URL에 직접 접근했을 때 권한 오류가 나는지 확인한다.

기대 결과:

- 조직 일반 사용자는 관리 메뉴를 볼 수 없다.
- 조직 소유자와 관리자는 자기 조직 관리 화면에 접근할 수 있다.
- 전체 관리자는 조직 선택 후 조직 화면을 볼 수 있다.

자동 테스트 커버:

- `TestOrganizationMemberCannotReadDashboard`
- `TestOrganizationAdminCanReadDashboard`
- `TestOrganizationOwnerCanReadDashboard`
- `TestOrganizationRootCanReadSelectedOrganizationDashboard`
- `TestOrganizationMemberCannotManageSubscriptionPlan`
- `organization.e2e.ts`
  - 전체 관리자 조직 메뉴 노출
  - 조직 소유자/관리자 조직 메뉴 노출
  - 조직 일반 사용자 조직 메뉴 비노출
  - 조직 일반 사용자 URL 직접 접근 403

직접 확인 필요:

- 조직 사용 로그/작업 로그 화면의 실제 데이터 행
- 조직 소유자와 조직 관리자 사이의 세부 버튼/액션 차이
- 모바일 또는 좁은 화면에서 메뉴 표시가 깨지지 않는지

## 4.3 시나리오 C: 조직 사용자 추가와 사용자 관리

목적:

- 조직 소유자가 사용자를 조직에 추가할 수 있는지 확인한다.
- 조직 관리자는 새 멤버 할당은 못 하고, 기존 조직 사용자 관리만 가능한지 확인한다.

절차:

1. 조직 소유자로 로그인한다.
2. `조직 사용자` 메뉴로 이동한다.
3. 조직 밖 일반 사용자를 선택한다.
4. 역할을 `member`로 선택하고 할당한다.
5. 새 사용자가 조직 사용자 목록에 표시되는지 확인한다.
6. 조직 관리자로 로그인한다.
7. 같은 화면에서 멤버 할당 영역이 보이지 않거나 사용할 수 없는지 확인한다.
8. 조직 관리자가 기존 member의 quota, 상태, remark를 수정할 수 있는지 확인한다.
9. 조직 관리자가 다른 조직 사용자 또는 전역 관리자 사용자를 수정할 수 없는지 확인한다.

기대 결과:

- 소유자는 조직 밖 일반 사용자를 자기 조직에 추가할 수 있다.
- 관리자는 새 멤버십 할당을 할 수 없다.
- 관리자는 자기 조직 일반 사용자의 quota와 상태를 수정할 수 있다.
- 다른 조직 사용자와 전역 관리자는 수정할 수 없다.

자동 테스트 커버:

- `TestOrganizationOwnerCanAssignMemberRole`
- `TestOrganizationAdminCannotAssignMembership`
- `TestOrganizationAdminCannotListAssignableUsers`
- `TestOrganizationAdminCanUpdateQuotaForMember`
- `TestOrganizationAdminAcceptsUserUpdateBoundaryInput`
- `TestOrganizationAdminRejectsInvalidUserUpdateInput`
- `TestOrganizationAdminCannotUpdateOutsideOrganization`
- `TestOrganizationAdminCannotUpdateGlobalAdmin`
- `TestOrganizationOwnerCannotAssignGlobalAdmin`
- `TestOrganizationOwnerCannotAssignInvalidRole`

직접 확인 필요:

- 사용자 목록 스크롤과 긴 목록 표시
- 실제 quota 입력 UX
- 상태 변경 버튼 UX
- role select 표시와 번역

## 4.4 시나리오 D: 조직 quota 지갑과 일반 quota 방식 사용

목적:

- 조직 구독이 없는 사용자가 사용자 quota와 조직 quota를 함께 사용하는지 확인한다.

절차:

1. 전체 관리자로 조직 quota를 충분히 설정한다.
2. 조직 소유자 또는 관리자로 조직 사용자 quota를 충분히 설정한다.
3. 조직 일반 사용자로 로그인한다.
4. 지갑 화면에서 조직이 quota를 관리한다는 안내와 본인 quota/사용량을 확인한다.
5. API 요청을 1회 수행한다.
6. 사용자 지갑 또는 사용자 목록에서 사용량이 증가했는지 확인한다.
7. 조직 대시보드에서 조직 사용량이 증가했는지 확인한다.
8. 조직 사용 로그에 해당 사용자의 요청이 표시되는지 확인한다.

기대 결과:

- 요청 전 사용자 quota와 조직 quota가 충분하면 요청이 성공한다.
- 사용자 사용량과 조직 사용량이 함께 증가한다.
- 사용 로그의 사용자는 실제 요청 사용자로 기록된다.

자동 테스트 커버:

- `TestOrganizationWalletBillingPreConsumesMemberLimitAndOrganizationWallet`
- `TestOrganizationWalletBillingSettlesRefundToMemberLimitAndOrganizationWallet`
- `TestOrganizationWalletBillingOwnerRequestUsesOrganizationWallet`
- `organization.e2e.ts`
  - 조직 일반 사용자가 지갑에서 직접 충전 대신 조직 할당량 안내를 보는지 확인

직접 확인 필요:

- 실제 API 요청 성공 여부
- UI에서 숫자가 갱신되는 타이밍
- 조직 사용 로그에 표시되는 모델명, 시간, 금액
- 실제 relay 로그와 대시보드 값의 일치

## 4.5 시나리오 E: 조직 quota 부족 실패

목적:

- 조직 quota가 부족하면 사용자 quota가 남아 있어도 요청이 실패하는지 확인한다.

절차:

1. 조직 사용자 quota는 충분히 설정한다.
2. 조직 quota를 아주 작게 설정한다.
3. 조직 일반 사용자로 API 요청을 수행한다.
4. 오류 메시지를 확인한다.
5. 사용자 quota와 조직 quota가 잘못 차감되지 않았는지 확인한다.

기대 결과:

- 요청이 실패한다.
- 조직 quota 부족 계열 메시지가 표시된다.
- 사용자 quota와 조직 quota가 예기치 않게 줄어들지 않는다.

자동 테스트 커버:

- `TestOrganizationWalletBillingRejectsWhenOrganizationWalletInsufficient`

직접 확인 필요:

- 실제 화면/API에서 보이는 오류 문구
- 프론트 toast 또는 에러 화면 처리
- 실패 로그 표시 여부

## 4.6 시나리오 F: 조직 구독 플랜 생성과 수정

목적:

- 조직 소유자 또는 조직 관리자가 조직 전용 구독 플랜을 만들고 수정할 수 있는지 확인한다.

절차:

1. 조직 소유자 또는 조직 관리자 계정으로 로그인한다.
2. `조직 구독` 메뉴로 이동한다.
3. 제목, 부제목, 금액, 기간을 입력한다.
4. 기간이 `사용자 지정`이 아닐 때 `사용자 지정 초` 입력이 비활성화되는지 확인한다.
5. 기간을 `사용자 지정`으로 바꾼 뒤 `사용자 지정 초`가 활성화되는지 확인한다.
6. 플랜을 생성한다.
7. 플랜 목록에 기간과 금액이 올바르게 표시되는지 확인한다.
8. 수정 버튼을 눌러 폼에 기존 값이 불러와지는지 확인한다.
9. 값을 변경하고 저장한다.
10. 플랜을 비활성화한다.

기대 결과:

- 플랜 생성/수정/비활성화가 가능하다.
- quota reset, reset custom seconds, upgrade group 입력은 UI에 노출되지 않는다.
- 리셋 주기는 플랜 기간 정책을 따른다.
- 비활성화된 플랜은 새 할당 대상에서 제외된다.

자동 테스트 커버:

- `TestOrganizationOwnerCanManageSubscriptionPlan`
- `TestOrganizationSubscriptionPlanAcceptsBoundaryInput`
- `TestOrganizationSubscriptionPlanRejectsInvalidInput`
- `TestOrganizationMemberCannotManageSubscriptionPlan`
- `TestOrganizationRootCanManageSelectedOrganizationSubscription`
- `TestOrganizationRootSubscriptionPlanRequiresOrganizationId`

직접 확인 필요:

- 실제 플랜 생성 폼 레이아웃
- 사용자 지정 초 enable/disable UI
- 플랜 목록의 기간 표시
- 비활성화 후 화면 표시
- 한국어 번역

## 4.7 시나리오 G: 조직 구독 플랜 할당

목적:

- 조직 사용자에게 조직 구독 플랜을 할당할 수 있는지 확인한다.
- active plan 사용자가 중복 할당 후보에서 제외되는지 확인한다.

절차:

1. 조직 구독 플랜을 하나 이상 만든다.
2. `사용자 플랜 할당` 영역에서 조직 사용자를 선택한다.
3. 플랜을 선택한다.
4. `할당` 버튼을 누른다.
5. 현재 할당 목록에 사용자가 표시되는지 확인한다.
6. 같은 사용자가 새 할당 사용자 드롭다운에서 사라졌는지 확인한다.
7. 해당 사용자로 로그인해 지갑 화면을 확인한다.

기대 결과:

- 사용자는 active 조직 구독을 가진다.
- 같은 사용자에게 여러 active 조직 구독이 남지 않는다.
- active plan 사용자는 새 할당 후보에서 제외된다.
- 사용자 지갑에는 잔액 대신 `조직 플랜 사용 중`이 표시된다.

자동 테스트 커버:

- `TestCreateOrganizationSubscriptionFromPlanCancelsExistingActiveSubscription`
- `TestCreateOrganizationSubscriptionFromPlanCancelsExpiredActiveSubscription`
- `TestOrganizationSubscriptionAssignmentAcceptsBoundaryPlanId`
- `TestOrganizationSubscriptionAssignmentRejectsInvalidPlanId`
- `filters active plan users out of assignable plan users`
- `detects only active organization plan assignments`
- `organization member wallet balance display > replaces current balance copy for active organization plan users`
- `WalletStatsCard > renders organization plan balance message while keeping usage and request stats`
- `organization.e2e.ts`
  - 조직 일반 사용자 지갑의 조직 할당량 안내 화면 진입

직접 확인 필요:

- 실제 드롭다운에서 active 사용자 제외
- 할당 성공 후 선택값 초기화
- 현재 할당 목록 refresh
- active 조직 plan이 할당된 실제 계정의 지갑 전체 표시
- 만료일과 사용량 표시

## 4.8 시나리오 H: 조직 구독 방식 API 사용

목적:

- active 조직 구독 사용자가 개인 quota 대신 조직 구독 한도를 사용하는지 확인한다.

절차:

1. 조직 quota를 충분히 설정한다.
2. 조직 사용자에게 충분한 조직 구독 플랜을 할당한다.
3. 필요하다면 사용자 개인 quota를 낮게 설정한다.
4. 해당 사용자로 API 요청을 수행한다.
5. 사용자 개인 quota가 줄지 않는지 확인한다.
6. 조직 구독 사용량이 증가하는지 확인한다.
7. 조직 quota가 함께 줄어드는지 확인한다.
8. 조직 대시보드와 사용 로그에 사용량이 반영되는지 확인한다.

기대 결과:

- active 조직 구독 사용자는 개인 quota로 fallback하지 않는다.
- 조직 구독 사용량과 조직 quota가 함께 차감된다.
- 사용량과 로그는 실제 요청 사용자 기준으로 표시된다.

자동 테스트 커버:

- `TestOrganizationSubscriptionBillingPreConsumesSubscriptionAndOrganizationWallet`
- `TestOrganizationSubscriptionBillingSettlesRefundToSubscriptionAndOrganizationWallet`
- `TestOrganizationSubscriptionBillingRefundRestoresSubscriptionAndOrganizationWallet`
- `TestOrganizationSubscriptionBillingRejectsWhenPlanLimitInsufficient`

직접 확인 필요:

- 실제 API 요청 후 UI 표시
- 조직 구독 사용량 표시
- 조직 quota 표시
- 대시보드/로그 반영 시점

## 4.9 시나리오 I: 조직 구독 한도 부족 실패

목적:

- 조직 구독 한도가 부족하면 사용자 개인 quota가 남아 있어도 요청이 실패하는지 확인한다.

절차:

1. 조직 quota는 충분히 설정한다.
2. 조직 사용자에게 매우 작은 구독 플랜을 할당한다.
3. 사용자 개인 quota는 충분히 설정한다.
4. 해당 사용자로 구독 한도를 초과하는 API 요청을 수행한다.
5. 오류 메시지를 확인한다.
6. 사용자 개인 quota로 fallback되지 않았는지 확인한다.

기대 결과:

- 요청이 실패한다.
- 조직 구독 한도 부족 메시지가 표시된다.
- 개인 quota는 사용되지 않는다.
- 조직 quota도 잘못 차감되지 않는다.

자동 테스트 커버:

- `TestOrganizationSubscriptionBillingRejectsWhenPlanLimitInsufficient`

직접 확인 필요:

- 실제 UI/API 오류 문구
- 실패 시 프론트 표시
- 로그 기록 방식

## 4.10 시나리오 J: 조직 구독 취소와 일반 quota 복귀

목적:

- 조직 구독 취소 후 사용자가 다시 일반 quota 방식으로 운영되는지 확인한다.

절차:

1. 조직 사용자에게 active 조직 구독을 할당한다.
2. 조직 구독 화면의 현재 할당 목록에서 해당 사용자를 취소한다.
3. 사용자가 현재 할당 목록에서 사라지는지 확인한다.
4. 사용자가 플랜 할당 드롭다운에 다시 나타나는지 확인한다.
5. 조직 사용자 화면에서 quota 입력칸이 다시 활성화되는지 확인한다.
6. 사용자 quota를 충분히 설정한다.
7. 해당 사용자로 API 요청을 수행한다.

기대 결과:

- active 조직 구독이 취소된다.
- 취소된 사용자는 다시 할당 후보가 된다.
- quota 입력이 다시 가능해진다.
- API 요청은 일반 조직 quota 방식으로 동작한다.

자동 테스트 커버:

- `TestCancelOrganizationUserSubscriptionCancelsExpiredActiveSubscription`
- `TestOrganizationAdminCanCancelActiveSubscription`
- `marks quota controls disabled only for active organization plan users`

직접 확인 필요:

- 취소 후 UI 목록 refresh
- 드롭다운 재등장
- quota input/button disabled 해제
- 취소 후 실제 API 사용 방식

## 4.11 시나리오 K: 조직 구독 자동 갱신

목적:

- active 조직 구독이 만료 시간이 지난 뒤에도 다음 조회 또는 요청 시 자동 갱신되는지 확인한다.

절차:

1. 짧은 기간의 테스트 플랜을 만든다.
2. 사용자에게 플랜을 할당한다.
3. 구독 만료 조건을 만든다.
4. active 구독 조회 또는 API 요청을 수행한다.
5. 구독 기간이 새로 계산되고 사용량이 초기화되는지 확인한다.

기대 결과:

- active 상태 구독은 자동 갱신된다.
- `amount_used`가 새 기간 기준으로 초기화된다.
- cancelled 구독은 자동 갱신되지 않는다.

자동 테스트 커버:

- `TestHasActiveOrganizationUserSubscriptionRenewsExpiredSubscription`
- `TestPreConsumeOrganizationUserSubscriptionRenewsExpiredSubscription`
- `TestPreConsumeOrganizationUserSubscriptionResetsPeriodicQuota`

직접 확인 필요:

- UI에서 갱신된 만료일 표시
- 실제 긴 시간 기반 테스트
- cancelled 구독의 UI 표시

## 4.12 시나리오 L: 조직 대시보드와 로그

목적:

- 조직 대시보드와 로그가 조직 범위로 제한되는지 확인한다.

절차:

1. 조직 A 사용자로 API 요청을 몇 번 수행한다.
2. 조직 B 사용자로 API 요청을 몇 번 수행한다.
3. 조직 A 소유자 또는 관리자로 로그인한다.
4. 조직 대시보드에서 조직 A 사용자 사용량만 보이는지 확인한다.
5. 사용자별 차트와 모델별 차트를 확인한다.
6. 조직 사용 로그에서 조직 A 사용자 로그만 보이는지 확인한다.
7. 조직 작업 로그에서 조직 관련 작업 이력이 보이는지 확인한다.
8. 전체 관리자로 로그인한다.
9. 조직 A를 선택하고 대시보드/로그를 확인한다.
10. 조직 B를 선택하고 데이터가 바뀌는지 확인한다.

기대 결과:

- 조직 밖 사용자의 사용량은 섞이지 않는다.
- 전체 관리자는 조직 선택에 따라 데이터가 바뀐다.
- 사용자별, 모델별, 일자별 차트가 표시된다.
- 사용 로그와 작업 로그가 조직 범위로 제한된다.

자동 테스트 커버:

- `TestGetOrganizationDashboardScopesQuotaDataToOrganization`
- `TestOrganizationOwnerCanReadDashboard`
- `TestOrganizationRootCanReadSelectedOrganizationDashboard`
- `TestOrganizationRootCanReadSelectedOrganizationLogs`
- `TestOrganizationDashboardAcceptsPresetRanges`
- `TestOrganizationDashboardRejectsInvalidPreset`
- `TestOrganizationDashboardRejectsMalformedStartTimestamp`
- `TestOrganizationDashboardRejectsMalformedEndTimestamp`

직접 확인 필요:

- 실제 차트 렌더링
- 차트 tooltip, legend, 기간 필터
- 작업 로그 화면 표시
- 전체 관리자 조직 선택 UI
- 메뉴 이동 후 선택 조직 유지

## 4.13 시나리오 M: 전체 관리자 조직 선택 유지

목적:

- 전체 관리자가 선택한 조직이 조직 대시보드, 사용 로그, 작업 로그, 조직 구독 사이에서 유지되는지 확인한다.

절차:

1. 전체 관리자 계정으로 로그인한다.
2. 조직 대시보드에서 조직 A를 선택한다.
3. 조직 사용 로그로 이동한다.
4. 선택 조직이 조직 A로 유지되는지 확인한다.
5. 조직 작업 로그로 이동한다.
6. 선택 조직이 조직 A로 유지되는지 확인한다.
7. 조직 구독 메뉴로 이동한다.
8. 선택 조직이 조직 A로 유지되는지 확인한다.
9. 조직 B로 변경한 뒤 다시 메뉴 이동을 반복한다.

기대 결과:

- 마지막 선택 조직이 localStorage에 저장된다.
- 메뉴 이동 후에도 선택 조직이 유지된다.
- 저장된 조직이 더 이상 목록에 없으면 사용 가능한 첫 조직으로 fallback한다.

자동 테스트 커버:

- `saves, reads, and clears the last selected organization id`
- `falls back to the first available organization when saved id is unavailable`
- `returns an empty id when storage is unavailable`
- `organization.e2e.ts`
  - 조직 대시보드에서 선택된 조직이 조직 사용 로그 이동 후에도 유지되는지 확인

직접 확인 필요:

- 브라우저 새로고침 후 유지
- 조직 삭제 또는 접근 불가 상태에서 fallback

## 4.14 시나리오 N: 한국어 UI와 문구

목적:

- UI 언어 선택지가 영어와 한국어만 제공되는지 확인한다.
- 조직 관련 화면이 한국어 환경에서 어색한 영문 문구 없이 표시되는지 확인한다.

절차:

1. 앱 언어 선택 메뉴를 연다.
2. 선택 가능한 언어가 `English`, `한국어` 두 개뿐인지 확인한다.
3. 브라우저 언어 또는 앱 언어를 한국어로 설정한다.
4. 조직 사용자, 조직 대시보드, 조직 사용 로그, 조직 작업 로그, 조직 구독, 지갑 화면을 확인한다.
5. 다음 문구가 한국어로 표시되는지 확인한다.
   - 조직 플랜 사용 중
   - 현재 잔액
   - 총 사용량
   - 총 요청 수
   - 기본값
   - 조직 구독
   - 조직 사용 로그
   - 조직 작업 로그
6. 플랜 생성/수정 폼의 label과 버튼 문구를 확인한다.
7. 오류 toast와 빈 상태 문구를 확인한다.

기대 결과:

- 언어 선택 UI에는 영어와 한국어만 표시된다.
- 주요 사용자 노출 문구가 한국어로 표시된다.
- `default` 같은 내부 값은 필요한 경우 `기본값`으로 표시된다.
- Organization Subscription 같은 영문이 불필요하게 노출되지 않는다.

자동 테스트 커버:

- `languages.test.ts`
  - UI 언어 선택지가 `en`, `kr`만 포함되는지 확인
  - `ko`, `ko-KR`, `kr`을 `kr`로 정규화하는지 확인
  - 제거된 `zh-CN`, `fr`, unknown 값이 `en`으로 fallback되는지 확인

직접 확인 필요:

- 모든 화면의 실제 한국어 번역 표시와 문구 품질
- toast와 오류 메시지
- 날짜, quota, 통화 표시 형식

## 4.15 시나리오 O: 조직 쓰기 API 입력값 검증

목적:

- 조직 생성/수정, 조직 사용자 수정, 조직 구독 플랜 생성/수정, 구독 할당에서 잘못된 값이 저장되지 않는지 확인한다.
- UI에서 막는 값도 서버가 최종적으로 거부하는지 확인한다.

절차:

1. 전체 관리자 계정으로 로그인한다.
2. 조직 생성에서 65자 이상 이름, 256자 이상 설명, 1,000,000,000 초과 quota를 각각 입력해 본다.
3. 조직 생성에서 빈 이름, owner ID 0, owner ID 1,000,000,001, quota -1도 API 또는 UI 개발 도구로 전송해 본다.
4. 조직 수정에서 256자 이상 설명, quota -1, 1,000,000,000 초과 quota, 허용되지 않는 상태값을 API 또는 UI 개발 도구로 전송해 본다.
5. 조직 소유자 또는 조직 관리자 계정으로 로그인한다.
6. 조직 사용자 수정에서 quota -1, 1,000,000,000 초과 quota, 허용되지 않는 상태값, 256자 이상 remark를 입력해 본다.
7. 조직 구독 플랜 생성에서 빈 제목, 129자 이상 제목, 256자 이상 부제목, 잘못된 기간 단위, -1 또는 1201 기간 값, 0 또는 31,536,001 사용자 지정 초, -1 또는 1,000,000,001 금액, -1,000,001 또는 1,000,001 정렬값을 입력해 본다.
8. 조직 구독 플랜 생성에서 `quota_reset_period`, `quota_reset_custom_seconds`, `upgrade_group`, `enabled` 같은 허용되지 않은 필드를 API로 전송해 본다.
9. 조직 구독 할당에서 `plan_id` 0 또는 1,000,000,000 초과 값을 API로 전송해 본다.
10. 조직 구독 할당에서 `quota` 같은 허용되지 않은 필드를 API로 전송해 본다.

기대 결과:

- 잘못된 값은 저장되지 않는다.
- 서버 응답은 `success=false` 또는 provider별 오류 응답으로 반환된다.
- 허용되지 않은 필드는 `unsupported field: <field>` 메시지로 거부된다.
- 정상 범위 안의 값은 기존처럼 저장된다.

자동 테스트 커버:

- `TestOrganizationRootRejectsInvalidCreateInput`
- `TestOrganizationRootRejectsInvalidUpdateInput`
- `TestOrganizationAdminRejectsInvalidUserUpdateInput`
- `TestOrganizationSubscriptionPlanRejectsInvalidInput`
- `TestOrganizationSubscriptionAssignmentRejectsInvalidPlanId`

직접 확인 필요:

- 실제 UI에서 표시되는 validation 메시지
- 저장 실패 후 화면 값이 이상하게 남지 않는지
- 브라우저 개발 도구 또는 API client로 직접 보낸 잘못된 값도 서버에서 거부되는지

## 5. 자동 테스트와 수동 테스트의 역할 분리

### 자동 테스트가 잘 커버하는 영역

- 조직 생성 시 owner 지정
- 이미 조직에 속한 사용자 차단
- 조직 역할 값 검증
- 조직 관리자/소유자 API 권한
- 조직 밖 사용자 관리 차단
- 조직 대시보드 집계 범위
- 조직 quota 방식 선차감과 정산
- 조직 quota 부족 실패
- 조직 구독 방식 선차감, 정산, 환불
- 조직 구독 한도 부족 실패
- 개인 quota fallback 방지
- active 구독 자동 갱신
- 중복 active 구독 방지
- 조직 구독 API 권한
- 조직 쓰기 API 입력값 경계값 허용, 범위 초과 거부, enum 검증
- active plan 사용자 판별 유틸
- 지갑 카드의 조직 plan 문구 표시
- 조직 선택 localStorage helper
- 역할별 조직 메뉴 노출/비노출
- 조직 일반 사용자 관리 URL 직접 접근 차단
- 전체 관리자 조직 선택 유지 E2E
- 조직 사용 로그 화면 렌더링 E2E
- 지갑 카드의 조직 plan 문구 컴포넌트 테스트
- 영어/한국어만 남긴 UI 언어 선택지와 제거된 언어 fallback

### 수동 테스트가 꼭 필요한 영역

- 조직 사용자 화면의 input/button disabled 상태
- 조직 구독 취소 후 목록 refresh와 재할당 가능 상태
- 대시보드 차트 렌더링과 시각적 정확성
- 조직 사용 로그/작업 로그 화면 필터
- 조직 사용 로그/작업 로그의 실제 행 데이터
- 한국어 번역 품질과 화면별 실제 문구
- 실제 API 요청과 화면 수치 갱신 타이밍
- 결제 provider를 통한 조직 지갑 충전

## 6. 최종 검수 체크리스트

릴리스 또는 병합 전에는 다음을 확인한다.

### 자동 테스트

- [ ] `go test ./model ./service ./controller -run 'TestOrganization' -count=1 -timeout=30s`
- [ ] `cd web/default && bun run test`
- [ ] `cd web/default && bun run e2e`

### 전체 관리자

- [ ] 조직 생성 가능
- [ ] 조직 소유자 후보가 조직 밖 일반 사용자로 제한됨
- [ ] 조직 quota 수정 가능
- [ ] 조직 생성/수정 입력값 제한이 UI와 서버에서 동작함
- [ ] 조직 대시보드/로그/구독에서 조직 선택 가능
- [ ] 조직 선택이 메뉴 이동 후 유지됨

### 조직 소유자

- [ ] 조직 사용자 추가 가능
- [ ] 사용자 역할 변경 가능
- [ ] 조직 지갑 확인 가능
- [ ] 조직 구독 플랜 생성/수정/비활성화 가능
- [ ] 사용자에게 plan 할당/취소 가능

### 조직 관리자

- [ ] 조직 사용자 목록 조회 가능
- [ ] 조직 사용자 quota/status 수정 가능
- [ ] 조직 사용자 quota/status/remark 제한값이 서버에서 검증됨
- [ ] 조직 대시보드/로그 조회 가능
- [ ] 조직 구독 플랜 관리 가능
- [ ] 새 멤버 할당은 불가

### 조직 일반 사용자

- [ ] 조직 관리 메뉴가 보이지 않음
- [ ] 지갑에서 직접 충전 기능이 보이지 않음
- [ ] 일반 quota 방식 사용자에게 할당량/사용량/요청 수가 표시됨
- [ ] 조직 plan 사용자에게 `조직 플랜 사용 중`이 표시됨
- [ ] API 요청 후 사용량이 반영됨

### 구독/과금

- [ ] 일반 quota 방식 요청 성공
- [ ] 조직 quota 부족 시 요청 실패
- [ ] 조직 구독 방식 요청 성공
- [ ] 조직 구독 플랜 입력값 제한과 `plan_id` 검증 동작
- [ ] 조직 구독 한도 부족 시 요청 실패
- [ ] 구독 취소 후 일반 quota 방식으로 복귀
- [ ] 대시보드와 로그에 사용량 반영

## 7. 알려진 검증 한계

현재 테스트 체계의 한계:

- 프론트 컴포넌트 테스트는 대부분 순수 유틸 또는 server render 수준이다.
- 실제 브라우저 클릭 기반 E2E는 smoke flow 중심이다.
- 다중 DB별 조직 기능 테스트는 자동화되어 있지 않다.
- 실제 결제 provider를 통한 조직 지갑 충전은 수동 또는 별도 sandbox 검증이 필요하다.
- 실제 upstream AI provider 요청에 따른 사용량 정산은 환경 의존성이 크므로 수동 확인이 필요하다.
- `bun run typecheck`는 기존 타입 오류로 실패한다.
- ESLint는 현재 의존성 문제로 실행 자체가 실패한다.

## 8. 추천 추가 자동화

우선순위가 높은 추가 자동화:

1. Playwright E2E 확장
   - 조직 생성
   - 조직 구독 할당/취소
   - 조직 사용 로그/작업 로그 데이터 검증
   - 대시보드 차트 렌더링 smoke
2. React 컴포넌트 테스트
   - 조직 사용자 화면 quota input disabled
   - 조직 구독 화면 사용자 드롭다운 필터
   - 플랜 취소 후 목록 갱신
3. i18n 테스트
   - 조직 관련 key 존재 여부
   - 한국어 locale에서 주요 문구가 영어 fallback 없이 렌더링되는지
   - 언어 변경 후 주요 화면이 즉시 갱신되는지
4. DB 호환 테스트
   - SQLite 외 MySQL/PostgreSQL에서 조직 subscription migration과 핵심 쿼리 검증
