# Toss Payments 사람 수동 테스트 시나리오

- 작성일: 2026-07-12
- 대상 브랜치: `toss`
- 기준 코드 커밋: `2c619a31e`, `28fe73ad5`, `c0f4b5a71`
- 현재 리베이스 기준 HEAD: `0bf89b004` (`origin/team`, 2026-07-13)
- 대상 UI: Default, Classic
- 목적: 자동 테스트로 충분히 증명하기 어려운 실제 Toss 테스트 상점·브라우저·웹훅·운영 환경 동작을 사람이 재현하고 증거를 남기기 위한 체크리스트

## 1. 이 문서의 사용 원칙

이 문서에서 별도 표시가 없는 결제 시나리오는 모두 **Toss 테스트 키와 폐기 가능한 테스트 DB**에서 실행한다.

### 절대 지켜야 할 안전 규칙

- `TossTestMode=true`와 `test_ck_`/`test_sk_` API 개별 연동 키를 확인하기 전에는 테스트를 시작하지 않는다.
- `live_ck_`/`live_sk_`가 보이면 즉시 중단한다. 라이브 테스트는 이 문서의 마지막 제한 구역만 따른다.
- 테스트 화면, 브라우저 개발자 도구, 로그, 이슈 티켓에 secret key, billingKey, authKey, 전체 카드번호, 환불계좌를 남기지 않는다.
- 운영 DB에서 시간을 수정하거나 pending row를 조작하거나 worker를 강제 실행하지 않는다.
- 장애 주입, DB row 수정, 다중 노드 경쟁 테스트는 운영 데이터가 없는 격리 환경에서만 수행한다.
- `TossPayments-Test-Code`는 테스트 시크릿 키에서만 사용한다. 애플리케이션에 별도 fault-injection 통로가 없다면 승인된 QA 프록시·테스트 harness를 사용한다.
- 테스트가 예상과 다르게 실제 과금으로 보이면 모든 결제 flag를 끄고 Toss 개발자센터·상점관리자에서 거래를 확인한 뒤 담당자에게 즉시 알린다.

### 테스트 등급

| 등급 | 의미 |
|---|---|
| P0 | 중복 청구·오지급·미지급·권한 우회·secret 노출을 막는 병합 전 필수 테스트 |
| P1 | 장애 복구·운영 안정성·브라우저 호환성을 확인하는 권장 테스트 |
| P2 | 실제 인프라나 장시간 관찰이 필요한 확장 테스트 |

### 환경 표기

| 표기 | 실행 환경 |
|---|---|
| TEST | Toss 테스트 MID와 일반 테스트 브라우저만 있으면 실행 가능 |
| STAGING | 공개 HTTPS callback, webhook, reverse proxy가 있는 격리 staging 필요 |
| HARNESS | 승인된 egress proxy, fault injector, 테스트 worker 또는 disposable DB 조작 필요 |
| LIVE-APPROVAL | 재무·운영 책임자의 명시적 승인 뒤 제한적으로만 실행 |

## 2. 공식 문서에서 확인할 기준

테스트 당일 아래 문서를 다시 열어 정책과 IP 목록이 바뀌지 않았는지 확인한다.

