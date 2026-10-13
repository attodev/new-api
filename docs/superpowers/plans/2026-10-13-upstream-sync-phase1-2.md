# Upstream 동기화 (1단계 + 2단계) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** upstream(QuantumNous/new-api, `qn/main` = `63cecab82`)의 2026-05-27 ~ 10-13 변경 중 상위 프로바이더 protocol 수정과 과금 계산 수정을 team 트리에 포팅한다. relaykit/web/src/task 플러그인/인증 재편은 범위 밖.

**Architecture:** upstream은 7월 말 변환 계층을 `relaykit/`로 분리했으므로 대부분의 커밋은 그대로 cherry-pick 되지 않는다. 각 Task는 upstream 커밋 해시를 source of truth로 삼고(`git show <hash>`), team 쪽 대응 파일·함수에 같은 동작을 옮긴다. upstream 테스트는 team 경로로 옮겨 그대로 통과시킨다. 8월 포팅과 같은 커밋 규약(`Upstream:` trailer)을 유지한다.

**Tech Stack:** Go 1.27, Gin, GORM, testify. 프론트는 2단계 Task 21에서만 `web/default`(React, Bun)를 만진다.

**Spec:** `docs/2026-10-13-upstream-protocol-billing-review.md` (검토 보고서, 권장 순서 포함)

## Global Constraints

- 작업 브랜치: `codex/upstream-sync-2026-10` (team에서 분기). Task 단위로 커밋.
- 커밋 본문 마지막에 반드시 `Upstream: https://github.com/QuantumNous/new-api/commit/<full sha>` 를 넣는다. full sha는 `git rev-parse <short>` 로 얻는다. 여러 커밋을 합친 Task는 줄을 여러 개 쓴다. 그 뒤에 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- JSON 처리는 `common.Marshal` / `common.Unmarshal` 만 사용 (CLAUDE.md Rule 1). upstream 코드의 `kitutil.Marshal`, `json.Marshal` 은 치환한다.
- 요청 DTO의 optional 스칼라는 포인터 + `omitempty` (Rule 6). upstream의 `json.RawMessage` passthrough 필드는 그대로 `json.RawMessage`.
- DB 코드는 SQLite/MySQL/PostgreSQL 동시 호환 (Rule 2). Task 20, 22만 해당.
- upstream 경로 → team 경로 치환표: `relaykit/dto/*.go` → `dto/*.go`, `relaykit/relayconvert/internal/oai_chat/to_claude_messages_req.go` → `relay/channel/claude/relay-claude.go` (`RequestOpenAI2ClaudeMessage`), `relaykit/relayconvert/internal/oai_chat/to_oai_responses_req.go` → `service/openaicompat/chat_to_responses.go`, `relaykit/relayconvert/reasoning/*` → `relay/channel/claude/relay-claude.go` + `relay/claude_handler.go` + `setting/reasoning/suffix.go`, `service/billing_usage.go` / `service/responses_usage.go` → `service/text_quota.go` + `relay/channel/openai/relay_responses.go`, `relay/channel/openai/relay_image.go` → `relay/channel/openai/relay-openai.go`.
- upstream 테스트 파일에서 `relaykit` import는 team 패키지로 바꾸고, `kitutil` 헬퍼는 `common` 으로 바꾼다.
- 베이스라인: Task 0에서 기록한 실패 테스트 집합 외의 새 실패는 허용하지 않는다.
- 한 Task가 끝날 때마다 `go build ./... && go vet ./relay/... ./dto/... ./service/...` 가 통과해야 한다.
- 이번 범위에서 의도적으로 제외(검토 보고서 2단계 목록 중): Google Search grounding / vendor-reported search 과금(`d10f11fff`, `acb347a19`)과 표현식 확장(`064ed943e`, `f362c7c51`, `25ec832fa`). 전자는 relaykit hosted-tool DTO, 후자는 upstream billingexpr 재편에 의존해 relaykit 도입 프로젝트와 함께 다룬다. 모델/벤더 가격 재편(`0c76e4dae`)도 team 할인 계층과 설계 충돌이라 제외.

## Review Focus

1. Claude 요청에서 클라이언트가 보낸 `thinking` 하위 필드(알 수 없는 키 포함)가 upstream으로 그대로 가야 한다. 모르는 키를 떨어뜨리면 멀티턴 대화가 깨진다. → Task 2 Step 1 테스트.
2. effort가 cap 될 때 클라이언트 요청 자체를 거부하면 안 되고, 모델이 받는 값으로 낮춰서 보내야 한다. → Task 5 Step 1 테스트의 xhigh→high 케이스.
3. Responses 스트림이 업스트림 오류로 끝난 경우에는 prompt 추정 과금을 하면 안 된다(정상 중단과 오류를 구분). → Task 10 Step 1 테스트의 error 케이스.
4. 이미지 응답에서 `revised_prompt` 만 있는 항목은 0장, url+b64_json 분리 항목은 1장으로 세야 한다. → Task 12 Step 1 테스트.
5. 응답 헤더 타임아웃은 스트리밍 본문 전송에는 적용되지 않아야 한다. `RELAY_RESPONSE_HEADER_TIMEOUT=0` 이면 이전처럼 무제한. → Task 14 Step 1 테스트.

---

### Task 0: 베이스라인 기록

**Files:**
- Create: `docs/superpowers/plans/2026-10-13-upstream-sync-baseline.txt` (커밋하지 않음, 작업용)

- [ ] **Step 1: 현재 브랜치와 HEAD 확인**

Run: `git status --short && git branch --show-current && git log -1 --format=%h`
Expected: 브랜치 `codex/upstream-sync-2026-10`, HEAD `5167b251d`, untracked는 `docs/2026-10-13-upstream-protocol-billing-review.md` 뿐.

- [ ] **Step 2: 검토 보고서 커밋**

```bash
git add docs/2026-10-13-upstream-protocol-billing-review.md docs/superpowers/plans/2026-10-13-upstream-sync-phase1-2.md
git commit -m "docs: record upstream protocol/billing review and sync plan

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

- [ ] **Step 3: 베이스라인 테스트 실행**

Run: `go build ./... && go test ./relay/... ./dto/... ./service/... ./common/... ./setting/... ./model/... ./controller/... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' > docs/superpowers/plans/2026-10-13-upstream-sync-baseline.txt; grep -c FAIL docs/superpowers/plans/2026-10-13-upstream-sync-baseline.txt`
Expected: 빌드 성공. FAIL 줄이 있으면 그 목록이 이후 Task의 허용 실패 집합이다. (`model`, `controller` 패키지는 SQLite 메모리 DB를 쓰므로 수 분 걸릴 수 있다.)

---

### Task 1: 그대로 적용되는 upstream 커밋 5건 cherry-pick

**Files:**
- Modify: `relay/channel/zhipu_4v/relay-zhipu_v4.go`, `relay/channel/ollama/stream.go`, `relay/channel/ali/image.go`, `model/log.go`, `service/task_billing.go`
- Test: `relay/channel/ollama/stream_test.go`, `relay/channel/ali/adaptor_test.go`

- [ ] **Step 1: 한 건씩 cherry-pick 하고 trailer 를 붙인다**

```bash
for h in 3e8c358da 8e5e09166 0bee5d441 8c8c4153d b22a43b6e; do
  git cherry-pick "$h" || exit 1
  git commit --amend --no-edit \
    --trailer "Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse "$h")" \
    --trailer "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
done
```
Expected: 5개 커밋 생성, `git log -5 --format=%B` 에 각각 `Upstream:` 줄과 Co-Authored-By 줄. 충돌이 나면 `git show <hash>` 로 의도를 확인하고 team 코드에 맞게 해결한 뒤 `git cherry-pick --continue`, 그 다음 같은 `--amend --trailer` 를 수동으로 실행.

- [ ] **Step 2: 커밋 본문 확인**

Run: `git log -5 --format='%h %s%n%b' | grep -c 'Upstream:'`
Expected: `5`

- [ ] **Step 3: 검증**

Run: `go build ./... && go test ./relay/channel/ollama/... ./relay/channel/ali/... ./relay/channel/zhipu_4v/... ./model/ -run 'Stream|Ali|SumUsedQuota' -count=1`
Expected: PASS. `8c8c4153d` 는 `SumUsedQuota` 가 rpm/tpm 스캔으로 `stat.Quota` 를 덮어쓰지 않게 하는 수정이다. `model/log.go` 의 `SumUsedQuota` 에서 `rpmTpmQuery.Scan(&stat)` 가 별도 구조체로 바뀌었는지 확인.

