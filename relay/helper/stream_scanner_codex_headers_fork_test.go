package helper

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Ported from team 9b4fed08a: Codex CLI tracks turn state through the
// X-Reasoning-Included and X-Codex-Turn-State response headers, so only those
// upstream headers are copied onto a streamed response.
func TestCopyCodexSSEHeadersCopiesOnlyCodexHeaders(t *testing.T) {
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

func TestCopyCodexSSEHeadersNilInputs(t *testing.T) {
	assert.NotPanics(t, func() {
		copyCodexSSEHeaders(nil, nil)
	})

	c, resp, _ := setupStreamTest(t, nil)
	assert.NotPanics(t, func() {
		copyCodexSSEHeaders(c, nil)
	})
	resp.Header = nil
	assert.NotPanics(t, func() {
		copyCodexSSEHeaders(c, resp)
	})
	assert.Empty(t, c.Writer.Header().Get("X-Codex-Turn-State"))
}
