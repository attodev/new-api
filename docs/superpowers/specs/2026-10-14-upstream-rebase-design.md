# upstream 재기반(reverse rebase) 설계

작성: 2026-10-14. 상태: 검토 대기.

## 1. 목적

team 브랜치는 upstream(QuantumNous/new-api) `74985fa87`(2026-05-26)에서 갈라진 뒤 628개의 fork 고유 커밋을 쌓았고, upstream은 같은 기간 705개 커밋으로 변환 계층(`relaykit`), 프런트엔드(`web/src`), 인증, 과금을 재편했다. 2026-10 동기화에서 확인한 대로 지금 구조에서는 upstream 수정을 매번 수동으로 번역해야 하고, 그 비용은 커지고 있다.

이 설계는 방향을 뒤집는다. **upstream 최신(`qn/main` `7de10f2c6`, 2026-10-14; 설계 작성 시점에는 `63cecab82`였고 이후 커밋 3개가 더 들어왔다)을 베이스로 새 브랜치를 만들고, 우리가 만든 기능을 그 위에 다시 얹는다.** Toss 결제 계열은 가져가지 않는다.

성공 기준:
1. 새 브랜치가 upstream 전체 테스트와 우리 기능 테스트를 모두 통과한다.
2. 아래 "유지 기능" 목록이 모두 동작하고, 조직·지갑 매뉴얼(`docs/manuals/*.md`)의 시나리오로 패리티를 확인한다.
3. 이후 upstream 동기화가 `git merge qn/main` + 소수 파일 충돌 해결로 끝난다. 그것을 위해 우리 코드는 upstream 파일을 "훅 한 줄 + 별도 파일" 형태로만 건드린다(§3 규칙).
4. 기존 운영 DB(MySQL/PostgreSQL, 이미 bigint quota)에서 데이터 손실 없이 기동한다(§5 리허설 통과).

## 2. 범위

### 2.1 유지하는 fork 기능 (overlay)

| ID | 기능 | 출처(현 team) | 규모(backend 비테스트 LOC) |
|---|---|---|---|
| O1 | 랜딩·가이드 정적 페이지, 서버 템플릿, SEO/사이트맵/GTM/GA4, 챗봇 위젯 | `web/default/templates`, `web/default/public`(자산 63), `router/web-router.go`(+220), `main.go` embed | 4,577 + 자산 |
| O2 | PayPal 충전(웹훅 기반), 설정, 조직 지갑 PayPal | `controller/topup_paypal.go`, `setting/payment_paypal.go`, 옵션/라우트, 프런트 wallet·settings 조각 | 861 |
| O3 | 조직 관리: 조직/멤버/역할, 조직 지갑과 과금 귀속, 조직 구독 플랜, 조직 대시보드·로그, 조직 지갑 결제(Stripe/Creem/Epay/Waffo/PayPal) | `model/organization*.go`, `controller/organization*.go`, `service/funding_source.go` 확장, `service/billing_session.go` 훅, 라우트 60여 개, 프런트 `features/organizations`(16) + 라우트 7 | 2,944 + 훅 |
| O4 | 사용자/조직 멤버 Excel 내보내기·가져오기 | `service/user_export.go`, `service/org_user_export.go`, excelize | 614 |
| O5 | 문의 메일(`POST /api/contact`), 관리자 테스트 메일, ContactEmail 옵션 | `controller/contact.go`, `controller/email.go`, `common/email.go` 일부 | 323 |
| O6 | 모델/벤더 할인 계층(percent → multiplier, model > vendor), 가격 API·로그·설정 UI 노출 | `setting/ratio_setting/model_discount.go`, `relay/helper/price.go` 훅, `model/pricing.go`, usage-log 표시 | 102 + 훅 |
| O7 | 요청 타이밍 분해(e2e/llm/gateway ms)와 로그 표시 | `middleware/request_timing.go`, `common/timing_response_writer.go`, `relay/common/relay_info.go` 필드, `service/log_info_generate.go` | 117 + 훅 |
| O8 | 소형 설정: OIDC 이메일 도메인 화이트리스트(가입+로그인), OpenRouter 로그 vendor 이름 제거 옵션, 인터페이스 언어 en/ko 제한, 기본 첫 화면 콘솔, 가격 페이지 모델 상세 숨김, 정적 자산 rate limit 제외·캐시 헤더 (단계 A 결과: 인터페이스 언어 제한은 단계 D로, 기본 첫 화면 콘솔은 team에서 되돌려져 제외, 64-bit 옵션 파싱은 no-op) | 각 수십 줄 | ~300 |
| O9 | 한글화: 프런트 `ko` locale(4,795 키 중 4,489 재사용 + upstream 신규 2,778 키 번역), 백엔드 `ko.yaml`(upstream 키 67개), `README.kr.md` (단계 D 결과: ko.json 7,538키 — team 재사용 4,412, 번역 2,943, 영어 유지 183; `ko.yaml` 66키; §4.8) | `web/default/src/i18n/locales/kr.json` | — |
| O10 | unode 진단 도구 | `tools/unode-diagnostics/` | 3,424(독립) |
| O11 | fork 고유 수정 중 upstream에 없는 것: Epay 콜백 트랜잭션 정산 + "fail" 응답 재시도, PayPal·조직 지갑 충전 전 수용량 검증, Gemini 프롬프트 IMAGE/VIDEO/DOCUMENT 귀속과 캐시·오디오 캡, Ali `thinking_budget` either-name 규칙 | 2026-10 동기화 기록 두 건 | 소 |
| O12 | OpenWebUI 연동(OAuth2/OIDC 제공자, relay JWT 치환, 사용자 링크 관리) | `OpenWebUI` 브랜치(미병합, Go 34파일 9,457 LOC + 프런트 28) | 9,457 |

