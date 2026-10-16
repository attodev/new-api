package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 04afc5a18. Team's applyOtherRatiosToQuota (task submission
// step 6) does not exist on develop; step 6 is inline in RelayTaskSubmit and is
// not exercised here. This covers the post-submit adjustment path through
// recalcQuotaFromRatios: an oversized ratio must saturate at common.MaxQuota
// (math.MaxInt32) and record the clamp instead of wrapping. Team's NaN/Inf
// cases are already covered by develop's TestRecalcQuotaFromRatios* in
// chat_completions_via_responses_test.go.
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
