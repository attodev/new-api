# Toss Payments 추가 코드 라인별 해설

작성일: 2026-07-01

기준 코드: `a3f805f9 feat: harden toss payments integration` 이후 현재 워크스페이스

## 읽는 방법

이 문서는 Toss Payments 연동을 나중에 다시 볼 때, 코드 옆에 열어두는 라인 단위 주석서다.

- `L10-L20`은 해당 파일의 현재 라인 번호 기준이다.
- 라인 번호는 코드 이동이나 포맷팅 후 달라질 수 있다. 그럴 때는 함수명과 파일명을 기준으로 다시 찾는다.
- 정말 모든 공백/brace까지 한 줄씩 설명하지 않고, 유지보수 판단이 필요한 추가 코드와 위험 지점을 1-10라인 단위로 쪼개서 설명한다.
- 공식 Toss 문서 요구와 연결되는 라인은 `공식 근거`로 표시한다.

공식 문서:

- 시작하기: https://docs.tosspayments.com/guides/v2/get-started
- 결제창 연동: https://docs.tosspayments.com/guides/v2/payment-window/integration
- 자동결제 연동: https://docs.tosspayments.com/guides/v2/billing/integration
- API 레퍼런스: https://docs.tosspayments.com/reference
- 웹훅: https://docs.tosspayments.com/guides/v2/webhook

## 전체 흐름 한 줄 요약

프론트는 Toss SDK로 결제창만 열고, 서버는 pending 주문을 만든 뒤 callback/webhook에서 Toss API를 다시 확인하고, 검증이 끝난 경우에만 충전 또는 구독 활성화를 수행한다.

## `setting/payment_toss.go`

### L5-L8: Toss 상수

- `TossCardMinimumAmountKRW`: Toss 카드 결제 최소 금액 방어선이다.
- `TossOrderNameMaxRunes`: Toss `orderName` 길이 제한에 맞춰 구독명/갱신명을 자르기 위한 상한이다.
- 공식 근거: 결제창과 자동결제 승인 API는 `amount`, `orderName` 같은 결제 요청 파라미터를 요구한다.
- 주의: 프론트의 `TOSS_CARD_MINIMUM_AMOUNT_KRW`와 의미가 같아야 한다. 둘 중 하나만 바꾸면 UI는 가능해 보이는데 서버가 막거나, UI가 막는데 서버는 가능한 상태가 된다.

### L13-L19: 일반 Toss 결제 설정

- `TossEnabled`: 일반 충전 결제 기능 on/off다.
- `TossTestMode`: Toss는 sandbox host가 따로 있는 방식이 아니라 test key/live key로 환경이 갈린다.
- `TossClientKey`, `TossSecretKey`: live 일반 결제 키다.
- `TossTestClientKey`, `TossTestSecretKey`: test 일반 결제 키다.
- 공식 근거: 결제창 연동 문서는 client key로 SDK를 초기화하고 secret key로 승인 API를 호출한다고 설명한다.
- 주의: client key는 프론트에 내려가도 되지만 secret key는 절대 프론트에 내려가면 안 된다.

### L20-L27: 자동결제 전용 key pair

- billing client/secret key를 일반 결제 key와 분리한다.
- Toss 자동결제는 별도 계약/MID가 필요할 수 있으므로 일반 결제 키만으로 활성화 판단하면 위험하다.
- 공식 근거: 자동결제 가이드는 자동결제로 계약된 MID의 API 키를 사용하라고 설명한다.
- 주의: 일반 결제 MID와 자동결제 MID가 다르면 webhook도 각각 등록해야 한다.

### L28-L29: 단가와 최소 충전

- `TossUnitPrice`: 내부 1 unit을 몇 KRW로 청구할지 정한다.
- `TossMinTopUp`: 사용자가 입력할 수 있는 최소 내부 충전 unit이다.
- 주의: `TossUnitPrice`는 Toss 청구 금액 계산과 subscription 표시 금액 계산 모두에 쓰인다. 값이 0 이하이면 결제 기능을 막는다.

### L31-L43: active 일반 key 선택

- `TossActiveClientKey()`는 test mode면 test client key, 아니면 live client key를 반환한다.
- `TossActiveSecretKey()`도 같은 방식으로 secret key를 선택한다.
- 주의: test client key와 live secret key를 섞으면 Toss에서 `UNAUTHORIZED_KEY`류 오류가 난다.

### L45-L57: billing key pair 선택과 fallback

- `tossConfiguredPair()`는 billing key가 비어 있으면 fallback key를 반환한다.
- `TossActiveBillingKeyPair()`는 과거 저장 데이터나 API 호출 호환을 위해 일반 Toss key fallback을 허용한다.
- 주의: fallback은 실제 API 호출 호환용이지, 자동결제 기능을 켜도 된다는 뜻이 아니다.

### L59-L76: 명시적 billing key pair

- `TossExplicitActiveBillingKeyPair()`는 billing key만 반환하고 fallback하지 않는다.
- `isTossBillingEnabled()`와 billing task runnable check가 이 함수를 사용한다.
- 주의: 자동결제 활성화 체크에서 `TossActiveBillingKeyPair()`를 쓰면 일반 키 fallback 때문에 billing이 잘못 켜질 수 있다.

## `controller/payment_webhook_availability.go`

### L30-L43: 일반 Toss top-up 활성화 조건

- compliance 확인이 안 되면 false다.
- `TossEnabled`가 false면 false다.
- `TossUnitPrice <= 0`이면 금액 계산이 불가능하므로 false다.
- active client/secret key가 모두 있어야 한다.
- `ServerAddress`가 Toss callback URL로 쓸 수 있어야 한다.
- 공식 근거: 결제창 연동은 success URL/fail URL을 Toss에 넘긴다.
- 주의: 운영에서 `ServerAddress`가 내부 주소면 사용자가 결제 후 돌아올 수 없다.

### L45-L61: Toss billing 활성화 조건

- 일반 결제 조건과 유사하지만 `TossBillingEnabled`와 명시적 billing key pair를 사용한다.
- 공식 근거: 자동결제는 별도 계약과 빌링키 발급/승인 흐름을 가진다.
- 주의: 이 함수가 느슨해지면 UI에 Toss Auto Pay가 보이지만 실제 billing auth 또는 billing charge가 실패한다.

## `router/api-router.go`

### L63-L69: Toss 공개 callback route

- `/api/toss/confirm`: 일반 결제 인증 성공 callback이다.
- `/api/toss/fail`: 일반 결제 인증 실패 callback이다.
- `/api/toss/webhook`: Toss webhook 수신 endpoint다.
- `/api/subscription/toss/confirm/:trade_no`: billing auth 성공 callback이다.
- `/api/subscription/toss/fail/:trade_no`: billing auth 실패 callback이다.
- query fallback route도 남겨 과거 callback URL 형태와 호환한다.
- 주의: 이 route들은 Toss와 브라우저가 호출하므로 user auth middleware를 거치지 않는다. 그래서 controller에서 local order와 Toss API 검증이 필수다.

### L115-L116: 개인 지갑 Toss route

- `/api/user/toss/pay`: pending top-up 주문을 만들고 프론트 SDK에 필요한 값을 반환한다.
- `/api/user/toss/amount`: 입력 unit 기준 Toss KRW 청구 금액을 미리 계산한다.
- 주의: `/amount`는 표시/확인용이고 `/pay`가 다시 서버에서 금액을 계산한다. 프론트가 보낸 금액을 신뢰하지 않는다.

### L209-L210: 조직 지갑 Toss route

- 개인 지갑과 같은 Toss 흐름을 조직 wallet target으로 재사용한다.
- 주의: target type/id를 controller에서 먼저 세팅한 뒤 `RequestTossPay()`를 호출한다.

### L230-L231: subscription Toss route

- `/api/subscription/toss/pay`: billing auth 세션을 만든다.
- `/api/subscription/toss/cancel`: 사용자 자동갱신을 끄고 billing key revocation을 시작한다.

## `router/web-router.go`

### L63-L68: COOP header

- SPA shell 응답에 `Cross-Origin-Opener-Policy: same-origin-allow-popups`를 넣는다.
- 공식 근거: Toss 결제창 문서는 브라우저/모바일 결제창 호출 환경에 주의하라고 안내한다.
- 주의: 이 header가 없으면 결제창 popup/redirect와 브라우저 보안 정책이 충돌할 수 있다.

## `controller/topup_toss.go`

