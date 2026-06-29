# Toss 결제 연동 — 설계 문서

**작성일:** 2026-06-29
**상태:** 설계 승인 완료, 구현 계획 대기
**범위:** Toss Payments(토스페이먼츠)를 결제 provider로 추가. 일회성 충전(top-up)과 빌링키 기반 자동 정기결제(구독) 모두 지원.

---

## 1. 배경 및 목표

new-api는 이미 여러 결제 provider(Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake)를 일관된 레이어드 아키텍처로 지원한다:

```
Router → Controller (topup_<provider>.go / subscription_payment_<provider>.go)
       → Service / Model (TopUp, SubscriptionOrder, 크레딧 적립)
       → Setting (payment_<provider>.go + model/option.go 영속화)
       → Frontend (web/default/src/features/wallet)
```

여기에 **Toss Payments**를 추가한다. 가장 유사한 기존 패턴(충전은 **PayPal** — 리다이렉트 + 서버사이드 confirm + 웹훅, 구독 주문 구조는 **Stripe**)을 따르되, Toss 고유의 2가지 차이를 반영한다:

1. **프론트 SDK가 결제창을 직접 연다.** 다른 provider는 리다이렉트 URL을 반환받아 브라우저가 이동한다. Toss는 Toss JS SDK(`@tosspayments/tosspayments-sdk`)로 `requestPayment()` / `requestBillingAuth()`를 클라이언트에서 호출해야 한다.
2. **빌링(구독)은 서버 주도 정기결제다.** Stripe는 Stripe Checkout 구독 모드가 자동 재청구를 대신한다. Toss는 **빌링키(billing key)**를 발급하고, 우리 서버가 이를 저장한 뒤 주기적으로 청구한다. 이를 위해 현재 존재하지 않는 **신규 정기청구 cron 서브시스템**이 필요하다(현재 `service/subscription_reset_task.go`는 만료/리셋만 하고 재청구는 하지 않음).

### 브레인스토밍에서 확정된 결정 사항

- **프론트엔드:** `web/default`(모던 React 19 / Rsbuild). 백엔드는 공통.
- **범위:** 충전 **및** 구독 — 구독은 **빌링키 기반 진짜 자동 정기결제**.
- **통화:** 원화 직접 — 유저가 입력한 충전 금액을 원화(KRW)로 다루고 Toss `amount`에 그대로 전달. 단일 `TossUnitPrice` 상수로 원화를 내부 크레딧으로 환산.
- **테스트 모드:** `TossTestMode` 토글로 test/live 클라이언트·시크릿 키 분리 지원.
- **단계 구분:** Phase 1(충전)을 토대로 먼저 출시하고, Phase 2(빌링 구독)가 그 위에 쌓인다. 각 단계는 독립적으로 동작하고 검증 가능하다.

---

## 2. Toss API 레퍼런스 (본 설계에서 사용하는 범위)

### 일회성 결제
- 프론트: `loadTossPayments(clientKey)` → `payment.requestPayment({ method, amount: { currency: 'KRW', value }, orderId, orderName, successUrl, failUrl })`.
- 성공 시 브라우저가 `successUrl?paymentKey=...&orderId=...&amount=...`로 리다이렉트된다.
- 서버 승인: `POST https://api.tosspayments.com/v1/payments/confirm`
  - 인증: `Authorization: Basic base64(secretKey + ":")`
  - 바디: `{ "paymentKey", "orderId", "amount" }`
  - 응답 Payment 객체: `status`(성공 시 `DONE`), `totalAmount`, `method`, `approvedAt`, `orderId`, `paymentKey`.

### 빌링 (정기결제)
- 프론트: `payment.requestBillingAuth({ method: 'CARD', customerKey, successUrl, failUrl })`.
- 성공 시: `successUrl?authKey=...&customerKey=...`.
- 빌링키 발급: `POST https://api.tosspayments.com/v1/billing/authorizations/issue`
  - 인증: Basic(시크릿 키). 바디: `{ "authKey", "customerKey" }`. 응답에 `billingKey`(+ 카드 메타데이터) 포함.
- 빌링키로 청구: `POST https://api.tosspayments.com/v1/billing/{billingKey}`
  - 인증: Basic(시크릿 키). 바디: `{ "customerKey", "amount", "orderId", "orderName" }`. 응답은 Payment 객체(`status` `DONE`, `card` 등).

