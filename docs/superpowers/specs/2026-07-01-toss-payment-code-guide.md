# Toss Payments 코드 레벨 개발자 가이드

작성일: 2026-07-01

## 문서 목적

이 문서는 new-api의 Toss Payments 연동 코드를 나중에 다시 확인하거나 수정할 때 참고할 수 있는 코드 레벨 가이드다. 공식 Toss Payments 개발 문서의 어느 단계 때문에 각 코드가 존재하는지, 어떤 값이 어떤 책임을 가지는지, 변경 시 어떤 부분을 조심해야 하는지 정리한다.

이 문서는 구현 변경 이력 요약이 아니라, 코드를 읽고 유지보수하는 개발자를 위한 지도에 가깝다. 변경 이력 중심 문서는 `docs/superpowers/specs/2026-06-30-toss-payment-change-summary.md`를 함께 참고한다.

## 공식 문서 링크 맵

| 구현 영역 | 공식 문서 | 이 코드에서 대응되는 부분 |
| --- | --- | --- |
| 전체 시작점 및 결제 제품 선택 | https://docs.tosspayments.com/guides/v2/get-started | 일반 결제와 자동결제 기능을 분리한다. |
| 결제창 연동 | https://docs.tosspayments.com/guides/v2/payment-window/integration | `requestPayment()`를 호출하는 프론트 훅과 서버 confirm 흐름 |
| 결제 승인 API | https://docs.tosspayments.com/reference#결제-승인 | `POST /v1/payments/confirm` 호출 |
| paymentKey 결제 조회 | https://docs.tosspayments.com/reference#paymentkey로-결제-조회 | confirm 실패, webhook, pending reconcile에서 authoritative fetch |
| orderId 결제 조회 | https://docs.tosspayments.com/reference#orderid로-결제-조회 | Toss billing charge 응답 유실 시 orderId로 재조회 |
| 자동결제 결제창 연동 | https://docs.tosspayments.com/guides/v2/billing/integration | `requestBillingAuth()`, billing key 발급, 첫 결제, 갱신 |
| 자동결제 승인 API | https://docs.tosspayments.com/reference#자동결제-승인 | `POST /v1/billing/{billingKey}` 호출 |
| 빌링키 삭제 API | https://docs.tosspayments.com/reference#빌링키-삭제 | 자동결제 해지, 원격 billing key 삭제, pending revocation |
| 웹훅 | https://docs.tosspayments.com/guides/v2/webhook | `PAYMENT_STATUS_CHANGED`, `BILLING_DELETED` 처리 |

## 핵심 불변식

Toss 연동 코드를 수정할 때는 아래 원칙을 먼저 확인한다.

1. 브라우저 callback과 webhook body는 최종 진실이 아니다.
2. 사용자 quota 지급과 구독 활성화는 Toss API 응답을 서버에서 검증한 뒤에만 한다.
3. 일반 결제와 자동결제는 키, 계약, MID, 흐름이 다를 수 있으므로 분리해서 다룬다.
4. `paymentKey`는 결제 조회, 승인, 취소, 대사에 필요한 값이므로 반드시 저장한다.
5. `customerKey`는 user id, 이메일, 전화번호처럼 유추 가능한 값이면 안 된다.
6. Toss 금액은 KRW 정수이고, 내부 지급 단위는 money/quota다. 둘을 섞지 않는다.
7. 카드 결제 전용 가정이 있는 코드가 많다. 가상계좌, 계좌이체, 간편결제를 추가할 때 그대로 재사용하면 안 된다.

## 라우팅 맵

파일: `router/api-router.go`

- `GET /api/toss/confirm`: 일반 결제 success URL. `controller.TossConfirm`
- `GET /api/toss/fail`: 일반 결제 fail URL. `controller.TossFail`
- `POST /api/toss/webhook`: Toss webhook. `controller.TossWebhook`
- `POST /api/user/toss/pay`: 개인 지갑 Toss 결제 세션 생성. `controller.RequestTossPay`
- `POST /api/user/toss/amount`: 개인 지갑 Toss 청구 금액 계산. `controller.RequestTossAmount`
- `POST /api/organization/toss/pay`: 조직 지갑 Toss 결제 세션 생성. `controller.RequestOrganizationTossPay`
- `POST /api/organization/toss/amount`: 조직 지갑 Toss 청구 금액 계산. `controller.RequestOrganizationTossAmount`
- `POST /api/subscription/toss/pay`: 정기결제 billing auth 시작. `controller.SubscriptionRequestTossBilling`
- `POST /api/subscription/toss/cancel`: Toss 자동갱신 해지. `controller.CancelTossAutoRenew`
- `GET /api/subscription/toss/confirm/:trade_no`: billing auth success URL. `controller.SubscriptionTossBillingConfirm`
- `GET /api/subscription/toss/fail/:trade_no`: billing auth fail URL. `controller.SubscriptionTossBillingFail`

