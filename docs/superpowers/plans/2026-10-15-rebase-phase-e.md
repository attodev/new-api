# Upstream Rebase 단계 E 구현 계획 — O11 fork 고유 수정과 단계 D 이월

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** team이 2026-10에 고친 fork 고유 수정 중 upstream 베이스에 아직 없는 세 가지(Gemini 이미지 토큰 이중 과금, Ali `thinking_budget` 소실, 출석·추천 적립의 지갑 상한)를 `develop`에 다시 올리고, 단계 D가 넘긴 한국어 공백(AI 클라이언트로 가는 조직 오류, 감사 로그, Excel 가져오기 행 오류)과 소형 항목을 닫는다.

**Architecture:** O11 조사 결과 Epay 트랜잭션 정산과 결제 전 수용량 검증은 upstream이 이미 같은 방식으로 고쳤으므로 다시 적용하지 않는다(스펙 §2.2). 남은 수정은 모두 "upstream 파일에 훅 한 줄 + fork 파일" 규칙으로 넣는다. Gemini 상한은 relaykit을 건드리지 않고(규칙 6) 호스트 정산 진입점 `effectiveBillingUsage`에서, Ali는 Ali 어댑터 안에서 한다. 한국어 이월은 기존 메커니즘(`i18n.T` + yaml, `AUDIT_TEMPLATES`, `common.Message` 키)을 쓴다. team 브랜치의 `TimingResponseWriter.Unwrap()` 결함은 team 기준의 별도 브랜치에 커밋만 하고 병합·푸시하지 않는다.

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, GORM(SQLite/MySQL/PostgreSQL), go-i18n v2, sjson/gjson, React 19 + i18next, vitest.

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §3(규칙 1–6), §4.9(O11), §8 단계 E. 이월 출처: `docs/2026-10-15-rebase-phase-d.md` §6, `docs/2026-10-14-rebase-phase-c.md` §5. team 출처: 커밋 `af1eddc8e`, `c20c6d664`, `afbf87cd3`, `70b14d049`, `2ba5cc513`, 기록 `git show team:docs/2026-10-13-upstream-sync-phase2.md`, `git show team:docs/2026-10-14-followups-2a.md`.

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`. `/Users/molla/work/ai/new-api`(team 체크아웃)는 읽기 전용이다. team 소스는 `git show team:<path>`/`git show <sha>`로 읽는다. 예외는 Task 8 하나로, team 기준 별도 worktree에서만 작업한다.
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal/DecodeJson*`만 쓴다. SQLite/MySQL/PostgreSQL을 동시에 지원하며, GORM 추상화를 우선하고 raw SQL에서는 `commonGroupCol`·`commonTrueVal`을 쓴다.
- upstream 파일 수정은 훅 한 줄(또는 상수·필드·map 항목 한 줄)로 제한하고 `// fork: O11` / `// fork: O3` / `// fork: O9` / `// fork: O8` 주석을 단다(yaml은 `# fork: O9`). 로직은 fork 파일에 둔다. 모든 훅은 `docs/fork-overlays.md`에 `파일:줄` 행으로 기록한다.
- relaykit(`relaykit/`)은 upstream 그대로 둔다(규칙 6). fork 수정이 필요하면 호스트 어댑터·서비스 쪽에서 한다.
- 한국어 문자열은 `docs/translation-glossary.ko.md`의 용어와 문체를 따른다. 새 프런트 키는 8개 locale 모두에 넣는다: en은 키와 같은 값, ko는 번역, 나머지 6개는 영어 placeholder. 넣은 뒤 `cd web && node scripts/ko-locale.mjs check`가 전부 ok이고 `bun run test -- ko-coverage`가 통과해야 한다.
- `web/src`의 새 TS/TSX 파일은 1행에 AGPL 헤더를 둔다. 손댄 파일마다 `bunx oxlint -c .oxlintrc.json ./src/...`와 `bunx oxfmt --check ./src/...`에서 새 문제가 없어야 한다. `bun run format`은 쓰지 않는다.
- 커밋 트레일러: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. team에서 옮긴 커밋은 `Ported-From: team <sha>`를 추가한다.
- 테스트는 SQLite in-memory(각 패키지의 기존 `TestMain`)로 한다. MySQL/PostgreSQL DSN 테스트와 결제 provider sandbox는 운영자 작업으로 기록한다.

## Review Focus

1. **캐시가 대부분인 Gemini 이미지 요청:** 이미지 토큰이 캐시·오디오와 겹치면 이미지 비율로 두 번 청구되면 안 된다. 예: P=2000, cached=1500, IMAGE 1089, ImageRatio 2 → 1575(지금 develop은 2753). → Task 1 `TestEffectiveBillingUsageCapsGeminiImageByCachedAndAudio`와 quota 테스트.
2. **Qwen 클라이언트 모델을 커스텀 배포 이름으로 매핑한 채널:** `thinking_budget: 0`(생각 끄기)이 upstream 본문에 남아야 한다. 0이 사라지면 Qwen이 기본 예산으로 생각해 비용과 지연이 늘어난다. → Task 2 `TestAliDoRequestKeepsThinkingBudgetForQwenClientMappedToCustomName`.
3. **지갑 상한 근처 사용자의 출석:** 보상이 거부되면 그날 출석 기록도 남지 않아야 한다(다시 시도 가능). MySQL/PostgreSQL 경로(트랜잭션)를 SQLite에서도 직접 실행해 확인한다. → Task 3 `TestUserCheckinWithTransactionAtWalletCeilingRollsBack`.
4. **비활성 조직 회원의 API 호출(Accept-Language: ko):** AI 클라이언트가 받는 403 본문이 한국어여야 한다(지금은 영어 고정). → Task 4 `TestOrganizationRelayErrorsAreTranslated`.
5. **조직 관리자가 깨진 Excel을 가져올 때(ko):** 행 오류가 한국어로, 줄 번호·사용자 이름과 함께 보여야 한다. 옛 서버 응답(문자열 `errors`만 있음)에도 화면이 깨지면 안 된다. → Task 6 `org-users-import-dialog.test.tsx` "renders translated row errors and falls back to plain errors".

