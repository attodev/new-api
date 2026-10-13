// dto/claude_passthrough_test.go
package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestClaudeRequestRoundTripsPassthroughFields(t *testing.T) {
	raw := `{"model":"claude-opus-4-7","max_tokens":10,
	  "safeguards":{"mode":"auto"},
	  "thinking":{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"}},
	  "messages":[{"role":"user","content":"hi"},{"role":"user","content":[],"output_config":{"effort":"low"}}]}`
	var req ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(raw, &req))
	require.JSONEq(t, `{"mode":"auto"}`, string(req.Safeguards))
	require.JSONEq(t, `{"prefix_mismatch_behavior":"drop_block"}`, string(req.Thinking.BlockBinding))
	require.JSONEq(t, `{"effort":"low"}`, string(req.Messages[1].OutputConfig))

	out, err := common.Marshal(req)
	require.NoError(t, err)
	require.Contains(t, string(out), `"safeguards":{"mode":"auto"}`)
	require.Contains(t, string(out), `"block_binding":{"prefix_mismatch_behavior":"drop_block"}`)
	require.Contains(t, string(out), `"output_config":{"effort":"low"}`)
}

func TestClaudeThinkingOmitsBlockBindingWhenAbsent(t *testing.T) {
	out, err := common.Marshal(Thinking{Type: "adaptive"})
	require.NoError(t, err)
	require.NotContains(t, string(out), "block_binding")
}