> API base host는 모두 `https://api.tosspayments.com`. Toss는 별도 샌드박스 host를 쓰지 않으며, test vs live는 오직 사용하는 **키 쌍**으로 구분된다. `TossTestMode`가 키 쌍을 선택한다.

---

## 3. 공통 토대 (Phase 1에서 구축)

### 3.1 설정 — `setting/payment_toss.go` (신규)

```go
package setting

var TossEnabled = false
var TossTestMode = false
var TossClientKey = ""      // live
var TossSecretKey = ""      // live
var TossTestClientKey = ""  // test
var TossTestSecretKey = ""  // test
var TossUnitPrice = 1300.0  // 내부 1 unit(USD 환산) 당 KRW
var TossMinTopUp = 1000     // 최소 충전 금액 (KRW)

func TossActiveClientKey() string { if TossTestMode { return TossTestClientKey }; return TossClientKey }
func TossActiveSecretKey() string { if TossTestMode { return TossTestSecretKey }; return TossSecretKey }
```

### 3.2 영속화 — `model/option.go`

PayPal 패턴을 양방향으로 그대로 따른다:
- 옵션맵 writer에: `common.OptionMap["TossClientKey"] = setting.TossClientKey` 등. (시크릿 키도 영속화를 위해 포함하되, 브라우저로는 절대 반환하지 않음 — §6 참조.)
- `SetOption`/로드 switch에: `case "TossClientKey": setting.TossClientKey = value`, `case "TossTestMode": setting.TossTestMode = value == "true"`, `case "TossUnitPrice": setting.TossUnitPrice, _ = strconv.ParseFloat(value, 64)` 등.

### 3.3 모델 상수 — `model/topup.go`

```go
const PaymentMethodToss   = "toss"
const PaymentProviderToss = "toss"
```

### 3.4 원화 → 크레딧 환산 (확정)

- 유저는 **원화 금액**(`amountKRW`)을 입력한다. 프론트는 이를 표시하고 Toss `amount`로 전달한다.
- 서버에서 최소 금액 강제: `amountKRW >= TossMinTopUp`.
- 성공 시 적립 크레딧:
  `quota = round( (amountKRW / TossUnitPrice) * QuotaPerUnit )`
  정밀도를 위해 `shopspring/decimal` 사용(`Recharge`가 쓰는 것과 동일 라이브러리).
- `TopUp.Amount`는 `amountKRW`(청구된 원화)를 저장하고, `TopUp.Money`는 다른 provider와의 리포트 일관성을 위해 USD 환산값 `amountKRW / TossUnitPrice`를 저장한다.
- `topupGroupRatio`(그룹 할인)와 `AmountDiscount`는 **청구 원화**에 적용한다(`getPayPalPayMoney`와 동일 의미). Toss 전용 `getTossPayMoney(amountKRW, group)`이 최종 청구 원화를 반환한다.

> 이것이 유일한 비즈니스 핵심 상수다. `TossUnitPrice`는 설정에서 관리자가 변경 가능하다.

---

## 4. Phase 1 — 일회성 충전 (Top-up)

### 4.1 백엔드 — `controller/topup_toss.go` (신규)

| 핸들러 | 라우트 | 인증 | 역할 |
|---|---|---|---|
| `RequestTossPay` | `POST /api/user/toss/pay` | user | 검증(provider, 최소 금액, 리다이렉트 URL 신뢰). `orderId`(= `TopUp.TradeNo`, 접두사 `toss_` + sha1) 생성. `getTossPayMoney`로 청구 원화 계산. 대기상태 `TopUp` 생성. `{ clientKey, orderId, orderName, amount, successUrl, failUrl }` 반환. |
| `RequestTossAmount` | `POST /api/user/toss/amount` | user | 금액 미리보기(청구 원화 반환). |
| `TossConfirm` | `GET /api/toss/confirm` | public | successUrl 핸들러. `paymentKey`,`orderId`,`amount` 수신. `LockOrder`. `orderId`로 `TopUp` 조회; provider 일치 + 상태 pending + `amount == 저장된 청구 원화` 검증. confirm API 호출. `status==DONE` 시 → `RechargeToss`(멱등) → `/console/log`로 리다이렉트. 불일치/실패 시 → 로그, `/console/topup`으로 리다이렉트. |
| `TossFail` | `GET /api/toss/fail` | public | failUrl 핸들러. 주문 실패 표시(best-effort), `/console/topup`으로 리다이렉트. |
| `TossWebhook` | `POST /api/toss/webhook` | public | 가상계좌 등 비동기 결제(`DONE`) 처리. 이벤트 검증, 주문 조회, 동일 `RechargeToss`로 멱등 적립. |