### 2.2 가져가지 않는 것

- **Toss 결제 전부**: 일반 충전, 정기결제·빌링키(`UserBillingKey`), webhook·reconciliation, 지갑 자동충전(임계값/정기, 프리셋)과 그 cron, 조직 지갑의 Toss 라우트, `web/classic`의 Toss 패리티. backend 23파일 27,087 LOC, 프런트 21파일.
- **`web/classic` 테마**: upstream이 삭제했다. classic 사용자는 없다고 확인되었으므로(§9 결정 1) 함께 버린다.
- **우리 quota 상한 구현의 코드 형태**(`common.MaxQuota`=5e14 상수, 자체 saturating 변환): upstream이 같은 문제를 64-bit 지갑 도메인으로 풀었으므로(`MaxWalletQuota`=2^53-1, 기본 단가 기준 약 180억 달러; 기동 시 `ensureUserQuotaColumns`가 bigint 컬럼을 요구) 그 구현을 쓴다. **큰 잔액을 저장·충전·표시하는 능력은 유지되고 상한은 오히려 올라간다.** 우리 DB 컬럼은 이미 bigint라 그대로 통과한다. 단일 요청 과금의 int32 포화는 양쪽 모두 같다. 이 변경의 나머지 조각은 overlay로 가져간다: 조직 쿼터 상한(`MaxOrganizationQuota`, O3), 프런트 큰 수 약어 표기(O8; upstream `currency.ts`는 `k` 단위만 있어 10억 단위 표기 확인 필요), 옵션 값의 64-bit 파싱(O8). 패리티 검증에 "2^31 이상 잔액을 관리자 수정·충전·환급 코드·조직 지갑·내보내기/가져오기·화면 표시에서 MySQL과 PostgreSQL 모두 확인"을 둔다(§7).
- **2026-10 동기화에서 포팅한 upstream 커밋 34건과 2-A 중 upstream에 이미 있는 것**(Claude 5.x thinking 표, 충전 가드의 upstream 부분 등): 베이스에 포함되므로 재적용하지 않는다.
- **web/classic 관련 백엔드 분기**(`theme` 설정의 classic 옵션): upstream 상태를 따른다.

## 3. 재적용 규칙 (이후 동기화 비용을 결정하는 부분)

1. **독립 파일은 그대로 복사**한다(조직 모델·컨트롤러, PayPal, 내보내기, 문의, 진단 도구, 템플릿). 파일명·패키지는 team과 같게 유지해 team 히스토리를 추적할 수 있게 한다.
2. **upstream 파일을 고칠 때는 "훅 한 줄"로 제한**한다. 로직은 우리 파일에 둔다. 예: `service/funding_source.go`에는 `newFundingSource`에서 조직 분기 한 줄만 두고 `OrganizationWalletFunding`은 `service/funding_source_organization.go`에 둔다. `router/api-router.go`는 `registerOrganizationRoutes(apiRouter)` 한 줄, 라우트 본문은 `router/organization-router.go`.
3. **upstream 구조체에 컬럼을 더할 때**(`User.OrganizationId`, `OrganizationRole`, `TopUp.TargetType`, `ChannelOtherSettings.UsageSemantic*`)는 필드를 구조체 끝에 모아 두고 `// fork:` 주석을 단다. 이 목록을 `docs/fork-overlays.md`에 유지한다.
4. **upstream이 같은 문제를 다른 방식으로 풀었으면 upstream 방식을 쓴다.** 할인(O6)은 `HandleGroupRatio`가 upstream에도 그대로 있어 같은 훅 지점을 쓰지만, 가격 페이지 표시는 upstream의 새 가격 페이지 구조 위에 다시 만든다. quota 상한, 인증, i18n 메커니즘은 upstream 것을 따른다.
5. **테스트는 기능별로 같이 옮긴다.** team의 `*_test.go` 중 유지 기능에 속한 것은 함께 복사하고, upstream 변경으로 깨지는 단언은 upstream 동작 기준으로 고친다.
6. **relaykit/tokenkit는 upstream 그대로** 쓴다. 변환 로직에 fork 수정이 필요하면 `relaykit/` 안이 아니라 호스트 어댑터에서 한다(Ali `KeepThinkingBudget` 플래그는 `relaykit/dto`가 아닌 Ali 어댑터 쪽 wrapper로 다시 구현한다. 방법은 계획 단계에서 정한다).

