# Toss Payments 하드닝 논리 커밋 상세 — C07부터 C12

이 문서는 [커밋 인덱스](2026-07-12-toss-hardening-commit-index.md)의 C07–C12를 설명한다. 일반 결제 취소·환불과 webhook·Transaction 대사를 먼저 완성한 뒤, 구독 주문 snapshot과 billing key 발급·삭제 lifecycle을 다룬다.

## C07. fix(toss): fence refunds and persist cancellation evidence

### 목적

provider에서 결제됐지만 local 권한을 지급할 수 없는 건을 안전하게 환불하고, 전액·부분 취소와 fulfillment가 경쟁해도 재무 evidence를 잃거나 중복 refund POST를 만들지 않게 한다.

### 선행 커밋

C01–C06

### 발견한 문제

1. provider 결제는 DONE인데 local amount·method·quota·target 계약이 맞지 않으면 지급도 방치도 위험하다.
2. callback·webhook·scanner가 결제를 정산하는 동안 cancellation worker가 동시에 실행될 수 있다.
3. PARTIAL_CANCELED를 완료로 닫으면 남은 balance가 Toss에 남는다.
4. refund POST timeout 뒤 새 Idempotency-Key를 만들면 같은 balance에 여러 취소 operation이 생길 수 있다.
5. 같은 cancellation webhook이 반복되거나 detailed cancel object와 cumulative snapshot이 섞이면 cancelAmount를 중복 계산할 수 있다.
6. provider의 2xx cancel 응답이 다른 payment identity여도 local event가 생성될 수 있었다.
7. 이미 quota를 지급한 뒤 취소된 건을 자동으로 회수하면 사용된 quota와 entitlement가 음수가 될 수 있다.
8. 입금된 가상계좌 환불은 refundReceiveAccount와 은행 refund status가 필요한데 현재 서비스는 이 정보를 갖고 있지 않다.

### 적용한 수정

- local fulfillment가 금지된 인증 결제에 refund_required event를 생성한다.
- refund_required를 운영 메모가 아니라 모든 settlement를 막는 provider-write fence로 사용한다.
- TopUp row를 먼저 잠근 뒤 TossPaymentEvent row를 잠그는 공통 lock order를 사용한다.
- refund operation에 다음 값을 영속화한다.
  - operation Idempotency-Key
  - 최초 시도 DB timestamp
  - 대상으로 삼은 authoritative remaining balance
- 같은 balance에는 15일 동안 같은 key를 사용한다.
- provider balance가 감소한 경우 이전 cancellation이 일부 성공했다는 증거로 보고 새 balance용 operation을 만든다.
- balance가 증가하면 stale·손상 observation으로 보고 새 POST를 거부한다.
- PARTIAL_CANCELED의 completed transaction을 먼저 저장한 뒤 남은 balance refund를 이어간다.
- cancels 배열의 transactionKey와 cancelAmount를 개별 event identity로 사용한다.
- 상세 transactionKey가 없는 legacy response는 cumulative snapshot mode로 별도 처리한다.
- event key가 같아도 evidence가 다르면 conflict로 막는다.
- cancel 2xx도 paymentKey, orderId, type, totalAmount, currency를 원 요청과 대조한다.
- CANCELED, PARTIAL_CANCELED evidence가 fulfillment보다 먼저 들어오면 settlement를 차단한다.
- 이미 지급된 결제의 취소는 quota를 자동 차감하지 않고 required reconciliation event로 남긴다.
- refund_required는 관리자 note만으로 resolve하지 못하고 full refund evidence로만 닫는다.
- 관리자 list, detail, resolve API에 resolver, time, note를 남긴다.

### event 종류

| event type | 의미 | 자동 처리 |
|---|---|---|
| cancellation | 전액·부분 취소 transaction evidence | 미지급 건 settlement 차단, 지급 후 건 운영 대사 |
| paid_pending_fulfillment | provider 결제 완료·local 권한 미지급 | 계약이 안전하면 재정산 |
| financial_mismatch | amount·currency·identity·balance 불일치 | 자동 수정하지 않음 |
| refund_required | local 지급 금지·provider 환불 필요 | full refund까지 지급 fence |

