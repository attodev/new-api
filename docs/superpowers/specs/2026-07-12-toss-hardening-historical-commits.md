# Toss Payments 기존 실제 커밋 이력 부록

- 작성일: 2026-07-12
- 통합 기준 HEAD: 4171f875b
- 반영 브랜치: toss
- 목적: 기존 Toss baseline 140개와 2026-07-12 공식문서 감사 통합 커밋을 분리

## 1. 범위와 결론

Toss 초기 도입과 wallet auto-recharge는 다음 140개 실제 이력으로 통합 기준 HEAD에 들어와 있다. 공식 문서 재감사 하드닝은 그 위에 `toss` 브랜치의 별도 통합 커밋으로 반영했다.

| 구간 | 실제 커밋 수 | 내용 |
|---|---:|---|
| 2026-06-29–06-30 | 49 | 일반 결제, 연속 hardening, subscription billing, 통합 hardening |
| 2026-06-30–07-02 | 89 | wallet auto-recharge, preset, wallet layout, quote·quota mode |
| 2026-07-03, 07-06 | 2 | 기능 branch 병합, organization wallet 정렬 |
| 합계 | 140 | 설계·계획·test·chore 포함 |

따라서 다음 세 표현을 구분해야 한다.

- **실제 과거 커밋**: 아래 140개, hash 존재
- **현재 하드닝 통합 커밋**: `474e12e70`, `1e596af12`, `34a33c78f`와 문서 커밋
- **C01–C18**: 실제 hash와 일대일 대응하지 않는 세부 리뷰·추적 단위

## 2. 병합 구조

~~~text
3677a3524
  └─ 8e006206d ... 일반결제·구독 ...
       └─ 3191eab01
          ├─ a3f805f9a  Toss 통합 hardening
          │   └─ f9c064ea0
          └─ 71cde7304 ... wallet auto-recharge ...
              └─ 52fff1c0f

f9c064ea0 + 52fff1c0f
  └─ 1ac852781  wallet branch를 toss에 병합
      └─ 5e24138a7  organization wallet 정렬

ee24f2ca6 + 5e24138a7
  └─ f75ba651e  team 계열에 Toss 기능 반입
      └─ 791d998e2  translation branch와 team 병합
          └─ ... 현재 HEAD 4171f875b
~~~

핵심 merge인 1ac852781은 단순 merge metadata가 아니다. 다음 파일에 충돌 해결 결과가 포함됐다.

- controller/topup_toss.go
- controller/topup_toss_test.go
- model/topup.go
- model/toss_billing.go
- model/toss_billing_test.go
- web/default/src/features/organizations/components/organization-wallet.tsx
- web/default/src/features/wallet/types.ts
- Default i18n report·en·kr

## 3. 2026-06-29–06-30 일반결제·billing — 49개

### 3.1 설계와 backend 기반

| Commit | 문제 | 수정 |
|---|---|---|
| **8e006206d** | Toss 연동의 server approval·webhook·organization 범위 기준이 없었음 | 최초 영문 integration design 작성 |
| **c15200d84** | 팀 운영 언어와 설계 문서 언어가 달랐음 | 설계 문서를 한국어로 변환 |
| **b639b61b0** | Toss key, mode, unit price를 저장·선택할 방법이 없었음 | normal/test key, enable, unit price, minimum option 추가 |
| **dfef081a1** | Toss provider 식별과 local recharge 함수가 없었음 | payment 상수와 row-lock 기반 RechargeToss 추가 |
| **e89fc7b54** | Toss Amount는 KRW인데 manual complete가 내부 단위처럼 지급할 수 있었음 | Toss를 Money 기반 지급 경로로 분기 |
| **a34a0c119** | pending order, quote, confirm API가 없었음 | pay·amount·confirm·fail·webhook의 최초 backend 구현 |
| **a543c9c32** | 구현을 노출할 route와 topup capability가 없었음 | enable helper, topup info, 개인·조직·callback route 추가 |
| **9265f24f6** | unsigned webhook body를 그대로 신뢰하고 organization amount route가 누락됨 | paymentKey GET 재검증과 organization amount endpoint 추가 |

