package claude

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The OpenAI-compatible path builds adaptive thinking for the effort suffixes.
// Anthropic defaults display to "omitted" from Opus 4.7 onwards, so without
// normalization the relayed client is billed for thinking it never receives.
func TestRequestOpenAI2ClaudeMessageAdaptiveThinkingIsSummarized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, model := range []string{"claude-opus-4-6-high", "claude-opus-4-7-high"} {
		t.Run(model, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			request := dto.GeneralOpenAIRequest{
				Model:    model,
				Messages: []dto.Message{{Role: "user", Content: "hi"}},
			}

			claudeRequest, err := RequestOpenAI2ClaudeMessage(c, request)
			require.NoError(t, err)
			require.NotNil(t, claudeRequest.Thinking)
			require.Equal(t, "adaptive", claudeRequest.Thinking.Type)
			require.Equal(t, "summarized", claudeRequest.Thinking.Display)
		})
	}
}

// Budget-based thinking returns full thinking text already and rejects display.
func TestRequestOpenAI2ClaudeMessageEnabledThinkingHasNoDisplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	request := dto.GeneralOpenAIRequest{
		Model:           "claude-opus-4-6",
		Messages:        []dto.Message{{Role: "user", Content: "hi"}},
		ReasoningEffort: "high",
	}

	claudeRequest, err := RequestOpenAI2ClaudeMessage(c, request)
	require.NoError(t, err)
	require.NotNil(t, claudeRequest.Thinking)
	require.Equal(t, "enabled", claudeRequest.Thinking.Type)
	require.Equal(t, "", claudeRequest.Thinking.Display)
}

func TestThinkingWithAdaptive(t *testing.T) {
	binding := json.RawMessage(`{"prefix_mismatch_behavior":"drop_block"}`)

	var nilThinking *dto.Thinking
	got := nilThinking.WithAdaptive()
	require.Equal(t, "adaptive", got.Type)
	require.Empty(t, got.BlockBinding)

	got = (&dto.Thinking{Type: "enabled", BudgetTokens: common.GetPointer(2048), Display: "omitted", BlockBinding: binding}).WithAdaptive()
	require.Equal(t, "adaptive", got.Type)
	require.Nil(t, got.BudgetTokens)
	require.Equal(t, "omitted", got.Display)
	require.JSONEq(t, string(binding), string(got.BlockBinding))
}

// A model that takes a lower effort than the suffix asks for is sent that
// lower effort, not rejected.
func TestRequestOpenAI2ClaudeMessageEffortSuffixIsCappedToModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct{ model, wantEffort string }{
		{"claude-opus-4-6-xhigh", "max"},
		{"claude-opus-4-7-xhigh", "xhigh"},
		{"claude-opus-4-7-minimal", "low"},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(nil)
			request := dto.GeneralOpenAIRequest{
				Model:    c.model,
				Messages: []dto.Message{{Role: "user", Content: "hi"}},
			}
			claudeRequest, err := RequestOpenAI2ClaudeMessage(ctx, request)
			require.NoError(t, err)
			require.Equal(t, c.wantEffort, claudeRequest.GetEfforts())
		})
	}
}

func convertOpenAIToClaudeForTest(t *testing.T, request dto.GeneralOpenAIRequest) *dto.ClaudeRequest {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	if request.Messages == nil {
		request.Messages = []dto.Message{{Role: "user", Content: "hi"}}
	}
	claudeRequest, err := RequestOpenAI2ClaudeMessage(c, request)
	require.NoError(t, err)
	return claudeRequest
}

func requireNoSampling(t *testing.T, r *dto.ClaudeRequest) {
	t.Helper()
	require.Nil(t, r.Temperature)
	require.Nil(t, r.TopP)
	require.Nil(t, r.TopK)
}

// (a) Claude 5.x rejects budget thinking: reasoning_effort becomes adaptive
// thinking at that effort, and strict-sampling models lose temperature/top_p/top_k.
func TestRequestOpenAI2ClaudeMessageClaude5ReasoningEffortIsAdaptive(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
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
	requireNoSampling(t, r)
}

// (b) Manual-capable models keep budget thinking from reasoning_effort.
func TestRequestOpenAI2ClaudeMessageManualReasoningEffortKeepsBudget(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:           "claude-opus-4-6",
		ReasoningEffort: "low",
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Equal(t, 1280, r.Thinking.GetBudgetTokens())
	require.Empty(t, r.OutputConfig)
}

// (c) The -thinking suffix on Claude 5.x renders adaptive at the model's
// default effort and is trimmed from the model name.
func TestRequestOpenAI2ClaudeMessageClaude5ThinkingSuffix(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:       "claude-sonnet-5-5-thinking",
		Temperature: common.GetPointer(0.5),
	})
	require.Equal(t, "claude-sonnet-5-5", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "high", r.GetEfforts())
	requireNoSampling(t, r)
}

// (d) Effort suffixes are expanded for every model in the effort table.
func TestRequestOpenAI2ClaudeMessageClaude5EffortSuffix(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{Model: "claude-sonnet-5-5-xhigh"})
	require.Equal(t, "claude-sonnet-5-5", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "xhigh", r.GetEfforts())
	requireNoSampling(t, r)
}

// (e) An openrouter budget on Claude 5.x becomes the matching effort.
func TestRequestOpenAI2ClaudeMessageClaude5OpenRouterBudget(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:     "claude-haiku-5-5",
		Reasoning: json.RawMessage(`{"max_tokens":4000}`),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.Equal(t, "medium", r.GetEfforts())
}

