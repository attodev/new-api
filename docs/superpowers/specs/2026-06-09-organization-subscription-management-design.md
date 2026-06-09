# 조직 Subscription 관리 기능 설계

## 배경

현재 시스템에는 전역 subscription plan과 사용자별 subscription이 있다. 이 기능은 결제 기반 사용자 subscription을 다루며, 전체 관리자가 plan을 만들고 사용자에게 subscription을 부여할 수 있다.

조직 기능에는 조직 quota 지갑, 조직 사용자 관리, 조직 사용량 대시보드와 로그가 이미 있다. 조직 사용자는 기존 방식대로 사용자별 quota를 배정받아 사용할 수 있고, 사용 금액은 조직 quota에서도 함께 차감된다.

이번 기능은 조직 안에서만 쓰는 subscription plan을 추가한다. 조직 소유자 또는 조직 관리자가 조직 전용 plan을 만들고, 조직 사용자에게 plan을 배정하거나 변경한다. 조직 일반 사용자는 스스로 plan을 구매하거나 변경하지 않는다.

## 목표

- 조직별 전용 subscription plan을 생성, 수정, 활성화, 비활성화할 수 있다.
- 조직 소유자와 조직 관리자가 자기 조직 사용자의 plan을 배정, 변경, 취소할 수 있다.
- 전체 관리자는 조직을 선택해 모든 조직의 조직 subscription을 관리할 수 있다.
- 조직 일반 사용자는 plan을 직접 변경할 수 없다.
- 조직 내 사용자는 기존 quota 방식과 조직 subscription 방식 중 하나로 운영될 수 있다.
- 실제 최종 예산은 기존 조직 quota에서 계속 차감한다.

## 제외 범위

- 조직 전용 plan 결제 기능은 만들지 않는다.
- 조직 전용 plan을 사용자가 직접 구매하는 기능은 만들지 않는다.
- 기존 전역 subscription plan 결제 흐름은 바꾸지 않는다.
- 조직 전용 plan과 전역 plan을 같은 테이블로 합치지 않는다.

## 데이터 모델

### OrganizationSubscriptionPlan

조직 전용 plan 테이블이다. 전역 `SubscriptionPlan`과 분리해 조직 내부 정책이라는 경계를 명확히 한다.

필드:

- `id`
- `organization_id`
- `title`
- `subtitle`
- `duration_unit`
- `duration_value`
- `custom_seconds`
- `total_amount`
- `quota_reset_period`
- `quota_reset_custom_seconds`
- `enabled`
- `sort_order`
- `upgrade_group`
- `created_at`
- `updated_at`

사용하지 않는 필드:

- 가격
- 통화
- 결제 상품 ID
- 구매 제한

### OrganizationUserSubscription

조직 사용자가 배정받은 조직 전용 subscription 인스턴스다.

필드:

- `id`
- `organization_id`
- `user_id`
- `plan_id`
- `amount_total`
- `amount_used`
- `start_time`
- `end_time`
- `status`
- `last_reset_time`
- `next_reset_time`
- `upgrade_group`
- `prev_user_group`
- `assigned_by_user_id`
- `created_at`
- `updated_at`

상태:

- `active`
- `expired`
- `cancelled`

## 권한

전체 관리자:

- 모든 조직의 조직 subscription plan과 사용자 배정을 조회, 생성, 수정, 취소할 수 있다.
- API 요청 시 `organization_id`를 지정해야 한다.

조직 소유자:

- 자기 조직의 plan을 생성, 수정, 활성화, 비활성화할 수 있다.
- 자기 조직 사용자에게 plan을 배정, 변경, 취소할 수 있다.

조직 관리자:

- 조직 소유자와 동일하게 자기 조직의 plan과 사용자 plan을 관리할 수 있다.

조직 일반 사용자:

- 조직 subscription 관리 메뉴를 볼 수 없다.
- 지갑 또는 프로필에서 본인에게 배정된 조직 subscription 요약만 볼 수 있다.
- 직접 plan을 변경할 수 없다.

## 과금 정책

조직 사용자는 두 가지 방식 중 하나로 관리된다.

### 1. 일반 quota 방식

활성 조직 subscription이 없는 사용자는 기존 조직 quota 방식으로 동작한다.

검증:

- 사용자 quota가 충분해야 한다.
- 조직 quota가 충분해야 한다.

차감:

- 사용자 quota를 차감한다.
- 조직 quota를 차감한다.
- 사용자 사용량, 조직 사용량, 로그는 기존 흐름을 유지한다.

### 2. 조직 subscription 방식

활성 조직 subscription이 있는 사용자는 조직 subscription을 사용자별 한도로 사용한다.

검증:

- 조직 subscription의 남은 한도가 충분해야 한다.
- 조직 quota가 충분해야 한다.

차감:

- 조직 subscription의 `amount_used`를 증가시킨다.
- 조직 quota를 차감한다.
- 사용자별 사용량과 조직 사용량은 기존처럼 누적한다.

중요한 점:

- 조직 subscription이 활성 상태이면 사용자 quota를 사용자별 한도로 쓰지 않는다.
- 조직 subscription 한도가 부족하면 사용자 quota가 남아 있어도 fallback하지 않는다.
- 활성 조직 subscription이 없을 때만 기존 사용자 quota 방식으로 사용한다.
- 조직 사용자는 개인 전역 subscription 또는 개인 wallet으로 fallback하지 않는다.

## API 설계

조직 메뉴 아래 API를 추가한다.

### 조직 plan 관리

