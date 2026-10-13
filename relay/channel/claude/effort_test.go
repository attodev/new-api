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
