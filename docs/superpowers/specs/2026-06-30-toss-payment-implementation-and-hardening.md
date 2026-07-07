# Toss 결제 연동 — 구현 및 하드닝 정리 문서

**작성일:** 2026-06-30
**브랜치:** `toss` (base: `team`), 총 31커밋
**범위:** Toss Payments 일회성 충전(Phase 1) 백엔드 + 프론트엔드(web/default) + 관리자 설정 UI + 운영 housekeeping
**관련 문서:** 설계 — [2026-06-29-toss-payment-integration-design.md](2026-06-29-toss-payment-integration-design.md)

> 이 문서는 (1) 무엇을 구현했는지, (2) 코드 리뷰/검토 과정에서 **발견한 잠재 결함과 그 위험·원인·수정**을 가능한 자세히 정리한다. 특히 결제는 금전이 걸리므로, "잘못되면 어떤 손실이 났을 수 있는가"를 명시한다.

---

## 1. 개요

기존 결제 provider(Epay/Stripe/PayPal/Creem/Waffo)와 동일한 레이어드 구조(Router→Controller→Service→Model + Setting)로 **Toss Payments 충전**을 추가했다. 가장 유사한 패턴은 PayPal(리다이렉트 + 서버 confirm + 웹훅 + 멱등 적립)이며, Toss 고유의 두 차이를 반영했다.

1. **프론트 SDK가 결제창을 직접 연다** — 다른 provider처럼 `pay_link` URL로 이동하는 대신, `@tosspayments/tosspayments-sdk`의 `requestPayment()`를 호출한다.
2. **통화는 KRW(원화)** — 사용자가 입력한 충전 금액을 원화로 다루고, `TossUnitPrice`(₩/내부 unit) 상수로 크레딧을 환산한다.

확정된 정책: 프론트=`web/default`, 통화=원화 직접, test/live 키 토글 지원, Phase 1=일회성 충전(빌링키 정기결제는 Phase 2).

---

## 2. 엔드투엔드 흐름

```
[유저] 충전 페이지에서 Toss 선택 + 금액 입력
   │
   ▼ POST /api/user/toss/pay   (RequestTossPay)
[서버] ServerAddress 검증 → customerKey 조회/생성 → 대기(pending) TopUp 생성
       → { client_key, customer_key, order_id, order_name, amount, success_url, fail_url } 반환
   │
   ▼ 프론트: loadTossPayments(client_key).payment({customerKey}).requestPayment({...})
[Toss] 결제창 → 사용자 인증/결제
   │
   ▼ 브라우저가 success_url(/api/toss/confirm)로 리다이렉트 (?paymentKey&orderId&amount)
[서버] TossConfirm:
       1) 금액 검증(저장된 주문 금액 == 쿼리 amount)
       2) paymentKey를 주문에 먼저 저장 (RecordTossPaymentKey, 필수)
       3) POST https://api.tosspayments.com/v1/payments/confirm  (Basic auth + Idempotency-Key)
       4) 응답 검증(status DONE, totalAmount, orderId, paymentKey, currency==KRW)
       5) RechargeToss → 크레딧 적립 → /console/log
   │
   ▼ (병렬/백업) Toss → POST /api/toss/webhook  (PAYMENT_STATUS_CHANGED)
[서버] TossWebhook: 페이로드 불신 → getTossPayment로 권위 재조회
       - DONE     → 적립(미적립 시 복구)
       - EXPIRED/ABORTED → pending 주문 종료
       - CANCELED/PARTIAL_CANCELED → 정산 로그(수동 조정), 자동 회수 없음
   │
   ▼ (housekeeping) 10분 주기 cron
[서버] StartTossPendingCleanupTask: 50분 초과 + 미승인(provider_order_id==trade_no) pending → expired
```

핵심 원칙: **"결제 완료 여부는 Toss API에 직접 물어 결정한다"** — confirm 동기 경로와 webhook 비동기 경로 모두, 신뢰 불가한 입력(브라우저 쿼리/웹훅 페이로드)만으로 적립/종료하지 않고 `confirm` 또는 `getTossPayment`의 권위 응답으로 판정한다.

