package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Upstream bug 1086038f5 lived in a converter this fork does not have. Our handler
// keys tool-call state by call_id and ignores the output of response.completed.
// This pins that a function_call repeated in response.completed cannot cause a
// second emission.
func TestResponsesToChatStreamEmitsToolCallOnce(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	functionCall := `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Seoul\"}","status":"completed"}`
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"","status":"in_progress"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"city\":"}`,
		`{"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":0,"arguments":"{\"city\":\"Seoul\"}"}`,
		`{"type":"response.output_item.done","output_index":0,"item":` + functionCall + `}`,
		`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[` + functionCall + `],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`,
	}
	lines := make([]string, 0, len(events)*2)
	for _, e := range events {
		lines = append(lines, "data: "+e, "")
	}
	body := strings.Join(lines, "\n") + "\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
		IsStream:    true,
		RelayFormat: types.RelayFormatOpenAI,
	}

	usage, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 15, usage.TotalTokens)

	argsByIndex := map[int]string{}
	idsByIndex := map[int]map[string]bool{}
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		payload, ok := strings.CutPrefix(line, "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.UnmarshalJsonStr(payload, &chunk))
		for _, choice := range chunk.Choices {
			for _, tc := range choice.Delta.ToolCalls {
				require.NotNil(t, tc.Index)
				idx := *tc.Index
				argsByIndex[idx] += tc.Function.Arguments
				if idsByIndex[idx] == nil {
					idsByIndex[idx] = map[string]bool{}
				}
				if tc.ID != "" {
					idsByIndex[idx][tc.ID] = true
				}
			}
		}
	}

	indexes := make([]int, 0, len(argsByIndex))
	for idx := range argsByIndex {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	require.Equal(t, []int{0}, indexes, "tool call must be emitted under a single index")
	require.Equal(t, `{"city":"Seoul"}`, argsByIndex[0])
	require.Equal(t, map[string]bool{"call_1": true}, idsByIndex[0])
}