---

## 파일 구조

| 영역 | 생성(fork) | 수정(훅) |
|---|---|---|
| Gemini 상한 | `service/gemini_image_cap_fork.go`, `service/gemini_image_cap_fork_test.go` | `service/billing_usage.go:19-24` |
| Ali 예산 | `relay/channel/ali/thinking_budget_fork.go`, `relay/channel/ali/thinking_budget_fork_test.go` | `relay/channel/ali/text.go:12-19`, `relay/channel/ali/adaptor.go:146,185-187`, `relay/channel/ali/adaptor_test.go:22-60` |
| 지갑 상한 | `model/wallet_ceiling_fork.go`, `model/wallet_ceiling_fork_test.go`, `model/topup_target_epay_test.go` | `model/checkin.go:103-104,133-136`, `model/user.go:617-618` |
| relay 조직 오류 | `i18n/keys_fork.go`, `service/organization_relay_errors_fork_test.go` | `i18n/locales/{en,zh-CN,zh-TW}.yaml`(끝에 fork 블록), `i18n/locales/ko.yaml`, `service/billing_session_organization.go`, `service/midjourney_organization.go`, `service/quota_organization.go` |
| 감사 로그 | `web/src/features/usage-logs/lib/fork-audit-templates.ts`, `web/src/features/usage-logs/__tests__/fork-audit-templates.test.ts` | `web/src/features/usage-logs/lib/format.ts:451`(spread 한 줄), 8개 locale |
| Excel 행 오류 | `service/org_import_errors_fork.go` | `service/org_user_export.go`(fork 파일 전체), `web/src/features/organizations/{types.ts,components/org-users-import-dialog.tsx}`, 8개 locale |
| 옵션 검증 | `common/interface_languages_fork_test.go`(추가 케이스) | `model/option.go:619-620`(InterfaceLanguages case) |
| team 역이식 | (team 기준 브랜치 `fix/team-timing-writer-unwrap`) | team `common/timing_response_writer.go`, `common/timing_response_writer_test.go` |
| 기록 | `docs/2026-10-15-rebase-phase-e.md` | `docs/fork-overlays.md`, spec §4.9·§9 |

---

### Task 1: Gemini 이미지 토큰 상한(캐시·오디오와 겹치는 부분)

**Files:**
- Create: `service/gemini_image_cap_fork.go`, `service/gemini_image_cap_fork_test.go`
- Modify: `service/billing_usage.go:19-24`(`effectiveBillingUsage`)

**Interfaces:**
- Consumes: `usageFromBillingUsage(usage) (*dto.Usage, bool)`(`service/billing_usage.go:63`, `BillingUsage.CanonicalUsage()`가 매번 새 `Usage`를 만든다), `dto.BillingUsageSemanticGemini`, `calculateTextQuotaSummary(ctx, relayInfo, usage) textQuotaSummary`(`service/text_quota.go:223`).
- Produces: `capGeminiImagePromptTokens(usage *dto.Usage) *dto.Usage`. Gemini 의미의 canonical usage일 때만 `ImageTokens`를 `max(PromptTokens - CachedTokens - AudioTokens, 0)`로 자르고, 잘라 낸 만큼을 `TextTokens`에 돌려 detail 합이 `PromptTokens`가 되게 한다. 그 밖의 usage는 그대로 돌려준다.

- [ ] **Step 1: 실패하는 테스트**

`service/gemini_image_cap_fork_test.go`(package `service`). `dto.GeminiUsageMetadata`의 실제 필드 이름(`PromptTokenCount`, `CachedContentTokenCount`, `CandidatesTokenCount`, `PromptTokensDetails []{Modality, TokenCount}`)은 `relaykit/dto`에서 확인해 맞춘다.

```go
func geminiUsage(prompt, cached, completion int, details map[string]int) *dto.Usage {
	md := &dto.GeminiUsageMetadata{PromptTokenCount: prompt, CachedContentTokenCount: cached, CandidatesTokenCount: completion, TotalTokenCount: prompt + completion}
	for modality, n := range details {
		md.PromptTokensDetails = append(md.PromptTokensDetails, dto.GeminiPromptTokensDetail{Modality: modality, TokenCount: n})
	}
	u := &dto.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	u.BillingUsage = dto.NewGeminiChatBillingUsage(md)
	return u
}

func TestEffectiveBillingUsageCapsGeminiImageByCachedAndAudio(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 1500, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 500, u.PromptTokensDetails.ImageTokens)
	require.Equal(t, 2000, u.PromptTokensDetails.ImageTokens+u.PromptTokensDetails.TextTokens+u.PromptTokensDetails.AudioTokens)

	u = effectiveBillingUsage(geminiUsage(478, 300, 20, map[string]int{"TEXT": 100, "IMAGE": 258, "AUDIO": 50, "VIDEO": 30, "DOCUMENT": 40}))
	require.Equal(t, 128, u.PromptTokensDetails.ImageTokens) // 478-300-50
	require.Equal(t, 50, u.PromptTokensDetails.AudioTokens)
}

func TestEffectiveBillingUsageLeavesUncappedGeminiAndNonGeminiAlone(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 0, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 1089, u.PromptTokensDetails.ImageTokens)

	plain := &dto.Usage{PromptTokens: 100, CompletionTokens: 10}
	plain.PromptTokensDetails.ImageTokens = 90
	plain.PromptTokensDetails.CachedTokens = 50
	require.Same(t, plain, effectiveBillingUsage(plain))
	require.Equal(t, 90, plain.PromptTokensDetails.ImageTokens)
}
```

