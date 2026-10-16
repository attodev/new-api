package operation_setting

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaSettingPreConsumeDefaults(t *testing.T) {
	// The package-level defaults are what an install with no stored option uses.
	defaults := quotaSetting
	assert.Equal(t, 1.0, defaults.PreConsumeMultiplier)
	assert.Equal(t, 10.0, defaults.TrustQuotaUSD)
	assert.True(t, defaults.EnableFreeModelPreConsume)
}

func TestValidateQuotaOptionPreConsumeKeys(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		ok         bool
	}{
		{"quota_setting.trust_quota_usd", "10.5", true},
		{"quota_setting.trust_quota_usd", "0", true},
		{"quota_setting.trust_quota_usd", "-1", false},
		{"quota_setting.trust_quota_usd", "NaN", false},
		{"quota_setting.trust_quota_usd", "+Inf", false},
		{"quota_setting.trust_quota_usd", "abc", false},
		{"quota_setting.pre_consume_multiplier", "1.5", true},
		{"quota_setting.pre_consume_multiplier", "0.5", true},
		{"quota_setting.pre_consume_multiplier", "0", false},
		{"quota_setting.pre_consume_multiplier", "-0.5", false},
		{"quota_setting.pre_consume_multiplier", "NaN", false},
		{"quota_setting.pre_consume_multiplier", "Inf", false},
		{"quota_setting.pre_consume_multiplier", "1e309", false},
		{"quota_setting.pre_consume_multiplier", "", false},
		{"quota_setting.enable_free_model_pre_consume", "anything", true},
		{"SomeOtherOption", "NaN", true},
	} {
		err := ValidateQuotaOption(tc.key, tc.value)
		if tc.ok {
			assert.NoError(t, err, "%s=%q", tc.key, tc.value)
		} else {
			assert.Error(t, err, "%s=%q", tc.key, tc.value)
		}
	}
}

func TestInputPreConsumeMultiplierRejectsInvalidRuntimeValues(t *testing.T) {
	previous := quotaSetting.PreConsumeMultiplier
	t.Cleanup(func() { quotaSetting.PreConsumeMultiplier = previous })

	quotaSetting.PreConsumeMultiplier = 1.5
	got, err := InputPreConsumeMultiplier()
	require.NoError(t, err)
	assert.Equal(t, 1.5, got)

	for _, bad := range []float64{0, -0.5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		quotaSetting.PreConsumeMultiplier = bad
		_, err := InputPreConsumeMultiplier()
		assert.Error(t, err, "%v", bad)
	}
}
