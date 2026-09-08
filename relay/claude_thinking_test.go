package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

// Opus 4.7 and later default thinking.display to "omitted": the response
// carries empty thinking blocks with valid signatures while thinking tokens are
// still billed. Clients relaying through the gateway must keep the summary that
// Opus 4.6 returned by default, whatever the model name happens to be.
func TestApplyClaudeThinkingSettingsRestoresAdaptiveDisplay(t *testing.T) {
	for _, model := range []string{"claude-opus-5", "claude-sonnet-5", "claude-opus-4-7"} {
		t.Run(model, func(t *testing.T) {
			request := &dto.ClaudeRequest{
				Model:    model,
				Thinking: &dto.Thinking{Type: "adaptive"},
			}
			require.False(t, applyClaudeThinkingSettings(request, model))
			require.Equal(t, model, request.Model)
			require.Equal(t, "summarized", request.Thinking.Display)
		})
	}
}

func TestApplyClaudeThinkingSettingsKeepsExplicitDisplay(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model:    "claude-opus-5",
		Thinking: &dto.Thinking{Type: "adaptive", Display: "omitted"},
	}
	require.False(t, applyClaudeThinkingSettings(request, "claude-opus-5"))
	require.Equal(t, "omitted", request.Thinking.Display)
}

func TestApplyClaudeThinkingSettingsLeavesEnabledThinking(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model:    "claude-opus-5",
		Thinking: &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048)},
	}
	require.False(t, applyClaudeThinkingSettings(request, "claude-opus-5"))
	require.Equal(t, "", request.Thinking.Display)
	require.Equal(t, "enabled", request.Thinking.Type)
}

func TestApplyClaudeThinkingSettingsNoThinkingRequested(t *testing.T) {
	request := &dto.ClaudeRequest{Model: "claude-opus-5"}
	require.False(t, applyClaudeThinkingSettings(request, "claude-opus-5"))
	require.Nil(t, request.Thinking)
}

// The effort-suffix branch builds adaptive thinking itself and must come out of
// the same normalization, not a per-model special case.
func TestApplyClaudeThinkingSettingsEffortSuffixIsSummarized(t *testing.T) {
	for _, model := range []string{"claude-opus-4-6-high", "claude-opus-4-7-high"} {
		t.Run(model, func(t *testing.T) {
			request := &dto.ClaudeRequest{Model: model}
			require.True(t, applyClaudeThinkingSettings(request, model))
			require.Equal(t, "adaptive", request.Thinking.Type)
			require.Equal(t, "summarized", request.Thinking.Display)
			require.Equal(t, `{"effort":"high"}`, string(request.OutputConfig))
		})
	}
}

func intPtr(v int) *int { return &v }
