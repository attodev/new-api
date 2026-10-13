# 2-A 후속 수정 8건 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** upstream 동기화 기록(`docs/2026-10-13-upstream-sync-phase2.md` 보류 TODO)에서 "실제 동작 문제"로 분류한 8건을 순서대로 고친다.

**Architecture:** 각 Task 는 독립 커밋. Task 1(Claude 5.x thinking 렌더링)만 설계 변경이 있고, 나머지는 국소 수정이다. upstream 에 대응 커밋이 있는 항목은 `Upstream:` trailer 를 남기고, fork 고유 수정은 trailer 없이 커밋한다.

**Tech Stack:** Go 1.26 (toolchain 1.27), Gin, GORM, testify. 프론트 변경 없음.

**Spec:** `docs/2026-10-13-upstream-sync-phase2.md` 의 "보류된 TODO" + 대화에서 확정한 Task 1 설계(아래 Task 1 에 전부 적음).

## Global Constraints

- 작업 브랜치 `codex/followups-2a-2026-10` (team 에서 분기). Task 단위 커밋, `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- JSON 은 `common.Marshal` / `common.Unmarshal` 만 (CLAUDE.md Rule 1). optional 스칼라는 포인터 + omitempty (Rule 6). DB 코드는 SQLite/MySQL/PostgreSQL 동시 호환, GORM 우선 (Rule 2).
- 베이스라인: `go build ./... && go test ./... -count=1` 전부 통과 상태(36 패키지 ok)에서 시작. 새 FAIL 금지.
- upstream 참조는 `qn/main` (`git show qn/main:<path>`). relaykit 은 도입하지 않는다.

## Review Focus

1. Claude 5.x 에 `thinking.type:"enabled"` 가 더 이상 전송되지 않아야 한다(모든 경로). → Task 1 테스트 표.
2. Epay 콜백에서 crediting 이 실패하면 주문 상태가 pending 으로 남아 재시도 가능해야 한다. → Task 2 테스트.
3. Gemini IMAGE 프롬프트 토큰을 `ImageTokens` 에 넣어도 `PromptTokens` 합계와 ImageRatio 미설정 모델의 quota 는 변하지 않아야 한다. → Task 4 테스트.
4. 관리자 preset 의 threshold 가 `MaxQuota - 충전량` 을 넘으면 생성/수정이 거부되어야 한다. → Task 5 테스트.
5. Ali 채널에서 매핑된 Qwen 배포명에도 `thinking_budget` 이 전달되어야 하고, 비 Qwen 모델에는 전달되지 않아야 한다. → Task 7 테스트.

---

### Task 0: 브랜치와 베이스라인

- [ ] `git checkout -b codex/followups-2a-2026-10 team`
- [ ] `go build ./... && go test ./... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort > <workspace>/baseline.txt` → FAIL 0 확인.

---

### Task 1: Claude 5.x thinking / effort 렌더링 통합

**배경.** upstream `claudeCapabilitiesFor`(`qn/main:relaykit/relayconvert/reasoning/claude.go:45-110`)는 `claude-fable-5*`, `claude-mythos-5*`, `claude-mythos-preview`, `claude-opus-5-5*`, `claude-sonnet-5-5*`, `claude-opus-5*`, `claude-sonnet-5*`, `claude-haiku-5*`, `claude-opus-4-8*`, `claude-opus-4-7*` 를 `supportsManual=false`(enabled+budget 거부, adaptive+effort 만), `strictSampling=true`(temperature/top_p/top_k 제거) 로 둔다. fork 는 4-7/4-8 prefix 만 하드코딩한다.

**Files:**
- Modify: `relay/channel/claude/effort.go` (표에 `supportsManual`, `strictSampling`, `defaultEffort` 열 추가; 렌더 helper 추가), `relay/channel/claude/relay-claude.go` (effort suffix 분기 ~224, `-thinking` 분기 ~240-262, `reasoning_effort` 분기 ~273-290, openrouter `reasoning.max_tokens` 분기 ~294-306), `relay/claude_handler.go` (`applyClaudeThinkingSettings` 전체: suffix gate ~33, `-thinking` ~52-70, 클라이언트 `enabled` passthrough 변환, strictSampling)
- Test: `relay/channel/claude/effort_test.go`, `relay/channel/claude/relay_claude_thinking_test.go`, `relay/claude_thinking_test.go`

**Interfaces (Produces):**
```go
// effort.go
type claudeEffortCapability struct { prefix string; supportsEffort, supportsXHigh, supportsMax, offAtMostHigh bool; supportsManual bool; strictSampling bool; defaultEffort string }
func claudeSupportsManualThinking(model string) bool   // 표에 없으면 true (알 수 없는 모델은 기존 동작)
func claudeStrictSampling(model string) bool            // 표에 없으면 false
func ClaudeEffortFromBudget(budget int) string          // upstream EffortFromBudget: 0→"none", <0→"high", ≤1024→"low", ≤8192→"medium", 그 외 "high"
// RenderClaudeThinking 은 요청에 thinking 을 "켜 달라"는 의도를 모델이 받는 형식으로 바꾼다.
// manual 지원 모델: {type:"enabled", budget_tokens:budget}; 미지원 모델: WithAdaptive() + output_config.effort (effort 가 비면 budget→effort, 그것도 없으면 defaultEffort).
func RenderClaudeThinking(req *dto.ClaudeRequest, model string, effort string, budget *int)
// ApplyClaudeStrictSampling 은 strictSampling 모델에서 Temperature/TopP/TopK 를 nil 로 만든다.
func ApplyClaudeStrictSampling(req *dto.ClaudeRequest, model string)
```

- [ ] **Step 1: 실패하는 테스트 작성** — `effort_test.go` 에 표 테스트: `claudeSupportsManualThinking`: opus-4-6 true, sonnet-4-5 true, opus-4-7/4-8 false, opus-5/5-5, sonnet-5/5-5, haiku-5-5, fable-5-1, mythos-5-1, mythos-preview false, unknown-model true. `claudeStrictSampling`: 4-7/4-8/5.x/fable/mythos true, 4-6 false. `ClaudeEffortFromBudget`: 0→none, -1→high, 1024→low, 8192→medium, 8193→high.
  `relay_claude_thinking_test.go`(OpenAI→Claude 변환)에 케이스: (a) `claude-opus-5-5` + `reasoning_effort:"high"` → thinking adaptive, output_config.effort high, Temperature/TopP/TopK nil; (b) `claude-opus-4-6` + `reasoning_effort:"low"` → enabled, budget 1280 (기존 동작 유지); (c) `claude-sonnet-5-5-thinking` → adaptive + effort high(기본), 모델명에서 `-thinking` 제거; (d) `claude-sonnet-5-5-xhigh` → 모델 `claude-sonnet-5-5`, adaptive, effort xhigh; (e) `claude-haiku-5-5` + openrouter `reasoning:{max_tokens:4000}` → adaptive + effort medium; (f) `claude-opus-5` + temperature 0.7, 아무 thinking 지시 없음 → Temperature nil (strictSampling), thinking 은 건드리지 않음.
  `relay/claude_thinking_test.go`(네이티브)에 케이스: (g) `claude-opus-5-5` 에 클라이언트가 `thinking:{type:"enabled",budget_tokens:2048}` → adaptive + effort medium + block_binding 유지(없으면 추가 안 함); (h) `claude-sonnet-5-5-high` suffix → 모델 `claude-sonnet-5-5`, adaptive, effort high; (i) `claude-opus-4-6` enabled+budget 은 그대로.
- [ ] **Step 2: 실패 확인** — `go test ./relay/channel/claude/ ./relay/ -run 'Manual|StrictSampling|EffortFromBudget|Claude5|Render' -count=1` → FAIL.
- [ ] **Step 3: 구현** — 표 열 추가(값은 upstream 표 그대로: fable/mythos/opus-5-5/sonnet-5-5 는 defaultEffort "" 이며 upstream 은 opus-5-5/haiku-5 가 defaultEffort medium, 나머지 high). `RenderClaudeThinking` 를 만들고 relay-claude.go 의 4개 분기와 claude_handler.go 의 분기가 모두 이것을 쓰게 한다. suffix gate 는 `strings.HasPrefix(... "claude-opus-4-6/7/8")` 대신 `claudeSupportsEffort(baseModel)` 로 바꾼다(단, `TrimEffortSuffix` 가 `-max/-xhigh/...` 를 떼어낸 뒤 base 가 표에 있을 때만). 네이티브 경로에서 클라이언트가 보낸 `thinking.type=="enabled"` + budget 을 manual 미지원 모델에 보내려 하면 `RenderClaudeThinking(req, model, "", budget)` 로 변환하고 `output_config` 는 기존 `NormalizeClaudeOutputConfig` 를 거친다. 모든 경로 끝에서 `ApplyClaudeStrictSampling`. 기존 4-7/4-8 전용 분기는 helper 로 흡수해 삭제한다.
- [ ] **Step 4: 통과 확인** — `go test ./relay/channel/claude/ ./relay/ ./relay/channel/aws/ ./relay/channel/vertex/ -count=1` PASS. `grep -n 'Type:\s*"enabled"' relay/channel/claude/relay-claude.go relay/claude_handler.go` 결과가 `RenderClaudeThinking` 내부 1곳뿐이어야 한다.
- [ ] **Step 5: Commit** — `fix(claude): render thinking and effort per model capability for Claude 5.x` + 본문에 upstream 표 참조 `Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 036ba376d)` (표의 출처).

---

### Task 2: Epay 콜백을 트랜잭션 정산으로

**Files:**
- Create: `model.RechargeEpay(tradeNo, actualPaymentMethod, callerIp string) (quota int64, err error)` in `model/topup.go` (Stripe 용 `Recharge` 539-595 와 같은 구조: `DB.Transaction` 안에서 `trade_no` 행을 `FOR UPDATE` 로 읽고, provider==epay, status==pending 검사, `PaymentMethod` 갱신, `CompleteTime`/`Status=success` 저장, `CreditTopUpTarget(tx, topUp, quota)`; 실패 시 전부 롤백)
- Modify: `controller/topup.go` `EpayNotify` 679-704: `topUp.Update()` + `CreditTopUpTarget(model.DB, …)` 두 단계를 `model.RechargeEpay` 한 번으로. 로그 메시지는 유지.
- Test: `controller/topup_epay_settlement_test.go` (새 파일; 기존 `topup_quota_limit_test.go` 의 SQLite fixture 패턴)

- [ ] **Step 1: 테스트** — (a) pending 주문 + 콜백 success → status success, 지갑 증가, `RecordTopupLog` 1건. (b) 사용자 quota 가 `MaxQuota-1` 이라 crediting 이 실패 → status 는 **pending 그대로**, 지갑 변화 없음(Review Focus 2). (c) 같은 콜백 두 번 → 두 번째는 "status error" 로 무시되고 지갑은 한 번만 증가.
- [ ] **Step 2: RED → Step 3: 구현 → Step 4: `go test ./controller/ ./model/ -run 'Epay' -count=1` PASS**
- [ ] **Step 5: Commit** — `fix(topup): settle epay callbacks in one transaction so failed credits stay pending` (fork 고유, trailer 없음)

---

### Task 3: task polling 2xx 허용

**Files:** Modify `service/task_polling.go:201` (`resp.StatusCode != http.StatusOK` → 2xx 판정). Test `service/task_polling_test.go` 에 201/202 성공, 400/502 실패 케이스(기존 테스트의 httptest 패턴).

- [ ] Step 1 테스트 → Step 2 RED → Step 3 `relay.taskSubmitStatusAccepted` 와 같은 술어를 service 에 두거나(패키지 순환 주의) 인라인 `code < 200 || code >= 300` → Step 4 `go test ./service/ -run 'Poll|Task' -count=1` PASS
- [ ] Commit — `fix(task): accept any 2xx status when polling async tasks` (`Upstream:` 없음 — upstream 도 미수정)

---

### Task 4: Gemini 프롬프트 IMAGE/VIDEO/DOCUMENT modality 귀속

**Files:** Modify `relay/channel/gemini/relay-gemini.go` `buildUsageFromGeminiMetadata` ~1018-1049. Test `relay/channel/gemini/relay_gemini_usage_test.go`.

- [ ] **Step 1: 테스트** — usageMetadata `promptTokenCount:2000, promptTokensDetails:[{TEXT:800},{IMAGE:1089},{VIDEO:100},{DOCUMENT:11}]` → `PromptTokens==2000`, `PromptTokensDetails.ImageTokens==1089`, `TextTokens==911`(TEXT + VIDEO + DOCUMENT: 텍스트 요금으로 과금되는 나머지), `AudioTokens==0`; IMAGE 만 있는 프롬프트 → `ImageTokens==1089`, `TextTokens==PromptTokens-1089`; `toolUsePromptTokensDetails` 의 IMAGE 도 합산. 그리고 settlement 테스트(`service/text_quota_test.go`): ImageRatio 미설정(=1) Gemini 모델에서 위 usage 의 quota 가 IMAGE 를 TEXT 로 셌을 때와 동일(Review Focus 3).
- [ ] **Step 2: RED → Step 3: 구현** — IMAGE → `PromptTokensDetails.ImageTokens`, VIDEO/DOCUMENT → `TextTokens`, TEXT → `TextTokens`, AUDIO → `AudioTokens`; 마지막 fallback 을 `TextTokens==0 && AudioTokens==0 && ImageTokens==0` 일 때 `TextTokens = PromptTokens` 로, 아니면 `TextTokens = PromptTokens - AudioTokens - ImageTokens`(음수면 0) 로 바꿔 합이 항상 `PromptTokens` 가 되게 한다. 주석으로 Google 과금(이미지 입력은 텍스트 단가, ImageRatio 기본 1)을 적는다.
- [ ] **Step 4:** `go test ./relay/channel/gemini/ ./service/ -count=1` PASS. **Step 5: Commit** — `fix(gemini): attribute image, video and document prompt tokens instead of dropping them`

---

### Task 5: 관리자 preset threshold 상한

**Files:** Modify `model/wallet_auto_recharge_preset.go` `normalizeAndValidate` ~157-160 (threshold 타입일 때 `ValidateWalletAutoRechargeThresholdCapacity(req.ThresholdQuota, WalletAutoRechargeCreditQuota(req.Amount))` 호출; `WalletAutoRechargeCreditQuota` 는 `model/wallet_auto_recharge.go` 에 있음). Test `model/wallet_auto_recharge_preset_test.go`.

- [ ] Step 1 테스트: threshold `MaxQuota` → 생성 거부(`ErrTopUpQuotaCapacityExceeded`), threshold `MaxQuota - credited - 1` → 허용, 수정(Update) 경로도 동일 → RED → 구현 → `go test ./model/ -run Preset -count=1` PASS
- [ ] Commit — `fix(wallet): reject auto-recharge presets whose threshold leaves no room for a charge`

---

### Task 6: 자동충전 fail-closed 로그의 target

**Files:** Modify `controller/topup.go` `topUpQuotaRejectionMessage` default 분기 ~373 와 `controller/wallet_auto_recharge.go` `validateWalletAutoRechargeCapacity` ~218-224 호출부: 자동충전 경로는 `walletRechargeTarget` 를 알고 있으므로 로그에 그 `TargetType/TargetId` 를 쓰도록 `topUpQuotaRejectionMessageForTarget(c, err, targetType, targetId)` 를 추가하고 기존 함수는 gin context 에서 읽는 wrapper 로 유지. Test: `controller/wallet_auto_recharge_capacity_test.go` 에 DB 오류를 흉내낼 수 없으면 로그 문자열 조립 함수를 분리해 단위 테스트(조직 target 이 `target_type=organization target_id=<org>` 로 찍힘).

- [ ] Step 1 테스트 → RED → 구현 → `go test ./controller/ -run 'AutoRecharge|Rejection' -count=1` PASS
- [ ] Commit — `fix(wallet): log the organization target in auto-recharge capacity failures`

---

### Task 7: Ali 채널의 Qwen thinking_budget 판정

**Files:** Modify `relay/channel/ali/text.go` `requestOpenAI2Ali` (origin 모델명도 받아 둘 중 하나가 Qwen 이면 유지), `relay/channel/ali/adaptor.go:175` (호출부 + 반환 타입을 `type aliChatRequest dto.GeneralOpenAIRequest` alias 로 바꿔 `GeneralOpenAIRequest.MarshalJSON` 의 이름 기반 drop 을 우회; Ali 채널에서는 `requestOpenAI2Ali` 가 유일한 판정자가 된다). `dto.IsQwenThinkingBudgetModel` 자체는 유지(다른 채널은 upstream 동일). Test `relay/channel/ali/adaptor_test.go` (기존 "mapped client alias to a Qwen upstream keeps 0" 패턴).

- [ ] Step 1 테스트: 클라이언트 `qwen3-max` → 채널 매핑 `my-qwen-deploy`(이름에 qwen 없음) + `thinking_budget:0` → marshal 결과에 `"thinking_budget":0` 존재; 클라이언트 `deepseek-v3` on Ali + `thinking_budget` → 없음(Review Focus 5) → RED → 구현 → `go test ./relay/channel/ali/ ./dto/ -count=1` PASS
- [ ] Commit — `fix(ali): keep thinking_budget for Qwen requests mapped to custom deployment names`

---

### Task 8: tokenizer 워밍업

**Files:** Modify `main.go` (`service.InitHttpClient()` 뒤에 `go func(){ tokenkit.Count("gpt-4o", "warm up"); tokenkit.Count("gpt-4", "warm up") }()` — o200k, cl100k 두 코덱을 비동기로 빌드; import `github.com/QuantumNous/new-api/tokenkit`). 테스트: `tokenkit` 에 `Warm()` 를 두고 싶으면 `tokenkit/tokenkit.go` 에 `func Warm()` 추가 + 테스트에서 두 번 호출해도 idempotent. 모듈 변경 시 `tokenkit` 디렉터리 README 한 줄.

- [ ] 구현 → `go build ./... && go test ./tokenkit/ -count=1` → Commit — `perf(tokenkit): warm the OpenAI codecs at startup`

---

### Task 9: 기록과 전체 검증

- [ ] `go build ./... ; go vet ./... | tail -5 ; go test ./... -count=1 | grep -E '^(--- FAIL|FAIL|ok)'` → 새 FAIL 없음. `(cd tokenkit && go test ./...)`.
- [ ] `docs/2026-10-14-followups-2a.md` 작성 (형식은 phase 기록과 동일: 검토 범위 / 반영 커밋 표 / 반영 후 동작 / 수행한 검증 / 운영 전제 / 보류된 TODO). `docs/2026-10-13-upstream-sync-phase2.md` 의 보류 TODO 에서 해소된 항목 8개에 "→ 2026-10-14 해소" 표시.
- [ ] Commit — `docs: record the 2-A follow-up fixes`
