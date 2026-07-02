package common

import (
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

// LastWriteTime returns the timestamp of the last Write()/WriteString()
// call. Zero value means nothing has been written yet.
func (w *TimingResponseWriter) LastWriteTime() time.Time {
	return w.lastWrite
}

// GetLastWriteTime returns the last-write timestamp recorded by a
// TimingResponseWriter installed on c.Writer. If c.Writer was never wrapped
// (e.g. non-relay routes), it falls back to time.Now() so callers always get
// a usable (if slightly less precise) value.
func GetLastWriteTime(c *gin.Context) time.Time {
	if w, ok := c.Writer.(*TimingResponseWriter); ok {
		if !w.LastWriteTime().IsZero() {
			return w.LastWriteTime()
		}
	}
	return time.Now()
}