quota 테스트는 team `service/text_quota_test.go`의 `TestCalculateTextQuotaSummaryGeminiCachedImagePromptIsNotOvercharged`와 `...GeminiImageAudioCachedPromptIsNotOvercharged`를 옮긴다(`git show team:service/text_quota_test.go | sed -n 969,1080p`). 이때 usage는 위 `geminiUsage`로 만들고 `effectiveBillingUsage`를 거친 값을 `calculateTextQuotaSummary`에 넣는다. 기대값은 team 그대로다: 상한 없는 전부 텍스트 1075, 상한 적용 이미지(ImageRatio 2) 1575, 이미지+오디오+캐시 1200과 상한 없는 1500. 실행: `GOWORK=off go test ./service/ -run 'GeminiImage|EffectiveBillingUsage' -v`. 지금은 FAIL이어야 한다(ImageTokens 1089, quota 2753 등).

- [ ] **Step 2: 구현**

`service/gemini_image_cap_fork.go`:

```go
package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// capGeminiImagePromptTokens keeps Gemini image prompt tokens out of the
// cached and audio portions. Gemini promptTokensDetails include cached tokens,
// and text settlement bills base = prompt - cached - image - audio plus
// cached*cacheRatio + image*imageRatio + audio price, so image tokens that
// overlap the cached or audio portions would be billed twice. The capped-off
// amount returns to TextTokens so the details still sum to PromptTokens.
// Ported from team af1eddc8e/c20c6d664 into the host (relaykit stays upstream).
func capGeminiImagePromptTokens(usage *dto.Usage) *dto.Usage {
	if usage == nil || usage.BillingUsage == nil || usage.BillingUsage.Semantic != dto.BillingUsageSemanticGemini {
		return usage
	}
	d := &usage.PromptTokensDetails
	maxImage := max(usage.PromptTokens-d.CachedTokens-d.AudioTokens, 0)
	if d.ImageTokens > maxImage {
		d.TextTokens += d.ImageTokens - maxImage
		d.ImageTokens = maxImage
	}
	return usage
}
```

`CanonicalUsage()`가 돌려준 `Usage`에 `BillingUsage`가 실려 있는지 확인한다(`relaykit/dto/billing_usage.go`의 canonical 함수). 실려 있지 않으면 `effectiveBillingUsage` 안에서 원래 `usage.BillingUsage.Semantic`으로 판정하도록 helper 시그니처를 `capGeminiImagePromptTokens(canonical *dto.Usage, semantic string)`으로 바꾸고 테스트도 맞춘다. 훅은 `service/billing_usage.go:19-24`에 둔다.

```go
func effectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return capGeminiImagePromptTokens(billingUsage) // fork: O11
	}
	return usage
}
```

실행: Step 1 테스트 PASS, `GOWORK=off go test ./service/... ./relay/... -count=1`. `PostAudioConsumeQuota` 경로(raw usage)는 이 상한을 거치지 않는다. 기록 §에 남긴다.

- [ ] **Step 3: 커밋**

```bash
git add service/gemini_image_cap_fork.go service/gemini_image_cap_fork_test.go service/billing_usage.go docs/fork-overlays.md
git commit -m "fix(gemini): cap image prompt tokens by the cached and audio portions

Ported-From: team af1eddc8e c20c6d664
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Ali `thinking_budget` either-name 규칙

**Files:**
- Create: `relay/channel/ali/thinking_budget_fork.go`, `relay/channel/ali/thinking_budget_fork_test.go`
- Modify: `relay/channel/ali/text.go:12-19`, `relay/channel/ali/adaptor.go:146`(변환 호출), `:185-187`(`DoRequest`), `relay/channel/ali/adaptor_test.go:22-60`

**Interfaces:**
- Consumes: `dto.IsQwenThinkingBudgetModel(name string) bool`, `info.OriginModelName`, `info.UpstreamModelName`, `channel.DoApiRequest(a, c, info, body)`, `relaycommon.NewOutboundJSONBody`(본문 재구성. 실제 이름은 `relay/common`에서 확인한다).
- Produces: `aliKeepsThinkingBudget(info *relaycommon.RelayInfo) bool`; `stashAliThinkingBudget(c *gin.Context, raw json.RawMessage)`; `doAliRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error)`. 이 함수는 stash가 있으면 본문에 `thinking_budget`을 원래 raw 값 그대로 다시 넣는다. `requestOpenAI2Ali(request, keepThinkingBudget bool)`는 계속 `*dto.GeneralOpenAIRequest`를 돌려준다(`relay/compatible_handler.go`가 이 타입을 단언한다).

배경: develop은 두 곳에서 예산을 지운다. `requestOpenAI2Ali`(upstream 이름 기준)와 `GeneralOpenAIRequest.MarshalJSON`(매핑 뒤의 `r.Model` 기준, relaykit)이다. relaykit은 고칠 수 없으므로(규칙 6) 마샬 뒤 Ali 어댑터의 `DoRequest`에서 다시 넣는다. 그 결과 `ParamOverride`와 `RemoveDisabledFields`는 다시 넣은 필드를 보지 못한다(기록에 남긴다).

- [ ] **Step 1: 실패하는 테스트**

`thinking_budget_fork_test.go`: `httptest.NewServer`로 upstream을 흉내 내고 받은 본문을 기록한다. 요청은 `{"model":"qwen3-max","messages":[{"role":"user","content":"hi"}],"enable_thinking":true,"thinking_budget":0}`이고 채널 매핑은 `{"qwen3-max":"my-qwen-deploy"}`다. 컨텍스트에 `model_mapping`을 넣고 `relayhelper.ModelMappedHelper`를 거친다. `ConvertOpenAIRequest` → `common.Marshal` → `(&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))`를 실행한다(`info.ChannelMeta`의 BaseURL을 테스트 서버로. `relay/channel/api_request_redirect_test.go:19`의 하네스를 참고한다). 단언: 서버가 받은 본문의 `model`이 `my-qwen-deploy`, `thinking_budget`이 존재하고 값이 정확히 `0`, `enable_thinking`이 true. 두 번째 테스트: 클라이언트 `deepseek-v3`, `thinking_budget:100` → 본문에 `thinking_budget`이 없다. 세 번째: 클라이언트 `qwen-plus` → upstream `deepseek-r1`(매핑) → 128이 남는다(team 케이스 "qwen client mapped to unsupported upstream keeps budget"). `adaptor_test.go:22`의 기존 케이스 "unsupported upstream overrides qwen request"(`wantBudget: false`)는 새 규칙과 충돌하므로 `wantBudget: true, wantValue: 128`로 바꾸고 이름을 "qwen client mapped to unsupported upstream keeps budget"으로 바꾼다. 테스트 함수 이름도 `TestConvertOpenAIRequestKeepsThinkingBudgetWhenEitherNameIsQwen`으로 바꾼다. 실행: `GOWORK=off go test ./relay/channel/ali/ -run ThinkingBudget -v`. 지금은 FAIL이어야 한다.

- [ ] **Step 2: 구현**

`thinking_budget_fork.go`:

```go
package ali