---

### Task 2: Claude DTO passthrough 필드 (safeguards, 메시지 output_config, thinking.block_binding)

upstream: `789c97019`, `c2b7a9a9e`, `767a82b87` 의 `relaykit/dto/claude.go` 부분.

**Files:**
- Modify: `dto/claude.go` (`ClaudeRequest` 약 220행, `ClaudeMessage`, `Thinking` 447행)
- Test: `dto/claude_passthrough_test.go` (새 파일)

**Interfaces:**
- Produces: `dto.Thinking.BlockBinding json.RawMessage`, `dto.ClaudeRequest.Safeguards json.RawMessage`, `dto.ClaudeMessage.OutputConfig json.RawMessage`. Task 3, 5가 `Thinking` 을 다시 만들 때 `BlockBinding` 을 복사해야 한다.

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// dto/claude_passthrough_test.go
package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeRequestRoundTripsPassthroughFields(t *testing.T) {
	raw := `{"model":"claude-opus-4-7","max_tokens":10,
	  "safeguards":{"mode":"auto"},
	  "thinking":{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"}},
	  "messages":[{"role":"user","content":"hi"},{"role":"user","content":[],"output_config":{"effort":"low"}}]}`
	var req ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(raw, &req))
	require.JSONEq(t, `{"mode":"auto"}`, string(req.Safeguards))
	require.JSONEq(t, `{"prefix_mismatch_behavior":"drop_block"}`, string(req.Thinking.BlockBinding))
	require.JSONEq(t, `{"effort":"low"}`, string(req.Messages[1].OutputConfig))

	out, err := common.Marshal(req)
	require.NoError(t, err)
	require.Contains(t, string(out), `"safeguards":{"mode":"auto"}`)
	require.Contains(t, string(out), `"block_binding":{"prefix_mismatch_behavior":"drop_block"}`)
	require.Contains(t, string(out), `"output_config":{"effort":"low"}`)
}

func TestClaudeThinkingOmitsBlockBindingWhenAbsent(t *testing.T) {
	out, err := common.Marshal(Thinking{Type: "adaptive"})
	require.NoError(t, err)
	require.NotContains(t, string(out), "block_binding")
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./dto/ -run 'TestClaudeRequestRoundTripsPassthroughFields|TestClaudeThinkingOmitsBlockBindingWhenAbsent' -count=1`
Expected: FAIL (컴파일 에러: `Safeguards`, `BlockBinding`, `OutputConfig` 필드 없음)

- [ ] **Step 3: 필드 추가**

`dto/claude.go`:
- `ClaudeRequest` 에 `Tools` 다음 줄: `Safeguards json.RawMessage \`json:"safeguards,omitempty"\``
- `ClaudeMessage` 에 `Content` 다음 줄: `OutputConfig json.RawMessage \`json:"output_config,omitempty"\`` (주석: effort-only system 메시지는 content가 비어 있고 output_config.effort 에 새 레벨이 온다)
- `Thinking` 에 `Display` 다음 줄: `BlockBinding json.RawMessage \`json:"block_binding,omitempty"\``

upstream `767a82b87` 는 "round-trips thinking fields it does not model yet" 까지 하지만, 그건 relaykit 의 generic passthrough 구조 전제다. team 에서는 `block_binding` 명시 필드만 추가한다 (Review Focus 1 은 이 필드로 충족).

- [ ] **Step 4: 통과 확인 + 기존 테스트**

Run: `go test ./dto/ ./relay/channel/claude/ ./relay/channel/aws/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add dto/claude.go dto/claude_passthrough_test.go
git commit -m "fix(claude): pass through safeguards, per-message output_config and thinking.block_binding

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 789c97019)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse c2b7a9a9e)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 767a82b87)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: anthropic-beta 다중 헤더 결합, Bedrock beta 목록 trim, block_binding 복원

upstream: `767a82b87` 의 `relay/channel/claude/adaptor.go`, `relay/channel/aws/dto.go`, 테스트 `relay/channel/claude/adaptor_test.go`, `relay/channel/aws/relay_aws_test.go`.

**Files:**
- Modify: `relay/channel/claude/adaptor.go:73-79` (`CommonClaudeHeadersOperation`), `relay/channel/aws/dto.go:42-55`, `relay/channel/claude/relay-claude.go:213-240` (adaptive 로 다시 렌더할 때 `BlockBinding` 유지), `relay/claude_handler.go:30` (`applyClaudeThinkingSettings`, 같은 처리)
- Test: `relay/channel/claude/adaptor_test.go`, `relay/channel/aws/relay_aws_test.go`

- [ ] **Step 1: upstream 테스트를 team 경로로 복사**

Run: `git show 767a82b87 -- relay/channel/claude/adaptor_test.go relay/channel/aws/relay_aws_test.go relay/channel/claude/relay_claude_test.go`
upstream 테스트 3개(`anthropic-beta` 여러 줄 → 한 줄 콤마 결합, Bedrock `anthropic_beta` 항목 공백 trim, effort suffix 로 adaptive 재렌더 시 클라이언트 `block_binding` 유지 + 클라이언트가 안 보낸 block_binding 은 추가하지 않음)를 team 테스트 파일에 추가한다. `relaykit/dto` import 는 `github.com/QuantumNous/new-api/dto` 로 바꾼다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/claude/ ./relay/channel/aws/ -count=1`
Expected: 새 테스트 FAIL (헤더가 첫 값만 전달됨 등)

- [ ] **Step 3: 구현**

`relay/channel/claude/adaptor.go` `CommonClaudeHeadersOperation`:
```go
if values := c.Request.Header.Values("anthropic-beta"); len(values) > 0 {
	req.Set("anthropic-beta", strings.Join(values, ","))
}
```
`relay/channel/aws/dto.go` 에서 `anthropic-beta` 헤더를 `anthropic_beta` 배열로 바꾸는 곳: 콤마로 split 한 뒤 각 항목 `strings.TrimSpace`, 빈 항목 제거.
`relay/channel/claude/relay-claude.go` 와 `relay/claude_handler.go` 에서 `claudeRequest.Thinking = &dto.Thinking{Type: "adaptive"}` 로 새로 만드는 모든 곳: 기존 `claudeRequest.Thinking` 이 nil 이 아니면 `BlockBinding` 과 `Display` 를 복사한다. 헬퍼를 `dto/claude.go` 에 추가:
```go
// WithAdaptive returns an adaptive Thinking that keeps the client's
// block_binding and display; it never adds a block_binding the client did not send.
func (c *Thinking) WithAdaptive() *Thinking {
	out := &Thinking{Type: "adaptive"}
	if c != nil {
		out.BlockBinding = c.BlockBinding
		out.Display = c.Display
	}
	return out
}
```
그리고 각 위치를 `claudeRequest.Thinking = claudeRequest.Thinking.WithAdaptive()` 로 바꾼다 (`request.Thinking` 변수명은 파일마다 다름).

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/claude/ ./relay/channel/aws/ ./relay/ -run 'Claude|Aws|Beta|Thinking' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/claude relay/channel/aws relay/claude_handler.go dto/claude.go
git commit -m "fix(claude): forward every anthropic-beta value and keep block_binding on adaptive re-render

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 767a82b87)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Claude 변환 시 빈 tools 생략, 파라미터 없는 tool 보존

upstream: `4442bb302`, `3dda1d50c` (`to_claude_messages_req.go`, `shared/claude/schema.go`).

**Files:**
- Modify: `relay/channel/claude/relay-claude.go:105-190` (`RequestOpenAI2ClaudeMessage` 의 tools 변환과 `Tools: claudeTools` 대입)
- Test: `relay/channel/claude/relay_claude_test.go`

- [ ] **Step 1: upstream 테스트 이식**

Run: `git show 3dda1d50c -- relaykit/relayconvert/internal/oai_chat/to_claude_messages_req_test.go; git show 4442bb302 -- relaykit/relayconvert/claude_default_max_tokens_test.go`
케이스: (a) OpenAI 요청에 `tools` 가 없으면 Claude 요청 JSON 에 `"tools"` 키가 없어야 한다(현재는 `[]` 로 전송). (b) `parameters` 가 없거나 `{}` 인 function tool 은 `input_schema: {"type":"object","properties":{}}` 로 보존되어야 한다(현재 코드가 떨어뜨리는지 확인). team 의 `RequestOpenAI2ClaudeMessage(c, textRequest)` 시그니처로 호출하도록 옮긴다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/claude/ -run 'Tools|Parameterless|EmptyTools' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

