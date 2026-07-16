package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRateLimitParametersUsesSafeMinimumForEveryBackend(t *testing.T) {
	for _, redisEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%t", redisEnabled), func(t *testing.T) {
			original := common.RedisEnabled
			common.RedisEnabled = redisEnabled
			t.Cleanup(func() { common.RedisEnabled = original })

			maxRequests, duration := normalizeRateLimitParameters(-10, -20)
			require.Equal(t, 1, maxRequests)
			require.Equal(t, int64(1), duration)
		})
	}
}

func TestNormalizeRateLimitParametersCapsWindowAtKeyTTL(t *testing.T) {
	maxDuration := int64(common.RateLimitKeyExpirationDuration / time.Second)
	require.Positive(t, maxDuration)

	maxRequests, duration := normalizeRateLimitParameters(10, maxDuration+300)

	require.Equal(t, 10, maxRequests)
	require.Equal(t, maxDuration, duration)
}

func TestRateLimitFactoryNegativeConfigurationDoesNotPanicOrDisableLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	engine := gin.New()
	engine.Use(rateLimitFactory(-1, -1, fmt.Sprintf("invalid-config-%d-", time.Now().UnixNano())))
	engine.GET("/payment-callback", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/payment-callback", nil)
		req.RemoteAddr = "198.51.100.25:443"
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response.Code
	}

	require.Equal(t, http.StatusOK, request())
	require.Equal(t, http.StatusTooManyRequests, request())
}

func TestGlobalAndCriticalPaymentLimitersNormalizeInvalidConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	tests := []struct {
		name      string
		configure func(t *testing.T)
		limiter   func() func(*gin.Context)
	}{
		{
			name: "global API",
			configure: func(t *testing.T) {
				oldEnabled, oldMax, oldDuration := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
				common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = true, -1, -1
				t.Cleanup(func() {
					common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = oldEnabled, oldMax, oldDuration
				})
			},
			limiter: GlobalAPIRateLimit,
		},
		{
			name: "critical payment",
			configure: func(t *testing.T) {
				oldEnabled, oldMax, oldDuration := common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration
				common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = true, -1, -1
				t.Cleanup(func() {
					common.CriticalRateLimitEnable, common.CriticalRateLimitNum, common.CriticalRateLimitDuration = oldEnabled, oldMax, oldDuration
				})
			},
			limiter: CriticalRateLimit,
		},
	}

	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.configure(t)
			engine := gin.New()
			engine.Use(testCase.limiter())
			engine.GET("/payment", func(c *gin.Context) { c.Status(http.StatusOK) })

			request := func() int {
				req := httptest.NewRequest(http.MethodGet, "/payment", nil)
				req.RemoteAddr = fmt.Sprintf("192.0.2.%d:443", 100+index)
				response := httptest.NewRecorder()
				engine.ServeHTTP(response, req)
				return response.Code
			}

			require.Equal(t, http.StatusOK, request())
			require.Equal(t, http.StatusTooManyRequests, request())
		})
	}
}

func TestUserRateLimitFactoryNormalizesInvalidSearchConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = originalRedisEnabled })

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", 71)
		c.Next()
	})
	engine.Use(userRateLimitFactory(-1, -1, fmt.Sprintf("invalid-user-config-%d-", time.Now().UnixNano())))
	engine.GET("/search", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/search", nil)
		req.RemoteAddr = "203.0.113.71:443"
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response.Code
	}

	require.Equal(t, http.StatusOK, request())
	require.Equal(t, http.StatusTooManyRequests, request())
}
