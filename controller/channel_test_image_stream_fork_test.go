package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// fork: O11 — the channel test must carry the stream toggle into the image
// generation request (team ecc9c511d).
func TestBuildTestRequestImageGenerationCarriesStream(t *testing.T) {
	for _, stream := range []bool{true, false} {
		req, ok := buildTestRequest("gpt-image-1", string(constant.EndpointTypeImageGeneration), &model.Channel{}, stream).(*dto.ImageRequest)
		require.True(t, ok)
		require.NotNil(t, req.Stream)
		require.Equal(t, stream, *req.Stream)
	}
}
