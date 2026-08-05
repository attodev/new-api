# Toss Payments 하드닝 논리 커밋 상세 — C13부터 C18

이 문서는 [커밋 인덱스](2026-07-12-toss-hardening-commit-index.md)의 C13–C18을 설명한다. C16과 C18은 변경량이 커서 실제 커밋 시 하위 커밋으로 나누는 것을 권장한다.

## C13. fix(toss): make renewal identities safe across rolling upgrades

### 목적

subscription 갱신의 상업 조건과 논리 attempt identity를 최초 계약에 고정하고, 구버전·신버전 worker가 공존하거나 DB가 부분 복원돼도 같은 주기에 alternate provider order를 만들지 못하게 한다.

### 선행 커밋

C02, C03, C10–C12

### 발견한 문제

1. 기존 갱신은 subscription id와 next billing time을 사용했지만 restore·timezone·schema 변화가 있으면 동일 주기를 다른 문자열로 만들 수 있었다.
2. 구버전 scheduler와 신버전 scheduler가 서로 다른 orderId protocol을 사용하면 같은 주기에 두 결제를 만들 수 있다.
3. deterministic provider-facing orderId에 business identity가 드러나고, collision·재생 경계가 코드 convention에 의존했다.
4. DB restore에서 opaque prefix만 남고 subscription·cycle association이 사라지면 renewal인지 일반 주문인지 안전하게 판별할 수 없다.
5. 갱신 때 current plan·unit price를 읽으면 최초 계약과 다른 금액·기간·quota가 적용된다.
6. legacy subscription contract를 backfill하는 동안 plan 또는 Toss 설정이 바뀌면 일부 row만 다른 조건으로 동결된다.
7. key rotation 뒤 renewal이 다른 API-key idempotency namespace에서 재시도될 수 있었다.
8. 장기 장애 후 수일·수주 지난 due renewal을 한꺼번에 청구하면 사용자가 예상하지 못한 소급 과금이 된다.

### 적용한 수정

- subscription renewal contract를 최초 successful order snapshot에서 write-once로 저장한다.
- 갱신은 current plan이 아니라 TossRenewalContractSnapshot에서 amount, money, quota, duration, reset, group을 읽는다.
- 논리 attempt identity를 subscription_id, billing_time, attempt tuple로 저장한다.
- tuple에 unique constraint와 association 검증을 적용한다.
- provider-facing 신규 renewal orderId는 trn_ prefix의 opaque random ID를 사용한다.
- application restart나 retry는 저장된 orderId를 재사용한다.
- DB-authoritative recurring order-id protocol singleton을 추가한다.
- fresh DB는 v2 writer를 사용할 수 있고 legacy DB는 explicit drain acknowledgement 없이는 v2를 활성화하지 않는다.
- protocol 전환 전 billing과 wallet auto-recharge flag를 DB에서 false로 저장하게 한다.
- old master·scheduler·callback을 drain한 sole migration master만 activation env를 사용할 수 있다.
- opaque prefix인데 association tuple·discriminator가 없거나 충돌하면 신규 recurring writer 전체를 fail-closed한다.
- 기존 attempt의 GET·settlement·cleanup은 protocol activation 상태와 무관하게 계속할 수 있다.
- legacy renewal contract migration은 max-ID cutoff, bounded batch, option revision, maintenance gate에 고정한다.
- current terms로 안전하게 동결할 수 없는 legacy subscription은 auto-renew를 비활성화한다.
- plan mutation은 같은 option lock을 사용해 migration과 경쟁하지 않는다.
- renewal exact credential을 provider POST 전 저장한다.
- definitive auth rejection 뒤 next same-MID credential을 저장하고 attempted=false로 되돌린다.
- 다음 POST는 final barrier와 cancellation·grace·contract 검사를 다시 통과한다.
- 24시간 operational grace를 넘긴 renewal은 새 charge를 만들지 않는다.
- failure count가 3회에 도달하면 auto-renew를 끄고 key lifecycle을 정리한다.

### 수정 뒤 불변조건

