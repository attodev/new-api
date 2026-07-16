# Toss Payments 공식 문서 기준 구현 감사 및 하드닝 보고서

- 작성일: 2026-07-12
- 최초 감사 기준: `toss` 브랜치, HEAD 4171f875b 이후 변경
- 현재 리베이스 기준: `origin/team` HEAD 0bf89b004 (2026-07-13)
- 코드 반영 커밋: `2c619a31e`, `28fe73ad5`, `c0f4b5a71`
- 문서 상태: 최종 반복 검토 결과를 반영한 운영 인계용 문서
- 검토 범위: 일반 결제, 개인·조직 지갑 충전, 구독 자동결제, 지갑 자동충전, 취소·환불, 웹훅, 정산 복구, 설정·키 교체, 마이그레이션, 프론트엔드, 테스트

> 이 문서는 기존 설계 문서의 단순 요약이 아니다. Toss Payments 공식 문서와 현재 구현을 다시 대조하면서 실제로 발견한 위험, 위험이 현실화되는 조건, 적용한 수정, 수정 뒤의 불변조건, 남아 있는 운영 책임을 함께 기록한다.
>
> 현재 결론은 다음과 같다. 검토 과정에서 식별된 추가 P0~P2 코드 결함은 반복 감사와 핵심 테스트 범위에서 더 발견되지 않았다. 다만 실 Toss 테스트·라이브 상점 E2E, 실제 MySQL/PostgreSQL 인스턴스의 마이그레이션·락 경합, 상점 계약 상태, 가상계좌 입금 후 환불처럼 코드만으로 종결할 수 없는 검증 항목은 남아 있다.

## 1. 문서의 목적과 판정 기준

결제 연동에서 가장 위험한 실패는 단순한 API 오류가 아니다. 다음과 같이 외부 결제 상태와 로컬 권한 상태가 갈라지는 경우가 핵심 위험이다.

- Toss에서는 결제가 완료됐지만 로컬에서는 실패로 기록되어 고객이 돈을 내고도 충전이나 구독을 받지 못한다.
- 로컬에서는 두 번 정산되어 한 번 결제하고 quota 또는 구독 권한을 두 번 받는다.
- 네트워크 타임아웃 뒤 다른 API 키 또는 다른 멱등키로 다시 POST하여 중복 결제가 생긴다.
- 취소가 먼저 완료됐는데 뒤늦은 callback이나 worker가 quota를 지급한다.
- 관리자가 결제를 비활성화하거나 키를 교체하는 순간에 오래된 API 노드가 이전 설정으로 새 결제를 만든다.
- 손상되거나 부분 복원된 설정, 변경된 가격·요금제, 삭제된 사용자·조직을 기준으로 결제가 진행된다.

이 문서에서는 각 항목을 다음 기준으로 판정한다.

| 상태 | 의미 |
|---|---|
| 해결 | 코드 수정과 회귀 테스트가 모두 반영됨 |
| 안전 정지 | 자동 복구가 위험한 경우 결제·정산을 중단하고 운영 이벤트를 남김 |
| 운영 확인 필요 | 상점 계약, 방화벽, 실제 DB, 실제 Toss 환경처럼 애플리케이션 코드가 증명할 수 없음 |
| 의도적 수동 처리 | 자동 회수·환불이 더 위험해 관리자가 provider evidence를 보고 판단하도록 설계 |

## 2. 기준으로 삼은 Toss Payments 공식 문서