### 가상계좌 처리

- UI는 CARD를 요청하지만 legacy·변조 response에서 가상계좌 상태가 나타날 수 있다.
- 입금 전 가상계좌는 일반 취소와 유사하게 처리할 수 있다.
- 입금 뒤 DONE 또는 PARTIAL_CANCELED이고 balance가 남으면 refundReceiveAccount가 필요하다.
- 현재 서비스는 구매자 계좌를 저장하지 않으므로 blind POST를 반복하지 않는다.
- CANCELED와 balance 0만으로 은행 입금 완료를 단정하지 않는다.
- 운영자가 Toss 콘솔, 고객 환불계좌, 은행 refund status를 확인해야 한다.

### 수정 뒤 불변조건

- refund가 필요한 payment는 local fulfillment될 수 없다.
- 같은 remaining balance의 refund operation은 15일 동안 같은 namespace다.
- refund balance는 단조 감소해야 한다.
- cancellation transaction은 transactionKey당 한 번 기록된다.
- partial refund 성공 evidence는 후속 실패 때문에 사라지지 않는다.
- 이미 지급된 권한은 자동으로 위험하게 clawback하지 않는다.

### 주요 구현 파일

- model/toss_payment_event.go
- controller/topup_toss.go
- controller/toss_payment_event_admin.go
- router/api-router.go
- model/topup.go
- model/subscription.go
- model/wallet_auto_recharge.go

### 관련 테스트

- model/toss_payment_event_test.go
- controller/topup_toss_refund_test.go
- controller/topup_toss_cancellation_validation_test.go
- controller/toss_payment_event_admin_test.go
- model/toss_settlement_cas_test.go
- controller/toss_subscription_cancel_webhook_test.go

대표 검증:

- concurrent refund prepare가 같은 key 반환
- 같은 balance에서 15일 동안 key 유지
- balance 감소 시에만 새 operation
- balance 증가 거부
- partial refund transaction을 먼저 저장
- malformed cancel 2xx identity 거부
- refund fence와 quota settlement의 atomic precedence
- refund_required 수동 resolve 거부

### rollback 주의

- refund_required event가 저장된 뒤 fence 검사를 제거하는 rollback은 금지한다.
- 새 cancellation ledger를 이해하지 못하는 구버전 handler가 cumulative amount를 중복 기록할 수 있으므로 old webhook worker를 다시 실행하면 안 된다.

---

## C08. fix(toss): authenticate and bound webhook processing

### 목적

서명이 없는 일반 Toss webhook을 네트워크 source와 provider 재조회로 검증하고, Toss의 10초 응답 정책 안에서 일관되게 처리한다.

### 선행 커밋

C01, C03, C06, C07

### 발견한 문제

1. PAYMENT_STATUS_CHANGED와 BILLING_DELETED는 payout.changed·seller.changed처럼 일반적으로 검증 가능한 signature가 없다.
2. X-Forwarded-For를 무조건 신뢰하면 공격자가 Toss source IP를 spoof할 수 있다.
3. reverse proxy를 전혀 고려하지 않으면 정상 Toss IP가 proxy IP로 보이고 모두 차단된다.
4. 공용 end-user API rate limit bucket에 webhook을 넣으면 정상 provider 재전송이 차단될 수 있다.
5. gzip wrapper, body read, DB busy wait, 여러 provider retry가 Toss의 10초 response requirement를 넘길 수 있다.
6. webhook payload를 그대로 신뢰해 DONE·CANCELED·BILLING_DELETED를 반영하면 위조 body에 취약하다.
7. transient DB·provider 오류에 200을 반환하면 Toss가 재전송하지 않아 event가 최종 유실된다.

### 적용한 수정

