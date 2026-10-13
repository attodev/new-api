package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestOpenaiImageStreamHandlerForwardsSSEAndUsage reproduces a real feature
// gap: the OpenAI Images API supports stream=true for partial-image SSE
// events, but the relay had no handler for it at all - a client requesting
// a streaming image generation would get the raw event-stream body relayed
// through the plain JSON path, which reads the whole body as one
// ImageResponse and never forwards the SSE events or extracts usage from
// the final data frame.
func TestOpenaiImageStreamHandlerForwardsSSEAndUsage(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    true,
	}

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Contains(t, recorder.Body.String(), `event: image_generation.partial_image`)
	require.Contains(t, recorder.Body.String(), `data: {"type":"image_generation.partial_image","b64_json":"partial"}`)
	require.Contains(t, recorder.Body.String(), `data: {"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestOpenaiImageStreamHandlerForwardsLargeSSELine(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	payload := strings.Repeat("x", helper.DefaultMaxScannerBufferSize+1)
	body := "data: " + payload + "\n\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    true,
	}

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), payload)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
}

// TestOpenaiImageStreamHandlerWrapsJSONResponse reproduces the fallback case:
// some OpenAI-compatible upstreams ignore stream=true for images and return
// a plain JSON ImageResponse anyway. A client that asked for a stream must
// still get an SSE stream back, not a raw JSON body.
func TestOpenaiImageStreamHandlerWrapsJSONResponse(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"created":1710000000,"data":[{"b64_json":"final","revised_prompt":"draw a cat"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7,"input_tokens_details":{"image_tokens":2,"text_tokens":1}}}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    true,
	}

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 3, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Equal(t, 7, usage.TotalTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1, usage.PromptTokensDetails.TextTokens)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	require.Empty(t, recorder.Header().Get("Content-Length"))
	require.Contains(t, recorder.Body.String(), `event: image_generation.completed`)
	require.Contains(t, recorder.Body.String(), `"type":"image_generation.completed"`)
	require.Contains(t, recorder.Body.String(), `"b64_json":"final"`)
	require.Contains(t, recorder.Body.String(), `"revised_prompt":"draw a cat"`)
	require.Contains(t, recorder.Body.String(), `data: [DONE]`)
}

// TestOpenaiHandlerWithUsageReturnsImageJSONError is a companion to
// TestOpenaiHandlerWithUsage_SurfacesUpstreamError, verifying the exact
// error fields survive the non-streaming image path too.
func TestOpenaiHandlerWithUsageReturnsImageJSONError(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"error":{"message":"content moderation failed","type":"upstream_error","code":"content_moderation_failed","status":502}}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    false,
	}

	usage, err := OpenaiHandlerWithUsage(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusOK, err.StatusCode)
	oaiError := err.ToOpenAIError()
	require.Equal(t, "content moderation failed", oaiError.Message)
	require.Equal(t, "upstream_error", oaiError.Type)
	require.Equal(t, "content_moderation_failed", oaiError.Code)
	require.Empty(t, recorder.Body.String())
}

func TestOpenaiImageStreamHandlerReturnsJSONErrorFallback(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"error":{"message":"image edit failed","type":"upstream_error","code":"content_moderation_failed","status":502}}`

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    true,
	}

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, http.StatusOK, err.StatusCode)
	oaiError := err.ToOpenAIError()
	require.Equal(t, "image edit failed", oaiError.Message)
	require.Empty(t, recorder.Body.String())
}

// TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent reproduces a real
// gap: an SSE "event: error" frame mid-stream (e.g. an HTTP/2 stream reset
// from upstream) must still be relayed to the client, but also recorded as
// a stream error so retry-eligibility/logging reflects the failure instead
// of looking like a clean completion.
func TestOpenaiImageStreamHandlerRecordsUpstreamErrorEvent(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := strings.Join([]string{
		`event: image_generation.partial_image`,
		`data: {"type":"image_generation.partial_image","b64_json":"partial"}`,
		``,
		`event: error`,
		`data: {"type":"upstream_error","error":{"message":"stream error: stream ID 77; INTERNAL_ERROR; received from peer"}}`,
		``,
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    true,
	}

	usage, err := OpenaiImageStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.HasErrors())
	require.Equal(t, 1, info.StreamStatus.TotalErrorCount())
	require.Contains(t, info.StreamStatus.Errors[0].Message, "INTERNAL_ERROR")
	require.Contains(t, recorder.Body.String(), `event: error`)
	require.Contains(t, recorder.Body.String(), `stream ID 77`)
}

func TestNormalizeOpenAIUsageMapsImageTokenDetailsWithoutDoubleCounting(t *testing.T) {
	usage := &dto.Usage{
		InputTokens:  5000,
		OutputTokens: 4000,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedCreationTokens: 200,
			ImageTokens:          1000,
			TextTokens:           4000,
		},
	}

	normalizeOpenAIUsage(usage)

	require.Equal(t, 5000, usage.PromptTokens)
	require.Equal(t, 4000, usage.CompletionTokens)
	require.Equal(t, 9000, usage.TotalTokens)
	require.Equal(t, 200, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 1000, usage.PromptTokensDetails.ImageTokens)
	require.Equal(t, 4000, usage.PromptTokensDetails.TextTokens)
}

