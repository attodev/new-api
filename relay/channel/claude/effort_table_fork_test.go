package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
)

// Ported from team effort_test.go (fb6c2c970). team kept its own effort table
// in this package; develop reads it from relaykit's claudeCapabilitiesFor, so
// the cases run against the exported relaykit API instead.
func TestClaudeEffortTableFork(t *testing.T) {
	cases := []struct{ model, in, want string }{
		{"claude-opus-4-7", "xhigh", "xhigh"},
		{"claude-opus-4-7", "max", "max"},
		{"claude-opus-4-7", "minimal", "low"},
		{"claude-opus-4-6", "xhigh", "max"}, // no xhigh, has max
		{"claude-opus-4-6", "max", "max"},
		{"claude-sonnet-4-6", "xhigh", "max"},
		{"claude-opus-4-5", "max", "high"}, // 4.5 takes high and below only
		{"claude-opus-4-5", "xhigh", "high"},
		{"claude-opus-4-5", "medium", "medium"},
		{"claude-sonnet-4-5", "high", ""}, // known model without effort
		{"claude-haiku-4-5", "low", ""},
		{"claude-3-7-sonnet-20250219", "high", ""},
		{"claude-opus-5", "max", "max"},
		{"claude-opus-5-5", "xhigh", "xhigh"},
		{"claude-haiku-5-5", "xhigh", "xhigh"},
		{"claude-sonnet-5-5", "xhigh", "xhigh"},
		{"claude-sonnet-5-5", "medium", "medium"},
		{"claude-fable-5-1", "max", "max"},
		{"claude-mythos-5-1", "low", "low"},
		{"claude-mythos-preview", "xhigh", "max"}, // no xhigh, has max
		{"claude-opus-4-7-thinking", "xhigh", "xhigh"},
		{"claude-opus-4-7", "", ""},
		{"claude-opus-4-7", "none", ""},
		{"claude-sonnet-4-5", "", ""},
		// team kept an unknown model's effort here; relaykit's ClaudeEffort
		// returns empty for models missing from its table (documented on the
		// function). The native path still forwards it unchanged: see
		// TestClaudeUnknownModelEffortPassesThroughFork.
		{"unknown-model", "high", ""},
		{"unknown-model", "xhigh", ""},
		{"unknown-model", "none", ""},
	}
	for _, c := range cases {
		if got := reasoning.ClaudeEffort(c.model, reasoning.Effort(c.in)); string(got) != c.want {
			t.Errorf("ClaudeEffort(%q,%q)=%q want %q", c.model, c.in, got, c.want)
		}
	}
}

// team asserted that a model outside the table kept its effort as sent;
// develop does that in RenderClaude for a request without thinking.
func TestClaudeUnknownModelEffortPassesThroughFork(t *testing.T) {
	for _, effort := range []reasoning.Effort{reasoning.EffortHigh, reasoning.EffortXHigh, reasoning.EffortMax} {
		render, err := reasoning.RenderClaude("unknown-model", reasoning.Intent{Effort: effort}, nil, 0)
		if err != nil {
			t.Fatalf("RenderClaude(unknown-model,%q): %v", effort, err)
		}
		if render.OutputEffort != effort || render.Thinking != nil {
			t.Errorf("RenderClaude(unknown-model,%q)=(%q,%v) want (%q,nil)", effort, render.OutputEffort, render.Thinking, effort)
		}
	}
}

// team ClaudeEffortForThinking(model, effort, thinkingType) is RenderClaude's
// OutputEffort for the matching intent mode in develop.
func TestClaudeEffortForThinkingFork(t *testing.T) {
	modes := map[string]reasoning.Mode{
		"disabled":      reasoning.ModeDisabled,
		"between_tools": reasoning.ModeDisabled,
		"adaptive":      reasoning.ModeAdaptive,
		"":              reasoning.ModeUnset,
	}
	cases := []struct{ model, in, thinking, want string }{
		// Opus 5, Haiku 5 and Sonnet 5.5 accept high or below while thinking is off.
		{"claude-opus-5", "xhigh", "disabled", "high"},
		{"claude-opus-5", "max", "disabled", "high"},
		{"claude-haiku-5-5", "max", "disabled", "high"},
		{"claude-sonnet-5-5", "xhigh", "between_tools", "high"},
		{"claude-sonnet-5-5", "medium", "disabled", "medium"},
		{"claude-opus-5", "xhigh", "adaptive", "xhigh"},
		{"claude-opus-5", "xhigh", "", "xhigh"},
		// team: xhigh. Opus 5.5 cannot turn thinking off, so develop renders
		// adaptive thinking at low effort for any disable request.
		{"claude-opus-5-5", "xhigh", "disabled", "low"},
		{"claude-opus-4-7", "xhigh", "disabled", "xhigh"},
		{"claude-sonnet-4-5", "high", "disabled", ""},
	}
	for _, c := range cases {
		intent := reasoning.Intent{Mode: modes[c.thinking], Effort: reasoning.Effort(c.in)}
		render, err := reasoning.RenderClaude(c.model, intent, nil, 0)
		if err != nil {
			t.Errorf("RenderClaude(%q,%q,%q): %v", c.model, c.in, c.thinking, err)
			continue
		}
		if string(render.OutputEffort) != c.want {
			t.Errorf("RenderClaude(%q,%q,%q).OutputEffort=%q want %q", c.model, c.in, c.thinking, render.OutputEffort, c.want)
		}
	}
}