| 공식 문서 | 수동 테스트에 적용할 기준 |
|---|---|
| [시작하기](https://docs.tosspayments.com/guides/v2/get-started) | 일반 결제와 자동결제 제품·계약 범위를 구분한다. |
| [환경 설정하기](https://docs.tosspayments.com/guides/v2/get-started/environment) | 테스트 결제는 실제 과금되지 않는다. 테스트 API는 분당 100건 제한이며 `TossPayments-Test-Code`로 오류를 재현할 수 있다. |
| [JavaScript SDK v2](https://docs.tosspayments.com/sdk/v2/js) | successUrl의 amount를 요청 금액과 비교하고, billing auth의 successUrl·failUrl을 검증한다. |
| [코어 API](https://docs.tosspayments.com/reference) | 일반 승인 인증은 10분 안에 완료하고 paymentKey·orderId·금액·상태를 저장·대조한다. Transaction API로 승인·취소 거래를 대사한다. |
| [멱등키](https://docs.tosspayments.com/reference/using-api/authorization) | 멱등성은 멱등키·API 키·URL·HTTP 메서드 조합에 묶이며 15일간 유지된다. |
| [API 에러 코드](https://docs.tosspayments.com/reference/error-codes) | 카드 거절, 중복 처리, 인증 실패와 일시적 provider 오류를 구분한다. |
| [자동결제 결제창](https://docs.tosspayments.com/guides/v2/billing/integration) | authKey와 customerKey로 빌링키를 발급하고, billingKey는 다시 조회할 수 없으므로 안전하게 저장한다. |
| [웹훅 연결](https://docs.tosspayments.com/guides/v2/webhook) | 10초 안에 200을 반환해야 하며 실패 시 최대 7회 재전송된다. 웹훅은 MID별로 등록한다. |
| [웹훅 이벤트](https://docs.tosspayments.com/reference/using-api/webhook-events) | 공식 이벤트 형식을 확인한다. 현재 애플리케이션의 처리 계약은 PAYMENT_STATUS_CHANGED와 BILLING_DELETED이며 취소 상태는 authoritative Payment GET·Transaction 대사로 검증한다. |
| [결제 취소](https://docs.tosspayments.com/guides/v2/cancel-payment) | 전액·부분 취소와 cancellation transactionKey를 확인한다. 입금된 가상계좌 환불에는 환불계좌가 필요하다. |
| [방화벽·TLS](https://docs.tosspayments.com/reference/using-api/security) | HTTPS·TLS 1.2 이상과 최신 inbound·outbound IP 허용 목록을 확인한다. |

현재 구현의 위험 분석과 운영 불변조건은 [공식 문서 기준 감사 보고서](2026-07-12-toss-payments-official-docs-audit-and-hardening.md)를 함께 참고한다.

## 3. 테스트 준비

### 3.1 환경 준비

- [ ] `toss` 브랜치의 테스트 대상 commit hash를 기록했다.
- [ ] 운영과 분리된 DB와 Redis를 사용한다.
- [ ] DB snapshot 또는 폐기 가능한 초기 데이터를 준비했다.
- [ ] 모든 노드에 동일한 32바이트 이상 영구 `CRYPTO_SECRET`을 설정했다.
- [ ] `TossTestMode=true`다.
- [ ] 일반 결제는 `test_ck_` + `test_sk_` API 개별 연동 키 쌍이다.
- [ ] 자동결제는 billing 계약이 연결된 테스트 MID의 `test_ck_` + `test_sk_` 쌍이다.
- [ ] 결제위젯용 `test_gck_`/`test_gsk_`를 사용하지 않는다.
- [ ] `TossEnabled`, `TossBillingEnabled`, `TossWalletAutoRechargeEnabled`의 초기값을 기록했다.
- [ ] Toss 개발자센터에서 테스트 MID별 API version을 기록했다.
- [ ] 공개 HTTPS callback origin과 웹훅 URL을 준비했다.
- [ ] 실제 웹훅 테스트에서는 공식 inbound IP와 reverse proxy CIDR을 최신 공식 문서와 대조했다.

### 3.2 테스트 계정과 데이터

다음 계정을 서로 다른 브라우저 프로필로 준비한다.

| 식별자 | 역할 | 용도 |
|---|---|---|
| U1 | 조직에 속하지 않은 일반 사용자 | 개인 충전·구독·자동충전 |
| U2 | 두 번째 일반 사용자 | customerKey·주문 격리 확인 |
| O1 | 조직 owner | 조직 지갑 결제·자동충전 |
| M1 | O1 조직의 일반 member | owner 전용 기능 차단 확인 |
| A1 | root 시스템 관리자 | Toss 설정·요금제·preset·대사 이벤트 확인 |

추가 준비:

- [ ] U1과 조직의 시작 quota·잔액을 기록했다.
- [ ] 구매 가능한 활성 구독 요금제 하나를 준비했다.
- [ ] test mode 전용 60초 custom scheduled preset 하나를 준비할 수 있다.
- [ ] threshold preset 하나를 준비했다.
- [ ] 테스트 시작 전 required reconciliation event와 pending order 수를 기록했다.

### 3.3 브라우저와 네트워크

최소 조합:

- 최신 Chrome 데스크톱
- Safari 또는 iOS Safari
- Android Chrome 또는 Samsung Internet
- 팝업 허용·차단 각각 한 번
- Default와 Classic 테마 각각 한 번

DevTools에서 확인할 것은 URL 전체 값이 아니라 다음뿐이다.

- 동일 클릭에서 checkout 생성 요청 횟수
- callback의 HTTP status와 최종 redirect 위치
- pending cleanup DELETE 호출 여부
- secret·billingKey·authKey가 응답 body나 console에 노출되지 않는지

## 4. 공통 증거 기록 양식

각 시나리오마다 아래 정보를 한 행으로 남긴다.

| 항목 | 기록 내용 |
|---|---|
| Run ID | 예: `TOSS-MANUAL-20260712-01` |
| Scenario ID | 아래 문서의 ID |
| Commit | 실제 테스트한 commit hash |
| 환경 | TEST/STAGING/HARNESS/LIVE-APPROVAL |
| MID | 전체 값 대신 test/live와 구분 가능한 별칭·마지막 4자만 기록 |
| 대상 | user ID 또는 organization ID |
| 주문 | tradeNo/orderId의 앞·뒤 일부만 기록 |
| Provider 증거 | Toss 테스트 결제내역의 상태·금액·시각·transactionKey 일부 |
| Local 증거 | TopUp/SubscriptionOrder/WalletAutoRecharge status와 quota 전후 값 |
| Event 증거 | reconciliation event ID·type·required/resolved 상태 |
| 결과 | PASS/FAIL/BLOCKED |
| 정리 | 결제 취소·policy 취소·flag 복구·테스트 데이터 삭제 여부 |

다음 값은 증거에 원문으로 붙이지 않는다.

- secret key
- billingKey
- authKey
- 암호화 전 provider credential
- 전체 paymentKey
- 전체 카드번호·계좌번호

## 5. 병합 전 최소 수동 스모크 세트

시간이 제한되면 아래 항목은 반드시 실행한다.

| ID | 등급 | 핵심 판정 |
|---|---|---|
| CFG-01 | P0 | 올바른 테스트 키 세대가 원자적으로 저장되고 secret은 UI에 재노출되지 않음 |
| PAY-01 | P0 | 개인 KRW 충전이 정확히 한 번 결제·지급됨 |
| PAY-02 | P0 | quota 입력 충전의 서버 quote와 실제 지급 quota가 일치함 |
| PAY-03 | P0 | 빠른 중복 클릭·새로고침·callback 재방문에도 주문·지급이 1건 |
| PAY-05 | P0 | 오래된 quote·변조 callback이 거부되고 오지급하지 않음 |
| PAY-09 | P0 | 늦게 도착한 이전 quote가 최신 입력·확인 상태를 덮지 않음 |
| ORG-01 | P0 | 조직 owner만 조직 결제를 실행할 수 있음 |
| CAN-01 | P0 | 지급 전 취소는 지급을 막고, 지급 후 취소는 대사 이벤트를 남김 |
| SUB-01 | P0 | 구독 빌링키 발급·최초 결제·구독 생성이 정확히 한 번 |
| SUB-03 | P0 | 주문 전 조건 변경은 재확인되고 주문 후 검증된 snapshot은 바뀌지 않음 |
| SUB-08 | P0 | billing ISSUE·최초 charge 응답 유실이 새 key·새 charge 없이 복구됨 |
| AUTO-01 | P0 | test custom scheduled 자동충전의 각 logical cycle이 한 번씩 실행됨 |
| AUTO-04 | P0 | worker 두 개가 경쟁해도 한 cycle에 provider charge가 한 번 |
| AUTO-09 | P0 | wallet ISSUE·charge 응답 유실이 activate·지급·폐기를 중복하지 않음 |
| WH-01 | P0 | 실제 Toss 웹훅이 10초 안에 완료 또는 retryable non-2xx이고 중복 수신이 무해함 |
| OPS-01 | P0 | callback·webhook 유실 뒤 Transaction 대사로 한 번만 복구됨 |
| OPS-11 | P0 | key rotation 뒤 old ambiguous attempt를 새 namespace로 POST하지 않음 |
| SEC-01 | P0 | 로그·API·관리 UI에 secret·billingKey·authKey가 노출되지 않음 |

## 6. 설정·활성화 시나리오

### CFG-01 — 정상 테스트 설정 원자 저장

- 등급/환경: P0 / TEST
- 사전 조건: 세 결제 flag가 모두 꺼져 있다.
- 절차:
  1. A1이 일반결제·billing 테스트 client/secret 쌍, test mode, 단가, 최소 충전액을 한 화면에서 저장한다.
  2. 페이지를 새로고침한다.
  3. 일반결제, billing, wallet 자동충전을 순서대로 활성화한다.
- 기대 결과:
  - 일반 저장 요청은 변경된 key만 보내며 credential을 건드렸을 때 matching pair를 함께 보낸다.
  - 제출된 변경은 기존 14개 complete generation에 원자적으로 반영되고 revision·attestation도 같은 경계에서 바뀐다.
  - client key는 필요한 화면에서만 보이고 secret은 다시 표시되지 않는다.
  - wallet 자동충전은 billing이 꺼져 있으면 활성화할 수 없다.
  - repair/maintenance 경고가 없다.
- 증거/정리: 설정 revision 시각과 flag만 기록하고 secret 값은 기록하지 않는다. 테스트 종료 시 초기 flag로 복원한다.

### CFG-02 — 잘못된 키 조합 거부

- 등급/환경: P0 / TEST
- 절차: 다음을 각각 저장해 본다.
  1. client와 secret 위치를 바꾼다.
  2. `gck/gsk` 결제위젯 키를 넣는다.
  3. test client와 live secret을 섞는다.
  4. client만 바꾸고 matching secret을 누락한다.
  5. 단가 0·음수·비정상 큰 값과 최소 충전액 99·소수를 넣는다.
  6. 동일 option key를 한 요청에 두 번 넣는다.
- 기대 결과:
  - 각 요청이 명확한 validation 오류로 전체 거부된다. 최소 카드 청구 안전 하한은 100 KRW다.
  - 기존 정상 generation과 flag는 바뀌지 않는다.
  - secret 원문이 오류 메시지나 로그에 없다.
- 정리: 정상 test key 세대를 다시 확인한다.

### CFG-03 — 결제 중 kill switch

- 등급/환경: P0 / STAGING
- 사전 조건: U1이 결제 확인창까지 열었지만 Toss 인증을 끝내지 않았다.
- 절차:
  1. A1이 `TossEnabled=false`로 저장한다.
  2. U1이 열려 있던 결제를 완료하려 한다.
  3. 새 브라우저에서 새 checkout도 시도한다.
- 기대 결과:
  - 새 checkout은 차단된다.
  - 이미 열린 결제도 provider POST 직전 fresh DB gate에서 차단된다.
  - 로컬 quota는 늘지 않는다.
  - provider에서 결제가 이미 확정된 경계라면 pending/refund reconciliation으로 남고 새 POST는 생기지 않는다.
- 증거/정리: TopUp 상태와 reconciliation event를 기록한 뒤 flag를 복구한다.

### CFG-04 — repair·maintenance 복구

- 등급/환경: P0(legacy upgrade 배포) 또는 P1(일반 회귀) / HARNESS
- 사전 조건: 운영 데이터가 없는 disposable DB snapshot.
- 절차:
  1. 테스트 fixture로 Toss option 한 행 누락 또는 attestation 불일치를 만든다.
  2. 애플리케이션을 시작하고 설정 화면을 연다.
  3. repair token과 all-disabled complete-set 복구 UI를 확인한다.
  4. token을 받은 뒤 DB generation을 다시 바꾸고 오래된 token으로 repair를 시도한다.
  5. 최신 token으로 14개 전체를 제출하고 maintenance retry를 수행한다.
- 기대 결과:
  - Toss만 안전 정지되고 애플리케이션 전체는 기동한다.
  - 오래된 token과 부분 집합은 거부된다.
  - repair 직후 세 결제 flag는 꺼져 있다.
  - maintenance 완료 전 enable·plan 변경·provider POST가 차단된다.
- 정리: fixture DB를 폐기한다. 손상 시험을 정상 staging DB에 남기지 않는다.

## 7. 개인·조직 일반 충전

### PAY-01 — 개인 지갑 KRW 입력 성공

- 등급/환경: P0 / TEST
- 사전 조건: U1 시작 quota와 현재 Toss 최소 충전액을 기록하고, Default에서는 Toss만 활성화해 KRW/quota 토글이 보이게 한다.
- 절차:
  1. Default 개인 지갑에서 KRW 모드를 고른다.
  2. 설정 최소액 이상 소액을 입력한다.
  3. 서버 quote의 결제 KRW와 지급 credit/quota를 확인한다.
  4. 결제를 한 번 클릭하고 테스트 카드 인증을 완료한다.
  5. 성공 redirect와 지갑·충전 내역을 확인한다.
- 기대 결과:
  - Toss 테스트 결제내역의 amount와 서버 quote가 같다.
  - TopUp은 한 건이며 `pending`에서 `success`로 한 번만 전이한다.
  - quota 증가량은 TopUp에 저장된 immutable quota와 같다.
  - 개인 성공 경로는 정상 화면으로 이동하고 callback query가 반복 처리되지 않는다.
- 정리: 테스트 결제를 취소한 뒤 disposable DB snapshot도 복원한다. 결제 취소만으로 이미 지급된 local quota가 자동 원복된다고 가정하지 않는다.

### PAY-02 — quota 입력 성공과 반올림 경계

- 등급/환경: P0 / TEST
- 사전 조건: Default 토글 검증에서는 Toss-only gateway 구성을 사용한다. 다른 결제수단이 함께 켜진 구성은 UI-01의 혼합 gateway 항목으로 별도 시험한다.
- 절차:
  1. Default에서 quota/credit 입력 모드를 선택한다.
  2. 작은 값, 단가로 나누어떨어지지 않는 값, 큰 정상값을 각각 quote한다.
  3. 한 건만 실제 테스트 결제로 완료한다.
- 기대 결과:
  - 입력 quota는 고정되고 KRW가 서버에서 계산된다.
  - 확인창·checkout 응답·Toss 승인 금액·TopUp amount가 모두 같다.
  - 지급 quota는 결제 뒤 현재 단가로 재계산되지 않고 주문 snapshot을 사용한다.
  - Classic은 별도 KRW/quota 토글 대신 기존 quota 중심 UX를 사용하므로 화면 차이를 결함으로 오판하지 않는다.
- 정리: 완료한 결제를 취소하고 disposable DB snapshot을 복원한다.

### PAY-03 — 중복 클릭·뒤로가기·callback 재방문

- 등급/환경: P0 / TEST
- 절차:
  1. 결제 버튼을 빠르게 여러 번 클릭한다.
  2. 결제창 로딩 중 화면을 전환했다가 돌아온다.
  3. 결제를 완료한 뒤 callback URL을 새로고침하고 브라우저 뒤로가기를 반복한다.
  4. 같은 callback URL을 새 탭에서도 한 번 연다.
- 기대 결과:
  - 활성 Toss iframe/window와 checkout order가 한 개다.
  - provider 승인과 quota 지급이 각각 한 번이다.
  - 이미 처리된 callback은 성공 결과를 재사용하거나 무해하게 종료한다.
  - URL query는 UI에서 한 번 소비된 뒤 history replace로 제거된다.
- 증거: Network 요청 수, TopUp 수, Toss 테스트 결제내역 수, quota delta를 함께 기록한다.

### PAY-04 — 사용자가 결제창 취소

- 등급/환경: P0 / TEST
- 절차:
  1. 결제창을 연 뒤 구매자 취소 버튼으로 닫는다.
  2. 브라우저 팝업 차단 상태에서도 한 번 시도한다.
  3. 페이지 새로고침 뒤 새 결제를 시작한다.
- 기대 결과:
  - `PAY_PROCESS_CANCELED` 계열은 성공으로 표시되지 않는다.
  - quota가 늘지 않고 provider 승인 내역이 없다.
  - orphan iframe이 남지 않으며 다음 결제창이 정상적으로 열린다.
  - durable pending order가 생성됐다면 cleanup 경로로 취소·만료된다.

### PAY-05 — stale quote와 가격 변경

- 등급/환경: P0 / STAGING
- 절차:
  1. U1이 quote를 받아 확인창을 연 상태로 멈춘다.
  2. A1이 Toss 단가 또는 사용자 group ratio를 변경한다.
  3. U1이 이전 확인창으로 결제를 진행한다.
  4. 새 quote를 받은 뒤 다시 진행한다.
- 기대 결과:
  - 오래된 확인 snapshot은 결제창을 열지 않거나 서버에서 거부된다.
  - pending reservation이 있으면 정리된다.
  - 새 quote에는 변경된 조건이 반영된다.
  - 이전 금액으로 provider charge나 quota 지급이 일어나지 않는다.
- 정리: 단가·group 설정을 원복한다.

### PAY-06 — callback 파라미터 변조

- 등급/환경: P0 / HARNESS
- 사전 조건: 테스트 키, 브라우저 redirect를 일시 정지할 수 있는 QA proxy.
- 절차:
  1. 정상 인증 결과의 `amount`, `orderId`, `paymentKey`를 각각 다른 값으로 바꿔 callback한다.
  2. U2의 callback 값을 U1 주문에 교차 사용한다.
  3. 같은 orderId에 서로 다른 paymentKey를 순서대로 보낸다.
- 기대 결과:
  - 어느 변조도 quota를 지급하지 않는다.
  - 최초 durable binding과 충돌하는 paymentKey는 거부된다.
  - provider에 이미 결제가 있다면 required reconciliation/refund fence가 남는다.
  - 서버 로그는 원문 paymentKey를 노출하지 않는다.
- 정리: 남은 테스트 provider 결제와 local pending을 대사해 0건으로 만든다.

### PAY-07 — 인증 뒤 서버 중단과 복구

- 등급/환경: P0 / STAGING
- 절차:
  1. success callback이 서버의 `/v1/payments/confirm` POST까지 보내게 하고, Toss가 요청을 처리한 뒤 응답만 유실되도록 QA egress proxy에서 연결을 끊는다.
  2. 10분 승인 유효시간 안에 노드를 재시작한다.
  3. 브라우저 callback을 새로고침한다.
  4. 같은 callback을 다시 한 번 실행한다.
- 기대 결과:
  - durable pending 주문이 유지된다.
  - 복구는 GET 우선·동일 멱등 namespace로 수행된다.
  - provider 승인과 local 지급이 각각 한 번이다.
  - 결과가 불명확하면 실패로 단정하지 않고 pending/reconciliation에 남는다.

### PAY-08 — 사용자 상태 변경과 정산 경쟁

- 등급/환경: P1 / HARNESS
- 절차:
  1. U1이 결제창을 열고 인증 완료 직전에 멈춘다.
  2. A1이 U1을 disable하거나 삭제하려 한다.
  3. 동시에 인증 완료를 진행한다.
- 기대 결과:
  - lifecycle 변경과 provider attempt가 정의된 lock 순서로 직렬화된다.
  - 대상 비활성화 뒤 pristine한 새 provider POST는 실행되지 않는다.
  - provider 요청이 이미 시도됐고 authoritative GET이 `DONE`이면 결제를 유실하지 않도록 정확히 한 번 정산하거나 durable reconciliation/refund event를 남긴다.
  - deadlock·중복 지급이 없다.

### PAY-09 — quote 응답 역전과 loading 상태

- 등급/환경: P0 / HARNESS
- 사전 조건: Default 개인·조직 지갑, `/toss/amount` 응답을 지연할 수 있는 browser proxy.
- 절차:
  1. 금액 A의 quote 응답을 지연한다.
  2. 곧바로 금액 B를 선택해 quote B를 먼저 완료한다.
  3. 이후 quote A 응답을 해제한다.
  4. loading, preview, 확인 dialog를 관찰하고 결제 직전까지 진행한다.
- 기대 결과:
  - 최신 입력 B의 quote만 유지된다.
  - 늦은 A가 금액·loading·confirmation snapshot을 덮지 않는다.
  - checkout은 B의 immutable quote와 일치할 때만 열리고 A로 provider POST하지 않는다.

### ORG-01 — 조직 owner와 member 권한

- 등급/환경: P0 / TEST
- 절차:
  1. O1로 조직 지갑 quote·결제를 완료한다.
  2. M1로 같은 조직 지갑을 연다.
  3. M1이 O1의 브라우저 요청을 복사해 조직 `/toss/amount`, `/toss/pay`를 직접 호출해 본다.
- 기대 결과:
  - O1 결제는 조직 TopUp으로 기록되고 조직 quota만 증가한다.
  - M1에는 owner 전용 결제 제어가 보이지 않는다.
  - 직접 `/organization/wallet` shell이 렌더링되더라도 결제 제어가 활성화되지 않는다.
  - 복사 요청도 HTTP error 또는 HTTP 200의 `success:false` 권한 오류로 차단되며 mutation이 없다.
  - M1 개인 지갑으로 잘못 라우팅되지 않는다.

### ORG-02 — 결제 중 owner·membership 변경

- 등급/환경: P1 / HARNESS
- 절차:
  1. O1이 조직 결제창을 연다.
  2. 별도 관리자 세션에서 owner를 변경하거나 조직을 disable한다.
  3. O1이 인증을 완료한다.
- 기대 결과:
  - 오래된 owner 권한으로 새 provider POST가 실행되지 않는다.
  - 조직 quota 오지급이 없다.
  - 이미 provider 결제가 존재하면 refund/reconciliation fence가 남는다.

## 8. 취소·부분취소·환불

### CAN-01 — 지급 전 취소와 지급 후 취소

- 등급/환경: P0 / STAGING
- 절차 A, 지급 전:
  1. 서버의 `/v1/payments/confirm` POST가 Toss에서 성공한 뒤 local settlement 전에 응답을 유실시키거나 process를 중단한다.
  2. Toss 테스트 결제내역에서 해당 승인 결제를 취소한다.
  3. callback, recovery worker 또는 웹훅 처리를 재개한다.
- 기대 결과 A: quota를 지급하지 않고 cancellation evidence를 기록한다.
- 절차 B, 지급 후:
  1. 정상 충전으로 quota를 받은 뒤 Toss 테스트 결제내역에서 전액 취소한다.
  2. 웹훅·Transaction 대사를 기다린다.
- 기대 결과 B:
  - 이미 소비 가능해진 quota를 무조건 음수 clawback하지 않는다.
  - cancellation/financial mismatch 운영 이벤트가 생성된다.
  - 동일 취소 웹훅 재전송으로 이벤트·차감 계산이 중복되지 않는다.

### CAN-02 — 여러 번의 부분 취소

- 등급/환경: P1 / TEST
- 사전 조건: Toss API 실행 도구 또는 secret을 노출하지 않는 승인된 QA script.
- 절차:
  1. 정상 테스트 카드 결제를 완료한다.
  2. 서로 다른 금액으로 두 번 부분 취소한다.
  3. 첫 cancellation payload를 다시 전달한다.
  4. 나머지 금액을 전액 취소한다.
- 기대 결과:
  - 각 취소 transactionKey가 별도 ledger로 기록된다.
  - 누적 취소액이 original amount를 넘지 않는다.
  - 동일 transactionKey 재전송은 무해하다.
  - 이미 지급된 주문은 cancellation reconciliation event를 유지하고 quota를 자동 회수하지 않는다.
  - `refund_required`는 미지급·이행 불가능 결제의 잔액 환불 경로에서만 사용한다.

### CAN-03 — 환불이 필요한 미지급 결제

- 등급/환경: P0 / HARNESS
- 절차:
  1. provider 인증/승인과 local fulfillment 사이에 대상 삭제, quota overflow 또는 contract mismatch를 만든다.
  2. recovery worker와 webhook을 실행한다.
  3. 같은 refund 작업을 여러 번 재시도한다.
- 기대 결과:
  - TopUp은 `refund_pending` 또는 안전한 실패 상태에 머문다.
  - 동일 balance에서는 같은 refund idempotency key를 사용한다.
  - 부분 환불로 balance가 변한 경우에만 다음 operation이 진행된다.
  - 관리자 note만으로 `refund_required`를 강제 resolved할 수 없다.

### CAN-04 — 가상계좌 defensive 경로

- 등급/환경: P2 / HARNESS
- 주의: 현재 사용자 UI는 CARD 결제만 요청한다. 이 시나리오는 legacy·변조·향후 데이터에 대한 방어 확인이며 필수 사용자 흐름이 아니다.
- 절차:
  1. 테스트 환경에서 가상계좌를 발급하고 개발자센터에서 입금 전 취소를 시험한다.
  2. 별도 건은 개발자센터의 입금처리 뒤 환불계좌 없이 취소를 유도한다.
  3. 은행 환불 상태가 완료되기 전과 후를 각각 대사한다.
- 기대 결과:
  - 입금 전에는 잘못된 부분취소를 시도하지 않는다.
  - 입금 후 refundReceiveAccount가 없으면 자동 반복 취소하지 않고 수동 required event를 유지한다.
  - 은행 환불 완료 증거 전에는 full refund로 닫지 않는다.
- 정리: 실제 계좌정보를 fixture·로그에 남기지 않는다.

## 9. 구독 자동결제

### SUB-01 — 빌링키 발급과 최초 결제 성공

- 등급/환경: P0 / TEST
- 절차:
  1. U1이 활성 plan의 조건과 KRW 금액을 확인한다.
  2. Toss 자동결제 카드 등록창을 연다.
  3. 등록 버튼을 빠르게 여러 번 눌러 `/api/subscription/toss/pay`가 한 번인지 확인한다.
  4. 공식 테스트 환경 안내에 따라 카드 인증을 완료한다. 본인인증 입력이 필요하면 테스트 값 `000000`을 사용한다.
  5. callback, 최초 billing charge, 구독 생성 결과를 확인한다.
- 기대 결과:
  - displayed plan snapshot과 checkout fingerprint가 일치한다.
  - billing key ISSUE와 최초 charge가 각각 한 번이다.
  - SubscriptionOrder는 `success`, UserSubscription은 한 건이다.
  - billingKey·authKey·provider secret은 API·로그에 원문으로 나타나지 않는다.
  - 카드사·마스킹 카드번호 증거는 현재 구독 상태 UI가 아니라 Toss 테스트 대시보드와 제한된 DB 확인에서 수집한다.

### SUB-02 — 카드 등록 취소·실패

- 등급/환경: P0 / TEST
- 절차:
  1. 카드 등록창을 구매자가 취소한다.
  2. 잘못된 테스트 카드 정보로 실패도 한 번 만든다.
  3. callback URL을 새로고침한다.
- 기대 결과:
  - 구독·billing key·최초 charge가 생성되지 않는다.
  - pending subscription order cleanup DELETE가 실행되거나 worker가 정리한다.
  - 취소는 일반 시스템 오류처럼 반복 경고하지 않는다.

### SUB-03 — plan 변경 뒤 stale checkout

- 등급/환경: P0 / STAGING
- 절차 A, 주문 생성 전:
  1. U1이 plan 확인 dialog를 열되 `/api/subscription/toss/pay` 전에서 멈춘다.
  2. A1이 가격·기간·quota·활성 상태 중 하나를 변경한다.
  3. U1이 기존 dialog로 카드 등록을 계속한다.
- 절차 B, 주문 생성 후:
  1. 별도 주문에서 `/api/subscription/toss/pay` 응답과 fingerprint 검증까지 완료한다.
  2. A1이 현재 plan을 변경한 뒤 기존 billing auth를 완료한다.
- 기대 결과:
  - A에서는 서버 응답이 사용자가 본 조건과 다르면 SDK를 열지 않고 pending reservation을 정리하며 새 조건 확인을 요구한다.
  - B에서는 주문 생성 시 검증된 `PlanSnapshot`이 immutable contract다. 이후 현재 plan 변경이 기존 주문 조건을 덮어쓰지 않는다.
  - B의 provider amount·기간·quota는 현재 변경된 plan이 아니라 검증된 주문 snapshot과 일치한다.

### SUB-04 — 두 탭 동시 구매와 구매 제한

- 등급/환경: P0 / TEST
- 사전 조건: MaxPurchasePerUser가 확인 가능한 plan.
- 절차:
  1. U1이 두 브라우저 탭에서 같은 plan checkout을 동시에 시작한다.
  2. 두 카드 등록을 거의 동시에 완료한다.
- 기대 결과:
  - pending order도 reservation으로 계산된다.
  - 허용 수량을 넘는 두 번째 provider charge가 실행되지 않는다.
  - 한 건만 구독 권한을 만들며 불필요 billing key는 폐기된다.

### SUB-05 — renewal 성공·카드 거절·재시도

- 등급/환경: P0 / HARNESS
- 사전 조건: 최소 1시간 이상 period의 테스트 plan과 scheduler를 안전하게 한 cycle 실행할 수 있는 fixture.
- 절차:
  1. 정상 renewal 한 cycle을 실행한다.
  2. 다음 cycle은 `TossPayments-Test-Code: REJECT_CARD_PAYMENT`가 적용되도록 QA fault injector를 설정한다.
  3. 같은 logical attempt를 재실행한다.
  4. fault를 제거하고 정책상 허용되는 retry를 실행한다.
- 기대 결과:
  - 정상 cycle은 charge와 구독 기간 연장이 각각 한 번이다.
  - 카드 거절은 실패 횟수·다음 처리 상태에 반영되고 기간을 연장하지 않는다.
  - 같은 cycle/order identity로 중복 charge하지 않는다.
  - 24시간 operational grace 밖에서는 새 charge를 안전 정지한다.

### SUB-06 — 자동갱신 전체 해제와 billing key 폐기

- 등급/환경: P0 / TEST
- 절차:
  1. U1에 Toss 자동갱신 구독을 둘 이상 준비한다.
  2. 지갑의 단일 “전체 Toss 자동갱신 해제” 동작을 실행한다.
  3. 같은 요청을 반복한다.
  4. billing key 삭제 성공 또는 BILLING_DELETED 웹훅을 확인한다.
- 기대 결과:
  - 모든 대상 구독은 다음 renewal 대상에서 빠진다.
  - billing key 상태는 `active → pending_revocation → revoked`로 단조 전이한다.
  - 반복 요청과 중복 BILLING_DELETED는 무해하다.
  - 이미 시작된 불명확 charge가 있으면 즉시 삭제로 덮지 않고 대사한다.

### SUB-07 — billing credential 교체 중 pending 주문

- 등급/환경: P1 / STAGING
- 사전 조건: 같은 테스트 MID에서 안전하게 교체 가능한 두 key generation 또는 회전 절차.
- 절차:
  1. old generation으로 billing auth/pending order를 만든다.
  2. 새 checkout을 중지한 뒤 key generation을 교체한다.
  3. old pending 복구와 new checkout을 각각 수행한다.
- 기대 결과:
  - old pending은 저장된 exact credential namespace로만 조회·재시도한다.
  - new checkout은 새 generation을 사용한다.
  - MID가 다른 key로 cross-credential retry하지 않는다.
  - old credential이 이미 폐기되어 결과를 증명할 수 없으면 자동 charge보다 required event를 선택한다.

### SUB-08 — billing ISSUE와 최초 charge 응답 유실 분리

- 등급/환경: P0 / HARNESS
- 절차 A, ISSUE:
  1. billing auth 뒤 `/billing/authorizations/issue`가 Toss에서 처리된 직후 응답만 유실시킨다.
  2. process를 재시작하고 동일 callback·cleanup worker를 실행한다.
- 절차 B, 최초 charge:
  1. 별도 주문에서 billing key attach까지 성공시킨다.
  2. 최초 billing charge가 Toss에서 처리된 직후 응답만 유실시킨다.
  3. process를 재시작하고 복구한다.
- 기대 결과:
  - A는 encrypted authKey/customerKey snapshot과 exact credential로 동일 ISSUE만 복구한다.
  - 발급 key를 attach할 수 없거나 authorization window가 끝났으면 pending-revocation으로 보존해 삭제한다.
  - B는 GET-before-POST로 같은 orderId를 복구하고 최초 charge와 구독 권한을 각각 한 번만 만든다.
  - A와 B 어느 쪽도 새 credential·새 order namespace로 대체 POST하지 않는다.

## 10. 지갑 자동충전

### AUTO-01 — test custom scheduled 등록과 즉시결제

- 등급/환경: P0 / TEST
- 사전 조건: test mode 전용 `custom_seconds=60` preset, 소액 amount, chargeImmediately=true.
- 절차:
  1. U1이 표시된 cadence·금액·즉시결제 조건을 확인한다.
  2. 등록 버튼을 빠르게 여러 번 눌러 wallet auto-recharge 생성 요청이 한 번인지 확인한다.
  3. 카드 등록을 완료한다.
  4. 즉시 charge와 60초 뒤 scheduler cycle을 각각 관찰한다.
- 기대 결과:
  - 표시된 preset fingerprint와 저장 policy가 같다.
  - pending policy와 billing auth session이 한 개다.
  - 즉시 charge는 한 번이고 다음 cycle도 별도 logical identity로 한 번이다.
  - 각 성공 TopUp의 immutable quota만큼 지갑이 증가한다.
  - test custom preset은 test mode를 끄면 사용자 목록에서 사라진다.
- 정리: policy를 취소하고 billing key revocation 완료를 확인한다.

### AUTO-02 — monthly scheduled는 즉시 청구하지 않음

- 등급/환경: P0 / TEST
- 절차:
  1. monthly preset을 만들 때 chargeImmediately를 켜 보려 한다.
  2. 사용자가 monthly 자동충전을 등록한다.
- 기대 결과:
  - 관리자 저장 시 monthly의 chargeImmediately가 false로 정규화된다.
  - 등록 직후 charge하지 않는다.
  - nextChargeTime은 다음 달 1일 기준이며 생성일 기준 drift가 없다.

### AUTO-03 — threshold 자동충전

- 등급/환경: P0 / HARNESS
- 절차:
  1. U1이 threshold quota와 충전 금액을 확인하고 등록한다.
  2. disposable DB fixture 또는 정상 사용 흐름으로 잔액을 threshold 위·같음·아래로 만든다.
  3. 각 상태에서 scheduler를 실행한다.
- 기대 결과:
  - threshold 위에서는 charge하지 않는다.
  - 정책 정의의 경계에서만 charge한다.
  - charge 뒤 cooldown·daily count가 갱신되고 같은 잔액에서 연속 중복 charge하지 않는다.
  - 표시·저장·판정 모두 raw money 재계산이 아니라 threshold quota를 사용한다.

### AUTO-04 — 두 worker의 동일 cycle 경쟁

- 등급/환경: P0 / HARNESS
- 사전 조건: 같은 DB를 보는 두 master test worker, 결제가 도래한 policy 하나.
- 절차:
  1. 두 worker의 iteration을 동시에 시작한다.
  2. provider 테스트 결제내역과 local TopUp을 확인한다.
  3. 같은 iteration을 다시 실행한다.
- 기대 결과:
  - composite logical attempt identity는 한 건이다.
  - provider charge, TopUp success, quota 지급이 각각 한 번이다.
  - 패배 worker는 winner를 조회하고 새 orderId를 만들지 않는다.

### AUTO-05 — pending 등록과 active policy 취소

- 등급/환경: P0 / TEST
- 절차:
  1. 카드 등록창을 연 뒤 취소하여 pending policy를 만든다.
  2. UI cleanup과 pending DELETE를 확인한다.
  3. 별도 active policy를 정상 등록한 뒤 취소한다.
  4. 취소를 반복한다.
- 기대 결과:
  - pending row는 안전하게 cancelled/expired되고 billing key가 남지 않는다.
  - active policy는 더 이상 scheduler 대상이 아니다.
  - 불명확 provider attempt가 있으면 `cancel_pending`으로 유지해 GET/cleanup을 거친다.
  - 반복 취소로 새 provider DELETE나 charge가 생기지 않는다.
  - 취소와 provider `DONE`이 경쟁하면 새 POST는 차단하되 이미 완료된 결제는 한 번 정산하거나 durable reconciliation으로 남긴다.

### AUTO-06 — preset 변경과 stale 동의

- 등급/환경: P0 / STAGING
- 절차:
  1. Default에서는 inline preset을 선택한 상태로, Classic에서는 preset 확인 dialog를 연 상태로 멈춘다.
  2. A1이 amount, threshold, cadence, target scope, immediate 조건 중 하나를 변경한다.
  3. U1이 이전 dialog로 등록을 계속한다.
- 기대 결과:
  - stale fingerprint가 거부된다.
  - 이전 조건의 policy·billing charge가 만들어지지 않는다.
  - 변경된 preset을 다시 확인해야 한다.

### AUTO-07 — 구독과 자동충전 상호배타성

- 등급/환경: P1 / TEST
- 절차:
  1. U1에 active auto-renew subscription이 있을 때 scheduled·threshold 생성을 시도한다.
  2. active scheduled가 있을 때 subscription과 threshold를 시도한다.
  3. 기존 policy 취소 동작은 실행한다.
- 기대 결과:
  - active subscription에서 새 auto-recharge 생성은 현재 UI 정책에 따라 잠긴다.
  - 데이터 로딩 중 잠깐 활성화되는 틈이 없다.
  - 이미 존재하는 policy의 취소는 항상 가능하다.
  - 반대 방향인 active auto-recharge 상태에서는 subscription 구매 버튼이 보일 수 있고 backend는 shared billing-key 계약을 지원한다. 제품 정책상 상호배제가 필요하다면 이를 별도 요구사항·UX 결함으로 기록하며 현재 테스트에서 서버 거절을 기대하지 않는다.

### AUTO-08 — 조직 자동충전 권한과 owner 교체

- 등급/환경: P1 / STAGING
- 절차:
  1. O1이 조직 auto-recharge를 등록한다.
  2. M1이 조회·등록·취소 API를 직접 호출한다.
  3. owner를 교체한 뒤 기존 policy의 다음 cycle을 실행한다.
- 기대 결과:
  - M1의 관리 동작은 차단된다.
  - 정책 대상은 개인이 아니라 조직 quota다.
  - owner lifecycle 변경 뒤 pristine 새 POST는 차단된다.
  - 이미 attempted이고 provider `DONE`인 charge는 유실하지 않도록 정확히 한 번 정산하거나 durable reconciliation으로 남긴다.

### AUTO-09 — wallet billing ISSUE와 charge 응답 유실 분리

- 등급/환경: P0 / HARNESS
- 절차 A, ISSUE:
  1. auto-recharge billing auth 뒤 ISSUE가 Toss에서 처리된 직후 응답만 유실시킨다.
  2. policy 취소 전·후에 각각 process를 재시작해 cleanup을 실행한다.
- 절차 B, charge:
  1. 별도 active policy의 immediate 또는 due charge를 실행한다.
  2. Toss가 charge를 처리한 직후 응답만 유실시키고 worker를 failover한다.
- 기대 결과:
  - A의 active 계약은 key를 한 번 attach하고, 취소된 계약은 activate·charge하지 않은 채 key를 pending-revocation으로 보존해 삭제한다.
  - B는 exact orderId·credential로 GET-before-POST하고 TopUp·quota를 한 번만 반영한다.
  - 동일 callback·worker retry로 provider key, policy, charge, quota가 중복되지 않는다.

## 11. 웹훅·재전송·대사

### WH-01 — 실제 Toss 웹훅 수신과 응답 시간

- 등급/환경: P0 / STAGING
- 절차:
  1. 테스트 MID에 `/api/toss/webhook` HTTPS URL을 등록한다.
  2. 테스트 카드 결제와 취소를 각각 발생시킨다.
  3. Toss 개발자센터 웹훅 전송 기록을 확인한다.
- 기대 결과:
  - 정상적으로 처리 완료된 공식 source 요청은 10초 안에 200이다.
  - provider 조회 지연, DB 오류, identity를 증명할 수 없는 BILLING_DELETED처럼 재시도가 필요한 상태는 제한 시간 안에 503 등 retryable non-2xx를 반환한다.
  - handler 내부 목표인 8초와 socket write deadline 안에서 끝난다.
  - callback과 webhook 순서가 바뀌어도 지급은 한 번이다.
  - 웹훅 payload만 믿지 않고 authoritative GET 결과와 local contract를 대조한다.

### WH-02 — 실패 뒤 재전송과 중복 처리

- 등급/환경: P0 / STAGING
- 사전 조건: 애플리케이션 앞 QA proxy가 첫 요청만 503으로 돌려줄 수 있다.
- 절차:
  1. 첫 webhook을 proxy에서 503으로 끝낸다.
  2. 이후 proxy를 정상화한다.
  3. 개발자센터 재전송 또는 “다시 시도”를 실행한다.
  4. 성공 payload를 한 번 더 재전송한다.
- 기대 결과:
  - Toss 기록이 전송 중에서 성공으로 바뀐다.
  - 재전송은 같은 결제를 중복 지급·중복 해지하지 않는다.
  - cancellation transactionKey와 BILLING_DELETED가 멱등 처리된다.

### WH-03 — source IP와 X-Forwarded-For 경계

- 등급/환경: P0 / HARNESS
- 사전 조건: production과 분리된 proxy, 테스트 runner IP만 임시 허용한 설정.
- 절차:
  1. 비신뢰 peer가 공식 Toss IP를 X-Forwarded-For에 넣어 요청한다.
  2. 명시적으로 trusted인 proxy가 올바른 client chain을 전달한다.
  3. 여러 hop chain을 만들어 오른쪽부터 trusted proxy를 제거한 뒤 처음 만나는 untrusted hop을 허용 IP와 비허용 IP로 각각 설정한다.
- 기대 결과:
  - 1번은 403이고 DB mutation이 없다.
  - 2번은 source 검사를 통과한다.
  - 3번은 오른쪽 기준 첫 untrusted hop이 allowlist에 있을 때만 통과하고 그렇지 않으면 403이다.
  - 전역 사용자 rate limit 예외는 정확한 POST `/api/toss/webhook`과 trusted source 조합에만 적용된다.
- 정리: QA source/proxy CIDR override를 즉시 제거한다.

### WH-04 — body·deadline·method 제한

- 등급/환경: P1 / HARNESS
- 절차:
  1. malformed JSON, 빈 body, 1MiB 초과 body를 보낸다.
  2. 같은 path에 GET·PUT을 보낸다.
  3. body를 매우 느리게 전송한다.
- 기대 결과:
  - malformed/oversize/slow 요청은 4xx 또는 503으로 제한 시간 안에 종료된다.
  - 지원하지 않는 method는 handler로 진입하지 않는다.
  - goroutine·connection·DB lock이 장시간 남지 않는다.
  - 오류 응답에 payload·secret이 반사되지 않는다.

### WH-05 — BILLING_DELETED

- 등급/환경: P1 / STAGING
- 절차:
  1. active test billing key를 Toss API로 삭제해 BILLING_DELETED를 발생시킨다.
  2. 동일 webhook을 재전송한다.
  3. 알 수 없는 billingKey event도 격리 환경에서 보낸다.
- 기대 결과:
  - 정확한 hashed identity의 key만 revoked된다.
  - 연결된 subscription/policy는 새 charge를 만들지 않는다.
  - 중복 event는 무해하다.
  - legacy identity를 증명할 수 없으면 200으로 삼키지 않고 retry/reconciliation 상태를 남긴다.

### WH-06 — 관리자 reconciliation event 조회·해결

- 등급/환경: P0 / STAGING
- 사전 조건: CAN-01~CAN-03 또는 OPS-01에서 만든 required event, A1 관리자 계정.
- 절차:
  1. `GET /api/admin/toss/reconciliation-events`에서 status·event_type·order_id filter를 확인한다.
  2. 상세 API로 provider evidence와 local 상태를 대조한다.
  3. 일반 사용자로 같은 API를 호출한다.
  4. 빈 note와 2,000자를 넘는 note로 resolve를 시도한다.
  5. 실제 보정·환불 확인 뒤 의미 있는 note로 resolve하고 같은 요청을 반복한다.
- 기대 결과:
  - 목록 기본값은 required이고 일반 사용자는 접근할 수 없다.
  - 빈 note·초과 note는 거부된다.
  - 해결 시 관리자 ID·시각·note가 감사 정보로 남는다.
  - cancellation/financial mismatch는 중복 resolve로 상태가 되돌아가지 않는다.
  - provider 잔액이 남은 `refund_required`는 note만으로 resolve할 수 없다.

## 12. 장애 복구·운영 시나리오

### OPS-01 — callback과 webhook 모두 유실된 결제

- 등급/환경: P0 / HARNESS
- 절차:
  1. callback이 서버의 provider confirm POST를 보내게 하고 Toss가 승인한 뒤 응답과 후속 local settlement만 유실시킨다.
  2. 같은 거래의 webhook도 QA proxy에서 차단한다.
  3. 애플리케이션을 재시작하고 Transaction reconciliation cycle을 실행한다.
- 기대 결과:
  - Transaction API가 거래를 찾아 local pending과 연결한다.
  - 유효한 contract는 한 번만 지급된다.
  - target·amount·MID가 맞지 않으면 지급하지 않고 required/refund event를 만든다.
  - cursor는 성공한 page 이후에만 전진한다.

### OPS-02 — reconciliation pagination·재시작·lease

- 등급/환경: P1 / HARNESS
- 절차:
  1. 한 page를 넘는 테스트 거래를 준비하되 테스트 API 분당 100건 제한을 넘지 않는다.
  2. page 중간에 worker를 종료하고 재시작한다.
  3. 두 노드에서 같은 source reconciliation을 동시에 시작한다.
- 기대 결과:
  - `startingAfter` checkpoint부터 재개한다.
  - recent lane과 historical lane이 서로 굶기지 않는다.
  - 한 source lease owner만 provider page를 처리한다.
  - 같은 transactionKey를 두 번 적용하지 않는다.
- 주의: 테스트 상점 Transaction API는 최근 3일 범위 제한이 있으므로 오래된 lane은 provider mock이 필요할 수 있다.

### OPS-03 — config generation과 다중 노드 stale snapshot

- 등급/환경: P0 / HARNESS
- 절차:
  1. 노드 A·B가 같은 DB를 보게 한다.
  2. A에서 설정 generation을 변경하고 B의 메모리 snapshot 갱신을 지연시킨다.
  3. B에서 새 checkout과 provider POST를 시도한다.
- 기대 결과:
  - DB revision·attestation·메모리 snapshot 불일치가 새 POST를 차단한다.
  - B가 fresh generation을 로드한 뒤에만 새 checkout이 가능하다.
  - 이미 완료된 provider payment의 GET·local reconciliation은 kill switch와 구분되어 계속 가능하다.

### OPS-04 — CRYPTO_SECRET 불일치와 복구

- 등급/환경: P0 / HARNESS
- 사전 조건: 암호화된 test credential이 든 DB clone.
- 절차:
  1. 다른 `CRYPTO_SECRET`으로 한 노드를 시작한다.
  2. 결제·billing·auto-recharge를 각각 시도한다.
  3. 원래 key로 되돌려 재시작한다.
- 기대 결과:
  - 잘못된 key에서는 복호화 실패를 감지하고 모든 신규 provider POST를 차단한다.
  - plaintext나 암호문이 오류에 노출되지 않는다.
  - 임의 새 key로 repair하도록 유도하지 않는다.
  - 원래 key 복구 뒤 정상 generation을 다시 읽고 동작한다.
- 정리: DB clone과 잘못된 key를 폐기한다.

### OPS-05 — provider timeout·409·429·5xx·불완전 2xx

- 등급/환경: P0 / HARNESS
- 절차:
  1. 승인, ISSUE, 최초 charge, renewal, wallet charge 각각에 대해 한 가지 ambiguous response를 주입한다.
  2. 동일 작업을 재시도한다.
  3. GET에서 success, not found, credential rejection을 각각 반환한다.
- 기대 결과:
  - ambiguous response를 definitive failure로 닫지 않는다.
  - 같은 API key·endpoint·method·idempotency namespace만 재사용한다.
  - GET success면 local settlement만 한 번 수행한다.
  - 결과를 증명할 수 없으면 required/pending으로 남고 다른 credential로 새 POST하지 않는다.

### OPS-06 — scheduler 지연·재시작·DB clock

- 등급/환경: P1 / HARNESS
- 절차:
  1. scheduler를 한동안 중지해 due renewal과 auto-recharge를 만든다.
  2. 동일 TZ를 사용하는 두 테스트 노드의 application clock만 test hook으로 앞·뒤로 흔들고 DB clock은 유지한다.
  3. scheduler를 재개한다.
- 기대 결과:
  - DB time 기준으로 due selection이 일관된다.
  - 같은 logical cycle을 한 번만 charge한다.
  - subscription은 24시간 operational grace 밖에서 새 charge하지 않는다.
  - monthly recharge는 다음 달 1일 anchor를 유지한다.
  - 노드별 TZ가 다른 구성은 지원 대상으로 간주하지 않고 배포 검증에서 차단한다.

### OPS-07 — SQLite·MySQL·PostgreSQL migration smoke

- 등급/환경: P0 / HARNESS
- 각 DB별 절차:
  1. 현재 team 기준 schema와 최소 Toss legacy 데이터를 가진 disposable DB를 만든다.
  2. 모든 payment flag를 false로 저장하고 구버전 worker가 없음을 확인한다.
  3. `toss` binary로 migration master를 한 번 시작한다.
  4. schema와 recurring protocol state를 확인한다.
  5. PAY-01, SUB-01, AUTO-01의 축약 smoke와 두 노드 동시 settlement·renewal·wallet cycle 경쟁을 실행한다.
  6. 동일 binary를 재시작해 migration idempotency를 확인한다.
- 기대 결과:
  - 세 DB에서 migration이 성공하고 두 번째 시작도 무해하다.
  - nullable composite unique identity와 row lock 동작이 중복 charge를 막는다.
  - MySQL의 큰 provider payload가 잘리지 않는다.
  - SQLite는 작은 batch/worker 경로로 동작한다.
- 주의: 운영 DB DSN을 이 테스트에 사용하지 않는다.

### OPS-08 — recurring protocol drain guard

- 등급/환경: P0 / HARNESS
- 절차:
  1. secret 암호화 전환은 세 결제 flag를 끄고 모든 노드가 dual-reader와 같은 영구 key를 쓰는 것을 확인한 뒤에만 fleet 전체에 `TOSS_OPTION_SECRET_ENCRYPTION_ENABLED=true`를 적용한다.
  2. paymentKey 멱등 프로토콜은 checkout을 중지하고 legacy pending을 대사한 뒤 `TOSS_PAYMENT_KEY_SCOPED_IDEMPOTENCY_ENABLED=true`로 전환한다.
  3. recurring migration 전 DB의 `TossBillingEnabled`와 `TossWalletAutoRechargeEnabled`를 명시적 false로 저장한다.
  4. old master·scheduler·billing callback traffic을 완전히 중지하고 최소 5분 기다린다. 이 동안 MID·key·test mode·TZ를 바꾸지 않는다.
  5. sole migration master 한 곳에만 `TOSS_RECURRING_PROTOCOL_MIGRATION_DRAINED=true`를 일시 적용한다.
  6. 기존 Phase-A/v1 DB의 별도 전환은 같은 drain 뒤 sole master에만 `TOSS_RECURRING_ORDER_ID_V2_ACTIVATION_DRAINED=true`를 일시 적용한다.
  7. 성공 즉시 승인 env를 제거하고 모든 노드를 새 binary로 교체한 뒤 pending을 대사한다.
- 기대 결과:
  - 1번은 시작/activation을 거부한다.
  - drain 승인 없이 opaque recurring protocol로 넘어가지 않는다.
  - 성공 뒤 승인 env를 제거해도 protocol version은 안정적으로 유지된다.
  - 구버전 binary를 다시 시작하지 않아야 한다는 운영 경고가 명확하다.

### OPS-09 — master failover와 task ownership

- 등급/환경: P1 / HARNESS
- 사전 조건: 같은 DB를 보는 master A와 slave B, due test 작업과 Transaction source lease.
- 절차:
  1. A에서 billing·wallet·pending cleanup·Transaction task 시작을 확인하고 B에는 task가 없음을 확인한다.
  2. provider POST 전 claim 직후와 provider 성공·local settlement 전을 별도 run으로 나눠 A를 종료한다.
  3. B를 master로 재시작한다.
  4. lease 만료 전과 후의 동작을 관찰한다.
- 기대 결과:
  - slave는 돈을 움직이는 scheduler를 실행하지 않는다.
  - 새 master는 startup iteration을 실행하되 유효한 기존 lease를 빼앗지 않는다.
  - top-up claim 약 2분, billing·wallet claim 약 5분, Transaction source lease 약 25분의 경계를 존중하고 만료 뒤 exact logical attempt와 page cursor에서 재개한다.
  - provider charge·local 지급은 한 번이고 poison item 하나가 전체 ticker를 멈추지 않는다.

### OPS-10 — 결제 유형별 kill switch

- 등급/환경: P0 / STAGING
- 절차:
  1. 일반결제, subscription billing, wallet auto-recharge에 각각 pristine checkout과 attempted/pending 복구 건을 하나씩 준비한다.
  2. `TossEnabled`, `TossBillingEnabled`, `TossWalletAutoRechargeEnabled`를 하나씩 독립적으로 끈다.
  3. 새 checkout·새 POST와 기존 GET·cleanup·revocation·reconciliation을 구분해 관찰한다.
  4. payment compliance 확인도 해제해 세 유형의 stale node에서 같은 경계를 확인한다.
- 기대 결과:
  - 해당 유형의 신규 money-moving POST는 즉시 차단된다.
  - 다른 유형의 flag 의미가 섞이지 않는다.
  - 이미 돈이 이동했을 수 있는 exact attempt의 GET·동일 멱등 cleanup, billing-key DELETE, Transaction 대사는 계속된다.
  - 완전한 provider 통신 차단이 필요한 보안 사고는 애플리케이션 flag가 아니라 별도 egress ACL이 필요하다.

### OPS-11 — 동일 MID·다른 MID key rotation

- 등급/환경: P0 / HARNESS
- 절차:
  1. old test key로 일반 confirm, subscription charge, wallet charge의 ambiguous attempted row를 각각 만든다.
  2. 같은 MID의 새 key generation으로 atomic rotation한다.
  3. old attempt 복구와 새 checkout을 병렬로 실행한다.
  4. 가능하면 별도 test MID로도 rotation해 cross-MID 음성 테스트를 수행한다.
- 기대 결과:
  - old attempt는 저장된 exact credential로 먼저 GET한다.
  - definitive credential rejection이 있더라도 same-MID 증명이 된 fallback은 우선 GET-only이며 ambiguous POST를 새 key namespace로 반복하지 않는다.
  - 새 checkout만 새 generation을 사용한다.
  - 다른 MID로 old order를 조회·POST하거나 cursor source를 합치지 않는다.

## 13. 보안·관찰 가능성

### SEC-01 — secret·개인정보 redaction

- 등급/환경: P0 / TEST
- 절차:
  1. 성공·실패·timeout·repair·BILLING_DELETED를 한 번씩 발생시킨다.
  2. 애플리케이션 로그, GORM 로그, HTTP access log, public·일반 사용자 API 응답, 브라우저 console을 검색한다.
- 기대 결과:
  - `test_sk_`, `live_sk_`, billingKey, authKey, Basic Authorization, 전체 paymentKey가 없다.
  - 카드번호는 공식 masked 형태 외에는 없다.
  - URL query의 민감 결제 식별자가 access log에서 redaction된다.
  - provider payload는 일반 사용자 API로 노출되지 않는다.
  - 관리자 reconciliation 상세는 업무상 paymentKey와 provider evidence를 제공할 수 있으므로 권한·감사·화면 복사 제한을 검증하고 증거 자료에는 마스킹한다.
- 증거: 발견한 secret 값을 복사하지 말고 “검색 결과 0건”과 검색 패턴 종류만 기록한다.

### SEC-02 — public option 응답

- 등급/환경: P0 / TEST
- 절차:
  1. 비로그인, 일반 사용자, admin으로 public/system option API를 확인한다.
  2. legacy client slot에 secret-like fixture가 들어간 disposable DB에서도 확인한다.
- 기대 결과:
  - 검증된 client key 이외 secret·repair digest·internal revision은 노출되지 않는다.
  - secret-like client value는 공개 응답에서 제거된다.
  - repair token은 필요한 admin에게만 제한적으로 보인다.

### SEC-03 — request body와 식별자 경계

- 등급/환경: P1 / HARNESS
- 절차:
  1. 일반 결제·billing·auto-recharge endpoint에 oversized JSON, duplicate JSON field, trailing JSON, 비정상 orderId/customerKey를 보낸다.
  2. 문자열에 control character와 매우 긴 값을 넣는다.
- 기대 결과:
  - 64KiB 일반 결제 body 제한과 trailing document 거부가 동작한다.
  - 현재 decoder는 unknown field를 무시하고 duplicate field의 마지막 값을 사용한다. 이 parser 동작을 기록하고 최종 decoded 값이 immutable server contract와 충돌하면 provider POST가 일어나지 않는지 확인한다.
  - duplicate·unknown field 자체를 금지해야 한다는 보안 요구가 있다면 별도 hardening issue로 등록한다.
  - 오류 로그에 원문 공격 payload가 남지 않는다.

## 14. 브라우저·테마 UX

### UI-01 — Default와 Classic 핵심 흐름 비교

- 등급/환경: P1 / TEST
- 절차:
  1. 두 테마에서 개인 일반결제, 구독, scheduled/threshold 자동충전을 실행한다.
  2. 조직 owner와 member로 같은 흐름을 확인한다.
- 기대 결과:
  - server contract와 최종 금액·대상은 동일하다.
  - Default의 KRW/quota 토글은 Toss-only 구성에서 제공된다. 혼합 gateway에서는 공용 preset UX가 유지되며 Classic은 기존 quota 중심 UX를 사용한다.
  - Classic `/wallet` 진입은 query를 보존해 `/console/topup`으로 연결된다.
  - 조직 owner endpoint와 개인 endpoint가 섞이지 않는다.

### UI-02 — cross-origin callback과 새로고침

- 등급/환경: P1 / STAGING
- 절차:
  1. 프론트와 API가 서로 다른 origin인 staging에서 결제·billing auth를 실행한다.
  2. callback 뒤 새로고침, 뒤로가기, 새 탭 복사를 수행한다.
- 기대 결과:
  - SDK는 필요한 경우 self target으로 redirect해 iframe CORS 실패를 피한다.
  - callback origin·path 검증이 허용한 endpoint만 사용한다.
  - 결과 query는 한 번 표시된 뒤 제거되고 재실행되지 않는다.

### UI-03 — 모바일·팝업·StrictMode

- 등급/환경: P1 / TEST
- 절차:
  1. iOS Safari, Android Chrome에서 결제창 열기·취소·복귀를 수행한다.
  2. 팝업 차단 상태와 느린 네트워크를 각각 시험한다.
  3. 개발 모드 React StrictMode에서도 한 번 시험한다.
- 기대 결과:
  - 중복 window/iframe이 생기지 않는다.
  - 취소 뒤 다음 결제가 가능하다.
  - unmount 뒤 늦게 resolve된 SDK instance도 정리된다.
  - 사용자에게 모호한 무한 spinner가 남지 않는다.

## 15. 제한된 라이브 확인

아래는 TEST·STAGING P0가 모두 통과하고 재무·운영·개인정보 책임자가 승인한 경우에만 실행한다.

### LIVE-01 — 라이브 소액 카드 승인과 즉시 전액 취소

- 등급/환경: P1, 병합 필수 gate 아님 / LIVE-APPROVAL
- 사전 조건:
  - 승인된 최소 금액·카드·담당자·실행 시간·취소 책임자를 티켓에 기록한다.
  - test/live MID API version과 webhook 설정을 각각 확인한다.
  - monitoring과 kill switch 담당자가 대기한다.
- 절차:
  1. 승인된 계정으로 한 건만 결제한다.
  2. 로컬 지급과 상점관리자 거래를 대조한다.
  3. 즉시 전액 취소한다.
  4. 취소 webhook과 reconciliation event를 확인한다.
  5. 테스트 계정의 API 사용을 중지하고 이미 지급된 local quota를 승인된 운영 절차로 보정한 뒤 reconciliation event를 해결한다.
- 중단 조건:
  - 금액·MID·대상이 다름
  - 중복 거래
  - 10초 이상 webhook 지연 증가
  - secret 또는 개인정보 노출
- 정리: 카드사 환불, local quota 보정, reconciliation event 종료까지 담당자가 추적한다.

### LIVE-02 — 라이브 billing 계약 확인

- 등급/환경: P1 / LIVE-APPROVAL
- 조건: 실제 자동결제 계약·상품 정책·고객 동의·해지 정책이 법무·운영 검토를 통과해야 한다.
- 확인 항목:
  - billing 전용 MID와 key pair
  - 최초 결제 고지 금액과 실제 승인 금액
  - 자동갱신 동의·해지 UI
  - billing key 삭제와 BILLING_DELETED 처리
  - 실패 고객 알림·재시도 정책
- 금지: 일반 테스트를 위해 실제 고객 카드나 운영 고객 계정을 사용하지 않는다.

## 16. 종료 조건과 결과 판정

병합 가능 판정은 다음을 모두 만족해야 한다.

- [ ] 최소 수동 스모크 세트의 P0가 모두 PASS다.
- [ ] provider 결제 수와 local 성공 주문 수가 설명 가능한 상태로 일치한다.
- [ ] 한 provider 결제로 quota·구독 기간이 두 번 증가한 사례가 없다.
- [ ] 결제됐지만 지급되지 않은 건은 refund/reconciliation event와 담당자가 지정되어 있다.
- [ ] required reconciliation event, refund fence, pending revocation, stale pending의 종료 전 수를 기록했다.
- [ ] 테스트로 만든 subscription·auto-recharge policy·billing key를 정리했다.
- [ ] 테스트 결제의 취소 여부를 Toss 개발자센터에서 확인했다.
- [ ] 모든 임시 CIDR, fault injection, proxy rule, test scheduler override를 제거했다.
- [ ] secret·authKey·billingKey·전체 카드정보가 증거 자료에 없다.
- [ ] 실제 MySQL/PostgreSQL, 다중 노드, webhook을 실행하지 못했다면 BLOCKED가 아니라 “미실행 위험”으로 명시해 merge 승인자가 판단할 수 있게 했다.

다음 중 하나라도 발견되면 즉시 P0 FAIL로 판정하고 병합을 중단한다.

- 중복 provider charge 또는 중복 quota·구독 지급
- provider amount와 immutable local quote 불일치
- 취소·refund fence 이후 지급
- 비owner의 조직 결제 또는 다른 사용자·조직으로 정산
- 설정 disable·maintenance·repair 중 신규 provider POST
- secret, billingKey, authKey, 전체 카드번호 노출
- 결과가 불명확한 요청을 다른 credential·order namespace로 새 POST

## 17. 테스트 결과 요약 템플릿

~~~text
Run ID:
Branch / commit:
Tester:
Environment:
Toss MID aliases:
Database:
Browser/device:

P0 PASS / FAIL / BLOCKED:
P1 PASS / FAIL / NOT RUN:
P2 PASS / FAIL / NOT RUN:

Provider transactions created:
Provider transactions canceled:
Local successful TopUps:
Local successful SubscriptionOrders:
Wallet auto-recharge charges:
Required reconciliation events before / after:
Pending billing-key revocations before / after:

Unexpected behavior:
Open incident or issue links:
Temporary configuration cleanup complete: YES / NO
Merge recommendation: GO / NO-GO / CONDITIONAL
Approver:
~~~
