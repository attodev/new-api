# Toss Payments 하드닝 논리 커밋 상세 — C01부터 C06

이 문서는 [커밋 인덱스](2026-07-12-toss-hardening-commit-index.md)의 첫 여섯 논리 커밋을 상세히 설명한다. C01–C03은 모든 Toss provider 요청의 기반이고, C04–C06은 일반 결제의 quote, confirm, 정산 lifecycle을 안전하게 만든다.

## C01. fix(toss): harden provider transport and request boundaries

### 목적

Toss API 요청이 애플리케이션 전역 HTTP 설정, 느슨한 TLS 옵션, redirect, 무제한 body, 짧은 잔여 context에 영향을 받아 금전 작업의 결과를 불명확하게 만드는 문제를 해결한다.

### 선행 커밋

없음. 이후 모든 money-moving Toss 커밋의 기반이다.

### 발견한 문제

1. 전역 HTTP transport의 insecure TLS 설정을 결제 API가 상속할 가능성이 있었다.
2. redirect를 자동으로 따라가면 Basic Authorization이 의도하지 않은 endpoint로 전달될 수 있었다.
3. 전역 timeout과 response-header timeout이 Toss 처리 요청의 정상 완료 전에 연결을 끊을 수 있었다.
4. POST를 시작할 때 context에 충분한 시간이 남아 있지 않으면 provider는 처리했지만 애플리케이션은 응답을 받지 못하는 ambiguous outcome이 늘어난다.
5. request·response body 상한이 없거나 trailing JSON을 허용하면 메모리 사용과 입력 해석이 예측 불가능해진다.
6. provider error body나 요청 URL을 그대로 wrapping하면 paymentKey, orderId, 메시지가 로그·사용자 응답에 노출될 수 있었다.
7. 409, 429, Retry-After, 5xx를 endpoint마다 다르게 처리하면 동일한 결제 attempt에 서로 다른 retry 정책이 적용된다.

### 발생 가능한 결과

- provider는 승인했는데 local에서는 timeout으로 실패 처리
- 짧은 retry가 새 요청을 만들어 중복 결제
- Authorization 또는 payment identifier 로그 노출
- oversized body를 통한 메모리 고갈
- redirect를 통한 credential 전달
- webhook 10초 budget과 일반 결제 60초 provider budget 충돌

### 원인

기존 HTTP helper가 일반 API client와 결제 처리 API의 요구를 완전히 분리하지 않았다. 결제 POST에는 “요청을 시작하기 전에 충분한 시간 확보”, “결과가 불명확하면 실패 확정 금지”, “인증 정보가 redirect되지 않음”이 함께 필요하다.

### 적용한 수정

- Toss 전용 secure transport를 둔다.
- TLS 최소 버전을 1.2로 고정한다.
- 전역 TLS_INSECURE_SKIP_VERIFY를 상속하지 않고 인증서 검증을 강제한다.
- redirect를 따라가지 않는다.
- Authorization은 시크릿 키 뒤 콜론을 붙인 값을 Base64로 인코딩한 Basic 형식으로 만든다.
- response body는 2MiB까지만 읽는다.
- 일반 결제 request body는 64KiB, webhook body는 1MiB로 제한한다.
- JSON 하나를 decode한 뒤 EOF까지 확인해 trailing object·garbage를 거부한다.
- money-moving POST는 full attempt budget을 확보하지 못하면 시작하지 않는다.
- 최대 retry 횟수는 3회로 제한한다.
- Retry-After는 최대 5초로 제한한다.
- 408, 409, 429, 5xx와 transport·body read 실패를 공통 ambiguous 분류로 보낸다.
- URL을 포함하지 않는 안전한 error wrapper를 사용한다.
- provider raw body는 사용자 응답에 전달하지 않는다.
- Toss Payment 응답의 문자열·카드 필드를 저장 전에 sanitization한다.

### 수정 뒤 불변조건

- Toss 요청은 항상 TLS 1.2 이상과 인증서 검증을 사용한다.
- Basic Authorization은 redirect되지 않는다.
- POST를 시작했다면 해당 attempt를 완료할 수 있는 최소 context budget이 있었다.
- body read·decode 실패는 terminal payment failure로 간주되지 않는다.
- 입력 하나를 완전히 소비하지 못하면 요청을 거부한다.
- provider raw error나 전체 결제 URL이 외부 응답에 노출되지 않는다.

