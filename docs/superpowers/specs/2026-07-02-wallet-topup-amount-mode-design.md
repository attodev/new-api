# 지갑 충전 금액 기준 선택 설계

## 배경

현재 Toss 일반 충전 화면은 사용자가 보는 금액과 실제 결제/적립 흐름이 명확히 분리되어 있지 않다. 특히 Toss는 원화로 결제되지만 지갑 잔액은 내부 할당량 기준으로 증가한다. 사용자는 충전 화면에서 "얼마를 결제하는지"와 "결제 후 잔액이 얼마나 늘어나는지"를 동시에 이해할 수 있어야 한다.

이번 변경은 충전 금액 선택 영역에 두 가지 기준을 제공한다.

- 원 기준: 사용자가 실제 카드로 결제할 원화 금액을 먼저 고른다.
- 할당량 기준: 사용자가 충전될 지갑 잔액/할당량을 먼저 고른다.

두 기준 모두 화면 미리보기와 백엔드 최종 결제 계산이 같은 단가를 사용해야 한다.

## 목표

1. 충전 금액 영역에 `원 기준`과 `할당량 기준` 선택 UI를 추가한다.
2. `원 기준`에서는 1만원, 5만원, 10만원, 20만원, 50만원, 100만원 버튼을 제공한다.
3. `할당량 기준`에서는 기존 프리셋인 10, 20, 50, 100, 200, 500 버튼을 제공한다.
4. 원 기준 버튼과 사용자 지정 입력은 실제 결제 원화를 기준으로 동작하고, 보조 문구에는 실제 충전될 잔액/할당량을 표시한다.
5. 할당량 기준 버튼과 사용자 지정 입력은 실제 충전될 잔액/할당량을 기준으로 동작하고, 보조 문구에는 실제 요구되는 원화 결제액을 표시한다.
6. 프론트엔드가 즉시 미리보기를 계산할 수 있도록 Toss 충전 단가를 API로 제공한다.
7. 백엔드는 프론트엔드 미리보기와 같은 기준으로 결제 직전 금액을 다시 계산하고 검증한다.

## 비목표

- Toss 외 결제수단의 결제 단위 정책을 바꾸지 않는다.
- 자동 충전/정기 충전 프리셋 정책은 이번 범위에서 바꾸지 않는다.
- 할인 정책 자체를 새로 설계하지 않는다. 기존 그룹 충전 비율과 금액별 할인은 백엔드의 기존 계산 흐름을 따른다.
- Toss 결제 승인/웹훅 처리 흐름은 바꾸지 않는다.

## 사용자 경험

충전 카드의 금액 영역 상단에 세그먼트 컨트롤을 둔다.

- `원 기준`
- `할당량 기준`

`원 기준`이 선택되면 버튼은 다음처럼 보인다.

- `10,000원`
- `50,000원`
- `100,000원`
- `200,000원`
- `500,000원`
- `1,000,000원`

각 버튼 아래에는 Toss 단가로 환산한 충전 증가분을 표시한다.

- 예: `충전 +𝕮 7.69`

`할당량 기준`이 선택되면 버튼은 다음처럼 보인다.

- `10`
- `20`
- `50`
- `100`
- `200`
- `500`

각 버튼 아래에는 실제 결제될 원화 금액을 표시한다.

- 예: `결제 13,000원`

사용자 지정 입력도 선택된 기준에 맞춘다.

- 원 기준 입력값: 결제할 원화 금액
- 원 기준 우측 미리보기: 실제 충전될 잔액/할당량
- 할당량 기준 입력값: 충전할 잔액/할당량
- 할당량 기준 우측 미리보기: 실제 요구 원화 금액

결제 확인 모달도 같은 기준을 따라야 한다.

- 원 기준: `결제 금액`과 `충전 증가분`을 모두 보여준다.
- 할당량 기준: `충전 증가분`과 `결제 금액`을 모두 보여준다.

## API 설계

### topup 정보

`GET /api/user/topup/info` 응답에 다음 필드를 추가한다.

```json
{
  "toss_unit_price": 1300
}
```

조직 지갑도 같은 `topupInfo`를 사용하므로 동일 응답을 그대로 활용한다.

