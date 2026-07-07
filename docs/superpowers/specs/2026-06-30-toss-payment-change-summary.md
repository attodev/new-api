# Toss Payments 연동 변경 정리

작성일: 2026-06-30

## 목적

이 문서는 현재까지 진행한 Toss Payments 연동 검토 및 수정 내용을 정리한다. 특히 공식 개발 문서의 결제창, 결제 승인, 웹훅, 자동결제 흐름과 비교하면서 발견했던 오류, 수정한 방식, 이후 작업 시 주의해야 할 점을 한곳에서 확인할 수 있도록 정리한다.

참고한 Toss Payments 공식 문서:

- 시작하기: https://docs.tosspayments.com/guides/v2/get-started
- 결제창 연동: https://docs.tosspayments.com/guides/v2/payment-window/integration
- 자동결제 연동: https://docs.tosspayments.com/guides/v2/billing/integration
- API 레퍼런스: https://docs.tosspayments.com/reference
- 웹훅: https://docs.tosspayments.com/guides/v2/webhook

## 전체 요약

초기 구현은 Toss 결제창, 결제 승인, 웹훅, 정기결제의 책임 경계가 충분히 분리되어 있지 않았다. 특히 브라우저 리다이렉트나 웹훅 payload를 그대로 신뢰할 수 있는 구조, 내부 금액 단위와 Toss 청구 금액 단위가 섞이는 구조, 결제 승인 실패와 실제 결제 상태가 어긋나는 구조가 문제였다.

현재 구조는 다음 원칙으로 교체되었다.

- 결제 생성은 반드시 서버의 pending 주문 생성에서 시작한다.
- 프론트엔드는 Toss SDK로 결제창 또는 빌링 인증창만 호출한다.
- 실제 충전, 구독 활성화, 갱신은 서버가 Toss API로 승인하거나 조회한 결과를 기준으로 처리한다.
- `orderId`, `paymentKey`, `amount`, `currency`, `method`, `customerKey`를 모두 서버에서 검증한다.
- 웹훅은 신호로만 사용하고, 최종 판단은 Toss API 재조회 결과로 한다.
- 정기결제는 빌링키 저장, 결제, 갱신, 해지, 원격 삭제 실패 재시도를 별도 상태로 관리한다.

## 현재 최종 결제 흐름

### 일반 사용자 충전

1. 사용자가 충전 금액을 선택한다.
2. 서버가 `POST /api/user/toss/pay`에서 pending top-up 주문을 생성한다.
3. 서버는 Toss `orderId`, 안정적인 `customerKey`, 주문명, 결제 금액, success URL, fail URL, client key를 반환한다.
4. 프론트엔드는 `loadTossPayments(clientKey)` 이후 `payment({ customerKey })`와 `requestPayment()`를 호출한다.
5. Toss 결제 성공 후 success URL로 `paymentKey`, `orderId`, `amount`가 전달된다.
6. 서버는 `GET /api/toss/confirm`에서 로컬 주문과 Toss callback 값을 먼저 대조한다.
7. 서버는 `paymentKey`를 로컬 주문에 먼저 저장한다.
8. 서버는 Toss `/v1/payments/confirm`을 호출한다.
9. 승인 응답 또는 재조회 응답에서 다음 조건을 모두 확인한다.
   - `status == DONE`
   - Toss `orderId`가 로컬 주문과 동일
   - Toss `totalAmount`가 로컬 주문의 KRW 금액과 동일
   - `currency == KRW`
   - 카드 결제 전용 흐름에서는 카드 결제 정보가 존재
10. 검증이 끝난 뒤에만 내부 충전 금액을 지급한다.

### 조직 지갑 충전

조직 지갑 충전도 사용자 충전과 동일한 Toss 승인 흐름을 사용한다. 차이는 충전 대상이 개인 지갑이 아니라 조직 지갑이라는 점이다.

주의할 점은 Toss가 결제하는 금액은 KRW 금액이고, 내부에서 지급하는 값은 조직 지갑의 quota 또는 money 값이라는 점이다. 두 값을 같은 필드처럼 취급하면 과지급 문제가 발생한다.