### 주요 구현 파일

- controller/topup_toss.go
- controller/toss_request_body.go
- controller/subscription_payment_toss.go
- controller/wallet_auto_recharge.go
- model/toss_provider_payload_test.go
- model/main.go

### 관련 테스트

- controller/topup_toss_http_resilience_test.go
- controller/toss_request_body_test.go
- controller/topup_toss_ambiguous_response_test.go
- controller/topup_toss_ui_contract_test.go
- model/toss_provider_payload_test.go

검증해야 하는 대표 시나리오:

- TLS 최소 버전과 insecure global option 격리
- redirect 거부
- request body 상한과 trailing JSON 거부
- response body 상한
- Retry-After cap
- 짧은 context에서는 POST 시작 안 함
- 성공 HTTP의 malformed body를 ambiguous로 유지

### 남은 위험과 rollback 주의

- Toss endpoint가 새로운 long-running 동작을 추가하면 60초 minimum budget이 적절한지 공식 문서와 함께 다시 검토해야 한다.
- C05·C11·C13·C14는 C01의 ambiguous 분류에 의존한다. C01만 rollback하면 상위 flow가 transport error를 잘못 terminal로 닫을 수 있다.

---

## C02. fix(toss): make payment configuration atomic and encrypted

### 목적

Toss 설정을 부분 row의 집합이 아니라 하나의 검증된 결제 권한 세대로 관리하고, secret을 암호화하며, 손상된 설정을 all-disabled 상태에서만 복구하게 한다.

### 선행 커밋

C01

### 발견한 문제

1. client key와 secret key를 서로 다른 요청으로 저장하면 중간에 섞인 pair가 노출될 수 있었다.
2. 여러 API 노드가 stale in-memory option을 기준으로 검증하면 한 노드의 key rotation을 다른 노드가 부분 overwrite할 수 있었다.
3. test/live, normal/billing key가 섞이거나 widget key가 API 개별 연동 key로 들어갈 수 있었다.
4. secret option이 평문으로 DB에 저장되거나 기존 encryption key가 일시적이면 재시작 뒤 복구할 수 없었다.
5. 14개 중 일부 row가 누락된 restore를 legacy 정상 설정으로 오해하면 이전 process memory의 enabled·secret 값이 되살아날 수 있었다.
6. ciphertext 손상 시 일부 필드만 수정하는 일반 UI로는 안전한 복구가 불가능했다.
7. repair 직후 MID·renewal migration이 끝나기 전에 기능을 다시 켤 수 있었다.

### 결제 영향 설정 14개

1. TossEnabled
2. TossBillingEnabled
3. TossWalletAutoRechargeEnabled
4. TossTestMode
5. TossClientKey
6. TossSecretKey
7. TossTestClientKey
8. TossTestSecretKey
9. TossBillingClientKey
10. TossBillingSecretKey
11. TossBillingTestClientKey
12. TossBillingTestSecretKey
13. TossUnitPrice
14. TossMinTopUp

### 적용한 수정

- 모든 Toss option write를 전용 bulk endpoint와 DB write-lock row로 직렬화한다.
- 일반 UpdateOption·UpdateOptionsBulk는 Toss option과 internal revision key를 거부한다.
- client·secret pair는 반드시 같은 요청에서 함께 수정한다.
- 빈 pair는 양쪽이 모두 비어야 한다.
- test_ck/test_sk, live_ck/live_sk 환경과 역할을 검증한다.
- 일반 결제와 billing의 active pair를 따로 검증한다.
- wallet auto-recharge는 billing이 켜져 있을 때만 활성화할 수 있다.
- secret 네 개는 AES-GCM enc:v1 envelope로 저장한다.
- option key를 AEAD associated data로 사용해 ciphertext slot 교환을 막는다.
- 14개 값 전체의 digest를 HMAC 기반 revision attestation에 묶는다.
- 정상 generation은 toss:v2, repair가 필요한 generation은 quarantine 상태로 구분한다.
- DB revision, row digest, runtime snapshot이 일치하지 않으면 신규 POST를 막는다.
- TOSS_OPTION_SECRET_ENCRYPTION_ENABLED를 fleet rolling gate로 둔다.
- 손상 상태에서는 secret이나 digest를 노출하지 않고 repairRequired, maintenanceRequired, HMAC repair token만 관리자에게 반환한다.
- repair는 정확한 token과 14개 complete set을 요구한다.
- repair 시 세 payment mode는 모두 false여야 한다.
- repair generation과 maintenance fence를 같은 transaction에 저장한다.
- maintenance retry는 secret을 다시 받지 않는다.
- maintenance 중에는 enable, key 변경, plan 변경, 신규 provider POST를 차단한다.
- false-only emergency disable은 허용한다.
- startup은 Toss를 fail-closed하되 전체 서비스는 시작할 수 있게 한다.

