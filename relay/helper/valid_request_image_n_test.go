package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGetAndValidOpenAIImageRequest_RejectsOversizedN reproduces a real
// billing-overflow bug: an unbounded image generation count "n" is later
// multiplied into the quota calculation (price * n); a huge or malicious n
// can overflow that product into a negative charge instead of being
// rejected up front.
func TestGetAndValidOpenAIImageRequest_RejectsOversizedN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := `{"model":"dall-e-3","prompt":"a cat","n":999999999999}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader([]byte(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	require.Error(t, err, "an n far beyond any plausible batch size must be rejected, not silently accepted")
}

func TestGetAndValidOpenAIImageRequest_MultipartRejectsOversizedN(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := &bytes.Buffer{}
	body.WriteString("--X\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\ngpt-image-1\r\n")
	body.WriteString("--X\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\na cat\r\n")
	body.WriteString("--X\r\nContent-Disposition: form-data; name=\"n\"\r\n\r\n999999999999\r\n")
	body.WriteString("--X--\r\n")

	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", strings.NewReader(body.String()))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=X")

	_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.Error(t, err, "the multipart form path must enforce the same n bound as the JSON path")
}
