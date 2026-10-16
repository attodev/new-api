package aws

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Ported from team format_request_test.go (66f767874). CLAUDE.md Rule 6: an
// explicit max_tokens/top_p/top_k of 0 must reach Bedrock, not be dropped as
// if absent.
func TestFormatRequestPreservesExplicitZeroValuesFork(t *testing.T) {
	body := `{
		"messages": [{"role":"user","content":"hi"}],
		"max_tokens": 0,
		"top_p": 0,
		"top_k": 0
	}`

	req, err := formatRequest(strings.NewReader(body), http.Header{})
	require.NoError(t, err)
	require.NotNil(t, req.MaxTokens, "explicit max_tokens=0 must be preserved, not treated as absent")
	require.Equal(t, uint(0), *req.MaxTokens)
	require.NotNil(t, req.TopP)
	require.Equal(t, float64(0), *req.TopP)
	require.NotNil(t, req.TopK)
	require.Equal(t, 0, *req.TopK)

	upstream, err := common.Marshal(req)
	require.NoError(t, err)
	for _, field := range []string{"max_tokens", "top_p", "top_k"} {
		value := gjson.GetBytes(upstream, field)
		require.True(t, value.Exists(), "%s must be sent upstream", field)
		require.Equal(t, int64(0), value.Int(), field)
	}
}

func TestFormatRequestOmittedFieldsStayNilFork(t *testing.T) {
	body := `{"messages": [{"role":"user","content":"hi"}]}`

	req, err := formatRequest(strings.NewReader(body), http.Header{})
	require.NoError(t, err)
	require.Nil(t, req.MaxTokens)
	require.Nil(t, req.TopP)
	require.Nil(t, req.TopK)

	upstream, err := common.Marshal(req)
	require.NoError(t, err)
	for _, field := range []string{"max_tokens", "top_p", "top_k"} {
		require.False(t, gjson.GetBytes(upstream, field).Exists(), "%s must be omitted", field)
	}
}
