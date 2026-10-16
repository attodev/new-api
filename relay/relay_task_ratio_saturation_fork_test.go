package relay

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 04afc5a18. Team extracted step 6 of task submission into
// applyOtherRatiosToQuota; develop keeps it inline in RelayTaskSubmit as
// QuotaFromFloatChecked(PriceData.ApplyOtherRatiosToFloat(Quota)), and the
// post-submit adjustment goes through recalcQuotaFromRatios. An oversized
// ratio must saturate at common.MaxQuota (math.MaxInt32) instead of wrapping.
// Team's NaN/Inf cases are already covered by develop's
// TestRecalcQuotaFromRatios* in chat_completions_via_responses_test.go.

func TestTaskStepSixOtherRatiosSaturateOnOverflow(t *testing.T) {
	priceData := hosttypes.PriceData{Quota: 1000}
	priceData.AddOtherRatio("huge", 1e10)

	quota, clamp := common.QuotaFromFloatChecked(priceData.ApplyOtherRatiosToFloat(float64(priceData.Quota)))

	require.Equal(t, common.MaxQuota, quota)
	require.Equal(t, math.MaxInt32, quota)
	require.NotNil(t, clamp)
	assert.Equal(t, common.QuotaClampOverflow, clamp.Kind)
}

func TestTaskStepSixOtherRatiosApply(t *testing.T) {
	priceData := hosttypes.PriceData{Quota: 1000}
	priceData.AddOtherRatio("double", 2.0)
	priceData.AddOtherRatio("noop", 1.0)

	quota, clamp := common.QuotaFromFloatChecked(priceData.ApplyOtherRatiosToFloat(float64(priceData.Quota)))

	require.Nil(t, clamp)
	require.Equal(t, 2000, quota)
}

func TestRecalcQuotaFromRatiosSaturatesOnOverflow(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		PriceData:   hosttypes.PriceData{Quota: 1000},
	}

	quota, ok := recalcQuotaFromRatios(info, map[string]float64{"huge": 1e10})

	require.True(t, ok)
	require.Equal(t, common.MaxQuota, quota)
	require.NotNil(t, info.QuotaClamp, "saturation must be recorded for the task log")
	assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
}
