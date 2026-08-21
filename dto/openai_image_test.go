package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImageRequestStreamJSON reproduces a real feature gap: the OpenAI
// Images API supports stream=true (partial image events), but ImageRequest
// had no Stream field at all and IsStream always returned false, so a
// streaming image request was silently relayed as non-streaming.
func TestImageRequestStreamJSON(t *testing.T) {
	var req ImageRequest
	require.NoError(t, req.UnmarshalJSON([]byte(`{"model":"gpt-image-1","prompt":"draw a cat","stream":true}`)))

	require.True(t, req.Stream)
	require.True(t, req.IsStream(nil))
}
