package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestOpenaiHandlerWithUsage_CopiesCachedAndAudioImageTokens reproduces a
// real under-billing bug: the OpenAI Images API (generations/edits) reports
// cache-hit and audio token detail under input_tokens_details alongside
// image/text tokens, but only ImageTokens/TextTokens were being copied onto
// PromptTokensDetails - cached and audio input tokens on image models were
// silently billed as if they weren't cached at all.
func TestOpenaiHandlerWithUsage_CopiesCachedAndAudioImageTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	body := []byte(`{
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"input_tokens_details": {
				"cached_tokens": 40,
				"image_tokens": 30,
				"text_tokens": 20,
				"audio_tokens": 10
			}
		}
	}`)
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(body))}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	usage, apiErr := OpenaiHandlerWithUsage(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 50, usage.CompletionTokens)
	require.Equal(t, 40, usage.PromptTokensDetails.CachedTokens, "cached image-input tokens must be billed at the cache rate, not the base rate")
	require.Equal(t, 30, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, 10, usage.PromptTokensDetails.AudioTokens)
}

// TestOpenaiHandlerWithUsage_SurfacesUpstreamError reproduces a real
// failover bug: when the upstream Images API rejects a request (e.g.
// invalid size, moderation block) with a 400 body shaped like
// {"error": {...}}, OpenaiHandlerWithUsage parsed it as a zero-usage
// success instead of returning a NewAPIError - so the relay's
// retry-on-error logic never saw a failure and never failed over to
// another channel.
func TestOpenaiHandlerWithUsage_SurfacesUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	body := []byte(`{
		"error": {
			"message": "Invalid size parameter",
			"type": "invalid_request_error",
			"code": "invalid_size"
		}
	}`)
	resp := &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(bytes.NewReader(body))}

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	usage, apiErr := OpenaiHandlerWithUsage(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
}