`relay-claude.go`: `claudeRequest := dto.ClaudeRequest{ ... Tools: claudeTools, ...}` 에서 `Tools` 줄을 빼고, 구조체 생성 뒤
```go
if len(claudeTools) > 0 {
	claudeRequest.Tools = claudeTools
}
```
파라미터 없는 tool: 변환 루프에서 `tool.Function.Parameters` 가 nil/빈 객체일 때 `claudeTool.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}` 로 채운다. upstream `schema.go` 의 `ensureObjectSchema` 를 같은 파일의 private 함수로 옮긴다.

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/claude/ ./relay/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/claude
git commit -m "fix(claude): omit empty tools and keep parameterless tools in OpenAI→Claude conversion

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 4442bb302)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 3dda1d50c)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: 모델별 Claude effort 허용 범위 매칭

upstream: `036ba376d` (`relaykit/relayconvert/reasoning/claude.go`). upstream 에는 모델 capability 표(`claudeCapabilitiesFor`)가 있고 team 에는 없다. team 에 작은 표를 새로 만든다.

**Files:**
- Create: `relay/channel/claude/effort.go`, `relay/channel/claude/effort_test.go`
- Modify: `relay/channel/claude/relay-claude.go:213-290` (effort suffix, `-thinking`, `reasoning_effort` 분기), `relay/claude_handler.go:30` (`applyClaudeThinkingSettings`, 네이티브 `/v1/messages` 경로의 `output_config.effort`)

**Interfaces:**
- Produces: `func ClaudeEffort(model, effort string) string` — 모델이 받는 effort 로 낮추거나(xhigh→high), 모델이 effort 를 못 받으면 `""`. `func claudeSupportsEffort(model string) bool`.

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// relay/channel/claude/effort_test.go
package claude

import "testing"

func TestClaudeEffort(t *testing.T) {
	cases := []struct{ model, in, want string }{
		{"claude-opus-4-7", "xhigh", "xhigh"},
		{"claude-opus-4-5", "max", "high"},      // 4.5 는 high 이하만
		{"claude-opus-4-5", "xhigh", "high"},
		{"claude-sonnet-4-5", "high", ""},       // effort 미지원
		{"claude-haiku-4-5", "low", ""},
		{"claude-opus-5", "max", "high"},        // Opus 5 / Haiku 5.5 / Sonnet 5.5 는 high 로 cap
		{"claude-haiku-5-5", "xhigh", "high"},
		{"claude-sonnet-5-5", "xhigh", "high"},
		{"claude-sonnet-5-5", "medium", "medium"},
		{"claude-fable-5-1", "max", "max"},      // Fable / Mythos 는 effort 지원
		{"claude-mythos-5-1", "low", "low"},
		{"claude-opus-4-7", "", ""},
		{"claude-opus-4-7", "none", ""},
		{"unknown-model", "high", "high"},       // 표에 없는 모델은 그대로
	}
	for _, c := range cases {
		if got := ClaudeEffort(c.model, c.in); got != c.want {
			t.Errorf("ClaudeEffort(%q,%q)=%q want %q", c.model, c.in, got, c.want)
		}
	}
}
```
표의 정확한 값은 `git show 036ba376d -- relaykit/relayconvert/reasoning/claude.go` 와 `git show qn/main:relaykit/relayconvert/reasoning/claude.go` 의 `claudeCapabilitiesFor` 를 읽고 맞춘다. 위 케이스가 upstream 표와 다르면 테스트를 upstream 기준으로 고친다 (upstream 이 정답).

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/claude/ -run TestClaudeEffort -count=1`
Expected: FAIL (함수 없음)

- [ ] **Step 3: 구현**

`relay/channel/claude/effort.go`: upstream `claudeCapabilitiesFor` 의 `supportsEffort`, `maxEffort`(high 또는 max), `offEffortAtMostHigh` 를 prefix 매칭 표로 옮기고 `ClaudeEffort` 를 작성한다. effort 순서는 `low < medium < high < xhigh < max`.

`relay-claude.go` 와 `claude_handler.go` 에서 `OutputConfig = {"effort": X}` 를 쓰는 모든 자리에서 `X = ClaudeEffort(baseModel, X)` 를 거치고, 결과가 `""` 이면 `OutputConfig` 를 설정하지 않는다. 네이티브 경로(`applyClaudeThinkingSettings`)에서 클라이언트가 보낸 `output_config.effort` 도 `req.GetEfforts()` 로 읽어 같은 규칙으로 낮춘다 (요청 거부 금지, Review Focus 2).

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/claude/ ./relay/ -count=1`
Expected: PASS. 기존 `relay_claude_thinking_test.go`, `relay/claude_thinking_test.go` 가 깨지면 그 테스트의 기대값이 upstream 규칙과 다른 것이므로 upstream 규칙 쪽으로 맞춘다.

- [ ] **Step 5: Commit**

```bash
git add relay/channel/claude relay/claude_handler.go
git commit -m "fix(claude): send each model the effort level it accepts

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 036ba376d)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Claude Opus 4.8 등록

upstream: `0c7aceb83`. 이 커밋은 team 과 같은 파일 구조라 cherry-pick 시도 후 충돌만 해결한다.

**Files:**
- Modify: `relay/channel/claude/constants.go`, `relay/channel/aws/constants.go`, `relay/channel/vertex/adaptor.go`, `relay/channel/claude/relay-claude.go`, `relay/claude_handler.go`, `setting/ratio_setting/model_ratio.go`, `setting/ratio_setting/cache_ratio.go`
- Test: `relay/channel/claude/relay_claude_test.go`

- [ ] **Step 1: cherry-pick**

```bash
git cherry-pick -x 0c7aceb83
```
Expected: `relay-claude.go` / `claude_handler.go` 에서 충돌 가능 (Task 3, 5 가 같은 구간을 바꿈). 충돌 시 의도는 "`claude-opus-4-7` prefix 분기마다 `claude-opus-4-8` 추가" 이므로 Task 5 에서 만든 코드에 4-8 을 추가하는 형태로 해결.

- [ ] **Step 2: 검증**

Run: `grep -rn 'opus-4-8' relay setting | grep -v _test | wc -l && go test ./relay/channel/claude/ ./setting/ratio_setting/ -count=1`
Expected: constants / ratio / cache_ratio / relay-claude / claude_handler / aws / vertex 모두에 등장, 테스트 PASS.

- [ ] **Step 3: 커밋 메시지 정리**

```bash
git commit --amend -m "feat(claude): add claude-opus-4-8 model family

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 0c7aceb83)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: OpenAI gpt-5 이후 세대 capability 판정

upstream: `49ec46966`, `d0cb7347c`.

**Files:**
- Modify: `dto/openai_request.go:222-240` (`IsOpenAIGPT5Model`, 호출부), `relay/channel/openai/adaptor.go:320-345`, `controller/channel-test.go` (테스트 요청 생성부)
- Test: `dto/openai_request_zero_value_test.go`, `controller/channel_test_request_test.go` (새 파일, upstream 에서 이식)

**Interfaces:**
- Produces: `dto.IsOpenAIGPT5Model(model string) bool` 의미 변경(메이저 ≥ 5), 새 `dto.GetOpenAIChatCapabilities(model, reasoningEffort string) dto.OpenAIChatCapabilities{UseMaxCompletionTokens, SupportsSampling bool}`.

- [ ] **Step 1: upstream 테스트 이식**

Run: `git show 49ec46966 d0cb7347c -- relaykit/dto/openai_request_zero_value_test.go controller/channel_test_request_test.go`
`relaykit/dto` → `dto`. 케이스: `gpt-6-astra`, `gpt-6-sol`, `gpt-6-luna`, `gpt-7-x` 는 `UseMaxCompletionTokens=true`; `gpt-4.1`, `gpt-4o`, `gpt-oss-120b`, `gpt-image-1`, `gpt-realtime` 은 false; `gpt-5.1/5.2/5.4` 와 `gpt-6-sol/luna` 는 effort 가 비어있거나 none 일 때만 sampling 허용; `gpt-6-astra` 는 sampling 불가; 채널 테스트 요청은 이 모델들에 `max_completion_tokens` 를 보낸다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./dto/ ./controller/ -run 'GPT5|Capabilit|TestRequest' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

`dto/openai_request.go`: `IsOpenAIGPT5Model` 을 upstream 과 같이 `gpt-<n>` 의 n 을 파싱해 `n >= 5` 로 판정(`gpt-4.1`, `gpt-4o`, `gpt-oss`, `gpt-image`, `gpt-realtime` 제외). `GetOpenAIChatCapabilities` 와 `isOpenAIModelSnapshot` 을 upstream 에서 옮긴다. `relay/channel/openai/adaptor.go` 의 `isOModel || isGPT5Model` 분기를 `caps := dto.GetOpenAIChatCapabilities(info.UpstreamModelName, request.ReasoningEffort)` 기반으로 바꾸고, `caps.SupportsSampling == false` 면 `Temperature`, `TopP`, `Logprobs`, `TopLogprobs` 를 nil 로. `controller/channel-test.go` 의 테스트 요청 생성에서 같은 헬퍼로 `MaxCompletionTokens` 를 쓴다.

- [ ] **Step 4: 통과 확인**

Run: `go test ./dto/ ./relay/channel/openai/ ./controller/ -run 'GPT5|Capabilit|TestRequest|OModel' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add dto relay/channel/openai controller/channel-test.go controller/channel_test_request_test.go
git commit -m "fix(openai): apply gpt-5+ chat capabilities by major version

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 49ec46966)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse d0cb7347c)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: Chat → Responses 변환 시 prompt_cache_key 전달

