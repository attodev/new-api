package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O7 — doRequest records the upstream call window used for llm_ms.
// Harness mirrors TestDoRequestReturnsUpstreamRedirectWithoutFollowing.

func TestDoRequest_RecordsUpstreamTimingOnSuccess(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)

	req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader([]byte("request body")))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	resp, err := doRequest(ctx, req, info)
	require.NoError(t, err)
	require.False(t, info.UpstreamRequestStartTime.IsZero())
	require.True(t, info.UpstreamResponseEndTime.IsZero(), "end time is only set once the body is drained")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "ok", string(body))
	require.NoError(t, resp.Body.Close())

	require.False(t, info.UpstreamResponseEndTime.IsZero())
	require.False(t, info.UpstreamResponseEndTime.Before(info.UpstreamRequestStartTime))
}

func TestDoRequest_RecordsUpstreamTimingOnFailure(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/relay", nil)

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:0", bytes.NewReader([]byte("request body")))
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	_, err = doRequest(ctx, req, info)
	require.Error(t, err)
	require.False(t, info.UpstreamRequestStartTime.IsZero())
	require.False(t, info.UpstreamResponseEndTime.IsZero())
	require.False(t, info.UpstreamResponseEndTime.Before(info.UpstreamRequestStartTime))
}