- 동일 subscription cycle·attempt에는 local logical identity가 하나뿐이다.
- provider orderId는 한번 만들어지면 retry 중 바뀌지 않는다.
- old·new writer가 동시에 alternate ID를 만들 수 없다.
- 기존 subscription의 상업 조건은 현재 plan 변경에 영향받지 않는다.
- migration 중 plan·Toss config generation이 바뀌면 stale batch가 commit되지 않는다.
- 24시간을 넘긴 오래된 due renewal은 장애 복구 후 소급 청구되지 않는다.
- credential promotion 뒤에도 operational disable·cancel이 새 POST를 막을 수 있다.

### 주요 구현 파일

- model/toss_recurring_order_id_protocol.go
- model/toss_recurring_migration_guard.go
- model/toss_billing.go
- model/subscription.go
- model/subscription_plan_snapshot.go
- service/toss_billing_task.go
- main.go

### 관련 테스트

- model/toss_recurring_order_id_protocol_test.go
- model/toss_recurring_migration_guard_test.go
- model/toss_renewal_contract_migration_test.go
- model/toss_renewal_lifecycle_race_test.go
- model/toss_opaque_restore_guard_test.go
- model/toss_billing_issue_rotation_test.go
- controller/toss_opaque_restore_route_test.go
- controller/toss_billing_issue_rotation_test.go
- model/subscription_timing_bounds_test.go
- service/toss_billing_task_test.go

대표 검증:

- v2 activation이 explicit drain 없이는 실패
- fresh v2 renewal이 opaque ID 사용
- existing recovery가 protocol fence를 우회해 저장된 ID 사용
- restored opaque row의 association 손상 시 신규 recurring 전체 차단
- opaque ID collision retry
- migration 중 plan mutation 차단
- unsafe legacy contract auto-renew 비활성화
- credential promotion과 disable race
- 24시간 grace 이후 charge 없음

### 배포·rollback 주의

- activation acknowledgement env는 one-shot이며 sole master에서만 사용한다.
- 성공 뒤 즉시 env를 제거하고 old binary를 다시 시작하지 않는다.
- v2 opaque row가 생긴 뒤 v1 writer로 rollback하면 안 된다.
- 실제 단계는 .env.example과 docker-compose.yml의 recurring migration 주석을 따라야 한다.

---

## C14. fix(toss): harden wallet auto-recharge lifecycle

### 목적

지갑 자동충전을 subscription billing과 구분된 계약·정책으로 다루고, preset 확인부터 billing ISSUE, 실제 charge, settlement, cancel, key cleanup까지 immutable identity와 durable attempt로 연결한다.

### 선행 커밋

C01–C03, C06–C07, C10–C13

### 발견한 문제

1. Toss 자동결제 계약은 구독형을 기본으로 하며 비구독 wallet 충전은 별도 검토가 필요한데 billing flag 하나로 모두 열 수 있었다.
2. 사용자가 본 preset과 billing auth·charge 직전 DB preset이 달라질 수 있었다.
3. schedule, threshold, cooldown, daily limit를 worker selection 때만 검사하면 provider POST 직전 값이 바뀔 수 있었다.
4. billing ISSUE 응답 유실·malformed 2xx가 orphan key를 만들 수 있었다.
5. charge POST 성공 뒤 local marker·settlement 실패에서 다음 tick이 새 charge를 만들 수 있었다.
6. policy cancel·organization owner 변경·user 비활성화와 worker가 경쟁할 수 있었다.
7. old credential rejection 뒤 current secret으로 넘어갈 때 final gate를 다시 통과하지 않으면 disable 뒤 charge가 나갈 수 있었다.
8. monthly preset의 immediate charge, custom test interval, cancel_pending 상태가 UI와 backend에서 다르게 해석될 수 있었다.
9. 여러 policy가 같은 billing key를 참조할 때 한 정책 취소가 key를 삭제할 수 있었다.

### 적용한 수정