## 4. 컴포넌트별 설계

### 4.1 베이스와 브랜치
- 브랜치 `develop`를 `qn/main` `7de10f2c6`에서 만든다(작업 중 이름 `rebase/upstream-2026-10`; 2026-10-14 단계 A 완료 후 `develop`으로 교체, 구 develop은 `develop-legacy-2026-10`로 보존). `team`은 손대지 않고, 전환 시점에 `team-legacy-2026-10`로 이름을 바꿔 보존한다.
- `go.mod`는 upstream 것을 쓰고 fork 의존성(excelize, waffo-pancake-sdk는 upstream에도 있음, miniredis 테스트용)을 더한다. 모듈 경로는 upstream과 동일(`github.com/QuantumNous/new-api`)하게 유지한다(CLAUDE.md Rule 5).

### 4.2 O1 랜딩·가이드
- 템플릿은 `web/landing/templates/`, 정적 자산은 `web/landing/public/`(upstream `web/public`은 rsbuild가 dist로 복사해 209 MB를 이중 embed하므로 분리). `main.go`에 embed 2개, `router/web-router.go`에 `registerLandingRoutes`·`landingStatic` 훅 2줄, 본문은 `router/landing-router.go`. 로그인 표시는 upstream 세션 힌트 쿠키만 쓰며 사용자명·역할 배지와 로그아웃은 없앤다.
- 실제 구현에서 페이지 4개는 명시 라우트가 아니라 NoRoute 체인의 `landingPages`(웹 rate limiter 뒤)와 `landingStatic`(앞)으로 서빙한다. 그래서 task 플러그인 디스패처가 우선권을 유지한다.
- GTM/GA4 주입은 upstream의 `analyticsInject` placeholder 방식(`main.go:265`)과 충돌하지 않도록 랜딩 템플릿에만 둔다. SPA 쪽 GA4는 upstream 메커니즘으로 설정한다.

### 4.3 O2 PayPal
- backend는 team 파일 그대로. `controller/topup.go`에 대한 의존(`rejectInvalidCreditedQuota`, `LockOrder`, `UnlockOrder`)은 upstream에도 같은 이름으로 존재함을 확인했다. `TopUp.ProviderOrderId` 컬럼을 추가한다(`// fork: O2`). 조직 target은 O3에서 추가되는 `TopUp.TargetType`을 쓴다.
- 프런트: upstream `features/wallet`에 `hooks/use-payment.ts`의 PayPal 분기, `recharge-form-card`의 provider 분기, settings의 PayPal 섹션을 추가(team의 해당 조각을 경로만 바꿔 이식).

