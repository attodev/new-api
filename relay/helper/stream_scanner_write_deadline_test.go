package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestExtendWriteDeadline_NilSafe reproduces a gap this session's port of
// "avoid stale stream writes after client disconnect" (153d7f01a) left open:
// removing the old bounded wg.Wait() timeout without also bounding each
// locked stream write means a write that blocks forever (a slow-but-connected
// client whose TCP window never drains, so c.Request.Context() never fires)
// can now hang StreamScannerHandler's cleanup indefinitely. ExtendWriteDeadline
// closes that gap by pushing a hard per-write deadline before each locked
// write. It must be safe to call on writers that don't support deadlines
// (e.g. httptest recorders) or on a nil context.
func TestExtendWriteDeadline_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		ExtendWriteDeadline(nil)
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	assert.NotPanics(t, func() {
		ExtendWriteDeadline(c)
	})
}