### 수정 뒤 불변조건

- 어느 시점에도 부분 client·secret pair가 active 설정이 될 수 없다.
- DB에 비어 있거나 손상된 row가 process memory의 이전 secret을 되살리지 않는다.
- secret ciphertext는 다른 Toss secret slot으로 이동할 수 없다.
- repair는 all-disabled complete set으로만 가능하다.
- maintenance가 끝나기 전 신규 결제·billing·wallet charge가 불가능하다.
- 관리자 API는 secret, client key, digest를 repair token에 포함해 노출하지 않는다.

### 주요 구현 파일

- setting/payment_toss.go
- model/option.go
- model/toss_option_secret.go
- controller/option.go
- controller/payment_webhook_availability.go
- main.go
- web/default/src/features/system-settings/integrations/payment-settings-section.tsx
- web/default/src/features/system-settings/integrations/toss-option-updates.ts
- web/default/src/features/system-settings/api.ts
- web/default/src/features/system-settings/types.ts

### 관련 테스트

- setting/payment_toss_test.go
- model/option_toss_atomic_test.go
- model/toss_option_secret_test.go
- controller/option_toss_test.go
- controller/toss_fresh_checkout_gate_test.go
- controller/toss_provider_post_operational_gate_test.go
- web/default/src/features/system-settings/integrations/toss-option-updates.test.ts

대표 검증:

- stale node의 partial pair update 거부
- corrupt ciphertext complete repair
- stale repair token replay 거부
- 누락 row가 stale runtime snapshot을 초기화
- maintenance 중 enable·plan mutation·POST 차단
- Option table이 사라진 경우 observed revision이 있으면 fail-closed
- repair retry가 secret을 재전송하지 않음

### 배포·rollback 주의

- 모든 노드가 enc:v1 dual-reader를 이해하기 전에 encryption gate를 켜면 안 된다.
- CRYPTO_SECRET 또는 기존 compatible SESSION_SECRET은 모든 노드에 동일하고 영구적이어야 한다.
- enc:v1 row가 저장된 뒤 구버전 binary로 rollback하면 안 된다.
- repair를 위해 DB option row를 수동 삭제하거나 일부만 수정하면 attestation이 깨지고 신규 POST가 계속 차단된다.

---

## C03. fix(toss): pin MID namespace across credential rotation

### 목적

Toss의 멱등성이 API 키에 scope된다는 사실을 모든 일반 결제, billing, renewal, wallet, refund 복구 경로에 반영한다.

### 선행 커밋

C01, C02

### 발견한 문제

같은 orderId와 Idempotency-Key를 사용해도 다른 API key로 요청하면 Toss는 같은 멱등 요청으로 보장하지 않는다. 주문 생성 뒤 관리자가 secret을 교체했을 때 recovery가 현재 active secret으로 POST하면 다음 문제가 생길 수 있었다.

- 과거 confirm의 두 번째 승인 namespace 생성
- renewal 또는 wallet charge의 중복 청구
- 다른 MID의 payment를 잘못 조회·정산
- orphan billing key를 엉뚱한 MID에서 삭제 시도
- key rotation 직전·직후 worker가 서로 다른 credential로 같은 attempt 실행

### 적용한 수정