### L28-L30: Toss API base와 retry 횟수

- `tossAPIBase`는 모든 Toss Core API 호출의 base URL이다.
- `tossAPIMaxAttempts`는 transient 실패 retry 횟수다.
- 주의: 테스트에서는 이 값을 바꾸기보다 HTTP mocking 또는 함수 injection을 사용한다.

### L32-L37: transient status 판정

- request timeout, conflict, rate limit, 5xx를 retry 가능 상태로 본다.
- `409 Conflict`는 idempotency 처리 중일 수 있으므로 곧바로 실패로 닫지 않는다.
- 공식 근거: 승인/조회 API는 HTTP status와 error object를 반환한다. 네트워크/처리 중 오류는 실제 결제 상태와 다를 수 있다.
- 주의: 4xx를 전부 terminal failure로 보면 이미 결제가 완료된 건을 미지급 처리할 수 있다.

### L39-L59: retry delay

- attempt index에 따라 100ms, 200ms, 300ms 식으로 짧게 대기한다.
- context cancellation을 존중한다.
- 주의: webhook handler는 응답 시간이 중요하다. retry delay를 크게 늘리면 Toss webhook retry 정책과 충돌한다.

### L61-L63: 기본 secret API wrapper

- 일반 Toss secret key를 사용하는 짧은 wrapper다.
- 내부적으로 `doTossAPIRequestWithSecret()`에 위임한다.

### L65-L89: Toss HTTP request 생성

- accepted status set을 만든다.
- body가 있으면 reader를 준비한다.
- `http.NewRequestWithContext()`로 context timeout을 묶는다.
- secret key 뒤에 `:`을 붙여 base64 인코딩하고 Basic Authorization header를 만든다.
- body가 있으면 JSON content type을 넣는다.
- idempotency key가 있으면 header에 넣는다.
- 공식 근거: 결제 승인과 빌링키 발급 문서는 `secretKey + ":"` Basic 인증을 요구한다.
- 주의: secret key 뒤 콜론을 빼면 Toss 인증이 실패한다.

### L91-L124: Toss HTTP 응답 처리와 retry

- HTTP 요청을 보내고 body를 읽은 뒤 response body를 닫는다.
- accepted status면 성공으로 반환한다.
- accepted가 아니면 status/body를 포함한 error를 만든다.
- transient가 아니면 즉시 반환한다.
- transient면 attempt가 남은 경우 delay 후 재시도한다.
- 마지막까지 실패하면 마지막 status/body/error를 반환한다.
- 주의: 이 함수는 지급/활성화 판단을 하지 않는다. 상위 함수가 Payment 객체를 검증해야 한다.

### L126-L146: Toss 요청/응답 DTO

- `TossPayRequest`: 프론트에서 들어오는 입력 unit과 payment method다.
- `tossPaymentCard`: Toss Payment 객체의 card subset이다.
- `tossConfirmResponse`: 서버가 실제로 검증하는 Payment 객체 subset이다.
- 공식 근거: 결제 승인/조회/자동결제 승인 API는 Payment 객체를 반환한다.
- 주의: 새 검증 필드가 필요하면 DTO에 추가하고 `isValid...` 함수도 같이 바꾼다.

### L148-L158: provider credential 복호화

- 주문 생성 시 암호화 저장한 secret key snapshot을 복호화한다.
- 실패하거나 비어 있으면 active key fallback을 사용한다.
- 주의: fallback은 운영 복구용이다. 가능하면 과거 주문은 snapshot key로 조회/승인해야 key rotation 후에도 안전하다.

### L160-L191: `getTossPayMoney`

- 사용자가 입력한 내부 금액을 Toss KRW 청구 금액으로 바꾼다.
- token 표시 모드에서는 token을 unit으로 환산한다.
- group ratio와 amount discount를 반영한다.
- `TossUnitPrice`가 0 이하이면 0을 반환해 상위 로직이 결제를 막게 한다.
- decimal 연산으로 float 오차를 줄이고 KRW 정수로 반올림한다.
- 주의: 이 함수 결과는 `TopUp.Amount`에 들어가는 KRW다. 내부 quota 지급량이 아니다.

### L193-L200: `tossUSDEquivalent`

- KRW 청구 금액을 내부 `Money` 값으로 되돌린다.
- `Money = chargedKRW / TossUnitPrice`다.
- 주의: `RechargeToss()`는 `Money * QuotaPerUnit`으로 quota를 계산한다. `Amount`로 계산하면 과지급된다.

### L202-L224: `RequestTossAmount`

- request JSON을 bind한다.
- Toss top-up availability를 확인한다.
- 최소 top-up unit을 확인한다.
- 사용자 group을 읽고 KRW 청구 금액을 계산한다.
- 카드 최소 금액 미만이면 거부한다.
- 성공하면 문자열 형태의 KRW 금액을 반환한다.
- 주의: 이 endpoint는 미리보기다. 실제 결제 주문 생성은 `RequestTossPay()`에서 다시 계산한다.

### L226-L242: `isValidServerAddress`

- `ServerAddress`를 trim하고 URL parse한다.
- host가 없으면 실패한다.
- HTTPS는 운영 callback으로 허용한다.
- HTTP는 localhost 계열만 허용한다.
- 주의: public HTTP callback을 허용하면 운영 결제 callback이 안전하지 않다.

### L244-L275: `RequestTossPay` 입력 검증

- request bind 실패는 invalid params다.
- payment method가 `toss`가 아니면 거부한다.
- Toss top-up 활성화와 ServerAddress를 확인한다.
- 최소 top-up unit과 카드 최소 KRW 금액을 확인한다.
- 주의: amount는 프론트가 보낸 값이므로 그대로 믿지 않고 서버에서 `getTossPayMoney()`로 다시 계산한다.

### L277-L284: customerKey 선생성

- `GetOrCreateTossCustomerKey()`로 사용자별 안정적인 customerKey를 가져온다.
- 실패하면 pending order를 만들지 않고 종료한다.
- 공식 근거: Toss Payment 객체의 customerKey는 유추 불가능한 구매자 식별자여야 한다.
- 주의: user id/email/phone을 customerKey로 쓰면 안 된다.

### L286-L293: orderId와 credential snapshot

- `new-api-toss-{userId}-{millis}-{random}` reference를 만들고 SHA1으로 `toss_...` orderId를 만든다.
- active secret key를 암호화해서 주문에 저장한다.
- 공식 근거: orderId는 각 주문을 식별하는 고유 값이어야 한다.
- 주의: orderId가 바뀌면 Toss callback과 local order 매칭이 깨진다.

### L295-L313: pending TopUp 생성

- `Amount`: Toss 청구 KRW
- `Money`: 내부 quota 지급 기준
- `TradeNo`: local orderId
- `ProviderOrderId`: 처음에는 orderId다. paymentKey 저장 전 상태를 나타낸다.
- `ProviderCredential`: secret key snapshot
- `PaymentMethod`, `PaymentProvider`: Toss 전용 guard
- `Status`: pending
- 주의: `ProviderOrderId == TradeNo`라는 상태 의미가 cleanup 로직의 핵심이다.

### L315-L327: 프론트 SDK용 응답

- client key, customer key, order id, order name, amount, success URL, fail URL을 반환한다.
- 공식 근거: `requestPayment()`에는 결제수단, 주문번호, 결제금액, success URL, fail URL이 필요하다.
- 주의: secret key는 이 응답에 절대 넣지 않는다.

### L330-L360: `confirmTossPaymentWithSecret`

- 15초 timeout context를 만든다.
- Toss 승인 body에 `paymentKey`, `orderId`, `amount`를 넣는다.
- `common.Marshal`로 JSON을 만든다.
- `POST /v1/payments/confirm`을 호출한다.
- 200 OK가 아니면 status와 함께 error를 반환한다.
- 응답 body를 `tossConfirmResponse`로 unmarshal한다.
- 공식 근거: 결제 승인 API는 success URL에서 받은 세 값을 body로 받는다.
- 주의: 이 함수가 성공해도 상위에서 `DONE`, 금액, 통화, 카드 필드를 다시 검증해야 한다.

### L362-L386: paymentKey 기반 결제 조회

- webhook 처리 시간을 고려해 8초 timeout을 잡는다.
- `GET /v1/payments/{paymentKey}`를 호출한다.
- 응답을 같은 `tossConfirmResponse`로 decode한다.
- `getTossBillingPayment()`는 billing secret key를 사용해 같은 조회를 한다.
- 공식 근거: paymentKey는 결제 조회, 승인, 취소, 대사에 쓰이는 고유 키다.
- 주의: webhook body만 믿지 않고 이 함수로 authoritative fetch를 수행한다.