const aliThinkingBudgetContextKey = "fork_ali_thinking_budget"

// aliKeepsThinkingBudget is the team either-name rule (70b14d049): a request
// keeps thinking_budget when the client model or the mapped upstream model is
// a Qwen/QwQ model, so a Qwen client mapped to a custom deployment name keeps
// an explicit budget (including 0, which turns thinking off).
func aliKeepsThinkingBudget(info *relaycommon.RelayInfo) bool {
	return dto.IsQwenThinkingBudgetModel(info.OriginModelName) || dto.IsQwenThinkingBudgetModel(info.UpstreamModelName)
}

func stashAliThinkingBudget(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) bool {
	keep := aliKeepsThinkingBudget(info)
	if keep && request.ThinkingBudget != nil {
		raw, err := common.Marshal(request.ThinkingBudget)
		if err == nil {
			c.Set(aliThinkingBudgetContextKey, json.RawMessage(raw))
		}
	}
	return keep
}

// doAliRequest re-inserts a stashed thinking_budget after the generic request
// marshaler (relaykit, upstream) dropped it by the mapped model name.
func doAliRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	raw, ok := c.Get(aliThinkingBudgetContextKey)
	if !ok {
		return channel.DoApiRequest(a, c, info, body)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(data, "thinking_budget").Exists() {
		if data, err = sjson.SetRawBytes(data, "thinking_budget", raw.(json.RawMessage)); err != nil {
			return nil, err
		}
	}
	return channel.DoApiRequest(a, c, info, bytes.NewReader(data))
}
```

`ThinkingBudget`의 실제 타입(포인터 정수 등)에 맞춰 마샬한다. 0이 사라지면 안 된다. 본문 재구성에 upstream이 쓰는 helper(`NewOutboundJSONBody` 등)가 있으면 그것을 쓴다(`relay/channel/api_request.go`에서 `requestBody`를 어떻게 다루는지 확인하고, 길이 헤더 등이 필요하면 그 helper로 맞춘다). `text.go:12`: 시그니처를 `requestOpenAI2Ali(request dto.GeneralOpenAIRequest, keepThinkingBudget bool) *dto.GeneralOpenAIRequest // fork: O11`로 바꾸고 본문의 판정을 `if !keepThinkingBudget { request.ThinkingBudget = nil }`로 바꾼다. 줄 수가 늘지 않게 기존 `modelName` 블록을 대체한다. 다른 호출부가 있으면 함께 바꾼다(`grep -rn requestOpenAI2Ali relay/`). `adaptor.go:146`: `aliReq := requestOpenAI2Ali(*request, stashAliThinkingBudget(c, info, request)) // fork: O11`. `adaptor.go:185-187`의 `DoRequest` 본문은 `return doAliRequest(a, c, info, requestBody) // fork: O11` 한 줄로 바꾼다. 실행: Step 1 PASS, `GOWORK=off go test ./relay/... -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add relay/channel/ali docs/fork-overlays.md
git commit -m "fix(ali): keep thinking_budget when either the client or the upstream model is Qwen

Ported-From: team afbf87cd3 70b14d049
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: 출석·추천 적립의 지갑 상한, 조직 Epay 롤백 테스트

**Files:**
- Create: `model/wallet_ceiling_fork.go`, `model/wallet_ceiling_fork_test.go`, `model/topup_target_epay_test.go`
- Modify: `model/checkin.go:103-104`(트랜잭션 경로), `:133-136`(SQLite 경로), `model/user.go:617-618`(`TransferAffQuotaToQuota`)

**Interfaces:**
- Consumes: `creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]any) error`(`model/topup.go:95`, 원자적 `quota <= max` UPDATE), `ErrTopUpQuotaLimitExceeded`(`model/topup.go:56`), `common.MaxWalletQuota`.
- Produces: `creditCheckinReward(tx *gorm.DB, userId, quota int) error`. `quota <= 0`이면 nil을 돌려주고, 상한 초과면 `ErrCheckinWalletLimit = common.NewMessage("Check-in failed: wallet quota limit reached")`, 그 밖의 오류면 `common.NewMessage("Check-in failed: quota update error")`를 돌려준다. `ErrAffTransferWalletLimit = common.NewMessage("Invitation quota transfer failed: wallet quota limit reached")`도 만든다.

- [ ] **Step 1: 실패하는 테스트**