upstream: `7d09c6954`.

**Files:**
- Modify: `service/openaicompat/chat_to_responses.go` (`ChatCompletionsRequestToResponsesRequest` 또는 team 의 동명 함수, 약 360-410행)
- Test: `service/openaicompat/chat_to_responses_test.go`

- [ ] **Step 1: 테스트 추가**

```go
func TestChatToResponsesForwardsPromptCacheKey(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{Model: "gpt-5.1", PromptCacheKey: "session-42"}
	req.Messages = []dto.Message{{Role: "user", Content: "hi"}}
	out, err := ChatCompletionsRequestToResponsesRequest(req) // team 함수명에 맞출 것
	require.NoError(t, err)
	require.JSONEq(t, `"session-42"`, string(out.PromptCacheKey))
}
```
빈 `PromptCacheKey` 면 출력에 키가 없어야 한다는 케이스도 추가.

- [ ] **Step 2: 실패 확인**

Run: `go test ./service/openaicompat/ -run PromptCacheKey -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

```go
var promptCacheKeyRaw json.RawMessage
if req.PromptCacheKey != "" {
	promptCacheKeyRaw, err = common.Marshal(req.PromptCacheKey)
	if err != nil {
		return nil, fmt.Errorf("marshal prompt_cache_key: %w", err)
	}
}
// ... out := &dto.OpenAIResponsesRequest{ ..., PromptCacheKey: promptCacheKeyRaw, ...}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./service/openaicompat/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add service/openaicompat
git commit -m "fix(relay): forward prompt_cache_key when converting chat to responses

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 7d09c6954)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Responses cached token 정산 정규화

upstream: `f11641428` (`service/billing_usage.go`). team 에는 `billing_usage.go` 가 없고 `service/text_quota.go` 가 `usage.PromptTokensDetails.CachedTokens` 를 바로 읽는다.

**Files:**
- Modify: `service/text_quota.go:185-200` (summary 구성 직전), 필요 시 `service/usage_helpr.go`
- Test: `service/text_quota_test.go`

- [ ] **Step 1: upstream 테스트 이식**

Run: `git show f11641428 -- service/text_quota_test.go`
케이스: `usage.InputTokensDetails.CachedTokens` 만 채워진 Responses usage 가 `PromptTokensDetails.CachedTokens` 로 복사되어 캐시 단가로 정산; `CachedCreationTokens` 도 같은 방식; DeepSeek 식 `PromptCacheHitTokens` 가 `CachedTokens` 로 복사. team 의 정산 함수 시그니처(`service/text_quota.go` 의 `calculateTextQuota`/`PostConsumeQuota` 류)에 맞춰 옮긴다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./service/ -run 'Cached|CacheHit' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

`service/text_quota.go` 에 정산 직전 호출되는 함수 추가:
```go
// normalizeCachedUsage copies cache counts that upstream reported under the
// Responses (input_tokens_details) or DeepSeek (prompt_cache_hit_tokens) names
// onto PromptTokensDetails, which settlement reads.
func normalizeCachedUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokensDetails != nil {
		if usage.PromptTokensDetails.CachedTokens == 0 && usage.InputTokensDetails.CachedTokens > 0 {
			usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		}
		if usage.PromptTokensDetails.CachedCreationTokens == 0 && usage.InputTokensDetails.CachedCreationTokens > 0 {
			usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		}
	}
	if usage.PromptTokensDetails.CachedTokens == 0 && usage.PromptCacheHitTokens > 0 {
		usage.PromptTokensDetails.CachedTokens = usage.PromptCacheHitTokens
	}
}
```
`dto.Usage.InputTokensDetails` 타입과 필드명은 `dto/openai_response.go:250-290` 에서 확인해 맞춘다.

- [ ] **Step 4: 통과 확인**

Run: `go test ./service/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add service
git commit -m "fix(billing): settle Responses and DeepSeek cached tokens at the cache price

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse f11641428)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: 중단된 Responses 스트림 usage 추정

upstream: `55a6cd2a4` (`service/responses_usage.go`). team 은 `relay/channel/openai/relay_responses.go` 의 `OaiResponsesStreamHandler` 가 `output_text.delta` 만 누적한다.

**Files:**
- Modify: `relay/channel/openai/relay_responses.go:72-170`
- Test: `relay/channel/openai/relay_responses_stream_usage_test.go` (새 파일)

- [ ] **Step 1: 실패하는 테스트 작성**

upstream `service/responses_usage_test.go` 의 케이스를 team 핸들러에 맞춰 SSE 바디를 직접 만들어 호출한다 (기존 `relay/channel/openai/image_stream_test.go` 의 `httptest` 패턴 참고). 케이스:
1. `response.function_call_arguments.delta` 만 있고 `response.completed` 가 없는 스트림 → `usage.CompletionTokens > 0`, `usage.PromptTokens == info.GetEstimatePromptTokens()`.
2. `response.reasoning_summary_text.delta` / `response.reasoning_text.delta` / `response.refusal.delta` 도 같은 결과.
3. `response.created` 하나만 오고 끊긴 스트림 → `CompletionTokens == 0`, `PromptTokens == 추정값` (업스트림이 생성을 시작했으므로 입력 과금).
4. `response.failed` 또는 `error` 이벤트로 끝난 스트림 → `PromptTokens == 0`, `CompletionTokens == 0` (Review Focus 3).
5. `response.completed` 에 usage 가 있는 정상 케이스는 변화 없음.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/openai/ -run ResponsesStreamUsage -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

핸들러에 `started bool`, `failed bool` 추가. 콜백에서: 어떤 이벤트든 파싱 성공 시 `started = true`; `case "response.output_text.delta", "response.function_call_arguments.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.refusal.delta":` 모두 `responseTextBuilder.WriteString(streamResponse.Delta)`; `case "response.failed", "error":` 에서 `failed = true`; `response.completed` 에서 `outputText` 가 비어 있으면 `service/openaicompat.ExtractOutputTextFromResponses(streamResponse.Response)` 로 채운다. 종료 후:
```go
billsPrompt := usage.CompletionTokens != 0 || (started && !failed)
if usage.PromptTokens == 0 && billsPrompt {
	usage.PromptTokens = info.GetEstimatePromptTokens()
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/openai/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/openai
git commit -m "fix(responses): estimate usage for streams cut before terminal usage

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 55a6cd2a4)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: Responses → Chat 스트림 중복 tool call 검증

upstream: `1086038f5`. team 의 `relay/channel/openai/chat_via_responses.go` 는 `callID` 키 기반 자체 구현이라 버그가 없을 수 있다. 테스트로 확인하고, 없으면 테스트만 커밋한다.

**Files:**
- Test: `relay/channel/openai/chat_via_responses_dedupe_test.go` (새 파일)
- Modify (필요 시): `relay/channel/openai/chat_via_responses.go:233-290, 395-425`

- [ ] **Step 1: 테스트 작성**

