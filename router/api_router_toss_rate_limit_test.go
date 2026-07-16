package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTrustedTossWebhookOnlyBypassesGlobalAPIRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// The in-memory limiter is intentionally process-global. Use a fresh
	// operator-allowlisted provider IP on every invocation so `-count=N` does not
	// inherit a previous run's bucket.
	seed := uint64(time.Now().UnixNano())
	trustedIP := fmt.Sprintf("198.18.%d.%d", byte(seed>>8), byte(seed))
	untrustedIP := fmt.Sprintf("198.19.%d.%d", byte(seed>>24), byte(seed>>16))
	t.Setenv("TOSS_WEBHOOK_SOURCE_CIDRS", trustedIP)
	t.Setenv("TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS", "")
	oldEnabled := common.GlobalApiRateLimitEnable
	oldLimit := common.GlobalApiRateLimitNum
	oldDuration := common.GlobalApiRateLimitDuration
	oldRedisEnabled := common.RedisEnabled
	common.GlobalApiRateLimitEnable = true
	common.GlobalApiRateLimitNum = 1
	common.GlobalApiRateLimitDuration = 60
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.GlobalApiRateLimitEnable = oldEnabled
		common.GlobalApiRateLimitNum = oldLimit
		common.GlobalApiRateLimitDuration = oldDuration
		common.RedisEnabled = oldRedisEnabled
	})

	engine := gin.New()
	api := engine.Group("/api")
	api.Use(middleware.GlobalAPIRateLimitExcept(isTrustedTossWebhookForGlobalRateLimit))
	api.POST("/toss/webhook", func(c *gin.Context) { c.Status(http.StatusOK) })
	api.GET("/status", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := func(method, path, remoteAddr string) int {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = remoteAddr
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response.Code
	}

	// The provider may legitimately deliver more events than the shared
	// end-user IP bucket permits. Both exact trusted webhook requests pass.
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/toss/webhook", trustedIP+":443"))
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/toss/webhook", trustedIP+":443"))

	// A non-Toss route from the same (otherwise trusted) source remains subject
	// to the ordinary global limit.
	require.Equal(t, http.StatusOK, request(http.MethodGet, "/api/status", trustedIP+":443"))
	require.Equal(t, http.StatusTooManyRequests, request(http.MethodGet, "/api/status", trustedIP+":443"))

	// An untrusted caller never gets the webhook exemption, even on the exact
	// path. This keeps the global limiter as an amplification/DoS boundary.
	require.Equal(t, http.StatusOK, request(http.MethodPost, "/api/toss/webhook", untrustedIP+":443"))
	require.Equal(t, http.StatusTooManyRequests, request(http.MethodPost, "/api/toss/webhook", untrustedIP+":443"))
}
