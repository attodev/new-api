package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	rdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Codex CLI sends client_metadata on /v1/responses and extended reasoning
// requests carry reasoning.mode/context; they must survive remarshal so they
// reach the upstream.
func TestRelaykitOpenAIResponsesRequestPreservesCodexPassthroughFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5-codex",
		"client_metadata":{"conversation_id":"abc123"},
		"reasoning":{"effort":"high","summary":"detailed","mode":"extended","context":{"turn":1}}
	}`)

	var req rdto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &req))
	require.JSONEq(t, `{"conversation_id":"abc123"}`, string(req.ClientMetadata))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.Equal(t, "abc123", gjson.GetBytes(encoded, "client_metadata.conversation_id").String())
	require.Equal(t, "extended", gjson.GetBytes(encoded, "reasoning.mode").String())
	require.True(t, gjson.GetBytes(encoded, "reasoning.context").Exists())
	require.Equal(t, float64(1), gjson.GetBytes(encoded, "reasoning.context.turn").Float())
}