### L388-L416: redirect와 fail query sanitizing

- Toss callback 후 SPA 경로로 302 redirect한다.
- fail code/message/orderId는 query string으로 넘기되 길이를 300 rune으로 제한한다.
- 주의: 외부 입력인 message를 무제한 query로 넘기면 URL 폭주나 로그 오염이 생길 수 있다.

### L418-L438: confirm 검증 헬퍼

- local topUp이 없으면 실패다.
- provider가 Toss가 아니면 실패다.
- local amount와 callback amount가 다르면 실패다.
- `isValidTossTopUpCardPayment()`는 DONE, totalAmount, orderId, KRW, card 존재를 확인한다.
- 공식 근거: success URL amount 검증과 Payment 객체의 결제수단 필드 확인 요구.
- 주의: 이 함수는 카드 전용이다. 다른 결제수단 추가 시 재설계해야 한다.

### L440-L511: recorded pending top-up reconcile

- 오래된 pending 중 paymentKey가 저장된 주문만 처리한다.
- order lock으로 confirm/webhook과 동시 처리를 막는다.
- 현재 주문 상태를 다시 읽어 pending인지 확인한다.
- 저장된 secret snapshot으로 Toss payment를 조회한다.
- 404면 failed로 닫는다.
- orderId mismatch면 forged/잘못된 key로 보고 failed 처리한다.
- DONE이고 검증 통과면 `RechargeToss()`로 지급한다.
- DONE인데 금액/통화/card가 맞지 않으면 failed 처리한다.
- EXPIRED/ABORTED/CANCELED/PARTIAL_CANCELED는 상태별로 닫거나 reconciliation 로그를 남긴다.
- 나머지 stale 상태는 approval window elapsed로 expired 처리한다.
- 주의: 이 함수는 paymentKey가 기록된 주문만 처리한다. paymentKey 없는 주문은 별도 expire 함수가 담당한다.

### L513-L540: `TossConfirm` callback 입력 처리

- success URL의 `paymentKey`, `orderId`, `amount`를 읽는다.
- 누락되거나 amount parse 실패면 topup 화면으로 돌린다.
- `LockOrder(orderId)`로 중복 confirm/webhook 경쟁을 막는다.
- local order를 조회하고 provider/amount를 검증한다.
- 이미 success면 log 화면으로 보낸다.
- pending이 아니면 결제를 진행하지 않는다.
- 주의: 인증 성공 callback도 브라우저 query라서 신뢰하지 않는다.

### L550-L560: paymentKey 선저장

- 주문의 provider credential에서 secret key를 복호화한다.
- `RecordTossPaymentKey()`를 confirm API 호출 전에 수행한다.
- 저장 실패 시 confirm API를 호출하지 않는다.
- 공식 근거: Toss 문서는 success URL의 `paymentKey`, `amount`, `orderId`를 서버에 저장하라고 한다.
- 주의: 이 순서가 깨지면 결제는 승인됐지만 local에 paymentKey가 없어 reconcile하기 어려워진다.

### L562-L615: confirm 실패 후 authoritative 조회

- `/v1/payments/confirm` 실패 시 바로 failed 처리하지 않는다.
- `GET /v1/payments/{paymentKey}`로 실제 상태를 다시 확인한다.
- 404면 forged/invalid로 보고 failed 처리한다.
- 조회 자체가 transient 실패면 pending으로 남긴다.
- order mismatch면 failed 처리한다.
- DONE이면 `RechargeToss()`로 지급한다.
- EXPIRED/ABORTED면 failed/expired로 닫는다.
- 그 외 상태는 pending으로 유지한다.
- 주의: 409 idempotency processing 같은 경우를 결제 실패로 닫지 않는 것이 핵심이다.

### L617-L630: confirm 성공 응답 검증과 지급

- confirm 응답이 card payment로 유효한지 다시 검증한다.
- 응답 paymentKey가 callback paymentKey와 같은지 확인한다.
- 검증 후에만 `RechargeToss()`를 호출한다.
- 성공하면 log 화면으로 redirect한다.
- 주의: 승인 API 200만으로 지급하면 금액/통화/order mismatch를 놓칠 수 있다.

### L632-L648: `TossFail`

- fail URL의 orderId/code/message를 로그로 남긴다.
- orderId가 있으면 pending top-up을 failed로 닫는다.
- 공식 근거: 결제 인증 실패 시 fail URL로 error code/message가 전달된다.
- 주의: 현재 카드 결제 전용 처리다. 비동기 결제수단 추가 시 여기서 바로 failed 처리하면 안 된다.

### L650-L677: Toss status 분류

- EXPIRED, ABORTED는 terminal fail로 본다.
- CANCELED, PARTIAL_CANCELED는 취소/환불 계열로 본다.
- PAYMENT_STATUS_CHANGED만 payment webhook event로 처리한다.
- 주의: WAITING_FOR_DEPOSIT 같은 상태는 현재 처리하지 않는다. 가상계좌 추가 시 별도 설계가 필요하다.

### L679-L774: subscription payment webhook 처리

- order가 Toss subscription order인지 확인한다.
- cancel이 아니고 DONE이 아니면 OK만 반환한다.
- 이미 success거나 pending이 아니면 idempotent하게 OK다.
- paymentKey와 billingKeyId가 없으면 reconciliation 로그만 남긴다.
- plan과 chargeKRW를 다시 계산 또는 snapshot에서 가져온다.
- paymentKey로 Toss payment를 재조회한다.
- billing charge 검증 통과 시 `CompleteTossBillingOrder()`를 호출한다.
- cancel/partial cancel이면 payment를 재조회하고 manual reconciliation log를 남긴다.
- 주의: subscription webhook도 body만 믿지 않고 Toss API 조회를 한다.

### L776-L797: `TossWebhook` 주석 블록

- DONE, EXPIRED/ABORTED, CANCELED/PARTIAL_CANCELED 처리 정책을 명시한다.
- 자동 quota rollback을 하지 않는 이유를 설명한다.
- BILLING_DELETED 매칭 정책을 설명한다.
- 가상계좌 DEPOSIT_CALLBACK 미지원도 명시한다.
- 주의: 이 주석은 future maintainer에게 정책을 알려주는 문서 역할을 하므로 흐름 변경 시 같이 수정한다.

### L797-L845: webhook body parse와 BILLING_DELETED

- request body를 읽고 JSON으로 parse한다.
- BILLING_DELETED면 billingKey를 top-level 또는 data에서 찾는다.
- billingKey가 없으면 OK로 종료한다.
- `RevokeTossBillingKeyByPlain()`로 local billing key와 매칭 후 revoked 처리한다.
- 실패 시 503으로 Toss 재시도를 유도한다.
- 공식 근거: 웹훅 문서는 `BILLING_DELETED` event type을 제공한다.
- 주의: billingKey는 암호화 저장되어 있으므로 plain key 비교는 model 쪽에서 hash/decrypt로 제한적으로 수행한다.

### L846-L877: payment webhook 필터링과 order routing

- PAYMENT_STATUS_CHANGED가 아니면 OK다.
- orderId가 없으면 OK다.
- DONE/cancel/terminal fail 외 중간 상태는 OK다.
- order lock을 잡는다.
- top-up order가 아니면 subscription webhook handler로 넘긴다.
- 주의: 알 수 없는 event에 4xx를 반환하면 Toss 재시도가 불필요하게 쌓일 수 있다.

### L879-L905: 이미 닫힌 top-up 처리

- 이미 success인 주문은 cancel/partial cancel만 관심 있게 본다.
- cancel webhook도 paymentKey로 Toss API를 재조회한다.
- 진짜 cancel이면 manual reconciliation log를 남긴다.
- pending이 아닌 failed/expired 주문은 OK로 무시한다.
- 주의: 이미 지급된 quota를 자동 차감하지 않는 정책이 여기서 유지된다.

### L907-L947: pending top-up DONE webhook 처리

- paymentKey가 없으면 지급하지 않는다.
- stored credential로 Toss payment를 재조회한다.
- orderId mismatch면 무시한다.
- DONE이면 amount/currency/card 검증을 한다.
- `RecordTossPaymentKey()`로 paymentKey를 저장한다.
- `RechargeToss()`로 idempotent 지급한다.
- 실패 시 503으로 webhook retry를 유도한다.
- 주의: webhook 지급도 confirm과 동일하게 paymentKey 선저장 후 지급한다.

