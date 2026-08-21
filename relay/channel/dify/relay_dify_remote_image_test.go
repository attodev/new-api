package dify

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestRequestOpenAI2Dify_RemoteImageDoesNotPanic reproduces a real bug: the
// remote-image branch assigned fields on a nil *DifyFile (declared but never
// allocated), panicking with a nil pointer dereference (a 500) for any
// multimodal message containing a remote image URL.
func TestRequestOpenAI2Dify_RemoteImageDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	request := dto.GeneralOpenAIRequest{
		Model: "dify-app",
		Messages: []dto.Message{
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "what is this?"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
				},
			},
		},
	}

	var difyReq *DifyChatRequest
	require.NotPanics(t, func() {
		difyReq = requestOpenAI2Dify(c, &relaycommon.RelayInfo{}, request)
	})

	require.Len(t, difyReq.Files, 1)
	require.Equal(t, "remote_url", difyReq.Files[0].TransferMode)
	require.Equal(t, "https://example.com/cat.png", difyReq.Files[0].URL)
}
