package relay

import (
	"encoding/json"
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

// The effort suffix re-renders thinking as adaptive; block_binding is a
// conversation control the native client sent, so it must survive, and it must
// never be invented when the client did not send one.
func TestApplyClaudeThinkingSettingsEffortSuffixKeepsBlockBinding(t *testing.T) {
	binding := `{"prefix_mismatch_behavior":"drop_block"}`
	request := &dto.ClaudeRequest{
		Model:    "claude-opus-4-7-high",
		Thinking: &dto.Thinking{Type: "adaptive", BlockBinding: json.RawMessage(binding)},
	}
	require.True(t, applyClaudeThinkingSettings(request, "claude-opus-4-7-high"))
	require.Equal(t, "claude-opus-4-7", request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Equal(t, "summarized", request.Thinking.Display)
	require.JSONEq(t, binding, string(request.Thinking.BlockBinding))

	request = &dto.ClaudeRequest{Model: "claude-opus-4-7-high"}
	require.True(t, applyClaudeThinkingSettings(request, "claude-opus-4-7-high"))
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Empty(t, request.Thinking.BlockBinding)
}

// The native path sends the model the effort it accepts: a client-sent
// output_config.effort is lowered or removed, never rejected, and the other
// output_config keys stay.
func TestApplyClaudeThinkingSettingsNormalizesClientEffort(t *testing.T) {
	cases := []struct {
		name, model, thinking, in, want string
	}{
		{"opus-4-5 caps xhigh", "claude-opus-4-5", "", `{"effort":"xhigh"}`, `{"effort":"high"}`},
		{"opus-4-6 xhigh to max", "claude-opus-4-6", "adaptive", `{"effort":"xhigh"}`, `{"effort":"max"}`},
		{"sonnet-4-5 drops effort", "claude-sonnet-4-5", "", `{"effort":"high"}`, ``},
		{"keeps other keys", "claude-sonnet-4-5", "", `{"effort":"high","format":{"type":"json_schema"}}`, `{"format":{"type":"json_schema"}}`},
		{"accepted effort untouched", "claude-opus-4-7", "adaptive", `{"effort":"xhigh"}`, `{"effort":"xhigh"}`},
		{"opus-5 off thinking caps to high", "claude-opus-5", "disabled", `{"effort":"max"}`, `{"effort":"high"}`},
		{"unknown model untouched", "some-claude-proxy", "", `{"effort":"max"}`, `{"effort":"max"}`},
		{"no effort key untouched", "claude-sonnet-4-5", "", `{"format":{"type":"json_schema"}}`, `{"format":{"type":"json_schema"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			request := &dto.ClaudeRequest{Model: c.model, OutputConfig: json.RawMessage(c.in)}
			if c.thinking != "" {
				request.Thinking = &dto.Thinking{Type: c.thinking}
			}
			applyClaudeThinkingSettings(request, c.model)
			if c.want == "" {
				require.Empty(t, request.OutputConfig)
				return
			}
			require.JSONEq(t, c.want, string(request.OutputConfig))
		})
	}
}