주의:

- Toss callback URL은 인증 middleware를 타지 않는 공개 endpoint다. 따라서 callback으로 받은 값을 그대로 믿으면 안 된다.
- `trade_no`를 path param으로 받는 subscription callback은 query fallback도 지원한다. 기존 callback URL 호환성을 유지하려는 목적이다.

## 설정과 활성화 조건

### `setting/payment_toss.go`

역할:

- Toss 일반 결제 key pair와 자동결제 key pair를 분리해서 보관한다.
- test/live 환경은 별도 host가 아니라 key pair 선택으로 구분한다.
- `TossUnitPrice`로 내부 unit을 KRW 청구 금액으로 변환한다.
- `TossCardMinimumAmountKRW`는 카드 결제 최소 금액 방어값이다.
- `TossOrderNameMaxRunes`는 Toss `orderName` 최대 길이 제한을 맞추기 위한 상한이다.

공식 문서 근거:

- 결제창 문서는 일반결제로 계약된 MID의 client key와 secret key를 사용하라고 설명한다.
- 자동결제 문서는 자동결제 계약된 MID의 client key와 secret key를 사용하라고 설명한다.
- 자동결제는 추가 리스크 검토 및 계약 후 사용할 수 있다고 명시되어 있다.

주의:

- `TossActiveBillingKeyPair()`는 구버전 저장 주문과 API 호출 호환을 위해 일반 Toss key fallback을 가진다.
- `TossExplicitActiveBillingKeyPair()`는 활성화 체크 전용이다. 자동결제가 명시적으로 설정된 billing key 없이 켜지지 않게 하는 방어선이다.
- 자동결제 설정 UI나 API에서 일반 결제 key만 보고 billing을 enabled로 판단하면 안 된다.

### `controller/payment_webhook_availability.go`

역할:

- `isTossTopUpEnabled()`: 결제 compliance, Toss enabled, unit price, 일반 key pair, callback URL을 모두 확인한다.
- `isTossBillingEnabled()`: 자동결제 compliance, TossBilling enabled, unit price, 명시적 billing key pair, callback URL을 확인한다.

주의:

- `ServerAddress`가 올바르지 않으면 success URL과 fail URL이 잘못 만들어진다.
- 운영 환경에서는 HTTPS callback이 필요하다. 로컬 개발에서는 `localhost`, `127.0.0.1`, `::1`에 한해 HTTP를 허용한다.

## Toss API HTTP 클라이언트

파일: `controller/topup_toss.go`

핵심 함수:

- `doTossAPIRequestWithSecret()`
- `confirmTossPaymentWithSecret()`
- `getTossPaymentWithSecret()`
- `getTossBillingPayment()`

공식 문서 근거:

- 결제 승인과 빌링키 발급 문서는 secret key 뒤에 `:`을 붙여 base64 인코딩한 Basic 인증 헤더를 사용하라고 설명한다.
- secret key는 클라이언트, GitHub 등 외부에 노출되면 안 된다고 설명한다.
- 결제 승인 API는 `paymentKey`, `orderId`, `amount`를 body로 받는다.
- paymentKey 조회 API는 `GET /v1/payments/{paymentKey}`를 사용한다.

코드에서 하는 일:

- `Authorization: Basic base64(secretKey + ":")`를 만든다.
- body가 있는 요청에는 `Content-Type: application/json`을 넣는다.
- idempotency key가 전달되면 `Idempotency-Key` header를 넣는다.
- timeout, 409, 429, 5xx는 transient로 보고 retry한다.
- JSON marshal/unmarshal은 프로젝트 규칙에 맞게 `common.Marshal`, `common.Unmarshal`을 사용한다.

주의:

- secret key는 프론트에 내려보내면 안 된다. 프론트에는 client key만 전달한다.
- stored order에는 `ProviderCredential`로 당시 secret key snapshot을 암호화 저장한다. 운영 중 key rotation이 있어도 과거 주문 reconcile이 가능해야 하기 때문이다.
- Toss 응답 실패 body를 그대로 사용자에게 노출하지 않는다. 로그에는 남기되 사용자 응답은 일반화한다.

## 일반 Toss 충전 흐름

### 1. 금액 계산

파일: `controller/topup_toss.go`

핵심 함수:

- `RequestTossAmount()`
- `getTossPayMoney()`
- `tossUSDEquivalent()`

공식 문서 근거:

- Toss 결제창과 승인 API에는 KRW 정수 금액을 넘긴다.
- success URL의 `amount`와 결제 요청 시 보낸 amount가 같은지 반드시 확인해야 한다.

코드에서 하는 일:

