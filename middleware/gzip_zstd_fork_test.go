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

// Ported from team 22f50eaf5: a zstd-encoded request body must be decoded
// before binding, and Content-Encoding must be cleared for downstream handlers.
func TestDecompressRequestMiddlewareZstd(t *testing.T) {
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
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.String(http.StatusOK, "%s|%s", body, c.GetHeader("Content-Encoding"))
	})

	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, string(want)+"|", recorder.Body.String())
}