의미:

- `toss_unit_price`: 내부 1 할당량 기준 금액을 Toss 원화로 환산할 때 사용하는 단가

### Toss 금액 계산 요청

기존 `amount` 요청에 `amount_mode`를 추가한다.

```json
{
  "amount": 10,
  "amount_mode": "quota"
}
```

```json
{
  "amount": 10000,
  "amount_mode": "krw"
}
```

허용 값:

- `quota`: 입력 `amount`를 충전될 할당량 기준 금액으로 해석한다.
- `krw`: 입력 `amount`를 원화 결제 기준 금액으로 해석한다.

하위 호환성:

- `amount_mode`가 없으면 현재 Toss 동작과 같은 `krw`로 처리한다.

적용 엔드포인트:

- `POST /api/user/toss/amount`
- `POST /api/user/toss/pay`
- `POST /api/organization/toss/amount`
- `POST /api/organization/toss/pay`

### Toss 금액 계산 응답

`/amount` 응답은 기존 문자열 응답을 유지해도 되지만, 프론트가 정확한 두 값을 동시에 다루기 위해 구조화 응답을 추가로 지원한다.

```json
{
  "charge_amount": 13000,
  "credit_amount": 10,
  "credit_quota": 5000000,
  "unit_price": 1300
}
```

하위 호환성:

- 기존 프론트 코드가 `data`를 문자열로 받는 흐름은 깨지지 않게 한다.
- 새 프론트는 객체 응답을 우선 해석하고, 문자열 응답이면 결제 금액만 있는 기존 응답으로 처리한다.

## 계산 규칙

기본 값:

- `amount`: 사용자가 입력/선택한 숫자
- `unit`: `TossUnitPrice`
- `chargeKRW`: Toss에 실제 청구할 원화
- `creditAmount`: 지갑에 증가할 할당량 기준 금액
- `creditQuota`: 실제 DB quota 증가량
- `quotaPerUnit`: `QuotaPerUnit`
- `priceFactor`: 그룹 충전 비율과 금액별 할인까지 반영한 최종 결제 계수

`amount_mode = "krw"`:

```text
chargeKRW = amount
creditAmount = chargeKRW / (unit * priceFactor)
creditQuota = creditAmount * quotaPerUnit
```

`amount_mode = "quota"`:

```text
creditAmount = amount
chargeKRW = creditAmount * unit * priceFactor
creditQuota = creditAmount * quotaPerUnit
```

원 기준은 사용자가 고른 원화가 최종 카드 청구액이 되도록 한다. 할인이나 그룹 비율이 있으면 같은 원화 결제액으로 충전되는 할당량이 달라진다.

할당량 기준은 사용자가 고른 할당량이 최종 충전량이 되도록 한다. 할인이나 그룹 비율이 있으면 해당 할당량을 충전하기 위해 필요한 원화 결제액이 달라진다.

프론트 미리보기는 `toss_unit_price` 기준의 빠른 예상값을 즉시 보여주고, 결제 직전 `/amount` 또는 `/pay` 응답에서 백엔드 확정값으로 갱신한다. 그룹 비율처럼 프론트가 모르는 사용자별 값이 있을 수 있으므로, 실제 결제 금액과 실제 충전량의 최종 권위는 백엔드 응답이다.

## 프론트엔드 구조

### 타입

지갑 타입에 다음을 추가한다.

- `TopupInfo.toss_unit_price?: number`
- `TopupAmountMode = 'krw' | 'quota'`
- Toss 요청 타입의 `amount_mode?: TopupAmountMode`
- Toss 금액 계산 응답 타입의 구조화 데이터

### 상태

사용자 지갑과 조직 지갑에 `topupAmountMode` 상태를 둔다.

기본값:

- Toss 사용 가능 시 `krw`
- 그 외 결제수단만 있을 때는 기존 동작과 가까운 `quota`

결제수단이 Toss가 아닌 경우:

- 기준 선택 UI는 Toss 결제수단이 활성화되어 있을 때만 노출한다.
- Toss 외 결제수단을 선택하면 기존 할당량 기준 흐름을 유지한다.

### 표시 헬퍼