최초 진입점은 사용자가 지정한 [시작하기](https://docs.tosspayments.com/guides/get-started) 문서이며, 그 문서에서 연결되는 결제·빌링·웹훅·취소·보안·API 레퍼런스를 함께 확인했다.

| 공식 문서 | 이번 검토에서 확인한 핵심 요구 |
|---|---|
| [시작하기](https://docs.tosspayments.com/guides/get-started) | 일반 결제와 자동결제의 제품·계약·결제수단 차이 |
| [결제 흐름](https://docs.tosspayments.com/guides/v2/get-started/payment-flow) | 브라우저 인증과 서버 승인의 분리, 서버 저장값과 callback 값 비교 |
| [결제창 연동](https://docs.tosspayments.com/guides/v2/payment-window/integration) | SDK 초기화, customerKey, orderId, amount, successUrl, failUrl |
| [인증 및 기타 헤더](https://docs.tosspayments.com/reference/using-api/authorization) | 시크릿 키 뒤 콜론을 붙인 Basic 인증, HTTPS, 모든 POST의 Idempotency-Key, 최대 300자, 15일 보관, 409 재시도 |
| [자동결제 연동](https://docs.tosspayments.com/guides/v2/billing/integration) | 자동결제 계약 MID, 일회성 authKey, billingKey 발급, billingKey 결제, 비구독 자동결제 제한 |
| [웹훅 연결](https://docs.tosspayments.com/guides/v2/webhook) | MID별 등록, 10초 안의 200 응답, 실패 시 최대 7회·약 3일 19시간 재전송 |
| [웹훅 이벤트](https://docs.tosspayments.com/reference/using-api/webhook-events) | PAYMENT_STATUS_CHANGED, BILLING_DELETED, 결제창 30분, 일반 결제 승인 10분, 일반 결제 웹훅에는 검증 가능한 서명이 없음 |
| [결제 취소](https://docs.tosspayments.com/guides/v2/cancel-payment) | 전액·부분 취소, cancellation transactionKey, 취소 멱등키, 입금된 가상계좌의 refundReceiveAccount |
| [보안](https://docs.tosspayments.com/reference/using-api/security) | HTTPS, TLS 1.2 이상, 443 포트, inbound webhook IP, outbound API IP |
| [API 레퍼런스](https://docs.tosspayments.com/reference) | 승인·조회·빌링·거래·취소 API의 요청 및 Payment 응답 구조 |
| [LLM Quick Reference](https://docs.tosspayments.com/guides/v2/get-started/llms-quick-reference) | 연동 전체의 빠른 교차 점검 |

공식 문서에서 특히 중요한 멱등성 범위는 “멱등키 하나”가 아니다. Toss는 API 키, URL, HTTP 메서드, Idempotency-Key 조합으로 요청을 구분한다. 따라서 같은 주문번호와 같은 멱등키를 사용하더라도 다른 시크릿 키 namespace로 POST하면 동일 요청이라고 가정할 수 없다. 이번 하드닝의 상당 부분이 이 경계를 안전하게 지키는 데 집중되어 있다.

## 3. 현재 지원 범위와 명시적 비지원 범위

### 3.1 지원하는 흐름

- 개인 지갑 일반 Toss 충전
- 조직 소유자에 의한 조직 지갑 일반 Toss 충전
- 국내 카드 기반 구독 자동결제 인증, 첫 결제, 갱신, 해지
- 별도 계약 승인을 전제로 한 지갑 예약·임계치 자동충전
- PAYMENT_STATUS_CHANGED 기반 일반 결제 상태 복구
- BILLING_DELETED 기반 로컬 billing key 상태 반영
- 취소·부분취소 감지와 미지급 결제의 자동 전액 환불 시도
- callback·웹훅 유실을 보완하는 Transaction API reconciliation
- 관리자용 결제 불일치 조회·해결 기록 API

### 3.2 의도적으로 완전 지원하지 않는 흐름

- DEPOSIT_CALLBACK 기반 가상계좌 입금 처리
- 입금된 가상계좌의 무인 자동 환불
- 결제 후 이미 사용된 quota 또는 구독 entitlement의 자동 회수
- Toss 상점의 실제 자동결제·비구독 자동결제 계약 상태를 API로 증명하는 기능
- provider 콘솔까지 포함한 완전 자동 재무 대사

현재 UI는 CARD 결제창만 요청한다. 그러나 변조된 입력이나 과거 데이터가 가상계좌 상태로 나타나더라도 오지급하지 않도록 서버는 상태를 안전 정지시키고 refund_required 운영 이벤트를 남긴다.

## 4. 현재 전체 구조

### 4.1 일반 결제의 신뢰 경계

~~~mermaid
flowchart TD
    A["서버가 최신 설정·가격·대상 확인"] --> B["pending 주문과 가격·quota·MID·secret snapshot 저장"]
    B --> C["브라우저는 Toss SDK 결제창만 호출"]
    C --> D["success callback의 paymentKey·orderId·amount 수신"]
    D --> E["로컬 주문·대상·승인 가능 시간·취소 fence 재검증"]
    E --> F["트랜잭션 안에서 final POST barrier와 durable attempt marker 기록"]
    F --> G["동일 secret·동일 Idempotency-Key로 Toss confirm POST"]
    G --> H{"응답이 명확한가"}
    H -->|"DONE + 전체 identity 일치"| I["row lock/CAS로 quota 1회 지급"]
    H -->|"명확한 사전 거절"| J["안전한 경우에만 binding 해제 또는 종료"]
    H -->|"timeout·409·5xx·불완전 2xx"| K["pending 유지, authoritative GET 및 background reconciliation"]
    K --> I
    K --> L["15일/승인시간 경계 이후 GET-only 또는 운영자 조정"]
~~~

### 4.2 결제 시스템의 권위 순서

1. 브라우저 값은 결제 결과가 아니라 lookup key와 사용자 복귀 신호다.
2. 웹훅 body는 상태 변경 신호이며, 일반 결제는 Toss Payment GET 결과로 다시 확인한다.
3. provider Payment 객체도 로컬 주문의 orderId, paymentKey, amount, currency, type, method 계약과 일치해야 한다.
4. 실제 quota·구독·정책 변경은 DB row lock과 CAS가 성공한 경우에만 한 번 반영한다.
5. 결과가 불명확하면 권한을 지급하거나 새 namespace로 POST하지 않고 pending 또는 reconciliation event로 남긴다.

## 5. 발견 이슈와 수정 결과 요약

| 우선순위 | 발견한 위험 | 가능한 결과 | 적용한 수정 | 현재 상태 |
|---|---|---|---|---|
| P0 | 브라우저가 전달한 paymentKey로 기존 orderId 멱등 namespace 선점 | 실제 결제 승인 방해 또는 다른 결과 재사용 | orderId와 paymentKey를 결합한 durable 멱등키, protocol version, 안전한 binding 복구 | 해결 |
| P0 | timeout·409·5xx·불완전 2xx를 실패로 확정 | 중복 POST 또는 결제됐지만 미지급 | ambiguous outcome 분류, 동일 namespace 재시도, GET 우선, pending 복구 | 해결 |
| P0 | API 키 교체 뒤 현재 키로 과거 POST 재시도 | API-key namespace가 달라져 중복 청구 | 주문 시점 exact credential, client-key fingerprint, 제한된 credential promotion | 해결 |
| P0 | callback·webhook·scheduler 동시 정산 | quota·구독 이중 지급 | row lock, CAS, durable claim, target lock order | 해결 |
| P0 | 취소와 지급이 경쟁 | 취소된 결제에 quota 지급 | cancellation/refund fence를 같은 결제 row lock 아래 확인 | 해결 |
| P0 | 부분취소 후 남은 balance를 완료 처리 | 고객 자금 일부가 provider에 남음 | balance 기반 15일 고정 환불 operation, transactionKey ledger | 해결 |
| P0 | billing ISSUE 성공 응답 유실 | orphan billing key, 중복 발급, 추적 불가 | auth snapshot, exact idempotent replay, claim lease, revocation queue | 해결 |
| P0 | 갱신 때 현재 가격·요금제를 재사용 | 과금·기간·quota가 구매 후 변경 | immutable plan/order/renewal contract snapshot | 해결 |
| P0 | mixed-version scheduler의 다른 orderId 해석 | 같은 주기 중복 청구 | DB protocol fence, opaque recurring ID, explicit drain migration | 해결 |
| P0 | 부분 복원·손상 설정 또는 stale node 사용 | 잘못된 MID·키·가격으로 결제 | 14-key atomic snapshot, HMAC attestation, final POST barrier | 해결 |
| P0 | 설정 repair가 일부 키만 복구하거나 결제를 즉시 활성화 | 혼합 key pair, 복구 도중 신규 청구 | exact 14-key all-disabled repair, HMAC token, maintenance fence | 해결 |
| P1 | legacy MID backfill이 대형 테이블 전체를 긴 transaction으로 잠금 | 서비스 중단·교착·stale migration | PK window, max-ID cutoff, bounded batch, revision CAS | 해결 |
| P1 | 과거 충전 quota를 현재 단가로 재계산 | 과지급·미지급 | immutable Quota 우선, legacy Money 차선, 손상값 fail-closed | 해결 |
| P1 | 일반 webhook을 서명됐다고 가정 | 위조 이벤트 처리 | 공식 IP allowlist, trusted proxy 경계, authoritative GET | 해결 |
| P1 | webhook이 10초를 넘기거나 rate limit에 막힘 | Toss 재전송·최종 유실 | 8초 handler, 9초 write, 1MiB, trusted route rate-limit 예외 | 해결 |
| P1 | callback과 webhook이 모두 유실 | provider·local 영구 불일치 | Transaction API cursor/lease/page checkpoint reconciliation | 해결 |
| P1 | 사용자·조직 상태가 POST 직전에 변경 | 삭제된 대상·권한 없는 조직에 청구 | owner-first lock과 실제 POST 직전 lifecycle 재검증 | 해결 |
| P1 | 비구독 지갑 자동충전을 구독 billing flag로만 허용 | 계약 범위 위반 | 별도 wallet auto-recharge flag와 세 단계 gate | 코드 해결, 계약은 운영 확인 |
| P2 | 전역 insecure TLS·redirect·무제한 body 영향 | secret 노출, 메모리 고갈, 조기 timeout | 전용 TLS 1.2 transport, redirect 금지, body·response 상한 | 해결 |
| P2 | URL·query·provider body·카드 정보 로그 노출 | 결제 식별자·비밀정보 유출 | access log redaction, parameterized DB log, 응답 sanitization | 해결 |
| P2 | React 중복 클릭·StrictMode·unmount | 결제창 중복 생성·orphan iframe | 동기 ref guard, SDK destroy 직렬화, session snapshot 비교 | 해결 |

## 6. 상세 수정 내용

### 6.1 브라우저 callback 위조와 멱등성 namespace 선점

#### 문제

successUrl의 paymentKey, orderId, amount는 브라우저를 거쳐 서버에 도착한다. 이를 권위 있는 결제 결과로 볼 수 없다. 특히 orderId 하나만으로 confirm 멱등키를 만들면 공격자나 잘못된 callback이 가짜 paymentKey를 먼저 결합해 실제 결제와 같은 멱등 결과를 차지할 수 있다.

#### 수정

- 신규 주문 생성 시 멱등 protocol version을 저장한다.
- 신규 confirm 멱등키는 orderId와 paymentKey를 함께 사용해 파생하고 DB에 영속화한다.
- provider POST 전에 paymentKey, exact credential, attempt marker, idempotency key가 같은 transaction에서 확정된다.
- 기존 프로토콜로 이미 POST됐을 가능성이 있는 legacy row는 기존 namespace를 유지한다. 과거 주문의 멱등키를 새 형식으로 임의 전환하지 않는다.
- 명확한 provider 사전 거절과 authoritative GET 404가 함께 확인될 때만 위조 가능 binding을 해제한다.
- 읽을 수 없는 오류 body, 알 수 없는 오류 코드, timeout, 성공 HTTP의 JSON 파싱 실패는 binding을 유지한다.
- 결제 승인 가능 시간이 지난 주문은 새로운 confirm POST를 만들지 않고 GET-only reconciliation으로 전환한다.

#### 핵심 코드

- [model/topup.go](../../../model/topup.go): **CreatePendingTossTopUpCheckout**, **PrepareClaimedPendingTossTopUpConfirm**, **ResetClaimedPendingTossTopUpPaymentBinding**
- [controller/topup_toss.go](../../../controller/topup_toss.go): **TossConfirm**, **recoverRejectedTossConfirmBinding**, **reconcileTossConfirmWithoutProviderPOST**

#### 수정 후 불변조건

- 같은 일반 결제 attempt는 항상 같은 paymentKey, 같은 provider credential, 같은 Idempotency-Key를 사용한다.
- callback 값만으로 quota를 지급하지 않는다.
- 이미 시도한 legacy 주문의 namespace를 편의상 새 버전으로 바꾸지 않는다.

### 6.2 불명확한 provider 결과와 중복 POST

#### 문제

다음 결과는 “결제가 실패했다”는 증거가 아니다.

- TCP 연결 종료 또는 timeout
- HTTP 408, 409, 429, 5xx
- HTTP 2xx이지만 body read 또는 JSON decode 실패
- provider가 새로 추가한 미분류 오류 코드
- POST는 처리됐지만 응답이 애플리케이션에 도착하지 않은 경우

이 상태에서 주문을 실패로 닫고 새 주문·새 키·새 멱등키로 POST하면 중복 청구가 생길 수 있다.

#### 수정

- 오류를 definitive rejection과 ambiguous outcome으로 구분한다.
- ambiguous 결과는 먼저 authoritative GET으로 확인한다.
- GET에서 DONE과 전체 identity가 확인되면 기존 주문을 정산한다.
- 아직 결과가 없으면 pending을 유지하고 cleanup·transaction reconciliation이 이어받는다.
- 409 IDEMPOTENT_REQUEST_PROCESSING은 terminal failure가 아니라 동일 요청을 다시 확인해야 하는 상태로 처리한다.
- money-moving POST는 남은 context 시간이 60초보다 짧으면 아예 시작하지 않는다.
- Toss의 15일 멱등 보장 경계를 넘긴 불명확한 ISSUE·charge·cancel은 새 POST를 자동 생성하지 않는다.

#### 핵심 코드

- [controller/topup_toss.go](../../../controller/topup_toss.go): **isTossAmbiguousPaymentOutcome**, **doTossAPIRequestWithSecretAndMinimumAttemptBudget**, **reconcileTossRecordedTopUp**
- [controller/subscription_payment_toss.go](../../../controller/subscription_payment_toss.go): billing ISSUE·첫 결제 조회 복구
- [model/toss_billing.go](../../../model/toss_billing.go): renewal attempt 복구

#### 안전성·가용성의 선택

새 status나 새 오류 코드가 생기면 기본적으로 지급과 새 POST를 멈춘다. 이 정책은 queue가 늘어날 수 있지만, 알 수 없는 응답을 성공·실패로 임의 해석해 금전 상태를 훼손하는 것보다 안전하다.

### 6.3 API 키 교체와 MID namespace 격리

#### 문제

Toss 멱등성은 API 키까지 포함한다. 과거 주문을 현재 활성 시크릿 키로 무조건 재시도하면 같은 orderId와 Idempotency-Key라도 다른 namespace에서 새 결제가 될 수 있다.

#### 수정

- 모든 신규 결제·구독 주문·billing key·wallet 정책에 주문 시점의 exact secret을 암호화 저장한다.
- client key의 SHA-256 fingerprint를 MID namespace 증거로 저장한다.
- fingerprint가 비어 있거나 형식이 잘못됐으면 같은 MID의 증거로 인정하지 않는다.
- 일반 confirm POST는 주문에 저장된 exact credential만 사용한다. 현재 같은 fingerprint의 키는 GET 확인에만 보조적으로 사용할 수 있다.
- 갱신·첫 결제·wallet charge는 provider가 기존 credential을 명확히 거절한 경우에만 다음 same-MID credential을 DB에 승격한다.
- credential 승격은 attempt marker를 false로 되돌린다. 다음 루프는 final operational gate를 다시 통과해야만 POST할 수 있다.
- 취소는 balance가 단조 감소하는 operation이라는 별도 성질이 있으므로, 명확한 credential 거절 뒤 같은 MID가 증명된 현재 키로 고정된 refund operation을 이어갈 수 있다.
- legacy row의 fingerprint는 키 교체 전에 bounded batch와 CAS로 backfill한다. 추론할 수 없는 row는 exact stored credential만 유지한다.

#### 핵심 코드

- [model/toss_client_fingerprint.go](../../../model/toss_client_fingerprint.go)
- [model/toss_billing.go](../../../model/toss_billing.go): **backfillLegacyTossProviderMIDFingerprintsBatched**, credential promotion
- [model/wallet_auto_recharge.go](../../../model/wallet_auto_recharge.go): wallet credential promotion
- [controller/topup_toss.go](../../../controller/topup_toss.go): **tossCredentialCandidatesForMID**

### 6.4 결제 설정의 원자성, 암호화, attestation

#### 문제

Toss 설정은 client key와 secret key만의 문제가 아니다. 활성화 flag, test/live 선택, billing 전용 pair, 단가, 최소 금액이 한 세대로 함께 움직여야 한다. 여러 API 노드가 일부 값만 저장하거나 DB가 부분 복원되면 다음과 같은 조합이 생길 수 있다.

- live client key + test secret key
- 새 client key + 이전 secret key
- 결제 활성화 true + 비어 있는 active key pair
- DB는 새 설정인데 process memory는 이전 설정
- ciphertext 일부 손상 또는 14개 row 중 일부 누락

#### 수정

결제에 영향을 주는 14개 옵션을 하나의 원자적 설정 세대로 관리한다.

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

세부 방어선은 다음과 같다.

- 일반 옵션 수정 API로 Toss 옵션 또는 내부 revision·maintenance key를 변경할 수 없다.
- client/secret pair는 반드시 한 요청에서 함께 수정한다.
- 공식 API 개별 연동 키 형태인 test_ck/test_sk 또는 live_ck/live_sk 조합을 요구한다.
- widget key, client·secret 위치 반전, test/live 혼합을 거부한다.
- 네 개 secret option은 AES-GCM enc:v1 envelope로 저장한다.
- option key를 AAD로 묶어 ciphertext를 다른 secret slot으로 옮길 수 없게 한다.
- 전체 설정 digest와 revision을 toss:v2 attestation에 묶는다.
- quarantine 상태는 toss:v2q로 구분하며 신규 결제를 fail-closed한다.
- DB revision, 14개 row digest, process runtime snapshot이 모두 일치해야 provider POST가 가능하다.

#### rolling encryption gate

**TOSS_OPTION_SECRET_ENCRYPTION_ENABLED**는 모든 노드가 enc:v1을 읽을 수 있게 배포된 뒤에만 fleet 전체에서 활성화한다. mixed-version 동안 먼저 켜면 구버전 노드가 암호문을 평문 secret으로 오해할 수 있다.

#### 핵심 코드

- [setting/payment_toss.go](../../../setting/payment_toss.go)
- [model/toss_option_secret.go](../../../model/toss_option_secret.go)
- [model/option.go](../../../model/option.go)
- [controller/option.go](../../../controller/option.go)

### 6.5 설정 repair와 유지보수 fence

#### 문제

손상된 ciphertext, 누락된 row, attestation이 없는 legacy generation은 일부 필드만 수정해서 안전하게 복구할 수 없다. 또한 repair 직후 legacy MID·renewal contract migration이 끝나기 전에 결제를 다시 켜면 새 설정과 과거 주문의 namespace가 섞일 수 있다.

#### 수정

- 관리자에게 secret, client key, digest를 노출하지 않고 repairRequired, maintenanceRequired, 불투명한 HMAC repair token만 반환한다.
- repair token은 관리자가 확인한 정확한 broken generation에 묶인다.
- repair는 14개 전체를 한 번에 제출해야 한다.
- 세 개 결제 mode는 모두 false여야 한다.
- 빈 secret 필드는 기존 값을 유지하는 것이 아니라 영구 삭제이므로 UI에서 명시적으로 경고하고 확인을 받는다.
- 새 attested generation과 maintenance fence를 같은 transaction에서 설치한다.
- MID fingerprint·renewal contract migration이 완료돼야 fence를 해제한다.
- maintenance 재시도 요청은 secret을 다시 받지 않는다.
- maintenance 동안 enable, key 변경, plan 변경, 신규 provider POST를 차단한다.
- 긴급 false-only disable은 허용한다.
- startup에서 repair 필요 상태가 발견돼도 전체 애플리케이션을 종료하지 않고 Toss 결제만 안전 정지한다.

~~~mermaid
flowchart TD
    A["누락·손상·미검증 Toss 설정 발견"] --> B["모든 Toss 기능 fail-closed"]
    B --> C["관리 UI에 repairRequired + HMAC token만 노출"]
    C --> D["관리자가 14개 전체를 all-disabled로 제출"]
    D --> E["새 attestation + maintenance fence 원자 저장"]
    E --> F["bounded MID fingerprint migration"]
    F --> G["bounded renewal contract migration"]
    G --> H{"미해결 legacy row가 있는가"}
    H -->|"예"| I["maintenance 유지, secret 없는 재시도"]
    H -->|"아니오"| J["fence 해제"]
    J --> K["관리자가 mode를 단계적으로 활성화"]
~~~

#### 핵심 코드와 UI

- [model/toss_option_secret.go](../../../model/toss_option_secret.go): **GetTossConfigState**
- [model/option.go](../../../model/option.go): **RepairTossOptionsBulk**, **CompleteTossConfigurationMaintenance**
- [controller/option.go](../../../controller/option.go): **UpdateTossOptions**
- [web/default payment-settings-section.tsx](../../../web/default/src/features/system-settings/integrations/payment-settings-section.tsx)
- [web/default toss-option-updates.ts](../../../web/default/src/features/system-settings/integrations/toss-option-updates.ts)

### 6.6 설정·대상 변경과 provider POST 사이의 TOCTOU

#### 문제

요청 시작 시점의 빠른 in-memory 체크만으로는 충분하지 않다. 체크 뒤 실제 POST 전까지 관리자가 기능을 끄거나 repair fence를 설치할 수 있고, 사용자·조직 상태가 바뀔 수도 있다.

#### 수정

두 겹의 fresh gate를 적용했다.

- 브라우저용 새 세션을 만들기 전에 DB의 최신 Toss snapshot을 다시 읽는다.
- provider POST 직전 attempt marker를 쓰는 transaction 안에서 최종 barrier를 수행한다.

최종 barrier는 동일한 option write-lock row를 잠그고 다음을 검사한다.

- 14개 row가 모두 존재하는지
- revision attestation과 실제 digest가 일치하는지
- runtime snapshot이 DB revision을 따라잡았는지
- maintenance fence가 없는지
- 결제 compliance가 유효한지
- 일반 결제, billing, wallet auto-recharge 각각의 enable flag가 유효한지
- 미완료 legacy renewal contract가 없는지

적용된 provider POST 경계는 여섯 개다.

1. 일반 결제 confirm
2. subscription billing key ISSUE
3. subscription 첫 charge
4. subscription renewal charge
5. wallet billing key ISSUE
6. wallet auto-recharge charge

이미 정확한 namespace로 provider에 전송된 attempt는 기능을 끈 뒤에도 조회·동일 멱등 replay·로컬 정산·billing key 삭제가 가능하다. kill switch가 이미 결제된 돈을 숨겨서는 안 되기 때문이다.

#### 핵심 코드

- [model/option.go](../../../model/option.go): **inspectTossProviderPOSTBarrierTx**
- [controller/payment_webhook_availability.go](../../../controller/payment_webhook_availability.go)
- [model/toss_target_lifecycle.go](../../../model/toss_target_lifecycle.go)

### 6.7 일반 결제 금액과 quota snapshot

#### 문제

Toss의 Amount는 KRW 청구액이고 내부 권한은 quota다. 두 값을 같은 단위로 처리하면 큰 과지급이 가능하다. 또한 과거 주문을 현재 TossUnitPrice 또는 현재 QuotaPerUnit으로 다시 계산하면 단가 변경 뒤 지급량이 달라진다.

#### 수정

- 주문 생성 시 서버가 KRW amount, 내부 Money, 최종 Quota를 함께 계산해 저장한다.
- 신규 주문은 immutable Quota를 최우선 정산 기준으로 사용한다.
- legacy 주문에서 Quota가 0이고 Money가 0이 아니면 저장된 Money snapshot을 사용한다.
- Quota와 Money가 모두 없는 가장 오래된 row만 Amount와 현재 환산값을 제한적으로 사용한다.
- nonzero Quota가 음수·overflow·허용범위 밖이면 Money나 Amount로 fallback하지 않고 fail-closed한다.
- nonzero legacy Money가 NaN, Infinity, 음수, overflow이면 fail-closed한다.
- response의 orderId, paymentKey, amount, currency, type, method 계약을 전부 확인한 뒤 지급한다.

일반 SDK CARD 창은 간편결제와 연결계좌 가능성을 포함하므로 일반 충전 최소 금액은 보수적으로 200원이다. 카드 전용 billing은 100원이다. 애플리케이션 상한은 32-bit signed integer 범위로 두어 quota·가격 변환 overflow를 막는다.

#### 남은 legacy 한계

Quota snapshot이 없고 Money만 있는 과거 row는 저장 당시의 QuotaPerUnit까지 복원할 수 없다. 새 주문은 Quota를 저장하므로 이 문제에서 분리된다. Money조차 0인 매우 오래된 row의 현재 값 fallback은 운영 대사 대상이다.

#### 핵심 코드

- [setting/payment_toss.go](../../../setting/payment_toss.go)
- [model/topup.go](../../../model/topup.go): **CreditedQuotaForTossTopUp**, Toss settlement
- [controller/topup_toss.go](../../../controller/topup_toss.go): quote·checkout·Payment 검증

### 6.8 중복 정산, 구매 제한, 사용자·조직 lifecycle

#### 문제

callback, webhook, stale cleanup, renewal worker, transaction scanner가 동시에 같은 결제를 처리할 수 있다. 조직 소유권이나 사용자 상태가 결제 도중 바뀔 수도 있다. 두 개의 구독 checkout이 동시에 완료되면 MaxPurchasePerUser를 초과할 수 있다.

#### 수정

- 사용자 또는 조직 owner row를 먼저 잠그고 결제 row를 잠그는 일관된 lock order를 사용한다.
- quota, subscription, wallet policy 변경과 결제 상태 전이를 하나의 transaction과 CAS로 묶는다.
- paymentKey는 다른 주문에 재결합할 수 없다.
- pending subscription order를 구매 제한 reservation으로 포함한다.
- provider 시도 증거가 전혀 없는 오래된 reservation만 안전하게 해제한다.
- unresolved payment·partial cancellation·refund fence가 있으면 대체 checkout을 막는다.
- 조직 충전은 실제 POST와 정산 직전에 owner, membership, organization enabled 상태를 다시 확인한다.
- 사용자·조직 삭제 또는 membership 전환은 provider activity와 미해결 payment event가 있으면 차단하거나 pristine order만 만료한다.

#### 핵심 코드

- [model/topup.go](../../../model/topup.go)
- [model/subscription.go](../../../model/subscription.go)
- [model/organization.go](../../../model/organization.go)
- [model/user.go](../../../model/user.go)
- [model/wallet_auto_recharge.go](../../../model/wallet_auto_recharge.go)

### 6.9 취소, 부분취소, 자동 환불 fence

#### 문제

결제는 Toss에서 완료됐지만 로컬 지급 계약이 깨진 경우에는 고객 돈을 그대로 두거나 quota를 억지로 지급해서는 안 된다. 자동 취소도 timeout 뒤 새 키로 반복하면 중복 operation이 될 수 있다. 부분취소가 됐는데 남은 balance를 무시하면 고객 자금 일부가 provider에 남는다.

#### 수정

- 지급할 수 없는 인증된 결제에 refund_required event를 저장한다.
- refund_required는 단순 운영 메모가 아니라 모든 fulfillment를 막는 provider-write fence다.
- TopUp row와 event row를 같은 순서로 잠가 settlement·refund worker 간 경합을 직렬화한다.
- 환불 operation에 Idempotency-Key, 최초 시도 시각, 대상 remaining balance를 영속화한다.
- 같은 balance는 Toss의 15일 멱등 보장 동안 같은 key를 재사용한다.
- provider balance 감소가 확인되면 이전 취소 성공의 증거로 보고 다음 balance용 operation을 만든다.
- balance가 증가하면 stale·손상 observation으로 간주하고 새 POST를 거부한다.
- 각 cancellation transactionKey와 cancelAmount를 별도 ledger로 저장해 webhook 재전송과 부분취소 누적을 중복 계산하지 않는다.
- 취소 API의 2xx 응답도 요청한 paymentKey, orderId, type, totalAmount, currency와 일치해야 한다.
- CANCELED 또는 PARTIAL_CANCELED evidence가 settlement보다 먼저 잠금을 얻으면 뒤 지급을 차단한다.
- 이미 quota가 지급된 뒤의 취소는 자동 clawback하지 않고 reconciliation event로 남긴다.

#### 가상계좌

입금된 가상계좌 취소에는 refundReceiveAccount가 필요하다. 현재 서비스는 구매자의 환불 계좌를 저장하지 않으며 DEPOSIT_CALLBACK도 구현하지 않는다. 따라서 이 경우 자동 취소를 반복하지 않고 수동 환불이 필요한 event로 유지한다. 또한 Toss가 취소를 접수한 것과 은행 환불 완료는 다르므로 bank refund status 증거 없이 full refund 완료로 닫지 않는다.

#### 핵심 코드

- [model/toss_payment_event.go](../../../model/toss_payment_event.go): **PrepareTossTopUpRefundOperationWithContext**, cancellation ledger
- [controller/topup_toss.go](../../../controller/topup_toss.go): **cancelRequiredTossTopUp**, refund finalization
- [controller/toss_payment_event_admin.go](../../../controller/toss_payment_event_admin.go)

### 6.10 billing key ISSUE, 첫 결제, 키 삭제 lifecycle

#### 문제

authKey는 일회성이고 ISSUE 결과로 받은 billingKey를 나중에 다시 조회하는 API가 없다. ISSUE가 성공했지만 응답을 잃으면 provider에는 key가 생겼는데 로컬은 모를 수 있다. 첫 charge도 provider 성공 뒤 로컬 transaction이 실패할 수 있다.

#### 수정

- authKey, customerKey, exact provider credential을 암호화 snapshot으로 저장한다.
- authKey hash와 billing issue idempotency key, attempted flag, claim token을 저장한다.
- same snapshot과 same namespace로 ISSUE 응답을 복구한다.
- 발급 응답에서 billing key identity만 확인되면 나머지 metadata가 불완전해도 cleanup 가능한 상태로 보존한다.
- 사용자 비활성화·예약 만료 뒤 회수된 ISSUE 결과는 attach·charge하지 않고 revocation queue로 보낸다.
- stale cleanup worker는 claim token과 CAS를 사용해 새 owner의 active key를 삭제할 수 없다.
- 첫 charge도 billing key, exact secret, attempt marker를 provider POST 전에 영속화한다.
- billing key 상태를 active, pending_revocation, revoked로 관리한다.
- 키를 공유하는 활성 subscription·wallet reference가 있으면 원격 DELETE하지 않는다.
- BILLING_DELETED는 local encrypted plaintext와 hash를 정확히 대조한 뒤 관련 subscription·wallet을 중단한다.
- 복호화 불가 또는 identity 불명확 시 웹훅을 200으로 삼키지 않고 503으로 재시도시킨다.

#### 핵심 코드

- [controller/subscription_payment_toss.go](../../../controller/subscription_payment_toss.go)
- [model/toss_billing.go](../../../model/toss_billing.go)
- [model/subscription.go](../../../model/subscription.go)
- [controller/wallet_auto_recharge.go](../../../controller/wallet_auto_recharge.go)
- [model/wallet_auto_recharge.go](../../../model/wallet_auto_recharge.go)

### 6.11 구독 가격·기간 snapshot과 갱신

#### 문제

갱신 때 현재 plan과 현재 TossUnitPrice를 읽으면 구매 이후 관리자가 변경한 가격, quota, 기간, reset 정책, group이 기존 고객에게 소급된다. 동일 주기 worker 재시도는 중복 청구 위험도 있다.

#### 수정

- 최초 checkout 주문에 plan의 모든 상업 조건과 provider KRW amount를 immutable snapshot으로 저장한다.
- 구독 생성 시 이 snapshot을 write-once TossRenewalContractSnapshot으로 복사한다.
- 갱신 주문은 현재 plan이 아니라 계약 snapshot에서 생성한다.
- 구독 ID, billing time, attempt로 논리적 갱신 identity를 만든다.
- provider-facing 신규 orderId는 opaque protocol을 사용하고 DB unique identity로 충돌을 막는다.
- 같은 주기에는 동일한 논리 attempt와 idempotency namespace를 사용한다.
- 실패 횟수는 최대 3회이며, 24시간 operational grace를 넘긴 오래된 갱신은 장애 복구 뒤 갑자기 청구하지 않는다.
- 반복 주기는 최소 1시간 이상이어야 한다.
- legacy 구독은 배포 시점의 검증 가능한 계약을 한 번 동결한다. 안전하게 계산할 수 없으면 auto-renew를 비활성화한다.
- migration 중 plan create, update, status 변경을 option maintenance lock으로 막는다.

#### 핵심 코드

- [model/subscription_plan_snapshot.go](../../../model/subscription_plan_snapshot.go)
- [model/toss_billing.go](../../../model/toss_billing.go)
- [model/subscription.go](../../../model/subscription.go): **RunSubscriptionPlanMutationWithTossBarrier**
- [model/toss_recurring_order_id_protocol.go](../../../model/toss_recurring_order_id_protocol.go)
- [model/toss_recurring_migration_guard.go](../../../model/toss_recurring_migration_guard.go)

### 6.12 지갑 자동충전

#### 문제

Toss 공식 문서는 자동결제를 구독형 결제로 설명하고 비구독 자동결제는 별도 정책 검토가 필요하다고 안내한다. 구독 billing 활성화만으로 지갑 자동충전을 켜면 계약 범위를 넘을 수 있다. 또한 사용자가 확인한 preset과 ISSUE·charge 직전 DB 값이 다를 수 있다.

#### 수정

- TossBillingEnabled와 별도로 TossWalletAutoRechargeEnabled를 둔다.
- 신규 browser session, billing ISSUE, 실제 wallet charge 세 경계에서 각각 flag와 compliance를 다시 검사한다.
- preset의 금액, quota, 일정, 임계치, cooldown, daily limit 등 재무 조건 전체를 fingerprint로 묶는다.
- 브라우저가 확인한 fingerprint와 row lock 아래 현재 preset을 비교한다.
- billing key 발급 직후 선택적으로 test-period 첫 charge를 검증할 수 있다.
- 사용자·조직 owner 상태, schedule/threshold, last charge, daily limit를 provider POST 직전에 다시 검사한다.
- renewal과 동일하게 deterministic logical identity, opaque provider orderId, exact attempt credential을 사용한다.
- 정책 취소·대상 삭제·billing key 삭제 시 새 charge를 막고 원격 key cleanup을 이어간다.

별도 flag는 계약을 증명하지 않는다. 운영자는 Toss와 비구독 지갑 자동충전 사용 승인을 별도로 확인해야 한다.

#### 핵심 코드

- [model/wallet_auto_recharge_preset.go](../../../model/wallet_auto_recharge_preset.go)
- [model/wallet_auto_recharge.go](../../../model/wallet_auto_recharge.go)
- [controller/wallet_auto_recharge.go](../../../controller/wallet_auto_recharge.go)
- [web/default wallet-auto-recharge-session.ts](../../../web/default/src/features/wallet/lib/wallet-auto-recharge-session.ts)

### 6.13 unsigned 일반 webhook 보안과 10초 응답

#### 문제

일반 PAYMENT_STATUS_CHANGED와 BILLING_DELETED는 payout.changed·seller.changed처럼 검증 가능한 signature를 제공하지 않는다. 임의 X-Forwarded-For를 믿으면 source IP 제한도 우회된다. 반대로 공용 API rate limit과 gzip wrapper, 긴 DB wait가 정상 Toss webhook을 10초 안에 처리하지 못하게 할 수 있다.

#### 수정

- Toss가 공식 게시한 inbound IP만 route 진입을 허용한다.
- direct RemoteAddr를 우선 사용한다.
- X-Forwarded-For는 direct peer가 TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS에 속할 때만 역방향으로 해석한다.
- 신뢰된 proxy chain의 첫 비신뢰 hop만 source로 사용한다.
- TOSS_WEBHOOK_SOURCE_CIDRS로 공식 문서 변경 직후 신규 inbound IP를 임시 추가할 수 있다.
- 정확한 POST /api/toss/webhook과 신뢰된 source 조합만 global end-user rate limit에서 제외한다.
- handler가 event별 source check를 다시 수행한다.
- request body는 1MiB로 제한한다.
- whole-handler context는 8초, response write는 9초 안으로 제한한다.
- Toss webhook route는 gzip wrapper를 우회해 socket deadline 제어를 보존한다.
- SQLite 요청은 장시간 busy wait가 10초 budget을 소비하지 않게 조정한다.
- PAYMENT_STATUS_CHANGED body는 신뢰하지 않고 Payment GET을 수행한다.
- transient provider·DB 오류와 아직 provider 상태가 따라오지 않은 취소는 503을 반환해 Toss 재전송을 요청한다.
- 지원하지 않는 event는 200으로 종료하되 DEPOSIT_CALLBACK은 지원된 것으로 오해하지 않게 명시한다.

#### 핵심 코드

- [controller/toss_webhook_security.go](../../../controller/toss_webhook_security.go)
- [router/api-router.go](../../../router/api-router.go)
- [controller/topup_toss.go](../../../controller/topup_toss.go): webhook middleware·handler
- [middleware/rate-limit.go](../../../middleware/rate-limit.go)
- [model/db_time.go](../../../model/db_time.go)

### 6.14 Transaction API reconciliation

#### 문제

브라우저 callback은 닫힐 수 있고 웹훅도 유한 횟수만 재전송된다. 단순 시간 cursor는 페이지 중간 실패 시 거래를 건너뛸 수 있으며, 한 건의 영구 손상 row가 같은 MID의 모든 후속 거래를 막을 수 있다.

#### 수정

- 한 시간마다 Toss Transaction API를 조회한다.
- credential source별 high-water cursor를 DB에 저장한다.
- 24시간 window, 1분 overlap으로 경계 누락을 줄인다.
- startingAfter page cursor를 별도 영속화한다.
- page의 local side effect가 적용된 뒤에만 checkpoint를 전진한다.
- 최근 7일 lane과 historical lane을 분리해 오래된 backlog가 최신 cancellation을 굶기지 않게 한다.
- 기본 bootstrap은 30일이지만, 해당 credential로 저장된 가장 오래된 로컬 주문 증거가 더 오래되면 그 시각까지 확장한다.
- test key 환경은 Toss의 최근 3일 조회 제한 안쪽으로 clamp한다.
- source마다 25분 DB lease를 사용해 여러 master가 같은 page를 동시에 처리하지 않는다.
- window당 page size는 1000, 최대 20 page, 한 run에서 historical window 최대 3개로 제한한다.
- MID fingerprint 또는 exact encrypted legacy credential이 일치해야 거래를 로컬 주문에 적용한다.
- 불변 snapshot·identity 불일치처럼 재시도로 해결되지 않는 row는 provider evidence를 reconciliation event로 dead-letter한 뒤 다음 row로 진행한다.
- provider·DB 오류, malformed response, 아직 종결되지 않은 상태는 cursor를 멈추고 재시도한다.

#### 핵심 코드

- [controller/toss_transaction_reconciliation.go](../../../controller/toss_transaction_reconciliation.go)
- [model/toss_transaction_reconciliation.go](../../../model/toss_transaction_reconciliation.go)

### 6.15 bounded migration과 세 DB 방언

#### 문제

수백만 row를 가진 테이블에서 invalid fingerprint 전체를 한 transaction으로 scan·update하면 option write lock이 오래 유지되고 서비스가 멈출 수 있다. migration 도중 키 설정이 바뀌면 일부 row만 이전 generation을 기준으로 backfill될 수도 있다.

#### 수정

- migration 시작 시 max ID cutoff를 고정한다.
- primary-key window를 100개 단위로 전진한다.
- fingerprint invalid 후보는 DB 방언별 predicate로 prefilter한다.
- 최종 형식 검사는 Go byte-exact validation으로 다시 수행한다.
- write batch마다 option revision과 maintenance fence를 다시 잠그고 확인한다.
- encrypted credential과 기존 hash를 CAS 조건에 포함한다.
- 잘못된 ciphertext·빈 credential·다중 key에 매칭되는 row는 추론하지 않고 unresolved로 남긴다.
- renewal contract migration도 bounded batch와 max-ID cutoff, revision pin을 사용한다.
- SQLite transaction 안에서 global DB를 다시 조회해 deadlock이 나지 않도록 transaction-scoped Migrator를 사용한다.

PostgreSQL, MySQL, SQLite용 SQL 분기와 단위 테스트는 존재한다. 그러나 이번 로컬 검증은 실제 MySQL·PostgreSQL 서버에서 migration과 lock 경합을 실행한 것은 아니다. 운영 전 staging에서 별도 확인해야 한다.

#### 핵심 코드

- [model/toss_billing.go](../../../model/toss_billing.go): batched MID·renewal migration
- [model/toss_maintenance_batch.go](../../../model/toss_maintenance_batch.go)
- [model/option.go](../../../model/option.go)

### 6.16 HTTP transport, body, 입력, 로그 보안

#### 적용한 방어선

- Authorization은 Base64(secret + colon) 형태의 Basic 인증을 사용한다.
- Toss 전용 transport는 TLS 1.2 이상을 강제한다.
- 전역 TLS_INSECURE_SKIP_VERIFY 설정을 Toss 요청에 상속하지 않는다.
- redirect를 따라가며 Authorization header가 다른 origin으로 전달되는 것을 막는다.
- payment processing context에는 hard timeout이 있고 browser cancellation과 분리한다.
- provider response body는 2MiB로 제한한다.
- 일반 결제 요청 body는 64KiB, webhook body는 1MiB로 제한한다.
- JSON 하나 뒤 trailing data가 있는 요청을 거부한다.
- orderId, paymentKey, billingKey, customerKey, transactionKey의 길이와 문자 범위를 확인한다.
- provider raw error body와 full request URL을 사용자에게 반환하지 않는다.
- access log에서 authKey, customerKey, paymentKey, orderId, trade number, message를 redaction한다.
- malformed query는 전체 query를 redaction한다.
- GORM은 parameterized query logging을 사용해 bind된 secret을 출력하지 않는다.
- 카드 응답은 전체 번호를 저장하지 않고 masked last four, 카드사, 간편결제 제공자 수준으로 sanitization한다.

#### 핵심 코드

- [controller/topup_toss.go](../../../controller/topup_toss.go)
- [controller/toss_request_body.go](../../../controller/toss_request_body.go)
- [middleware/logger.go](../../../middleware/logger.go)
- [model/main.go](../../../model/main.go)

### 6.17 프론트엔드 안전성

#### 적용한 수정

- SDK v2의 payment 인스턴스와 CARD amount object를 사용한다.
- billing은 requestBillingAuth CARD 흐름을 사용한다.
- 브라우저용 customerKey를 2~50자 범위에서 검증한다.
- 서버가 반환한 orderId, amount, callback origin·path를 검증한다.
- cross-origin callback 구성에서는 iframe에 머물지 않고 self target으로 이동한다.
- React state보다 빠른 synchronous ref로 double-click을 차단한다.
- StrictMode replay, component unmount, 재시도 시 SDK iframe destroy를 직렬화한다.
- 사용자가 본 일반 결제 quote와 서버 session의 immutable snapshot을 비교한다.
- 구독 plan fingerprint와 wallet preset fingerprint가 바뀌면 결제창을 열지 않고 pending reservation을 취소한 뒤 재확인을 요구한다.
- sessionStorage에 pending lifecycle을 보존해 브라우저 복귀·취소·새로고침을 처리한다.
- 브라우저 오류만으로 provider 결과가 불명확한 서버 주문을 삭제하지 않는다.
- 개인 지갑과 조직 지갑 route를 분리하고 비소유자의 조직 결제를 차단한다.
- Default와 Classic 테마에 같은 안전 흐름을 반영했다.

#### 핵심 코드

- [web/default use-toss-payment.ts](../../../web/default/src/features/wallet/hooks/use-toss-payment.ts)
- [web/default toss-payment-lifecycle.ts](../../../web/default/src/features/wallet/lib/toss-payment-lifecycle.ts)
- [web/default use-toss-billing.ts](../../../web/default/src/features/subscriptions/hooks/use-toss-billing.ts)
- [web/default toss-checkout.ts](../../../web/default/src/features/subscriptions/lib/toss-checkout.ts)
- [web/classic tossPaymentLifecycle.js](../../../web/classic/src/components/topup/tossPaymentLifecycle.js)
- [web/classic tossSubscriptionCheckout.js](../../../web/classic/src/components/topup/tossSubscriptionCheckout.js)
- [web/classic tossWalletAutoRecharge.js](../../../web/classic/src/components/topup/tossWalletAutoRecharge.js)

### 6.18 scheduler, cleanup, reconciliation queue

#### 적용한 수정

- 일반 paymentKey 복구는 15초 tick으로 빠르게 시작한다.
- provider claim은 DB clock을 사용하고 일반 confirm의 crashed owner lease는 짧게 유지한다.
- paymentKey가 전혀 없는 abandoned checkout은 합법적 30분 창과 10분 승인 시간을 고려해 50분 뒤 만료한다.
- subscription billing ISSUE·charge claim은 5분부터 복구하고 pending session은 50분 상한을 둔다.
- billing과 wallet scheduler는 1분 tick을 사용한다.
- top-up recovery와 subscription cleanup guard를 분리해 느린 queue가 다른 queue를 막지 않게 한다.
- worker 수만큼만 row를 예약하고 SQLite에서는 더 작은 동시성을 사용한다.
- payment batch callback panic을 격리해 한 row의 panic이 나머지 worker와 다음 tick을 중단하지 않게 한다.
- 모든 due·cutoff 비교는 application node clock이 아니라 DB clock을 기준으로 한다.
- 24시간 operational grace를 넘긴 subscription·wallet charge는 장기 장애 뒤 소급 청구하지 않는다.

#### 핵심 코드

- [service/toss_pending_cleanup_task.go](../../../service/toss_pending_cleanup_task.go)
- [service/toss_billing_task.go](../../../service/toss_billing_task.go)
- [service/wallet_auto_recharge_task.go](../../../service/wallet_auto_recharge_task.go)
- [service/payment_batch.go](../../../service/payment_batch.go)

## 7. 운영자 reconciliation event

자동으로 단정하면 위험한 상황은 durable event로 남긴다.

| event type | 의미 | 자동 처리 정책 |
|---|---|---|
| cancellation | provider에서 전액·부분 취소 evidence 확인 | 미지급이면 지급 차단, 지급 후면 관리자 대사 |
| paid_pending_fulfillment | provider 결제는 확인됐지만 local 지급이 끝나지 않음 | 안전한 조건에서 재정산, 불일치면 유지 |
| financial_mismatch | amount, currency, balance, identity 등 재무 계약 불일치 | 자동 수정하지 않음 |
| refund_required | 결제됐지만 local 지급이 금지되어 provider 환불 필요 | full refund evidence 전까지 지급 fence 유지 |

관리자 API:

- GET /api/admin/toss/reconciliation-events
- GET /api/admin/toss/reconciliation-events/:id
- POST /api/admin/toss/reconciliation-events/:id/resolve

resolve 기록에는 관리자, 시각, note가 남는다. 다만 refund_required는 운영자가 메모만 남겨 임의 해제할 수 없으며 실제 full refund evidence로만 닫힌다.

관련 코드:

- [model/toss_payment_event.go](../../../model/toss_payment_event.go)
- [controller/toss_payment_event_admin.go](../../../controller/toss_payment_event_admin.go)
- [router/api-router.go](../../../router/api-router.go)

## 8. 남아 있는 잠재 이슈와 운영 책임

현재 반복 코드 감사에서 추가 P0~P2 결함을 발견하지 못했더라도 아래 항목은 배포 전에 반드시 확인해야 한다.

### 8.1 실제 Toss sandbox·live E2E 미실행

이번 검증 환경에서는 실제 Toss 테스트 상점 카드 인증, 실제 webhook delivery, provider 콘솔 대사, 라이브 소액 결제를 수행하지 않았다. mock과 단위·통합 테스트가 API 경계를 검증하지만, 상점 계약·MID·API version·콘솔 설정까지 증명하지는 않는다.

권장:

- test key로 성공, 사용자 취소, 승인 실패, duplicate callback, timeout, 409, billing ISSUE, 첫 결제, 갱신, billing delete를 실행한다.
- test/live, 일반/billing 각 MID의 API version과 webhook 등록을 별도로 확인한다.
- 라이브 활성화 전 최소 금액 소액 결제와 즉시 취소를 운영 담당자 입회 아래 대사한다.

### 8.2 자동결제 계약과 비구독 wallet auto-recharge

코드는 키 prefix와 별도 enable flag를 확인하지만 해당 MID가 실제 계약상 자동결제·비구독 충전을 허용하는지는 알 수 없다. 특히 지갑 자동충전은 Toss와 별도 리스크 검토를 마쳤는지 확인해야 한다.

### 8.3 CRYPTO_SECRET 또는 SESSION_SECRET 분실

영구 암호화 키가 바뀌거나 노드마다 다르면 다음 데이터를 복호화할 수 없다.

- billing key
- order-time provider credential
- billing ISSUE authKey
- enc:v1 Toss option secret

DB 백업만으로는 복구되지 않는다. 암호화 키를 별도 안전 저장소에 백업하고 모든 노드가 동일한 값을 사용해야 한다. 기존 ciphertext가 있는 상태에서 별도 re-encryption migration 없이 키만 교체하면 안 된다.

### 8.4 webhook IP와 proxy 설정 변경

일반 webhook 보안은 signature가 아니라 IP allowlist와 authoritative GET 조합이다. Toss가 IP를 추가했는데 allowlist가 갱신되지 않거나 reverse proxy CIDR을 잘못 설정하면 정상 webhook이 403으로 차단된다.

- 공식 inbound 목록을 정기적으로 확인한다.
- 앱 업데이트 전 임시 추가가 필요하면 TOSS_WEBHOOK_SOURCE_CIDRS를 좁게 설정한다.
- TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS에는 실제 app 직전 proxy만 넣는다.
- 임의 인터넷 대역을 trusted proxy로 넣지 않는다.
- outbound ACL은 앱이 자동 설정하지 않으므로 최신 공식 api.tosspayments.com IP와 443 egress를 운영 방화벽에 반영한다.

### 8.5 가상계좌 입금 후 환불

현재 UI는 CARD지만 legacy·변조 데이터가 가상계좌로 나타날 수 있다. 입금 완료 뒤 refundReceiveAccount가 없으면 자동 환불하지 않는다. 해당 refund_required event는 Toss 콘솔과 구매자 계좌 확인을 거쳐 수동 처리해야 한다.

### 8.6 지급 후 취소·부분취소의 자동 회수 없음

이미 사용된 quota와 구독 entitlement를 단순 비율로 회수하면 음수 잔액, 이미 소비된 서비스, 부분 기간 문제를 만들 수 있다. 그래서 자동 clawback 대신 cancellation event를 남긴다. 재무·CS 정책을 정해 관리자가 처리해야 한다.

### 8.7 15일 이후 unresolved provider POST

15일이 지나면 Toss의 이전 멱등 결과 보장을 코드가 더 이상 전제로 삼지 않는다. 이 시점의 unresolved ISSUE·charge·cancel은 새 POST보다 GET·Transaction API·Toss 콘솔·운영자 대사를 우선한다. 안전성을 위해 자동화가 멈추는 것이므로 alert와 runbook이 필요하다.

### 8.8 Transaction reconciliation backlog

cursor와 page checkpoint로 유실은 막지만 처리량은 의도적으로 제한되어 있다. MID가 많거나 backlog가 매우 크면 회복이 지연될 수 있다.

모니터링 권장 항목:

- source cursor lag
- recent·historical page cursor age
- lease가 오래 유지되는 source
- 시간당 처리 window·page 수
- permanent mismatch event 증가율
- refund_required event age

### 8.9 실제 MySQL·PostgreSQL 통합 검증

세 DB용 방언 분기, GORM abstraction, predicate·CAS 테스트는 반영했지만 이번 로컬 실행은 실제 MySQL과 PostgreSQL 인스턴스를 포함하지 않았다.

운영 전 각 DB에서 확인할 항목:

- 전체 schema migration
- Option row lock과 concurrent config update
- billing·top-up·subscription·wallet settlement race
- large table MID fingerprint backfill
- renewal contract migration
- page cursor lease와 unique constraint
- MySQL changed rows 동작과 PostgreSQL row locking

### 8.10 provider schema·status·error code 변경

알 수 없는 응답은 fail-closed하므로 금전 손실보다 처리 지연을 선택한다. Toss가 새 status, error code, Payment shape를 도입하면 reconciliation queue와 503 비율이 증가할 수 있다. 문서·release note와 운영 지표를 함께 모니터링해야 한다.

### 8.11 mixed-version rollback

enc:v1 option, 새 attestation, paymentKey-scoped idempotency, opaque recurring orderId가 저장된 뒤 구버전 binary로 되돌리면 구버전이 새 상태를 이해하지 못할 수 있다. 결제 스키마·프로토콜 전환 뒤의 rollback은 일반 애플리케이션 rollback으로 취급하면 안 된다.

안전한 기본 대응:

- 결제 flag를 모두 끈다.
- 신버전 코드로 조회·reconciliation·revocation을 유지한다.
- 원인 수정 버전을 배포한다.
- 반드시 필요하면 DB와 암호화 키를 같은 시점의 검증된 백업으로 함께 복원한다.

## 9. 배포 체크리스트

### 9.1 배포 전

- [ ] DB와 영구 암호화 키를 같은 시점에 백업했다.
- [ ] 모든 노드가 동일하고 영구적인 32바이트 이상 CRYPTO_SECRET 또는 기존 호환 SESSION_SECRET을 사용한다.
- [ ] ServerAddress가 외부에서 접근 가능한 HTTPS origin이다.
- [ ] outbound 443과 공식 api.tosspayments.com IP가 방화벽에서 허용됐다.
- [ ] 일반 live/test client·secret pair가 같은 MID와 환경에 속한다.
- [ ] billing live/test client·secret pair가 자동결제로 계약된 MID에 속한다.
- [ ] wallet auto-recharge의 비구독 사용 승인을 별도로 확인했다.
- [ ] payment compliance가 완료됐다.
- [ ] 모든 사용 MID에 PAYMENT_STATUS_CHANGED가 등록됐다.
- [ ] billing MID에 BILLING_DELETED가 등록됐다.
- [ ] reverse proxy 환경에서 direct peer와 trusted proxy CIDR을 확인했다.
- [ ] 공식 inbound webhook IP 목록을 최신 문서와 대조했다.
- [ ] 기존 pending order, pending revocation, reconciliation event 수를 기록했다.

### 9.2 rolling upgrade

- [ ] 신규 Toss checkout을 잠시 중단했다.
- [ ] old master scheduler와 callback traffic을 drain했다.
- [ ] 모든 노드를 enc:v1 dual-reader 코드로 먼저 교체했다.
- [ ] 그 뒤에만 TOSS_OPTION_SECRET_ENCRYPTION_ENABLED를 전체 노드에 활성화했다.
- [ ] paymentKey-scoped idempotency protocol 전환 전 legacy pending 주문을 대사했다.
- [ ] recurring protocol migration 전에 billing과 wallet auto-recharge를 DB에서 명시적으로 false로 저장했다.
- [ ] old scheduler를 중단하고 최소 drain 시간을 기다렸다.
- [ ] migration acknowledgement 환경변수는 sole migration master에서만 일시 사용했다.
- [ ] migration 성공 직후 acknowledgement 환경변수를 제거했다.
- [ ] 구버전 binary를 다시 시작하지 않도록 배포 정책을 잠갔다.

세부 환경변수와 순서는 [.env.example](../../../.env.example) 및 [docker-compose.yml](../../../docker-compose.yml)의 Toss rollout 주석을 함께 따른다.

### 9.3 repair 또는 maintenance가 표시될 때

- [ ] Toss의 세 enable flag를 모두 false로 유지한다.
- [ ] 화면을 새로고침해 최신 repair token을 받는다.
- [ ] live/test 일반·billing 네 key pair와 가격·최소 금액을 포함한 14개 전체를 확인한다.
- [ ] 빈 secret이 기존 secret 유지가 아니라 삭제임을 확인한다.
- [ ] repair 확인 dialog를 거쳐 complete-set을 제출한다.
- [ ] maintenance가 남으면 secret을 다시 입력하지 말고 retry maintenance를 실행한다.
- [ ] maintenance가 끝나기 전 기능을 활성화하지 않는다.
- [ ] unresolved legacy row와 reconciliation queue를 확인한다.

### 9.4 단계적 기능 활성화

권장 순서:

1. test mode 일반 결제
2. live 일반 결제
3. test mode subscription billing
4. live subscription billing
5. 계약이 확인된 뒤 wallet auto-recharge

각 단계에서 다음 smoke test와 대사를 마친 뒤 다음 flag를 켠다.

- 정상 결제와 1회 quota 지급
- 사용자가 결제창을 닫은 경우
- callback 중복 호출
- webhook 중복 수신
- confirm timeout 또는 409 뒤 동일 결제 복구
- 다른 amount·orderId·paymentKey callback 거절
- key rotation 전후 과거 주문 GET 복구
- 결제 후 local settlement 실패를 가정한 reconciliation
- 취소·부분취소 event
- billing ISSUE, 첫 charge, renewal, cancel, BILLING_DELETED
- 사용자·조직 비활성화와 provider POST 경쟁

## 10. 장애 대응 runbook

### 10.1 confirm timeout 또는 409

하지 말아야 할 것:

- 새 orderId 생성
- 새 Idempotency-Key 생성
- 현재 활성 secret으로 임의 교체
- local order를 즉시 failed 처리

해야 할 것:

1. order에 저장된 paymentKey, exact credential, idempotency key, attempt time을 확인한다.
2. Payment GET과 Transaction reconciliation 결과를 확인한다.
3. pending recovery worker와 cursor 상태를 확인한다.
4. 15일 안이면 같은 namespace의 복구만 허용한다.
5. 15일 이후 unresolved면 Toss 콘솔과 reconciliation event로 수동 대사한다.

### 10.2 Toss config repair required

1. 모든 Toss flag가 false인지 확인한다.
2. DB row를 수동으로 일부 수정하지 않는다.
3. 관리 UI에서 14개 complete-set repair를 수행한다.
4. maintenance retry가 남으면 기존 repair를 다시 제출하지 않는다.
5. MID·renewal migration이 끝난 뒤 단계적으로 활성화한다.

### 10.3 webhook 403 증가

1. direct RemoteAddr와 proxy topology를 확인한다.
2. Toss 공식 inbound IP 변경을 확인한다.
3. trusted proxy CIDR이 실제 app 직전 proxy만 포함하는지 확인한다.
4. X-Forwarded-For 순서가 proxy 설정과 일치하는지 확인한다.
5. 필요 시 신규 Toss IP만 TOSS_WEBHOOK_SOURCE_CIDRS에 임시 추가한다.

### 10.4 webhook 503 증가

1. provider API 장애와 DB lock·latency를 확인한다.
2. 8초 handler budget 안에서 GET·DB가 끝나는지 확인한다.
3. BILLING_DELETED이면 billing key ciphertext와 CRYPTO_SECRET 일치 여부를 확인한다.
4. cancellation이면 provider status propagation 지연인지 확인한다.
5. 웹훅 재전송 종료 전에 Transaction reconciliation이 보완하는지 확인한다.

### 10.5 refund_required

1. local quota가 아직 지급되지 않았는지 확인한다.
2. authoritative Payment의 paymentKey, orderId, amount, currency, balance를 확인한다.
3. 같은 balance의 refund operation key를 바꾸지 않는다.
4. partial cancellation transactionKey ledger를 확인한다.
5. 가상계좌 입금 건이면 refundReceiveAccount와 은행 환불 상태를 수동 확인한다.
6. full refund evidence 전에는 fence를 해제하지 않는다.

### 10.6 pending_revocation billing key

1. active subscription·wallet reference가 남아 있는지 확인한다.
2. 정확한 stored credential과 MID fingerprint를 확인한다.
3. billing cleanup worker가 kill switch와 무관하게 실행되는지 확인한다.
4. 복호화 실패면 암호화 키 배포 상태를 먼저 해결한다.
5. key를 로컬에서 삭제해 추적을 잃지 않는다.

### 10.7 CRYPTO_SECRET 불일치

1. 즉시 모든 신규 Toss 결제·billing·wallet flag를 끈다.
2. 다른 노드에서 동일 오류가 발생하는지 확인한다.
3. DB ciphertext가 만들어진 당시의 키를 비밀 저장소·백업에서 복구한다.
4. 임의의 새 키로 repair하지 않는다.
5. re-encryption이 필요하면 별도 오프라인 migration과 백업·검증 계획을 수립한다.

## 11. 검증 결과

### 11.1 백엔드 핵심 패키지

다음 명령은 최종 커밋 직전 작업 트리에서 통과했다.

~~~text
go test -count=1 . ./controller ./model ./service ./middleware ./router ./setting ./i18n
~~~

통과 패키지:

- controller
- model
- service
- middleware
- router
- setting
- i18n

### 11.2 race 검증

다음 넓은 race 명령과 동시성 경계를 실행해 통과했다.

~~~text
go test -race -count=1 ./controller ./model ./service ./middleware ./router ./setting
~~~

- 일반 Toss 충전 동시 정산 시 quota 1회 지급
- 구독 charge claim 단일 owner
- wallet auto-recharge webhook 동시 정산 1회 지급
- 환불 operation key 동시 준비 시 동일 key 반환
- Toss maintenance 중 subscription plan 변경 차단

추가 결제 집중 race 실행에서도 Toss·wallet·subscription 관련 race가 발견되지 않았다.

### 11.3 프론트엔드

- Default 테마 전체 `bun test src`: 23개 파일, 132건 통과
- Classic 테마 Toss 집중 테스트 18건 통과
- Default production build 통과
- Classic production build 통과

### 11.4 정적·형식 검증

- `go vet . ./controller ./model ./service ./middleware ./router ./setting ./i18n` 통과
- 변경 Go 파일 gofmt 확인 통과
- 변경 프론트 파일 Prettier 확인 통과
- Classic 변경 파일 ESLint 확인 통과
- git diff --check 통과
- Default i18n sync에서 missing·extra key 0
- 이번 Toss 변경으로 추가·수정된 locale key는 지원 언어에 반영

주의: 저장소 전체의 기존 untranslated report에는 이번 Toss 변경과 무관한 항목이 남을 수 있으므로 “프로젝트 전체 번역이 모두 완성됐다”는 의미는 아니다.

### 11.5 전체 저장소 테스트에 대한 제한

전체 `go test ./...`는 이번 변경과 무관한 기존 실패 때문에 전체 green으로 기록하지 않는다.

이번 최종 실행에서 확인된 비-Toss 항목:

- 변경되지 않은 `relay/channel/claude`의 content conversion expectation 3건 실패
- 변경되지 않은 `relay/helper`의 non-positive ticker/global-state 성격 테스트 1건 실패

Toss 핵심 패키지와 위에 명시한 집중 테스트는 통과했다. 이 둘을 혼동해 “전체 저장소 테스트가 모두 통과했다”고 표현하면 안 된다.

또한 Default `build:check`의 TypeScript 단계는 기존 organization·usage-log·vendor-discount·`bun:test` typing 오류로 실패했다. `bun:test` import는 기준 HEAD에도 존재해 테스트 타입 설정 문제임을 확인했고 production build와 Default 132개 테스트는 통과했다. Default ESLint는 기존 dependency tree의 `brace_expansion_1.expand` 오류로 시작하지 못했다. Classic 전체 i18n lint도 저장소 전역의 기존 hardcoded string 330건을 보고했지만, Classic 변경 파일 ESLint·Prettier·테스트·production build는 통과했다.

## 12. 추가 권장 작업

실제 사람 검증은 [Toss Payments 사람 수동 테스트 시나리오](2026-07-12-toss-payments-manual-test-scenarios.md)에 준비물, 단계, 기대 결과, 증거, 정리 절차별로 정리했다. 병합 전에는 최소 P0 스모크 세트를 실행하고 미실행 항목을 위험으로 명시한다.

현재 구현의 안전성을 더 높이기 위한 후속 작업은 다음과 같다.

### 우선순위 높음

- 실제 Toss test MID를 사용한 자동 E2E suite 구축
- Toss test error header를 이용한 409, timeout 유사 오류, 카드 거절 회귀 시나리오
- MySQL·PostgreSQL·SQLite 3종 CI에서 migration·lock·settlement race 실행
- reconciliation event, pending revocation, cursor lag, refund fence age의 운영 대시보드와 alert
- Toss inbound·outbound IP 변경 감시와 배포 체크 자동화

### 제품 정책이 확정될 때

- 가상계좌를 실제 지원한다면 DEPOSIT_CALLBACK, 암호화된 refundReceiveAccount의 제한 저장, 보존기간·삭제정책 추가
- 지급 후 취소의 quota·entitlement 회수 정책과 승인 workflow 정의
- 관리자 reconciliation UI에서 provider evidence와 local ledger를 한 화면에 비교
- 대규모 MID·거래량을 위한 reconciliation throughput 지표와 동적 batch 조정

### 운영 문서

- 키 교체와 동일 MID·다른 MID 구분 절차
- CRYPTO_SECRET rotation용 별도 re-encryption runbook
- recurring protocol migration의 배포 자동화와 old binary 재기동 방지
- Toss status·error code 변경 시 fail-closed queue 처리 절차

## 13. 주요 코드 지도

| 영역 | 파일 |
|---|---|
| Toss 설정·key pair·금액 상한 | [setting/payment_toss.go](../../../setting/payment_toss.go) |
| 일반 결제 API·transport·confirm·webhook·refund | [controller/topup_toss.go](../../../controller/topup_toss.go) |
| request body 제한 | [controller/toss_request_body.go](../../../controller/toss_request_body.go) |
| billing auth·ISSUE·첫 결제 | [controller/subscription_payment_toss.go](../../../controller/subscription_payment_toss.go) |
| wallet auto-recharge API | [controller/wallet_auto_recharge.go](../../../controller/wallet_auto_recharge.go) |
| webhook source 보안 | [controller/toss_webhook_security.go](../../../controller/toss_webhook_security.go) |
| Transaction API reconciliation | [controller/toss_transaction_reconciliation.go](../../../controller/toss_transaction_reconciliation.go) |
| atomic option·final POST barrier | [model/option.go](../../../model/option.go) |
| option secret encryption·repair state | [model/toss_option_secret.go](../../../model/toss_option_secret.go) |
| top-up lifecycle·quota snapshot·settlement | [model/topup.go](../../../model/topup.go) |
| billing key·renewal·migration | [model/toss_billing.go](../../../model/toss_billing.go) |
| subscription settlement·plan mutation barrier | [model/subscription.go](../../../model/subscription.go) |
| immutable plan snapshot | [model/subscription_plan_snapshot.go](../../../model/subscription_plan_snapshot.go) |
| wallet policy·charge lifecycle | [model/wallet_auto_recharge.go](../../../model/wallet_auto_recharge.go) |
| cancellation·mismatch·refund events | [model/toss_payment_event.go](../../../model/toss_payment_event.go) |
| transaction cursor·lease | [model/toss_transaction_reconciliation.go](../../../model/toss_transaction_reconciliation.go) |
| recurring protocol migration | [model/toss_recurring_order_id_protocol.go](../../../model/toss_recurring_order_id_protocol.go) |
| pending cleanup | [service/toss_pending_cleanup_task.go](../../../service/toss_pending_cleanup_task.go) |
| renewal scheduler | [service/toss_billing_task.go](../../../service/toss_billing_task.go) |
| wallet scheduler | [service/wallet_auto_recharge_task.go](../../../service/wallet_auto_recharge_task.go) |
| router·admin API | [router/api-router.go](../../../router/api-router.go) |
| access log redaction | [middleware/logger.go](../../../middleware/logger.go) |
| Default 결제 설정 UI | [payment-settings-section.tsx](../../../web/default/src/features/system-settings/integrations/payment-settings-section.tsx) |

## 14. 기존 관련 문서

이 문서는 현재 최종 감사 결과와 운영 경계를 설명하는 최신 문서다. 초기 설계와 단계별 구현 배경은 다음 문서를 참고한다.

- [현재 하드닝의 커밋별 인덱스](2026-07-12-toss-hardening-commit-index.md)
- [현재 하드닝 변경 파일 완전 매핑](2026-07-12-toss-hardening-file-manifest.md)
- [기존 실제 Toss 커밋 이력](2026-07-12-toss-hardening-historical-commits.md)
- [Toss 결제 연동 설계](2026-06-29-toss-payment-integration-design.md)
- [Toss 결제 구현 및 하드닝 정리](2026-06-30-toss-payment-implementation-and-hardening.md)
- [Toss 결제 변경 요약](2026-06-30-toss-payment-change-summary.md)
- [Toss 결제 코드 가이드](2026-07-01-toss-payment-code-guide.md)
- [Toss 결제 라인별 해설](2026-07-01-toss-payment-line-by-line-guide.md)
- [지갑 자동충전 통합 설계](2026-06-30-wallet-auto-recharge-integrated-design.md)

## 15. 최종 결론

이번 검토의 핵심 변화는 Toss API를 호출하는 코드 몇 개를 보완한 것이 아니라, 결제 attempt의 identity와 권한을 DB에 영속화한 것이다.

- provider POST 전에 exact credential과 idempotency namespace를 고정한다.
- 결과가 불명확하면 새 결제를 만들지 않고 같은 namespace로 조회·복구한다.
- 결제 설정, 사용자·조직 상태, 취소 fence를 실제 POST와 정산 직전에 다시 확인한다.
- 가격·요금제·quota는 구매 시점 snapshot을 사용한다.
- callback과 webhook이 유실돼도 Transaction API와 durable cursor로 복구한다.
- 자동화가 안전하지 않은 가상계좌 환불, 지급 후 취소, 15일 이후 불명확 요청은 운영 event로 넘긴다.

코드 수준에서 확인된 고위험 결함은 현재 반복 감사 범위에서 해소됐다. 운영 배포의 최종 승인 조건은 실제 Toss test/live E2E, 상점 계약 확인, 방화벽·웹훅 설정, 영구 암호화 키 관리, 실제 사용 DB에서의 migration·락 검증을 완료하는 것이다.