---

## 3. 추가·변경 파일

**백엔드 (신규)**
- `setting/payment_toss.go` — Toss 설정 변수 + test/live 활성 키 헬퍼
- `controller/topup_toss.go` — 충전 핸들러 전체(pay/amount/confirm/fail/webhook) + Toss API 클라이언트
- `service/toss_pending_cleanup_task.go` — stale pending 주문 만료 cron
- 테스트: `setting/payment_toss_test.go`, `model/topup_toss_test.go`, `controller/topup_toss_test.go`, `web/.../lib/payment.toss.test.ts`

**백엔드 (수정)**
- `model/topup.go` — 상수(`PaymentMethodToss`/`PaymentProviderToss`), `RechargeToss`, `RecordTossPaymentKey`, `ExpireStaleTossPendingTopUps`, `GetOrCreateTossCustomerKey`, `generateTossCustomerKey`, `ManualCompleteTopUp` 라우팅 수정, `ProviderOrderId` 컬럼 폭
- `model/option.go` — Toss 8개 옵션 영속화(양방향)
- `model/user.go` — `TossCustomerKey` 컬럼(`json:"-"`)
- `controller/payment_webhook_availability.go` — `isTossTopUpEnabled`
- `controller/topup.go` — `GetTopUpInfo`에 Toss 노출
- `router/api-router.go` — 라우트, `router/web-router.go` — COOP 헤더, `main.go` — cleanup 태스크 등록

**프론트엔드 (web/default)**
- `features/wallet/`: `constants.ts`, `types.ts`, `api.ts`, `lib/payment.ts`, `hooks/use-payment.ts`, `hooks/use-toss-payment.ts`(신규), `hooks/index.ts`, `index.tsx`
- `features/system-settings/`: `types.ts`, `billing/section-registry.tsx`, `billing/index.tsx`, `integrations/payment-settings-section.tsx`
- `i18n/locales/kr.json`, `package.json`(SDK 의존성)

---

## 4. 구현 요약

### 4.1 설정 (`setting/payment_toss.go` + `model/option.go`)
- 변수: `TossEnabled`, `TossTestMode`, `TossClientKey/SecretKey`(live), `TossTestClientKey/TossTestSecretKey`(test), `TossUnitPrice`(기본 1300, ₩/unit), `TossMinTopUp`(기본 1000 KRW).
- 헬퍼 `TossActiveClientKey()/TossActiveSecretKey()` — `TossTestMode`로 test/live 키 선택(Toss는 별도 샌드박스 host 없이 키쌍으로만 구분).
- `model/option.go`에 8개 키 양방향 영속화(OptionMap writer + SetOption switch).

### 4.2 금액·크레딧 규칙
- `req.Amount` = 사용자 입력 KRW 정수.
- `getTossPayMoney(amountKRW, group)` = `round(amountKRW × groupRatio × amountDiscount)` → 청구 원화(int64).
- `TopUp.Amount` = 청구 원화, `TopUp.Money` = `chargedKRW / TossUnitPrice`(USD 환산, 리포트 일관성).
- 적립 크레딧 = `decimal(Money) × QuotaPerUnit` (`shopspring/decimal`로 PostgreSQL float→int 안전).

### 4.3 핸들러 (`controller/topup_toss.go`)
- `RequestTossPay` / `RequestTossAmount` — 결제 시작/금액 미리보기 (+ 조직 미러 `RequestOrganizationTossPay/Amount`).
- `TossConfirm` — successUrl 핸들러(금액검증 → paymentKey 저장 → confirm → 검증 → 적립).
- `TossFail` — failUrl 핸들러.
- `TossWebhook` — PAYMENT_STATUS_CHANGED 처리(권위 재조회 후 DONE/종료/정산).
- API 클라이언트: `confirmTossPayment`(POST /v1/payments/confirm), `getTossPayment`(GET /v1/payments/{paymentKey}).