### 3.2 Default 프론트엔드 최초 연동

| Commit | 문제 | 수정 |
|---|---|---|
| **36907c461** | Toss provider를 표현하는 타입·상수가 없었음 | wallet Toss 상수와 response 타입 추가 |
| **acdb7385b** | SDK와 server API adapter가 없었음 | Toss SDK dependency 및 pay·amount API 함수 추가 |
| **067bc0999** | provider 식별·기본값·minimum 분기가 없었음 | isTossPayment와 provider-specific helper 추가 |
| **8fa59a806** | SDK 결제창을 여는 client flow가 없었음 | loadTossPayments와 requestPayment hook 추가 |
| **e06b700e6** | wallet 공통 결제 흐름에 Toss가 연결되지 않음 | amount preview·confirm·payment branch 연결 |
| **76a788a71** | anonymous customerKey, cancel error toast, 공통 provider 중복 실행 | server customerKey 사용, user cancel 분리, duplicate path 제거 |
| **0f16227bd** | 관리자가 Toss key·mode·price를 설정할 UI가 없었음 | payment settings Toss section 추가 |
| **49631c082** | frontend 기본값·i18n source key가 backend와 불일치 | 기본 단가·minimum과 영어 source key 정렬 |
| **b7dfe1e61** | Toss만 AmountDiscount를 적용하지 않음 | group ratio와 amount discount를 quote에 함께 반영 |

### 3.3 일반결제 연속 hardening

| Commit | 문제 | 수정 |
|---|---|---|
| **a9f005583** | 예측 가능한 customerKey, 약한 confirm validation, paymentKey 추적 부족 | random stable customerKey, order·amount·currency·paymentKey 검증 강화 |
| **c0b0c1b67** | paymentKey column truncation, customerKey JSON 노출, pending 고아 | column 255, JSON 제외, customerKey 선확보 |
| **0c0b03dd6** | 잘못된 callback base, 무한 대기, 중복 confirm | ServerAddress 검증, timeout, Idempotency-Key, paymentKey early store |
| **d3eb62291** | paymentKey를 confirm 뒤 저장할 수 있었음 | confirm POST 전에 기록하고 availability에 ServerAddress 추가 |
| **fddd68db0** | EXPIRED·ABORTED order가 pending으로 남고 webhook SLA 초과 | terminal status close, timeout 8초 |
| **ce1c492b2** | 지급 후 cancellation을 일반 실패로 오해 | post-credit cancel을 manual reconciliation으로 분리, callback URL normalize |
| **8e820f7e5** | pending partial cancel과 abandoned checkout 처리 없음 | partial-cancel reconciliation 및 stale pending sweep 추가 |
| **1898912ac** | sweep가 실제 승인된 pending도 만료할 수 있음 | paymentKey 미기록 주문만 50분 뒤 정리 |
| **97e033302** | paymentKey 저장 실패를 무시하고 confirm할 수 있었음 | 저장 성공을 필수 precondition으로 변경 |
| **af8085436** | webhook DONE 지급 전 paymentKey persistence가 빠짐 | webhook도 paymentKey 선저장 후 지급 |
| **5ab0ecfb1** | webhook currency 미검증, rejected confirm pending 잔존 | KRW 검증과 confirm rejection close, webhook 안내 |
| **1650a3159** | 모든 4xx를 terminal로 보면 409 processing도 실패 처리 | confirm failure 뒤 Payment GET으로 DONE·terminal·pending 재분류 |
| **f71b79546** | forged key의 404·order mismatch가 무한 pending | 404·identity mismatch close, transient만 pending |
| **5f50b6a62** | COOP가 SDK popup·redirect를 방해 | SPA에 same-origin-allow-popups 적용 |

### 3.4 문서·UI 후속