SSE 순서: `response.output_item.added`(function_call, item id `fc_1`, call_id `call_1`, name `get_weather`) → `response.function_call_arguments.delta`(`{"city":`) → `response.function_call_arguments.done`(`{"city":"Seoul"}`) → `response.output_item.done` → `response.completed`(output 에 같은 function_call 포함, usage 포함). 클라이언트로 나간 Chat chunk 들을 모아 `tool_calls` 의 `index` 별 `arguments` 를 이어 붙였을 때 정확히 `{"city":"Seoul"}` 한 번이어야 하고, 서로 다른 `index` 가 2개 생기면 실패.

- [ ] **Step 2: 실행**

Run: `go test ./relay/channel/openai/ -run Dedupe -count=1`
Expected: PASS 면 Step 3 건너뛰고 Step 4 로. FAIL 이면 `response.completed` 처리에서 `toolCallCanonicalIDByItemID` / `toolCallIndexByID` 를 먼저 조회해 기존 index 를 재사용하도록 고친다 (upstream 과 같은 의도).

- [ ] **Step 3: (FAIL 인 경우) 구현 후 재실행**

Run: `go test ./relay/channel/openai/ -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add relay/channel/openai
git commit -m "test(relay): pin single tool call emission across Responses-to-Chat stream completion

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 1086038f5)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```
(코드를 고쳤으면 subject 를 `fix(relay): prevent duplicate tool calls in Responses-to-Chat streaming` 으로.)

---

### Task 12: 이미지 수 payload 기준 계산, output_tokens_details → img_o

upstream: `07578faf2`, `e2bb05f70` (`relay/channel/openai/relay_image.go`). team 은 `relay/channel/openai/relay-openai.go:590-840` (`OpenaiImageStreamHandler`, `OpenaiImageJSONAsStreamHandler`, 비스트림 이미지 처리 832행 `len(imageResp.Data)`).

**Files:**
- Modify: `relay/channel/openai/relay-openai.go`
- Test: `relay/channel/openai/image_stream_test.go`, `relay/channel/openai/relay_openai_image_usage_test.go`

- [ ] **Step 1: upstream 테스트 이식**

Run: `git show 07578faf2 e2bb05f70 -- relay/channel/openai/image_stream_test.go`
케이스: object 형 `data` 1장, url+b64_json 분리 2항목 → 1장, `revised_prompt` 만 → 0장(요청 수량 유지), SSE fallback 이 이미지당 1 completed 이벤트, `output_tokens_details.image_tokens` 가 `CompletionTokenDetails.ImageTokens` 로.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/openai/ -run 'ImageCount|ImageResponseCount|OutputTokensDetails|ImgO' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

upstream `openaiImageResponseCount`, `openaiImageDataHasField` 를 `relay-openai.go` 에 그대로 옮기고(`gjson` 사용, `max` 는 Go 1.21+ 내장), `gjson.GetBytes(responseBody, "data.#")` 와 `len(imageResp.Data)` 로 세던 자리를 모두 `openaiImageResponseCount(responseBody)` 로 바꾼다. JSON→SSE fallback 루프는 `gjson.GetBytes(responseBody, "data").Array()` 를 돌며 url/b64_json 없는 항목을 건너뛰고 `emitted` 수를 `info.ReceivedResponseCount` 에 더한다. usage 매핑 함수(약 600행, `usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens` 근처)에 `if usage.OutputTokensDetails != nil { usage.CompletionTokenDetails = *usage.OutputTokensDetails }` 추가. `dto.Usage` 에 `OutputTokensDetails` 가 없으면 `dto/openai_response.go` 에 `OutputTokensDetails *CompletionTokenDetails \`json:"output_tokens_details,omitempty"\`` 를 추가한다.

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/openai/ ./relay/ ./service/ -run 'Image|img' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/openai dto
git commit -m "fix(billing): count upstream images by payload and map output_tokens_details to img_o

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 07578faf2)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse e2bb05f70)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 13: Gemini 스트림 usage 누적 여부 확인 (회귀 테스트만)

upstream: `78bd5b1cb` 의 `usage_merge.go`. team 의 `geminiStreamHandler` 는 프레임마다 `*usage = mappedUsage` 로 교체하므로 누적 버그가 없다. 그 사실을 테스트로 고정한다.

**Files:**
- Test: `relay/channel/gemini/relay_gemini_usage_test.go`

- [ ] **Step 1: 테스트 추가**

`usageMetadata` 가 `promptTokensDetails:[{modality:"IMAGE",tokenCount:1089}]` 를 담은 프레임 3개를 가진 SSE 를 `GeminiChatStreamHandler` 에 통과시켜 최종 `usage.PromptTokensDetails.ImageTokens == 1089` (3267 이 아님) 임을 확인. 기존 파일의 `httptest` 패턴을 따른다.

- [ ] **Step 2: 실행**

Run: `go test ./relay/channel/gemini/ -run Cumulative -count=1`
Expected: PASS. FAIL 이면 `buildUsageFromGeminiMetadata` 호출 결과를 누적하는 코드가 생긴 것이므로 교체 방식으로 고친다.

- [ ] **Step 3: Commit**

```bash
git add relay/channel/gemini
git commit -m "test(gemini): pin cumulative stream usage snapshots to the last frame

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 78bd5b1cb)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 14: 업스트림 응답 헤더 대기 시간 bound

upstream: `b518d0033`. team 의 `service/http_client.go:36` `InitHttpClient` 는 `&http.Transport{...}` 리터럴을 직접 만든다.

**Files:**
- Modify: `common/constants.go` (`RelayIdleConnTimeout` 근처), `common/init.go` (`InitEnv`), `service/http_client.go`, `.env.example`
- Test: `service/http_client_response_header_timeout_test.go` (새 파일)

- [ ] **Step 1: 실패하는 테스트 작성**

```go
func TestRelayTransportBoundsResponseHeaderWait(t *testing.T) {
	old := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = old })

	common.RelayResponseHeaderTimeout = 1
	tr := newRelayTransport() // Step 3 에서 InitHttpClient 에서 추출
	require.Equal(t, time.Second, tr.ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = 0
	require.Zero(t, newRelayTransport().ResponseHeaderTimeout)

	common.RelayResponseHeaderTimeout = math.MaxInt
	require.Equal(t, time.Duration(maxTimeoutSeconds)*time.Second, newRelayTransport().ResponseHeaderTimeout)
}
```
그리고 실제 동작 테스트: 헤더를 보내지 않고 2초 대기하는 `httptest.Server` 에 `RelayResponseHeaderTimeout=1` 로 요청하면 1초 안에 에러가 나고, 헤더를 바로 보낸 뒤 본문을 2초에 걸쳐 흘리는 서버는 성공해야 한다 (Review Focus 5).

- [ ] **Step 2: 실패 확인**

Run: `go test ./service/ -run ResponseHeader -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

upstream diff(`git show b518d0033 -- common service`) 를 그대로 옮긴다: `common.RelayResponseHeaderTimeout int` 변수, `InitEnv` 에 `GetEnvOrDefault("RELAY_RESPONSE_HEADER_TIMEOUT", 1800)`, `service/http_client.go` 의 transport 생성을 `newRelayTransport() *http.Transport` 함수로 추출하고 `maxTimeoutSeconds` 클램프 후 `transport.ResponseHeaderTimeout` 설정. `.env.example` 에 upstream 과 같은 4줄 주석 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./service/ ./common/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add common service .env.example
git commit -m "fix(relay): bound the wait for upstream response headers

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse b518d0033)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 15: 비동기 task 제출 시 2xx 전부 성공 처리

upstream: `d61d6be75`.

**Files:**
- Modify: `relay/relay_task.go:220`
- Test: `relay/relay_task_test.go`

- [ ] **Step 1: 테스트 추가**

upstream `relay/relay_task_test.go` 의 회귀 테스트(201/202 → 성공, 400/502 → `fail_to_fetch_task` 에 status 유지)를 team 의 task adaptor 인터페이스에 맞춰 옮긴다. team 에는 플러그인 시스템이 없으므로 `httptest.Server` 로 업스트림을 흉내 내고 기존 `TaskAdaptor` 구현 중 가장 단순한 것(예: `relay/channel/task/suno`)을 사용한다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/ -run TaskSubmit -count=1`
Expected: FAIL (201 이 실패로 처리됨)

- [ ] **Step 3: 구현**

```go
if resp != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/relay_task.go relay/relay_task_test.go
git commit -m "fix(task): accept any 2xx upstream status on task submission

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse d61d6be75)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 16: 모든 릴레이 요청에 Request.GetBody 제공 (HTTP/2 재시도)