검증 헬퍼는 `validatePayPalTopUpOrder`를 따른다(provider 일치, order id 일치, 금액 일치). 모든 적립은 `LockOrder`/`UnlockOrder` + DB `FOR UPDATE`를 거친다.

### 4.2 모델 — `model/topup.go`

`RechargeToss(referenceId, callerIp)` — `RechargePayPal`을 그대로 따름:
- 트랜잭션 + trade_no `FOR UPDATE`.
- 가드: provider == toss, 상태 == pending.
- 상태 success + 완료 시각 설정.
- `quota = decimal(amountKRW / TossUnitPrice).Mul(QuotaPerUnit)`.
- `CreditTopUpTarget(tx, topUp, quota)`(유저 + 조직 타겟 지원).
- `RecordTopupLog`.

### 4.3 라우트 — `router/api-router.go`

```
# public (인증 없음)
GET  /api/toss/confirm   → TossConfirm
GET  /api/toss/fail      → TossFail
POST /api/toss/webhook   → TossWebhook

# user (인증)
POST /api/user/toss/pay     → RequestTossPay
POST /api/user/toss/amount  → RequestTossAmount

# organization 미러 (인증)
POST /api/organization/toss/pay → RequestOrganizationTossPay
```

`successUrl = system_setting.ServerAddress + "/api/toss/confirm"`,
`failUrl = system_setting.ServerAddress + "/api/toss/fail"`.

### 4.4 프론트엔드 — `web/default`

- Bun으로 `@tosspayments/tosspayments-sdk` 의존성 추가.
- `features/wallet/api.ts`: `requestTossPayment`, `calculateTossAmount`.
- `features/wallet/hooks/use-toss-payment.ts`(신규): `/api/user/toss/pay` 호출 → `loadTossPayments(clientKey)` → `payment.requestPayment({ method:'CARD', amount:{currency:'KRW', value}, orderId, orderName, successUrl, failUrl })`. SDK가 confirm 라우트로 리다이렉트.
- `features/wallet/components/recharge-form-card.tsx` + `hooks/use-topup-info.ts` + `lib/payment.ts`: `TossEnabled`일 때 Toss를 결제수단으로 노출.
- 기존 결제 설정 패턴을 따르는 `web/default` 신규 설정 UI(키 입력, test 토글, unit price, 최소 충전).
- 백엔드 `GetTopUpInfo`를 확장해 `toss_enabled`(및 비밀이 아닌 표시용 config)를 프론트에 노출.

---

## 5. Phase 2 — 빌링키 기반 자동 정기결제 (구독)

### 5.1 빌링키 발급 흐름

1. 프론트 `payment.requestBillingAuth({ method:'CARD', customerKey, successUrl, failUrl })`.
2. 브라우저가 `/api/toss/billing/confirm?authKey=...&customerKey=...`로 리다이렉트.
3. 서버 `POST /v1/billing/authorizations/issue` `{authKey, customerKey}` → `billingKey`(+ 카드 메타데이터).
4. 빌링키를 **암호화 저장**한 뒤, 즉시 첫 기간을 청구하고 구독을 활성화.

`customerKey`는 유저별 고정 식별자(예: `cust_<userId>_<random>`)로, 한 번 생성해 재사용한다.

### 5.2 신규 모델 — `model/toss_billing.go`

`UserBillingKey`:
- `Id`, `UserId`(인덱스), `CustomerKey`(인덱스), `BillingKey`(암호화 blob), `CardCompany`, `CardNumberMasked`, `Status`(`active`/`revoked`), `CreateTime`.

`UserSubscription` 필드 추가(마이그레이션, SQLite 안전 `ADD COLUMN`):
- `AutoRenew bool`, `NextBillingTime int64`(인덱스), `BillingKeyId int`.

### 5.3 구독 컨트롤러 — `controller/subscription_payment_toss.go` (신규)

| 핸들러 | 라우트 | 역할 |
|---|---|---|
| `SubscriptionRequestTossBilling` | `POST /api/subscription/toss/pay` | 플랜 + 컴플라이언스 검증. 대기상태 `SubscriptionOrder` 생성. `requestBillingAuth`용 `{ clientKey, customerKey, successUrl, failUrl }` 반환. |
| `TossBillingConfirm` | `GET /api/toss/billing/confirm` | 빌링키 발급·저장, `POST /v1/billing/{billingKey}`로 첫 기간 청구, `CompleteSubscriptionOrder`, `AutoRenew=true`·`NextBillingTime=기간 종료 시각` 설정. `/console/topup`으로 리다이렉트. |
| `TossBillingFail` | `GET /api/toss/billing/fail` | 주문 실패 표시, 리다이렉트. |
| (관리) | `POST /api/user/toss/billing/cancel` | 자동갱신/빌링키 해지. |