### L949-L976: pending top-up terminal/cancel webhook

- EXPIRED/ABORTED면 failed 또는 expired로 닫는다.
- CANCELED/PARTIAL_CANCELED면 manual reconciliation log를 남기고 failed로 닫는다.
- 그 외 상태는 OK다.
- 주의: partial cancel은 남은 결제 잔액이 있을 수 있으므로 단순 성공/실패로 자동 정산하지 않는다.

### L978-L990: 조직 지갑 wrapper

- 조직 target 준비가 성공하면 일반 Toss handler를 재사용한다.
- 주의: target context가 request에 붙어 있어야 `TopUp.TargetType/TargetId`가 조직으로 저장된다.

## `model/topup.go`

### L16-L32: `TopUp` 모델 필드

- `Amount`: provider 청구 금액이다. Toss에서는 KRW다.
- `Money`: 내부 quota 지급 기준이다.
- `TradeNo`: local order id이자 Toss orderId다.
- `ProviderOrderId`: Toss paymentKey 저장 위치다. 생성 직후에는 orderId를 넣는다.
- `ProviderOrderTime`: paymentKey 기록 시각이다.
- `ProviderCredential`: provider secret snapshot이다.
- `TargetType/TargetId`: 개인 지갑과 조직 지갑을 구분한다.
- 주의: Toss에서 `Amount`와 `Money`의 의미가 다르다.

### L39-L58: payment method/provider 상수

- `PaymentMethodToss`와 `PaymentProviderToss`를 추가해 method와 provider guard를 건다.
- 주의: method/provider 둘 중 하나만 맞는 상태를 허용하면 cross-provider callback 공격에 취약해진다.

### L66-L72: Toss top-up reconciler injection

- model은 controller를 import하지 않기 위해 function pointer를 둔다.
- controller init에서 실제 Toss API 조회 함수를 주입한다.
- 주의: import cycle을 피하기 위한 구조다. model에서 HTTP controller를 직접 import하지 않는다.

### L147-L172: pending top-up status update

- tradeNo가 비면 실패한다.
- DB별 quoting을 맞춘다.
- transaction에서 row lock 후 provider와 pending status를 확인한다.
- target status로 저장한다.
- 주의: webhook/fail/cleanup이 모두 이 함수로 pending만 닫아야 중복 처리에 안전하다.

### L458-L530: 수동 완료와 Toss 과지급 방지

- admin 수동 완료도 row lock을 잡는다.
- 이미 success면 idempotent하게 반환한다.
- pending이 아니면 수동 완료를 막는다.
- Stripe/PayPal/Toss는 `Money * QuotaPerUnit`으로 quota를 계산한다.
- 그 외 provider는 기존 로직처럼 `Amount * QuotaPerUnit`을 사용한다.
- 주의: Toss `Amount`는 KRW라서 quota 계산에 쓰면 수백/수천 배 과지급될 수 있다.

### L650-L666: paymentKey 없는 stale pending expire

- 오래된 Toss pending 중 `provider_order_id = trade_no`인 주문만 expired 처리한다.
- 이 조건은 paymentKey가 한 번도 기록되지 않은 주문을 의미한다.
- 공식 근거: 결제창 이탈 또는 `PAY_PROCESS_CANCELED`에서는 callback/webhook이 충분히 오지 않을 수 있다.
- 주의: paymentKey가 있는 주문은 이 함수로 만료하면 안 된다.

### L668-L700: paymentKey 있는 stale pending reconcile

- injected reconciler가 없으면 no-op이다.
- cutoff 이전의 pending 중 `provider_order_id <> trade_no`인 주문을 찾는다.
- limit만큼 순서대로 reconciler를 호출한다.
- 개별 실패는 기록하고 다음 주문을 계속 처리한다.
- 주의: 이 함수는 DB row 선별만 하고 Toss API 판단은 controller-injected 함수가 한다.

### L702-L737: `RecordTossPaymentKey`

- tradeNo/paymentKey 누락을 막는다.
- DB별 quoting을 맞춘다.
- provider가 Toss인 주문만 업데이트한다.
- `provider_order_id`를 paymentKey로 바꾸고 `provider_order_time`을 기록한다.
- MySQL RowsAffected 0 문제를 피하려고 업데이트 후 값을 직접 다시 조회한다.
- 저장된 값이 paymentKey가 아니거나 timestamp가 없으면 error다.
- 공식 근거: Toss 문서는 paymentKey를 결제 승인/취소/조회에 쓰므로 반드시 저장하라고 한다.
- 주의: 이 함수는 confirm 전과 webhook 지급 전 모두에서 필수다.

### L739-L799: `RechargeToss`

- tradeNo 누락을 막는다.
- row lock으로 주문을 잡는다.
- provider가 Toss인지 확인한다.
- 이미 success면 idempotent하게 반환한다.
- pending이 아니면 실패한다.
- paymentKey가 있으면 provider_order_id에 저장한다.
- complete time과 success status를 저장한다.
- `Money * QuotaPerUnit`으로 quota를 계산한다.
- 개인/조직 target wallet에 quota를 더한다.
- 성공 로그를 남긴다.
- 주의: 이 함수는 Toss 검증을 하지 않는다. 호출자가 반드시 검증 후 호출해야 한다.

### L925-L957: Toss customerKey 생성

- `cust_` prefix와 32자 random string으로 customerKey를 만든다.
- 사용자 row에 저장된 key가 있으면 재사용한다.
- 없으면 conditional update로 race-safe하게 저장한다.
- 다른 요청이 먼저 저장했으면 다시 읽어서 winner를 반환한다.
- 공식 근거: customerKey는 유추 불가능한 고유 값이어야 하며 billingKey와 연결된다.
- 주의: customerKey를 user id나 email로 바꾸지 않는다.

## `controller/subscription_payment_toss.go`

### L23-L33: billing timeout과 dependency injection

- `tossBillingChargeTimeout`은 70초다.
- 자동결제 승인 API가 최대 60초 걸릴 수 있다는 문서 요구를 반영한다.
- `init()`에서 model에 top-up reconciler, subscription reconciler, billing charger, billing revoker를 주입한다.
- 주의: model에서 controller를 import하지 않기 위한 구조다.

### L35-L69: billing key 발급 응답 DTO와 card storage helper

- `tossBillingIssueResponse`는 billingKey, customerKey, 카드사, 마스킹 번호를 담는다.
- Toss API 버전/응답 형태 차이를 고려해 top-level cardCompany/cardNumber와 nested card를 모두 본다.
- 저장용 카드사/번호는 공백을 제거하고 fallback 순서로 선택한다.
- 주의: billingKey 원문은 이 DTO를 지나 모델에서 암호화 저장된다.

### L71-L86: subscription KRW 계산

- plan price를 `model.TossPlanKRW()`로 KRW 변환한다.
- order에 `ProviderAmount`가 있으면 snapshot을 우선한다.
- currency가 비어 있거나 KRW일 때만 snapshot을 신뢰한다.
- 주의: plan 가격 변경 후에도 이미 생성된 pending order는 기존 provider amount를 유지해야 한다.

### L88-L104: helper

- secret key가 비어 있으면 active billing secret으로 fallback한다.
- callback path param 또는 query에서 trade no를 읽는다.
- 주의: fallback은 복구/호환용이다. 새 주문은 provider credential snapshot을 갖는 것이 정상이다.

### L106-L129: billing key issue API

- `authKey`, `customerKey`를 JSON body로 보낸다.
- `POST /v1/billing/authorizations/issue`를 호출한다.
- idempotency key로 tradeNo를 사용할 수 있게 받는다.
- 응답을 DTO로 decode한다.
- 공식 근거: 자동결제 success URL의 `authKey`, `customerKey`로 billing key 발급 API를 호출한다.
- 주의: billingKey 발급 응답을 잃으면 서버가 모르는 remote billingKey가 생길 수 있다.

### L131-L160: billing key로 결제 승인

- `POST /v1/billing/{billingKey}`를 호출한다.
- body에 customerKey, amount, orderId, orderName을 넣는다.
- 70초 timeout을 사용한다.
- 응답은 Payment 객체 subset으로 decode한다.
- 공식 근거: 자동결제 승인 API는 billingKey path와 customerKey/body 주문 정보를 요구한다.
- 주의: billingKey와 customerKey가 매칭되지 않으면 Toss가 거부한다.