### 4.4 O3 조직 관리
- 데이터: `organizations`, `organization_subscription_plans`, `organization_user_subscriptions`, `organization_subscription_pre_consume_records` 테이블(team 모델 그대로), `users.organization_id`/`organization_role` 컬럼, `topups.target_type`/`target_id`.
- 권한: 조직 역할(owner/admin/member)은 테넌트 범위의 자체 검사이고, upstream casbin은 관리자 리소스 권한이다. 서로 독립이므로 `/api/organization/*`(멤버·조직 관리자용 46개 라우트)는 team 검사 로직을 유지한다. 플랫폼 관리자용 `/api/organizations/*`(목록·생성·수정·삭제, 현재 `RootAuth`)만 upstream `RequirePermission`으로 감싼다: `service/authz/resources_organization.go`에 `organization` 리소스와 `read`/`write` 액션을 등록하고(`DefaultRoles`는 `root`만, 현재 범위와 동일), `router/channel-router.go`의 `handlePermissionRoute` 패턴으로 등록해 접근 토큰 스코프에도 자동 노출되게 한다(§9 결정 5).
- 과금: `OrganizationWalletFunding`/`OrganizationSubscriptionFunding`을 별도 파일로, `billing_session.go`에는 선택 훅만. upstream의 지갑 상한(2^53)과 `creditUserQuotaWithinCeiling` 패턴을 조직 지갑에도 동일 적용(team의 `ValidateOrganizationWalletCapacity`를 upstream 상한 상수로 교체).
- 프런트: `features/organizations` 16파일과 라우트 7개를 `web/src`로 이식. 의존하는 `features/wallet` 조각 중 Toss·자동충전 컴포넌트는 제거하고 결제 탭은 Stripe/Creem/Epay/Waffo/PayPal만 남긴다. 사이드바 메뉴 등록은 upstream `components/layout`의 메뉴 정의에 항목 추가.
- 단계 B 구현에서 위 문장과 달라진 점 (기록: `docs/2026-10-14-rebase-phase-b.md` §3):
  - 결제 provider는 6종이다: Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake(위 5종 표기에 Waffo Pancake가 빠져 있었다).
  - 라우트는 38개다: `/api/organization/*` 34개 + `/api/organizations/*` 4개(team 46개에서 Toss·자동충전 8개 제외). 위 "46개"는 team 기준 수치다.
  - 접근 토큰 스코프: `/api/organization/*`는 기본 세션 전용이고, 조직 지갑 조회·충전 13개만 `wallet:read`/`wallet:write`, `subscription/self`는 `profile:read`다. `/api/organizations/*`는 `organization:read`/`organization:write`가 자동 선언된다.
  - 감사 로그: 조직 변경 핸들러 12개가 `organization.*` 항목을 남긴다(upstream `auditContentTemplates`는 건드리지 않음).
  - 조직 필드는 사용자 캐시(UserBase/Lua)에 넣지 않는다. 멤버십 변경 시 사용자 캐시를 무효화한다.
  - 조직 자금 예약은 원자적이다: 이미지 수량 예약과 조직 구독 선차감은 조직 지갑을 `TryReserveOrganizationQuota`로 예약하고 부족하면 403(team은 음수 잔액을 허용했다).
  - 레거시 Midjourney는 upstream 호출 전에 조직 플랜 회원과 빈 조직 지갑을 거부한다(`relay/mjproxy_handler.go` 훅 2줄 추가).
  - "team 검사 로직을 유지"의 예외로, 이식으로 드러난 보안 결함을 고쳤다: 자기 자신 대상 변경 금지, owner 대상 변경은 owner만(구독 할당·취소는 root도), `owner` 역할 부여 금지, 멤버 제거·조직 삭제 시 `users.quota` 0, 목록 API 제한 컬럼, `ORDER BY` dialect quoting.
  - 프런트: 사이드바 등록은 `components/layout`이 아니라 `hooks/use-sidebar-data.ts`(+ `use-sidebar-config.ts`, `use-sidebar-view.ts`)다. 라우트는 `/organization` 아래 5개와 `/wallet`의 역할별 화면 전환이다. 조직 사용 로그는 prop 전달 대신 `UsageLogsProvider` 컨텍스트(`logsScope`)로 범위를 넘긴다.
  - 개인 잔액 가드(최종 리뷰 C1·M10): 조직 회원의 `users.quota`는 회원 한도라 개인 잔액 구독 결제, 교환 코드, 개인 유료 충전 주문을 거부한다(`PurchaseSubscriptionWithBalance` 훅, `TopUp` 훅, `ValidateTopUpTargetCapacity`).
  - 비활성 조직(최종 리뷰 I4): `status=2`이면 회원 relay는 403, 조직 충전은 거부, `/api/organization/*` 쓰기는 미들웨어 `RejectDisabledOrganizationMutation`이 거부한다. 조회와 root 관리는 그대로다.
  - 환급 고정(최종 리뷰 I1·M3): 환급과 조직 `used_quota`는 과금 때 고정한 조직(`RelayInfo.BillingOrganizationId`, `TaskPrivateData.OrganizationId`, `midjourneys.organization_id`)으로 가고, 회원 한도는 그 조직에 아직 소속일 때만 돌려준다.

### 4.5 O4·O5·O10
- 파일 복사 + 라우트 등록 한 줄. excelize 의존성 추가. 테스트 동반.
- 가져오기는 어떤 호출자에게도 관리자·root 계정을 만들지 않고, 행은 `common.Validate.Struct`로 검증한다. 내보내기·가져오기 오류는 UI에 표시한다.