- 사용자가 입력한 내부 unit을 `TossUnitPrice`로 KRW 청구 금액으로 바꾼다.
- group top-up ratio와 amount discount를 반영한다.
- quota display가 token 모드면 token 입력을 unit으로 환산한다.
- `Money`에는 내부 지급 기준이 되는 USD-equivalent 값을 저장한다.

주의:

- `TopUp.Amount`는 Toss에 청구한 KRW다.
- `TopUp.Money`는 내부 quota 적립 계산용이다.
- 수동 완료, webhook, reconcile에서 `Amount`를 quota 계산에 쓰면 과지급이 생긴다.

### 2. 서버 pending 주문 생성

파일: `controller/topup_toss.go`

핵심 함수:

- `RequestTossPay()`
- `RequestOrganizationTossPay()`

공식 문서 근거:

- 결제창 문서는 client key로 SDK를 초기화하고, `payment()`와 `requestPayment()`로 결제를 요청하라고 설명한다.
- 결제 요청에는 주문번호, 결제금액, success URL, fail URL이 필요하다.
- orderId는 각 주문을 식별하는 값이고 저장해야 한다.

코드에서 하는 일:

- 결제 수단이 Toss인지 확인한다.
- Toss top-up이 활성화되어 있는지 확인한다.
- callback URL에 쓸 `ServerAddress`가 유효한지 확인한다.
- 최소 충전 금액과 카드 최소 결제 금액을 확인한다.
- `model.GetOrCreateTossCustomerKey()`로 안정적인 customerKey를 준비한다.
- `toss_` prefix를 붙인 고유 orderId를 만든다.
- active secret key를 암호화해서 `ProviderCredential`에 저장한다.
- pending `TopUp`을 생성한다.
- 프론트에 `client_key`, `customer_key`, `order_id`, `order_name`, `amount`, `success_url`, `fail_url`을 반환한다.

주의:

- pending 주문 생성 전에 customerKey를 먼저 준비한다. customerKey 생성 실패 후 pending 주문만 남는 상황을 피하기 위해서다.
- `ProviderOrderId`는 처음에는 orderId와 같다. paymentKey가 기록되면 paymentKey로 바뀐다.
- 조직 지갑 충전은 같은 Toss 흐름을 재사용하지만 target이 organization이다. target type/id가 꼬이면 개인 지갑으로 지급될 수 있다.

### 3. 프론트 결제창 호출

파일:

- `web/default/src/features/wallet/hooks/use-toss-payment.ts`
- `web/default/src/features/organizations/components/organization-wallet.tsx`
- `web/default/src/features/organizations/lib/organization-wallet-payment.ts`

공식 문서 근거:

- 결제창 문서는 SDK 설치 후 client key로 초기화하고, `payment({ customerKey })`를 만든 뒤 `payment.requestPayment()`를 호출하라고 설명한다.
- requestPayment 파라미터에는 결제수단, 주문번호, 결제금액, success URL, fail URL이 들어간다.
- 모바일 환경에서는 iframe/frame 위에서 결제창을 호출하면 안 된다고 설명한다.

코드에서 하는 일:

- 백엔드 `/toss/pay`에서 세션 정보를 받아온다.
- `loadTossPayments(client_key)`로 SDK를 로드한다.
- `tossPayments.payment({ customerKey: customer_key })`로 payment 객체를 만든다.
- `requestPayment({ method: 'CARD', amount: { currency: 'KRW', value }, orderId, orderName, successUrl, failUrl })`를 호출한다.

주의:

- 현재 구현은 카드 결제 전용이다. `method: 'CARD'`와 서버의 `card != nil` 검증이 한 세트다.
- `PAY_PROCESS_CANCELED`는 사용자가 결제창을 닫은 케이스로 보고 조용히 실패 처리한다.
- `router/web-router.go`는 Toss 결제창 popup/redirect 차단 이슈를 줄이기 위해 `Cross-Origin-Opener-Policy: same-origin-allow-popups`를 내려준다.

### 4. success URL confirm

파일: `controller/topup_toss.go`

핵심 함수:

- `TossConfirm()`
- `validateTossConfirm()`
- `confirmTossPaymentWithSecret()`
- `isValidTossTopUpCardPayment()`

공식 문서 근거:

- 결제 인증 성공 시 success URL에 `paymentKey`, `orderId`, `amount`가 붙는다.
- success URL에서 받은 amount와 requestPayment의 amount가 같은지 반드시 확인해야 한다.
- `paymentKey`, `amount`, `orderId`는 서버에 저장해야 한다.
- 결제 승인 API는 인증 유효 시간 안에 호출해야 하며, 승인 성공 시 Payment 객체가 돌아온다.
- Payment 객체에서 사용한 결제수단 필드가 있는지 확인해야 한다.