- 공식 Toss inbound IP allowlist를 애플리케이션 route 진입 전에 검사한다.
- direct RemoteAddr가 공식 source면 즉시 허용한다.
- X-Forwarded-For는 direct peer가 TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS에 포함될 때만 사용한다.
- app-facing 끝에서 chain을 역방향으로 걸어 trusted proxy를 건너뛰고 첫 untrusted hop을 original source로 취급한다.
- invalid XFF 항목이나 전체가 trusted proxy뿐인 비정상 chain은 거부한다.
- TOSS_WEBHOOK_SOURCE_CIDRS로 공식 IP 추가 직후 좁은 임시 allowlist 확장을 허용한다.
- 정확한 POST /api/toss/webhook과 trusted source 조합만 global API rate limit에서 제외한다.
- controller가 event 처리 전에 source를 defense-in-depth로 다시 검사한다.
- webhook route는 gzip을 우회한다.
- request socket read·write deadline을 route 초기에 설정한다.
- whole-handler context는 8초, response write는 9초 안으로 제한한다.
- body는 1MiB로 제한하고 trailing JSON을 거부한다.
- SQLite connection-local busy timeout을 handler budget보다 짧게 제한한다.
- PAYMENT_STATUS_CHANGED는 body의 Payment를 권위로 쓰지 않고 paymentKey로 authoritative GET한다.
- GET 결과의 MID namespace, orderId, paymentKey, amount, currency, type, status를 local order와 대조한다.
- BILLING_DELETED는 lookup API가 없으므로 trusted source, 엄격한 billingKey shape, local hash·encrypted plaintext exact match를 요구한다.
- transient provider·DB 실패, status propagation 지연, unresolved billing key는 503으로 재전송을 요청한다.
- unsupported event는 200으로 닫되 DEPOSIT_CALLBACK을 처리한 것으로 기록하지 않는다.

### 수정 뒤 불변조건

- 외부에서 조작한 XFF만으로 webhook route에 들어올 수 없다.
- 일반 payment webhook body만으로 quota·subscription을 변경하지 않는다.
- trusted webhook만 end-user rate limit을 우회한다.
- handler가 10초 response budget을 넘기지 않도록 자체 deadline을 가진다.
- 재시도로 해결 가능한 실패는 200으로 삼키지 않는다.

### 주요 구현 파일

- controller/toss_webhook_security.go
- controller/topup_toss.go
- controller/payment_webhook_availability.go
- router/api-router.go
- middleware/rate-limit.go
- model/db_time.go

### 관련 테스트

- controller/toss_webhook_security_test.go
- controller/toss_webhook_context_test.go
- router/api_router_toss_rate_limit_test.go
- router/api_router_toss_webhook_deadline_test.go
- middleware/rate_limit_test.go
- controller/topup_toss_test.go
- controller/toss_subscription_cancel_webhook_test.go

대표 검증:

- untrusted peer와 spoofed XFF 거부
- trusted proxy 뒤 공식 source 허용
- 일반 request는 rate limit을 우회하지 않음
- Toss route만 gzip bypass
- 8초 handler·9초 write deadline
- Payment GET 실패 시 503
- BILLING_DELETED identity 불명확 시 503

### 운영상 남은 위험

- source allowlist는 코드가 자동으로 Toss 문서를 동기화하지 않는다.
- reverse proxy topology를 잘못 설정하면 정상 webhook이 403이 된다.
- official inbound IP 변경을 모니터링하고, 임시 env 확장 뒤 코드 목록을 갱신해야 한다.

---

## C09. feat(toss): reconcile missed payments from the Transaction API

### 목적

브라우저 callback과 유한 webhook 재전송이 모두 유실된 뒤에도 Toss와 local DB를 지속적으로 대사한다.

### 선행 커밋

C01, C03, C06–C08

### 발견한 문제

1. 브라우저가 successUrl로 돌아오기 전에 닫힐 수 있다.
2. webhook은 10초 응답 실패 뒤 최대 7회만 재전송하므로 영구 복구 수단이 아니다.
3. 단순 시간 cursor를 page 처리 전에 전진하면 page 중간 crash에서 거래를 건너뛴다.
4. startingAfter를 메모리에만 두면 process restart 뒤 큰 page를 처음부터 읽거나 일부를 잃을 수 있다.
5. 한 건의 영구적인 snapshot·identity mismatch가 같은 MID의 모든 후속 거래를 영구 정지시킬 수 있다.
6. 여러 master가 같은 credential source를 동시에 scan하면 provider 부하와 중복 local processing이 생긴다.
7. current active key로 모든 local order를 매칭하면 다른 MID 거래를 잘못 적용할 수 있다.
8. historical backlog가 크면 최근 cancellation이 처리되지 않는 starvation이 생긴다.