team `git show team:model/checkin_quota_test.go`의 세 테스트를 옮긴다(`TestUserCheckinCreditsReward`, `TestUserCheckinAtWalletCeilingIsRejectedAndNotRecorded`, `TestUserCheckinWithTransactionAtWalletCeilingRollsBack`). `common.MaxQuota`는 `common.MaxWalletQuota`로, `insertUserWithQuota`는 develop helper `insertUserForPaymentGuardTest(t, id, quota)`(`model/payment_method_guard_test.go:12`)로 바꾸고, `resetUserQuotaBatchState`가 없으면 빼거나 develop의 batch 비활성 방식을 따른다. 기대 메시지는 `Check-in failed: wallet quota limit reached`(develop의 NewMessage 형식)로 한다. 추가 테스트: `TestTransferAffQuotaToQuotaRespectsWalletCeiling`(quota `MaxWalletQuota-10`, aff 1,000,000 → 이전 시도는 `ErrAffTransferWalletLimit`이고 quota와 aff_quota는 변하지 않는다). `topup_target_epay_test.go`의 `TestRechargeEpayOrganizationOrderStaysPendingWhenWalletFull`: 조직 쿼터가 `MaxWalletQuota-10`인 조직의 epay 주문 → `RechargeEpay` 오류, 주문은 pending, 조직 쿼터는 그대로. 조직·주문 생성은 `controller/organization_payment_test.go`나 `model/topup_target_test.go`의 fixture를 참고한다. 실행: `GOWORK=off go test ./model/ -run 'Checkin|AffQuota|RechargeEpayOrganization' -v`. 트랜잭션 경로 테스트와 aff 테스트는 지금 FAIL이어야 하고, Epay 테스트는 이미 PASS일 수 있다(회귀 고정용).

- [ ] **Step 2: 구현**

`model/wallet_ceiling_fork.go`:

```go
package model

var (
	ErrCheckinWalletLimit     = common.NewMessage("Check-in failed: wallet quota limit reached")
	errCheckinQuotaUpdate     = common.NewMessage("Check-in failed: quota update error")
	ErrAffTransferWalletLimit = common.NewMessage("Invitation quota transfer failed: wallet quota limit reached")
)

// creditCheckinReward credits a check-in reward within the wallet ceiling in
// the check-in transaction, so a rejected reward rolls the check-in row back
// (team 2ba5cc513; upstream checkin.go credits without a ceiling).
func creditCheckinReward(tx *gorm.DB, userId, quota int) error {
	if quota <= 0 {
		return nil
	}
	if err := creditTopUpQuota(tx, userId, quota, nil); err != nil {
		if errors.Is(err, ErrTopUpQuotaLimitExceeded) {
			return ErrCheckinWalletLimit
		}
		return errCheckinQuotaUpdate
	}
	return nil
}
```

`checkin.go:103-104`의 두 줄(`tx.Model(&User{})… Update(...)` + 오류 반환)을 `if err := creditCheckinReward(tx, userId, quotaAwarded); err != nil { return err } // fork: O11` 한 블록으로 바꾼다. gofmt가 세 줄로 펼치면 첫 줄에 marker를 둔다. SQLite 경로(`:133-136`)에서는 `IncreaseUserQuota` 실패 시 반환하는 `common.NewMessage("Check-in failed: quota update error")`를 `checkinQuotaErrorFor(err)`(fork, 상한 오류를 구분)로 바꾼다. 더 간단하게는 SQLite 경로도 `DB.Transaction(func(tx) { create; creditCheckinReward })`로 바꾸는 fork 함수를 부르는 방법이 있다(기존 함수 본문을 fork 함수 호출 한 줄로 대체). 둘 중 hunk가 작은 쪽을 고르고 보고서에 적는다. `user.go:617-618`(`user.AffQuota -= quota; user.Quota += quota`) 바로 앞에 `if err := checkAffTransferCeiling(user.Quota, quota); err != nil { return err } // fork: O11`를 넣는다(fork 함수는 `quota > common.MaxWalletQuota - user.Quota`면 `ErrAffTransferWalletLimit`). 새 메시지 키 3개를 프런트 locale 8개에 넣는다(ko 번역). Phase D Task 9의 `controller/organization_i18n_test.go` `forkMessageSources`에 `model/wallet_ceiling_fork.go`를 추가한다. 실행: Step 1 PASS, `GOWORK=off go test ./model/ ./controller/ -count=1`, `cd web && node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋**

```bash
git add model web/src/i18n/locales controller/organization_i18n_test.go docs/fork-overlays.md
git commit -m "fix(quota): keep check-in and invitation transfers within the wallet ceiling

Ported-From: team 2ba5cc513
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: AI 클라이언트로 가는 조직 오류의 번역

**Files:**
- Create: `i18n/keys_fork.go`, `service/organization_relay_errors_fork_test.go`
- Modify: `i18n/locales/en.yaml`, `zh-CN.yaml`, `zh-TW.yaml`(파일 끝에 `# fork: O3` 블록), `i18n/locales/ko.yaml`, `service/billing_session_organization.go`(오류 생성 지점), `service/midjourney_organization.go:38,45,69`, `service/quota_organization.go`

**Interfaces:**
- Consumes: `i18n.T(c *gin.Context, key string, args ...map[string]any) string`(`i18n/i18n.go:93`), 기존 패턴 `errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{...}))`(`service/billing_session.go:244`).
- Produces: `i18n.MsgOrganizationDisabled = "organization.disabled"`, `MsgOrganizationMemberSuspended = "organization.member_suspended"`, `MsgOrganizationWalletInsufficient = "organization.wallet_insufficient"`(param `Error`), `MsgOrganizationMemberQuotaInsufficient = "organization.member_quota_insufficient"`(param `Error`), `MsgOrganizationMjPlanUnsupported = "organization.mj_plan_unsupported"`, `MsgOrganizationQuotaInsufficient = "organization.quota_insufficient"`.

