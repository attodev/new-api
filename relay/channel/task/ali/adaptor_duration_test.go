package ali

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// TestConvertToAliRequest_NonPositiveSecondsFallsBackToDefault reproduces a
// real bug: a non-positive "seconds" value (e.g. "0" or "-1") parses
// successfully via strconv.Atoi, so it was assigned verbatim instead of
// falling back to the 5-second default - sending an invalid duration
// upstream.
func TestConvertToAliRequest_NonPositiveSecondsFallsBackToDefault(t *testing.T) {
	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	for _, seconds := range []string{"0", "-1"} {
		req := relaycommon.TaskSubmitReq{Model: "wan2.6-i2v-plus", Prompt: "a cat", Seconds: seconds}
		got, err := a.convertToAliRequest(info, req)
		require.NoError(t, err)
		require.Equal(t, 5, got.Parameters.Duration, "seconds=%q should fall back to the 5s default", seconds)
	}
}

func TestConvertToAliRequest_PositiveSecondsIsKept(t *testing.T) {
	a := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	req := relaycommon.TaskSubmitReq{Model: "wan2.6-i2v-plus", Prompt: "a cat", Seconds: "8"}
	got, err := a.convertToAliRequest(info, req)
	require.NoError(t, err)
	require.Equal(t, 8, got.Parameters.Duration)
}
