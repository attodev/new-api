package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

// TestDecompressRequestMiddleware_Zstd reproduces a real protocol gap: a
// client sending Content-Encoding: zstd (a common choice for large request
// bodies, e.g. base64 image payloads) hit the "default" branch, which
// treats the body as already-plain and passes the raw zstd frame straight
// through to JSON binding - a client using zstd could never actually reach
// this API at all.
func TestDecompressRequestMiddleware_Zstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	want := []byte(`{"hello":"world"}`)
	encoder, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := encoder.EncodeAll(want, nil)
	require.NoError(t, encoder.Close())

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/echo", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.String(http.StatusOK, "%s|%s", body, c.GetHeader("Content-Encoding"))
	})

	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, string(want)+"|", recorder.Body.String())
}
