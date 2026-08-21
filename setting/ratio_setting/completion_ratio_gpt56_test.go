package ratio_setting

import "testing"

// TestGetCompletionRatioInfo_GPT56FamilyIsUnlocked reproduces a real bug:
// getHardcodedCompletionModelRatio special-cased "gpt-5.6" to a locked,
// hardcoded completion ratio of 6 for every variant (sol/terra/luna alike).
// Since GetCompletionRatioInfo returns immediately when the hardcoded
// lookup reports Locked=true, this made it impossible to ever configure a
// differentiated completion ratio per gpt-5.6 variant (matching their very
// different prompt-token prices - sol at $5/1M vs luna at $1/1M) - any
// completion_ratio map entry for these models would be silently ignored.
// gpt-5.5 and later models are meant to be "unlocked" (admin-configurable
// via the completion ratio map), per the same rule already applied to
// gpt-5.5 itself.
func TestGetCompletionRatioInfo_GPT56FamilyIsUnlocked(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		info := GetCompletionRatioInfo(model)
		if info.Locked {
			t.Errorf("GetCompletionRatioInfo(%q).Locked = true, want false (gpt-5.5+ models must be unlocked/configurable)", model)
		}
	}
}