### L162-L211: billing charge 실패 후 orderId 조회

- billing charge API가 실패하면 `GET /v1/payments/orders/{orderId}`로 다시 조회한다.
- 조회 결과가 유효한 DONE이면 응답 유실로 보고 성공 복구한다.
- charge 실패가 transient면 `ErrTossBillingChargePending`으로 감싸 pending 유지 신호를 준다.
- non-transient면 원래 error를 반환한다.
- 공식 근거: API 레퍼런스는 orderId 결제 조회를 제공한다.
- 주의: 자동결제 charge도 HTTP 실패만 보고 실패 처리하면 중복 청구 또는 미활성화가 생길 수 있다.

### L213-L229: model용 billing charger adapter

- controller의 Toss HTTP 응답을 model의 `TossBillingChargeResult`로 변환한다.
- `Done`, `Total`, `PaymentKey`, `ProviderPayload`만 넘긴다.
- 주의: model이 controller DTO를 직접 알지 않게 하는 adapter다.

### L231-L267: billing key 원격 삭제

- `DELETE /v1/billing/{billingKey}`를 호출한다.
- 200 OK와 404 Not Found를 받아들인다.
- 404는 이미 삭제된 key로 별도 error sentinel을 반환한다.
- `revokeTossBillingKey()`는 local pending_revocation을 먼저 표시하고 remote delete를 시도한다.
- remote delete 성공 후 local revoked로 바꾼다.
- 공식 근거: 발급된 billingKey가 필요 없으면 삭제 API로 삭제할 수 있다.
- 주의: remote delete 실패 시 local key를 잊으면 재시도할 수 없다.

### L269-L305: 발급된 billingKey cleanup 보조

- issue 성공 후 local 저장/attach/첫 결제 실패가 나면 billingKey를 pending revocation으로 저장하고 삭제 시도한다.
- queue 실패 시 운영 로그를 남겨 Toss 콘솔 수동 확인을 유도한다.
- 첫 결제 DONE 후 subscription activation 실패는 reconciliation required 로그를 남긴다.
- 주의: remote side effect가 생긴 뒤 local 단계가 실패하는 케이스를 계속 추적해야 한다.

### L307-L314: billing charge 검증

- DONE, amount, orderId, KRW, card 존재를 확인한다.
- 공식 근거: 자동결제 승인 성공 Payment 객체에는 card 필드가 있어야 한다.
- 주의: 카드 자동결제 전용 검증이다.

### L316-L440: stale pending subscription reconcile

- tradeNo가 없으면 resolved 처리한다.
- order lock 후 현재 주문을 다시 읽는다.
- 이미 success거나 pending이 아니면 resolved다.
- billingKeyId가 없으면 아직 발급 전이므로 false다.
- renewal tradeNo인지 파싱한다.
- provider amount snapshot 또는 plan price로 chargeKRW를 결정한다.
- 최소 금액 미만이면 order/subscription 상태를 정리한다.
- stored credential로 orderId 조회를 한다.
- 404면 결제 없음으로 보고 expire/failure 처리한다.
- DONE이고 검증되면 첫 결제 order 또는 renewal order를 완료한다.
- terminal fail이면 expire/failure 처리한다.
- cancel이면 manual reconciliation log 후 expire/failure 처리한다.
- stale non-terminal도 expire/failure 처리한다.
- 주의: renewal order와 first subscription order는 완료 함수가 다르다.

### L442-L476: reconcile용 expire helper

- renewal order는 failure count를 올릴 수 있다.
- invalid renewal order는 order만 expire한다.
- first subscription order는 billing key를 pending revocation으로 바꿀 수 있다.
- 주의: cleanup 경로가 billingKey를 잊지 않도록 helper가 분리되어 있다.

### L480-L574: billing auth 세션 생성

- request plan id를 검증한다.
- Toss billing enabled를 확인한다.
- plan existence/enabled/max purchase를 확인한다.
- plan price를 KRW로 바꾸고 최소 금액을 확인한다.
- ServerAddress를 확인한다.
- user와 customerKey를 준비한다.
- `toss_sub_` tradeNo를 만든다.
- billing secret snapshot을 암호화한다.
- pending `SubscriptionOrder`를 만든다.
- 프론트에 billing client key, customerKey, tradeNo, success/fail URL을 반환한다.
- 공식 근거: 자동결제는 `requestBillingAuth()`로 billing auth를 시작하고 success URL로 authKey/customerKey를 받는다.
- 주의: 일반 Toss client key가 아니라 billing client key를 내려야 한다.

### L576-L627: billing auth success callback 검증

- `authKey`, `customerKey`, `tradeNo` 누락을 막는다.
- order lock 후 local subscription order를 읽는다.
- Toss order인지, pending인지 확인한다.
- plan과 chargeKRW를 다시 구한다.
- 최소 금액 미만이면 order를 expire한다.
- local canonical customerKey와 callback customerKey를 비교한다.
- provider credential snapshot으로 secret key를 정한다.
- 주의: customerKey mismatch는 billingKey를 다른 사용자에 붙이는 공격/오류를 막는 핵심이다.

### L629-L647: billing key issue 실패 처리

- issue API를 호출한다.
- response uncertain이고 billingKey도 모르면 local order를 expire하고 Toss 콘솔 수동 확인 로그를 남긴다.
- billingKey가 있는데 validation 실패면 remote revoke를 시도한다.
- local order는 expire한다.
- 주의: billingKey를 모르는 경우 API로 삭제할 수 없기 때문에 운영 로그가 유일한 추적 단서다.

### L648-L662: billing key local 저장과 order attach

- billingKey를 암호화 저장하고 card metadata를 저장한다.
- 저장 실패 시 issued billingKey revoke를 시도하고 order를 expire한다.
- order에 billingKeyId를 attach한다.
- attach 실패 시 billingKey를 revoke하고 order를 expire한다.
- 주의: attach 전에 첫 결제를 진행하면 webhook/reconcile 시 어떤 key로 결제됐는지 추적하기 어렵다.

### L664-L687: 첫 결제와 subscription activation

- orderName을 Toss 제한에 맞게 만든다.
- billingKey로 첫 결제를 시도한다.
- charge pending이면 local order를 pending으로 두고 redirect한다.
- 명확히 실패하면 billingKey를 revoke하고 order를 expire한다.
- 성공 응답을 provider payload로 저장하고 `CompleteTossBillingOrder()`를 호출한다.
- activation 실패 시 reconciliation 로그를 남기고 billingKey revoke를 시도한다.
- 주의: 첫 결제 DONE 후 activation 실패는 돈은 결제됐지만 구독은 안 열린 상태라 운영 개입이 필요하다.

### L689-L699: billing auth fail callback

- tradeNo, code, message를 읽고 로그를 남긴다.
- local order를 expire한다.
- fail 정보를 topup 화면 query로 넘긴다.
- 주의: billing auth 단계 실패이므로 billingKey가 발급되지 않은 정상 취소/실패로 본다.

### L701-L709: 사용자 자동갱신 해지 endpoint

- user id를 읽는다.
- model에서 사용자의 active Toss auto-renew를 취소하고 billingKey revocation을 진행한다.
- 주의: controller는 HTTP 응답만 담당하고 실제 DB/remote cleanup은 model/service 쪽으로 위임한다.

## `model/toss_billing.go`

### L16-L28: `UserBillingKey`

- Toss billingKey 저장 row다.
- `CustomerKey`: billingKey와 매핑되는 Toss customerKey다.
- `EncryptedKey`: billingKey 원문 암호화 값이다.
- `BillingKeyHash`: webhook matching을 위한 HMAC이다.
- `ProviderCredential`: 해당 key에 사용할 secret snapshot이다.
- `Status`: active, pending_revocation, revoked다.
- 주의: billingKey 원문은 로그나 API 응답에 노출하지 않는다.

### L30-L40: 상태와 error 상수

- active/pending_revocation/revoked 상태 문자열을 정의한다.
- `TossBillingMaxFails`는 갱신 실패 허용 횟수다.
- already deleted와 charge pending sentinel error를 정의한다.
- renewal order prefix를 정의한다.

### L42-L60: billingKey hash와 provider credential 암복호화

- billingKey hash는 HMAC으로 만든다.
- provider credential은 공백이면 빈 값으로 두고, 있으면 프로젝트 공통 암호화로 저장한다.
- 주의: credential snapshot 복호화 실패 시 API 호출은 active key fallback으로 갈 수 있지만, 운영 로그를 확인해야 한다.

