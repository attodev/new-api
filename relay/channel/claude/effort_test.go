package claude

import "testing"

// The table mirrors upstream claudeCapabilitiesFor
// (relaykit/relayconvert/reasoning/claude.go).
func TestClaudeEffort(t *testing.T) {
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
		{"unknown-model", "high", "high"}, // not in the table: unchanged
		{"unknown-model", "xhigh", "xhigh"},
		{"unknown-model", "none", ""},
	}
	for _, c := range cases {
		if got := ClaudeEffort(c.model, c.in); got != c.want {
			t.Errorf("ClaudeEffort(%q,%q)=%q want %q", c.model, c.in, got, c.want)
		}
	}
}

func TestClaudeEffortForThinking(t *testing.T) {
	cases := []struct{ model, in, thinking, want string }{
		// Opus 5, Haiku 5 and Sonnet 5.5 accept high or below while thinking is off.
		{"claude-opus-5", "xhigh", "disabled", "high"},
		{"claude-opus-5", "max", "disabled", "high"},
		{"claude-haiku-5-5", "max", "disabled", "high"},
		{"claude-sonnet-5-5", "xhigh", "between_tools", "high"},
		{"claude-sonnet-5-5", "medium", "disabled", "medium"},
		{"claude-opus-5", "xhigh", "adaptive", "xhigh"},
		{"claude-opus-5", "xhigh", "", "xhigh"},
		{"claude-opus-5-5", "xhigh", "disabled", "xhigh"},
		{"claude-opus-4-7", "xhigh", "disabled", "xhigh"},
		{"claude-sonnet-4-5", "high", "disabled", ""},
	}
	for _, c := range cases {
		if got := ClaudeEffortForThinking(c.model, c.in, c.thinking); got != c.want {
			t.Errorf("ClaudeEffortForThinking(%q,%q,%q)=%q want %q", c.model, c.in, c.thinking, got, c.want)
		}
	}
}

func TestClaudeSupportsEffort(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-opus-4-7":   true,
		"claude-opus-4-5":   true,
		"claude-sonnet-4-5": false,
		"claude-haiku-4-5":  false,
		"claude-opus-4-1":   false,
		"unknown-model":     false,
	} {
		if got := claudeSupportsEffort(model); got != want {
			t.Errorf("claudeSupportsEffort(%q)=%v want %v", model, got, want)
		}
	}
}

// Mirrors upstream claudeCapabilitiesFor supportsManual
// (relaykit/relayconvert/reasoning/claude.go:45-110). claude-mythos-preview
// keeps supportsManual=true upstream: its case never clears it.
func TestClaudeSupportsManualThinking(t *testing.T) {
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
		if got := claudeSupportsManualThinking(c.model); got != c.want {
			t.Errorf("claudeSupportsManualThinking(%q)=%v want %v", c.model, got, c.want)
		}
	}
}

func TestClaudeStrictSampling(t *testing.T) {
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
		if got := claudeStrictSampling(c.model); got != c.want {
			t.Errorf("claudeStrictSampling(%q)=%v want %v", c.model, got, c.want)
		}
	}
}

// Mirrors upstream EffortFromBudget (relaykit/relayconvert/reasoning/intent.go:674).
func TestClaudeEffortFromBudget(t *testing.T) {
	cases := []struct {
		budget int
		want   string
	}{
		{0, "none"},
		{-1, "high"},
		{1, "low"},
		{1024, "low"},
		{1025, "medium"},
		{8192, "medium"},
		{8193, "high"},
	}
	for _, c := range cases {
		if got := ClaudeEffortFromBudget(c.budget); got != c.want {
			t.Errorf("ClaudeEffortFromBudget(%d)=%q want %q", c.budget, got, c.want)
		}
	}
}

// Upstream defaultEffort: medium on Opus 5.5 and Haiku 5.x, high elsewhere.
func TestClaudeDefaultEffort(t *testing.T) {
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
		if got := claudeDefaultEffort(c.model); got != c.want {
			t.Errorf("claudeDefaultEffort(%q)=%q want %q", c.model, got, c.want)
		}
	}
}

// Mirrors upstream supportsDisable / disableAsBetweenTools
// (relaykit/relayconvert/reasoning/claude.go:55,63,74-75).
func TestClaudeDisableCapability(t *testing.T) {
	cases := []struct {
		model                 string
		supportsDisable, asBT bool
	}{
		{"claude-fable-5-1", false, false},
		{"claude-mythos-5-1", false, false},
		{"claude-mythos-preview", false, false},
		{"claude-opus-5-5", false, false},
		{"claude-sonnet-5-5", false, true},
		{"claude-opus-5", true, false},
		{"claude-sonnet-5", true, false},
		{"claude-haiku-5-5", true, false},
		{"claude-opus-4-7", true, false},
		{"claude-opus-4-6", true, false},
		{"unknown-model", true, false},
	}
	for _, c := range cases {
		supportsDisable, asBT := claudeDisableCapability(c.model)
		if supportsDisable != c.supportsDisable || asBT != c.asBT {
			t.Errorf("claudeDisableCapability(%q)=(%v,%v) want (%v,%v)", c.model, supportsDisable, asBT, c.supportsDisable, c.asBT)
		}
	}
}
