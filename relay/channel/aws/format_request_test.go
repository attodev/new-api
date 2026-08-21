package aws

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFormatRequest_PreservesExplicitZeroValues reproduces a CLAUDE.md Rule 6
// violation: max_tokens/top_p/top_k were non-pointer with omitempty, so a
// client explicitly setting one of them to 0 (a valid, if unusual, request)
// had that value silently dropped instead of forwarded to Bedrock.
func TestFormatRequest_PreservesExplicitZeroValues(t *testing.T) {
	body := `{
		"messages": [{"role":"user","content":"hi"}],
		"max_tokens": 0,
		"top_p": 0,
		"top_k": 0
	}`

	req, err := formatRequest(strings.NewReader(body), map[string][]string{})
	require.NoError(t, err)
	require.NotNil(t, req.MaxTokens, "explicit max_tokens=0 must be preserved, not treated as absent")
	require.Equal(t, uint(0), *req.MaxTokens)
	require.NotNil(t, req.TopP)
	require.Equal(t, float64(0), *req.TopP)
	require.NotNil(t, req.TopK)
	require.Equal(t, 0, *req.TopK)
}

func TestFormatRequest_OmittedFieldsStayNil(t *testing.T) {
	body := `{"messages": [{"role":"user","content":"hi"}]}`

	req, err := formatRequest(strings.NewReader(body), map[string][]string{})
	require.NoError(t, err)
	require.Nil(t, req.MaxTokens)
	require.Nil(t, req.TopP)
	require.Nil(t, req.TopK)
}