공통 헬퍼를 추가해 UI 계산을 분리한다.

- 원화 포맷: `10,000원`
- 할당량 증가분 포맷: 현재 지갑 잔액 표시 방식과 같은 `formatCurrencyFromUSD` 또는 quota 기반 표시
- 원 기준 미리보기: `amount / tossUnitPrice`
- 할당량 기준 미리보기: `amount * tossUnitPrice`

### RechargeFormCard

`RechargeFormCard`는 다음 props를 받는다.

- `amountMode`
- `onAmountModeChange`
- `tossUnitPrice`
- `tossKrwPresets`
- `quotaPresets`

기준별로 버튼 목록과 보조 문구를 바꾼다.

## 백엔드 구조

### TossPayRequest

```go
type TossPayRequest struct {
    Amount int64 `json:"amount"`
    AmountMode string `json:"amount_mode"`
    PaymentMethod string `json:"payment_method"`
}
```

### 계산 함수

Toss 충전 계산을 하나의 함수로 모은다.

입력:

- amount
- amountMode
- user group

출력:

- chargeKRW
- creditAmount
- creditQuota

`krw` 모드는 입력 원화를 최종 청구액으로 고정한 뒤 충전될 할당량을 역산한다. `quota` 모드는 입력 할당량을 최종 충전량으로 고정한 뒤 필요한 원화를 계산한다. 두 모드 모두 그룹 충전 비율과 금액별 할인은 `priceFactor`로 반영한다.

### TopUp 저장

기존 Toss 주문의 핵심 필드는 유지하되, `Money`의 의미를 명확히 한다.

- `TopUp.Amount = chargeKRW`
- `TopUp.Money = creditAmount`

따라서 승인 후 적립은 기존 `RechargeToss`의 `TopUp.Money * QuotaPerUnit` 계산을 그대로 사용한다. 이 구조에서는 실제 카드 청구액과 실제 충전량이 독립적으로 저장되므로, 원 기준과 할당량 기준 모두 화면 안내와 적립 결과를 맞출 수 있다.

## 테스트

### 백엔드

1. `amount_mode=krw`일 때 입력 13000원이 청구 13000원, 적립 10으로 계산되는지 검증한다.
2. `amount_mode=quota`일 때 입력 10이 청구 13000원, 적립 10으로 계산되는지 검증한다.
3. `amount_mode`가 없을 때 기존 호환을 위해 `krw`로 처리되는지 검증한다.
4. 조직 Toss amount/pay 엔드포인트도 동일 계산 함수를 타는지 검증한다.

### 프론트엔드

1. 원 기준 선택 시 원화 프리셋 버튼이 표시되는지 검증한다.
2. 원 기준 버튼 아래에 충전 증가분이 표시되는지 검증한다.
3. 할당량 기준 선택 시 기존 10/20/50/100/200/500 버튼이 표시되는지 검증한다.
4. 할당량 기준 버튼 아래에 실제 결제 원화가 표시되는지 검증한다.
5. 사용자 지정 입력 미리보기가 기준에 따라 반대로 표시되는지 검증한다.
6. Toss 요청에 `amount_mode`가 포함되는지 검증한다.

## 배포와 호환성

백엔드가 새 `amount_mode`를 지원해도 기존 클라이언트는 계속 동작한다. `amount_mode`를 보내지 않는 요청은 `krw`로 처리한다. 프론트는 `toss_unit_price`가 응답에 없으면 기본 표시를 보수적으로 처리하고, 실제 결제 금액은 기존 `/amount` 결과를 기준으로 표시한다.

## 완료 기준

- Toss 충전 화면에서 사용자가 `원 기준`과 `할당량 기준`을 선택할 수 있다.
- 각 기준의 버튼과 사용자 지정 입력이 서로 반대 값을 보조 문구로 보여준다.
- 결제 직전 Toss에 전달되는 원화 금액과 화면에 표시된 원화 금액이 일치한다.
- 결제 성공 후 증가하는 지갑 잔액이 화면에 안내된 충전 증가분과 같은 계산 체계를 따른다.
- 사용자 지갑과 조직 지갑에서 같은 단위 정책을 사용한다.