### 5.4 정기청구 cron — `service/toss_billing_task.go` (신규)

`service/subscription_reset_task.go`를 본떠서:
- `AutoRenew = true` AND `NextBillingTime <= now`인 `UserSubscription`을 배치로 주기 조회.
- 각 건: 새 멱등 `orderId`로 `POST /v1/billing/{billingKey}` `{customerKey, amount, orderId, orderName}` 청구.
- 성공 시: 구독 기간 연장, `NextBillingTime` 진행, `SubscriptionOrder` + 로그 기록.
- 실패 시: 제한된 재시도; N회 실패 후 자동갱신 정지 및 기존 `ExpireDueSubscriptions` 의미대로 다운그레이드.
- 전부 주문 락 하에서, `orderId` 기준 멱등.

### 5.5 프론트엔드 (Phase 2)

- `features/wallet/components/subscription-plans-card.tsx`: `requestBillingAuth`를 호출하는 Toss 구독 버튼.
- 빌링키/카드 관리 UI(등록 카드 조회, 자동갱신 해지).

---

## 6. 에러 처리 · 보안 · 멱등성

- **멱등성:** `LockOrder`/`UnlockOrder`(refcount 뮤텍스 맵) + DB row lock(`FOR UPDATE`) 재사용. confirm·웹훅 경로 모두 이미 `success`인 주문이면 no-op.
- **금액 위변조 방지:** `TossConfirm`은 confirm API 호출 전에 `amount` 쿼리 파라미터가 해당 주문의 저장된 청구 원화와 일치할 때만 진행.
- **시크릿 키:** 시크릿 키는 서버 전용. `GetTopUpInfo` 및 브라우저로 반환되는 모든 설정 읽기는 **오직** 활성 클라이언트 키와 비밀이 아닌 표시용 config만 노출; 시크릿 키는 절대 API 응답으로 직렬화하지 않음.
- **빌링키 저장:** `common/`의 기존 암호화 헬퍼로 암호화 저장.
- **test/live 격리:** 모든 Toss API 호출과 프론트에 전달되는 클라이언트 키는 `TossActiveClientKey()` / `TossActiveSecretKey()`를 거치며 `TossTestMode`로 분기.
- **DB 호환성:** 신규 테이블/컬럼은 GORM 추상화와 SQLite 안전 `ADD COLUMN` 마이그레이션 사용; JSON은 `TEXT`로 저장; 예약어 컬럼은 기존 `commonXxxCol` 헬퍼 사용(Rule 2).
- **JSON:** 모든 marshal/unmarshal은 `common.*` 래퍼 경유(Rule 1).
- **요청 DTO:** 0 값이 그대로 전달되어야 하는 옵션 스칼라는 포인터 + `omitempty`(Rule 6).

## 7. 테스트

- Go 단위 테스트:
  - `getTossPayMoney` 원화 환산(그룹 비율 + 금액 할인 포함).
  - `TossConfirm` 검증(금액 불일치, provider 불일치, 상태 가드).
  - `RechargeToss` 멱등성(이중 confirm + 웹훅 경합).
  - Phase 2: 빌링키 청구 성공/실패 처리, `NextBillingTime` 진행.
- 기존 `setting/operation_setting/payment_setting_test.go` 스타일 준수.
- 프론트엔드: Toss 경로가 추가되는 wallet 컴포넌트 테스트 확장.

## 8. 범위 외 (v1)

- CARD 외 빌링 수단(계좌 기반 자동결제).
- 부분 환불 / 취소 API(주문 실패 표시 이상).
- Toss 다중 통화(KRW 전용).

## 9. 구현 순서

1. **Phase 1** — 설정 + 영속화 + 모델 상수 + `topup_toss.go` + `RechargeToss` + 라우트 + 프론트 충전 + 테스트. 단독 출시 가능.
2. **Phase 2** — `UserBillingKey` 모델 + `UserSubscription` 마이그레이션 + `subscription_payment_toss.go` + `toss_billing_task.go` cron + 프론트 구독 + 테스트.