### L62-L75: Toss 금액 변환과 최소 금액

- `TossPlanKRW()`는 subscription price를 KRW 정수로 바꾼다.
- `IsTossCardAmountPayableKRW()`는 카드 최소 금액 이상인지 확인한다.
- 주의: controller와 cron이 모두 이 함수를 사용하므로 단일 source of truth다.

### L77-L126: orderName과 renewal tradeNo helper

- `TossSubscriptionOrderName()`은 plan title에 "구독" 또는 "구독 갱신" suffix를 붙인다.
- rune 단위로 잘라 한글 title도 길이 제한을 지킨다.
- `ParseTossRenewalTradeNo()`는 deterministic renewal order id에서 sub id와 next billing time을 복원한다.
- 주의: renewal tradeNo 포맷을 바꾸면 pending reconcile과 테스트를 같이 바꿔야 한다.

### L128-L145: 다음 billing time 계산

- 구독 종료 전에 갱신되도록 lead time을 둔다.
- lead는 period/4 또는 1시간 중 작은 값이다.
- start/end boundary를 벗어나지 않게 보정한다.
- 주의: 너무 늦으면 expire task가 먼저 구독을 만료시킬 수 있다.

### L147-L178: model/controller dependency inversion

- model은 Toss HTTP 호출 결과 DTO와 charger/revoker interface만 안다.
- 실제 HTTP 구현은 controller init에서 주입된다.
- 주의: controller import cycle을 만들지 않는다.

### L180-L278: `ProcessTossRenewal`

- charger가 없으면 error다.
- subscription row를 읽는다.
- 결제 직전 autoRenew/status를 다시 확인한다.
- plan을 읽고 KRW charge를 계산한다.
- deterministic tradeNo를 만든다.
- 기존 pending/success renewal order가 있으면 상태와 snapshot amount를 재사용한다.
- 최소 금액 미만이면 failure count를 올리고 필요 시 billingKey를 revoke한다.
- pending renewal order를 먼저 준비한다.
- billingKey/customerKey/secret snapshot을 복호화한다.
- Toss billing charge를 호출한다.
- charge pending은 실패 count를 올리지 않는다.
- 명확한 실패는 failure count를 올리고 max 초과 시 revoke한다.
- 성공하면 `RenewTossSubscription()`으로 기간을 연장한다.
- 주의: remote charge 전에 audit order를 만들어야 응답 유실/재시도 시 중복 청구를 막을 수 있다.

### L280-L373: `PrepareTossRenewalOrder`

- subId와 tradeNo를 검증한다.
- subscription row를 lock한다.
- autoRenew/status/billingKeyId를 다시 확인한다.
- billing key의 provider credential을 읽는다.
- 기존 order가 있으면 provider/method/status를 확인하고 누락 snapshot만 보강한다.
- 새 order면 pending `SubscriptionOrder`를 만든다.
- `ProviderAmount`, `ProviderCurrency`, `ProviderCredential`, `BillingKeyId`를 snapshot으로 저장한다.
- 주의: 가격 변경 후에도 기존 pending order의 amount를 덮어쓰지 않는 것이 중요하다.

### L375-L420: billingKey 저장

- public helper는 active 저장과 pending_revocation 저장으로 나뉜다.
- billingKey 공백은 거부한다.
- billingKey 원문을 암호화한다.
- secret key snapshot도 암호화한다.
- billingKey HMAC hash를 저장한다.
- status와 card metadata를 함께 저장한다.
- 주의: billingKey hash는 webhook matching 성능과 안전성을 위해 필요하다.

### L422-L458: billingKey 복호화 조회

- active row만 복호화해 반환한다.
- encrypted billingKey와 provider credential을 각각 복호화한다.
- status가 active가 아니면 "billing key revoked" error다.
- 주의: pending_revocation key로 charge하면 안 된다.

### L460-L487: local revoke와 pending revocation

- `RevokeTossBillingKey()`는 billing key row를 revoked로 바꾸고 autoRenew subscription을 끈다.
- `MarkTossBillingKeyPendingRevocation()`은 remote 삭제 재시도를 위해 key를 보존하면서 future charge에서 제외한다.
- 주의: remote 삭제 실패 전에는 pending_revocation으로 둔다.

### L489-L539: pending revocation retry

- remote delete를 시도하고 already deleted는 성공처럼 처리한다.
- pending_revocation row를 batch로 읽는다.
- encrypted key와 credential을 복호화한다.
- remote delete 성공 후 local revoked로 바꾼다.
- 실패한 row는 로그만 남기고 다음 row를 처리한다.
- 주의: 이 task가 멈추면 Toss에 불필요한 billingKey가 남을 수 있다.

### L541-L587: BILLING_DELETED webhook matching

- plain billingKey가 비어 있으면 no-op이다.
- hash가 있는 row는 HMAC hash로 먼저 찾는다.
- customerKey가 있으면 함께 필터한다.
- hash 없는 과거 row는 decrypt해서 plain 비교한다.
- 매칭되면 local revoke한다.
- 주의: webhook으로 온 billingKey를 그대로 저장하거나 로그에 남기지 않는다.

### L589-L622: billingKey hash backfill

- hash가 비어 있는 기존 row를 id 순서로 batch 처리한다.
- encrypted key를 decrypt하고 HMAC hash를 채운다.
- decrypt 실패는 시스템 로그로 남기고 계속 진행한다.
- 주의: migration 중 일부 row가 실패할 수 있으므로 운영 로그를 확인해야 한다.

## `model/subscription.go`

### L42-L48: subscription order reconciler injection

- stale subscription order reconcile은 controller의 Toss API 조회가 필요하다.
- model은 callback function만 보관한다.
- 주의: top-up reconciler와 같은 import cycle 회피 패턴이다.

### L205-L224: `SubscriptionOrder` Toss 관련 필드

- `ProviderPayload`: Toss Payment 객체 audit JSON이다.
- `ProviderAmount`: KRW 청구 금액 snapshot이다.
- `ProviderCurrency`: 현재 KRW다.
- `ProviderCredential`: billing secret snapshot이다.
- `BillingKeyId`: 사용한 billing key row id다.
- 주의: first purchase와 renewal audit 모두 이 모델을 사용한다.

### L248-L276: billing key attach

- tradeNo와 billingKeyId를 검증한다.
- order row를 lock한다.
- provider가 Toss인지 확인한다.
- 이미 success면 idempotent하게 끝낸다.
- pending 상태에서만 billingKeyId를 저장한다.
- 주의: billingKeyId attach 전 첫 결제를 진행하면 webhook/reconcile이 billing key를 찾지 못한다.

### L712-L743: 첫 결제 pending order expire와 billingKey pending revoke

- pending Toss subscription order를 expired로 바꾼다.
- BillingKeyId가 있으면 billingKey row를 pending_revocation으로 바꾼다.
- 주의: 첫 결제/발급 중간 실패 후 remote billingKey가 남는 케이스를 추적하기 위한 함수다.

### L745-L778: renewal pending order expire와 failure count

- renewal order를 expired로 바꾼다.
- subscription billing failure count를 올린다.
- 주의: renewal 실패는 기존 구독 상태와 자동갱신 상태에도 영향을 준다.

### L780-L812: stale pending subscription reconcile row selection

- reconciler가 없으면 no-op이다.
- cutoff 이전이고 billingKeyId가 있는 pending order만 조회한다.
- 각 row를 injected reconciler에 넘긴다.
- 주의: billingKeyId 없는 pending order는 아직 billing key 발급 전이라 별도 expire 함수가 처리한다.

### L814-L861: billingKey 없는 stale pending subscription expire

- billingKeyId가 0/null인 오래된 Toss pending subscription order를 lock해서 가져온다.
- order ids와 key ids를 모은다.
- pending order를 expired로 업데이트한다.
- key ids가 있다면 pending_revocation으로 바꾼다.
- 주의: query 조건상 billingKeyId 없는 주문이 대상이지만, defensive key handling이 남아 있다.

### L1603-L1680: `CompleteTossBillingOrder`

- pending Toss subscription order를 lock한다.
- provider/status/billingKeyId를 검증한다.
- plan snapshot으로 `UserSubscription`을 만든다.
- autoRenew를 켜고 billingKeyId/nextBillingTime/fail count를 설정한다.
- provider payload와 complete time을 저장한다.
- top-up log/audit row를 맞춘다.
- 주의: first charge DONE 후 호출되는 activation 함수다. 실패 시 controller가 reconciliation required로 처리한다.