### 적용한 수정

- master node에서 한 시간마다 Transaction API scanner를 실행한다.
- credential source를 normal·billing·wallet row의 저장 credential과 fingerprint에서 발견한다.
- source key별 DB high-water cursor를 저장한다.
- page의 startingAfter checkpoint를 별도 table에 영속화한다.
- 각 transaction의 local effect가 성공한 뒤에만 page checkpoint를 전진한다.
- 24시간 window와 1분 overlap을 사용한다.
- historical lane과 최근 7일 recent lane을 분리한다.
- 기본 bootstrap은 30일이다.
- 해당 credential로 저장된 가장 오래된 local order가 더 오래됐으면 그 create time까지 시작점을 확장한다.
- test key는 Toss 조회 제약을 고려해 최근 3일 안쪽으로 clamp한다.
- 한 run에 historical window 최대 3개, page size 1000, window당 최대 20 page를 사용한다.
- source별 25분 lease로 여러 node의 동일 source scan을 직렬화한다.
- lease owner와 cursor version을 모든 advance 조건에 포함한다.
- MID fingerprint 또는 exact encrypted legacy secret이 source와 일치해야 local order에 적용한다.
- Payment GET을 추가 수행해 transaction summary와 authoritative payment를 비교한다.
- immutable plan snapshot, target, cancellation evidence처럼 재시도로 고칠 수 없는 mismatch는 reconciliation event로 dead-letter한다.
- dead-letter가 영속화된 뒤 다음 transaction으로 진행한다.
- provider transport, malformed response, DB failure, concurrent local state 변화는 cursor를 고정하고 다음 run에서 재시도한다.
- null·unknown discriminator·unknown status를 성공으로 해석하지 않는다.

### 수정 뒤 불변조건

- cursor는 local side effect보다 먼저 전진하지 않는다.
- process crash 뒤에도 startingAfter에서 다시 시작한다.
- 같은 source lease는 한 node만 소유한다.
- different MID source는 local order에 적용되지 않는다.
- 영구 mismatch 한 건이 후속 결제를 굶기지 않지만 evidence 없이 건너뛰지도 않는다.
- 최근 상태 변경은 historical backlog와 별도 lane으로 확인된다.

### 주요 구현 파일

- controller/toss_transaction_reconciliation.go
- model/toss_transaction_reconciliation.go
- main.go
- model/toss_payment_event.go

### 관련 테스트

- controller/toss_transaction_reconciliation_test.go
- controller/toss_transaction_reconciliation_resilience_test.go
- model/toss_transaction_reconciliation_test.go

대표 검증:

- page 중간 run budget 종료 뒤 durable resume
- 각 적용 row마다 checkpoint
- source lease single owner
- exact billing attempt credential source discovery
- permanent mismatch dead-letter 뒤 후속 row 정산
- transient failure에서 cursor 고정
- recent lane과 historical lane starvation 방지
- test key 3일 floor

### 남은 운영 위험

- batch와 page limit은 의도적으로 bounded이므로 MID·거래량이 매우 많으면 cursor lag가 누적될 수 있다.
- cursor lag, page cursor age, source lease age, dead-letter 증가율을 모니터링해야 한다.
- Toss 콘솔과 local ledger의 완전한 회계 대사를 대체하지는 않는다.

---

## C10. fix(toss): snapshot subscription terms and reserve purchase capacity

### 목적

구독 구매 시점의 가격·기간·quota·group·reset 조건을 immutable하게 저장하고, 동시 checkout이 구매 제한을 초과하지 않게 한다.

### 선행 커밋

C02, C03

### 발견한 문제

1. 결제 완료·갱신 시 현재 SubscriptionPlan을 다시 읽으면 구매 뒤 변경된 가격과 혜택이 과거 주문에 소급된다.
2. TossUnitPrice 변경 뒤 provider KRW amount를 다시 계산하면 local 계약과 실제 청구가 달라진다.
3. plan이 삭제·disable·변경되는 동안 pending billing auth가 완료될 수 있다.
4. 두 checkout이 동시에 pending을 만들면 둘 다 MaxPurchasePerUser 검사에 통과할 수 있다.
5. 오래된 pending reservation을 무조건 제거하면 provider에 이미 ISSUE·charge가 전송된 주문의 구매 slot이 풀린다.
6. migration 중 관리자가 plan을 바꾸면 일부 legacy subscription만 다른 시점 조건으로 동결된다.
7. 지나치게 짧은 반복 기간은 scheduler race와 과도한 charge를 만든다.