### 4.6 O6 할인 계층
- 설정 저장(`ModelDiscount`/`VendorDiscount` 옵션, JSON map)과 `HandleGroupRatio`의 multiplier 적용은 team 코드를 그대로 쓴다(upstream `relay/helper/price.go`에 같은 함수가 있고 `GroupRatioInfo`도 유지됨).
- 표시: upstream의 가격 페이지·모델 카드·usage-log 상세는 구조가 바뀌었으므로(변경량 2,693/3,509줄) team 컴포넌트를 복사하지 않고 upstream 컴포넌트에 할인 배지·할인 요소를 추가하는 식으로 다시 만든다(§9 결정 3). 범위: 가격 표 할인 배지, 모델 카드 할인 가격, usage-log 공식의 할인 요소, 관리자 할인 편집기(모델/벤더 JSON 편집기).
- 2026-10-14 구현 메모(단계 C): 할인 조회 순서는 요청 모델명 → 그 벤더 → 과금 모델명 → 그 벤더다(마켓플레이스가 요청 모델명으로 표시하므로 보이는 할인 == 청구 할인). `GroupRatioInfo.BaseGroupRatio`로 로그에 할인 전 그룹 배율을 나눗셈 없이 남겨 100% 할인도 안전하다. 기록 `docs/2026-10-14-rebase-phase-c.md` §1.

### 4.7 O7 타이밍
- `middleware/request_timing.go`, `common/timing_response_writer.go`는 복사. `RelayInfo`에 `UpstreamRequestStartTime/EndTime` 필드(규칙 3), `api_request.go doRequest`에 기록 두 줄, `log_info_generate.go`에 `other` 필드 채우기 함수 호출 한 줄(본문은 `service/log_timing.go`). usage-log 상세의 타이밍 표시는 upstream 다이얼로그에 섹션 추가.
- 2026-10-14 구현 메모(단계 C): 본문은 `service/log_timing_fork.go`다. `e2e_ms`/`llm_ms`/`gateway_ms`는 public, 내부 구간 `pre_llm_ms`/`post_llm_ms`는 admin 전용(`SetAdmin`)이고, hijack된 realtime WSS 로그에는 타이밍 키가 없다. `TimingResponseWriter.Unwrap()`으로 write deadline이 유지된다(team에도 같은 결함이 있어 역이식 필요). 기록 `docs/2026-10-14-rebase-phase-c.md` §2.

### 4.8 O9 한글

단계 D에서 아래처럼 구현했다(기록 `docs/2026-10-15-rebase-phase-d.md`). 처음 문안(team `kr.json` 4,489키 재사용 + 신규 2,778키 번역, `docs/translation-glossary.md`의 한글 규칙, 백엔드 67키, "upstream의 옵션 메커니즘으로 en/ko만 노출")은 아래 내용으로 대체됐다.

- 언어 코드: 내부 코드는 BCP-47 `ko`다. team의 `kr`은 레거시 값으로만 받는다(`Intl`이 `kr`을 카누리어로 읽기 때문). 받는 곳: 프런트 `normalizeInterfaceLanguage`·`convertDetectedLanguage`(`kr`, `ko`, `ko-*` → `ko`; `kok`·`kos`는 아님), 로그인 직후 저장된 사용자 언어 정규화(`use-auth-redirect.ts`), 백엔드 `i18n.normalizeLang`(`Accept-Language`·사용자 설정), `common.ParseInterfaceLanguages`. 기존 team 사용자의 `setting.language = 'kr'`는 데이터 마이그레이션 없이 읽을 때 정규화한다.
- 프런트 locale: fork 파일 `web/src/i18n/locales/ko.json`(en.json 키 순서, 단계 D 끝 7,538키). i18next `resources`·`supportedLngs`, `languages.ts` 옵션 `{ code: 'ko', label: '한국어' }`, 달력 `react-day-picker`의 `ko`는 한 줄 훅이다.
- 번역 도구: `web/scripts/ko-locale.mjs`(`bun run i18n:ko`). `seed`는 team `kr.json`에서 검증을 통과한 한글 값을 재사용하고(4,412키) 나머지를 영어 placeholder로 채운다. ko.json이 있으면 `--force` 없이는 거부한다. `check`는 보간 `{{…}}`·Trans/이름 태그·printf·앞뒤 공백·`\n` 수·한글 포함을 검사하고, `status`는 남은 영어 placeholder 수를 센다. 영어를 유지할 키(브랜드·식별자·테마 이름)는 스크립트의 `MANUAL_SKIP`과 `docs/i18n/ko-skip.json`(183키)에 있다. 번역은 6배치 LLM 초안 + 용어집 검수 + 배치별 리뷰였고, 배치 목록 파일은 작업 뒤 지웠다.
- 용어집: `docs/translation-glossary.ko.md`(문체 규칙과 용어 표). 주요 결정은 §9 결정 13.
- 인터페이스 언어 제한(O8): team의 하드코딩(`[en, kr]`) 대신 옵션 `InterfaceLanguages`(쉼표로 구분한 코드, 기본 `en,ko`)다. `/api/status`가 `interface_languages`(허용 코드 `en, ko, zhCN, zhTW, fr, ru, ja, vi`, 대소문자 무시, `kr`→`ko`, 관리자 순서 유지, 비면 `["en"]`)를 내려 주고, 헤더 언어 메뉴와 프로필 언어 카드가 이 목록으로 거른다. 리소스는 8개 locale을 모두 로드한다. 사용자가 이미 고른 언어는 목록에 없어도 메뉴에 남는다. 관리 화면은 시스템 설정 → 운영 → 인터페이스 언어(체크박스, 하나 이상).
- 백엔드: fork 파일 `i18n/locales/ko.yaml`(develop `en.yaml`과 같은 66키; team 값 19개 재사용 + 47개 번역), `i18n.go`의 `LangKo`·파일 목록·localizer·`SupportedLanguages()`. fork 파일에서 웹 콘솔 사용자에게 나가는 오류 41곳을 `common.NewMessage`/`ApiErrorT`(message_key) 경로로 옮겼고, 그 키 66개를 8개 locale JSON에 넣었다(ko 번역, 다른 6개는 영어). upstream 파일의 같은 패턴은 훅 규칙상 그대로 둔다.
- README: `README.kr.md`(upstream 신판 `README.md` 기준 번역, team 구판의 같은 절 문장 재사용)와 6개 README의 언어 nav 한 줄(`<!-- fork: O9 -->`).