### L1682-L1788: `RenewTossSubscription`

- subscription과 renewal order를 transaction에서 lock한다.
- order가 이미 success면 idempotent하게 끝낸다.
- provider/status/amount/currency를 확인한다.
- subscription 기간과 reset time을 다음 주기로 연장한다.
- failure count를 reset한다.
- renewal order를 success로 저장하고 provider payload를 남긴다.
- 주의: orderId가 deterministic이라 같은 cycle 재시도도 이 함수에서 idempotent하게 정리된다.

### L1790-L1821: `MarkTossBillingFailure`

- billing fail count를 1 증가시킨다.
- max fail 이상이면 autoRenew를 끄고 billing key를 pending_revocation으로 바꾼다.
- 주의: pending charge는 failure로 세지 않는다. 명확한 실패만 이 함수로 온다.

### L1823-L1830: due renewal 조회

- active, autoRenew, billingKeyId가 있는 subscription 중 nextBillingTime이 지난 row를 조회한다.
- limit을 둔다.
- 주의: service task에서 master node만 실행하므로 여기서는 distributed lock을 하지 않는다.

### L1832-L1889: 사용자 Toss 자동갱신 취소

- 사용자의 active autoRenew Toss subscription을 찾는다.
- subscription을 autoRenew=false로 바꾼다.
- billing key를 pending_revocation으로 표시한다.
- transaction 후 remote revoke 후보를 실제 Toss 삭제 API로 보낸다.
- 주의: DB 상태 변경과 remote 삭제는 한 transaction으로 묶을 수 없으므로 pending_revocation 상태가 필요하다.

## `service/toss_pending_cleanup_task.go`

### L17-L23: cleanup 주기와 cutoff

- 10분마다 실행한다.
- paymentKey 없는 pending은 50분 후 expire한다.
- paymentKey 있는 pending은 15분 후 reconcile한다.
- subscription pending은 50분 후 정리한다.
- 주의: Toss 결제창/승인 유효시간을 고려한 값이다. 너무 줄이면 정상 흐름을 닫는다.

### L30-L46: task start

- master node에서만 goroutine을 띄운다.
- 시작 시 한 번 실행하고 이후 ticker로 반복한다.
- 주의: 여러 노드가 동시에 cleanup하면 같은 주문 처리 경쟁이 늘어난다.

### L48-L87: cleanup 한 사이클

- atomic flag로 중복 실행을 막는다.
- paymentKey 없는 top-up을 expire한다.
- paymentKey 있는 top-up을 reconcile한다.
- billingKeyId 있는 subscription order를 reconcile한다.
- billingKeyId 없는 stale subscription order를 expire한다.
- 주의: 순서가 중요하다. paymentKey/billingKey가 있는 주문은 단순 expire보다 authoritative 조회가 먼저다.

## `service/toss_billing_task.go`

### L20-L28: billing task 주기와 running flag

- 1분마다 갱신 대상 subscription을 확인한다.
- atomic flag로 한 process 내 중복 실행을 막는다.

### L30-L46: task start

- master node에서만 실행한다.
- 시작 즉시 한 번 실행한 뒤 ticker로 반복한다.
- 주의: billing charge는 돈이 움직이는 작업이라 multi-node 중복 실행을 피해야 한다.

### L48-L88: billing task 한 사이클

- compliance가 안 되어 remote cleanup을 못 하면 local overdue expire만 수행한다.
- remote cleanup이 가능하면 pending_revocation billing key 삭제를 먼저 재시도한다.
- billing runnable이 아니면 due subscription을 expire한다.
- runnable이면 due renewals를 조회하고 하나씩 charge한다.
- 주의: key 설정이 깨진 상태에서 계속 active subscription만 유지하면 사용자에게 잘못된 이용 가능 상태가 보일 수 있다.

### L90-L101: runnable 조건

- payment compliance가 필요하다.
- `TossBillingEnabled`가 true여야 한다.
- `TossUnitPrice > 0`이어야 한다.
- 명시적 billing client/secret key가 있어야 한다.
- 주의: 일반 Toss key fallback을 사용하지 않는 점이 중요하다.

### L103-L109: renewal 처리 위임

- service는 subscription row를 넘기고 model `ProcessTossRenewal()`에 위임한다.
- 오류는 warn log로 남긴다.
- 주의: 실제 idempotency와 billing charge는 model/controller injection 경로에 있다.

## 프론트: 일반 Toss 결제

### `web/default/src/features/wallet/api.ts` L186-L208

- `calculateTossAmount()`는 `/api/user/toss/amount`를 호출한다.
- `requestTossPayment()`는 `/api/user/toss/pay`를 호출한다.
- `skipBusinessError`를 사용해 business error를 UI에서 직접 표시할 수 있게 한다.
- 주의: amount preview와 payment session 생성은 별도 API다.

### `web/default/src/features/wallet/hooks/use-toss-payment.ts` L29-L40

- `processTossPayment()`는 UI 입력 amount를 floor 처리한다.
- 백엔드에 Toss payment session 생성을 요청한다.
- API 실패면 toast를 띄우고 false를 반환한다.
- 주의: 프론트 amount는 UX용 입력일 뿐, 서버가 다시 계산한다.

### `web/default/src/features/wallet/hooks/use-toss-payment.ts` L44-L64

- 백엔드 응답에서 client key, customer key, order id, amount, callback URL을 꺼낸다.
- `loadTossPayments(client_key)`로 SDK를 로드한다.
- `payment({ customerKey })`를 만든다.
- `requestPayment()`를 카드/KRW/orderId/orderName/successUrl/failUrl로 호출한다.
- 공식 근거: Toss 결제창 연동 문서의 SDK 호출 단계다.
- 주의: `method: 'CARD'`가 서버의 card 검증과 맞물려 있다.

### `web/default/src/features/wallet/hooks/use-toss-payment.ts` L67-L76

- SDK reject 중 `PAY_PROCESS_CANCELED`는 사용자 취소로 보고 조용히 false를 반환한다.
- 다른 error는 일반 결제 실패 toast를 보여준다.
- finally에서 processing state를 내린다.

## 프론트: 조직 지갑 Toss 결제

### `web/default/src/features/organizations/lib/organization-wallet-payment.ts` L1-L23

- payment type을 stripe/paypal/waffo_pancake/toss/generic flow로 분류한다.
- `isTossPayment()`가 true면 Toss SDK flow로 보낸다.
- 주의: Toss는 generic redirect/pay link flow가 아니라 SDK flow다.

### `web/default/src/features/organizations/api.ts` L208-L254

- 조직 Toss amount preview와 payment session API를 제공한다.
- endpoint는 `/api/organization/toss/amount`, `/api/organization/toss/pay`다.
- 주의: 개인 wallet API와 path가 다르지만 응답 DTO는 TossPaymentResponse를 공유한다.

### `web/default/src/features/organizations/components/organization-wallet.tsx` L240-L287

- payment type에 따라 backend API를 분기한다.
- flow가 `toss`면 Toss payment session 응답을 받는다.
- `loadTossPayments()`와 `payment({ customerKey })`를 호출한다.
- `requestPayment()`를 카드/KRW로 호출한다.
- Toss SDK 호출 후 dialog를 닫고 return한다.
- 주의: Toss는 `window.location.href`나 `pay_link`를 쓰지 않는다.

## 프론트: Toss 자동결제

### `web/default/src/features/subscriptions/api.ts` L139-L157

- `paySubscriptionToss()`는 `/api/subscription/toss/pay`를 호출한다.
- 응답 data는 client key, customer key, trade no, success URL, fail URL을 가진다.
- `cancelTossAutoRenew()`는 `/api/subscription/toss/cancel`을 호출한다.
- 주의: billing auth success에서 authKey는 URL query로 서버 callback에 직접 들어간다. 프론트 API 응답에는 없다.

### `web/default/src/features/subscriptions/hooks/use-toss-billing.ts` L29-L45

- backend에서 billing auth session 정보를 받는다.
- 기존 subscription payment response shape를 고려해 `success === true` 또는 `message === 'success'`를 허용한다.
- 응답이 없으면 toast 후 false다.

### `web/default/src/features/subscriptions/hooks/use-toss-billing.ts` L47-L58

