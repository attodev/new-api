package common

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// TimingResponseWriter wraps gin.ResponseWriter to record the timestamp of
// the most recent Write()/WriteString() call. It is used to approximate the
// end-to-end latency's end point ("last byte written to the client"),
// including the final chunk of a streaming response.
//
// Not safe for concurrent use: it assumes the single per-request goroutine
// model of net/http/gin, where all writes to c.Writer happen sequentially
// from the request-handling goroutine.
type TimingResponseWriter struct {
	gin.ResponseWriter
	lastWrite time.Time
}

func (w *TimingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.lastWrite = time.Now()
	return n, err
}

func (w *TimingResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.lastWrite = time.Now()
	return n, err
}

// Unwrap exposes the wrapped writer to http.ResponseController. Without it,
// SetWriteDeadline/SetReadDeadline/EnableFullDuplex cannot reach gin's own
// (*responseWriter).Unwrap and return http.ErrNotSupported, which would
// silently disable the stream write deadline (relay/helper
// StreamWriteBudget) on every relay route.
func (w *TimingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// LastWriteTime returns the timestamp of the last Write()/WriteString()
// call. Zero value means nothing has been written yet.
func (w *TimingResponseWriter) LastWriteTime() time.Time {
	return w.lastWrite
}

// HasWritten reports whether any body bytes were written through this
// writer. It stays false for hijacked connections (e.g. realtime WebSocket
// sessions), whose frames bypass the writer.
func (w *TimingResponseWriter) HasWritten() bool {
	return !w.lastWrite.IsZero()
}

// GetLastWriteTime returns the last-write timestamp recorded by a
// TimingResponseWriter installed on c.Writer. ok is false when c.Writer was
// never wrapped (non-relay routes) or nothing was written through it (e.g. a
// hijacked WebSocket); callers must then skip end-to-end metrics instead of
// guessing an end point.
func GetLastWriteTime(c *gin.Context) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	if w, ok := c.Writer.(*TimingResponseWriter); ok && w.HasWritten() {
		return w.LastWriteTime(), true
	}
	return time.Time{}, false
}