### 4.4 프론트엔드
- `@tosspayments/tosspayments-sdk@^2.7.1` 의존성.
- `use-toss-payment.ts` — `/api/user/toss/pay` 호출 후 SDK로 결제창 호출.
- `recharge-form-card`/`use-topup-info`/`index.tsx` — `enable_toss_topup`일 때 Toss 노출 및 결제 확정 분기.
- 관리자 설정 UI — 8개 키 + 단가/최소액 폼, webhook URL 안내.

### 4.5 운영 cron (`service/toss_pending_cleanup_task.go`)
- 10분 주기, master 노드 한정, 50분 초과 + 미승인 pending 주문을 expired 처리.

---

## 5. 발견·수정한 이슈 상세 (핵심)

> 이 절이 핵심이다. 각 이슈에 대해 **위험(잘못되면 무슨 일) / 원인 / 수정 / 커밋**을 정리한다. 다수가 다단계 리뷰와 사용자 분석 라운드에서 발견되었고, 일부는 **실제 금전 손실로 이어질 수 있는 결함**이었다.

### 5.1 [금전] 관리자 수동완료가 Toss 주문에 잘못된 크레딧 — `e89fc7b5`
- **위험:** 관리자가 Toss 주문을 수동 완료(`ManualCompleteTopUp`)하면 정상의 약 **1300배 크레딧이 과다 적립**될 수 있었다.
- **원인:** `ManualCompleteTopUp`은 Stripe/PayPal만 `Money`(USD 환산) 경로로 크레딧을 계산하고, 그 외는 `Amount × QuotaPerUnit` 경로를 썼다. Toss는 `Amount`에 KRW(예: 13000), `Money`에 USD환산(예: 10)을 저장하므로 `Amount` 경로를 타면 13000×QuotaPerUnit이 되어 폭증.
- **수정:** `PaymentProviderToss`를 Money 경로 조건에 추가(`model/topup.go`). Toss도 PayPal처럼 `Money` 기반으로 적립.

### 5.2 [금전·보안] 웹훅 페이로드만 믿고 적립 — `9265f24f`
- **위험:** `/api/toss/webhook`은 인증 없는 public 엔드포인트. 초기 구현은 페이로드의 `status==DONE` + 금액 일치만 보고 적립했다. 공격자가 orderId를 추측해 위조 DONE 웹훅을 보내면 **결제 없이 크레딧 발급** 가능.
- **원인:** 다른 provider(Stripe/Creem/Waffo HMAC, PayPal 서명검증)와 달리 호출자 인증이 없었다.
- **수정:** 페이로드 불신 원칙 도입 — 웹훅 수신 시 `getTossPayment`로 **Toss API에 권위 재조회** 후 `status DONE + 금액 + orderId` 일치할 때만 적립. 위조 페이로드는 무력화.
- 함께: 조직 충전 amount 핸들러/라우트 누락 추가, `RequestTossAmount` enable 가드, 성공 로깅.

### 5.3 [금전·치명] 프론트 customerKey에 `ANONYMOUS` 사용(결제창 API에서 무효) — `76a788a7`
- **위험:** 결제창 자체가 **리다이렉트 후 Toss 서버단에서 실패**해 결제가 전혀 진행되지 않을 수 있었다.
- **원인:** Toss v2 SDK에서 `ANONYMOUS`는 **위젯(`widgets()`)** 전용이고, **결제창(`payment()`)**은 실제 고유 customerKey(2~50자)를 요구한다. (Toss 문서로 검증.)
- **수정:** 백엔드가 사용자별 customerKey를 응답에 내려주고 프론트가 사용. (초기엔 `cust_<userId>`, 이후 5.7에서 랜덤화.)