upstream: `d6b5ce99d`. team 은 `DoTaskApiRequest` 에만 `GetBody` 가 있고, 그것도 소비된 reader 를 `NopCloser` 로 돌려주는 잘못된 구현이다.

**Files:**
- Modify: `common/body_storage.go`, `relay/channel/api_request.go:307-560` (`DoApiRequest`, `DoTaskApiRequest`, `doRequest`), `relay/common/relay_info.go` 또는 `relay/common/outbound_body.go`(team 에 있으면), 각 handler(`relay/*_handler.go`)에서 요청 본문을 `io.Reader` 로 넘기는 자리
- Test: `relay/channel/api_request_getbody_test.go`, `relay/common/outbound_body_test.go` (upstream 에서 이식)

- [ ] **Step 1: upstream 변경 범위 파악**

Run: `git show d6b5ce99d --stat && git show d6b5ce99d -- common/body_storage.go relay/channel/api_request.go relay/common/outbound_body.go relay/common/relay_info.go`
핵심: 요청 본문이 `BodyStorage` 에서 왔으면 `req.GetBody = storage 의 새 reader 반환`, `bytes.Reader`/`strings.Reader` 면 `net/http` 가 알아서 하므로 손대지 않음, 그 외 타입이면 nil 유지. team 에 `relay/common/outbound_body.go` 가 없으면 `RelayInfo` 에 `OutboundBodyStorage common.BodyStorage` 필드(또는 upstream 의 `SetOutboundBody`)를 추가한다.

- [ ] **Step 2: upstream 테스트 이식 후 실패 확인**

`api_request_getbody_test.go` 의 케이스 중 플러그인/jimeng/sora 전용은 제외. 핵심 케이스: BodyStorage 기반 요청의 `req.GetBody()` 를 두 번 호출해 같은 바이트를 두 번 얻는다; `GetBody` 로 다시 읽어도 원 reader 를 소비하지 않는다; 리다이렉트 후 재전송 가능.
Run: `go test ./relay/channel/ ./relay/common/ -run 'GetBody|OutboundBody' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

upstream diff 를 team 파일에 적용. `DoTaskApiRequest` 의 잘못된 `GetBody` 는 제거하고 공통 헬퍼(`setRequestGetBody(req, requestBody, info)`)로 통일.

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/... ./common/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add common relay
git commit -m "fix(relay): set Request.GetBody so HTTP/2 can retry after an upstream stream reset

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse d6b5ce99d)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 17: 클라이언트 끊김 시 Bedrock 요청 취소

upstream: `bd585d78e`. team 의 `relay/channel/aws/relay-aws.go:43` `newAwsInvokeContext()` 는 `context.Background()` 를 쓴다.

**Files:**
- Modify: `relay/channel/aws/relay-aws.go:43-48, 224-260`
- Test: `relay/channel/aws/relay_aws_test.go`

- [ ] **Step 1: upstream 테스트 이식**

Run: `git show bd585d78e -- relay/channel/aws/relay_aws_test.go`
케이스: `c.Request.Context()` 가 취소되면 InvokeModel/InvokeModelWithResponseStream 에 넘긴 ctx 도 취소된다; `RelayTimeout>0` 이면 타임아웃도 함께 적용. `service/billing_usage.go` 쪽 변경("log effective usage billing path")은 relaykit 전제라 제외.

- [ ] **Step 2: 실패 확인**

Run: `go test ./relay/channel/aws/ -run 'Cancel|Disconnect' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

```go
func newAwsInvokeContext(c *gin.Context) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if c != nil && c.Request != nil {
		parent = c.Request.Context()
	}
	if common.RelayTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, time.Duration(common.RelayTimeout)*time.Second)
}
```
호출부 2곳(`awsHandler`, `awsStreamHandler`)에 `c` 전달.

- [ ] **Step 4: 통과 확인**

Run: `go test ./relay/channel/aws/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add relay/channel/aws
git commit -m "fix(aws): cancel Bedrock requests when the client disconnects

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse bd585d78e)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 18: Qwen thinking_budget passthrough

upstream: `66ee6b8f9`. team `dto/openai_request.go` 에는 `EnableThinking` 만 있고 `thinking_budget` 필드가 없다.

**Files:**
- Modify: `dto/openai_request.go` (`GeneralOpenAIRequest` 92행 근처, `OpenAIResponsesRequest` 881행 근처), `relay/channel/ali/text.go`, `relay/channel/ali/adaptor.go`, `service/openaicompat/chat_to_responses.go`, `service/openaicompat/responses_to_chat.go`, 그리고 upstream 이 한 줄씩 지운 어댑터들(`baidu`, `cloudflare`, `cohere`, `dify`, `jina`, `mistral`, `mokaai`, `palm`, `tencent`, `xunfei`, `zhipu`)
- Test: `dto/openai_request_zero_value_test.go`, `relay/channel/ali/adaptor_test.go`

- [ ] **Step 1: upstream diff 와 테스트 확인**

Run: `git show 66ee6b8f9 -- relaykit/dto/openai_request.go relay/channel/ali/text.go relay/channel/baidu/adaptor.go relaykit/dto/openai_request_zero_value_test.go relay/channel/ali/adaptor_test.go`
각 어댑터에서 지운 한 줄이 무엇인지 확인하고(아마 `request.ThinkingBudget` 또는 `EnableThinking` 초기화), 테스트 케이스(`thinking_budget: 0` 명시값 보존 포함, Rule 6)를 team 경로로 옮긴다.

- [ ] **Step 2: 실패 확인**

Run: `go test ./dto/ ./relay/channel/ali/ -run 'ThinkingBudget' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

`GeneralOpenAIRequest` 와 `OpenAIResponsesRequest` 에 `ThinkingBudget json.RawMessage \`json:"thinking_budget,omitempty"\`` 추가 (RawMessage 라 `0` 도 보존). Ali `text.go` 의 요청 변환에서 그대로 복사. chat↔responses 변환 양방향에서 복사. upstream 이 지운 어댑터 줄을 team 에서도 지운다(해당 줄이 team 에 있을 때만).

- [ ] **Step 4: 통과 확인**

Run: `go test ./dto/ ./relay/channel/... ./service/openaicompat/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add dto relay/channel service/openaicompat
git commit -m "fix(ali): preserve Qwen thinking_budget passthrough

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 66ee6b8f9)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 19: 1단계 기록 및 전체 테스트

**Files:**
- Create: `docs/2026-10-13-upstream-sync-phase1.md`

- [ ] **Step 1: 전체 테스트**

Run: `go build ./... && go vet ./... && go test ./... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort > /tmp/after.txt; diff <(grep FAIL docs/superpowers/plans/2026-10-13-upstream-sync-baseline.txt | sort) <(grep FAIL /tmp/after.txt)`
Expected: diff 출력 없음 (베이스라인 이외의 새 FAIL 없음).

- [ ] **Step 2: 기록 문서 작성**

`docs/2026-08-24-upstream-quota-integration-review.md` 와 같은 형식(검토 범위 / 병합된 커밋 / 반영 후 동작 / 수행한 검증 / 보류된 TODO)으로 Task 1~18 의 커밋 해시와 upstream 해시 표를 쓴다. 보류 TODO 에 relaykit 전제 항목(`@` modifier, hosted tool 변환, Responses WebSocket)을 명시.

- [ ] **Step 3: Commit**

```bash
git add docs/2026-10-13-upstream-sync-phase1.md
git commit -m "docs: record upstream protocol and billing port (phase 1)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 20: 충전 전 지갑 수용량 검증과 정산 불가 주문 거부 (PayPal/Toss/조직 포함)

upstream: `47ba9d2c6`, `2a0ce3475`. upstream 은 epay/stripe/creem/waffo 만 있고, team 에는 PayPal, Toss, 조직 지갑 결제, 지갑 자동충전이 추가로 있다.

**Files:**
- Modify: `model/topup.go`, `controller/topup.go`, `controller/topup_stripe.go`, `controller/topup_creem.go`, `controller/topup_waffo.go`, `controller/topup_waffo_pancake.go`, `controller/topup_paypal.go` (`RequestPayPalPay` 278행), `controller/topup_toss.go` (`RequestTossPay`), `controller/organization.go` 의 `RequestOrganization*Pay`, `controller/wallet_auto_recharge.go`
- Test: `controller/topup_quota_limit_test.go`, `model/payment_method_guard_test.go` (upstream 에서 이식), `controller/topup_paypal_capacity_test.go`, `controller/topup_toss_capacity_test.go` (새 파일)

