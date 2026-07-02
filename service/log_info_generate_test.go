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