### 5.4 [정합성] 금액별 할인(AmountDiscount) 미적용 — provider 패리티 위반 — `b7dfe1e6`
- **위험:** UI는 `discount` 맵을 노출하는데 Toss만 할인을 미적용 → **광고한 할인과 실제 청구액 불일치**, 타 provider와 동작 차이.
- **원인:** `getTossPayMoney`가 그룹 비율만 적용, `AmountDiscount`를 빠뜨림(설계 §3.4 위반).
- **수정:** `getPayPalPayMoney`와 동일하게 입력 금액 키로 `AmountDiscount` 적용. (비-항진 단위테스트 추가: 13000×0.9=11700.)

### 5.5 [보안] customerKey 예측 가능 — `a9f00558`
- **위험:** `cust_<userId>`는 순차/유추 가능. Toss는 무작위 고유값 권장. 특히 **Phase 2 빌링키가 customerKey에 묶이므로** 예측 가능성은 결제 식별/빌링 안전성에 약점.
- **원인:** Phase 1 편의로 userId 기반 키 사용(설계 §5.1의 `_<random>` 미준수).
- **수정:** `User.TossCustomerKey` 컬럼 추가 + `GetOrCreateTossCustomerKey`로 **랜덤(`cust_`+32 alnum) 1회 생성·저장·재사용**(race-safe 조건부 UPDATE). 런타임 검증: 같은 유저 두 호출에 동일한 랜덤 키 반환.
- 함께: paymentKey 영속화(5.8), confirm 검증 강화(5.9), webhook 주석 정직화(5.13).

### 5.6 [정합성·DB] `ProviderOrderId` 컬럼 폭 + customerKey 노출 — `c0b0c1b6`
- **위험(폭):** Toss paymentKey 최대 200자인데 컬럼이 `varchar(128)` → **PostgreSQL은 INSERT 에러로 적립 트랜잭션 실패(결제됐는데 미적립)**, MySQL은 잘림.
- **수정(폭):** `varchar(255)`로 확대(AutoMigrate가 MySQL/PG에서 자동 확대, SQLite는 길이 미강제). `TradeNo`가 이미 varchar(255) 인덱스라 검증된 패턴.
- **위험(노출):** `TossCustomerKey`가 GetUser/GetSelf 응답에 직렬화 → 비예측 키 취지 약화.
- **수정(노출):** `json:"-"`로 서버 전용(클라이언트는 `/toss/pay`로 받음).
- 함께: orphan 방지 — customerKey 조회 실패 시 주문 미생성(fail-fast).

### 5.7 [운영] ServerAddress 미검증 → orphan pending — `0c0b03dd`, `d3eb6229`
- **위험:** `success_url/fail_url`을 `ServerAddress`로 만드는데 검증이 없어, ServerAddress가 비었거나 scheme 없으면 **SDK 단계 실패 + 대기 주문만 orphan**으로 남음.
- **수정:** `isValidServerAddress`(http/https + host) 추가, `RequestTossPay`에서 주문 생성 전 검증(fail-fast), `isTossTopUpEnabled`에도 포함(불량 시 UI에서 Toss 미노출). 이후 5.14에서 raw값 정규화 보강.

### 5.8 [금전·핵심] paymentKey 저장 시점/보장 — `0c0b03dd` → `d3eb6229` → `97e03330` → `af808543`
이 항목은 여러 라운드에 걸쳐 점진적으로 강화된 **결제완료-미적립 방지의 핵심**이다.
- **위험:** Toss는 "성공 redirect의 paymentKey/orderId/amount를 서버에 저장한 뒤 승인 API를 호출"하라고 권고. 저장이 늦거나 실패하면, 승인(실제 결제) 성공 후 적립 트랜잭션이 실패/프로세스 사망 시 **paymentKey가 유실되어 취소·조회·복구 불가**.
- **추가 위험(sweep 연계):** 후술하는 stale-pending sweep이 `provider_order_id == trade_no`를 "미승인"으로 보고 만료하므로, **승인했는데 paymentKey 미기록이면 결제된 주문이 expired로 닫혀 영구 미적립**.
- **수정 경과:**
  1. `RecordTossPaymentKey` 도입, confirm **직후** 저장(`0c0b03dd`).
  2. confirm **호출 전**으로 이동(`d3eb6229`) — Toss 문서 순서 준수.
  3. **저장을 필수화**(`97e03330`): `RecordTossPaymentKey`가 저장 후 재조회로 `provider_order_id == paymentKey` 검증(MySQL의 "변경행 기준 RowsAffected==0" 함정 회피), `TossConfirm`은 저장 실패 시 **confirm 미호출하고 중단**. → 승인은 paymentKey가 확실히 커밋된 뒤에만 발생.
  4. **웹훅 DONE 경로에도 동일 적용**(`af808543`): 적립 전 `RecordTossPaymentKey`를 fatal로 호출(실패 시 503 재시도 유도). → confirm/webhook 두 적립 경로 모두에서 불변식 성립.