- TossWalletAutoRechargeEnabled를 TossBillingEnabled와 별도 option으로 둔다.
- checkout, ISSUE, 실제 charge의 세 provider 경계에서 두 flag와 payment compliance를 다시 확인한다.
- preset의 모든 재무·일정 조건을 canonical SHA-256 fingerprint로 만든다.
- server response와 browser-reviewed fingerprint를 비교한다.
- policy 생성 시 enabled preset의 amount, quota, threshold, interval, immediate flag를 snapshot한다.
- monthly preset은 charge_immediately를 false로 강제한다.
- custom test preset만 명시적 immediate charge를 사용할 수 있게 한다.
- user·organization owner, target, policy status를 provider POST 직전 row lock 아래 재검사한다.
- threshold balance, cooldown, daily limit, next charge time을 DB clock으로 다시 검사한다.
- wallet billing ISSUE에 authKey, customerKey, exact credential, idempotency key, attempted marker, claim token을 저장한다.
- stale ISSUE cleanup은 claim CAS와 key association을 재검사한다.
- 15일 이후 ISSUE POST를 재생하지 않는다.
- charge attempt identity를 policy, due cycle, attempt tuple에 고정한다.
- provider-facing 신규 orderId는 twa_ opaque protocol을 사용한다.
- pending TopUp과 policy association을 저장한다.
- provider POST 전에 exact attempt credential을 저장한다.
- ambiguous charge는 GET lookup과 known-charged recovery를 사용한다.
- settlement는 immutable Quota snapshot을 사용하고 policy·target row lock 아래 한 번만 지급한다.
- definitive auth rejection 뒤 next same-MID credential로 promotion하고 attempted=false로 되돌린다.
- 다음 POST가 별도 wallet final barrier를 다시 통과한다.
- policy cancel, target deletion, billing key deletion은 새 charge를 막지만 already-paid settlement와 remote cleanup은 계속한다.
- 24시간 grace를 넘긴 scheduled charge는 건너뛰고 다음 schedule로 전진한다.
- max failure 3회 뒤 policy를 비활성화하고 key reference를 정리한다.

### 수정 뒤 불변조건

- subscription billing 활성화만으로 wallet auto-recharge를 사용할 수 없다.
- 사용자가 확인한 preset과 server policy snapshot이 다르면 billing auth를 시작하지 않는다.
- 같은 wallet attempt는 하나의 TopUp, orderId, exact credential, idempotency namespace를 가진다.
- policy cancel 뒤 pristine new charge는 불가능하다.
- 이미 provider에서 charge된 건은 policy가 취소돼도 정확한 target에 정산하거나 refund fence로 보낸다.
- monthly schedule은 등록 즉시 무단 charge하지 않는다.

### 주요 구현 파일

- model/wallet_auto_recharge.go
- model/wallet_auto_recharge_preset.go
- controller/wallet_auto_recharge.go
- controller/payment_webhook_availability.go
- service/wallet_auto_recharge_task.go
- setting/payment_toss.go
- router/api-router.go

### 관련 테스트

- model/wallet_auto_recharge_test.go
- model/wallet_auto_recharge_preset_test.go
- model/wallet_auto_recharge_issue_cleanup_claim_test.go
- model/wallet_auto_recharge_issue_rotation_test.go
- controller/wallet_auto_recharge_test.go
- controller/wallet_auto_recharge_preset_test.go
- controller/wallet_toss_issue_cleanup_claim_test.go
- service/wallet_auto_recharge_task_test.go
- model/toss_target_lifecycle_test.go

대표 검증:

- separate wallet contract gate
- preset fingerprint 전체 필드 반영
- monthly immediate charge 강제 비활성
- custom test interval 표시·처리
- stale ISSUE worker가 새 active key를 삭제하지 않음
- duplicate callback·worker가 한 번만 settlement
- policy cancel과 charge final gate 경쟁
- organization owner 변경 차단
- credential promotion 뒤 final gate 재검사
- 24시간 grace 이후 scheduled charge skip

### 남은 운영 책임

- 별도 flag는 실제 Toss 계약을 증명하지 않는다. 운영 전 비구독 자동결제 사용 승인을 Toss와 확인해야 한다.
- wallet auto-recharge는 사용자 의도와 금전 영향이 크므로 live 활성화 전 test MID와 소액 live smoke test가 필요하다.

---

## C15. fix(toss): bound payment schedulers and use database time

### 목적

일반 confirm recovery, pending cleanup, billing renewal, wallet charge, key revocation, Transaction reconciliation이 clock skew·느린 provider·poison row·panic 때문에 서로를 굶기거나 중복 실행하지 않게 한다.

### 선행 커밋

C05, C07, C09, C11–C14

### 발견한 문제

