package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	rdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRelaykitImageRequestStreamRoundTrip(t *testing.T) {
	t.Run("stream true", func(t *testing.T) {
		var req rdto.ImageRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-1","prompt":"x","stream":true}`), &req))
		require.True(t, req.IsStream(nil))

		encoded, err := common.Marshal(&req)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(encoded, "stream").Bool())
	})

	t.Run("explicit stream false survives remarshal", func(t *testing.T) {
		var req rdto.ImageRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-1","prompt":"x","stream":false}`), &req))
		require.False(t, req.IsStream(nil))

		encoded, err := common.Marshal(&req)
		require.NoError(t, err)
		stream := gjson.GetBytes(encoded, "stream")
		require.True(t, stream.Exists(), "explicit stream:false must not be dropped as a zero value")
		require.False(t, stream.Bool())
	})

	t.Run("absent stream stays omitted", func(t *testing.T) {
		var req rdto.ImageRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"gpt-image-1","prompt":"x"}`), &req))
		require.False(t, req.IsStream(nil))

		encoded, err := common.Marshal(&req)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(encoded, "stream").Exists())
	})
}