### 4.9 O11 fork 고유 수정
- Epay 트랜잭션 정산(`model.RechargeEpay`, `EpayNotify` 응답 후행): team 커밋 2건을 upstream `controller/topup.go` 구조에 맞춰 재적용.
- 충전 전 수용량 검증은 upstream에 epay/Stripe/Creem/Waffo 용이 있으므로 PayPal·조직 지갑 진입점에만 team 패턴을 추가.
- Gemini modality 귀속·캡: upstream `relaykit/dto/usage_merge.go`와 `relay-gemini.go`를 읽고 같은 효과가 없으면 호스트 정산 쪽(`service/text_quota.go` 진입점)에 캡을 둔다.
- Ali `thinking_budget` either-name: 규칙 6에 따라 호스트 어댑터에서.

### 4.10 O12 OpenWebUI
- `service/oidc`, `service/openwebui`, `controller/oauth2_provider.go`, `openwebui_link.go`, 모델·라우트·설정 파일은 복사. `middleware/auth.go`의 TokenAuth 치환 훅은 upstream의 `classifyDashboardCredential`/`lookupAccessToken` 구조에 맞춰 다시 쓴다(OpenWebUI 서비스 키 → 사용자 토큰 치환이 scoped access token 경로와 충돌하지 않도록 relay 경로의 `TokenAuth`에만 둔다). 프런트 28파일은 `web/src`로 이식.
- 이 overlay는 마지막에 하고, 그 전까지 `OpenWebUI` 브랜치는 그대로 둔다.

## 5. 데이터와 스키마

- 운영 DB에는 Toss·자동충전·빌링키 테이블과 `users.toss_customer_key` 컬럼이 남는다. 첫 릴리스에서는 **삭제하지 않는다**(되돌림 대비, §9 결정 2). 삭제는 전환 후 별도 작업.
- upstream 신규 테이블(`audit_logs`, `user_sessions`, `authz_roles`, `casbin_rule`, `task_plugins`, `system_tasks`, `login_encryption_keys`, `external_identity_claims` 등)은 AutoMigrate가 만든다. upstream의 추가 마이그레이션(`options` PK 재구성, token key 유니크 제약, prefill group 인덱스)은 그대로 실행된다.
- **quota 컬럼**: upstream 구조체는 `type:int` 태그에 Go `int`이고, 기동 시 `ensureUserQuotaColumns`가 MySQL/PostgreSQL에서 bigint가 아니면 거부한다. 우리 DB는 이미 bigint다. upstream의 커스텀 migration dialector(`model/migration_dialector.go`)가 기존 컬럼을 좁히지 않는지 **운영 DB 사본에서 리허설**한다. 좁히려 들면 `User`/`Token`/`Log`/`TopUp`의 quota 컬럼 태그를 bigint로 두는 패치를 규칙 3 목록에 추가한다.
- 리허설 절차: MySQL과 PostgreSQL 운영 사본에 새 바이너리를 기동 → 로그에서 ALTER 문 확인 → `users.quota` 등 타입이 bigint 유지인지 조회 → 조직/구독/충전 샘플 데이터 조회로 읽기 호환 확인.

