package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// TestCompactionRequestToResponsesRequest_ForwardsCacheAndServiceFields
// reproduces a real feature-parity gap: /v1/responses/compact silently
// dropped prompt_cache_key, prompt_cache_retention, parallel_tool_calls,
// service_tier, tools, reasoning, and text when relaying to the underlying
// Responses API - e.g. the client's own cache-key hint never reached
// upstream, hurting cache hit rates, and a compact request that supplied
// tools/reasoning got silently downgraded on retry.
func TestCompactionRequestToResponsesRequest_ForwardsCacheAndServiceFields(t *testing.T) {
	req := &dto.OpenAIResponsesCompactionRequest{
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

	got := compactionRequestToResponsesRequest(req)

	require.Equal(t, req.Model, got.Model)
	require.Equal(t, req.Input, got.Input)
	require.Equal(t, req.Instructions, got.Instructions)
	require.Equal(t, req.PreviousResponseID, got.PreviousResponseID)
	require.Equal(t, req.PromptCacheKey, got.PromptCacheKey)
	require.Equal(t, req.PromptCacheRetention, got.PromptCacheRetention)
	require.Equal(t, req.ParallelToolCalls, got.ParallelToolCalls)
	require.Equal(t, req.ServiceTier, got.ServiceTier)
	require.Equal(t, req.Tools, got.Tools)
	require.Equal(t, req.Reasoning, got.Reasoning)
	require.Equal(t, req.Text, got.Text)
}
