package claude

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Ported from team relay_claude_thinking_test.go (9088a5703). team converted
// in RequestOpenAI2ClaudeMessage; develop strips model suffixes in the relay
// handler (ModelMappedHelper, ApplyReasoningModelSuffix) and converts through
// relaykit, so the helper runs those steps and then the adaptor.
func convertOpenAIToClaudeFork(t *testing.T, request dto.GeneralOpenAIRequest) *dto.ClaudeRequest {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if request.Messages == nil {
		request.Messages = []dto.Message{{Role: "user", Content: "hi"}}
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: request.Model,
		Request:         &request,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: request.Model},
	}
	outbound, err := common.DeepCopy(&request)
	require.NoError(t, err)
	require.NoError(t, helper.ModelMappedHelper(c, info, outbound))
	require.NoError(t, helper.ApplyReasoningModelSuffix(c, info, outbound))
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, outbound)
	require.NoError(t, err)
	claudeRequest, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok, "converted request is %T", converted)
	return claudeRequest
}

func requireNoSamplingFork(t *testing.T, r *dto.ClaudeRequest) {
	t.Helper()
	require.Nil(t, r.Temperature)
	require.Nil(t, r.TopP)
	require.Nil(t, r.TopK)
}

// Effort suffixes build adaptive thinking; Opus 4.7+ defaults display to
// "omitted", so the relayed request asks for the summary.
func TestOpenAIToClaudeAdaptiveThinkingIsSummarizedFork(t *testing.T) {
	for _, model := range []string{"claude-opus-4-6-high", "claude-opus-4-7-high"} {
		t.Run(model, func(t *testing.T) {
			r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: model})
			require.NotNil(t, r.Thinking)
			require.Equal(t, "adaptive", r.Thinking.Type)
			require.Equal(t, "summarized", r.Thinking.Display)
		})
	}
}

// Budget-based thinking returns full thinking text already and rejects display.
// team reached budget thinking with reasoning_effort on Opus 4.6; relaykit
// renders adaptive thinking there unless the client sends an explicit budget,
// so the budget comes from OpenRouter reasoning.max_tokens.
func TestOpenAIToClaudeEnabledThinkingHasNoDisplayFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-4-6",
		MaxTokens: common.GetPointer[uint](10000),
		Reasoning: json.RawMessage(`{"max_tokens":3000}`),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Equal(t, 3000, r.Thinking.GetBudgetTokens())
	require.Equal(t, "", r.Thinking.Display)
}

// develop: Opus 4.6 takes adaptive thinking, so a bare reasoning_effort is
// sent as adaptive at that effort (team sent budget thinking).
func TestOpenAIToClaudeOpus46ReasoningEffortIsAdaptiveFork(t *testing.T) {
	for _, effort := range []string{"low", "high"} {
		r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: "claude-opus-4-6", ReasoningEffort: effort})
		require.NotNil(t, r.Thinking, effort)
		require.Equal(t, "adaptive", r.Thinking.Type, effort)
		require.Nil(t, r.Thinking.BudgetTokens, effort)
		require.Equal(t, "summarized", r.Thinking.Display, effort)
		require.Equal(t, effort, r.GetEfforts(), effort)
	}
}

// A model that takes a lower effort than the suffix asks for is sent that
// lower effort, not rejected.
func TestOpenAIToClaudeEffortSuffixIsCappedToModelFork(t *testing.T) {
	cases := []struct{ model, wantEffort string }{
		{"claude-opus-4-6-xhigh", "max"},
		{"claude-opus-4-7-xhigh", "xhigh"},
		{"claude-opus-4-7-minimal", "low"},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: c.model})
			require.Equal(t, c.wantEffort, r.GetEfforts())
		})
	}
}

// Claude 5.x rejects budget thinking: reasoning_effort becomes adaptive
// thinking at that effort, and strict-sampling models lose temperature/top_p/top_k.
func TestOpenAIToClaudeClaude5ReasoningEffortIsAdaptiveFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:           "claude-opus-5-5",
		ReasoningEffort: "high",
		Temperature:     common.GetPointer(0.7),
		TopP:            common.GetPointer(0.9),
		TopK:            common.GetPointer(40),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "summarized", r.Thinking.Display)
	require.Equal(t, "high", r.GetEfforts())
	requireNoSamplingFork(t, r)
}

// Manual-only models keep budget thinking from reasoning_effort: low is 20%
// of max_tokens. team used Opus 4.6, which develop renders adaptive (see
// above); Sonnet 4.5 has no adaptive thinking.
func TestOpenAIToClaudeManualReasoningEffortKeepsBudgetFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: "claude-sonnet-4-5", ReasoningEffort: "low"})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.NotNil(t, r.MaxTokens)
	require.Equal(t, int(*r.MaxTokens)*20/100, r.Thinking.GetBudgetTokens())
	require.Empty(t, r.OutputConfig)
}

// The -thinking suffix on Claude 5.x renders adaptive at the model's default
// effort and is trimmed from the model name.
func TestOpenAIToClaudeClaude5ThinkingSuffixFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:       "claude-sonnet-5-5-thinking",
		Temperature: common.GetPointer(0.5),
	})
	require.Equal(t, "claude-sonnet-5-5", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "high", r.GetEfforts())
	requireNoSamplingFork(t, r)
}

// Effort suffixes are expanded for every model in the effort table.
func TestOpenAIToClaudeClaude5EffortSuffixFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: "claude-sonnet-5-5-xhigh"})
	require.Equal(t, "claude-sonnet-5-5", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "xhigh", r.GetEfforts())
	requireNoSamplingFork(t, r)
}