1. application node clock이 다르면 due 이전 charge 또는 active claim 조기 탈취가 가능하다.
2. provider timestamp와 retry timestamp를 하나로 쓰면 오래된 poison row가 매 tick 첫 batch를 점유한다.
3. 큰 row batch를 먼저 claim하고 작은 worker pool로 처리하면 뒤 row가 lease 만료 뒤에야 provider call을 시작한다.
4. 일반 confirm은 10분 승인 window가 짧은데 billing cleanup이 느리면 같은 task guard 아래 굶을 수 있다.
5. callback panic이 worker goroutine 또는 전체 scheduler iteration을 중단할 수 있다.
6. SQLite는 DB write를 직렬화하므로 server DB와 같은 worker 수를 사용하면 lock contention이 커진다.
7. 장기 장애 후 오래된 renewal·wallet schedule을 한꺼번에 소급 청구할 수 있다.
8. 여러 master 또는 overlapping tick이 같은 queue를 처리할 수 있다.

### 적용한 수정

- due, claim, retry, expiry cutoff를 DB timestamp로 계산한다.
- 일반 paymentKey recovery를 15초 tick으로 분리한다.
- never-recorded checkout cleanup은 1분 tick과 50분 cutoff를 사용한다.
- general top-up recovery와 subscription cleanup에 독립 atomic running guard를 둔다.
- general recovery는 provider issuance deadline을 고려한 live lane과 poison/refund lane을 분리한다.
- worker가 즉시 처리할 수 있는 수만큼만 row를 reserve한다.
- bounded generic payment batch helper를 추가한다.
- item callback panic을 fixed message로 복구하고 다음 item·tick을 계속한다.
- panic value와 payment row를 로그에 출력하지 않는다.
- SQLite worker count를 더 작게 제한한다.
- billing scheduler는 1분 tick, bounded batch, max failure 3을 사용한다.
- wallet scheduler도 1분 tick과 scheduled·threshold·pending settlement queue를 분리한다.
- pending settlement를 신규 charge보다 먼저 처리한다.
- key revocation·ISSUE cleanup을 새 charge와 별도 tail queue로 둔다.
- billing과 wallet의 24시간 operational grace를 final gate에서도 다시 확인한다.
- 오래된 scheduled wallet policy는 charge하지 않고 다음 cycle로 advance한다.
- Transaction scanner는 한 시간 tick, per-source lease, 20분 run budget 안에서 순환한다.
- master-only startup과 process-local guard를 유지하되 DB claim·lease를 실제 중복 방지 경계로 사용한다.

### 수정 뒤 불변조건

- 재무 시각 판단은 DB clock 기준이다.
- claim한 batch가 worker capacity보다 커서 lease가 대기 중 만료되지 않는다.
- 느린 billing cleanup이 일반 confirm 10분 window를 빼앗지 않는다.
- 한 item panic이 다음 payment를 영구 정지시키지 않는다.
- 장기 장애 뒤 24시간을 넘긴 charge를 새로 만들지 않는다.
- process-local guard가 없어도 DB claim·lease가 동일 row provider operation을 직렬화한다.

### 주요 구현 파일

- model/db_time.go
- model/toss_maintenance_batch.go
- service/payment_batch.go
- service/toss_pending_cleanup_task.go
- service/toss_billing_task.go
- service/wallet_auto_recharge_task.go
- model/topup.go
- model/subscription.go
- model/wallet_auto_recharge.go
- controller/toss_transaction_reconciliation.go
- main.go

### 관련 테스트

- model/toss_maintenance_batch_test.go
- service/payment_batch_test.go
- service/toss_pending_cleanup_task_test.go
- service/toss_billing_task_test.go
- service/wallet_auto_recharge_task_test.go
- model/toss_pending_cleanup_test.go
- model/toss_webhook_context_test.go
- model/toss_renewal_lifecycle_race_test.go

대표 검증:

- DB clock 기반 cutoff
- bounded worker 수와 SQLite 축소
- item panic 뒤 다음 item 실행
- 일반 recovery·subscription cleanup independent guard
- poison row가 live approval을 굶기지 않음
- 24시간 grace 뒤 renewal·wallet charge 없음
- claim lease single owner

### 남은 운영 위험

- bounded queue는 유실보다 지연을 선택한다. backlog 지표가 없으면 처리 지연을 늦게 발견할 수 있다.
- DB 자체 clock과 timezone 설정이 비정상인 경우 애플리케이션 보정으로 해결되지 않는다.

---

## C16. Default 프론트엔드 커밋 그룹

