package claude

import (
	"testing"

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
