package relay

import (
	"math"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// TestRecalcQuotaFromRatios_RejectsNaNAndInf reproduces a real bug: unlike
// PriceData.AddOtherRatio (which rejects NaN/+Inf multipliers), the task
// submission path assigned adaptor.AdjustBillingOnSubmit's result straight
// into info.PriceData.OtherRatios and multiplied it into the quota with no
// validation at all - a task adaptor computing a malformed ratio from
// upstream response data (duration, resolution) could poison the settled
// quota with NaN or an unbounded value.
func TestRecalcQuotaFromRatios_RejectsNaNAndInf(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		PriceData:   types.PriceData{Quota: 1000},
	}

	got := recalcQuotaFromRatios(info, map[string]float64{
		"nan":  math.NaN(),
		"inf":  math.Inf(1),
		"zero": 0,
	})

	require.Equal(t, 1000, got, "a fully invalid ratio set must leave the base quota unchanged, not poison it")
}

func TestRecalcQuotaFromRatios_AppliesValidRatiosAndDropsInvalidOnes(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		PriceData:   types.PriceData{Quota: 1000},
	}

	got := recalcQuotaFromRatios(info, map[string]float64{
		"valid": 2.0,
		"nan":   math.NaN(),
	})

	require.Equal(t, 2000, got, "the valid ratio must still apply even when a sibling ratio in the same map is invalid")
}

// TestApplyOtherRatiosToQuota_SaturatesOnOverflow reproduces a real bug: the
// pre-consume-estimate application of AddOtherRatio-validated ratios (task
// submission step "6. OtherRatios") multiplied them into info.PriceData.Quota
// with a raw int(float64(...)) cast and no saturation. The ratios reaching
// here are already NaN/+Inf-filtered by AddOtherRatio, but the arithmetic
// PRODUCT itself can still exceed the 32-bit quota column's range - an
// oversized ratio (e.g. a large duration/resolution multiplier) would
// silently overflow instead of clamping, corrupting the pre-consumed amount
// before the wallet is even charged.
func TestApplyOtherRatiosToQuota_SaturatesOnOverflow(t *testing.T) {
	got := applyOtherRatiosToQuota(1000, map[string]float64{"huge": 1e10})
	require.Equal(t, math.MaxInt32, got)
}

func TestApplyOtherRatiosToQuota_AppliesRatios(t *testing.T) {
	got := applyOtherRatiosToQuota(1000, map[string]float64{"double": 2.0, "noop": 1.0})
	require.Equal(t, 2000, got)
}