### 정기결제

1. 사용자가 구독 상품과 주기를 선택한다.
2. 서버가 `POST /api/subscription/toss/pay`에서 pending subscription order를 만든다.
3. 프론트엔드는 Toss SDK의 `requestBillingAuth()`로 카드 자동결제 인증을 시작한다.
4. success URL로 `authKey`, `customerKey`가 돌아온다.
5. 서버는 로컬 주문의 canonical `customerKey`와 callback의 `customerKey`를 검증한다.
6. 서버는 Toss `/v1/billing/authorizations/issue`를 호출해 billing key를 발급받는다.
7. billing key는 암호화해서 저장하고, 검색 및 중복 방지를 위한 hash를 별도로 저장한다.
8. 첫 결제는 `/v1/billing/{billingKey}`로 서버에서 실행한다.
9. 결제 성공 후 구독을 활성화하고 `auto_renew`, `billing_key_id`, `next_billing_time`을 저장한다.
10. 갱신 작업은 `service/toss_billing_task.go`에서 만료 예정 구독을 찾아 deterministic order id와 idempotency key로 결제한다.

## 발견했던 주요 오류와 수정 내용

### 1. Toss 수동 완료 시 과지급 가능성

문제:

Toss top-up 주문에서는 `Amount`가 Toss에 청구할 KRW 금액이고, 내부 지급 금액은 `Money`로 계산된다. 기존 수동 완료 로직은 일부 결제 수단만 `Money`를 사용하고, 그 외는 `Amount`를 내부 지급 금액처럼 처리할 수 있었다. Toss 주문이 이 경로를 타면 KRW 청구 금액이 그대로 내부 money로 지급되어 약 1000배 이상 과지급될 수 있었다.

수정:

Toss 수동 완료 경로는 `Amount`가 아니라 `Money` 기준으로 지급하도록 교체했다. provider별 금액 의미를 명확히 분리했다.

주의:

- Toss `Amount`: Toss에 청구하는 KRW 금액
- `Money`: 내부에 지급할 충전 단위
- `Amount`와 `Money`를 같은 의미로 사용하면 안 된다.

### 2. 웹훅 payload 신뢰 문제

문제:

웹훅 요청은 외부에서 들어오는 HTTP 요청이므로 payload만 보고 충전 완료, 구독 활성화, 결제 실패 처리를 하면 위조 요청에 취약하다.

수정:

웹훅은 상태 변경 신호로만 사용하고, `paymentKey` 또는 관련 식별자로 Toss API를 다시 조회한 뒤 로컬 주문과 대조하도록 교체했다. 로컬 주문의 `orderId`, 금액, 통화, 결제 수단, 현재 상태까지 함께 확인한다.

주의:

- 웹훅 body만으로 사용자 잔액을 변경하면 안 된다.
- 웹훅 retry 또는 중복 수신을 고려해 모든 처리는 idempotent해야 한다.
- Toss 콘솔에서 모든 결제 MID에 웹훅 URL을 등록해야 한다.

### 3. `customerKey` 생성 방식 문제

문제:

Toss SDK v2에서는 결제 객체 생성 시 안정적인 `customerKey`가 필요하다. user id, 이메일, 전화번호처럼 개인정보이거나 예측 가능한 값을 그대로 쓰면 보안과 개인정보 측면에서 부적절하다. 또한 임시 값이나 매번 바뀌는 값은 자동결제 billing key와 연결하기 어렵다.

수정:

사용자별로 안정적인 Toss 전용 `customerKey`를 생성하고 저장하는 방식으로 교체했다. 이 값은 서버가 관리하며, 결제창과 자동결제 인증 모두 같은 canonical 값을 기준으로 검증한다.

주의:

- `customerKey`에는 user id, 이메일, 전화번호를 직접 넣지 않는다.
- callback으로 돌아온 `customerKey`는 로컬 주문의 값과 반드시 비교한다.
- 자동결제에서는 `customerKey` 불일치를 결제 실패로 처리해야 한다.

### 4. `paymentKey` 저장 순서 문제

