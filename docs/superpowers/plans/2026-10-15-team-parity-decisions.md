# team 재현 감사 결정 항목 포팅 계획 (1·3~11)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `docs/2026-10-15-team-parity-audit.md`의 결정 항목 11개 중 사용자가 정한 대로 2번(API 키 캐시 동기 델타·재큐)만 보류하고 나머지 10개를 `develop`에 반영한다: ComplianceConfirmed 기본값, 로고·Home의 랜딩 이동, 빠른 시작 PDF, SPA 챗봇 위젯, 가격 프로모션 안내, 가입 도메인 안내와 로그 열 이름, 사이드바 모듈 카드 라벨, GA4 ID 운영값 문서화, Claude adaptive thinking 표시 기본값, 이메일 발송 오류 메시지.

**Architecture:** 백엔드는 upstream 파일에 `// fork: O11` 훅 한 줄 + fork 파일(로직·테스트). 결제 설정 기본값은 upstream 리터럴 대신 같은 패키지의 fork `init()`으로 덮어써 upstream 줄을 바꾸지 않는다. Claude adaptive display는 relaykit을 건드리지 않고 호스트의 네이티브 핸들러와 Claude 어댑터 두 지점에 훅을 둔다. 웹은 upstream 컴포넌트에 최소 줄(표식)만 바꾸고 10줄 넘는 블록은 fork 컴포넌트로 뺀다. GA4는 코드 변경 없이 운영값을 문서에 적는다(upstream 테스트가 "환경 변수 없음 = GA 없음"을 고정하므로 기본값을 코드에 넣지 않는다). 모든 훅은 `docs/fork-overlays.md` O11 "team 재현 감사 이월" 절에 기록한다.

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, React 19 + TanStack Router + i18next, vitest, Bun.

