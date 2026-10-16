package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	rdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRelaykitClaudeRequestRoundTripsPassthroughFields(t *testing.T) {
	raw := `{"model":"claude-opus-4-7","max_tokens":10,
	  "safeguards":{"mode":"auto"},
	  "thinking":{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"}},
	  "messages":[{"role":"user","content":"hi"},{"role":"user","content":[],"output_config":{"effort":"low"}}]}`
	var req rdto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(raw, &req))
	require.JSONEq(t, `{"mode":"auto"}`, string(req.Safeguards))

	out, err := common.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"auto"}`, gjson.GetBytes(out, "safeguards").Raw)
	require.JSONEq(t, `{"prefix_mismatch_behavior":"drop_block"}`, gjson.GetBytes(out, "thinking.block_binding").Raw)
	require.JSONEq(t, `{"effort":"low"}`, gjson.GetBytes(out, "messages.1.output_config").Raw)
}

func TestRelaykitClaudeThinkingOmitsBlockBindingWhenAbsent(t *testing.T) {
	out, err := common.Marshal(rdto.Thinking{Type: "adaptive"})
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(out, "block_binding").Exists())
}