Default 변경은 한 커밋보다 아래 다섯 커밋으로 나누는 것이 review와 rollback에 안전하다.

### C16a. feat(web-default): add safe Toss browser session primitives

#### 문제

- server order·customer·callback·amount를 SDK에 전달하기 전에 충분히 검증하지 않았다.
- 오래된 quote response가 최신 입력을 덮을 수 있었다.
- 빠른 double click, React StrictMode replay, unmount가 복수 주문·orphan iframe을 만들 수 있었다.
- cross-origin callback에서 iframe redirect가 실패할 수 있었다.

#### 수정

- 일반 payment·billing session의 callback URL, origin, path, customerKey, orderId, amount 범위를 검증한다.
- server quote를 immutable confirmation으로 고정하고 /toss/pay response 전 필드와 비교한다.
- request generation counter로 stale quote response를 무시한다.
- React state보다 빠른 synchronous ref로 중복 클릭을 차단한다.
- SDK instance destroy를 promise chain으로 직렬화한다.
- component unmount와 StrictMode replay에서 iframe을 정리한다.
- PAY_PROCESS_CANCELED를 정상 사용자 취소로 분리한다.
- cross-origin callback은 windowTarget self를 사용한다.
- browser 오류만으로 ambiguous server order를 삭제하지 않는다.

#### 파일

- web/default/src/features/wallet/api.ts
- web/default/src/features/wallet/types.ts
- web/default/src/features/wallet/hooks/index.ts
- web/default/src/features/wallet/hooks/use-payment.ts
- web/default/src/features/wallet/hooks/use-toss-payment-lifecycle.ts
- web/default/src/features/wallet/hooks/use-toss-payment.ts
- web/default/src/features/wallet/lib/index.ts
- web/default/src/features/wallet/lib/payment.ts
- web/default/src/features/wallet/lib/topup-amount-mode.ts
- web/default/src/features/wallet/lib/toss-payment-lifecycle.ts

#### 테스트

- web/default/src/features/wallet/lib/payment.toss.test.ts
- web/default/src/features/wallet/lib/topup-amount-mode.test.ts
- web/default/src/features/wallet/lib/toss-payment-lifecycle.test.ts

권장 검증:

~~~text
cd web/default
bun test src/features/wallet/lib/payment.toss.test.ts +  src/features/wallet/lib/topup-amount-mode.test.ts +  src/features/wallet/lib/toss-payment-lifecycle.test.ts
bun run build
~~~

### C16b. feat(web-default): bind Toss subscriptions to reviewed immutable terms

#### 문제

- confirmation 뒤 plan이 갱신되면 사용자가 본 조건과 server order 조건이 달라질 수 있었다.
- Toss가 legacy Epay path로 잘못 routing될 수 있었다.
- SDK 준비 실패·session mismatch에서 pending subscription order가 남을 수 있었다.
- 여러 auto-renew subscription에 개별 cancel UI를 보여 실제 전체 cancel API 의미와 달랐다.

#### 수정

- plan, KRW amount, duration, quota 조건 fingerprint를 고정한다.
- billing session이 reviewed snapshot과 정확히 일치할 때만 CARD billing auth를 연다.
- failure·cancel·mismatch에서 pending order cancel API를 호출한다.
- Toss를 Epay 목록에서 제외한다.
- 전체 Toss auto-renew count와 destructive confirmation을 사용하는 단일 cancel control을 제공한다.

#### 파일과 테스트

- web/default/src/features/subscriptions/api.ts
- web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx
- web/default/src/features/subscriptions/hooks/use-toss-billing.ts
- web/default/src/features/subscriptions/lib/index.ts
- web/default/src/features/subscriptions/lib/toss-checkout.ts
- web/default/src/features/subscriptions/lib/toss-checkout.test.ts
- web/default/src/features/subscriptions/types.ts
- web/default/src/features/wallet/components/cancel-all-toss-auto-renew.tsx
- web/default/src/features/wallet/components/subscription-plans-card.tsx
- web/default/src/features/wallet/components/subscription-plans-card.test.js
- web/default/src/features/wallet/components/wallet-subscription-status-card.tsx
- web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts

### C16c. feat(web-default): pin wallet auto-recharge preset terms

#### 문제

