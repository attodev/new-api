package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const responsesStreamUsageEstimate = 42

func runResponsesStreamUsage(t *testing.T, events ...string) (promptTokens, completionTokens, totalTokens int) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	lines := make([]string, 0, len(events)*2)
	for _, e := range events {
		lines = append(lines, "data: "+e, "")
	}
	body := strings.Join(lines, "\n") + "\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
		IsStream:    true,
	}
	info.SetEstimatePromptTokens(responsesStreamUsageEstimate)

	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	return usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens
}

func TestResponsesStreamUsageEstimatedFromDeltas(t *testing.T) {
	cases := map[string]string{
		"function_call_arguments": `{"type":"response.function_call_arguments.delta","delta":"{\"city\":\"Seoul\",\"unit\":\"celsius\"}"}`,
		"reasoning_summary_text":  `{"type":"response.reasoning_summary_text.delta","delta":"The user wants the weather in Seoul."}`,
		"reasoning_text":          `{"type":"response.reasoning_text.delta","delta":"Let me think about the weather in Seoul."}`,
		"refusal":                 `{"type":"response.refusal.delta","delta":"I cannot help with that request."}`,
		"output_text":             `{"type":"response.output_text.delta","delta":"Seoul is sunny today."}`,
	}
	for name, delta := range cases {
		t.Run(name, func(t *testing.T) {
			prompt, completion, total := runResponsesStreamUsage(t,
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				delta,
			)
			require.Greater(t, completion, 0)
			require.Equal(t, responsesStreamUsageEstimate, prompt)
			require.Equal(t, prompt+completion, total)
		})
	}
}

func TestResponsesStreamUsageBillsPromptWhenOnlyCreated(t *testing.T) {
	prompt, completion, total := runResponsesStreamUsage(t,
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
	)
	require.Equal(t, 0, completion)
	require.Equal(t, responsesStreamUsageEstimate, prompt)
	require.Equal(t, responsesStreamUsageEstimate, total)
}

func TestResponsesStreamUsageBillsNothingOnUpstreamFailure(t *testing.T) {
	t.Run("response.failed", func(t *testing.T) {
		prompt, completion, total := runResponsesStreamUsage(t,
			`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
			`{"type":"response.failed","response":{"id":"resp_1","status":"failed"}}`,
		)
		require.Equal(t, 0, prompt)
		require.Equal(t, 0, completion)
		require.Equal(t, 0, total)
	})
	t.Run("error", func(t *testing.T) {
		prompt, completion, total := runResponsesStreamUsage(t,
			`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
			`{"type":"error","message":"upstream exploded"}`,
		)
		require.Equal(t, 0, prompt)
		require.Equal(t, 0, completion)
		require.Equal(t, 0, total)
	})
}

func TestResponsesStreamUsageKeepsReportedUsageOnCompleted(t *testing.T) {
	prompt, completion, total := runResponsesStreamUsage(t,
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_text.delta","delta":"hi"}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`,
	)
	require.Equal(t, 11, prompt)
	require.Equal(t, 7, completion)
	require.Equal(t, 18, total)
}

func runResponsesStreamUsageDetails(t *testing.T, events ...string) (prompt, completion, cached int) {
	t.Helper()
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	lines := make([]string, 0, len(events)*2)
	for _, e := range events {
		lines = append(lines, "data: "+e, "")
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(strings.Join(lines, "\n") + "\n")),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
		IsStream:    true,
	}
	info.SetEstimatePromptTokens(responsesStreamUsageEstimate)
	usage, err := OaiResponsesStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.NotNil(t, usage)
	return usage.PromptTokens, usage.CompletionTokens, usage.PromptTokensDetails.CachedTokens
}

func TestResponsesStreamUsageReadFromNonCompletedTerminalEvents(t *testing.T) {
	const usageJSON = `"usage":{"input_tokens":1000,"output_tokens":4000,"input_tokens_details":{"cached_tokens":800}}`
	for _, eventType := range []string{"response.incomplete", "response.done", "response.cancelled", "response.failed"} {
		t.Run(eventType, func(t *testing.T) {
			prompt, completion, cached := runResponsesStreamUsageDetails(t,
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				`{"type":"response.output_text.delta","delta":"partial"}`,
				`{"type":"`+eventType+`","response":{"id":"resp_1",`+usageJSON+`}}`,
			)
			require.Equal(t, 1000, prompt)
			require.Equal(t, 4000, completion)
			require.Equal(t, 800, cached)
		})
	}
}

func TestResponsesStreamUsageFailedWithoutUsageStillBillsNothing(t *testing.T) {
	prompt, completion, cached := runResponsesStreamUsageDetails(t,
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.failed","response":{"id":"resp_1","status":"failed"}}`,
	)
	require.Equal(t, 0, prompt)
	require.Equal(t, 0, completion)
	require.Equal(t, 0, cached)
}
