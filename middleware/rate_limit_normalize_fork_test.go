package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRateLimitParameters(t *testing.T) {
	maxDur := int64(common.RateLimitKeyExpirationDuration / time.Second)
	cases := []struct {
		name    string
		num     int
		dur     int64
		wantNum int
		wantDur int64
	}{
		{"valid passes through", 100, 60, 100, 60},
		{"zero requests becomes 1", 0, 60, 1, 60},
		{"negative duration becomes 1", 100, -5, 100, 1},
		{"duration above key expiry is clamped", 100, maxDur + 1, 100, maxDur},
		{"duration at key expiry passes through", 100, maxDur, 100, maxDur},
		{"both invalid", -1, 0, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			num, dur := normalizeRateLimitParameters(tc.num, tc.dur)
			require.Equal(t, tc.wantNum, num)
			require.Equal(t, tc.wantDur, dur)
		})
	}
}

func TestRateLimitFactoryUsesNormalizedValues(t *testing.T) {
	// memory limiter path: 0 requests must not block everyone
	old := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = old })

	gin.SetMode(gin.TestMode)
	handler := rateLimitFactory(0, 60, "fork-normalize-test")
	limited := func() bool {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.RemoteAddr = "192.0.2.77:1234"
		handler(c)
		return c.IsAborted() && c.Writer.Status() == http.StatusTooManyRequests
	}
	// first request passes (normalized to 1 request / 60s), second is limited
	require.False(t, limited())
	require.True(t, limited())
}