| Commit | 문제 | 수정 |
|---|---|---|
| **ec4d72c62** | 구현·hardening 근거가 흩어짐 | 당시 이슈·수정 상세 문서 작성 |
| **af485ada7** | Toss만 enabled이면 online top-up UI가 숨겨짐 | enable_toss_topup을 UI gate에 포함 |
| **4700f200b** | 운영 log 메시지가 팀 언어와 불일치 | Toss 성공 로그 한국어화 |

### 3.5 subscription billing

| Commit | 문제 | 수정 |
|---|---|---|
| **c71df532f** | billingKey secret-at-rest 암호화 helper가 없었음 | AES-256-GCM EncryptString·DecryptString 추가 |
| **880d6f94e** | billing key와 auto-renew 상태 model이 없었음 | UserBillingKey, next billing, fail count, key reference 추가 |
| **e48b9b94d** | ISSUE·billing charge client와 guard가 없었음 | billing API client, KRW conversion, charger injection 추가 |
| **e3c6f2828** | subscription complete·renew·fail transition이 없었음 | transaction 기반 lifecycle 함수 추가 |
| **92673fc85** | billing callback·pay·cancel route가 없었음 | public callback과 authenticated API 연결 |
| **70d25aa8f** | due subscription을 자동 charge하지 않음 | master-only 1분 recurring task 추가 |
| **4328ffaee** | dangling order, lost success, duplicate renewal, cancel race | deterministic renewal tradeNo, reconciliation, final auto-renew check |
| **8aa7f9d4a** | browser billing auth flow가 없었음 | requestBillingAuth API·hook 추가 |
| **d65e6f401** | subscription UI에 Toss 선택·cancel이 없었음 | Toss subscribe button, auto-renew cancel, capability 노출 |
| **131586690** | expiry task와 renewal race, callback customerKey 위조 | expiry 전 renewal scheduling, canonical customerKey 비교 |
| **f66d16a0c** | frontend success envelope 판정이 server와 불일치 | message success도 성공으로 인정 |

### 3.6 wallet 설계와 통합 hardening

| Commit | 문제 | 수정 |
|---|---|---|
| **49a06183d** | wallet auto-recharge domain 기준이 없었음 | 영문 design 작성 |
| **7dc60960f** | design 언어 접근성 | 한국어 번역 |
| **3191eab01** | 구현 순서·test 기준 부재 | implementation plan 작성 |
| **a3f805f9a** | 일반·billing 전반에 retry, credential snapshot, key lifecycle, cleanup 경계가 부족 | 46파일 약 +7,152/-395 규모 통합 hardening |

### 3.7 a3f805f9a 상세

이 커밋은 다음을 한 번에 도입했다.

- 공통 Toss HTTP retry와 408·409·429·5xx transient 분류
- billing charge 70초 timeout
- 일반 Payment의 DONE·orderId·paymentKey·amount·KRW·card 검증
- ProviderOrderTime과 encrypted ProviderCredential snapshot
- paymentKey가 있는 pending order의 조회 reconciliation
- normal key와 billing key 분리, TossBillingEnabled 별도 opt-in
- lost billing charge response의 orderId lookup
- billing key hash와 pending_revocation lifecycle
- BILLING_DELETED local encrypted key 비교
- deterministic renewal trade number와 frozen order amount
- paymentKey 미기록·기록·subscription pending의 분리 cleanup
- organization·frontend·admin settings 보강
- 대규모 controller·model·service·frontend 테스트와 445줄 변경 문서

당시에도 남았고 현재 C01–C18에서 보완한 경계:

- API-key scoped idempotency와 exact credential pin
- webhook source IP·trusted proxy
- 14-option atomic encrypted configuration
- provider POST 직전 transaction barrier
- durable reconciliation event와 refund operation
- Transaction API scanner
- immutable plan·target lifecycle
- rolling protocol fence

## 4. 2026-06-30–07-02 wallet auto-recharge — 89개

아래 표는 실제 first-parent 순서이며 모든 commit을 포함한다.

### 4.1 core model, worker, API, UI