### 적용한 수정

- subscription checkout 주문에 plan commercial snapshot을 저장한다.
- snapshot에는 plan identity뿐 아니라 price, duration, quota, reset, group, provider amount 등 갱신에 필요한 조건을 포함한다.
- snapshot의 canonical fingerprint를 생성한다.
- 프론트가 표시한 fingerprint와 server session fingerprint를 비교한다.
- 최초 결제 완료 시 snapshot을 UserSubscription의 write-once TossRenewalContractSnapshot으로 복사한다.
- purchase limit 계산에 pending Toss order reservation을 포함한다.
- reservation 생성과 count 검사를 user lock 아래 직렬화한다.
- pristine unattempted stale reservation만 해제한다.
- ISSUE·charge attempt, billing key, unresolved event가 있으면 replacement checkout을 차단한다.
- plan create, update, status 변경을 RunSubscriptionPlanMutationWithTossBarrier로 감싼다.
- maintenance fence·legacy renewal migration과 plan mutation이 같은 option lock을 사용한다.
- recurrence period에 최소 1시간 bound를 둔다.
- billing auth reservation에 최대 application lifetime을 둔다.

### 수정 뒤 불변조건

- 주문 생성 뒤 plan 변경이 해당 주문의 결제·구독 조건을 바꾸지 않는다.
- 갱신 계약은 최초 주문 snapshot에서만 파생된다.
- pending purchase reservation도 구매 제한에 포함된다.
- provider attempt evidence가 있는 reservation은 단순 timeout으로 해제되지 않는다.
- legacy contract migration과 plan 변경이 동시에 commit되지 않는다.

### 주요 구현 파일

- model/subscription_plan_snapshot.go
- model/subscription.go
- controller/subscription.go
- controller/subscription_payment_toss.go
- web/default/src/features/subscriptions/types.ts
- web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx
- web/default/src/features/subscriptions/lib/toss-checkout.ts

### 관련 테스트

- model/subscription_plan_snapshot_test.go
- model/toss_subscription_reservation_test.go
- model/toss_plan_mutation_barrier_test.go
- model/subscription_timing_bounds_test.go
- controller/subscription_payment_toss_test.go
- web/default/src/features/subscriptions/lib/toss-checkout.test.ts

대표 검증:

- plan 모든 상업 조건의 fingerprint
- plan 변경 뒤 기존 order snapshot 유지
- concurrent reservation single winner
- pending attempt가 purchase slot을 계속 보존
- maintenance 중 plan mutation 차단
- 최소 recurrence period

### 남은 위험

- snapshot 이전 legacy subscription은 구매 당시 조건을 완전히 복원하지 못할 수 있다. C13 migration이 안전하게 계산할 수 없는 row의 auto-renew를 비활성화한다.

---

## C11. fix(toss): recover billing issue and initial charge safely

### 목적

일회성 authKey로 billing key를 발급하고 첫 charge를 수행하는 두 provider POST에서 응답이 유실돼도 orphan key·중복 charge를 만들지 않게 한다.

### 선행 커밋

C01–C03, C10

### 발견한 문제

1. authKey는 일회성이며 ISSUE 성공 뒤 billingKey를 다시 조회하는 API가 없다.
2. ISSUE는 성공했지만 response를 잃으면 local은 billing key identity를 모를 수 있다.
3. malformed 2xx에서 일부 billingKey identity만 보이는데 전체 response를 실패로 버리면 원격 key를 정리하지 못한다.
4. ISSUE 재시도 중 authKey가 만료되거나 15일 idempotency retention을 넘을 수 있다.
5. 첫 charge가 성공했지만 local subscription transaction이 실패할 수 있다.
6. stale cleanup worker가 claim을 잃은 뒤 새 owner에게 attach된 key를 삭제할 수 있었다.
7. user·plan·reservation이 비활성화된 뒤 늦게 ISSUE result가 돌아올 수 있다.
8. key rotation 중 ISSUE와 charge가 서로 다른 credential namespace를 사용할 수 있었다.