문제:

Toss 결제 승인 전에 `paymentKey`를 로컬에 저장하지 않으면, 승인 API 호출은 성공했지만 서버 저장이나 이후 처리가 실패했을 때 해당 결제를 다시 추적하기 어렵다. 반대로 승인 후 저장에 실패하면 실제 결제는 완료되었는데 로컬 주문은 pending으로 남을 수 있다.

수정:

결제 승인 API를 호출하기 전에 callback의 `paymentKey`를 먼저 로컬 주문에 기록하도록 교체했다. 저장에 실패하면 승인 API를 호출하지 않고 실패 처리한다.

주의:

- `paymentKey` 저장 실패 상태에서 `/v1/payments/confirm`을 호출하면 안 된다.
- `paymentKey` 컬럼 길이와 provider order id 저장 필드는 Toss 값이 잘리지 않도록 충분히 길어야 한다.
- 이후 reconcile 작업은 저장된 `paymentKey`를 기준으로 Toss 결제를 재조회한다.

### 5. 승인 실패와 실제 결제 상태 불일치

문제:

`/v1/payments/confirm` 호출이 네트워크 오류, timeout, 중복 처리, idempotency 처리 중 상태 등으로 실패해도 Toss 쪽에서는 결제가 완료되었을 수 있다. HTTP status만 보고 주문을 실패 처리하면 실제 결제 완료 주문이 미지급 상태로 남을 수 있다.

수정:

승인 실패 시 즉시 주문을 실패시키지 않고, 가능한 경우 Toss 결제를 재조회한다. 재조회 결과가 `DONE`이면 정상 충전 처리하고, 명확한 terminal failure일 때만 실패 처리한다. 판단이 어려운 중간 상태는 pending으로 유지해 cleanup/reconcile 작업이 다시 확인하도록 했다.

주의:

- confirm API의 HTTP 실패가 곧 결제 실패는 아니다.
- `IDEMPOTENT_REQUEST_PROCESSING`처럼 처리 중인 응답은 재조회 또는 재시도 관점으로 다뤄야 한다.
- `DONE` 상태를 확인하기 전까지 내부 지급을 하면 안 된다.

### 6. 결제 수단과 통화 검증 부족

문제:

금액과 상태만 확인하면, 의도하지 않은 결제 수단이나 통화가 섞여도 통과할 수 있다. 현재 구현은 카드 결제 기반으로 설계되어 있으므로 카드 결제가 아닌 흐름은 별도 검토가 필요하다.

수정:

Toss 응답 검증에 `currency == KRW`, 카드 결제 정보 존재 여부, `orderId`, `totalAmount`, `paymentKey` 비교를 추가했다.

주의:

- 카드 전용 흐름에서는 `card` 정보가 없는 결제를 성공 처리하면 안 된다.
- 가상계좌, 계좌이체, 간편결제 등 다른 수단을 추가하면 fail URL 처리, webhook event, 상태 전이가 달라질 수 있다.
- 비동기 결제 수단을 추가할 때는 현재 카드 전용 fail 처리 로직을 그대로 재사용하면 안 된다.

### 7. callback URL 생성 문제

문제:

서버의 외부 접근 주소가 비어 있거나 잘못되어 있으면 Toss success URL, fail URL이 잘못 생성된다. 이 경우 사용자는 결제를 마쳐도 서비스로 돌아오지 못하거나, pending 주문만 남을 수 있다.

수정:

Toss 주문 생성 전 `ServerAddress` 기반 callback URL을 검증하는 흐름을 추가했다. 유효하지 않으면 결제 세션을 만들지 않도록 했다.

주의:

- 운영 환경에서는 `ServerAddress`가 실제 외부 접근 가능한 HTTPS URL이어야 한다.
- reverse proxy, CDN, ingress 경로가 있다면 최종 callback URL이 Toss에서 접근 가능한지 확인해야 한다.

### 8. pending 주문 정리 부족

문제:

결제창을 열고 사용자가 이탈하거나, 결제 승인 직후 서버 처리가 실패하거나, callback/webhook이 누락되면 pending 주문이 장기간 남을 수 있다. 특히 `paymentKey`가 저장된 pending 주문과 아예 결제를 시작하지 않은 pending 주문은 다르게 처리해야 한다.

수정:

pending cleanup/reconcile 작업을 추가해 상태별로 다르게 처리하도록 했다.

- `paymentKey`가 없는 오래된 주문: 안전 기간이 지난 뒤 만료 처리
- `paymentKey`가 있는 pending 주문: Toss API 재조회 후 `DONE`이면 지급, 실패/취소면 종료, 중간 상태면 유지
- subscription pending 주문: billing auth 또는 첫 결제 상태에 따라 별도 정리

주의:

- 결제 상태를 모르는 주문을 단순 만료하면 실제 결제 완료 건이 누락될 수 있다.
- cleanup 작업은 master 노드 또는 단일 실행 보장이 있는 환경에서 동작해야 한다.

### 9. 정기결제 key 분리와 활성화 검증

문제:

일반 결제 키와 자동결제 키가 항상 같은 권한을 가진다고 가정하면, 실제 운영에서 billing key 발급이나 자동결제 승인 단계가 실패할 수 있다. Toss에서는 결제 MID와 자동결제 MID, 키 권한, 웹훅 등록 상태가 운영 설정에 따라 달라질 수 있다.

수정:

정기결제용 client key, secret key, 활성화 여부를 별도로 관리하도록 분리했다. 자동결제가 명시적으로 활성화되어 있고 필요한 키가 설정된 경우에만 자동결제 플로우를 노출하도록 했다.

주의:

- Toss 콘솔에서 자동결제 사용 가능 MID인지 확인해야 한다.
- 일반 결제 MID와 자동결제 MID가 다르면 두 MID 모두에 웹훅을 등록해야 한다.
- test key와 live key를 섞어 쓰면 callback 검증 또는 billing key 결제가 실패한다.

### 10. billing key 저장 및 원격 삭제 실패 처리

문제:

billing key 발급 후 로컬 저장, 첫 결제, 구독 활성화 중간 단계에서 실패할 수 있다. 또한 사용자가 자동결제를 해지할 때 Toss 원격 billing key 삭제가 실패할 수 있다. 이때 로컬에서 먼저 삭제 완료로 처리하면 원격에 살아 있는 billing key를 추적하지 못한다.

수정:

billing key는 암호화 저장하고, 삭제 실패 시 `pending_revocation` 상태로 남겨 재시도할 수 있게 했다. 원격 삭제가 성공한 뒤에 최종 revoked 상태로 전환한다.

주의:

- 원격 삭제가 실패한 billing key를 로컬에서 완전히 잊으면 안 된다.
- `pending_revocation` 상태는 운영 모니터링 대상이다.
- 암호화된 billing key 복호화 실패도 수동 조치가 필요한 운영 이벤트로 봐야 한다.

### 11. billing auth 성공 여부가 불명확한 경우

문제:

`/v1/billing/authorizations/issue` 호출이 서버 입장에서는 실패했지만 Toss 쪽에서는 billing key가 발급되었을 수 있다. 응답에 billing key가 없으면 서버가 원격 key를 직접 삭제할 수 없다.

수정:

billing key를 알 수 없는 실패는 로컬 주문을 안전하게 만료시키고, 운영 로그로 Toss 콘솔 수동 확인이 필요하다는 신호를 남기도록 했다. billing key를 알고 있는 실패는 원격 삭제 또는 pending revocation으로 이어지게 했다.

주의:

- billing key를 모르는 상태에서는 API로 원격 삭제할 수 없다.
- Toss 콘솔의 고객키, 주문번호, 시간대 기준 수동 확인 절차가 필요하다.

### 12. 정기결제 갱신 중복 청구 가능성

문제:

정기결제 갱신 작업이 첫 결제 성공 후 로컬 저장에 실패하거나, 작업이 재시도되면 같은 주기에 중복 청구가 발생할 수 있다.

수정:

