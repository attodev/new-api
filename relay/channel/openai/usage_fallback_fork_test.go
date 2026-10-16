package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// An upstream that omits usage on a tool-calling turn must still bill the
// tool call it generated: the completion fallback counts tool names and
// arguments, not only message content. Ported from team c0b313844.
func TestOpenaiHandlerMissingUsageCountsToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	body := []byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"gpt-5","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]}}]}`)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}
	info.SetEstimatePromptTokens(100)

	usage, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, tokenkit.Count("gpt-5", `read_file{"path":"main.go"}`), usage.CompletionTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestFallbackCompletionTextIncludesToolCalls(t *testing.T) {
	var msg dto.Message
	require.NoError(t, common.UnmarshalJsonStr(`{"role":"assistant","content":"hi","reasoning_content":"think","tool_calls":[`+
		`{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}},`+
		`{"id":"c2","type":"function","function":{"name":"ls","arguments":"{}"}},`+
		`{"id":"c3","type":"custom","custom":{"name":"apply_patch","input":"x"}}]}`, &msg))

	require.Equal(t,
		`hithinkread_file{"path":"a"}ls{}{"name":"apply_patch","input":"x"}`,
		fallbackCompletionText(&msg))
}