코드에서 하는 일:

1. query에서 `paymentKey`, `orderId`, `amount`를 읽는다.
2. `LockOrder(orderId)`로 같은 주문 동시 처리를 막는다.
3. 로컬 pending 주문을 조회하고 provider와 amount를 검증한다.
4. 저장된 provider credential로 secret key를 복호화한다.
5. `model.RecordTossPaymentKey(orderId, paymentKey)`를 먼저 호출한다.
6. `/v1/payments/confirm`을 호출한다.
7. confirm 응답을 검증한다.
8. `model.RechargeToss()`로 quota를 지급한다.

검증 조건:

- `status == DONE`
- `totalAmount == local amount`
- `orderId == local orderId`
- `currency == KRW`
- `card != nil`
- 응답 `paymentKey`가 callback의 `paymentKey`와 동일

주의:

- paymentKey 저장이 실패하면 confirm API를 호출하지 않는다.
- confirm 실패가 곧 결제 실패는 아니다. network, timeout, 409, 5xx 등은 실제 결제가 완료되었을 수 있다.
- confirm 실패 시 `GET /v1/payments/{paymentKey}`로 다시 조회한다.
- 조회 결과가 DONE이면 지급한다. EXPIRED/ABORTED면 닫는다. 판단 불가 상태는 pending으로 남겨 webhook/cleanup이 처리하게 한다.

### 5. fail URL 처리

파일: `controller/topup_toss.go`

핵심 함수:

- `TossFail()`

공식 문서 근거:

- 결제 인증 실패 시 fail URL로 이동하고, code/message를 확인해 사용자에게 안내한다.
- 사용자가 결제를 취소하면 `PAY_PROCESS_CANCELED`가 발생할 수 있다.

코드에서 하는 일:

- fail URL의 `orderId`, `code`, `message`를 로그에 남긴다.
- `orderId`가 있으면 pending 주문을 failed로 닫는다.

주의:

- 이 즉시 실패 처리는 카드 결제 전용 가정이다.
- 가상계좌처럼 나중에 입금 성공이 올 수 있는 비동기 결제 수단을 추가하면, fail URL에서 바로 failed 처리하면 안 된다.

### 6. quota 지급

파일: `model/topup.go`

핵심 함수:

- `RechargeToss()`
- `CreditTopUpTarget()`
- `ManualCompleteTopUp()`

코드에서 하는 일:

- DB transaction 안에서 top-up row를 lock한다.
- provider가 Toss인지 확인한다.
- 이미 success면 idempotent하게 반환한다.
- pending 상태만 success로 바꾼다.
- `Money * QuotaPerUnit`으로 quota를 계산한다.
- 개인 또는 조직 target에 quota를 지급한다.
- top-up log를 남긴다.

주의:

- `RechargeToss()` 호출 전에는 반드시 Toss 응답 검증이 끝나 있어야 한다.
- `ManualCompleteTopUp()`에서도 Toss는 `Money` 기준으로 quota를 계산한다. `Amount`는 KRW라서 내부 quota 계산에 쓰면 안 된다.
- row lock과 status check가 중복 지급을 막는 핵심이다.

## Webhook 처리

파일: `controller/topup_toss.go`

핵심 함수:

- `TossWebhook()`
- `handleTossSubscriptionPaymentWebhook()`
- `isTossPaymentWebhookEventType()`
- `isTossTerminalFailStatus()`
- `isTossCancelStatus()`

공식 문서 근거:

- 웹훅 문서는 결제 상태 변경을 `PAYMENT_STATUS_CHANGED` 이벤트로 받을 수 있다고 설명한다.
- 빌링키 삭제는 `BILLING_DELETED` 이벤트로 받을 수 있다.
- Payment 상태에는 `DONE`, `CANCELED`, `PARTIAL_CANCELED`, `ABORTED`, `EXPIRED` 등이 있다.

코드에서 하는 일:

- webhook body를 parse한다.
- `BILLING_DELETED`는 저장된 billing key와 대조해 local key를 revoked로 바꾼다.
- `PAYMENT_STATUS_CHANGED`만 결제 이벤트로 처리한다.
- 중간 상태는 바로 OK로 넘긴다.
- top-up 주문이면 `paymentKey`로 Toss API를 다시 조회한다.
- subscription order면 별도 subscription webhook handler로 넘긴다.
- DONE은 검증 후 지급/구독 완료 처리한다.
- EXPIRED/ABORTED는 pending 주문을 닫는다.
- CANCELED/PARTIAL_CANCELED는 자동 차감하지 않고 reconciliation required 로그를 남긴다.

주의:

- webhook payload만으로 지급하면 안 된다. 반드시 Toss API를 다시 조회한다.
- webhook handler가 `503`을 반환하는 경로는 Toss 재시도를 유도하기 위한 것이다.
- 이미 성공 처리된 주문의 cancel/partial cancel은 자동 rollback하지 않는다. 사용자가 이미 quota를 썼을 수 있고 부분 환불은 정책 판단이 필요하기 때문이다.
- 현재 webhook은 가상계좌 `DEPOSIT_CALLBACK` shape를 구현하지 않는다.

## Pending cleanup과 reconcile

파일:

- `service/toss_pending_cleanup_task.go`
- `model/topup.go`
- `controller/topup_toss.go`
- `controller/subscription_payment_toss.go`

역할:

- 사용자가 결제창을 닫거나 callback/webhook이 누락된 pending 주문을 정리한다.
- paymentKey를 기록한 pending 주문은 Toss API로 재조회한다.
- paymentKey가 없는 오래된 pending 주문은 안전 기간 이후 expired로 닫는다.
- pending subscription order도 billing key/charge 상태를 기준으로 reconcile한다.

중요 구분:

- `provider_order_id == trade_no`: 아직 paymentKey가 기록되지 않은 주문이다. 결제 승인 전 이탈 가능성이 높다.
- `provider_order_id != trade_no`: Toss paymentKey가 기록된 주문이다. 실제 결제 상태를 Toss에서 조회해야 한다.

주의:

- paymentKey가 있는 주문을 단순 만료하면 실제 결제 완료 건을 미지급 처리할 수 있다.
- cleanup task는 master node에서만 실행된다.
- cleanup window 값은 Toss 결제창 유효 시간과 승인 유효 시간을 고려해서 잡혀 있다. 줄이면 정상 결제 중인 주문을 닫을 수 있다.

## 자동결제 시작 흐름

### 1. billing auth 세션 생성

파일: `controller/subscription_payment_toss.go`

핵심 함수:

- `SubscriptionRequestTossBilling()`

공식 문서 근거:

- 자동결제 문서는 client key로 SDK를 초기화하고 `payment().requestBillingAuth()`를 호출하라고 설명한다.
- 자동결제는 추가 계약된 MID의 key를 사용해야 한다.
- 자동결제 결제수단은 국내 발급 카드만 지원한다고 설명한다.

코드에서 하는 일:

- payment compliance와 Toss billing 설정을 확인한다.
- plan이 존재하고 enabled인지 확인한다.
- plan price를 KRW로 변환한다.
- 카드 최소 금액과 callback URL을 확인한다.
- 사용자별 Toss customerKey를 준비한다.
- `toss_sub_` prefix의 pending subscription order를 만든다.
- billing secret key snapshot을 `ProviderCredential`에 저장한다.
- 프론트에 `client_key`, `customer_key`, `trade_no`, `success_url`, `fail_url`을 반환한다.

주의:

- 일반 Toss 결제 key가 아니라 billing active client key를 내려줘야 한다.
- `ProviderAmount`와 `ProviderCurrency`는 첫 결제/갱신 reconciliation에 쓰이는 provider snapshot이다.
- plan 가격이 변경되어도 이미 생성된 pending order의 `ProviderAmount`를 함부로 바꾸면 안 된다.

### 2. 프론트 billing auth 호출

파일: `web/default/src/features/subscriptions/hooks/use-toss-billing.ts`

공식 문서 근거:

- 자동결제 문서는 `requestBillingAuth()`를 호출해 카드 등록창을 띄운다고 설명한다.
- 인증 성공 시 success URL에 `authKey`, `customerKey`가 전달된다.

코드에서 하는 일:

- `/api/subscription/toss/pay`로 billing auth 세션을 요청한다.
- `loadTossPayments(client_key)`로 SDK를 로드한다.
- `payment({ customerKey })`를 만든다.
- `requestBillingAuth({ method: 'CARD', successUrl, failUrl })`를 호출한다.

주의:

- `authKey`는 프론트에서 직접 쓰지 않는다. success URL callback에서 서버가 받아 billing key 발급에 사용한다.
- `customerKey`는 billing key와 매핑되므로 callback에서 서버 canonical 값과 반드시 비교한다.

### 3. billing key 발급과 첫 결제

파일: `controller/subscription_payment_toss.go`

핵심 함수:

- `SubscriptionTossBillingConfirm()`
- `issueTossBillingKeyWithSecret()`
- `StoreTossBillingKeyWithSecret()`
- `confirmTossBillingChargeOrLookupWithSecret()`
- `chargeTossBillingWithSecret()`
- `isValidTossBillingCharge()`

공식 문서 근거:

- success URL에는 `authKey`, `customerKey`가 전달된다.
- billing key 발급 API는 `authKey`, `customerKey`를 body로 받는다.
- billing key는 customerKey와 매핑해서 서버에 저장해야 한다.
- 한 번 발급된 billing key는 다시 조회할 수 없다.
- 자동결제 승인 API는 `POST /v1/billing/{billingKey}`이며 `customerKey`, `amount`, `orderId`, `orderName`을 보낸다.
- 자동결제 승인은 최대 60초가 걸릴 수 있으므로 timeout을 최소 60초로 잡아야 한다.
- 자동결제 승인 성공 응답의 Payment 객체에는 card 필드가 있어야 한다.

코드에서 하는 일:

1. `authKey`, `customerKey`, `trade_no`를 확인한다.
2. local subscription order를 lock한다.
3. pending 상태인지 확인한다.
4. callback `customerKey`가 사용자 canonical key와 같은지 확인한다.
5. 저장된 billing secret key snapshot을 복호화한다.
6. `/v1/billing/authorizations/issue`로 billing key를 발급한다.
7. billing key를 암호화 저장하고 hash를 저장한다.
8. order에 billing key id를 attach한다.
9. `/v1/billing/{billingKey}`로 첫 결제를 시도한다.
10. 응답을 `DONE`, 금액, orderId, KRW, card 기준으로 검증한다.
11. `CompleteTossBillingOrder()`로 subscription을 활성화한다.

주의:

- billing key 저장 실패 시 이미 발급된 remote billing key가 남을 수 있다. 이 경우 즉시 삭제하거나 pending revocation으로 남겨야 한다.
- issue API 응답이 불확실하고 billing key를 모르면 API로 삭제할 수 없다. Toss 콘솔 수동 확인 로그를 남긴다.
- 첫 결제가 처리 중이면 local order를 pending으로 둔다. webhook 또는 cleanup이 다시 확인한다.
- 활성화 실패 후 첫 결제는 DONE이면 자동 rollback하지 않는다. 로그를 남기고 billing key를 revoke 대상으로 만든다.

## Billing key 저장과 해지

파일: `model/toss_billing.go`

핵심 구조체:

- `UserBillingKey`

핵심 함수:

- `StoreTossBillingKeyWithSecret()`
- `StoreTossBillingKeyPendingRevocationWithSecret()`
- `GetTossBillingKeyPlainWithSecret()`
- `MarkTossBillingKeyPendingRevocation()`
- `RevokeTossBillingKey()`
- `RetryPendingTossBillingKeyRevocations()`
- `RevokeTossBillingKeyByPlain()`

공식 문서 근거:

- billing key는 고객의 결제 정보를 대신하는 값이며, customerKey와 매핑해서 저장해야 한다.
- 발급된 billing key가 더 이상 필요 없으면 삭제 API로 삭제할 수 있다.
- billing key 삭제 API는 `DELETE /v1/billing/{billingKey}`다.

코드에서 하는 일:

- `EncryptedKey`: billing key 원문을 암호화 저장한다.
- `BillingKeyHash`: webhook match와 조회 성능을 위해 HMAC hash를 저장한다.
- `ProviderCredential`: billing key 발급/결제 당시 secret key snapshot을 암호화 저장한다.
- `Status`: `active`, `pending_revocation`, `revoked`를 가진다.
- pending revocation은 원격 삭제 실패 후 재시도 가능한 상태다.

주의:

- remote delete 실패 시 local row를 바로 revoked로 만들면 재시도할 billing key를 잃어버린다.
- `pending_revocation`은 future charge에서 제외되어야 하고, background task가 계속 원격 삭제를 재시도해야 한다.
- `BILLING_DELETED` webhook은 plain billing key를 전달하므로, local encrypted key/hash와 비교해서 정확한 row만 revoke한다.

## 자동갱신 task

파일:

- `service/toss_billing_task.go`
- `model/toss_billing.go`

핵심 함수:

- `StartTossBillingTask()`
- `runTossBillingOnce()`
- `ProcessTossRenewal()`
- `PrepareTossRenewalOrder()`
- `RenewTossSubscription()`

공식 문서 근거:

- Toss는 자동결제 스케줄링을 제공하지 않으므로 서비스가 직접 스케줄링해야 한다.
- 결제 금액이 변경되면 자동결제 승인 API의 `amount`를 바꾸면 된다고 설명한다.
- 결제 주기가 변경되면 API 호출 주기를 서비스가 바꿔야 한다.

코드에서 하는 일:

- master node에서 1분마다 due renewal을 확인한다.
- remote cleanup이 가능한 경우 pending revocation billing key 삭제를 먼저 재시도한다.
- billing key 설정이 불완전하면 due subscription을 로컬 만료 처리한다.
- due subscription은 `ProcessTossRenewal()`로 결제한다.
- 갱신 주문은 `toss_sub_renew_{subId}_{nextBillingTime}` 형태의 deterministic trade no를 쓴다.
- pending renewal order를 먼저 만들고, 여기에 provider amount/currency/credential snapshot을 저장한다.
- 같은 cycle 재시도는 같은 orderId와 idempotency key를 사용한다.
- 성공하면 `RenewTossSubscription()`으로 기간을 연장한다.