### 적용한 수정

- callback의 customerKey를 local canonical customerKey와 비교한다.
- authKey, customerKey, exact provider credential을 암호화해 SubscriptionOrder에 저장한다.
- authKey hash와 ISSUE idempotency key를 별도 저장한다.
- ISSUE attempted flag, first attempt time, claim token, claim time을 영속화한다.
- provider POST 직전 C02 final barrier와 C03 exact credential 검사를 수행한다.
- 같은 snapshot·same idempotency·same secret로 ISSUE를 재생해 lost response를 회수한다.
- malformed 2xx에 billing key identity가 있으면 cleanup 가능한 evidence로 보존한다.
- billing key metadata와 customerKey·MID identity를 검증한 뒤 local key row에 attach한다.
- user·plan·reservation이 더 이상 유효하지 않으면 attach·charge하지 않고 revocation queue로 보낸다.
- claim lease와 CAS로 cleanup owner를 직렬화한다.
- stale worker는 persisted claim token과 key association이 계속 같은 경우에만 삭제를 시도한다.
- 15일을 넘긴 attempted ISSUE는 새 POST를 만들지 않는다.
- 첫 charge에 billing key, immutable order terms, exact attempt credential, attempted marker를 영속화한다.
- 첫 charge POST 전 GET lookup을 우선하고 ambiguous 결과는 pending으로 둔다.
- definitive credential rejection 뒤에만 same-MID credential promotion을 수행하고 final gate를 다시 통과한다.
- charge DONE과 orderId, amount, currency, type, billing key customer identity가 일치해야 subscription을 활성화한다.

### 수정 뒤 불변조건

- ISSUE attempt의 authKey·credential·Idempotency-Key가 crash 뒤에도 동일하다.
- stale cleanup owner가 새 owner의 key를 삭제할 수 없다.
- 15일 이후 ISSUE를 blind replay하지 않는다.
- 첫 charge와 subscription 활성화 사이에 provider success evidence를 잃지 않는다.
- user·plan이 무효화된 뒤 늦게 돌아온 key는 charge하지 않고 정리한다.

### 주요 구현 파일

- controller/subscription_payment_toss.go
- model/toss_billing.go
- model/subscription.go
- model/toss_client_fingerprint.go
- service/toss_pending_cleanup_task.go

### 관련 테스트

- controller/subscription_toss_issue_cleanup_claim_test.go
- controller/subscription_payment_toss_initial_recovery_test.go
- controller/toss_issue_authorization_expiry_test.go
- controller/toss_issue_corrupt_snapshot_test.go
- model/toss_billing_issue_cleanup_claim_test.go
- model/toss_initial_charge_recovery_test.go
- model/toss_billing_issue_rotation_test.go
- controller/toss_billing_issue_rotation_test.go
- controller/subscription_payment_toss_test.go

대표 검증:

- ISSUE Idempotency-Key 존재
- incomplete successful ISSUE response를 uncertainty로 보존
- stale cleanup이 새 owner key를 삭제하지 않음
- authKey application expiry
- 15일 이후 ISSUE POST 없음
- 첫 charge malformed 2xx + lookup miss pending
- initial charge exact credential recovery
- definitive rejection 뒤 gated promotion

### 운영상 남은 위험

- provider가 ISSUE를 성공했지만 billingKey identity를 전혀 반환하지 않았고 same-idempotency replay 기간도 지난 경우 API만으로 원격 key를 찾을 수 없다. Toss 콘솔에서 customerKey·시간대를 기준으로 수동 조사해야 한다.

---

## C12. fix(toss): harden billing-key revocation and subscription cancellation

### 목적

한 billing key가 여러 subscription·wallet reference에 연결된 경우 조기 삭제하지 않고, 사용자 취소·만료·BILLING_DELETED·관리자 삭제가 동시에 발생해도 local과 provider lifecycle을 일관되게 유지한다.