## 6. 프런트엔드

- 베이스는 upstream `web/src`(React 19, Base UI, Tailwind, TanStack Router). team의 `web/default`와 같은 컴포넌트 라이브러리라 조직 UI 16파일의 import는 전부 해석된다(확인함).
- 재작성이 필요한 화면(upstream 변경량이 큰 곳): 시스템 설정(PayPal·문의 메일·할인 편집기·GTM 섹션 추가), 가격 페이지(할인 표시), usage-log 상세(타이밍·할인 요소), 사용자 페이지(내보내기·가져오기 버튼·다이얼로그), 프로필(보안 설정 상태 표시는 upstream에 유사 기능이 있으면 그것을 씀).
- 빌드는 upstream `web/`(Bun, rsbuild) 그대로. Dockerfile도 upstream 것.

## 7. 테스트와 검증

1. 베이스 브랜치에서 upstream 테스트 전부 통과(기준선 기록).
2. overlay마다 team 테스트 이식 + 통과. 과금 overlay(O3, O6, O11)는 SQLite 외에 MySQL/PostgreSQL 컨테이너에서도 1회 실행.
3. 프런트: `bun run typecheck`, `bun test`, `bun run build`; 조직 e2e(playwright, team `web/default/e2e`)를 새 UI에 맞춰 이식.
4. 패리티 체크리스트: `docs/manuals/organization-*.md`, `docs/discount/admin-guide.md`, 랜딩 릴리스 체크리스트(`docs/landing-release-checklist.md`)를 수동 시나리오로 실행. 큰 잔액 시나리오(2^31 초과, 예: 50억 quota)를 관리자 사용자 수정·조직 쿼터 수정·충전 정산·환급 코드·내보내기/가져오기·대시보드 표시에서 MySQL과 PostgreSQL 각각 확인한다(`docs/quota-ceiling-and-bigint-migration.md` §2.1의 원래 증상이 재발하지 않아야 한다).
5. 스키마 리허설(§5).
6. 전환 전 1주 스테이징 운영(실 트래픽 미러 또는 내부 사용).

## 8. 단계와 전환

| 단계 | 내용 | 산출물 |
|---|---|---|
| A | 브랜치 생성, go.mod/의존성, 기준선 테스트, 스키마 리허설, O1·O2·O4·O5·O8·O10 | 독립 기능이 동작하는 빌드 |
| B | O3 조직(backend → 프런트) | 조직 매뉴얼 시나리오 통과 |
| C | O6 할인, O7 타이밍, usage-log·가격 UI | 할인 가이드 시나리오 통과 |
| D | O9 한글 | ko locale, 용어집 검수 |
| E | O11 fork 고유 수정 | 기록 문서 |
| F | O12 OpenWebUI | OpenWebUI 통합 테스트 스택(`deploy/openwebui-test`) 통과 |
| G | 스테이징 → `team` 교체(`team-legacy-2026-10` 보존), `docs/fork-overlays.md`와 동기화 절차 문서 | 전환 |

각 단계는 독립 PR/브랜치로 리뷰·병합하고, 단계마다 별도의 구현 계획(`docs/superpowers/plans/`)을 쓴다. 단계 A가 끝나면 이후 upstream 동기화 리허설(`git merge qn/main`)을 한 번 해 보고 충돌 파일 수를 기록한다. 그 수가 규칙 3 목록과 일치해야 한다.

## 9. 결정 사항

2026-10-14 확정.

1. `web/classic` 사용자 없음 → classic은 버린다(§2.2).
2. Toss 데이터 테이블은 첫 릴리스에서 남긴다(§5).
3. 할인 UI는 upstream 가격 페이지 위에 다시 만든다(§4.6).
4. 한글 신규 키 2,778개는 기계 번역 초안 + 용어집 기준 사람 검수(§4.8). 기본값으로 진행하며, 전부 사람 번역으로 바꾸려면 단계 D 계획 전에 알린다.
5. `RequirePermission` 범위는 플랫폼 관리자용 `/api/organizations/*`만(§4.4). 기본값으로 진행하며, 조직 내부 관리 라우트까지 넓히려면 단계 B 계획 전에 알린다.

