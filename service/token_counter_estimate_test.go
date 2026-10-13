package service

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// claudeCodeFixture is a Claude-Code-style /v1/messages request: a two-block
// system prompt, 20 tool definitions, and 30 messages of an agent loop
// (tool_use / tool_result round trips).
const claudeCodeFixture = "testdata/claude_code_request.json"

func estimateClaudeCodeFixture(t *testing.T, model string) (int, *types.TokenCountMeta) {
	t.Helper()
	raw, err := os.ReadFile(claudeCodeFixture)
	require.NoError(t, err)
	var request dto.ClaudeRequest
	require.NoError(t, common.Unmarshal(raw, &request))
	meta := request.GetTokenCountMeta()

	oldCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = oldCountToken })

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, model)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, IsStream: true}

	tokens, err := EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	return tokens, meta
}

// The prompt estimate of an agent request must include its tool definitions
// and every tool call and tool result. Before tokenkit, Claude tool
// definitions decoded from client JSON never reached the counter and tool
// results were counted as escaped JSON: the same fixture estimated 3650 tokens
// for both models. Values are pinned so a change in the estimator shows up here.
func TestEstimateRequestTokenClaudeCodeFixture(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  int
	}{
		{"claude-sonnet-4-5-20250929", 7748},
		{"claude-opus-4-7", 10200},
	} {
		t.Run(tc.model, func(t *testing.T) {
			tokens, meta := estimateClaudeCodeFixture(t, tc.model)
			t.Logf("model=%s prompt_estimate=%d combine_text_bytes=%d", tc.model, tokens, len(meta.CombineText))

			require.Equal(t, 30, meta.MessagesCount)
			require.Equal(t, 20, meta.ToolsCount)
			// Tool definitions, tool_use input and tool_result text reach the counter.
			require.Contains(t, meta.CombineText, "Executes a given bash command in a persistent shell session")
			require.Contains(t, meta.CombineText, `"old_string":{"description":"The text to replace","type":"string"}`)
			require.Contains(t, meta.CombineText, "--- FAIL: TestLineTax (0.00s)\n")
			require.False(t, strings.Contains(meta.CombineText, `\tgithub.com/acme`), "tool_result must be counted as raw text, not JSON-escaped")

			require.Equal(t, tc.want, tokens)
		})
	}
}