- [ ] **Step 1: 실패하는 테스트**

`service/organization_relay_errors_fork_test.go`: `Accept-Language: ko` 컨텍스트에서 비활성 조직 회원의 `NewBillingSession`(Phase B·C의 `service/billing_session_organization_test.go` fixture를 쓴다)이 돌려준 `*types.NewAPIError`의 메시지가 `ko.yaml`의 `organization.disabled` 값과 같다. 정지 회원과 조직 지갑 부족도 같은 방식으로 확인한다. `Accept-Language` 없음 → 영어 원문(기존 테스트 문구 유지). MJ: `CheckMidjourneyOrganizationBilling`은 `userId`만 받으므로 c가 없다. 시그니처를 `(c *gin.Context, userId int)`로 바꿀지(호출부 `relay/mjproxy_handler.go:229` fork 훅 한 줄도 같이 바꾼다), 영어로 둘지 정한다. 이 계획은 c를 넘기기로 한다. 실행: `GOWORK=off go test ./service/ -run OrganizationRelayErrors -v`. 지금은 FAIL이어야 한다.

- [ ] **Step 2: 구현**

`i18n/keys_fork.go`에 위 상수를 둔다. 각 yaml 끝에 `# fork: O3 — organization relay errors` 주석과 6개 키를 둔다. 값: en은 현재 영어 원문(`organization is disabled`, `organization membership is suspended`, `organization wallet quota insufficient: {{.Error}}`, `member quota insufficient: {{.Error}}`, `legacy Midjourney billing does not support organization plans`, `organization quota insufficient`). zh-CN과 zh-TW는 정확한 번역(예: `组织已停用` / `組織已停用`)을 넣고, 자신 없는 값은 영어로 두고 보고서에 적는다. ko는 용어집으로 번역한다(`조직이 비활성화되었습니다`, `조직 멤버십이 일시 정지되었습니다`, `조직 지갑 쿼터가 부족합니다: {{.Error}}` 등). 생성 지점은 `types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgOrganizationDisabled)), …)`처럼 바꾼다. `errors.Is(err, model.ErrOrganizationDisabled)` 판정은 그대로 두고, 클라이언트로 나가는 메시지만 번역한다. `TestKoreanYamlCoversEveryEnglishKey`가 ko 키 누락을 잡는다. 실행: Step 1 PASS, `GOWORK=off go test ./i18n/ ./service/... ./relay/ ./controller/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add i18n service relay/mjproxy_handler.go docs/fork-overlays.md
git commit -m "feat(i18n): translate organization errors returned to AI clients

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: fork 감사 로그 템플릿

**Files:**
- Create: `web/src/features/usage-logs/lib/fork-audit-templates.ts`, `web/src/features/usage-logs/__tests__/fork-audit-templates.test.ts`
- Modify: `web/src/features/usage-logs/lib/format.ts:451`(`AUDIT_TEMPLATES` 객체 안 spread 한 줄), 8개 locale

**Interfaces:**
- Consumes: `renderAuditContent(other, t)`(`format.ts:602-639`: `AUDIT_TEMPLATES[op.action]`이 있으면 `t(template, op.params)`), 감사 params(각 `recordOrganizationAudit` 호출부의 `organizationAuditParams(base, updates)`, `controller/topup_expire_admin.go:44-50`의 `user_id/expired/skipped_recent/expired_subscriptions/...`).
- Produces: `FORK_AUDIT_TEMPLATES: Record<string, string>`. 14개 액션: `organization.create`, `organization.update`, `organization.delete`, `organization.user.assign`, `organization.user.update`, `organization.user.remove`, `organization.user.import`, `organization.plan.create`, `organization.plan.update`, `organization.plan.disable`, `organization.subscription.assign`, `organization.subscription.cancel`, `organization.topup.expire`, `topup.expire`.

- [ ] **Step 1: 실패하는 테스트**

각 액션의 실제 params 키를 Go 호출부에서 읽는다(`grep -n 'recordOrganizationAudit(c, "' -A 4 controller/*.go`). 템플릿은 그 키만 보간한다(예: `organization.user.update` → `'Updated member {{user_id}} of organization {{organization_id}}'`, `topup.expire` → `'Expired {{expired}} pending top-ups and {{expired_subscriptions}} subscription orders of user {{user_id}}'`). 테스트는 각 액션마다 실제 params 모양으로 `renderAuditContent({op:{action, params}}, t)`를 부르고 결과가 null이 아니며 `{{`가 남지 않음(보간 키 누락 없음)을 단언한다. ko로 바꾼 i18n에서 한 액션이 한국어로 렌더되는지도 확인한다(Phase D `server-error-message.test.ts`의 언어 전환 방식을 따른다). 실행: `cd web && bun run test -- fork-audit-templates` → FAIL.

- [ ] **Step 2: 구현**

`fork-audit-templates.ts`(AGPL 헤더)에 맵을 둔다. `format.ts:451` 객체 첫 줄 다음에 `...FORK_AUDIT_TEMPLATES, // fork: O3` 한 줄과 import 한 줄을 넣는다. 14개 템플릿 문자열을 8개 locale에 넣는다(ko는 용어집으로 번역: 조직, 멤버, 플랜, 할당, 일시 정지, 만료). 실행: Step 1 PASS, `bun run test -- usage-logs`, typecheck, 파일별 oxlint/oxfmt, `node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋**

```bash
git add web/src docs/fork-overlays.md
git commit -m "feat(web): render organization and top-up expiry audit entries as translated sentences

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Excel 조직 가져오기 행 오류의 구조화와 번역

**Files:**
- Create: `service/org_import_errors_fork.go`
- Modify: `service/org_user_export.go`(fork 파일, `OrgImportResult`와 오류 추가 지점 약 25곳), `web/src/features/organizations/types.ts:113`, `web/src/features/organizations/components/org-users-import-dialog.tsx:155-165`, `web/src/features/organizations/__tests__/org-users-import-dialog.test.tsx`(없으면 생성), 8개 locale

**Interfaces:**
- Produces: Go `type OrgImportRowError struct { Line int \`json:"line"\`; Username string \`json:"username,omitempty"\`; MessageKey string \`json:"message_key"\`; Params map[string]any \`json:"params,omitempty"\` }`; `OrgImportResult.ErrorDetails []OrgImportRowError \`json:"error_details"\``. 기존 `Errors []string`은 영어 문자열로 그대로 남겨 호환을 유지한다. helper `(r *OrgImportResult) addRowError(line int, username, key string, params map[string]any)`는 `Errors`(영어, 기존 형식 `line N (user): …`)와 `ErrorDetails`에 함께 추가한다. 중첩된 `%v` 오류(`err.Error()`)는 `params["error"]`에 문자열로 넣는다(번역하지 않음). 그 오류가 `*common.Message`면 `params["error_key"]`/`params["error_params"]`도 넣어 프런트가 번역할 수 있게 한다. TS `OrgImportRowError`, `OrgImportResult.error_details?: OrgImportRowError[]`.

- [ ] **Step 1: 실패하는 테스트**

Go: `service/org_user_export_test.go`에 "빈 username 행과 잘못된 역할 행 → `ErrorDetails`에 line·username·message_key(`username is empty`, `invalid organization_role {{role}} (only member or admin can be imported)`)가 들어가고 `Errors` 문자열은 기존과 같다"를 추가한다. 프런트: 다이얼로그 테스트 "renders translated row errors and falls back to plain errors". `error_details`가 있으면 `t('Line {{line}}', …)`, 사용자 이름, `t(message_key, params)`로 그리고, `error_details`가 없는 옛 응답이면 `errors` 문자열을 그대로 그린다. ko에서 한 행이 한국어로 나오는지도 확인한다. 실행 → FAIL.

- [ ] **Step 2: 구현**

`org_user_export.go`의 `result.Errors = append(result.Errors, fmt.Sprintf(...))` 지점을 모두 `result.addRowError(lineNum, username, "<영어 템플릿>", params)`로 바꾼다. 영어 템플릿은 `{{name}}` 보간 형식이다. 같은 의미의 기존 문장은 같은 키를 쓴다(예: `failed to set quota: {{error}}`). remove-absent 오류는 line 0이다. 새 키(템플릿 약 18개 + `Line {{line}}`)를 8개 locale에 넣고 ko는 번역한다. 다이얼로그는 `error_details ?? []`로 그리고 없으면 `errors`로 fallback한다. 실행: Step 1 PASS, `GOWORK=off go test ./service/ ./controller/ -count=1`, `cd web && bun run test -- organizations`, typecheck, oxlint/oxfmt, `node scripts/ko-locale.mjs check`. `controller/organization_i18n_test.go`의 키 검사가 `addRowError` 키도 보도록 정규식에 `addRowError\([^,]+, [^,]+, "…"`를 추가한다.

- [ ] **Step 3: 커밋**

```bash
git add service web/src controller/organization_i18n_test.go docs/fork-overlays.md
git commit -m "feat(organization): return structured import row errors and render them translated

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: `InterfaceLanguages` 쓰기 시점 정규화

**Files:**
- Modify: `model/option.go:619-620`(`case "InterfaceLanguages":`), `common/interface_languages_fork.go`, `common/interface_languages_fork_test.go`

**Interfaces:**
- Produces: `common.NormalizeInterfaceLanguagesOption(value string) string`. `ParseInterfaceLanguages`의 결과를 쉼표로 이어 저장 형식으로 돌려준다. 빈 결과는 `"en"`.

- [ ] **Step 1: 테스트(실패)** — `require.Equal(t, "ko,en", NormalizeInterfaceLanguagesOption(" kr , EN, xx "))`, `require.Equal(t, "en", NormalizeInterfaceLanguagesOption("xx"))`. model 테스트: `UpdateOption("InterfaceLanguages", " kr ,xx")` 후 `common.OptionMap["InterfaceLanguages"] == "ko"`이고 DB에 저장된 값도 `"ko"`다(`model/option.go`의 `UpdateOption`이 DB와 map에 쓰는 순서를 읽고, 정규화를 DB 쓰기 전에 적용해야 하면 `updateOptionMap` 대신 `UpdateOption` 앞의 검증 지점에 훅을 둔다. `validateOptionValue`(`model/option.go:238`)가 그 자리다).
- [ ] **Step 2: 구현** — 정규화를 DB 저장 전에 적용하는 한 줄 훅 `value = common.NormalizeInterfaceLanguagesOption(value) // fork: O8`(위치는 Step 1에서 정한 곳). 실행: `GOWORK=off go test ./common/ ./model/ -run Interface -count=1`.
- [ ] **Step 3: 커밋** `fix(settings): store InterfaceLanguages normalized` (트레일러 동일).

---

### Task 8: team 브랜치에 `TimingResponseWriter.Unwrap()` 역이식(별도 브랜치)

**Files (team 기준 worktree):**
- Modify: `common/timing_response_writer.go`(team), `common/timing_response_writer_test.go`(team)

**Interfaces:**
- Produces: 로컬 브랜치 `fix/team-timing-writer-unwrap`(team HEAD 기준), worktree `/Users/molla/work/ai/new-api-team-fix`, 커밋 1개. **병합·푸시하지 않는다.**

- [ ] **Step 1: worktree** — `git -C /Users/molla/work/ai/new-api-rebase worktree add /Users/molla/work/ai/new-api-team-fix -b fix/team-timing-writer-unwrap team`. `/Users/molla/work/ai/new-api`(team 체크아웃)는 건드리지 않는다.
- [ ] **Step 2: 테스트(실패)** — develop의 `common/timing_response_writer_test.go`에서 `SetWriteDeadline` 테스트(실제 `httptest.Server`에서 `http.NewResponseController(wrapped).SetWriteDeadline(...)`가 nil)를 옮긴다(`git show develop:common/timing_response_writer_test.go`). team에서 실행 → `feature not supported`로 FAIL.
- [ ] **Step 3: 구현** — `func (w *TimingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }` 추가(develop `af5f67506`과 같음). 실행: `GOWORK=off go test ./common/ ./relay/... -count=1`(team 트리에서).
- [ ] **Step 4: 커밋** — `fix(relay): keep write deadlines working under the timing writer` + `Ported-From: develop af5f67506` + 트레일러. `git log --oneline -1`과 브랜치 이름을 보고한다. worktree는 그대로 둔다.

---

### Task 9: 기록·overlay 표·전체 검증·병합 리허설

**Files:**
- Create: `docs/2026-10-15-rebase-phase-e.md`
- Modify: `docs/fork-overlays.md`(O11 절 신설과 이번 단계의 O3·O8·O9 행), spec §4.9·§9

- [ ] **Step 1: 전체 검증** — `GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"`; `GOWORK=off go test -race ./model/ ./service/ -run 'Checkin|Gemini|Vendor' -count=1`; `cd web && bun run typecheck && bun run test && bun run build && node scripts/ko-locale.mjs check && node scripts/ko-locale.mjs status && bun run e2e`(fresh SQLite, root/12345678; 21 통과 기대). 정확한 수치를 기록한다.
- [ ] **Step 2: 병합 리허설** — `git fetch qn && git merge --no-commit --no-ff qn/main`, 충돌 목록이 overlay 표의 upstream 파일 집합 안인지 확인, `git merge --abort`. ko.json이 upstream 새 키를 못 받으면 coverage 테스트가 실패하는 것을 리허설 트리에서 확인한다(`bun run test -- ko-coverage`; 확인 뒤 abort).
- [ ] **Step 3: 기록** — §1 O11 조사 결과(이미 equivalent인 항목과 근거: upstream `RechargeEpay` `50e5377ea`, `rejectInvalid*` 진입점 목록, Gemini 프롬프트만 사용량·누적 프레임, gpt-5 capability, Claude 5.x 표), §2 이번에 올린 세 수정(수치 예시 포함), §3 한국어 이월(감사 로그, Excel, relay 오류), §4 Task 7·8, §5 결정 사항(계획 결정 + 실행 중 ruling), §6 단계 F로 넘기는 것(Phase C·D §5·§6 중 남은 것: I5 조직 로그 가입 시점 제한, 할인 로그 MJ·태스크, `doRequest` 우회 `llm_ms`, 무료 모델 세션 우회, 다른 locale placeholder, `kr` 데이터 마이그레이션, `<html lang>`, Epay not-found 응답(upstream 유지), Claude native passthrough(upstream 유지), Gemini `TextTokens` 표시 차이와 `PostAudioConsumeQuota` 경로), §7 검증. overlay 표 줄 번호를 `grep -rn "fork: O" …`로 검증한다. spec §4.9에 "Epay·수용량 검증은 upstream이 해결"과 실제 구현 위치를 적는다.
- [ ] **Step 4: 커밋** — `docs: record phase E of the upstream rebase (O11 fixes and Korean carry-overs)` (트레일러 동일).

---

## 계획 결정 사항

1. Epay 트랜잭션 정산과 "fail" 응답, 결제 전 수용량 검증(PayPal·조직 포함)은 upstream과 단계 A·B가 이미 같은 동작을 하므로 다시 적용하지 않는다. 유일한 차이(develop은 not-found·다른 provider·만료 주문에 "fail"을 답해 Epay가 한도까지 재시도)는 upstream 동작을 따른다.
2. Gemini 이미지 상한은 relaykit을 고치지 않고 호스트의 `effectiveBillingUsage` 한 지점에 둔다. `TextTokens`에 VIDEO·DOCUMENT를 더하는 표시 차이는 과금에 영향이 없어 옮기지 않는다. `PostAudioConsumeQuota` 경로는 상한을 거치지 않는다(기록).
3. Ali 예산은 relaykit 마샬러가 지우므로 Ali 어댑터의 `DoRequest`에서 다시 넣는다. `ParamOverride`·`RemoveDisabledFields`는 다시 넣은 필드를 보지 못한다(기록).
4. 출석 보상과 추천 적립 이전에 지갑 상한을 둔다. upstream도 상한이 없지만, team이 막았던 구멍이라 fork로 유지한다.
5. Claude 5.x native passthrough(클라이언트가 보낸 `thinking.enabled`를 그대로 전달)는 upstream 설계를 따르고 team의 재작성은 옮기지 않는다. gpt-5 channel-test의 `IsOpenAIReasoningOModel` 한 줄 교체도 하지 않는다.
6. AI 클라이언트로 가는 조직 오류는 upstream의 `i18n.T` + yaml 경로를 쓴다. upstream yaml 파일에는 끝의 fork 블록만 더한다(충돌면 최소화).
7. Excel 행 오류는 기존 `errors []string`을 유지하고 `error_details`를 더해 호환을 지킨다.
8. team 역이식은 별도 브랜치·worktree에 커밋만 한다. team 병합과 배포는 사용자가 정한다.
9. 다른 6개 locale placeholder와 단계 B I5(조직 로그 가입 시점 제한)는 이번에도 다루지 않는다.