- `GET /api/organization/subscription/plans`
- `POST /api/organization/subscription/plans`
- `PUT /api/organization/subscription/plans/:id`
- `PATCH /api/organization/subscription/plans/:id/status`

전체 관리자는 query로 `organization_id`를 전달한다. 조직 소유자/관리자는 자기 조직으로 자동 제한된다.

### 조직 사용자 subscription 관리

- `GET /api/organization/subscription/users`
- `GET /api/organization/subscription/users/:id`
- `POST /api/organization/subscription/users/:id/subscriptions`
- `POST /api/organization/subscription/user_subscriptions/:id/invalidate`
- `DELETE /api/organization/subscription/user_subscriptions/:id`

`POST /users/:id/subscriptions`는 해당 사용자에게 새 조직 subscription을 배정한다. 기존 활성 조직 subscription은 정책상 취소하고 새 subscription을 생성한다. 이렇게 하면 "plan 변경"이 명확한 이력으로 남는다.

## UI 설계

조직 메뉴에 `조직 Subscription` 메뉴를 추가한다.

### 전체 관리자 화면

- 상단에 조직 선택 드롭다운을 표시한다.
- 선택한 조직의 plan 목록과 사용자별 subscription 상태를 보여준다.
- 조직 선택은 기존 조직 대시보드와 같은 방식으로 브라우저에 저장한다.

### 조직 소유자/관리자 화면

- 조직 선택 없이 자기 조직 기준으로 표시한다.
- plan 생성/수정 폼을 제공한다.
- 사용자 목록에서 현재 plan, 남은 사용량, 만료일, 상태를 확인한다.
- 사용자별로 plan 배정, 변경, 취소 작업을 수행한다.

### 조직 일반 사용자 화면

- 조직 subscription 관리 메뉴는 표시하지 않는다.
- 지갑 요약에 현재 배정된 조직 subscription이 있으면 이름, 남은 한도, 만료일을 표시한다.

## 구현 방향

### 모델

`model/organization_subscription.go`를 추가한다.

담당 기능:

- 조직 plan CRUD
- 조직 사용자 subscription 생성/취소/삭제
- 활성 조직 subscription 조회
- pre-consume / settle / refund 처리
- 기간 계산과 reset 계산

기존 `model/subscription.go`의 기간 계산과 reset 계산은 전역 subscription과 조직 subscription이 함께 사용할 수 있도록 공통 helper로 분리한다.

### 과금

`service/funding_source.go`에 `OrganizationSubscriptionFunding`을 추가한다.

동작:

- 조직 subscription에서 사용자별 한도를 pre-consume한다.
- 조직 quota도 함께 pre-consume한다.
- settle 시 subscription 사용량과 조직 quota를 함께 조정한다.
- refund 시 둘 다 되돌린다.

`service/billing_session.go`의 조직 사용자 분기에서 활성 조직 subscription을 먼저 확인한다.

- 활성 조직 subscription 있음: `OrganizationSubscriptionFunding`
- 활성 조직 subscription 없음: 기존 `OrganizationWalletFunding`

### 로그

relay log의 `other` 정보에 조직 subscription 정보를 추가한다.

예상 필드:

- `organization_subscription_id`
- `organization_subscription_plan_id`
- `organization_subscription_plan_title`
- `organization_subscription_amount_total`
- `organization_subscription_amount_used_after_pre_consume`

## 에러 처리

조직 subscription 한도 부족:

- 메시지: `조직 subscription 한도가 부족합니다`
- HTTP status: 403
- error code: 기존 quota 부족 계열 사용

조직 quota 부족:

- 기존 조직 quota 부족 메시지를 유지한다.

권한 부족:

- 조직 관리자 권한이 없으면 `organization admin permission required`
- 조직 밖 사용자를 수정하려 하면 `target user is outside organization`

유효하지 않은 plan:

- plan이 비활성화되었거나 다른 조직에 속하면 배정할 수 없다.

## 테스트 계획

백엔드 모델 테스트:

- 조직 plan 생성/수정/비활성화
- 조직 사용자 subscription 생성
- 기존 활성 subscription이 있을 때 새 plan 배정 시 기존 subscription 취소
- 기간과 리셋 주기 계산
- 다른 조직 plan을 배정할 수 없음

백엔드 컨트롤러 테스트:

- 조직 소유자/관리자 권한 허용
- 조직 일반 사용자 접근 거부
- 전체 관리자 조직 선택 허용
- 조직 밖 사용자 배정 거부

과금 테스트:

- 활성 조직 subscription이 있으면 subscription 사용량과 조직 quota가 함께 차감된다.
- 활성 조직 subscription이 없으면 기존 사용자 quota 방식이 유지된다.
- 조직 subscription 한도 부족 시 사용자 quota fallback 없이 실패한다.
- 조직 quota 부족 시 실패한다.
- 정산 환불 시 subscription 사용량과 조직 quota가 함께 복구된다.

프론트 테스트:

- 조직 소유자/관리자 메뉴 노출
- 조직 일반 사용자 메뉴 비노출
- plan 생성/수정/활성화/비활성화
- 사용자별 plan 배정/변경/취소
- 지갑 요약에 조직 subscription 표시

## 기준 테스트 상태

새 워크트리 생성 직후 `go test ./...`를 실행했으나 현재 HEAD 기준으로 실패가 있다.

확인된 실패:

- `web/classic/dist` 누락으로 root package setup 실패
- `controller` 일부 테스트 실패
- `relay/channel/claude` 일부 파일 content 변환 테스트 실패
- `relay/helper` stream scanner 테스트 panic

이 실패들은 이번 설계 작성 전에 존재한 기준 상태로 기록한다.