단계 B 최종 리뷰가 넘긴 제품 결정(기록 `docs/2026-10-14-rebase-phase-b.md` §3·§4). 단계 C에서 정하고 구현했다(기록 `docs/2026-10-14-rebase-phase-c.md` §3):

6. 2026-10-14 확정: 조직 미소속 사용자 할당(I2)은 초대·수락 단계 없이 "정확 조회 + 개인 잔액이 있으면 거부"다. 할당 후보 목록(`GET /api/organization/assignable-users`)을 없애고 `GET /api/organization/users/lookup`(username 정확 일치, 없고 `@`가 있으면 email 정확 일치; 응답은 id·username·display_name·organization_id만)으로 바꿨다. 미소속 사용자는 `quota > 0`, `aff_quota > 0` 또는 활성 개인 구독이 있으면 할당이 거부된다(API·Excel 기존 사용자 행 모두; Excel이 새로 만드는 사용자는 해당 없음). 단계 C Task 8.
7. 2026-10-14 확정: 플랫폼 사용자 필드(I7)는 조직 범위 필드로 분리했다. `users.organization_member_status`(1 active / 2 suspended)와 `users.organization_remark`(255자) 컬럼을 추가하고, 조직 API는 `member_status`·`organization_remark`만 쓰며 플랫폼 `status`·`remark`는 root 전용이다(옛 키는 `unsupported field`로 거부). 정지된 회원은 relay(403)·레거시 MJ·`/api/organization/*`의 GET이 아닌 요청이 모두 막히고, 로그인과 본인 조회(`/wallet` 요약, `subscription/self`, `topup/self`)는 된다. 조직을 떠나면 두 값은 초기화된다. 단계 C Task 9.

단계 D(한국어·인터페이스 언어·메시지 경로)에서 정한 것. 2026-10-15 확정(기록 `docs/2026-10-15-rebase-phase-d.md` §5):

8. 한국어 코드는 `ko`이고 `kr`은 레거시 입력으로만 받는다. 기존 `kr` 사용자 데이터는 마이그레이션하지 않고 읽을 때 정규화한다(§4.8).
9. 다른 6개 locale(zh, zh-TW, fr, ru, ja, vi)의 단계 A~D fork 키 영어 placeholder는 이 단계에서 번역하지 않는다(단계 E 이후).
10. 결정 4의 실행 방식: 6배치 LLM 초안 + 용어집 + 배치별 기계 검증(`ko-locale.mjs check`)과 표본 리뷰, 마지막에 전체 용어 통일. 사람 번역 전면 교체는 하지 않았다.
11. O8 인터페이스 언어 제한은 옵션 `InterfaceLanguages`(기본 `en,ko`)로 한다. 리소스는 모두 로드하고 메뉴만 거르며, 사용자의 현재 언어는 유지한다(§4.8).
12. 백엔드 `ko.yaml`은 develop `en.yaml`의 66키 기준이다. fork 파일의 사용자 대상 오류만 message_key 경로로 옮기고, upstream 파일의 같은 패턴은 손대지 않는다. Excel 조직 가져오기의 행 오류(포맷 문자열)는 구조화된 행 오류가 필요해 영어로 남긴다.
13. 용어: Quota = 쿼터, Reset = 초기화, Vendor = 공급업체(Provider = 제공자와 구분), Tier = 등급, Revoke = 폐기, sk- 자격 증명 Token = API 키(개인 액세스 토큰 = 액세스 토큰), Redemption code = 충전 코드, 문체는 합쇼체·명사형 라벨(`docs/translation-glossary.ko.md`).
14. 버려진 개인 충전·구독 주문은 관리자 `POST /api/user/topup/expire`(1시간 지난 pending만, 감사 `topup.expire`)로 만료한다. 조직 주문 만료(`POST /api/organizations/:id/topups/expire`)와 분리한다.

## 10. 리스크

- upstream `controller/user.go`·`model/user.go` 변경(1,000줄 규모)에 조직 컬럼·훅을 다시 넣는 과정에서 사용자 관리 흐름이 깨질 수 있다. 사용자 CRUD 테스트를 단계 B 게이트로 둔다.
- OpenWebUI의 토큰 치환이 upstream의 scoped access token과 경로를 공유하면 보안 경계가 흐려질 수 있다. relay 경로에만 두고 대시보드 경로에는 절대 두지 않는다.
- 한글 번역 품질: 기계 번역 초안은 용어집으로 검수하지 않으면 기존 톤과 어긋난다.
- 전환 후 되돌림: `team-legacy-2026-10` 바이너리와 DB는 upstream 마이그레이션(옵션 PK 재구성 등) 이후 호환되지 않을 수 있다. 전환 전에 DB 스냅샷을 남긴다.