- reviewed preset과 server session의 amount·period·immediate 조건이 바뀔 수 있었다.
- malformed session에서도 durable pending trade를 정리하지 못할 수 있었다.
- cancel_pending을 active policy처럼 표시할 수 있었다.
- monthly preset에 immediate charge를 허용하고 custom test period를 monthly로 설명할 수 있었다.

#### 수정

- preset SHA-256 fingerprint와 immutable policy 필드를 전부 비교한다.
- session 일부가 malformed여도 valid tradeNo가 있으면 pending cancel을 시도한다.
- 공통 SDK lifecycle과 click guard를 auto-recharge에도 적용한다.
- monthly immediate를 강제로 끄고 custom test period에만 노출한다.
- 실제 custom interval과 immediate charge amount를 표시한다.

#### 파일과 테스트

- web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts
- web/default/src/features/wallet/components/auto-recharge-card.tsx
- web/default/src/features/wallet/components/auto-recharge-card.test.ts
- web/default/src/features/wallet/lib/auto-recharge-options.ts
- web/default/src/features/wallet/lib/wallet-auto-recharge-session.ts
- web/default/src/features/wallet/lib/wallet-auto-recharge-session.test.ts
- web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx
- web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts

### C16d. feat(web-default): integrate safe Toss flows into personal and organization wallets

#### 문제

- personal·organization wallet의 quote minimum unit, owner permission, callback return handling이 달랐다.
- organization user가 personal endpoint로 결제하거나 stale confirmation snapshot을 재사용할 수 있었다.
- callback query가 browser history에 남아 refresh 때 재실행될 수 있었다.

#### 수정

- personal·organization 모두 server quote 뒤 minimum을 판정한다.
- organization owner만 organization payment·auto-recharge를 관리한다.
- confirmation dialog는 immutable server quote만 표시한다.
- callback result를 한 번 toast로 처리한 뒤 query를 제거한다.
- subscription, top-up, scheduled, threshold gateway flag를 실제 wallet 화면에서 조립한다.

#### 파일과 테스트

- web/default/src/features/organizations/api.ts
- web/default/src/features/organizations/components/organization-wallet.tsx
- web/default/src/features/organizations/components/organization-wallet.test.ts
- web/default/src/features/wallet/components/recharge-form-card.tsx
- web/default/src/features/wallet/index.tsx
- web/default/src/routes/_authenticated/organization/wallet.tsx
- web/default/src/routes/_authenticated/wallet/index.tsx

### C16e. feat(web-default-admin): add atomic Toss repair and maintenance UI

#### 문제

- client·secret을 개별 option API로 저장하면 generation이 섞일 수 있었다.
- corrupt·unattested config를 안전하게 complete replacement할 UI가 없었다.
- maintenance 중 enable·field edit가 가능했다.
- wallet auto-recharge 계약 flag가 subscription billing flag와 구분되지 않았다.

#### 수정

- 14개 Toss option을 전용 bulk API로 보낸다.
- credential 한쪽이 바뀌면 같은 form snapshot의 pair를 함께 보낸다.
- all-disabled repair, repair token, maintenance retry UI를 제공한다.
- blank secret이 삭제임을 destructive dialog에서 경고한다.
- repair·maintenance 동안 enable과 unsafe field를 잠근다.
- 별도 wallet auto-recharge flag와 KRW minimum validation을 표시한다.

#### 파일과 테스트

- web/default/src/features/system-settings/api.ts
- web/default/src/features/system-settings/types.ts
- web/default/src/features/system-settings/billing/index.tsx
- web/default/src/features/system-settings/billing/section-registry.tsx
- web/default/src/features/system-settings/integrations/payment-settings-section.tsx
- web/default/src/features/system-settings/integrations/toss-option-updates.ts
- web/default/src/features/system-settings/integrations/toss-option-updates.test.ts

### C16 공통 불변조건

- SDK를 열기 전에 reviewed snapshot과 server session이 일치한다.
- 한 UI action은 동시에 한 SDK instance만 만든다.
- unmount·StrictMode가 iframe과 pending lifecycle을 고아로 남기지 않는다.
- organization endpoint와 personal endpoint가 섞이지 않는다.
- config repair UI는 backend C02의 all-disabled complete-set 계약을 우회하지 않는다.

---

## C17. feat(web-classic): mirror Toss billing and auto-recharge safety

### 목적

