package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O7 — ported from team router/relay-router_test.go.
//
// TestRequestTimingAppliesEngineWide guards the invariant that
// middleware.RequestTiming(), registered first in SetRelayRouter, ends up on
// every route registered on the same *gin.Engine afterwards (relay, video,
// task, plugin NoRoute). gin.Engine.Use() mutates the engine's root
// Handlers and Group()/handle() combine whatever Handlers exist at that
// time, so any route created after SetRelayRouter inherits RequestTiming.
// Routes registered before it (SetApiRouter/SetDashboardRouter run earlier
// in SetRouter) are deliberately untouched, as on team.
//
// Approach: only a probe route is exercised. It is registered on the same
// engine AFTER SetRelayRouter, and its handler asserts the wrapped writer and
// the entry key. Real relay routes are not requested here; they share the
// same engine-level handler chain (RequestTiming first) that the probe
// inherits, because they are registered after the same Use() call.
func TestRequestTimingAppliesEngineWide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetRelayRouter(r)

	var sawEntryTime, sawTimingWriter bool
	r.GET("/__timing_probe", func(c *gin.Context) {
		sawEntryTime = !common.GetContextKeyTime(c, constant.ContextKeyGatewayEntryTime).IsZero()
		_, sawTimingWriter = c.Writer.(*common.TimingResponseWriter)
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/__timing_probe", nil))

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, sawEntryTime, "gateway entry time must be set engine-wide once RequestTiming is registered in SetRelayRouter")
	require.True(t, sawTimingWriter, "c.Writer must be wrapped by *common.TimingResponseWriter")
}

// TestTimingWriterSurvivesPluginGatedWriter: the plugin dispatcher
// (router/plugin-router.go) wraps the outer c.Writer — already the
// *common.TimingResponseWriter installed by RequestTiming, since the plugin
// NoRoute chain is built after SetRelayRouter — in a gatedResponseWriter.
// Writes that pass the gate forward to the timing writer, so last-write
// tracking keeps working; gated (unmatched) writes are dropped and must not
// move the timestamp.
func TestTimingWriterSurvivesPluginGatedWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	timing := &common.TimingResponseWriter{ResponseWriter: c.Writer}

	gated := newGatedResponseWriter(timing)
	_, err := gated.Write([]byte("dropped"))
	require.NoError(t, err)
	require.True(t, timing.LastWriteTime().IsZero(), "inactive gate must not touch the timing writer")

	gated.activate()
	_, err = gated.Write([]byte("plugin body"))
	require.NoError(t, err)
	require.False(t, timing.LastWriteTime().IsZero())
}
