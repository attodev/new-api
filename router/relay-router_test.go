package router

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestRequestTimingAppliesEngineWide guards the invariant that
// middleware.RequestTiming(), registered first in SetRelayRouter, ends up
// on every route group registered on the same *gin.Engine afterward —
// including SetVideoRouter's video/kling/jimeng groups, which are set up
// after SetRelayRouter in router.SetRouter. gin.Engine.Use() mutates the
// engine's shared RouterGroup.Handlers, and RouterGroup.Group() combines
// whatever Handlers exist at the time it's called, so any group created
// after SetRelayRouter runs — regardless of which SetXRouter function
// creates it — inherits RequestTiming without needing to call it again.
func TestRequestTimingAppliesEngineWide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetApiRouter(r)
	SetDashboardRouter(r)
	SetRelayRouter(r)
	SetVideoRouter(r)

	var sawGatewayEntryTime bool
	// A group registered after SetVideoRouter sees the exact same base
	// Handlers state SetVideoRouter's own groups captured, since nothing
	// mutates the engine's root Handlers in between.
	probe := r.Group("/zz-request-timing-probe")
	probe.GET("/x", func(c *gin.Context) {
		entryTime := common.GetContextKeyTime(c, constant.ContextKeyGatewayEntryTime)
		sawGatewayEntryTime = !entryTime.IsZero()
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/zz-request-timing-probe/x", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.True(t, sawGatewayEntryTime,
		"gateway entry time must propagate engine-wide once RequestTiming is registered in SetRelayRouter")
}