Classic theme에도 일반 Toss top-up, subscription billing, wallet auto-recharge를 추가하되 Default에서 해결한 snapshot·SDK lifecycle·target routing 문제를 되풀이하지 않는다.

### 선행 커밋

C04, C10–C14, C16a–C16d

### 발견한 문제

- Classic에는 Toss SDK dependency와 safe lifecycle helper가 없거나 legacy provider path로 빠졌다.
- server quote와 displayed terms를 비교하지 않았다.
- subscription billing과 wallet auto-recharge session validation이 없었다.
- organization user의 target endpoint와 owner permission이 명확하지 않았다.
- callback query와 SDK iframe cleanup이 빠졌다.
- 전체 auto-renew cancel API 의미와 UI가 맞지 않았다.

### 적용한 수정

- Toss SDK v2 dependency를 추가한다.
- payment() instance와 CARD requestPayment·requestBillingAuth를 사용한다.
- 일반 quote, subscription snapshot, wallet preset snapshot을 server response와 비교한다.
- personal·organization target routing helper를 추가한다.
- owner-only organization action을 적용한다.
- SDK lifecycle, duplicate guard, destroy, pending cleanup을 추가한다.
- /wallet callback을 query를 보존해 /console/topup으로 redirect한다.
- callback query를 한 번 처리한 뒤 정리한다.
- WalletAutoRechargeCard와 전체 Toss auto-renew cancel UI를 추가한다.

### 수정 뒤 불변조건

- Classic과 Default가 같은 backend session contract를 따른다.
- Classic도 reviewed quote·plan·preset이 바뀌면 SDK를 열지 않는다.
- target type은 route helper로 명시하며 organization member가 personal charge를 만들지 않는다.
- SDK cancel·unmount가 duplicate order를 만들지 않는다.

### 주요 구현 파일

- web/classic/package.json
- web/classic/bun.lock
- web/classic/src/App.jsx
- web/classic/src/components/topup/RechargeCard.jsx
- web/classic/src/components/topup/SubscriptionPlansCard.jsx
- web/classic/src/components/topup/WalletAutoRechargeCard.jsx
- web/classic/src/components/topup/index.jsx
- web/classic/src/components/topup/modals/SubscriptionPurchaseModal.jsx
- web/classic/src/components/topup/tossPaymentLifecycle.js
- web/classic/src/components/topup/tossSubscriptionCheckout.js
- web/classic/src/components/topup/tossTargetRouting.js
- web/classic/src/components/topup/tossWalletAutoRecharge.js

### 관련 테스트

- web/classic/src/components/topup/tossPaymentLifecycle.test.js
- web/classic/src/components/topup/tossSubscriptionCheckout.test.js
- web/classic/src/components/topup/tossTargetRouting.test.js
- web/classic/src/components/topup/tossWalletAutoRecharge.test.js

신규 네 테스트 파일에는 18개의 test 선언이 있다. 최종 실행 검증은 다음 범위로 기록해야 한다.

~~~text
cd web/classic
bun test src/components/topup/tossPaymentLifecycle.test.js +  src/components/topup/tossSubscriptionCheckout.test.js +  src/components/topup/tossTargetRouting.test.js +  src/components/topup/tossWalletAutoRecharge.test.js
bun run build
~~~

### 남은 검증

- 실제 Classic browser에서 popup·redirect·history cleanup smoke test
- organization owner·member 계정별 route 확인
- mobile browser의 self-target callback

---

## C18. 운영·관측·i18n·문서 커밋 그룹

C18은 실제로 아래 네 커밋으로 분리하는 것이 좋다.

### C18a. fix(toss): redact payment identifiers and database bind values

#### 문제

- callback query와 provider error에 authKey, customerKey, paymentKey, orderId, trade number, message가 포함될 수 있었다.
- malformed query가 부분 redaction을 우회할 수 있었다.
- GORM logger가 parameterized SQL의 bind 값을 출력하면 encrypted·plaintext secret evidence가 로그에 남을 수 있었다.
- panic value가 payment row나 provider identifier를 포함할 수 있었다.

#### 수정