- 신규 결제 관련 row에 주문 시점 exact secret을 암호화 저장한다.
- client key의 SHA-256 fingerprint를 provider_client_key_hash로 저장한다.
- fingerprint 형식이 올바른 경우에만 같은 MID namespace 증거로 인정한다.
- legacy row는 exact encrypted secret이 일치할 때만 source namespace를 증명한다.
- 일반 confirm POST는 저장된 exact credential만 사용한다.
- 현재 active same-fingerprint credential은 일반 결제 GET lookup에만 fallback한다.
- billing ISSUE·첫 charge·renewal·wallet charge에서 기존 secret이 provider에 의해 명확히 거절된 경우에만 credential promotion을 허용한다.
- promotion은 row lock과 CAS로 exact old ciphertext를 확인한다.
- promotion 뒤 attempted=false로 되돌려 다음 POST가 final operational gate를 다시 통과하게 한다.
- refund는 balance가 단조 감소하는 별도 operation이므로 명확한 auth 거절 뒤 same-MID current secret을 사용할 수 있지만 refund key와 balance는 그대로 유지한다.
- key·test mode·credential namespace 변경 전에 legacy fingerprint를 backfill한다.
- billing key, top-up, subscription order, wallet policy 네 테이블을 대상으로 한다.
- max-ID cutoff와 primary-key window를 사용해 큰 테이블 scan을 제한한다.
- SQL predicate는 PostgreSQL, MySQL, SQLite별로 최적화하되 Go exact validation을 최종 경계로 둔다.
- write batch마다 option revision을 다시 확인한다.
- decrypt 불가, empty secret, ambiguous mapping은 추론하지 않고 exact credential-only row로 남긴다.

### 수정 뒤 불변조건

- provider POST가 어떤 secret namespace에서 처음 시도됐는지 DB가 증명한다.
- ambiguous attempt는 다른 API key namespace로 조용히 넘어가지 않는다.
- different MID fingerprint의 current key는 recovery candidate가 되지 않는다.
- credential promotion 자체는 결제를 만들지 않으며, 다음 POST는 새 final gate를 통과해야 한다.
- legacy backfill이 증명할 수 없는 row는 availability보다 isolation을 우선한다.

### 주요 구현 파일

- model/toss_client_fingerprint.go
- model/toss_billing.go
- model/topup.go
- model/wallet_auto_recharge.go
- model/option.go
- controller/topup_toss.go
- controller/subscription_payment_toss.go
- controller/wallet_auto_recharge.go

### 관련 테스트

- model/toss_attempt_credential_pin_test.go
- model/toss_billing_issue_rotation_test.go
- model/wallet_auto_recharge_issue_rotation_test.go
- controller/toss_billing_issue_rotation_test.go
- controller/topup_toss_mid_test.go
- model/option_toss_atomic_test.go

대표 검증:

- different MID current key가 후보에 들어가지 않음
- general confirm이 fallback key로 POST하지 않음
- definitive auth rejection 뒤 same-MID promotion
- promotion과 disable race에서 다음 POST 차단
- legacy 네 테이블 backfill
- migration batch 사이 revision 변경 시 stale rotation 거부
- corrupt fingerprint를 valid evidence로 취급하지 않음

### 남은 위험과 운영 주의

- client key fingerprint는 provider의 실제 MID 조회 API가 아니라 저장된 key namespace의 보수적 proxy다. client key가 바뀌면 같은 상점이라고 운영자가 알고 있어도 자동으로 같은 namespace라 가정하지 않는다.
- exact old secret을 잃었고 fingerprint도 없으면 자동 recovery보다 운영 대사를 선택한다.
- C03의 schema·backfill을 적용한 뒤 구버전 worker를 다시 실행하면 안 된다.

---

## C04. fix(toss): persist immutable top-up quote and checkout identity

### 목적

사용자가 확인한 KRW 청구액과 실제 지급 quota를 서버가 한 번 계산해 영속화하고, 결제 시점 이후 가격·할인·단가 변경이 과거 주문 정산을 바꾸지 못하게 한다.

### 선행 커밋

C01, C02, C03

### 발견한 문제

