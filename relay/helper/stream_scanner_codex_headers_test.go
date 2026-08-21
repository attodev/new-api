package helper

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCopyCodexSSEHeaders reproduces a real bug: Codex CLI relies on the
// "X-Reasoning-Included" and "X-Codex-Turn-State" response headers to track
// turn state across a streamed /v1/responses call, but StreamScannerHandler
// only ever set SSE framing headers - it never copied these upstream
// response headers onto the client response at all.
func TestCopyCodexSSEHeaders(t *testing.T) {
	c, resp, _ := setupStreamTest(t, nil)
	resp.Header = http.Header{
		"X-Reasoning-Included": []string{"true"},
		"X-Codex-Turn-State":   []string{"opaque-state-token"},
		"X-Unrelated-Header":   []string{"should-not-be-copied"},
	}

	copyCodexSSEHeaders(c, resp)

	assert.Equal(t, "true", c.Writer.Header().Get("X-Reasoning-Included"))
	assert.Equal(t, "opaque-state-token", c.Writer.Header().Get("X-Codex-Turn-State"))
	assert.Empty(t, c.Writer.Header().Get("X-Unrelated-Header"))
}

func TestCopyCodexSSEHeaders_NilInputs(t *testing.T) {
	assert.NotPanics(t, func() {
		copyCodexSSEHeaders(nil, nil)
	})
}