주의:

- 갱신 결제 성공 후 local renew가 실패할 수 있으므로 deterministic order id가 중요하다.
- 기존 pending renewal order가 있으면 그 order의 `ProviderAmount`를 우선한다. 가격 변경이 이미 생성된 pending order를 바꾸면 안 된다.
- billing key 조회 실패, 결제 실패가 누적되면 자동갱신을 끄고 billing key를 revoke한다.

## 데이터 모델별 의미

### `TopUp`

파일: `model/topup.go`

Toss에서 중요한 필드:

- `Amount`: Toss에 청구한 KRW 금액
- `Money`: 내부 quota 지급 계산용 USD-equivalent
- `TradeNo`: local orderId, Toss `orderId`
- `ProviderOrderId`: 처음에는 orderId, paymentKey 수신 후 paymentKey
- `ProviderOrderTime`: paymentKey를 기록한 시각
- `ProviderCredential`: 해당 주문에 사용한 Toss secret key snapshot
- `PaymentProvider`: `toss`
- `Status`: pending, success, failed, expired

주의:

- `ProviderOrderId == TradeNo`면 아직 paymentKey가 없는 주문이다.
- `ProviderOrderId != TradeNo`면 paymentKey가 저장된 주문이다.
- cleanup 로직이 이 차이를 사용하므로 의미를 바꾸면 안 된다.

### `SubscriptionOrder`

파일: `model/subscription.go`

Toss에서 중요한 필드:

- `TradeNo`: billing auth order 또는 renewal order id
- `ProviderAmount`: Toss에 청구할 KRW snapshot
- `ProviderCurrency`: 현재는 `KRW`
- `ProviderCredential`: billing secret key snapshot
- `BillingKeyId`: billing key row id
- `ProviderPayload`: Toss Payment 객체 audit payload

주의:

- subscription order는 첫 결제와 renewal audit 모두에 쓰인다.
- `ProviderAmount`는 pending order 생성 시점의 금액 snapshot이다.

### `UserBillingKey`

파일: `model/toss_billing.go`

Toss에서 중요한 필드:

- `CustomerKey`: Toss customerKey
- `EncryptedKey`: 암호화된 billingKey
- `BillingKeyHash`: billingKey HMAC hash
- `ProviderCredential`: 해당 billing key에 사용할 secret key snapshot
- `Status`: active, pending_revocation, revoked

주의:

- billing key 원문은 로그에 남기지 않는다.
- decrypted key는 API 호출이나 webhook matching에 필요한 순간에만 메모리에서 다룬다.

## 프론트 UI와 표시 금액

파일:

- `web/default/src/features/subscriptions/lib/format.ts`
- `web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx`
- `web/default/src/features/wallet/components/subscription-plans-card.tsx`
- `web/default/src/features/system-settings/integrations/payment-settings-section.tsx`

역할:

- Toss 청구 예정 금액을 `price_amount * TossUnitPrice`로 표시한다.
- 최소 카드 금액 미만이면 Toss 자동결제 버튼을 비활성화한다.
- admin settings에서 일반 Toss 결제와 Toss billing 설정을 구분해 입력한다.

주의:

- 프론트 표시 금액은 편의용이다. 실제 청구 금액의 source of truth는 서버 계산이다.
- `TOSS_CARD_MINIMUM_AMOUNT_KRW` 상수는 backend `setting.TossCardMinimumAmountKRW`와 의미가 같아야 한다.
- 결제 버튼 enable 조건은 backend availability와 맞아야 한다. UI에서 보이더라도 backend가 거부할 수 있다.

## 변경 시 체크리스트

### 결제 수단을 카드 외로 늘릴 때

- 프론트 `method: 'CARD'`를 바꿀지 분기할지 결정한다.
- `isValidTossTopUpCardPayment()`와 `isValidTossBillingCharge()`의 `card != nil` 검증을 재설계한다.
- fail URL에서 즉시 failed 처리하는 로직을 비동기 수단에 맞게 바꾼다.
- webhook event type과 payload shape를 다시 확인한다.
- `WAITING_FOR_DEPOSIT` 등 중간 상태를 어떻게 유지할지 정한다.
- 취소/부분 취소 시 자동 rollback 정책을 별도로 정의한다.

### 금액 정책을 바꿀 때