갱신 주문은 구독 id와 결제 주기를 기준으로 deterministic order id를 만들고, Toss `Idempotency-Key`를 함께 사용하도록 했다. pending renewal 주문에는 결제 당시 금액과 provider credential snapshot을 저장해 이후 설정 변경과 분리했다.

주의:

- 같은 갱신 주기에는 같은 idempotency key를 사용해야 한다.
- 가격 변경은 새 주기부터 적용되어야 하며 이미 생성된 pending 주문의 금액을 바꾸면 안 된다.
- 재시도 작업은 로컬 상태와 Toss 상태를 모두 확인해야 한다.

### 13. 취소, 환불, 부분 환불 처리

문제:

결제 완료 후 사용자가 이미 내부 quota를 사용했을 수 있으므로, Toss 취소 또는 부분 환불 이벤트를 자동으로 내부 차감으로 매핑하기 어렵다. 부분 환불은 내부 사용량과 충돌할 수 있다.

수정:

취소 또는 환불 webhook은 자동 롤백하지 않고 reconciliation required 로그를 남기는 방향으로 정리했다. 운영자가 top-up log와 Toss 콘솔을 기준으로 수동 조치할 수 있게 했다.

주의:

- 부분 환불을 자동 차감하면 사용자 잔액이 음수가 되거나 사용 완료 quota와 충돌할 수 있다.
- 환불 정책과 내부 quota 회수 정책은 별도 제품 정책으로 확정해야 한다.

## 교체한 구현 패턴

### 기존: 결제 링크 또는 redirect 결과 중심

기존에는 결제창 시작과 결제 완료 판단이 브라우저 redirect 결과에 많이 의존할 수 있었다.

현재:

- 서버 pending 주문 생성
- Toss SDK `requestPayment`
- 서버 confirm
- Toss API 재조회
- idempotent 지급

이 흐름으로 교체했다.

### 기존: 웹훅 payload 직접 처리

기존에는 웹훅 payload의 상태값만으로 로컬 상태를 바꿀 위험이 있었다.

현재:

- 웹훅 수신
- 로컬 주문 조회
- Toss API authoritative fetch
- 로컬 주문과 Toss 응답 대조
- 상태 전이

이 흐름으로 교체했다.

### 기존: 정기결제 단일 단계 처리

기존 정기결제는 billing auth, billing key 저장, 첫 결제, 구독 활성화, 해지가 한 흐름으로 뭉쳐 있으면 중간 실패 복구가 어렵다.

현재:

- pending subscription order
- billing authorization
- billing key issue
- encrypted billing key 저장
- 첫 결제
- subscription activation
- renewal task
- revocation retry

각 단계를 상태로 나누어 복구할 수 있게 했다.

## 주요 파일

백엔드 설정:

- `setting/payment_toss.go`
- `setting/operation_setting.go`
- `setting/config.go`

일반 결제 및 웹훅:

- `controller/topup_toss.go`
- `controller/payment_webhook_availability.go`
- `controller/topup_manual.go`
- `model/topup.go`
- `service/toss_pending_cleanup_task.go`
- `router/api-router.go`

정기결제:

- `controller/subscription_payment_toss.go`
- `model/toss_billing.go`
- `model/subscription.go`
- `service/toss_billing_task.go`
- `service/subscription_task.go`

프론트엔드:

- `web/default/src/components/playground/HeaderBar.tsx`
- `web/default/src/components/subscription/SubscriptionPlanGrid.tsx`
- `web/default/src/components/subscription/SubscriptionPage.tsx`
- `web/default/src/pages/SubscriptionCallback/index.tsx`
- `web/default/src/pages/SubscriptionFail/index.tsx`
- `web/default/src/pages/TossFail/index.tsx`
- `web/default/src/services/PaymentService.ts`
- `web/default/src/services/SubscriptionPaymentService.ts`

테스트:

- `controller/topup_toss_test.go`
- `controller/subscription_payment_toss_test.go`
- `model/topup_toss_cleanup_test.go`
- `model/toss_billing_test.go`
- `service/toss_billing_task_test.go`
- `setting/payment_toss_test.go`

## 운영 체크리스트