| Commit | 문제 | 수정 |
|---|---|---|
| **71cde7304** | scheduled·threshold policy model 부재 | WalletAutoRecharge model, cooldown, daily limit, billing link |
| **f103c6951** | check-then-insert race, normalize 미반영, key 조기 revoke | ActiveKey unique, 저장값 normalize, shared key 보존 |
| **deba725fe** | due policy executor 부재 | master-only wallet worker |
| **c9dbfb662** | 개인·조직 create·cancel·callback API 부재 | controller와 route 추가 |
| **b33c819e0** | organization 권한 조건 오류, duplicate callback | owner condition, order lock, failed pending cleanup |
| **5f1e98b32** | frontend primitive 부재 | API, card, hook, type 추가 |
| **8c05105e5** | disabled·selection·accessibility review 문제 | UI state와 tests 보강 |
| **c9c979c56** | 명시적 0이 fallback으로 바뀜 | zero-preserving hydration |
| **6a72d5664** | wallet auto-recharge tab 부재 | personal·organization scheduled·threshold tab |
| **01dc730b6** | async loading·tab·active policy review 문제 | task review 수정 |
| **b72738ea9** | locale file이 i18next에 등록되지 않음 | wallet locale resource 등록 |
| **55ce0caad** | 지원 언어 기대값이 변경과 불일치 | language tests 수정 |
| **a03b637f1** | Amount가 KRW인지 unit인지 혼용 | wallet amount를 KRW로 정렬, Money·quota 분리, pending cancel |

### 4.2 preset model과 admin

| Commit | 문제 | 수정 |
|---|---|---|
| **c37b14fcc** | preset domain 설계 부재 | preset design |
| **c9f516376** | 구현 계획 부재 | preset plan |
| **6f9b6ef49** | preset persistence·filter 부재 | preset CRUD·validation model |
| **b2a58eaa3** | generated report가 tracked됨 | SDD report untrack |
| **efb34abdd** | 없는 preset disable이 성공처럼 보임 | RowsAffected 0을 not found |
| **be15445a0** | generated task report tracked | report untrack |
| **5ff573b9e** | client가 amount·interval을 임의 제출 | preset_id만 받고 server snapshot |
| **bc01de8c7** | admin route가 user group 아래 있음 | /api/admin으로 이동 |
| **1c3062bd8** | wallet UI preset 선택 부재 | preset fetch·selection |
| **f9770a2e6** | generated report tracked | report untrack |
| **008926722** | preset load 전 tab flicker | loaded 전 tab 유지 |
| **e424a38a9** | generated fix report tracked | report untrack |
| **0af1039a6** | active policy load 전 tab 숨김 | preset·policy 둘 다 loaded 요구 |
| **59bb2bbee** | generated second report tracked | report untrack |
| **8008c99e8** | preset admin UI 부재 | settings CRUD section |
| **0c6b4e39e** | parse·sort·dedupe helper 불안정 | normalized admin helpers |
| **ae43fe514** | generated task report tracked | report untrack |
| **48956308e** | interval summary가 locale 무시 | translation-key formatter |
| **c97d89e9e** | generated i18n report tracked | report untrack |
| **6a75189ee** | preset locale 누락 | 7 locale 보완 |

### 4.3 provider/local reconciliation

| Commit | 문제 | 수정 |
|---|---|---|
| **d17216619** | provider POST를 DB transaction 안에서 실행 | pending TopUp 선생성, transaction 밖 charge, 별도 settlement |
| **d5f13f541** | charge 성공·local 실패 뒤 새 결제 가능 | charged pending 우선 복구 |
| **cb8629464** | charged marker write 실패를 미청구로 오인 | last_trade_no·reconciliation error로 known-charge 복구 |
| **e377a8ef1** | i18n report stale | sync report refresh |

### 4.4 option-style UI와 문서

