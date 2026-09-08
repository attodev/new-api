package dto

import "testing"

func TestThinkingNormalizeAdaptiveDisplay(t *testing.T) {
	cases := []struct {
		name     string
		thinking *Thinking
		expect   string
	}{
		{
			// Anthropic defaults display to "omitted" on Opus 4.7 and later,
			// which bills thinking tokens and returns empty thinking blocks.
			name:     "adaptive without display gets the visible summary",
			thinking: &Thinking{Type: "adaptive"},
			expect:   "summarized",
		},
		{
			name:     "explicit client display wins",
			thinking: &Thinking{Type: "adaptive", Display: "omitted"},
			expect:   "omitted",
		},
		{
			name:     "explicit summarized stays",
			thinking: &Thinking{Type: "adaptive", Display: "summarized"},
			expect:   "summarized",
		},
		{
			// display is an adaptive-only field; enabled thinking already
			// returns full thinking text and would reject it.
			name:     "enabled thinking is untouched",
			thinking: &Thinking{Type: "enabled"},
			expect:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.thinking.NormalizeAdaptiveDisplay()
			if tc.thinking.Display != tc.expect {
				t.Fatalf("display = %q, want %q", tc.thinking.Display, tc.expect)
			}
		})
	}
}

func TestThinkingNormalizeAdaptiveDisplayNilSafe(t *testing.T) {
	var thinking *Thinking
	thinking.NormalizeAdaptiveDisplay()
}