// An openrouter effort without max_tokens is honored as an effort.
func TestRequestOpenAI2ClaudeMessageOpenRouterEffort(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-5-5",
		Reasoning: json.RawMessage(`{"effort":"low"}`),
	})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "low", r.GetEfforts())

	// Manual-capable and unknown models ignore an openrouter effort, as
	// before: enabled thinking there would need temperature 1.
	for _, model := range []string{"claude-opus-4-6", "claude-3-5-sonnet-20241022"} {
		r = convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
			Model:       model,
			Reasoning:   json.RawMessage(`{"effort":"high"}`),
			Temperature: common.GetPointer(0.7),
		})
		require.Nil(t, r.Thinking, model)
		require.Empty(t, r.OutputConfig, model)
	}
}

// reasoning_effort "none" on a model that cannot turn thinking off runs it
// at its lowest setting instead of being rejected; other models are untouched.
func TestRequestOpenAI2ClaudeMessageReasoningEffortNone(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{Model: "claude-opus-5-5", ReasoningEffort: "none"})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Equal(t, "omitted", r.Thinking.Display)
	require.Equal(t, "low", r.GetEfforts())

	r = convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{Model: "claude-sonnet-5-5", ReasoningEffort: "none"})
	require.NotNil(t, r.Thinking)
	require.Equal(t, "between_tools", r.Thinking.Type)
	require.Empty(t, r.OutputConfig)

	for _, model := range []string{"claude-opus-4-6", "claude-opus-5"} {
		r = convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{Model: model, ReasoningEffort: "none"})
		require.Nil(t, r.Thinking, model)
		require.Empty(t, r.OutputConfig, model)
	}
}

// The existing Opus 4.6 -thinking behavior stays: budget at 80% of max_tokens.
func TestRequestOpenAI2ClaudeMessageManualThinkingSuffixKeepsBudget(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:     "claude-opus-4-6-thinking",
		MaxTokens: common.GetPointer[uint](10000),
	})
	require.Equal(t, "claude-opus-4-6", r.Model)
	require.NotNil(t, r.Thinking)
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Equal(t, 8000, r.Thinking.GetBudgetTokens())
	require.NotNil(t, r.Temperature)
	require.Equal(t, 1.0, *r.Temperature)
}

// (f) Strict-sampling models lose temperature even without any thinking
// request, and thinking is left alone.
func TestRequestOpenAI2ClaudeMessageClaude5StrictSamplingWithoutThinking(t *testing.T) {
	r := convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:       "claude-opus-5",
		Temperature: common.GetPointer(0.7),
	})
	require.Nil(t, r.Thinking)
	require.Empty(t, r.OutputConfig)
	requireNoSampling(t, r)

	r = convertOpenAIToClaudeForTest(t, dto.GeneralOpenAIRequest{
		Model:       "claude-opus-4-6",
		Temperature: common.GetPointer(0.7),
	})
	require.NotNil(t, r.Temperature)
	require.Equal(t, 0.7, *r.Temperature)
}

func TestRenderClaudeThinking(t *testing.T) {
	binding := json.RawMessage(`{"prefix_mismatch_behavior":"drop_block"}`)

	// Manual-capable models get budget thinking.
	r := &dto.ClaudeRequest{Model: "claude-opus-4-6"}
	RenderClaudeThinking(r, r.Model, "", common.GetPointer(3000))
	require.Equal(t, "enabled", r.Thinking.Type)
	require.Equal(t, 3000, r.Thinking.GetBudgetTokens())
	require.Empty(t, r.OutputConfig)

	// Others get adaptive + effort; an explicit effort wins over the budget.
	r = &dto.ClaudeRequest{Model: "claude-opus-5-5", Thinking: &dto.Thinking{Type: "enabled", BudgetTokens: common.GetPointer(2048), BlockBinding: binding}}
	RenderClaudeThinking(r, r.Model, "max", common.GetPointer(2048))
	require.Equal(t, "adaptive", r.Thinking.Type)
	require.Nil(t, r.Thinking.BudgetTokens)
	require.JSONEq(t, string(binding), string(r.Thinking.BlockBinding))
	require.Equal(t, "max", r.GetEfforts())

	// No effort, budget -> effort from budget; other output_config keys stay.
	r = &dto.ClaudeRequest{Model: "claude-opus-5", OutputConfig: json.RawMessage(`{"format":{"type":"json_schema"}}`)}
	RenderClaudeThinking(r, r.Model, "", common.GetPointer(500))
	require.JSONEq(t, `{"effort":"low","format":{"type":"json_schema"}}`, string(r.OutputConfig))

	// Neither -> the model's default effort.
	r = &dto.ClaudeRequest{Model: "claude-opus-5-5"}
	RenderClaudeThinking(r, r.Model, "", nil)
	require.Equal(t, "medium", r.GetEfforts())
}

func TestApplyClaudeStrictSampling(t *testing.T) {
	r := &dto.ClaudeRequest{Model: "claude-opus-4-7", Temperature: common.GetPointer(0.3), TopP: common.GetPointer(0.5), TopK: common.GetPointer(5)}
	ApplyClaudeStrictSampling(r, r.Model)
	requireNoSampling(t, r)

	r = &dto.ClaudeRequest{Model: "claude-opus-4-6", Temperature: common.GetPointer(0.3)}
	ApplyClaudeStrictSampling(r, r.Model)
	require.NotNil(t, r.Temperature)
}
