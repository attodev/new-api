package common

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoSetUpstreamRequestStart(t *testing.T) {
	info := &RelayInfo{}
	require.True(t, info.UpstreamRequestStartTime.IsZero())

	info.SetUpstreamRequestStart()

	require.False(t, info.UpstreamRequestStartTime.IsZero())
}

func TestRelayInfoSetUpstreamResponseEnd(t *testing.T) {
	info := &RelayInfo{}
	require.True(t, info.UpstreamResponseEndTime.IsZero())

	info.SetUpstreamResponseEnd()

	require.False(t, info.UpstreamResponseEndTime.IsZero())
}

func TestRelayInfoUpstreamTimestamps_LatestAttemptWins(t *testing.T) {
	info := &RelayInfo{}

	info.SetUpstreamRequestStart()
	first := info.UpstreamRequestStartTime

	time.Sleep(time.Millisecond)
	info.SetUpstreamRequestStart() // simulates a retry attempt

	require.True(t, info.UpstreamRequestStartTime.After(first))
}
