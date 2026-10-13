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
		Model:           "claude-opus-5",
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
