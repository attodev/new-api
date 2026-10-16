package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team c33293ac3. Team forwards tools/reasoning/text on
// /v1/responses/compact through a compactionRequestToResponsesRequest helper;
// develop maps the request inline in ResponsesHelper and intentionally forwards
// only the documented compact fields. This asserts develop's behaviour on the
// actual upstream body: cache/service fields reach upstream, tools/reasoning/
// text do not.
func TestResponsesHelperCompactionForwardsCacheAndServiceFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	captured := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- body
		// Stop after the request is captured; no response settlement needed.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop","type":"invalid_request_error"}}`))
	}))
	defer server.Close()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.1")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	// service_tier is stripped by RemoveDisabledFields unless the channel opts in.
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: true})

	request := &dto.OpenAIResponsesCompactionRequest{
		Model:                "gpt-5.1",
		Input:                json.RawMessage(`"hi"`),
		Instructions:         json.RawMessage(`"be nice"`),
		PreviousResponseID:   "resp_123",
		PromptCacheKey:       json.RawMessage(`"my-cache-key"`),
		PromptCacheRetention: json.RawMessage(`"24h"`),
		ParallelToolCalls:    json.RawMessage(`true`),
		ServiceTier:          "flex",
		Tools:                json.RawMessage(`[{"type":"function","name":"lookup"}]`),
		Reasoning:            &dto.Reasoning{Effort: "high"},
		Text:                 json.RawMessage(`{"format":{"type":"text"}}`),
	}
	info := relaycommon.GenRelayInfoResponsesCompaction(c, request)

	apiErr := ResponsesHelper(c, info)
	require.NotNil(t, apiErr, "upstream stub answers 400 to stop after capture")

	var upstream map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(<-captured, &upstream))

	assert.JSONEq(t, `"gpt-5.1"`, string(upstream["model"]))
	assert.JSONEq(t, `"hi"`, string(upstream["input"]))
	assert.JSONEq(t, `"be nice"`, string(upstream["instructions"]))
	assert.JSONEq(t, `"resp_123"`, string(upstream["previous_response_id"]))
	assert.JSONEq(t, `"my-cache-key"`, string(upstream["prompt_cache_key"]))
	assert.JSONEq(t, `"24h"`, string(upstream["prompt_cache_retention"]))
	assert.JSONEq(t, `"flex"`, string(upstream["service_tier"]))
	assert.JSONEq(t, `true`, string(upstream["parallel_tool_calls"]))
	for _, key := range []string{"tools", "reasoning", "text"} {
		_, present := upstream[key]
		assert.False(t, present, "undocumented compact field %q must not be forwarded", key)
	}
}
