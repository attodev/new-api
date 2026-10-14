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

func TestGetLastWriteTime_FallsBackWhenNotWrapped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	before := time.Now()
	got := GetLastWriteTime(c)
	after := time.Now()

	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

func TestGetLastWriteTime_ReturnsWrappedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writer := &TimingResponseWriter{ResponseWriter: c.Writer}
	c.Writer = writer
	_, err := c.Writer.Write([]byte("x"))
	require.NoError(t, err)

	require.Equal(t, writer.LastWriteTime(), GetLastWriteTime(c))
}