1. TopUp.Amount는 Toss KRW이고 내부 지급값은 Money 또는 Quota인데, 일부 경로가 둘을 같은 단위로 취급할 수 있었다.
2. 과거 주문 정산 때 현재 TossUnitPrice 또는 현재 QuotaPerUnit을 다시 읽으면 지급량이 달라질 수 있었다.
3. 프론트가 표시한 quote 없이 결제 confirm dialog를 열거나 표시한 값과 실제 서버 session 값이 달라질 수 있었다.
4. 사용자가 보낸 amount, mode, discount 결과를 서버가 그대로 믿을 여지가 있었다.
5. 일반 CARD 결제창에서 간편결제 연결계좌가 선택될 수 있는데 카드 최소 100원만 적용하면 provider의 계좌 최소 200원과 충돌할 수 있었다.
6. organization target과 personal target이 callback에서 뒤섞이면 잘못된 wallet에 지급할 수 있었다.

### 적용한 수정

- amount preview와 checkout 생성 모두 서버가 group ratio, discount, amount mode, TossUnitPrice를 다시 계산한다.
- checkout 생성 시 KRW Amount, legacy Money, immutable Quota를 함께 저장한다.
- target_type과 target_id를 저장하고 personal·organization endpoint가 명시적으로 target을 설정한다.
- stable customerKey를 서버에서 준비하고 개인정보·예측 가능한 raw user id를 사용하지 않는다.
- orderId, orderName, client key, customerKey, amount, currency, successUrl, failUrl을 server session으로 반환한다.
- ServerAddress의 scheme, host, callback origin·path를 검증한다.
- 새 browser flow 시작 전에 fresh DB Toss config와 compliance를 다시 검사한다.
- 신규 일반 결제는 보수적으로 200원 floor를 사용한다.
- billing CARD-only는 100원 floor를 유지한다.
- 최대 charge는 signed 32-bit 범위로 제한해 quota 변환 overflow를 막는다.
- CreditedQuotaForTossTopUp은 다음 우선순위를 사용한다.
  1. nonzero Quota snapshot
  2. Quota가 0이고 nonzero Money인 legacy snapshot
  3. 둘 다 없는 매우 오래된 row의 제한적 Amount fallback
- nonzero Quota 또는 Money가 invalid하면 다음 source로 fallback하지 않고 fail-closed한다.
- 프론트는 quote 없이 confirmation을 진행하지 않고 server session과 표시 snapshot을 비교한다.

### 수정 뒤 불변조건

- Toss에 청구한 KRW와 local quota는 다른 필드와 다른 단위다.
- 신규 주문의 local 지급량은 주문 생성 뒤 단가 변경에 영향받지 않는다.
- invalid nonzero snapshot은 현재 설정으로 재해석되지 않는다.
- organization 주문은 정산 시에도 동일 organization owner·target을 증명해야 한다.
- 프론트 표시값은 서버 quote의 복사본이며 권위값은 서버 session이다.

### 주요 구현 파일

- setting/payment_toss.go
- controller/topup.go
- controller/topup_toss.go
- model/topup.go
- web/default/src/features/wallet/lib/payment.ts
- web/default/src/features/wallet/lib/topup-amount-mode.ts
- web/default/src/features/wallet/hooks/use-payment.ts
- web/default/src/features/organizations/components/organization-wallet.tsx

### 관련 테스트

- model/topup_toss_test.go
- controller/topup_toss_ui_contract_test.go
- controller/toss_fresh_checkout_gate_test.go
- controller/topup_toss_test.go
- web/default/src/features/wallet/lib/payment.toss.test.ts
- web/default/src/features/wallet/lib/topup-amount-mode.test.ts
- web/default/src/features/organizations/components/organization-wallet.test.ts

대표 검증:

- UnitPrice 변경 뒤에도 Quota snapshot 유지
- invalid Quota가 Money·Amount fallback을 사용하지 않음
- legacy Money snapshot 유지
- 200원 일반 결제 floor와 100원 billing floor
- quote fingerprint mismatch 시 SDK 호출 안 함
- organization target route와 personal target 분리

### 남은 위험

- Quota snapshot이 없고 Money만 있는 legacy row는 구매 당시 QuotaPerUnit까지 복원할 수 없다.
- Quota와 Money가 모두 없는 오래된 row는 운영 대사 대상이 될 수 있다.

---

## C05. fix(toss): scope confirm idempotency to the payment key