**Spec:** `docs/2026-10-15-team-parity-audit.md` §3.2·§3.4·§3.6·§4와 사용자 결정(2026-10-15: 2번 보류, 나머지 진행). team 출처: 3f6ab2e33(기본값 부분), a05006778·4e787397f·a2f680700(내비 부분), 9b4ee5213·a3e606bc9, 3757c8244(SPA 부분), d5e459993·330d46466, 7daa397a1(가입 안내·열 이름), c1325db97(사이드바 카드 라벨만), 350662f4c(값만), 9088a5703, 30a3fe6a9(misc.go 부분).

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`(시작 HEAD `2cf12c526`). `/Users/molla/work/ai/new-api`(team)는 읽기 전용.
- Go는 `GOWORK=off`. JSON은 `common.*`만. 새 SQL 없음. `relaykit/` 수정 금지(`dto.Thinking.Display` 공개 필드만 읽고 쓴다).
- upstream Go 파일은 한 줄 훅 `// fork: O11`; 로직은 `*_fork.go`. 웹 upstream 파일은 최소 줄에 `// fork: O11`/`{/* fork: O11 */}`/`<!-- fork: O11 -->` 표식, 10줄 넘는 블록은 fork 파일. 모든 변경을 `docs/fork-overlays.md` O11 "team 재현 감사 이월" 절에 기록.
- 웹 i18n 키는 en.json(값=키)·ko.json에 텍스트로 추가(`bun run i18n:sync` 금지); `node scripts/ko-locale.mjs check`와 `bun run test -- ko-coverage` 통과. 백엔드 `common.NewMessage` 키는 en/ko json에 넣고 `controller/organization_i18n_test.go`의 `forkMessageSources`에 fork 파일을 추가한다.
- 웹 게이트: 손댄 파일에 oxlint/oxfmt 새 문제 없음(`bun run format:check` 출력에서 손댄 파일만 확인), `bun run typecheck`. 새 TS/TSX 파일은 AGPL 헤더.
- 커밋 트레일러: `Ported-From: team <sha>`(출처마다 한 줄) + `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.

## Review Focus

1. **새로 설치한 운영자:** 결제 수단이 compliance 확인 없이 바로 보여야 한다(team과 같음). 이미 저장된 설정(`compliance_confirmed=false`)이 있으면 그 값이 이긴다. → Task 1 `TestPaymentComplianceDefaultsToConfirmed`.
2. **adaptive thinking을 `display` 없이 보내는 클라이언트:** Opus 4.7+에서 요약이 돌아와야 하고, `display`를 명시한 요청과 budget 기반 thinking은 손대지 않아야 한다. → Task 1 네이티브·OpenAI 호환 두 경로 테스트.
3. **메일 서버가 죽었을 때 인증 메일을 요청한 사용자:** SMTP 원문이 아니라 고정 문구를 받고, 원문은 서버 로그에만 남아야 한다. → Task 1 `TestSendEmailVerificationHidesSMTPError`.
4. **로그인한 사용자가 로고를 누를 때:** 대시보드로 가야 하고, 비로그인은 서버 렌더 랜딩으로 전체 새로고침해야 한다. → Task 2 public-header 테스트.
5. **한국어 사용자의 빠른 시작 PDF:** ko면 한국어 PDF, en이면 영어 PDF가 열려야 한다. → Task 3 overview 테스트.

---

### Task 1: 백엔드 셋 — ComplianceConfirmed 기본값, Claude adaptive display 기본값, 이메일 발송 오류 문구

**Files:**
- Create: `setting/operation_setting/payment_setting_fork.go`, `setting/operation_setting/payment_setting_fork_test.go`, `relay/channel/claude/thinking_display_fork.go`, `relay/channel/claude/thinking_display_fork_test.go`, `relay/claude_thinking_display_fork_test.go`, `controller/email_send_error_fork.go`, `controller/email_send_error_fork_test.go`
- Modify: `relay/claude_handler.go`(relaykit `ApplyClaudeThinkingModel` 호출 직후 한 줄), `relay/channel/claude/adaptor.go`(`ConvertOpenAIRequest`가 relaykit 변환 결과를 얻은 직후 한 줄), `controller/misc.go:243`(한 줄), `controller/organization_i18n_test.go`(`forkMessageSources`에 한 줄), `web/src/i18n/locales/en.json`·`ko.json`(키 1개), `web/src/features/system-settings/billing/index.tsx:74-75`·`web/src/features/subscriptions/components/subscriptions-provider.tsx`(compliance 기본값 두 줄, 표식)

**Interfaces:**
- Consumes: `paymentSetting`(`setting/operation_setting/payment_setting.go:19`, 패키지 변수), `CurrentComplianceTermsVersion`(:16), `IsPaymentComplianceConfirmed()`(:33); `dto.Thinking{Type, Display}`(relaykit, 공개); `common.ApiError`, `common.NewMessage`, `common.SysError`.
- Produces: fork `init()`이 `paymentSetting.ComplianceConfirmed = true`, `ComplianceTermsVersion = CurrentComplianceTermsVersion`로 기본값을 바꾼다(옵션 로드가 DB 값으로 덮어쓰는 것은 그대로). `claude.NormalizeAdaptiveThinkingDisplay(t *dto.Thinking)`: `t != nil && t.Type == "adaptive" && t.Display == ""`이면 `Display = "summarized"`. `apiEmailSendFailed(c *gin.Context, err error)`: `common.SysError`에 원문을 남기고 `common.ApiError(c, common.NewMessage("Failed to send email"))`.

- [ ] **Step 1: 실패하는 테스트**

`payment_setting_fork_test.go`: 패키지 초기 상태에서 `IsPaymentComplianceConfirmed()`가 true(다른 테스트가 전역을 바꾸면 `t.Cleanup`으로 복구하는 helper를 쓴다). 옵션 로드 경로(`payment_setting.go`의 setter/`UpdatePaymentSetting` 같은 함수)로 `compliance_confirmed=false`를 넣으면 false가 되는지도 확인.

`thinking_display_fork_test.go`: nil → 그대로; `{Type:"adaptive"}` → `Display=="summarized"`; `{Type:"adaptive", Display:"omitted"}` → 그대로; `{Type:"enabled", BudgetTokens:…}` → 그대로. `relay/claude_thinking_display_fork_test.go`: 네이티브 요청 `{"model":"claude-opus-4-7","thinking":{"type":"adaptive"}}`를 핸들러의 요청 변환 경로에 넣어 upstream 본문의 `thinking.display`가 `summarized`인지(기존 `relay/*claude*_test.go`의 변환 테스트 패턴을 따른다); OpenAI 호환 경로는 `relay/channel/claude` 어댑터의 `ConvertOpenAIRequest` 테스트에 같은 단언을 더한다(IncludeThoughts 없이 adaptive가 나오는 입력: 기존 테스트에서 adaptive가 만들어지는 케이스를 찾아 쓴다).

`email_send_error_fork_test.go`: `common.SendEmail`을 실패시키는 설정(SMTP 서버 주소를 비우거나 닫힌 포트)으로 `SendEmailVerification`을 httptest로 호출 → 응답 `message`가 `Failed to send email`(ko 헤더면 `메일을 보내지 못했습니다`)이고 SMTP 원문이 응답에 없음. 실행: 각 패키지 `-run` → FAIL/컴파일 실패.

- [ ] **Step 2: 구현**

`payment_setting_fork.go`:

```go
package operation_setting

// fork: O11 — a fresh install shows payment methods without the compliance
// click (team 3f6ab2e33). Persisted options still override at load time.
func init() {
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = CurrentComplianceTermsVersion
}
```

웹 기본값: `billing/index.tsx:74-75`의 `'payment_setting.compliance_confirmed': false` → `true // fork: O11`, `'payment_setting.compliance_terms_version': ''` → `CURRENT_COMPLIANCE_TERMS_VERSION`(그 상수가 파일에 있으면; 없으면 `'v1'`), subscriptions-provider도 같은 두 줄.

`relay/channel/claude/thinking_display_fork.go`:

```go
package claude

import "github.com/QuantumNous/new-api/relaykit/dto"

// fork: O11 — Anthropic defaults adaptive thinking.display to "omitted" from
// Opus 4.7: thinking tokens are billed but the blocks come back empty. Relayed
// clients expect the Opus 4.6 summary, so fill the default when the client
// said nothing. An explicit value always wins; budget thinking is untouched
// (team 9088a5703).
func NormalizeAdaptiveThinkingDisplay(t *dto.Thinking) {
	if t == nil || t.Type != "adaptive" || t.Display != "" {
		return
	}
	t.Display = "summarized"
}
```

훅: `relay/claude_handler.go`에서 relaykit의 thinking 처리(`relayconvert.ApplyClaudeThinkingModel(...)` 호출)가 끝난 직후 `claude.NormalizeAdaptiveThinkingDisplay(request.Thinking) // fork: O11`(어댑터 패키지 import가 이미 있으면 재사용). `relay/channel/claude/adaptor.go`의 `ConvertOpenAIRequest`에서 relaykit 변환이 돌려준 `*dto.ClaudeRequest`에 대해 같은 한 줄. 두 위치의 정확한 줄은 구현 때 찾아 overlay에 적는다.

`controller/email_send_error_fork.go`:

```go
package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O11 — never echo the SMTP error to the user (team 30a3fe6a9).
func apiEmailSendFailed(c *gin.Context, err error) {
	common.SysError(common.LogText("email send failed: %s", err.Error()))
	common.ApiError(c, common.NewMessage("Failed to send email"))
}
```

`controller/misc.go:243` `common.ApiError(c, err)` → `apiEmailSendFailed(c, err) // fork: O11`. en.json/ko.json에 `Failed to send email` / `메일을 보내지 못했습니다`. `forkMessageSources`에 `controller/email_send_error_fork.go`.

실행: Step 1 PASS, `GOWORK=off go vet ./setting/... ./relay/... ./controller/ && GOWORK=off go test ./setting/... ./relay/... ./controller/ -count=1`, `cd web && bun run typecheck && bun run test -- ko-coverage billing && node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋** (항목별 커밋 3개)

```bash
git commit -m "feat(payment): confirm payment compliance by default on fresh installs

Ported-From: team 3f6ab2e33
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git commit -m "fix(claude): return the adaptive thinking summary when the client sets no display

Ported-From: team 9088a5703
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git commit -m "fix(email): hide the SMTP error behind a fixed message on verification mail failure

Ported-From: team 30a3fe6a9
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: 로고·Home 링크의 랜딩 이동

**Files:**
- Modify: `web/src/components/layout/types.ts`(`forceReload?: boolean` 한 줄), `web/src/hooks/use-top-nav-links.ts:67`(Home에 `forceReload: true`), `web/src/components/layout/components/nav-link-item.tsx`(forceReload면 `<a href>`), `web/src/components/layout/components/public-header.tsx:220, 267, 397`(로고와 링크 두 곳), `web/src/components/layout/components/top-nav.tsx`(forceReload 분기), `web/src/components/layout/components/system-brand.tsx:60-76`(`Link to='/'` → `<a href='/'>`)
- Test: `web/src/components/layout/components/__tests__/public-header-landing-nav.test.tsx`(생성)

**Interfaces:**
- Consumes: `NavLink` 타입(layout/types.ts), `homeUrl` prop(public-header.tsx:56,72), `isAuthenticated`(public-header.tsx:420에서 이미 씀), TanStack `Link`.
- Produces: `forceReload` 링크는 `<a href>`로 렌더해 Go 서버가 랜딩을 서빙하게 한다. 로고는 로그인 상태면 `<Link to='/dashboard'>`, 아니면 `<a href={homeUrl}>`. Home 링크는 `forceReload: true`(team a05006778·4e787397f·a2f680700의 내비 부분; 프로필 배지와 서버 템플릿 부분은 §4.2 결정으로 제외).

- [ ] **Step 1: 실패하는 테스트** — 비로그인으로 렌더하면 로고가 `a[href="/"]`이고 Home 링크도 `a[href="/"]`(Link가 아닌 일반 앵커, `data-status`/router 속성 없음); 로그인 상태(auth store에 사용자)면 로고가 `/dashboard`로 가는 Link. 기존 layout 테스트의 라우터·store 셋업을 재사용. 실행 → FAIL.
- [ ] **Step 2: 구현** — 위 파일에 표식과 함께 최소 변경. 로고 분기(`isAuthenticated ? <Link …> : <a …>`)가 10줄을 넘으면 `brand-link-fork.tsx`로 빼서 한 줄로 끼운다. 실행: Step 1 PASS, `cd web && bun run typecheck && bun run test -- public-header top-nav system-brand`, oxlint/oxfmt.
- [ ] **Step 3: 커밋** — `feat(web): send the logo and Home link to the server-rendered landing page` (`Ported-From: team a05006778`, `4e787397f`, `a2f680700`).

---

### Task 3: 빠른 시작 PDF, SPA 챗봇 위젯, 가격 프로모션 안내

**Files:**
- Create: `web/public/AlRouter Quick Start Guide_v1.0.pdf`, `web/public/AlRouter Quick Start Guide_EN_v1.0.pdf`(team에서 `git show 9b4ee5213:"web/default/public/AlRouter Quick Start Guide_v1.0.pdf" > …`, `a3e606bc9` 영어본), `web/src/features/dashboard/components/overview/quickstart-pdf-link-fork.tsx`
- Modify: `web/src/features/dashboard/components/overview/overview-dashboard.tsx`(빠른 작업 목록에 PDF 항목 한 줄 또는 `Create API Key` 버튼 옆에 fork 컴포넌트 한 줄 — "Create API Key"는 지우지 않는다), `web/index.html:19` 앞(챗봇 스크립트 한 줄, `<!-- fork: O11 -->`), `web/src/components/ui/alert.tsx:28-35`(`notice` variant 한 항목), `web/src/features/pricing/index.tsx`(제목 아래 `<Alert variant='notice'>` 블록 → `pricing-promo-notice-fork.tsx`로 빼서 한 줄), en/ko 키
- Test: overview 테스트(ko → 한국어 PDF 경로, en → `_EN_` 경로), pricing 테스트(안내 문구 렌더)

**Interfaces:**
- Produces: `QuickstartPdfLink`: `i18n.language`가 `en`이면 `/AlRouter Quick Start Guide_EN_v1.0.pdf`, 아니면 `/AlRouter Quick Start Guide_v1.0.pdf`(team a3e606bc9 최종 규칙), `<a target='_blank' rel='noopener noreferrer'>`, 라벨 `t('Quick Start Guide(PDF)')`(ko `빠른 시작 가이드(PDF)`). 챗봇: `<script src="https://chatbot.alrouter.ai/widget.js" data-api-base="https://chatbot.alrouter.ai"></script>`(team 3757c8244). 안내: `Prices reflect promotional rates and may vary over time and by model.`(ko `표시된 가격은 프로모션 요율이며 시기와 모델에 따라 달라질 수 있습니다.`), `notice` variant는 team 330d46466 최종 스타일(굵게, warning 색).

- [ ] **Step 1: 실패하는 테스트** → FAIL.
- [ ] **Step 2: 구현** — 실행: `cd web && bun run typecheck && bun run test -- overview pricing ko-coverage && node scripts/ko-locale.mjs check && bun run build`(PDF가 `dist`에 복사되는지 확인), oxlint/oxfmt.
- [ ] **Step 3: 커밋** — 세 커밋: `feat(web): add the quick start guide PDF download` (`Ported-From: team 9b4ee5213`, `a3e606bc9`), `feat(web): load the chatbot widget in the dashboard SPA` (`3757c8244`), `feat(pricing): show the promotional pricing notice` (`d5e459993`, `330d46466`).

---

### Task 4: 가입 화면 도메인 안내, 사용 로그 토큰 열 이름, 사이드바 모듈 카드 라벨

**Files:**
- Modify: `web/src/features/auth/sign-up/index.tsx`(제목 `t('Create an account')` 아래 안내 `<p>` — 4줄이면 표식으로 직접), `web/src/features/usage-logs/components/columns/common-logs-columns.tsx:735`(`header: t('Tokens', { context: 'usage' })` → `t('tokenInOut')`, `meta.label`도), `web/src/features/profile/components/sidebar-modules-card.tsx:62, 79, 84, 89, 116, 121, 126`(라벨 7개; 항목은 지우지 않는다), en/ko 키
- Test: sign-up 테스트(안내 문구 렌더), columns 테스트(헤더 라벨), sidebar-modules-card 테스트(라벨)

**Interfaces:**
- Produces: i18n 키 `Only contracted domains can sign up`(ko `계약된 도메인만 가입할 수 있습니다`; team 7daa397a1은 한국어 원문을 키로 썼지만 develop 규칙대로 영어 키), `tokenInOut`(en `Tokens (in/out)`, ko `토큰 (입력/출력)`); 라벨: `Chat Area`→`Chat`, `Console Area`→`General`, `Dashboard`→`Overview · Dashboard`, `Token Management`→`API Keys`, `Personal Center Area`→`Personal`, `Wallet Management`→`Wallet`, `Personal Settings`→`Profile`(team c1325db97; 항목 제거·프로필 배치·passkey/2FA 카드 변경은 upstream `/security` 구조라 제외). 새 라벨이 기존 키면 재사용.

- [ ] **Step 1: 실패하는 테스트** → FAIL.
- [ ] **Step 2: 구현** — 실행: `cd web && bun run typecheck && bun run test -- sign-up common-logs sidebar-modules ko-coverage && node scripts/ko-locale.mjs check`, oxlint/oxfmt.
- [ ] **Step 3: 커밋** — 두 커밋: `fix(web): sign-up domain notice and token column label` (`Ported-From: team 7daa397a1`), `fix(web): shorter sidebar module labels` (`Ported-From: team c1325db97`).

---

### Task 5: GA4 운영값 문서화, 기록, overlay, 전체 검증, 병합 리허설

**Files:**
- Modify: `.env.example`(`# GOOGLE_ANALYTICS_ID=G-Z73Y7213LF  # 운영 GA4 ID(team 350662f4c는 코드에 고정했음; develop은 환경 변수)`), `docker-compose.yml`(같은 주석), `docs/manuals/`의 적절한 운영 문서 또는 `docs/2026-10-15-team-parity-audit.md` §3.6(운영 배포 설정에 반드시 넣을 것), `docs/2026-10-15-team-parity-audit.md`(결정 항목 행에 develop 커밋과 결과; 2번은 "보류 — 키 한도에만 영향"), `docs/fork-overlays.md`

- [ ] **Step 1: 전체 검증** — `GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"`; `cd web && bun run typecheck && bun run test && bun run build && node scripts/ko-locale.mjs check`. 숫자 기록.
- [ ] **Step 2: overlay 확인** — `grep -rn "fork: O11"`로 이번 계획의 훅이 모두 표에 있고 줄이 맞는지.
- [ ] **Step 3: 병합 리허설** — `git fetch qn && git merge --no-commit --no-ff qn/main`; 충돌 분류; `git merge --abort`.
- [ ] **Step 4: 문서** — 감사 문서 §3.1~§3.6·§4 갱신, GA 주석, 상태 줄.
- [ ] **Step 5: 커밋** — `docs: record the parity decision ports and the GA4 production id` (트레일러 Fable).

---

## 계획 결정 사항

1. 2번(API 키 캐시 동기 델타·재큐)은 보류한다. 키 한도와 키 통계에만 영향이 있고 전체 차감에는 영향이 없다(사용자 결정).
2. ComplianceConfirmed 기본값은 upstream 리터럴을 고치지 않고 fork `init()`으로 덮어쓴다. DB에 저장된 값이 로드 때 이기므로 기존 운영 DB에는 영향이 없다.
3. Claude adaptive display는 "빈 값일 때만 summarized"다. 과금에 영향이 없고 클라이언트 값은 그대로 전달되므로 "네이티브 passthrough 유지" 결정과 충돌하지 않는다(사용자 확인).
4. GA4 ID는 코드에 넣지 않는다. upstream 테스트가 "환경 변수 없음 = GA 없음"을 고정하므로, 운영값을 배포 설정 문서에 적는다.
5. c1325db97은 사이드바 모듈 카드 라벨만 옮긴다. 프로필 배치·passkey/2FA 카드·메뉴 순서는 upstream의 `/security` 구조와 맞지 않아 뺀다.
6. 빠른 시작 PDF는 "Create API Key" 버튼을 대체하지 않고 옆에 더한다(team은 대체했음).
7. PDF 두 개(각 약 550KB)는 저장소에 바이너리로 들어간다. 내용이 upstream 새 UI와 맞는지는 운영자가 확인한다(기록에 남긴다).