- Toss 일반 결제 client key, secret key가 같은 환경의 키인지 확인한다.
- Toss 자동결제 client key, secret key가 자동결제 권한이 있는 MID의 키인지 확인한다.
- `ServerAddress`가 외부에서 접근 가능한 HTTPS URL인지 확인한다.
- Toss 콘솔에 `<ServerAddress>/api/toss/webhook`을 등록한다.
- 일반 결제 MID와 자동결제 MID가 다르면 둘 다 웹훅을 등록한다.
- 카드 결제만 지원하는 현재 전제를 운영 정책과 UI에 맞춘다.
- `TossUnitPrice`, `TossMinTopUp`, `TossCardMinimumAmountKRW`의 단위를 혼동하지 않는다.
- `pending` top-up, `pending` subscription order, `pending_revocation` billing key가 장기간 남는지 모니터링한다.
- `TOSS RECONCILIATION REQUIRED` 유형의 로그를 운영 알림 대상으로 둔다.
- live key 적용 전 test key로 결제창, 승인, 웹훅, billing auth, 첫 결제, 갱신, 해지까지 한 번씩 검증한다.

## 향후 변경 시 주의사항

### 결제 수단 추가

현재 구현은 카드 결제를 기준으로 검증한다. 가상계좌, 계좌이체, 휴대폰, 간편결제 등을 추가할 경우 다음을 다시 확인해야 한다.

- Toss SDK `requestPayment` 파라미터
- success/fail callback 의미
- 비동기 입금 또는 대기 상태
- webhook event 종류
- `DONE` 외 상태 전이
- 환불 및 취소 정책
- 카드 정보 검증 로직 제거 또는 분기

### 자동결제 정책 변경

구독 주기, 가격, 갱신 시점, 실패 재시도 정책을 바꾸는 경우 다음을 확인해야 한다.

- 이미 생성된 pending renewal 주문의 금액 보존
- idempotency key 생성 규칙
- 다음 결제 예정일 계산
- 구독 만료와 갱신 실패의 사용자 노출 방식
- billing key 해지와 구독 해지의 순서

### 금액 단위 변경

Toss는 KRW 정수 금액을 사용하고, 서비스 내부는 money/quota 단위를 별도로 사용한다.

변경 시 다음을 반드시 구분해야 한다.

- Toss 청구 금액
- 내부 지급 금액
- 표시용 금액
- 최소 결제 금액
- 환율 또는 단가 설정

## 검증 관점

코드 변경 후에는 최소한 다음 관점의 테스트가 필요하다.

- top-up 생성 시 잘못된 `ServerAddress`를 거부하는지
- Toss confirm callback의 `amount`, `orderId`, `paymentKey` 불일치를 거부하는지
- confirm 실패 후 Toss 재조회 결과가 `DONE`이면 정상 지급하는지
- 중복 confirm, 중복 webhook이 중복 지급되지 않는지
- 수동 완료에서 Toss 주문이 `Money` 기준으로 지급되는지
- billing auth callback의 `customerKey` 불일치를 거부하는지
- billing key 저장 실패 시 원격 삭제 또는 pending revocation으로 이어지는지
- renewal retry가 같은 주기에 중복 청구하지 않는지
- `pending_revocation` billing key가 재시도 후 정리되는지
- 취소, 부분 환불 webhook이 자동 차감 대신 reconciliation 로그를 남기는지

## 결론

현재 Toss 연동은 공식 문서의 핵심 흐름인 SDK 결제 요청, 서버 승인, server-side 검증, billing authorization, billing key 기반 자동결제 구조에 맞춰 정리되었다.

가장 중요한 운영 원칙은 세 가지다.

1. 브라우저 callback과 웹훅은 최종 진실이 아니다.
2. 실제 지급과 구독 활성화는 Toss API 확인 뒤에만 수행한다.
3. Toss KRW 금액과 내부 money/quota 단위를 절대 혼동하지 않는다.

이 세 가지가 깨지면 과지급, 미지급, 중복 청구, 원격 billing key 누락 같은 문제가 다시 발생할 수 있다.