### 5.9 [방어] confirm 응답 검증 강화 — `a9f00558`, `5ab0ecfb`
- **위험:** 응답을 `status`/`totalAmount`만 검증하면 응답 위변조/혼선 케이스 방어가 느슨.
- **수정:** `TossConfirm`은 `status DONE && totalAmount==amount && orderId 일치 && paymentKey 일치 && currency==KRW`까지 검증(`tossConfirmResponse`에 `Currency` 추가). webhook DONE 경로에도 `currency==KRW` 추가(`5ab0ecfb`)로 대칭화.

### 5.10 [금전·핵심] confirm 실패를 4xx=terminal로 단정 → 409에서 미적립 — `1650a315`, `f71b7954`
이 항목도 두 라운드로 강화된 중요한 금전 결함이다.
- **1차 위험(`5ab0ecfb`에서 도입한 로직의 결함):** confirm 실패를 `4xx → failed` 처리했더니, Toss의 **HTTP 409 `IDEMPOTENT_REQUEST_PROCESSING`**(이전 멱등요청 처리중 = transient, 재조회 필요)까지 failed로 닫힘 → 이후 DONE 웹훅이 non-pending으로 무시 → **결제 완료인데 미적립**.
- **1차 수정(`1650a315`):** HTTP 코드 추측을 폐기하고 **`getTossPayment` 권위 재조회로 전환**. DONE→적립, EXPIRED/ABORTED→종료, 그 외(409/진행중 포함)→pending 유지(웹훅 해소).
- **2차 위험:** 위 전환 후 "verify 실패/order mismatch → pending 유지"가, 위조/없는 paymentKey처럼 **결제 없음이 확정인데도 영구 pending**으로 stranded(이미 paymentKey가 저장돼 sweep 대상에서 제외됨).
- **2차 수정(`f71b7954`):** `getTossPayment`도 HTTP status 반환. **verify 404(결제 없음) → failed로 종료**, **order mismatch(변조 키) → 종료**, network/5xx/진행중만 pending 유지. FORBIDDEN(403, 키 설정오류)은 404가 아니므로 transient로 두어 정상 주문을 오인 종료하지 않음.

### 5.11 [네트워크] Toss API 타임아웃·멱등키 부재 — `0c0b03dd`, `fddd68db`
- **위험:** `http.DefaultClient`(무제한 타임아웃)로 호출 → confirm은 **`LockOrder`를 쥔 채 호출**되므로 행(hang) 시 주문 락 무한 점유. 웹훅은 Toss가 ~10초 내 200을 기대하는데 15초 타임아웃이면 불필요한 재전송 유발.
- **수정:** `confirmTossPayment`/`getTossPayment`에 `context.WithTimeout` 적용. confirm에 `Idempotency-Key: orderId`(브라우저 새로고침/중복 redirect 안전). webhook 전용인 `getTossPayment`는 **8초**로 축소(Toss 10초 window 준수), confirm은 15초.

