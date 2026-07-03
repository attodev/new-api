# 지갑 결제 화면 정리 설계

작성일: 2026-07-02

## 1. 배경

현재 개인 지갑 화면은 `Top up`, `Scheduled recharge`, `Auto recharge`가 같은 상위 탭에 놓이고, 구독 카드는 `Top up` 탭 내부 오른쪽에 별도 카드로 표시된다. 이 구조는 결제성 기능이 여러 위치에 흩어져 보이고, 구독/자동충전/정기충전 사이의 관계도 분명하지 않다.

이번 변경은 결제 화면을 다음 두 축으로 정리한다.

- 왼쪽: 일반 충전
- 오른쪽: 반복/계약성 결제 설정

오른쪽 영역에서는 `구독`, `자동충전`, `정기충전` 중 사용자가 현재 설정할 수 있거나 이미 설정한 항목만 보여준다. 세 항목은 동시에 여러 개 설정하지 못하도록 FE에서만 막는다.

## 2. 목표

- 활성 구독이 없을 때 지갑 화면의 구독 영역을 숨긴다.
- 자동충전과 정기충전을 상위 탭에서 제거하고 구독 영역 옆 결제 설정 영역으로 옮긴다.
- 지갑 화면의 기본 구조를 `충전`과 `결제 설정`의 2컬럼으로 정리한다.
- `구독`, `자동충전`, `정기충전` 중 하나가 active/pending이면 나머지는 UI에서 선택할 수 없게 한다.
- 백엔드 정책과 API는 변경하지 않는다.

## 3. 비목표

- 백엔드에서 구독/자동충전/정기충전 상호 배타를 강제하지 않는다.
- 구독 구매 플로우를 새로 만들지 않는다.
- 자동충전/정기충전 정책 생성 API를 바꾸지 않는다.
- 조직 지갑 화면의 구독 기능을 새로 만들지 않는다.
- 기존 `/subscriptions` 구독 관리 화면을 제거하지 않는다.

## 4. 화면 구조

개인 지갑 화면은 상단 지갑 통계 아래에 결제 영역을 배치한다.

```text
Wallet
┌──────────────────────────────────────────────┐
│ Balance / Usage / Requests                   │
└──────────────────────────────────────────────┘

┌──────────────────────┐ ┌──────────────────────┐
│ Top up               │ │ Payment settings      │
│ 일반 충전            │ │ Subscription          │
│ 결제 수단 / 쿠폰     │ │ Auto recharge         │
│ 결제 내역            │ │ Scheduled recharge    │
└──────────────────────┘ └──────────────────────┘

┌──────────────────────────────────────────────┐
│ Affiliate rewards                            │
└──────────────────────────────────────────────┘
```

반응형:

- 데스크톱: 왼쪽 충전, 오른쪽 결제 설정 2컬럼
- 모바일/좁은 화면: 충전 카드 다음에 결제 설정 카드가 세로로 쌓임
- 오른쪽 결제 설정 항목이 하나도 없으면 오른쪽 카드 자체를 숨기고 충전 카드가 전체 폭을 사용

## 5. 오른쪽 결제 설정 영역

오른쪽 영역은 기존 프로젝트의 `Tabs` 컴포넌트를 사용해 하나의 카드 안에서 구성한다.

탭 후보:

- `Subscription`
- `Auto recharge`
- `Scheduled recharge`

노출 조건:

- `Subscription`: 활성 구독이 있을 때만 표시
- `Auto recharge`: 자동충전 프리셋이 있거나 active/pending 자동충전 정책이 있을 때 표시
- `Scheduled recharge`: 정기충전 프리셋이 있거나 active/pending 정기충전 정책이 있을 때 표시

초기 선택:

1. 현재 active/pending 항목이 있으면 그 항목을 우선 선택
2. 여러 항목이 이미 존재하는 예외 상태라면 `Subscription`, `Auto recharge`, `Scheduled recharge` 순서로 첫 항목 선택
3. 설정된 항목이 없으면 표시 가능한 첫 항목 선택

## 6. 구독 영역 변경

기존 `SubscriptionPlansCard`는 다음 역할을 함께 수행한다.

