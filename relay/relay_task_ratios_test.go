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