### 5.12 [운영] stale/lingering pending 주문 정리 — `fddd68db`, `8e820f7e`, `1898912a`
- **위험:** 사용자가 결제창을 닫거나(PAY_PROCESS_CANCELED, 이 경우 webhook 미발생) 승인 시간이 지나면 대기 주문이 **영구 pending으로 누적** → 운영 목록/사용자 결제내역 오염.
- **수정 경과:**
  1. webhook이 EXPIRED/ABORTED를 받으면 pending 주문 종료(`fddd68db`).
  2. webhook이 안 오는 케이스(즉시 취소)까지 포괄하는 **시간 기반 sweep**(`8e820f7e`): 50분 초과 pending → expired (master 노드 한정, 10분 주기).
  3. **sweep 안전성**(`1898912a`, 금전 위험 수정): sweep이 모든 오래된 pending을 만료하면, paymentKey가 있는(승인됐을 수 있는) 주문도 만료될 위험. → **`provider_order_id == trade_no`(paymentKey 미기록=미승인)인 주문만** 만료. cutoff 35→50분(정상 흐름 최악 ≈ 30분 창+10분 승인=40분 + 여유).

### 5.13 [정직성] 웹훅 범위/형식 명확화 — `a9f00558`, `5ab0ecfb`
- **이슈:** 초기 webhook 주석은 "가상계좌 DONE 처리"라 했지만 실제 가상계좌 `DEPOSIT_CALLBACK`은 top-level `secret`/`status`/`orderId` 형식이라 우리 파서(`data.*`)와 불일치 → 무해하지만 비기능적.
- **수정:** Phase 1은 카드 전용(동기 confirm 적립)임을 주석으로 정직하게 명시. webhook은 `PAYMENT_STATUS_CHANGED`(카드) 처리용 재검증 스텁이며, 가상계좌 `DEPOSIT_CALLBACK`/`secret`은 추후로 명시 보류. (이후 EXPIRED/취소 처리가 실제로 추가되며 PAYMENT_STATUS_CHANGED는 우리 파서와 일치함을 Toss 문서로 검증.)

### 5.14 [정합성] ServerAddress 정규화 — `ce1c492b`
- **위험:** `isValidServerAddress`는 trim 후 검증하지만 URL 생성은 raw값을 써서, 앞뒤 공백·끝슬래시가 있으면 **깨진/중복슬래시 redirect URL**이 SDK로 전달.
- **수정:** `serverBase = TrimRight(TrimSpace(ServerAddress), "/")`로 정규화해 `success_url/fail_url` 생성.

### 5.15 [정산] 결제 완료 후 취소/부분취소 — `ce1c492b`, `8e820f7e`
- **위험:** Toss 대시보드에서 환불/취소 시 `CANCELED`/`PARTIAL_CANCELED` 웹훅이 오는데, 이미 success인 로컬 주문은 무시 → **Toss는 환불됐는데 로컬 크레딧 잔존(정산 불일치)**. 특히 `PARTIAL_CANCELED`는 잔액이 남는다.
- **수정:**
  - 성공 주문에 cancel 웹훅 → 권위 재검증 후 **운영 로그(LogError) + 관리자 충전로그(`RecordTopupLog`, "toss-cancel")** 로 수동 정산 표시(자동 회수는 위험하므로 미수행).
  - pending 주문의 EXPIRED/ABORTED는 조용히 종료하되, **CANCELED/PARTIAL_CANCELED는 조용히 failed로 닫지 않고** 동일하게 정산 로그 후 failed(완료됐는데 미적립인 케이스 — 잔액 수동 확인 필요).

### 5.16 [운영] 관리자 설정 UI 오류·누락 — `49631c08`, `5ab0ecfb`
- **defaultValues 폴백 오류:** `TossUnitPrice ?? 1`/`TossMinTopUp ?? 1`이 백엔드 기본값(1300/1000)과 모순 → `?? 1300`/`?? 1000`.
- **`defaultBillingSettings` 누락:** 8개 Toss 필드 추가로 TS2740 → 백엔드 기본값으로 채움.
- **i18n 키 오류:** 한국어 문자열을 i18n 키로 사용 → 영어 키 + `kr.json` 번역(프로젝트 규칙).
- **webhook 등록 안내 부재:** 설정 UI에 `<ServerAddress>/api/toss/webhook` + `PAYMENT_STATUS_CHANGED` 안내 추가(운영자 누락 방지).

