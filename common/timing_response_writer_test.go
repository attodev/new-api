package common

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O7 — relay/helper/common.go asserts c.Writer.(http.Flusher) for SSE;
// the wrapper must keep satisfying it (and gin.ResponseWriter) by embedding.
var (
	_ gin.ResponseWriter = (*TimingResponseWriter)(nil)
	_ http.Flusher       = (*TimingResponseWriter)(nil)
)

func TestTimingResponseWriter_PreservesFlusher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Writer = &TimingResponseWriter{ResponseWriter: c.Writer}

	flusher, ok := c.Writer.(http.Flusher)
	require.True(t, ok)
	flusher.Flush()
}

func TestTimingResponseWriter_TracksLastWriteTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	require.True(t, writer.LastWriteTime().IsZero())

	before := time.Now()
	_, err := c.Writer.Write([]byte("chunk-1"))
	require.NoError(t, err)
	after := time.Now()

	got := writer.LastWriteTime()
	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

func TestTimingResponseWriter_WriteStringUpdatesTimestamp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	_, err := c.Writer.WriteString("hello")
	require.NoError(t, err)
	require.False(t, writer.LastWriteTime().IsZero())
}

func TestGetLastWriteTime_NotOkWhenNotWrapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	got, ok := GetLastWriteTime(c)
	require.False(t, ok)
	require.True(t, got.IsZero())
}

// A wrapped writer with no writes (e.g. a hijacked realtime WebSocket whose
// frames bypass c.Writer) must not report an end point.
func TestGetLastWriteTime_NotOkWhenNothingWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer

	got, ok := GetLastWriteTime(c)
	require.False(t, ok)
	require.True(t, got.IsZero())
	require.False(t, writer.HasWritten())
}

func TestGetLastWriteTime_ReturnsWrappedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	_, err := c.Writer.Write([]byte("x"))
	require.NoError(t, err)

	got, ok := GetLastWriteTime(c)
	require.True(t, ok)
	require.True(t, writer.HasWritten())
	require.Equal(t, writer.LastWriteTime(), got)
}

// noUnwrapTimingWriter mirrors TimingResponseWriter without Unwrap, to show
// what the Unwrap method protects against.
type noUnwrapTimingWriter struct {
	gin.ResponseWriter
}

// TestTimingResponseWriter_KeepsWriteDeadlineSupport runs a real HTTP server
// whose gin engine wraps c.Writer exactly like middleware.RequestTiming and
// checks that http.ResponseController can still set write deadlines (used by
// relay/helper.StreamWriteBudget for every stream write and ping).
func TestTimingResponseWriter_KeepsWriteDeadlineSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var wrappedErr, unwrappedErr error
	engine := gin.New()
	engine.GET("/wrapped", func(c *gin.Context) {
		c.Writer = &TimingResponseWriter{ResponseWriter: c.Writer}
		wrappedErr = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Second))
		c.String(http.StatusOK, "ok")
	})
	engine.GET("/no-unwrap", func(c *gin.Context) {
		c.Writer = &noUnwrapTimingWriter{ResponseWriter: c.Writer}
		unwrappedErr = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Second))
		c.String(http.StatusOK, "ok")
	})
	server := httptest.NewServer(engine)
	defer server.Close()

	for _, path := range []string{"/wrapped", "/no-unwrap"} {
		resp, err := http.Get(server.URL + path)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	require.NoError(t, wrappedErr)
	require.ErrorIs(t, unwrappedErr, http.ErrNotSupported, "without Unwrap the deadline cannot reach the connection")
}
