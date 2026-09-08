package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// TestBuildTestRequest_ImageGenerationHonorsStreamToggle reproduces a real
// gap: every other endpoint type in buildTestRequest threads the admin's
// stream toggle (?stream=true) through to the built request (e.g.
// GeneralOpenAIRequest/OpenAIResponsesRequest set Stream: lo.ToPtr(isStream)),
// but the image-generation branch never set it - an admin testing an image
// channel with stream=true to validate the new SSE image relay path
// (OpenaiImageStreamHandler) silently got a non-streaming JSON test request
// instead, so the streaming code path could never actually be exercised by
// the channel-test admin feature.
func TestBuildTestRequest_ImageGenerationHonorsStreamToggle(t *testing.T) {
	req := buildTestRequest("gpt-image-1", string(constant.EndpointTypeImageGeneration), nil, true)
	imageReq, ok := req.(*dto.ImageRequest)
	require.True(t, ok)
	require.True(t, imageReq.IsStream(nil))
}

func TestBuildTestRequest_ImageGenerationNonStreamDefault(t *testing.T) {
	req := buildTestRequest("gpt-image-1", string(constant.EndpointTypeImageGeneration), nil, false)
	imageReq, ok := req.(*dto.ImageRequest)
	require.True(t, ok)
	require.False(t, imageReq.IsStream(nil))
}