- client key, customer key, success/fail URL을 검증한다.
- Toss SDK를 로드한다.
- `payment({ customerKey })`를 만든다.
- `requestBillingAuth({ method: 'CARD', successUrl, failUrl })`를 호출한다.
- 공식 근거: 자동결제 연동 문서의 billing auth 결제창 단계다.
- 주의: 여기서 billingKey가 발급되는 것이 아니다. billingKey 발급은 서버 callback에서 한다.

### `web/default/src/features/subscriptions/hooks/use-toss-billing.ts` L61-L74

- `PAY_PROCESS_CANCELED`와 `USER_CANCEL`은 사용자 취소로 조용히 처리한다.
- 다른 SDK error는 일반 결제 실패 toast를 보여준다.
- finally에서 processing state를 내린다.

### `web/default/src/features/subscriptions/lib/format.ts` L23-L24

- 프론트 카드 최소 금액 상수다.
- backend `TossCardMinimumAmountKRW`와 의미가 같다.

### `web/default/src/features/subscriptions/lib/format.ts` L70-L85

- subscription plan price와 Toss unit price로 KRW 청구 금액을 계산한다.
- invalid/0 이하 값은 0을 반환한다.
- `Math.round()`로 정수 KRW를 만든다.
- 주의: 실제 청구 금액 source of truth는 서버다. 이 함수는 UI 표시/버튼 상태용이다.

### `web/default/src/features/subscriptions/lib/format.ts` L87-L100

- 계산된 KRW를 locale currency string으로 표시한다.
- 0 이하이면 빈 문자열을 반환한다.

### `web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx` L102-L142

- 각 결제수단 활성화 여부를 계산한다.
- Toss는 `enableToss`만 보면 된다. plan별 Toss product id가 없다.
- Toss KRW 표시 금액과 최소 금액 미만 여부를 계산한다.
- 주의: Toss 자동결제는 plan price를 TossUnitPrice로 환산해 청구한다.

### `subscription-purchase-dialog.tsx` L280-L284

- Toss 버튼 클릭 시 `subscribeWithToss(plan.id)`를 호출한다.
- SDK가 success URL로 redirect하므로 dialog를 즉시 성공 처리하지 않는다.

### `subscription-purchase-dialog.tsx` L345-L352

- Toss charge 표시를 plan card 안에 보여준다.
- 주의: 이 표시 금액은 billing auth 세션 생성 전 사용자 안내용이다.

### `subscription-purchase-dialog.tsx` L424-L438

- Toss Auto Pay 버튼을 렌더링한다.
- paying, tossBillingProcessing, limitReached, tossChargeUnavailable이면 disabled다.
- 주의: 최소 금액 미만을 프론트에서도 막아 사용자가 실패 흐름을 타지 않게 한다.

### `web/default/src/features/wallet/components/subscription-plans-card.tsx` L118-L123

- top-up info에서 Toss billing 활성화 여부와 unit price를 읽는다.
- 이 값이 purchase dialog로 전달된다.

### `subscription-plans-card.tsx` L192-L207

- Toss auto-renew cancel 버튼이 API를 호출한다.
- 성공하면 subscription 정보를 다시 fetch한다.
- 주의: remote billing key 삭제는 서버에서 pending_revocation/retry까지 처리한다.

### `subscription-plans-card.tsx` L663-L690

- `SubscriptionPurchaseDialog`에 enableToss, tossUnitPrice, userQuota, purchase limit 정보를 전달한다.
- 주의: dialog가 표시/disabled를 판단할 수 있게 source data를 빠짐없이 넘긴다.

## 프론트: system settings Toss 설정

### `payment-settings-section.tsx` L139-L151

- Toss 설정 form schema를 정의한다.
- 일반 결제 enabled, billing enabled, test mode, 일반 key, billing key, unit price, min top-up을 모두 검증한다.
- `TossUnitPrice`는 positive로 강제한다.
- 주의: backend도 unit price 0 이하를 막지만, UI에서도 먼저 막는다.

### `payment-settings-section.tsx` L443-L455

- 저장 payload에 Toss 설정 값을 넣는다.
- key 값은 trim한다.
- 주의: secret field는 빈 값이면 업데이트하지 않는 패턴이 뒤 diff builder에서 적용된다.

### `payment-settings-section.tsx` L678-L772

- initial 값과 sanitized 값을 비교해 변경된 Toss 설정만 update list에 넣는다.
- secret key류는 값이 있을 때만 업데이트한다.
- billing key도 일반 key와 별도로 비교한다.
- 주의: 빈 secret field를 저장해서 기존 secret을 지우지 않게 하는 UX/보안 패턴이다.

### `payment-settings-section.tsx` L1807-L1824

- Toss Gateway 섹션 제목과 설명을 표시한다.
- webhook URL `<ServerAddress>/api/toss/webhook`을 안내한다.
- Toss 콘솔에서 PAYMENT_STATUS_CHANGED와 BILLING_DELETED를 모든 MID에 등록하라고 안내한다.
- 주의: 일반 결제 MID와 billing MID가 다르면 둘 다 등록해야 한다.

### `payment-settings-section.tsx` L1833-L1889

- Toss 일반 결제 enabled, test mode, Toss Auto Pay enabled switch를 렌더링한다.
- Auto Pay는 billing MID 승인 후에만 켜라는 설명을 붙인다.
- 주의: 이 switch가 켜져도 backend의 explicit billing key check가 최종 방어선이다.

### `payment-settings-section.tsx` L1900-L1983

- 일반 Toss live/test client key와 secret key 입력 필드다.
- secret field는 password type이다.
- 설명은 "비워두면 업데이트하지 않음" 패턴을 따른다.

### `payment-settings-section.tsx` L1994-L2084

- billing live/test client key와 secret key 입력 필드다.
- recurring-billing MID용 키임을 설명한다.
- 주의: 일반 결제 key와 billing key를 같은 것으로 가정하지 않는다.

### `payment-settings-section.tsx` L2096-L2118

- Toss unit price와 min top-up 입력 필드다.
- 주의: unit price 변경은 새 결제/새 renewal부터 영향을 준다. 이미 생성된 pending order의 provider amount는 snapshot이다.

## 테스트 파일 라인별 읽기 포인트

### `controller/topup_toss_test.go`

- 일반 결제 세션 생성, ServerAddress 검증, customerKey 생성, paymentKey 선저장, confirm 실패 후 조회 복구, webhook 재조회 검증을 테스트한다.
- 주의: 새 상태값이나 결제수단을 추가하면 이 테스트에서 card-only assumption을 먼저 확인한다.

### `controller/subscription_payment_toss_test.go`

- billing auth 시작, customerKey mismatch, billing key issue 실패, 첫 결제 pending, billing key cleanup, activation 실패를 테스트한다.
- 주의: billingKey 발급 후 local 실패 케이스가 가장 중요하다.

### `model/toss_billing_test.go`

- deterministic renewal tradeNo, minimum KRW guard, orderName truncation, provider credential snapshot, pending order snapshot, pending charge handling, pending revocation retry를 테스트한다.
- 주의: renewal 중복 청구 방지 테스트를 깨뜨리면 운영 리스크가 크다.

### `model/toss_pending_cleanup_test.go`

- paymentKey 없는 pending expire와 paymentKey 있는 pending reconcile 분리를 테스트한다.
- 주의: `provider_order_id == trade_no` 의미를 바꾸면 이 테스트도 바뀌어야 한다.

### `service/toss_billing_task_test.go`

- billing task runnable 조건과 cleanup/renewal 위임을 테스트한다.
- 주의: compliance나 explicit billing key 조건을 완화하면 테스트 의도를 다시 확인해야 한다.

## 라인 단위 변경 시 최종 체크

1. `paymentKey`를 다루는 라인을 바꿨다면 confirm, webhook, cleanup 세 경로가 모두 같은 의미를 유지하는지 확인한다.
2. `Amount`, `Money`, `ProviderAmount`를 바꿨다면 과지급/미지급 테스트를 추가한다.
3. `customerKey`를 바꿨다면 일반 결제와 billing auth 모두에서 canonical key 검증을 확인한다.
4. billingKey 저장/삭제 라인을 바꿨다면 remote delete 실패 후 retry 가능성이 남아 있는지 확인한다.
5. Toss API 호출 라인을 바꿨다면 Basic auth, idempotency key, timeout, retry, common JSON wrapper를 확인한다.
6. frontend SDK 호출 라인을 바꿨다면 backend validation의 결제수단/card-only assumption과 맞는지 확인한다.