**Interfaces:**
- Produces: `model.ValidateTopUpQuotaCapacity(userId int, creditedQuota int64) error` (현재 quota + 적립 quota 가 `common.MaxQuota` 를 넘으면 에러), `controller.validateTopUpQuota(amount int64) (creditedQuota int64, err error)`, `controller.rejectInvalidTopUpQuota(c, userId, amount) bool`. 조직 지갑은 `model.ValidateOrganizationWalletCapacity(orgId int, creditedQuota int64) error`.

- [ ] **Step 1: upstream diff 와 테스트 읽기**

Run: `git show 47ba9d2c6 2a0ce3475 -- controller/topup.go model/topup.go controller/topup_quota_limit_test.go model/payment_method_guard_test.go`
team 의 `controller/topup.go` 에는 `validateTopUpQuota` 가 없다(8월 포팅 `50e5377ea` 는 model 쪽만 가져옴). 함수명은 upstream 을 따른다.

- [ ] **Step 2: 테스트 이식 + PayPal/Toss/조직 케이스 추가 후 실패 확인**

PayPal/Toss 케이스: 사용자 quota 가 `common.MaxQuota - 1` 인 상태에서 `RequestPayPalPay` / `RequestTossPay` 를 호출하면 주문 생성 전에 400 으로 거부되고 `topups` 행이 생기지 않는다. 조직: `RequestOrganizationTossPay` 도 동일. 자동충전: `RequestWalletThresholdRecharge` 에서 임계값+충전액이 상한을 넘으면 거부.
Run: `go test ./controller/ ./model/ -run 'TopUpQuota|Capacity|Uncreditable' -count=1`
Expected: FAIL

- [ ] **Step 3: 구현**

upstream 코드를 옮기되 금액→quota 변환은 team 의 각 provider 함수(`getPayPalPayMoney`, Toss 의 quote 계산 `TossTopUpQuote`)를 그대로 써서 "정산 시 적립될 quota" 를 먼저 계산하고 `ValidateTopUpQuotaCapacity` 를 호출한다. 조직 지갑은 `model/organization.go` 의 지갑 quota 컬럼을 읽는 `ValidateOrganizationWalletCapacity` 를 추가. 모든 DB 접근은 GORM (Rule 2).

- [ ] **Step 4: 통과 확인**

Run: `go test ./controller/ ./model/ -count=1`
Expected: PASS (베이스라인 외 실패 없음)

- [ ] **Step 5: Commit**

```bash
git add controller model
git commit -m "fix(topup): reject uncreditable orders and guard wallet capacity before payment

Covers epay, Stripe, Creem, Waffo like upstream, plus this fork's PayPal,
Toss, organization wallet and auto-recharge entry points.

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 47ba9d2c6)
Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 2a0ce3475)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 21: pre-consume multiplier 와 trust threshold 옵션

upstream: `9978ee1e2`. `common.PreConsumedQuota`(500 토큰 고정)와 `common.GetTrustQuota()`(10 USD 고정)를 운영 옵션으로 바꾼다.

**Files:**
- Modify: `setting/operation_setting/quota_setting.go`, `relay/helper/price.go:100-140`, `service/billing_session.go:320-340` (`shouldTrust`), `common/quota.go` (삭제), `common/constants.go` (`PreConsumedQuota` 제거), `model/option.go` (옵션 키 등록), `pkg/billingexpr/expr.md` (Rule 7: 문서 갱신), `web/default/src/features/system-settings/general/quota-settings-section.tsx`, `web/default/src/features/system-settings/types.ts`, `web/default/src/i18n/locales/{en,zh,ko}.json`
- Test: `relay/helper/price_test.go`, `controller/billing_option_test.go` (upstream 이식), `service/quota_preconsume_test.go`

**Interfaces:**
- Produces: `operation_setting.QuotaSetting{ PreConsumeMultiplier float64 \`json:"pre_consume_multiplier"\`; TrustQuotaUSD float64 \`json:"trust_quota_usd"\` }`, `operation_setting.InputPreConsumeMultiplier() (float64, error)` (기본 1, 양의 유한 실수만), `operation_setting.TrustQuota() int64` (USD × QuotaPerUnit, 0 이면 bypass 없음), `operation_setting.ValidateQuotaOption(key, value string) error`.

- [ ] **Step 1: upstream diff 읽기**

Run: `git show 9978ee1e2 -- setting/operation_setting/quota_setting.go relay/helper/price.go service/billing_session.go common controller/billing_option_test.go relay/helper/price_test.go pkg/billingexpr/expr.md`

- [ ] **Step 2: 테스트 이식 후 실패 확인**

케이스: multiplier 1.5, prompt 1000 → 예약 토큰 1500 (MaxTokens 는 더하지 않음, upstream 이 출력 추정을 제거); multiplier 0/음수/NaN/Inf 는 옵션 저장 시 거부; `trust_quota_usd=0` 이면 `shouldTrust` 가 항상 false; 옵션 미설정 시 기본값(1, 10).
Run: `go test ./relay/helper/ ./service/ ./controller/ ./setting/operation_setting/ -run 'PreConsume|Trust' -count=1`
Expected: FAIL

- [ ] **Step 3: 백엔드 구현**

upstream 을 따라 `relay/helper/price.go` 의
```go
preConsumedTokens := common.Max(promptTokens, common.PreConsumedQuota)
if meta.MaxTokens > 0 { preConsumedTokens += meta.MaxTokens }
```
를
```go
preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
if err != nil { return ..., err }
preConsumedTokens := float64(promptTokens) * preConsumeMultiplier
```
로 바꾼다. **주의:** team 의 tiered 표현식 pre-consume(`c0323dfe0` "default token estimate for tiered expression pre-consume")과 조직 지갑 pre-consume 경로도 같은 함수를 타는지 `grep -rn PreConsumedQuota` 로 확인하고 모두 치환한다. `shouldTrust` 는 `operation_setting.TrustQuota()` 사용. `pkg/billingexpr/expr.md` 의 pre-consume 설명을 upstream 문구로 갱신.

- [ ] **Step 4: 프론트 설정 UI**

`quota-settings-section.tsx` 에 두 입력(`pre_consume_multiplier` 숫자, `trust_quota_usd` 숫자)을 기존 `enable_free_model_pre_consume` 토글 옆에 추가하고 `types.ts` 의 quota 설정 타입에 필드를 넣는다. 라벨 키는 영어 원문을 키로 쓰고 `bun run i18n:sync` 로 ko/zh 를 채운다(`web/default` 에서).
Run: `cd web/default && bun run typecheck && bun run i18n:sync`
Expected: 타입 에러 없음, 누락 키 없음.

- [ ] **Step 5: 통과 확인**

Run: `go test ./relay/... ./service/ ./controller/ ./setting/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add setting relay/helper service common model pkg/billingexpr/expr.md web/default
git commit -m "feat(billing): configure trust threshold and input pre-consume multiplier

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse 9978ee1e2)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 22: int32 폐기 변경과 team bigint 확장 대조

upstream: `a073f74b3`. team 은 `docs/quota-ceiling-and-bigint-migration.md` 로 이미 bigint 확장을 했지만 `common/quota_math.go` 의 `QuotaFromFloat*` 는 여전히 `math.MaxInt32` 에서 clamp 한다(`common.MaxQuota` 는 5e14). 두 구현을 맞춘다.

**Files:**
- Modify: `common/quota_math.go`, `common/quota_math_test.go`, `middleware/model-rate-limit.go`, `setting/rate_limit.go`, `controller/redemption.go`, `controller/token.go`, `model/redemption.go`, `model/utils.go`, `model/main.go`(legacy 스키마 검사)
- Test: `common/quota_math_test.go`, `service/quota_saturation_test.go`, `model/quota_reserve_test.go`

- [ ] **Step 1: diff 대조표 작성**

Run: `git show a073f74b3 --stat; git show a073f74b3 -- common/quota_math.go middleware/model-rate-limit.go setting/rate_limit.go model/main.go model/utils.go controller/redemption.go controller/token.go`
각 hunk 에 대해 team 에 (a) 이미 동등한 코드가 있음 / (b) 없음 / (c) team 설계와 충돌 중 하나로 분류해 Task 23 의 기록 문서에 표로 남긴다. 판단 기준: team 은 `common.MaxQuota` 를 단일 상한으로 쓴다. upstream 의 `math.MaxInt64` 기반 상한보다 team 상한이 작으므로 team 상한을 유지한다.

- [ ] **Step 2: 실패하는 테스트 작성**

```go
func TestQuotaFromFloatSaturatesAtMaxQuota(t *testing.T) {
	require.Equal(t, int(common.MaxQuota), common.QuotaFromFloat(float64(common.MaxQuota)*2))
	v, err := common.QuotaFromFloatStrict(float64(common.MaxQuota) * 2)
	require.Error(t, err)
	require.Zero(t, v)
	v, err = common.QuotaFromFloatStrict(3_000_000_000) // > MaxInt32, < MaxQuota
	require.NoError(t, err)
	require.Equal(t, 3_000_000_000, v)
}
```
rate limit: `setting/rate_limit.go` 의 count × duration 가 int64 wrap 되지 않음 (upstream 테스트 이식). redemption/token: 생성 시 quota 가 `MaxQuota` 를 넘으면 400.

- [ ] **Step 3: 실패 확인**

Run: `go test ./common/ ./setting/ ./controller/ -run 'Saturat|Wrap|MaxQuota' -count=1`
Expected: FAIL (`QuotaFromFloatStrict(3e9)` 가 에러)

- [ ] **Step 4: 구현**

`common/quota_math.go` 의 `math.MaxInt32`/`MinInt32` 를 `MaxQuota`/`-MaxQuota` 로. (b) 로 분류한 hunk 를 옮긴다. (c) 는 옮기지 않고 기록만. `model/main.go` 의 "legacy user quota schema 거부" 는 team 마이그레이션(`quota-ceiling-and-bigint-migration`)이 이미 컬럼을 bigint 로 바꾸므로, 기동 시 `users.quota` 컬럼 타입이 bigint 가 아니면 경고만 남기는 수준으로 축소(세 DB 모두에서 컬럼 타입 조회는 GORM `Migrator().ColumnTypes`).

- [ ] **Step 5: 통과 확인**

Run: `go test ./common/ ./setting/ ./controller/ ./model/ ./service/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add common setting controller model service
git commit -m "fix(quota): align saturating conversions and bounds with the bigint quota ceiling

Reconciles upstream's int32 removal with this fork's MaxQuota ceiling.

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse a073f74b3)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 23: tokenkit 모듈 도입