// team claudeSupportsEffort: a supported model returns a non-empty effort.
func TestClaudeSupportsEffortFork(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-opus-4-7":   true,
		"claude-opus-4-5":   true,
		"claude-sonnet-4-5": false,
		"claude-haiku-4-5":  false,
		"claude-opus-4-1":   false,
		"unknown-model":     false,
	} {
		if got := reasoning.ClaudeEffort(model, reasoning.EffortHigh) != ""; got != want {
			t.Errorf("supports effort %q=%v want %v", model, got, want)
		}
	}
}

// team claudeSupportsManualThinking: ClaudeUsesManualThinking for an enabled
// budget request.
func TestClaudeSupportsManualThinkingFork(t *testing.T) {
	budget := 2048
	intent := reasoning.Intent{Mode: reasoning.ModeEnabled, BudgetTokens: &budget}
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-opus-4-6", true},
		{"claude-sonnet-4-5", true},
		{"claude-opus-4-7", false},
		{"claude-opus-4-8", false},
		{"claude-opus-5", false},
		{"claude-opus-5-5", false},
		{"claude-sonnet-5", false},
		{"claude-sonnet-5-5", false},
		{"claude-haiku-5-5", false},
		{"claude-fable-5-1", false},
		{"claude-mythos-5-1", false},
		{"claude-mythos-preview", true},
		{"claude-opus-5-5-thinking", false},
		{"unknown-model", true},
	}
	for _, c := range cases {
		if got := reasoning.ClaudeUsesManualThinking(c.model, intent); got != c.want {
			t.Errorf("ClaudeUsesManualThinking(%q)=%v want %v", c.model, got, c.want)
		}
	}
}

// team claudeStrictSampling: RenderClaude clears sampling for these models
// even when no thinking is requested.
func TestClaudeStrictSamplingFork(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-opus-4-7", true},
		{"claude-opus-4-8", true},
		{"claude-opus-5", true},
		{"claude-opus-5-5", true},
		{"claude-sonnet-5", true},
		{"claude-sonnet-5-5", true},
		{"claude-haiku-5-5", true},
		{"claude-fable-5-1", true},
		{"claude-mythos-5-1", true},
		{"claude-mythos-preview", true},
		{"claude-opus-4-6", false},
		{"claude-sonnet-4-5", false},
		{"unknown-model", false},
	}
	for _, c := range cases {
		render, err := reasoning.RenderClaude(c.model, reasoning.Intent{}, nil, 0)
		if err != nil {
			t.Errorf("RenderClaude(%q): %v", c.model, err)
			continue
		}
		if render.ClearSampling != c.want {
			t.Errorf("RenderClaude(%q).ClearSampling=%v want %v", c.model, render.ClearSampling, c.want)
		}
	}
}

func TestClaudeEffortFromBudgetFork(t *testing.T) {
	cases := []struct {
		budget int
		want   reasoning.Effort
	}{
		{0, reasoning.EffortNone},
		{-1, reasoning.EffortHigh},
		{1, reasoning.EffortLow},
		{1024, reasoning.EffortLow},
		{1025, reasoning.EffortMedium},
		{8192, reasoning.EffortMedium},
		{8193, reasoning.EffortHigh},
	}
	for _, c := range cases {
		if got := reasoning.EffortFromBudget(c.budget); got != c.want {
			t.Errorf("EffortFromBudget(%d)=%q want %q", c.budget, got, c.want)
		}
	}
}

// Medium on Opus 5.5 and Haiku 5.x, high elsewhere.
func TestClaudeDefaultEffortFork(t *testing.T) {
	cases := []struct{ model, want string }{
		{"claude-opus-5-5", "medium"},
		{"claude-haiku-5-5", "medium"},
		{"claude-sonnet-5-5", "high"},
		{"claude-opus-5", "high"},
		{"claude-fable-5-1", "high"},
		{"claude-opus-4-7", "high"},
		{"unknown-model", "high"},
	}
	for _, c := range cases {
		if got := reasoning.ClaudeDefaultEffort(c.model); string(got) != c.want {
			t.Errorf("ClaudeDefaultEffort(%q)=%q want %q", c.model, got, c.want)
		}
	}
}

// team claudeDisableCapability(model) -> (supportsDisable, disableAsBetweenTools)
// shows in develop as the thinking type RenderClaude picks for a disable
// request: disabled, between_tools, or adaptive when neither is possible.
func TestClaudeDisableCapabilityFork(t *testing.T) {
	cases := []struct{ model, want string }{
		{"claude-fable-5-1", "adaptive"},
		{"claude-mythos-5-1", "adaptive"},
		{"claude-mythos-preview", "adaptive"},
		{"claude-opus-5-5", "adaptive"},
		{"claude-sonnet-5-5", "between_tools"},
		{"claude-opus-5", "disabled"},
		{"claude-sonnet-5", "disabled"},
		{"claude-haiku-5-5", "disabled"},
		{"claude-opus-4-7", "disabled"},
		{"claude-opus-4-6", "disabled"},
		{"unknown-model", "disabled"},
	}
	for _, c := range cases {
		render, err := reasoning.RenderClaude(c.model, reasoning.Intent{Mode: reasoning.ModeDisabled}, nil, 0)
		if err != nil {
			t.Errorf("RenderClaude(%q, disabled): %v", c.model, err)
			continue
		}
		got := ""
		if render.Thinking != nil {
			got = render.Thinking.Type
		}
		if got != c.want {
			t.Errorf("RenderClaude(%q, disabled).Thinking.Type=%q want %q", c.model, got, c.want)
		}
	}
}
