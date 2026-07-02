# 자동 충전 기준 할당량 전환 설계

## 배경

현재 자동 충전 UI는 `threshold_amount`를 기준 잔액처럼 보여주고, 충전 금액을 먼저 선택한 뒤 기준 잔액을 고르는 구조다. 사용자가 보기에는 자동 충전의 조건이 먼저 보여야 더 자연스럽다. 또한 충전 금액은 Toss 결제 금액이므로 원화 기준이 맞지만, 자동 충전의 기준은 new-api 내부에서 실제로 차감/보유하는 할당량(quota)과 비교되어야 한다.

이번 변경은 자동 충전(`threshold`)에만 적용한다. 정기 충전(`scheduled`)은 기존처럼 기간을 먼저 고르고 충전 금액을 버튼으로 고르는 흐름을 유지한다.

## 결정 사항

기준 잔액 필드는 방식 B를 따른다.

- `threshold_quota`를 자동 충전 기준의 공식 DB/API/FE 필드로 사용한다.
- 기존 `threshold_amount`는 자동 충전 기준값으로 더 이상 사용하지 않는다.
- 기존 데이터 마이그레이션은 하지 않는다. 필요하면 관리자가 기존 프리셋/정책을 삭제하고 다시 만든다.
- Toss 결제 충전 금액 `amount`는 계속 원화(KRW) 기준으로 유지한다.
- 자동 충전 트리거 비교는 대상 지갑의 `quota`와 `threshold_quota`를 직접 비교한다.

## 사용자 UI

자동 충전 설정 화면은 기준 할당량을 먼저 선택하고, 그 다음 충전 금액을 선택한다.

문장형 안내를 사용한다.

```text
남은 할당량이 [기준 할당량 드롭다운] 보다 적어지면 다음 금액을 충전합니다.
```

- `[기준 할당량 드롭다운]`은 관리자가 만든 `threshold_quota` 프리셋 목록에서 선택한다.
- 드롭다운 라벨은 기존 quota 표시 유틸을 사용해 `$1`, `$5` 또는 시스템 표시 설정에 맞는 값으로 보여준다.
- 충전 금액은 지금처럼 버튼으로 선택한다.
- 충전 금액 버튼은 원화 기준임을 알 수 있게 `1000원`, `3000원`처럼 표시한다.
- 하단 버튼 `카드 등록하고 자동충전 설정`을 누를 때만 Toss 카드 등록/자동 충전 설정 API를 호출한다.
- 기준 할당량 또는 충전 금액 중 하나라도 선택되지 않았으면 하단 버튼은 비활성화한다.

정기 충전 화면은 이번 변경 범위에 포함하지 않는다.

## 관리자 UI

자동 충전 프리셋 관리 화면은 기준값을 `threshold_amount`가 아니라 `threshold_quota`로 저장/전송한다.

- 자동 충전 설정 영역의 기준값 명칭은 “기준 할당량”으로 바꾼다.
- 기준 할당량은 여러 개를 등록할 수 있고, 사용자 UI에서는 드롭다운 옵션으로 나타난다.
- 충전 금액은 여러 개를 등록할 수 있고, 사용자 UI에서는 버튼 옵션으로 나타난다.
- 저장 시 자동 충전 프리셋은 `threshold_quota × amount` 조합으로 생성한다.
- `threshold_amount`는 새 요청에서 0 또는 미사용 값으로 둔다.

## API 및 모델

### 프리셋

`WalletAutoRechargePreset`와 `WalletAutoRechargePresetRequest`에 `threshold_quota`를 공식 필드로 둔다.

- JSON: `threshold_quota`
- 타입: 정수 quota 값
- 검증: 자동 충전 프리셋에서는 0 이상이어야 한다.
- `threshold_amount`는 호환을 위해 남길 수 있지만, 새 자동 충전 로직에서는 사용하지 않는다.

### 정책

`WalletAutoRecharge` 생성 시 자동 충전 정책은 프리셋의 `threshold_quota`를 그대로 저장한다.

- `CreateWalletAutoRechargeRequest`는 `ThresholdQuota`를 받는다.
- `CreatePendingWalletAutoRecharge`는 `ThresholdQuota`를 그대로 `WalletAutoRecharge.ThresholdQuota`에 저장한다.
- `ThresholdAmount`에서 quota를 환산하는 기존 흐름은 자동 충전 기준으로 사용하지 않는다.

### 트리거 비교

자동 충전 처리 시 현재 대상의 `quota`와 정책의 `ThresholdQuota`를 비교한다.

```text
current quota <= threshold_quota 이면 자동 충전 대상
```

기존 비교 방식 자체는 유지하되, `ThresholdQuota` 값의 출처를 `threshold_amount` 환산값이 아니라 DB/API의 `threshold_quota`로 바꾼다.

## 데이터 호환성

기존 데이터 마이그레이션은 하지 않는다.

- 기존 자동 충전 프리셋/정책은 삭제 후 재생성하는 운영 방식을 전제로 한다.
- 새 코드에서 기존 프리셋에 `threshold_quota`가 없으면 0으로 취급된다.
- 이 경우 자동 충전 조건이 너무 낮거나 의도와 다를 수 있으므로, 관리자 화면에서 새 프리셋을 다시 저장하도록 안내하는 것이 좋다.

## 테스트 계획

백엔드:

- 자동 충전 프리셋 생성/수정 요청이 `threshold_quota`를 저장하는지 테스트한다.
- 프리셋 기반 정책 생성 시 `ThresholdQuota`가 그대로 정책에 저장되는지 테스트한다.
- 자동 충전 트리거가 `ThresholdQuota`와 지갑 `quota`를 직접 비교하는지 테스트한다.

프론트엔드:

- 자동 충전 옵션 helper가 `threshold_quota` 기준으로 그룹을 만든다는 테스트를 추가/수정한다.
- 사용자 자동 충전 카드가 기준 할당량 문장과 드롭다운을 먼저 보여주는지 테스트한다.
- 충전 금액은 기준 할당량 선택 이후 버튼으로 고르고, 하단 버튼을 눌러야 설정 흐름이 시작되는지 테스트한다.
- 관리자 프리셋 UI가 `threshold_quota`를 저장 요청에 포함하는지 테스트한다.

## 제외 범위

- 기존 `threshold_amount` 데이터의 자동 변환 마이그레이션
- Toss 결제 금액 `amount`의 단위 변경
- 정기 충전 UI 구조 변경
- 백엔드에서 구독/자동 충전/정기 충전 상호배제를 강제하는 변경