// An OpenRouter budget on Claude 5.x becomes the matching effort.
func TestOpenAIToClaudeClaude5OpenRouterBudgetFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:     "claude-haiku-5-5",
		Reasoning: json.RawMessage(`{"max_tokens":4000}`),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "medium", r.GetEfforts())
}

// An OpenRouter effort without max_tokens is honored as an effort.
func TestOpenAIToClaudeOpenRouterEffortFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-5-5",
		Reasoning: json.RawMessage(`{"effort":"low"}`),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "low", r.GetEfforts())

	// team ignored an OpenRouter effort on manual-capable and unknown models.
	// develop honors it: Opus 4.6 renders adaptive at that effort, and a model
	// without adaptive thinking gets budget thinking with temperature removed
	// (enabled thinking only takes the default temperature).
	r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-6",
		Reasoning:   json.RawMessage(`{"effort":"high"}`),
		Temperature: common.GetPointer(0.7),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "high", r.GetEfforts())

	r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:       "claude-3-5-sonnet-20241022",
		Reasoning:   json.RawMessage(`{"effort":"high"}`),
		Temperature: common.GetPointer(0.7),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Positive(t, r.Thinking.GetBudgetTokens())
	require.Empty(t, r.OutputConfig)
	require.Nil(t, r.Temperature)
}

// reasoning_effort "none" on a model that cannot turn thinking off runs it at
// its lowest setting instead of being rejected.
func TestOpenAIToClaudeReasoningEffortNoneFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: "claude-opus-5-5", ReasoningEffort: "none"})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	// team expected display "omitted" (the client asked for no thinking).
	// relaykit leaves display empty here and the fork display default
	// (NormalizeAdaptiveThinkingDisplay) fills "summarized"; display is not
	// asserted until that interaction is settled.
	require.Equal(t, "low", r.GetEfforts())

	r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: "claude-sonnet-5-5", ReasoningEffort: "none"})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "between_tools", r.Thinking.Type)
	require.Empty(t, r.OutputConfig)

	// team left thinking unset on models that can turn it off; relaykit sends
	// an explicit {"type":"disabled"}, which those models accept.
	for _, model := range []string{"claude-opus-4-6", "claude-opus-5"} {
		r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{Model: model, ReasoningEffort: "none"})
		require.NotNil(t, r.Thinking, model)
		require.Equal(t, "disabled", r.Thinking.Type, model)
		require.Empty(t, r.OutputConfig, model)
	}
}

// The -thinking suffix is trimmed. team kept Opus 4.6 on budget thinking at
// 80% of max_tokens with temperature 1; develop renders adaptive at the
// model's default effort because Opus 4.6 takes adaptive thinking. A model
// without adaptive thinking still gets the 80% budget.
func TestOpenAIToClaudeThinkingSuffixOnOpus46Fork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-4-6-thinking",
		MaxTokens: common.GetPointer[uint](10000),
	})
	require.Equal(t, "claude-opus-4-6", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "high", r.GetEfforts())

	r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:     "claude-sonnet-4-5-thinking",
		MaxTokens: common.GetPointer[uint](10000),
	})
	require.Equal(t, "claude-sonnet-4-5", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Equal(t, 8000, r.Thinking.GetBudgetTokens())
}

// Strict-sampling models lose temperature even without any thinking request,
// and thinking is left alone.
func TestOpenAIToClaudeStrictSamplingWithoutThinkingFork(t *testing.T) {
	r := convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:       "claude-opus-5",
		Temperature: common.GetPointer(0.7),
	})
	require.Nil(t, r.Thinking)
	require.Empty(t, r.OutputConfig)
	requireNoSamplingFork(t, r)

	r = convertOpenAIToClaudeFork(t, dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-6",
		Temperature: common.GetPointer(0.7),
	})
	require.NotNil(t, r.Temperature)
	require.Equal(t, 0.7, *r.Temperature)
}

// team RenderClaudeThinking(request, model, effort, budget) mutated a request;
// relaykit's RenderClaude returns the thinking and effort to send instead.
func TestRenderClaudeThinkingFork(t *testing.T) {
	// Manual-capable models get budget thinking.
	budget := 3000
	maxTokens := uint(8000)
	render, err := reasoning.RenderClaude("claude-opus-4-6", reasoning.Intent{Mode: reasoning.ModeEnabled, BudgetTokens: &budget}, &maxTokens, 0)
	require.NoError(t, err)
	require.Equal(t, "enabled", render.Thinking.Type)
	require.Equal(t, 3000, render.Thinking.GetBudgetTokens())
	require.Empty(t, render.OutputEffort)

	// Others get adaptive + effort; an explicit effort wins over the budget.
	budget = 2048
	render, err = reasoning.RenderClaude("claude-opus-5-5", reasoning.Intent{Mode: reasoning.ModeEnabled, Effort: reasoning.EffortMax, BudgetTokens: &budget}, nil, 0)
	require.NoError(t, err)
	require.Equal(t, "adaptive", render.Thinking.Type)
	require.Nil(t, render.Thinking.BudgetTokens)
	require.Equal(t, reasoning.EffortMax, render.OutputEffort)

	// No effort, budget -> effort from budget.
	budget = 500
	render, err = reasoning.RenderClaude("claude-opus-5", reasoning.Intent{Mode: reasoning.ModeEnabled, BudgetTokens: &budget}, nil, 0)
	require.NoError(t, err)
	require.Equal(t, "adaptive", render.Thinking.Type)
	require.Equal(t, reasoning.EffortLow, render.OutputEffort)

	// Neither -> the model's default effort.
	render, err = reasoning.RenderClaude("claude-opus-5-5", reasoning.Intent{Mode: reasoning.ModeEnabled}, nil, 0)
	require.NoError(t, err)
	require.Equal(t, "adaptive", render.Thinking.Type)
	require.Equal(t, reasoning.EffortMedium, render.OutputEffort)
}
