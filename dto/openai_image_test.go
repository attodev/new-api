package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestImageRequestStreamJSON reproduces a real feature gap: the OpenAI
// Images API supports stream=true (partial image events), but ImageRequest
// had no Stream field at all and IsStream always returned false, so a
// streaming image request was silently relayed as non-streaming.
func TestImageRequestStreamJSON(t *testing.T) {
	var req ImageRequest
	require.NoError(t, req.UnmarshalJSON([]byte(`{"model":"gpt-image-1","prompt":"draw a cat","stream":true}`)))

	require.True(t, req.IsStream(nil))
}

// TestImageRequestStreamPreservesExplicitFalse reproduces a CLAUDE.md Rule 6
// violation: Stream was a non-pointer bool with omitempty, so an explicit
// client "stream": false was indistinguishable from "field omitted" once the
// request got remarshaled toward an upstream (the JSON-body ConvertImageRequest
// path forwards the whole struct as-is) - any upstream whose own default for
// a missing stream field differs from false would silently lose that intent.
func TestImageRequestStreamPreservesExplicitFalse(t *testing.T) {
	var req ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-1","prompt":"draw a cat","stream":false}`), &req))
	require.False(t, req.IsStream(nil))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(encoded, "stream").Exists(), "explicit stream:false must survive remarshal, not be dropped as a zero value")
}