| Commit | 문제 | 수정 |
|---|---|---|
| **77d39f076** | option UI 설계 부재 | design |
| **a5476e6aa** | implementation plan 부재 | plan |
| **88599fb45** | raw preset을 option으로 조합할 helper 부재 | period·threshold·amount grouping helper |
| **3a06d82d0** | raw preset selection UX 복잡 | 단계형 selection |
| **5f2cd66a5** | admin이 row만 편집 | option combination editor |
| **064ab89d5** | 새 option 문구 locale 누락 | translation update |
| **99118bf26** | comma·empty draft가 즉시 normalize | raw draft와 parsed state 분리 |
| **36c008656** | 사용자·관리자 운영 문서 부재 | manuals, images, docs E2E, integrated design |

### 4.5 wallet payment settings layout

| Commit | 문제 | 수정 |
|---|---|---|
| **f1f299be3** | payment setting layout 기준 부재 | design |
| **2665b1495** | 구현 순서 부재 | plan |
| **ed82d7dbf** | subscription·scheduled·threshold 상호배타 helper 부재 | common payment setting helper |
| **0afac83d1** | 충돌 설정 중 auto-recharge 생성 가능 | UI create lock |
| **0f1217081** | 현재 subscription 상태를 wallet에서 확인 불가 | status card |
| **2fbe17142** | 결제 설정 UI 분산 | unified tab layout |
| **4927e2dc2** | subscription load 전 auto-recharge unlock | loaded 전 lock |
| **5b5bb8a21** | disabled tab이 initial selection | enabled tab 우선 |
| **926be3814** | disabled 이유 미표시 | lock reason 표시 |
| **66d544673** | 조건 확인 전에 billing auth 창 열림 | confirmation 먼저 |

### 4.6 quota threshold와 monthly schedule

| Commit | 문제 | 수정 |
|---|---|---|
| **1ceedc801** | quota threshold 설계 부재 | design |
| **6bf5624fc** | 구현 계획 부재 | plan |
| **6c6f7ffb4** | 단가 변경이 threshold를 흔듦 | ThresholdQuota snapshot |
| **0e2c92660** | frontend grouping이 amount 기반 | quota 기반 helper |
| **4e3a8b674** | admin quota threshold 편집 부재 | settings UI |
| **f4057215f** | threshold와 charge amount 선택 순서 불명확 | 기준잔액 먼저 선택 |
| **3aa1bd11f** | legacy zero quota preset 노출 | server·client에서 제외 |
| **738d961b3** | balance editing 설계 부재 | design·admin note |
| **28a3be7d2** | raw quota 편집 UX | balance 단위 편집·quota 변환 |
| **e4b0fe968** | threshold 설명 중복 | duplicate copy 제거 |
| **77628e87a** | monthly schedule이 생성일 기준 drift | 다음 달 1일 anchor, immediate off |
| **065a7611a** | monthly 제한이 test custom preset도 숨김 | single custom test period 허용 |
| **d07282db6** | scheduled amount 단위 미표시 | KRW unit 표시 |

### 4.7 KRW·quota top-up mode와 immutable quote

| Commit | 문제 | 수정 |
|---|---|---|
| **386935108** | input mode 설계 부재 | design |
| **8be152761** | implementation plan 부재 | plan |
| **a80168385** | plan이 project i18n rule과 불일치 | i18n rule 정렬 |
| **2a0be799e** | KRW 입력과 credit 입력을 server가 통일 계산 못함 | TossTopUpQuote와 krw·quota mode |
| **db4387484** | invalid UnitPrice를 잘못 fallback | nonpositive unit price fail-closed |
| **1504530a5** | float precision으로 quota 1단위 오차 | decimal direct division tests |
| **4e44d42fc** | generated report tracked | report untrack |
| **08e585087** | amount API가 scalar만 반환 | structured quote와 session fields |
| **0978d39b9** | quota 입력값을 KRW minimum과 직접 비교 | 계산된 ChargeKRW로 검증 |
| **3cff71cf4** | generated report tracked | report untrack |
| **7da4f31ce** | mode·quote frontend helper 부재 | parsing·preview helpers |
| **22b30b688** | generated report tracked | report untrack |
| **8d798cc01** | locale-dependent won format 불안정 | stable KRW format |
| **69ffca850** | personal wallet mode UI 부재 | KRW·credit selector와 quote |
| **aceed9258** | generated report tracked | report untrack |
| **3f293f14b** | Toss UI가 other provider preset 제거 | mixed-provider preset 유지 |
| **e9e27cd99** | organization wallet mode 부재 | organization mode·quote |
| **50d083248** | Korean mode locale 누락 | kr translation |
| **2f810ab32** | confirmation이 local preview를 표시 | server quote 우선 |
| **599a407b0** | settlement가 현재 단가로 quota 재계산 | TopUp.Quota immutable snapshot |
| **52fff1c0f** | quote 실패에도 confirmation 가능 | positive server quote 필수 |