### 선행 커밋

C02, C03, C07–C11

### 발견한 문제

1. subscription 하나가 취소됐다고 공유 billing key를 즉시 DELETE하면 다른 활성 reference가 깨질 수 있다.
2. local row를 먼저 삭제하고 provider DELETE가 실패하면 원격 billing key를 추적할 수 없다.
3. BILLING_DELETED webhook에는 일반 payment처럼 authoritative lookup할 API가 없다.
4. encrypted billing key가 손상됐거나 legacy hash가 없으면 잘못된 local key를 revoke할 수 있다.
5. 사용자 취소와 renewal claim이 경쟁하면 취소 뒤 charge가 나갈 수 있다.
6. 관리자 invalidate·delete와 scheduler가 다른 lock order를 사용하면 deadlock 또는 stale reference가 생길 수 있다.
7. payment kill switch가 remote cleanup까지 중단하면 이미 비활성화해야 할 key가 계속 살아 있다.

### 적용한 수정

- UserBillingKey lifecycle을 active, pending_revocation, revoked로 구분한다.
- billing key plaintext는 암호화하고 one-way hash를 별도 저장한다.
- legacy hash는 decrypt 가능한 row에 한해 backfill한다.
- subscription·wallet reference count를 owner lock 아래 확인한다.
- 마지막 reference가 사라질 때만 pending_revocation으로 전환한다.
- provider DELETE 성공 뒤 revoked로 전환한다.
- DELETE 실패·timeout은 row를 지우지 않고 retry queue에 남긴다.
- kill switch와 payment compliance가 꺼져도 remote key cleanup은 계속 실행할 수 있다.
- BILLING_DELETED는 trusted source와 strict key shape를 먼저 확인한다.
- hash candidate를 고른 뒤 encrypted plaintext를 decrypt하여 constant-time exact identity를 확인한다.
- active legacy candidate를 안전하게 판별할 수 없으면 503으로 재전송을 유도한다.
- key revoke와 관련 subscription·wallet auto-renew 중단을 owner-first lock으로 처리한다.
- 사용자 cancel, subscription expiry, 관리자 invalidate·delete가 같은 helper와 lock order를 사용한다.
- cancel marker를 renewal·initial charge·wallet charge final gate에서 다시 확인한다.
- active claim이 이미 provider에 시도됐으면 GET·settlement·cleanup은 계속하되 새로운 pristine POST는 막는다.

### 수정 뒤 불변조건

- active reference가 하나라도 남은 billing key는 provider에서 삭제하지 않는다.
- DELETE 실패는 추적 가능한 pending_revocation 상태로 남는다.
- BILLING_DELETED body만으로 임의 local key를 revoke하지 않는다.
- payment disable은 새 charge를 막지만 이미 존재하는 key cleanup을 막지 않는다.
- cancellation이 final charge gate보다 먼저 직렬화되면 새 charge가 불가능하다.

### 주요 구현 파일

- model/toss_billing.go
- model/subscription.go
- model/wallet_auto_recharge.go
- controller/subscription_payment_toss.go
- controller/topup_toss.go
- service/toss_billing_task.go
- service/wallet_auto_recharge_task.go

### 관련 테스트

- model/toss_shared_billing_lifecycle_test.go
- model/toss_subscription_cancel_test.go
- controller/toss_subscription_cancel_webhook_test.go
- controller/subscription_payment_toss_test.go
- model/toss_billing_test.go
- model/wallet_auto_recharge_test.go

대표 검증:

- shared key가 마지막 reference 전에는 삭제되지 않음
- provider DELETE 실패 후 pending_revocation 유지
- kill switch 중 revocation retry
- BILLING_DELETED exact plaintext/hash match
- corrupt candidate에서 503
- cancel과 charge claim 경쟁에서 cancellation precedence
- 관리자 delete·expiry와 scheduler lock order

### 남은 운영 위험

- CRYPTO_SECRET을 잃으면 hash만으로 key DELETE API를 호출할 수 없다. 암호화 키 복구 또는 Toss 콘솔 수동 삭제가 필요하다.
- BILLING_DELETED 재전송 기간이 끝나기 전에 unresolved key alert를 처리해야 한다.
