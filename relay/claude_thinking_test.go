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

// (i) Manual-capable models keep the client's budget thinking as sent.
func TestApplyClaudeThinkingSettingsLeavesEnabledThinking(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model:    "claude-opus-4-6",
		Thinking: &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048)},
	}
	require.False(t, applyClaudeThinkingSettings(request, "claude-opus-4-6"))
	require.Equal(t, "", request.Thinking.Display)
	require.Equal(t, "enabled", request.Thinking.Type)
	require.Equal(t, 2048, request.Thinking.GetBudgetTokens())
	require.Empty(t, request.OutputConfig)
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

// (g) Claude 5.x rejects budget thinking: a client-sent enabled+budget becomes
// adaptive at the effort the budget maps to, keeping block_binding.
func TestApplyClaudeThinkingSettingsClaude5EnabledBecomesAdaptive(t *testing.T) {
	binding := `{"prefix_mismatch_behavior":"drop_block"}`
	request := &dto.ClaudeRequest{
		Model:       "claude-opus-5-5",
		Thinking:    &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048), BlockBinding: json.RawMessage(binding)},
		Temperature: floatPtr(0.7),
	}
	require.False(t, applyClaudeThinkingSettings(request, "claude-opus-5-5"))
	require.Equal(t, "claude-opus-5-5", request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Nil(t, request.Thinking.BudgetTokens)
	require.Equal(t, "summarized", request.Thinking.Display)
	require.JSONEq(t, binding, string(request.Thinking.BlockBinding))
	require.JSONEq(t, `{"effort":"medium"}`, string(request.OutputConfig))
	require.Nil(t, request.Temperature)

	request = &dto.ClaudeRequest{
		Model:    "claude-opus-5-5",
		Thinking: &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048)},
	}
	applyClaudeThinkingSettings(request, "claude-opus-5-5")
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Empty(t, request.Thinking.BlockBinding)
}

// A client effort in output_config wins over the budget, and other
// output_config keys stay.
func TestApplyClaudeThinkingSettingsClaude5EnabledKeepsClientEffort(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model:        "claude-sonnet-5",
		Thinking:     &dto.Thinking{Type: "enabled", BudgetTokens: intPtr(2048)},
		OutputConfig: json.RawMessage(`{"effort":"xhigh","format":{"type":"json_schema"}}`),
	}
	applyClaudeThinkingSettings(request, "claude-sonnet-5")
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.JSONEq(t, `{"effort":"xhigh","format":{"type":"json_schema"}}`, string(request.OutputConfig))
}

// (h) Effort suffixes expand for Claude 5.x on the native path too.
func TestApplyClaudeThinkingSettingsClaude5EffortSuffix(t *testing.T) {
	request := &dto.ClaudeRequest{Model: "claude-sonnet-5-5-high", Temperature: floatPtr(0.2), TopK: intPtr(10)}
	require.True(t, applyClaudeThinkingSettings(request, "claude-sonnet-5-5-high"))
	require.Equal(t, "claude-sonnet-5-5", request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Equal(t, "summarized", request.Thinking.Display)
	require.JSONEq(t, `{"effort":"high"}`, string(request.OutputConfig))
	require.Nil(t, request.Temperature)
	require.Nil(t, request.TopP)
	require.Nil(t, request.TopK)
}

// The -thinking suffix on Claude 5.x renders adaptive at the default effort.
func TestApplyClaudeThinkingSettingsClaude5ThinkingSuffix(t *testing.T) {
	request := &dto.ClaudeRequest{Model: "claude-haiku-5-5-thinking"}
	require.True(t, applyClaudeThinkingSettings(request, "claude-haiku-5-5-thinking"))
	require.Equal(t, "claude-haiku-5-5", request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.JSONEq(t, `{"effort":"medium"}`, string(request.OutputConfig))
}

// Strict-sampling models lose temperature/top_p/top_k on the native path
// even without thinking; other models keep them.
func TestApplyClaudeThinkingSettingsStrictSampling(t *testing.T) {
	request := &dto.ClaudeRequest{Model: "claude-opus-4-8", Temperature: floatPtr(0.7), TopP: floatPtr(0.9)}
	applyClaudeThinkingSettings(request, "claude-opus-4-8")
	require.Nil(t, request.Thinking)
	require.Nil(t, request.Temperature)
	require.Nil(t, request.TopP)

	request = &dto.ClaudeRequest{Model: "claude-opus-4-6", Temperature: floatPtr(0.7)}
	applyClaudeThinkingSettings(request, "claude-opus-4-6")
	require.NotNil(t, request.Temperature)
}

func floatPtr(v float64) *float64 { return &v }

// Models that cannot turn thinking off get their lowest thinking setting for
// a client's {type:"disabled"} instead of a 400 (upstream RenderClaude,
// relaykit/relayconvert/reasoning/claude.go:166-217).
func TestApplyClaudeThinkingSettingsDisabledOnCannotDisableModels(t *testing.T) {
	binding := `{"prefix_mismatch_behavior":"drop_block"}`

	// Sonnet 5.5 turns thinking off with between_tools, which takes no other
	// thinking field and effort high or below.
	request := &dto.ClaudeRequest{Model: "claude-sonnet-5-5", Thinking: &dto.Thinking{Type: "disabled"}}
	applyClaudeThinkingSettings(request, request.Model)
	require.Equal(t, &dto.Thinking{Type: "between_tools"}, request.Thinking)
	require.Empty(t, request.OutputConfig)

	request = &dto.ClaudeRequest{Model: "claude-sonnet-5-5", Thinking: &dto.Thinking{Type: "disabled"}, OutputConfig: json.RawMessage(`{"effort":"xhigh"}`)}
	applyClaudeThinkingSettings(request, request.Model)
	require.Equal(t, "between_tools", request.Thinking.Type)
	require.JSONEq(t, `{"effort":"high"}`, string(request.OutputConfig))

	// Opus 5.5 cannot turn thinking off: adaptive at low effort.
	request = &dto.ClaudeRequest{Model: "claude-opus-5-5", Thinking: &dto.Thinking{Type: "disabled", BlockBinding: json.RawMessage(binding)}}
	applyClaudeThinkingSettings(request, request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Equal(t, "summarized", request.Thinking.Display)
	require.JSONEq(t, binding, string(request.Thinking.BlockBinding))
	require.JSONEq(t, `{"effort":"low"}`, string(request.OutputConfig))

	// A disable request runs at low whatever effort the client sent; other
	// output_config keys stay.
	request = &dto.ClaudeRequest{Model: "claude-fable-5-1", Thinking: &dto.Thinking{Type: "disabled"}, OutputConfig: json.RawMessage(`{"effort":"xhigh","format":{"type":"json_schema"}}`)}
	applyClaudeThinkingSettings(request, request.Model)
	require.Equal(t, "adaptive", request.Thinking.Type)
	require.Empty(t, request.Thinking.BlockBinding)
	require.JSONEq(t, `{"effort":"low","format":{"type":"json_schema"}}`, string(request.OutputConfig))

	// Models that can turn thinking off keep the client's request.
	for _, model := range []string{"claude-opus-4-6", "claude-opus-5"} {
		request = &dto.ClaudeRequest{Model: model, Thinking: &dto.Thinking{Type: "disabled"}}
		applyClaudeThinkingSettings(request, model)
		require.Equal(t, &dto.Thinking{Type: "disabled"}, request.Thinking, model)
		require.Empty(t, request.OutputConfig, model)
	}
}