- `getTossPayMoney()`, `tossUSDEquivalent()`, `TossPlanKRW()`, 프론트 `getTossChargeKRW()`를 함께 본다.
- `TopUp.Amount`와 `TopUp.Money` 의미를 바꾸지 않는다.
- subscription pending/renewal order의 `ProviderAmount`는 snapshot이라는 점을 유지한다.
- 최소 결제 금액이 바뀌면 backend와 frontend 상수를 함께 갱신한다.

### key rotation 또는 test/live 전환을 바꿀 때

- 새 주문은 active setting을 사용한다.
- 기존 주문 reconcile은 `ProviderCredential` snapshot을 우선해야 한다.
- billing key row도 provider credential을 들고 있어야 renewal과 revocation이 과거 key로 가능하다.
- 일반 결제 key와 billing key fallback을 활성화 조건에 섞지 않는다.

### webhook을 수정할 때

- webhook body만으로 상태를 바꾸지 않는다.
- 가능한 한 Toss API 조회로 authoritative 상태를 확인한다.
- retry가 필요한 실패는 5xx를 반환한다.
- 이미 success인 주문은 중복 지급하지 않는다.
- cancel/partial cancel은 manual reconciliation 로그 정책을 유지한다.

### DB migration을 수정할 때

- SQLite, MySQL, PostgreSQL을 모두 고려한다.
- raw SQL이 필요하면 프로젝트의 DB compatibility 규칙을 따른다.
- `provider_order_id`, `trade_no`, `billing_key_hash` 등 key 저장 컬럼 길이를 줄이면 안 된다.
- billing key hash backfill은 기존 encrypted key를 복호화하므로 실패 로그와 partial progress를 고려한다.

## 테스트 맵

백엔드:

- `controller/topup_toss_test.go`: 일반 Toss 결제 생성, callback 검증, confirm 실패/재조회, webhook 검증
- `controller/subscription_payment_toss_test.go`: billing auth, billing key 발급, 첫 결제, 실패 cleanup
- `model/toss_billing_test.go`: billing key 저장/해지, renewal order, deterministic trade no, pending revocation
- `model/toss_pending_cleanup_test.go`: stale pending top-up/subscription cleanup
- `service/toss_billing_task_test.go`: billing task runnable 조건, renewal task 동작
- `setting/payment_toss_test.go`: Toss key selection과 billing key pair 설정

프론트:

- `web/default/src/features/organizations/lib/organization-wallet-payment.test.ts`: 조직 지갑 Toss SDK flow 분기
- `web/default/src/features/subscriptions/lib/format.toss.test.ts`: Toss KRW 표시 금액 계산

권장 검증 명령:

```bash
GOCACHE=/tmp/go-build-cache go test ./setting ./controller ./model ./service
```

```bash
cd web/default
bun test src/features/organizations/lib/organization-wallet-payment.test.ts src/features/subscriptions/lib/format.toss.test.ts
```

참고:

- 전체 `bun run typecheck`는 현재 Toss 변경 외 기존 타입 이슈가 남아 있어 실패할 수 있다. 실패 위치가 Toss 변경 파일인지 먼저 구분한다.

## 운영 점검 포인트

- Toss 일반 결제 MID의 client key/secret key가 같은 환경인지 확인한다.
- Toss 자동결제 MID의 client key/secret key가 자동결제 계약된 MID인지 확인한다.
- test/live key를 섞지 않는다.
- `ServerAddress`는 외부에서 접근 가능한 HTTPS URL이어야 한다.
- Toss 콘솔에 `<ServerAddress>/api/toss/webhook`을 등록한다.
- 일반 결제 MID와 자동결제 MID가 다르면 양쪽 모두 webhook을 등록한다.
- `PAYMENT_STATUS_CHANGED`와 `BILLING_DELETED` 이벤트를 확인한다.
- `pending_revocation` billing key가 쌓이는지 모니터링한다.
- `TOSS RECONCILIATION REQUIRED` 로그는 운영 알림 대상으로 둔다.
- live 전환 전 결제창, 승인, webhook, billing auth, 첫 결제, renewal, cancel을 test key로 end-to-end 검증한다.

## 가장 깨지기 쉬운 지점 요약

1. `Amount`와 `Money` 혼동: Toss KRW를 내부 quota 단위처럼 쓰면 과지급된다.
2. `paymentKey` 저장 순서: confirm 전에 저장하지 않으면 paid-but-uncredited 주문 추적이 어려워진다.
3. webhook 신뢰: body만 믿으면 forged webhook으로 지급될 수 있다.
4. billing key 원격 삭제 실패: local에서 먼저 잊으면 Toss에 살아 있는 key를 다시 지울 수 없다.
5. 자동갱신 중복 청구: deterministic trade no와 idempotency key를 유지해야 한다.
6. 카드 전용 가정: 비동기 결제 수단을 추가할 때 fail URL, webhook, validation을 모두 다시 설계해야 한다.