## 5. merge·organization 후속 — 2개

| Commit | 문제 | 수정 |
|---|---|---|
| **1ac852781** | a3f hardening branch와 89개 wallet branch가 분리 | 충돌을 해결해 toss branch로 통합 |
| **5e24138a7** | organization wallet layout·lock semantics가 personal과 불일치 | top-up form과 payment settings 배치·helper·owner lock 정렬 |

Team 반입 trace:

| Commit | 역할 |
|---|---|
| **f75ba651e** | 5e24138a7을 team 계열에 실질 반입한 bridge merge |
| **791d998e2** | 위 결과와 translation branch 통합 |
| **2beca7094** | 초기 Toss ancestor 067bc0999의 별도 translation 경로; 전체 기능 merge로 중복 집계 금지 |

## 6. 현재 logical commit으로 이어진 보완 관계

| 기존 실제 커밋 | 당시 해결 | 현재 논리 커밋에서 추가 강화 |
|---|---|---|
| 0c0b03dd6, 1650a3159, f71b79546 | orderId idempotency, confirm 실패 GET | C03·C05 exact secret, paymentKey scope, unknown outcome fail-closed |
| 97e033302, af8085436 | paymentKey 선저장 | C05 generic durable attempt와 final barrier |
| a3f805f9a | provider credential snapshot | C03 MID fingerprint, credential promotion, bounded backfill |
| c71df532f, a3f805f9a | billing key 암호화 | C02 option secret enc:v1·AAD·persistent key |
| 599a407b0 | Quota snapshot | C04 invalid nonzero snapshot fail-closed와 legacy Money 우선 |
| ce1c492b2, 8e820f7e5 | 취소 manual log | C07 durable event, refund fence, balance idempotency |
| d17216619–cb8629464 | wallet provider/local 분리 | C03·C14 exact attempt, target lifecycle, rotation |
| a3f805f9a | pending cleanup | C09 Transaction API scanner |
| 0afac83d1–4927e2dc2 | frontend exclusivity lock | C02·C10·C14 server-side barrier |
| 9265f24f6, a3f805f9a | webhook Payment GET | C08 source IP·trusted proxy·deadline |

## 7. 재현 명령

첫 49개:

~~~text
git log --reverse --first-parent +  3677a352419eb0c10ae610d27b1d46dccaca35ec..a3f805f9a7659bf6153a357ee9e205b29898cc1d
~~~

wallet 89개:

~~~text
git log --reverse --first-parent +  3191eab01202f212466c5d06e736e28109cf9457..52fff1c0fddbd576cef9d1c5ae4b95108e2dcafc
~~~

merge 확인:

~~~text
git show --summary 1ac852781
git show --summary 5e24138a7
git show --summary f75ba651e
git show --summary 791d998e2
~~~

## 8. 해석 시 주의

- 설계·계획·report untrack commit도 140개 총계에 포함한다.
- merge commit을 부모 branch commit과 별도 기능 구현으로 중복 계산하지 않는다.
- a3f805f9a는 46개 파일의 통합 hardening이라 하나의 작은 주제로 설명하면 안 된다.
- C01–C18에는 단일 hash를 붙이지 않는다. 대형 shared file 의존성 때문에 실제 반영은 세 개의 코드 통합 커밋과 문서 커밋을 사용했다.
- C01–C15와 backend C18은 `474e12e70`, C16과 Default locale은 `1e596af12`, C17과 Classic locale은 `34a33c78f`에서 찾는다.