upstream: `bb28f2cca`. `tokenkit/` 은 독립 Go 모듈(`replace github.com/QuantumNous/new-api/tokenkit => ./tokenkit`)이다. team 의 `service/token_counter.go`, `token_estimator.go`, `tokenizer.go` 를 tokenkit 호출로 바꾼다. team 이 `service/token_counter.go` 에 넣은 자체 변경(`git log main..team -- service/token_counter.go`)은 유지한다.

**Files:**
- Create: `tokenkit/` (upstream `qn/main:tokenkit/` 복사: `go.mod`, `go.sum`, `tokenkit.go`, `tokenizer.go`, `image.go`, `README.md`, 테스트)
- Modify: `go.mod`, `go.sum`, `Dockerfile`, `Dockerfile.dev`, `makefile`, `service/token_counter.go`, `service/token_estimator.go`(삭제), `service/tokenizer.go`(삭제), `dto/claude.go`, `dto/gemini.go`, `dto/openai_request.go` (upstream 이 추가한 `TokenCountMeta` 생성 메서드: tool 정의/호출/결과를 포함), `relay/channel/claude/relay-claude.go`, `relay/channel/gemini/relay-gemini.go`, `relay/channel/openai/relay-openai.go` (completion fallback 에 tool call 포함)
- Test: `tokenkit/*_test.go`, `dto/token_count_meta_test.go` (이식), `service/token_counter_test.go`

- [ ] **Step 1: 모듈 복사와 go.mod**

```bash
git archive qn/main tokenkit | tar -x -C .
sed -n '/^require (/,/^)/p' go.mod | head -3   # 위치 확인
```
`go.mod` 에 `github.com/QuantumNous/new-api/tokenkit v0.0.0` require 와 `replace ... => ./tokenkit` 추가 (upstream `git show bb28f2cca -- go.mod` 참고). `Dockerfile`/`Dockerfile.dev`/`makefile` 의 `COPY tokenkit` 변경을 옮긴다.
Run: `go mod tidy && go build ./tokenkit/... && go test ./tokenkit/ -count=1`
Expected: PASS

- [ ] **Step 2: team token_counter 의 자체 변경 확인**

Run: `git log --format='%h %s' main..team -- service/token_counter.go service/token_estimator.go; git diff main...team -- service/token_counter.go | head -80`
team 전용 변경(있다면 조직 과금, 타이밍 등)을 메모해 Step 4 에서 보존한다.

- [ ] **Step 3: upstream 테스트 이식 후 실패 확인**

Run: `git show bb28f2cca -- relaykit/dto/token_count_meta_test.go relay/channel/claude/relay_claude_test.go relay/channel/gemini/relay_gemini_usage_test.go`
`relaykit/dto` → `dto`. 케이스: Claude tool 정의가 prompt 추정에 포함, Responses function_call/출력 포함, Gemini systemInstruction/functionCall/functionResponse/tools 포함, Claude 4.7+ 가 다른 가중치, completion fallback 이 tool_use 블록당 40 토큰 가산.
Run: `go test ./dto/ ./relay/channel/claude/ ./relay/channel/gemini/ -run 'TokenCount|Estimate' -count=1`
Expected: FAIL

- [ ] **Step 4: 구현**

upstream diff(`git show bb28f2cca -- service/token_counter.go relaykit/dto/claude.go relaykit/dto/gemini.go relaykit/dto/openai_request.go relay/channel`)를 team 경로에 적용. `service/token_estimator.go`, `service/tokenizer.go` 삭제, `common/model.go` 의 tokenizer 초기화 제거, `main.go` 에서 호출 제거. Step 2 의 team 전용 변경은 새 코드에 다시 넣는다.

- [ ] **Step 5: 통과 확인 + 추정치 변화 기록**

Run: `go build ./... && go test ./service/ ./dto/ ./relay/... -count=1`
Expected: PASS.
그리고 같은 입력에 대한 전/후 추정 토큰을 비교해 기록한다: Task 19 와 같은 형식의 기록 문서(Task 24)에 "Claude Code 스타일 요청(tool 정의 20개, 메시지 30개) 샘플 1건의 전/후 prompt 추정값" 표를 넣는다. 샘플은 `service/token_counter_test.go` 의 fixture 로 커밋.

- [ ] **Step 6: Commit**

```bash
git add tokenkit go.mod go.sum Dockerfile Dockerfile.dev makefile service dto relay common main.go
git commit -m "feat(tokenkit): move token counting into a module calibrated on agent traffic

Upstream: https://github.com/QuantumNous/new-api/commit/$(git rev-parse bb28f2cca)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 24: 2단계 기록, 전체 검증, 병합 준비

**Files:**
- Create: `docs/2026-10-13-upstream-sync-phase2.md`

- [ ] **Step 1: 전체 테스트와 베이스라인 비교**

Run: `go build ./... && go vet ./... && go test ./... -count=1 2>&1 | grep -E '^(--- FAIL|FAIL|ok)' | sort > /tmp/after2.txt; diff <(grep FAIL docs/superpowers/plans/2026-10-13-upstream-sync-baseline.txt | sort) <(grep FAIL /tmp/after2.txt); cd web/default && bun run typecheck && bun run build`
Expected: 새 FAIL 없음, 프론트 빌드 성공.

- [ ] **Step 2: 기록 문서**

Task 20~23 의 커밋/upstream 해시 표, Task 22 의 대조표, Task 23 의 추정치 전/후 표, 운영 전제(새 환경변수 `RELAY_RESPONSE_HEADER_TIMEOUT`, 새 옵션 `pre_consume_multiplier`/`trust_quota_usd` 기본값), 보류 TODO(relaykit, `@` modifier, task 플러그인, Responses WebSocket, 인증/i18n 재편).

- [ ] **Step 3: Commit 과 병합 안내**

```bash
git add docs/2026-10-13-upstream-sync-phase2.md
git commit -m "docs: record upstream billing and token-counting port (phase 2)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
git log --oneline team..HEAD
```
사용자에게 `team` 병합 여부를 확인받는다. 병합은 `git checkout team && git merge --no-ff codex/upstream-sync-2026-10`.
