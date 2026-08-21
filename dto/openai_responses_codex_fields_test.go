package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestOpenAIResponsesRequestPreservesCodexPassthroughFields reproduces a real
// bug: Codex CLI sends client_metadata on /v1/responses, and extended
// reasoning requests carry reasoning.mode/reasoning.context, but the DTO had
// no fields to unmarshal them into - both were silently dropped instead of
// being forwarded upstream.
func TestOpenAIResponsesRequestPreservesCodexPassthroughFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5-codex",
		"client_metadata":{"conversation_id":"abc123"},
		"reasoning":{"effort":"high","summary":"detailed","mode":"extended","context":{"turn":1}}
	}`)

	var req OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "client_metadata").Exists())
	require.Equal(t, "abc123", gjson.GetBytes(encoded, "client_metadata.conversation_id").String())
	require.True(t, gjson.GetBytes(encoded, "reasoning.mode").Exists())
	require.Equal(t, "extended", gjson.GetBytes(encoded, "reasoning.mode").String())
	require.True(t, gjson.GetBytes(encoded, "reasoning.context").Exists())
	require.Equal(t, float64(1), gjson.GetBytes(encoded, "reasoning.context.turn").Float())
}
