package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfo_TimingBreakdown_FullPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(2 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	_, err := c.Writer.Write([]byte("done"))
	require.NoError(t, err)

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	e2eMs, ok := other["e2e_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, e2eMs, int64(0))

	llmMs, ok := other["llm_ms"].(int64)
	require.True(t, ok)
	require.GreaterOrEqual(t, llmMs, int64(2))

	gatewayMs, ok := other["gateway_ms"].(int64)
	require.True(t, ok)
	require.Equal(t, e2eMs-llmMs, gatewayMs)

	require.Contains(t, other, "pre_llm_ms")
	require.Contains(t, other, "post_llm_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsLlmWhenNoUpstreamCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, time.Now())

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}} // no upstream call happened

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	require.Contains(t, other, "e2e_ms")
	require.NotContains(t, other, "llm_ms")
	require.NotContains(t, other, "gateway_ms")
}

func TestGenerateTextOtherInfo_TimingBreakdown_OmitsE2eWhenNoGatewayEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	// No ContextKeyGatewayEntryTime set (e.g. request never went through
	// RequestTiming middleware, such as in some internal/test call paths).

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	require.NotContains(t, other, "e2e_ms")
	require.NotContains(t, other, "llm_ms")
}

// TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway
// pins down a known, documented limitation: on a retried request, llm_ms
// only reflects the *final* attempt (SetUpstreamRequestStart/End are plain
// overwrites, by design — see relay/common/relay_info.go), while e2e_ms
// spans the whole request including time spent on earlier failed attempts.
// gateway_ms therefore absorbs that failed-attempt time. This test
// documents the current behavior so a future change to the underlying
// timestamp semantics doesn't silently alter it unnoticed.
func TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)

	writer := &common.TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	entry := time.Now()
	common.SetContextKey(c, constant.ContextKeyGatewayEntryTime, entry)

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	// Attempt 1: fails against channel A after a (simulated) longer wait.
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(5 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	// Attempt 2 (retry): succeeds against channel B quickly. Per Task 4,
	// these overwrite attempt 1's timestamps unconditionally.
	relayInfo.SetUpstreamRequestStart()
	time.Sleep(1 * time.Millisecond)
	relayInfo.SetUpstreamResponseEnd()

	_, err := c.Writer.Write([]byte("done"))
	require.NoError(t, err)

	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 0)

	e2eMs := other["e2e_ms"].(int64)
	llmMs := other["llm_ms"].(int64)
	gatewayMs := other["gateway_ms"].(int64)

	// llm_ms only reflects attempt 2's ~1ms, not attempt 1's ~5ms.
	require.Less(t, llmMs, int64(5))
	// gateway_ms picks up the difference, including attempt 1's failed-call
	// time — this is the documented limitation, not a crash/negative value.
	require.GreaterOrEqual(t, gatewayMs, int64(5))
	require.Equal(t, e2eMs-llmMs, gatewayMs)
}
