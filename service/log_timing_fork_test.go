package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O7 — e2e/llm/gateway timing breakdown in usage-log other
// (ported from team service/log_info_generate_test.go).

func newTimingTestContext(t *testing.T, wrap bool) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if wrap {
		c.Writer = &common.TimingResponseWriter{ResponseWriter: c.Writer}
	}
	return c
}

func TestGenerateTextOtherInfo_TimingBreakdown_FullPipeline(t *testing.T) {
	c := newTimingTestContext(t, true)
	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(2 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	_, err := c.Writer.Write([]byte("done"))
	require.NoError(t, err)

	snap := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0).Snapshot()

	e2eMs, ok := snap["e2e_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, e2eMs, int64(0))

	llmMs, ok := snap["llm_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, llmMs, int64(2))

	gatewayMs, ok := snap["gateway_ms"].(int64)
	require.True(t, ok)
	require.Equal(t, e2eMs-llmMs, gatewayMs)

	require.Contains(t, snap, "pre_llm_ms")
	require.Contains(t, snap, "post_llm_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsLlmWhenNoUpstreamCall(t *testing.T) {
	c := newTimingTestContext(t, true)
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}} // no upstream call happened

	snap := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0).Snapshot()

	require.Contains(t, snap, "e2e_ms")
	require.NotContains(t, snap, "llm_ms")
	require.NotContains(t, snap, "gateway_ms")
	require.NotContains(t, snap, "pre_llm_ms")
	require.NotContains(t, snap, "post_llm_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsE2eWhenNoGatewayEntry(t *testing.T) {
	// No ContextKeyGatewayEntryTime set (e.g. request never went through
	// RequestTiming middleware, such as in some internal/test call paths).
	c := newTimingTestContext(t, false)

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	snap := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0).Snapshot()

	require.NotContains(t, snap, "e2e_ms")
	require.NotContains(t, snap, "llm_ms")
}

// TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway
// pins down a known, documented limitation: on a retried request, llm_ms
// only reflects the *final* attempt (SetUpstreamRequestStart/End are plain
// overwrites, by design — see relay/common/relay_info.go), while e2e_ms
// spans the whole request including time spent on earlier failed attempts.
// gateway_ms therefore absorbs that failed-attempt time.
func TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway(t *testing.T) {
	c := newTimingTestContext(t, true)
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	// Attempt 1: fails against channel A after a (simulated) longer wait.
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(5 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	// Attempt 2 (retry): succeeds against channel B quickly; overwrites attempt 1.
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(1 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	_, err := c.Writer.Write([]byte("done"))
	require.NoError(t, err)

	snap := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0).Snapshot()

	e2eMs := snap["e2e_ms"].(int64)
	llmMs := snap["llm_ms"].(int64)
	gatewayMs := snap["gateway_ms"].(int64)

	// llm_ms only reflects attempt 2's ~1ms, not attempt 1's ~5ms.
	require.Less(t, llmMs, int64(5))
	// gateway_ms picks up the difference, including attempt 1's failed-call time.
	require.GreaterOrEqual(t, gatewayMs, int64(5))
	require.Equal(t, e2eMs-llmMs, gatewayMs)
}

func TestAppendTimingBreakdown_NegativeGatewayFloorsToZero(t *testing.T) {
	c := newTimingTestContext(t, true)
	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)
	_, _ = c.Writer.Write([]byte("partial")) // client disconnected: last Write happens first
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.UpstreamRequestStartTime = entry
	info.UpstreamResponseEndTime = entry.Add(5 * time.Second) // upstream body is closed later

	other := model.NewLogOther()
	appendTimingBreakdown(c, info, other)

	snap := other.Snapshot()
	require.Equal(t, int64(0), snap["gateway_ms"])
	require.GreaterOrEqual(t, snap["llm_ms"].(int64), int64(5000))
	require.Equal(t, int64(0), snap["post_llm_ms"], "negative post_llm_ms is floored too")
}