### 목적

브라우저 callback 위조, provider 응답 유실, API 처리 중 409, 승인 가능 시간·15일 멱등 보장 경계에서도 같은 결제를 중복 승인하지 않게 한다.

### 선행 커밋

C01, C02, C03, C04

### 발견한 문제

1. successUrl의 paymentKey, orderId, amount는 브라우저가 전달하므로 위조 가능하다.
2. orderId만으로 confirm Idempotency-Key를 만들면 공격자가 가짜 paymentKey로 실제 주문의 namespace를 먼저 소비할 수 있다.
3. confirm timeout·409·5xx 뒤 local failed로 닫으면 Toss에서는 DONE인데 quota가 지급되지 않을 수 있다.
4. 새 key·새 secret으로 재시도하면 중복 승인이 생길 수 있다.
5. 2xx이지만 body decode가 실패한 경우를 성공도 실패도 아닌 상태로 보존해야 한다.
6. 일반 결제 승인 유효시간이 지난 뒤 confirm POST를 새로 만들면 provider 계약과 어긋난다.
7. 15일 이후 같은 Idempotency-Key 결과를 provider가 보존한다고 가정할 수 없다.

### 적용한 수정

- 신규 checkout에 paymentKey-scoped protocol version을 저장한다.
- 신규 confirm Idempotency-Key는 orderId와 paymentKey에서 파생한다.
- callback paymentKey를 로컬 주문에 먼저 기록한다.
- paymentKey, exact credential, Idempotency-Key, attempted flag를 final barrier transaction에서 함께 고정한다.
- attempted row는 이 네 요소를 변경할 수 없다.
- legacy attempted row는 기존 orderId namespace를 계속 사용한다.
- provider POST 전 Payment GET을 먼저 수행할 수 있는 recovery flow를 둔다.
- confirm error 뒤 authoritative GET으로 DONE, terminal, unknown을 구분한다.
- timeout, 408, 409, 429, 5xx, body read 실패, malformed 2xx, unknown new 4xx를 ambiguous로 유지한다.
- 명확한 provider rejection과 GET 404가 함께 확인될 때만 forged binding을 안전하게 해제한다.
- identity가 다르거나 결과가 불명확하면 binding을 보존한다.
- local approval window가 지나면 GET-only로 처리한다.
- 15일 이후 unresolved attempt는 새 confirm POST를 만들지 않고 Transaction API·운영 대사로 넘긴다.
- browser request cancellation과 provider operation context를 분리하되 hard timeout은 유지한다.

### 수정 뒤 불변조건

- 하나의 신규 일반 결제는 orderId와 paymentKey가 결합된 하나의 멱등 namespace를 가진다.
- 한번 provider에 시도된 namespace는 ambiguous 결과 때문에 변경되지 않는다.
- confirm HTTP 실패는 곧 payment failure가 아니다.
- quota 지급은 authoritative DONE과 전체 identity 일치 뒤에만 가능하다.
- 승인시간·15일 경계를 넘으면 availability보다 중복 방지를 우선한다.

### 주요 구현 파일

- model/topup.go
- controller/topup_toss.go
- service/toss_pending_cleanup_task.go
- model/toss_billing.go의 공통 idempotency retention 상수

### 관련 테스트

- model/toss_topup_rolling_test.go
- controller/topup_toss_confirm_retry_test.go
- controller/topup_toss_reconciliation_retry_test.go
- controller/topup_toss_ambiguous_response_test.go
- controller/topup_toss_http_resilience_test.go
- controller/topup_toss_test.go
- model/toss_pending_cleanup_test.go
- service/toss_pending_cleanup_task_test.go

대표 검증:

- forged paymentKey가 실제 paymentKey namespace를 소비하지 못함
- terminal rejection + GET 404에서만 binding reset
- unreadable error body에서 binding 유지
- malformed 2xx + lookup miss에서 pending 유지
- 409 processing 뒤 동일 namespace 재확인
- local 10분 window 이후 confirm POST 없음
- 15일 이후 new key POST 없음
- exact credential conflict fail-closed

### rollback 주의