- 활성/과거 구독 표시
- billing preference 표시/수정
- 구독 플랜 구매 카드 표시
- 구독 구매 다이얼로그 열기

이번 지갑 화면에서는 활성 구독이 있을 때만 구독 상태를 보여준다.

변경 방향:

- 지갑 화면에서는 활성 구독이 없으면 구독 탭을 숨긴다.
- 지갑 화면의 구독 탭에서는 활성 구독 상태, 사용량, 만료/갱신 정보, 자동갱신 취소 등 현재 구독 관리에 필요한 정보만 보여준다.
- 신규 구독 플랜 구매 카드는 지갑 화면에서 숨긴다.
- 신규 구독 구매는 기존 별도 구독 화면에서 처리한다.

컴포넌트 설계:

- `SubscriptionPlansCard`에 표시 모드를 추가하거나, 활성 구독 요약 전용 컴포넌트를 분리한다.
- 추천은 전용 요약 컴포넌트 분리다. 기존 구매 카드와 지갑 화면 요약 카드의 책임을 나누면 상호 배타 UI를 붙이기 쉽다.

필요한 상태:

- `activeSubscriptions`
- `allSubscriptions`
- `billingPreference`
- `loading`
- `refresh`
- `cancelTossAutoRenew`

기존 API:

- `getSelfSubscriptionFull`
- `updateBillingPreference`
- `cancelTossAutoRenew`

## 7. 자동충전/정기충전 이동

기존 상위 탭:

- `Top up`
- `Scheduled recharge`
- `Auto recharge`

변경 후:

- 상위 탭 제거
- `RechargeFormCard`는 왼쪽 카드에 고정
- `AutoRechargeCard(mode='threshold')`는 오른쪽 결제 설정 카드의 `Auto recharge` 탭에 배치
- `AutoRechargeCard(mode='scheduled')`는 오른쪽 결제 설정 카드의 `Scheduled recharge` 탭에 배치

기존 `getVisibleAutoRechargeModes`는 계속 사용하되, 상위 탭 계산이 아니라 오른쪽 카드의 탭 후보 계산에 사용한다.

## 8. 상호 배타 규칙

FE에서만 다음 규칙을 적용한다.

상태 판정:

- 활성 구독 있음: `activeSubscriptions.length > 0`
- 자동충전 설정 있음: `walletAutoRecharge.policies` 중 `type='threshold'`이고 `status`가 `active` 또는 `pending`
- 정기충전 설정 있음: `walletAutoRecharge.policies` 중 `type='scheduled'`이고 `status`가 `active` 또는 `pending`

탭 선택 규칙:

- active/pending으로 이미 설정된 항목의 탭은 열 수 있다. 예외적으로 여러 항목이 이미 설정되어 있어도 각각 열어서 상태 확인과 취소를 할 수 있어야 한다.
- 설정되지 않은 항목의 탭은 다른 항목이 하나라도 설정되어 있으면 disabled 처리한다.
- disabled 탭에는 tooltip 또는 짧은 안내를 제공한다.

안내 문구:

- `Cancel the current payment setting before choosing another one.`

생성 버튼 규칙:

- `AutoRechargeCard`가 표시되어도 다른 항목이 설정되어 있으면 새 정책 선택 버튼은 disabled 처리한다.
- 현재 항목이 이미 설정되어 있으면 기존처럼 current policy와 cancel 버튼만 보여준다.
- 구독이 활성화된 상태에서는 자동충전/정기충전 생성 버튼을 누를 수 없다.
- 자동충전/정기충전이 active/pending이면 지갑 화면에서는 신규 구독 구매를 노출하지 않는다. 활성 구독이 이미 있는 예외 상태에서는 구독 탭을 열어 상태 확인과 해지를 할 수 있다.

백엔드와 실제 API는 그대로 두므로, 이 규칙은 UX 차단이다. 외부 API 호출이나 다른 화면에서의 예외 상태까지 막는 보안 정책은 아니다.

## 9. 데이터 흐름

개인 지갑 페이지에서 병렬로 로드한다.