func newImageCountTestContext(t *testing.T, body, contentType string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		IsStream:    stream,
	}
	return c, recorder, resp, info
}

func TestOpenaiImageResponseCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int64
	}{
		{"array of b64_json entries", `{"data":[{"b64_json":"a"},{"b64_json":"b"}]}`, 2},
		{"array of url entries", `{"data":[{"url":"https://example.com/a.png"}]}`, 1},
		{"object data with url and b64_json is one image", `{"data":{"url":"https://example.com/a.png","b64_json":"a"}}`, 1},
		{"split url and b64_json entries count once", `{"data":[{"url":"https://example.com/a.png"},{"b64_json":"a"}]}`, 1},
		{"revised_prompt only entry is not an image", `{"data":[{"revised_prompt":"draw a cat"}]}`, 0},
		{"empty string payloads are not images", `{"data":[{"url":"","b64_json":""}]}`, 0},
		{"object data without payload", `{"data":{"revised_prompt":"x"}}`, 0},
		{"empty data array", `{"data":[]}`, 0},
		{"missing data", `{}`, 0},
		{"non-string payload is not an image", `{"data":[{"url":null,"b64_json":123}]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, openaiImageResponseCount([]byte(tc.body)))
		})
	}
}

// TestOpenaiImageStreamHandlerWrapsNonStandardDataShapes covers the JSON-to-SSE
// fallback when upstream returns data as an object, splits one image across a
// url entry and a b64_json entry, or returns entries without any image payload.
func TestOpenaiImageStreamHandlerWrapsNonStandardDataShapes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantEvents int
		wantBody   []string
	}{
		{
			name:       "object data is forwarded as one completed event",
			body:       `{"data":{"url":"https://example.com/a.png","b64_json":"first"}}`,
			wantEvents: 1,
			wantBody:   []string{`"url":"https://example.com/a.png"`, `"b64_json":"first"`},
		},
		{
			name:       "split url and b64_json entries are each forwarded",
			body:       `{"data":[{"url":"https://example.com/a.png"},{"b64_json":"first"}]}`,
			wantEvents: 2,
			wantBody:   []string{`"url":"https://example.com/a.png"`, `"b64_json":"first"`},
		},
		{
			name:       "entries without image payload are dropped",
			body:       `{"data":[{"revised_prompt":"draw a cat"}]}`,
			wantEvents: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, recorder, resp, info := newImageCountTestContext(t, tc.body, "application/json", true)

			_, err := OpenaiImageStreamHandler(c, info, resp)
			require.Nil(t, err)
			out := recorder.Body.String()
			require.Equal(t, tc.wantEvents, strings.Count(out, "event: image_generation.completed"))
			require.Equal(t, tc.wantEvents, info.ReceivedResponseCount)
			for _, want := range tc.wantBody {
				require.Contains(t, out, want)
			}
			require.True(t, strings.HasSuffix(out, "data: [DONE]\n\n"))
		})
	}
}

func TestNormalizeOpenAIUsageMapsOutputTokensDetailsToImgO(t *testing.T) {
	usage := &dto.Usage{
		InputTokens:        15,
		OutputTokens:       1352,
		InputTokensDetails: &dto.InputTokenDetails{TextTokens: 15},
		OutputTokensDetails: &dto.OutputTokenDetails{
			ImageTokens: 1120,
			TextTokens:  232,
		},
	}
	normalizeOpenAIUsage(usage)
	require.Equal(t, 1120, usage.CompletionTokenDetails.ImageTokens)
	require.Equal(t, 232, usage.CompletionTokenDetails.TextTokens)
}

func TestOpenaiImageOutputTokensDetailsMapsToImgOAcrossResponseFormats(t *testing.T) {
	const usageJSON = `{"input_tokens":15,"output_tokens":1352,"total_tokens":1367,"input_tokens_details":{"text_tokens":15,"image_tokens":0},"output_tokens_details":{"image_tokens":1120,"text_tokens":232}}`
	for _, tc := range []struct {
		name        string
		body        string
		contentType string
		stream      bool
	}{
		{"non-stream JSON", `{"data":[{"b64_json":"image"}],"usage":` + usageJSON + `}`, "application/json", false},
		{"JSON wrapped as SSE", `{"data":[{"b64_json":"image"}],"usage":` + usageJSON + `}`, "application/json", true},
		{"SSE", "data: {\"type\":\"image_generation.completed\",\"usage\":" + usageJSON + "}\n\ndata: [DONE]\n\n", "text/event-stream", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, response, info := newImageCountTestContext(t, tc.body, tc.contentType, tc.stream)
			info.RelayMode = relayconstant.RelayModeImagesGenerations
			result, apiErr := (&Adaptor{}).DoResponse(ctx, response, info)
			require.Nil(t, apiErr)
			usage := result.(*dto.Usage)
			require.Equal(t, 15, usage.PromptTokens)
			require.Equal(t, 1352, usage.CompletionTokens)
			require.Equal(t, 1120, usage.CompletionTokenDetails.ImageTokens)
			require.Equal(t, 232, usage.CompletionTokenDetails.TextTokens)
		})
	}
}