- paymentKey-scoped row가 저장된 뒤 구버전 confirm writer를 실행하면 안 된다.
- protocol gate를 끄더라도 이미 versioned row는 저장된 key를 유지해야 한다.
- C05를 rollback하려면 pending versioned order를 모두 대사하거나 새 writer를 완전히 중단해야 한다.

---

## C06. fix(toss): serialize settlement with account and organization lifecycle

### 목적

callback, webhook, cleanup, transaction scanner, 사용자·조직 삭제, membership 변경이 동시에 발생해도 결제당 local 권한을 한 번만 정확한 대상에 지급한다.

### 선행 커밋

C01–C05

### 발견한 문제

1. callback과 webhook이 동시에 DONE을 처리하면 quota가 두 번 지급될 수 있었다.
2. provider POST 직전에 사용자가 비활성화되거나 organization owner가 바뀔 수 있었다.
3. organization 결제 도중 owner가 organization을 떠나거나 organization이 disable·delete될 수 있었다.
4. 정산 transaction과 cancellation event insert의 lock 순서가 다르면 취소 뒤 지급 race가 생긴다.
5. 사용자 hard delete·organization delete가 unresolved provider activity를 제거하면 나중에 결제됐는지 추적할 수 없다.
6. provider paymentKey가 다른 주문으로 재결합되면 한 결제가 잘못된 target에 지급될 수 있다.

### 적용한 수정

- settlement에서 user 또는 organization owner를 먼저 잠그고 payment row를 잠그는 공통 lock order를 사용한다.
- TopUp status, paymentKey, target, quota update를 하나의 transaction과 CAS로 처리한다.
- 이미 success인 동일 evidence는 idempotent success로 처리하고 다른 evidence는 conflict로 막는다.
- provider POST 직전과 settlement 직전에 사용자 status를 다시 확인한다.
- personal target은 사용자가 organization membership에 들어가 personal wallet이 비활성화된 경우 차단한다.
- organization target은 organization enabled, owner_user_id, user membership, owner role을 재검증한다.
- organization owner·membership 변경은 active Toss provider attempt와 unresolved event를 고려한다.
- pristine unattempted order만 안전하게 만료하거나 취소할 수 있다.
- attempted·ambiguous order가 있으면 user·organization hard delete를 차단한다.
- service export·organization lifecycle 경로도 Toss 관련 상태를 보존한다.
- paymentKey unique association과 target_type·target_id 불변성을 확인한다.
- refund_required와 authoritative cancellation이 있으면 settlement를 중단한다.

### 수정 뒤 불변조건

- 같은 provider payment는 local wallet에 최대 한 번만 반영된다.
- 결제 대상은 checkout 생성 시 저장한 target에서 바뀌지 않는다.
- organization 지급은 정산 순간에도 동일 owner 관계가 유효해야 한다.
- attempted·unresolved payment evidence는 user·organization 삭제로 사라지지 않는다.
- cancellation이 먼저 직렬화되면 이후 fulfillment는 불가능하다.

### 주요 구현 파일

- model/toss_target_lifecycle.go
- model/topup.go
- model/organization.go
- model/organization_subscription.go
- model/user.go
- controller/organization.go
- controller/topup_toss.go
- service/org_user_export.go

### 관련 테스트

- model/toss_settlement_cas_test.go
- model/toss_topup_lifecycle_lock_test.go
- model/toss_target_lifecycle_test.go
- model/organization_toss_lifecycle_test.go
- controller/organization_test.go
- controller/topup_toss_test.go

대표 검증:

- concurrent settlement가 quota를 한 번만 지급
- paymentKey를 다른 order에 연결하지 못함
- organization owner 변경 뒤 provider POST 차단
- organization disable·delete와 settlement 경합
- attempted top-up이 있는 user·organization hard delete 차단
- personal wallet이 organization membership 중 비활성화
- cancellation event가 settlement보다 먼저 들어오면 지급 차단

### 남은 정책 영역

- 이미 지급된 quota가 사용된 뒤 cancellation이 발생한 경우 자동 회수하지 않는다. 이 처리는 C07의 reconciliation event와 운영 정책으로 넘긴다.
- organization owner가 바뀌어 지급할 수 없는 이미 결제된 건은 자동으로 새 owner에게 지급하지 않고 refund 또는 운영 대사를 선택한다.