### 5.17 [안정성] 결제창 팝업 차단 회피 헤더(COOP) — `5f50b6a6`
- **이슈:** Toss는 결제창을 여는 페이지에 `Cross-Origin-Opener-Policy: same-origin-allow-popups`를 권장(일부 브라우저 팝업 차단 회피).
- **수정:** SPA 문서 응답(`router/web-router.go`의 NoRoute 핸들러)에만 해당 헤더 추가. `-allow-popups` 변형이라 앱이 여는 OAuth 팝업 등은 정상.

---

## 6. 보안·정합성 원칙 (정리)

- **권위 판정:** 신뢰 불가 입력(브라우저 쿼리/웹훅 페이로드)만으로 적립/종료하지 않음. `confirm`/`getTossPayment` 권위 응답으로 판정. confirm·webhook 모두 동일 원칙.
- **멱등성:** `LockOrder`/`UnlockOrder`(refcount 뮤텍스) + DB row lock(`FOR UPDATE`) + status 가드. `RechargeToss`는 이미 success면 no-op. confirm에 `Idempotency-Key`.
- **금액 위변조 방지:** confirm 시 쿼리 amount == 저장 주문 금액, 그리고 권위 응답 `totalAmount`/`currency==KRW`/`orderId`/`paymentKey` 일치.
- **paymentKey 불변식:** 승인 전 paymentKey를 반드시 커밋 → `provider_order_id != trade_no`. sweep은 `provider_order_id == trade_no`(미승인)만 만료 → 결제된 주문 오만료 불가.
- **시크릿 비노출:** 시크릿 키는 Basic auth 헤더로만 사용, 응답/로그 미노출. 옵션 API는 `*Key` 접미사를 필터링. `TossCustomerKey`는 `json:"-"`.
- **fail-safe:** 모호하면 적립/종료하지 않고 pending 유지(웹훅/재시도/sweep이 해소). 결제 없음이 확정(verify 404/mismatch)일 때만 종료.
- **DB 호환:** GORM 추상화, 신규 컬럼은 struct+AutoMigrate(SQLite는 ALTER COLUMN 회피), `UPDATE ... LIMIT` 미사용, 예약어 아닌 컬럼 비교.
- **JSON:** 전부 `common.Marshal/Unmarshal`(Rule 1). **보호 식별자**(new-api/QuantumNous) 미변경(Rule 5).

---

## 7. 설정·운영 가이드

1. **Toss 키 발급:** developers.tosspayments.com에서 test/live `클라이언트 키`/`시크릿 키`.
2. **시스템 설정 → Server Address**를 실제 접속 도메인(scheme 포함, 끝슬래시 없이)으로 설정 — Toss 콜백이 여기로 돌아온다. 불량 시 Toss는 결제수단에 노출되지 않는다.
3. **결제 설정:** 컴플라이언스 동의 → Toss 활성화 + 테스트 모드 + 키 입력 + `TossUnitPrice`(₩/unit)·`TossMinTopUp` 저장.
4. **Toss 개발자센터 webhook 등록:** `<ServerAddress>/api/toss/webhook`, 이벤트 `PAYMENT_STATUS_CHANGED`. (복구 적립, EXPIRED/ABORTED 정리, 취소 감지에 사용 — 등록 권장.)
5. **원화 환산:** 사용자 입력 KRW → 크레딧 = `KRW / TossUnitPrice × QuotaPerUnit`. `TossUnitPrice`로 환율/단가 조정.

---

## 8. 테스트·검증

