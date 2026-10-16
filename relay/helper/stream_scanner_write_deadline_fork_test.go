package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Ported from team fac49f5de: ExtendWriteDeadline bounds each locked stream
// write, and must be a no-op on a nil context or on writers without deadline
// support (httptest recorders).
func TestExtendWriteDeadlineNilSafe(t *testing.T) {
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