- access log의 결제 query key를 case-insensitive하게 redaction한다.
- malformed query는 전체 query를 redaction한다.
- callback path와 raw URL을 안전한 형태로 기록한다.
- model payment log에서 card·easy-pay 정보를 masked subset만 보존한다.
- GORM parameterized query logging을 사용한다.
- payment batch panic은 fixed operational signal만 출력한다.
- provider raw error body·URL을 사용자와 일반 log에 전달하지 않는다.

#### 파일과 테스트

- middleware/logger.go
- middleware/logger_test.go
- model/log.go
- model/main.go
- model/payment_log_redaction_test.go
- service/payment_batch.go
- service/payment_batch_test.go

### C18b. i18n(toss): localize checkout, repair and auto-recharge operations

#### 수정

- backend Toss error·log message key를 en·ko locale에 등록한다.
- Default의 quote 변경, 전체 auto-renew cancel, immediate recharge, wallet 계약 경고, repair·maintenance 문구를 지원 locale에 반영한다.
- Classic의 auto-recharge·auto-renew 문구를 지원 locale에 반영한다.
- Default sync report를 재생성한다.

#### 파일

- i18n/keys.go
- i18n/locales/en.yaml
- i18n/locales/ko.yaml
- web/default/src/i18n/locales/en.json
- web/default/src/i18n/locales/fr.json
- web/default/src/i18n/locales/ja.json
- web/default/src/i18n/locales/kr.json
- web/default/src/i18n/locales/ru.json
- web/default/src/i18n/locales/vi.json
- web/default/src/i18n/locales/zh.json
- web/default/src/i18n/locales/_reports/_sync-report.json
- web/default/src/i18n/locales/_reports/kr.untranslated.json
- web/classic/src/i18n/locales/en.json
- web/classic/src/i18n/locales/fr.json
- web/classic/src/i18n/locales/ja.json
- web/classic/src/i18n/locales/kr.json
- web/classic/src/i18n/locales/ru.json
- web/classic/src/i18n/locales/vi.json
- web/classic/src/i18n/locales/zh-CN.json
- web/classic/src/i18n/locales/zh-TW.json
- web/classic/src/i18n/locales/zh.json

#### 정확한 검증 범위

- Default sync report의 missingCount와 extrasCount는 0이다.
- Toss 변경 key는 지원 locale에 반영됐다.
- 이는 프로젝트 전체 untranslated entry가 0이라는 뜻이 아니다.
- locale diff에는 Toss 외 catalog churn이 섞일 수 있으므로 실제 commit 시 hunk를 분리한다.

### C18c. docs(ops): document secrets, rolling migration and webhook network policy

#### 수정

- persistent CRYPTO_SECRET과 기존 SESSION_SECRET ciphertext 호환 조건을 설명한다.
- TOSS_OPTION_SECRET_ENCRYPTION_ENABLED rolling gate 순서를 설명한다.
- paymentKey-scoped idempotency rollout을 설명한다.
- recurring protocol과 orderId v2 drain acknowledgement를 설명한다.
- master-before-slave migration과 old worker 재기동 금지를 설명한다.
- inbound webhook source와 trusted proxy CIDR을 설명한다.
- outbound 443과 공식 API IP 운영 확인을 설명한다.

#### 파일

- .env.example
- docker-compose.yml
- main.go

자동 테스트는 없으므로 실제 secret이 들어가지 않았는지, 공식 IP가 최신인지, 배포 절차가 현재 코드 env name과 일치하는지 review한다.

### C18d. docs(toss): add official-reference audit and commit handoff

#### 파일

- docs/superpowers/specs/2026-07-12-toss-payments-official-docs-audit-and-hardening.md
- 현재 커밋별 문서 묶음

#### 내용

- 공식 문서 요구사항과 구현 매핑
- P0–P2 이슈와 수정
- 신뢰 경계·불변조건
- 남은 E2E·실 DB·계약 위험
- 배포·repair·rollout 체크리스트
- timeout, webhook, refund, key revocation, crypto key 장애 runbook
- 실제 baseline commit과 현재 logical commit 구분

### C18 공통 rollback 주의

- redaction을 rollback하면 기존 로그 pipeline에서 결제 식별자가 다시 노출될 수 있다.
- env 문서만 코드보다 앞서 배포하거나 반대로 새 gate를 문서 없이 켜면 mixed-version 사고가 날 수 있다.
- locale report 전체 재생성은 Toss-only commit에 불필요한 catalog churn을 섞을 수 있으므로 hunk review가 필요하다.