- **Go 단위테스트(순수):** `getTossPayMoney`(+할인), `validateTossConfirm`, 크레딧 환산식, 상수, `TossActiveKeysToggle`, `isValidServerAddress`, `isTossTerminalFailStatus`, `isTossCancelStatus`, `generateTossCustomerKey`.
- **런타임 검증(로컬 구동):** 변경분 바이너리 빌드·실행 후 `topup/info` 노출, `/toss/amount`·`/toss/pay`(계약 필드·KRW 환산), 최소금액 거부, **test/live 토글**, **금액 위변조 가드(불일치 confirm → 적립 없음·302)**, **랜덤 customerKey 안정성**, `toss_customer_key` 미노출을 curl로 확인.
- **참고(기존 이슈):** `model`/`controller` 전체 테스트 스위트에는 DB 테이블 미마이그레이션으로 인한 **기존(team에서도 동일, 43건) 실패**가 있으며 Toss 작업과 무관(Toss 단위테스트는 전부 통과).

---

## 9. 범위 외 / 향후 (Phase 2)

- **빌링키 정기결제(구독):** `requestBillingAuth` → 빌링키 발급·암호화 저장 → 정기 청구 cron. 본 Phase 1의 customerKey 저장이 그 토대.
- **가상계좌(비동기) 결제:** `DEPOSIT_CALLBACK`(top-level `secret`) 전용 처리 + secret 저장.
- **자동 환불 회수:** 현재는 취소/부분취소를 정산 로그로만 표시(수동 조정). 자동 quota 회수는 미구현(이미 소비된 quota·부분취소 계산 위험).
- **전 provider 공통 pending 만료 sweep:** 현재 sweep은 Toss 한정.

---

## 부록 — 커밋 목록 (오래된 순)

| 커밋 | 요약 |
|---|---|
| `8e006206`/`c15200d8` | 설계 문서(영문→한국어) |
| `b639b61b` | 설정 변수·영속화 |
| `dfef081a` | 모델 상수·`RechargeToss` |
| `e89fc7b5` | [fix] ManualCompleteTopUp Money 경로 라우팅 |
| `a34a0c11` | 충전 시작/금액 핸들러·confirm 클라이언트 |
| `a543c9c3` | enable 헬퍼·topup/info 노출·라우트 |
| `9265f24f` | [fix] webhook 권위 재검증·조직 amount·가드 |
| `36907c46`~`e06b700e` | 프론트 상수/타입/API/lib/SDK 훅/분기 |
| `76a788a7` | [fix] 프론트 실제 customerKey·취소 UX·정리 |
| `0f16227b` | 관리자 설정 UI |
| `49631c08` | [fix] 설정 폴백·defaults·i18n 키 |
| `b7dfe1e6` | [fix] AmountDiscount 적용(패리티) |
| `a9f00558` | [fix] 랜덤 customerKey·paymentKey 저장·confirm 강화·webhook 정직화 |
| `c0b0c1b6` | [fix] ProviderOrderId 폭·customerKey 비노출·fail-fast |
| `0c0b03dd` | [fix] ServerAddress 검증·paymentKey 조기저장·timeout/멱등키 |
| `d3eb6229` | [fix] paymentKey를 confirm 전 저장·enabled에 ServerAddress |
| `fddd68db` | [fix] webhook EXPIRED 종료·webhook 8초 |
| `ce1c492b` | [fix] 성공후 취소 정산 로그·ServerAddress 정규화 |
| `8e820f7e` | [fix] pending 부분취소 정산·stale pending sweep |
| `1898912a` | [fix] sweep 미승인 주문만·cutoff 50분 |
| `97e03330` | [fix] confirm 전 paymentKey 저장 필수화 |
| `af808543` | [fix] webhook DONE 적립 전 paymentKey 저장 |
| `5ab0ecfb` | [fix] webhook currency·confirm 거부 종료·webhook URL 안내 |
| `1650a315` | [fix] confirm 실패 권위 재검증(409 처리) |
| `f71b7954` | [fix] verify 404/mismatch 종료·transient만 pending |
| `5f50b6a6` | [fix] SPA 문서에 COOP 헤더 |
</content>