- 사용자 지갑 정보: `getSelf`
- 일반 충전 정보: `useTopupInfo`
- 자동충전/정기충전 정책: `useWalletAutoRecharge('user', true)`
- 구독 상태: `getSelfSubscriptionFull`

계산 값:

- `visiblePaymentSettings`: 오른쪽 카드에 표시할 탭 목록
- `activePaymentSetting`: 현재 설정된 항목
- `disabledPaymentSettingReason`: 다른 항목이 이미 설정되어 있는지

페이지는 이 계산 값을 기반으로 레이아웃과 탭 disabled 상태를 결정한다.

## 10. 컴포넌트 경계

추천 분리:

- `WalletPaymentSettingsCard`
  - 오른쪽 카드와 탭 orchestration 담당
  - 구독/자동충전/정기충전 중 표시 가능한 탭 목록 계산
  - disabled 상태와 선택 상태 관리
- `SubscriptionStatusCard` 또는 `WalletSubscriptionStatus`
  - 활성 구독 상태 표시 전용
  - 구매 플랜 목록은 표시하지 않음
- `AutoRechargeCard`
  - 현재 정책 표시와 프리셋 선택 UI 유지
  - 외부에서 `creationDisabled`와 `creationDisabledMessageKey`를 받을 수 있게 확장

`Wallet` 페이지는 데이터 훅을 호출하고, 카드 컴포넌트에 필요한 props만 전달한다.

## 11. 에러 처리

- 구독 상태 로드 실패 시 구독 탭은 숨기고, 기존 화면 전체를 깨지 않게 한다.
- 자동충전 프리셋/정책 로드 전에는 기존처럼 로딩 중인 후보를 임시로 표시할 수 있다.
- 결제 설정 탭 후보가 로딩 완료 후 모두 사라지면 오른쪽 카드를 숨긴다.
- 사용자가 disabled 탭을 클릭하려 할 때는 탭 전환이 되지 않는다.

## 12. 테스트 계획

프론트엔드 단위 테스트:

- 활성 구독이 없으면 구독 탭을 만들지 않는다.
- 활성 구독이 있으면 구독 탭을 만들고 자동충전/정기충전 탭은 disabled 된다.
- active 자동충전 정책이 있으면 정기충전 탭과 구독 탭이 disabled 된다.
- active 정기충전 정책이 있으면 자동충전 탭과 구독 탭이 disabled 된다.
- 결제 설정 후보가 없으면 오른쪽 카드가 렌더링되지 않는다.
- `AutoRechargeCard`는 `creationDisabled`일 때 프리셋 선택 버튼을 disabled 처리한다.

기존 테스트 갱신:

- `auto-recharge-card.test.ts`에 생성 disabled 케이스 추가
- 새 helper를 만들 경우 별도 test 파일 추가

수동 확인:

- 활성 구독 없음 + 자동충전/정기충전 프리셋 있음
- 활성 구독 있음
- active 자동충전 있음
- active 정기충전 있음
- 오른쪽 후보 없음
- 모바일 폭에서 세로 배치 확인

## 13. 구현 순서

1. 결제 설정 상태 계산 helper와 테스트 추가
2. `AutoRechargeCard`에 생성 disabled props 추가
3. 활성 구독 상태 표시 전용 컴포넌트 작성 또는 기존 구독 컴포넌트 모드 분리
4. `Wallet` 페이지 레이아웃을 2컬럼으로 변경
5. 상위 탭 제거
6. i18n 키 추가/동기화
7. 테스트와 빌드 검증

## 14. 열린 결정

이번 설계에서 확정한 내용:

- 지갑 화면의 구독 구매 카드는 숨긴다.
- 활성 구독이 있을 때만 구독 탭을 표시한다.
- 개인 지갑 화면부터 적용한다.
- 백엔드는 변경하지 않는다.

향후 별도 검토 가능:

- 조직 지갑에도 동일한 오른쪽 결제 설정 카드 패턴을 적용할지
- 백엔드에서도 구독/자동충전/정기충전 상호 배타를 강제할지
- 지갑 화면에서 별도 구독 화면으로 이동하는 CTA를 추가할지
