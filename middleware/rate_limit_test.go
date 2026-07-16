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

func TestGlobalWebRateLimitExemptsOnlyStaticReadRequests(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		rangeHeader string
		want        bool
	}{
		{name: "stylesheet", method: http.MethodGet, path: "/landing.css?v=release", want: true},
		{name: "script", method: http.MethodGet, path: "/assets/app.A1B2C3.js", want: true},
		{name: "image", method: http.MethodHead, path: "/images/hero.AVIF", want: true},
		{name: "font", method: http.MethodGet, path: "/fonts/site.woff2", want: true},
		{name: "video range", method: http.MethodGet, path: "/videos/main/demo.mp4", rangeHeader: "bytes=0-1023", want: true},
		{name: "HTML", method: http.MethodGet, path: "/en/guide", want: false},
		{name: "HTML file", method: http.MethodGet, path: "/index.html", want: false},
		{name: "dynamic JSON", method: http.MethodGet, path: "/api/pricing.json", want: false},
		{name: "range without static extension", method: http.MethodGet, path: "/download", rangeHeader: "bytes=0-1023", want: false},
		{name: "static-looking write", method: http.MethodPost, path: "/upload/image.png", want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			if testCase.rangeHeader != "" {
				request.Header.Set("Range", testCase.rangeHeader)
			}
			require.Equal(t, testCase.want, isGlobalWebRateLimitExempt(request))
		})
	}
}

func TestGlobalWebRateLimitDoesNotChargeStaticAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldEnabled := common.GlobalWebRateLimitEnable
	oldLimit := common.GlobalWebRateLimitNum
	oldDuration := common.GlobalWebRateLimitDuration
	oldRedisEnabled := common.RedisEnabled
	common.GlobalWebRateLimitEnable = true
	common.GlobalWebRateLimitNum = 1
	common.GlobalWebRateLimitDuration = 60
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.GlobalWebRateLimitEnable = oldEnabled
		common.GlobalWebRateLimitNum = oldLimit
		common.GlobalWebRateLimitDuration = oldDuration
		common.RedisEnabled = oldRedisEnabled
	})

	engine := gin.New()
	engine.Use(GlobalWebRateLimit())
	engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	seed := uint64(time.Now().UnixNano())
	remoteAddr := fmt.Sprintf("198.18.%d.%d:443", byte(seed>>8), byte(seed))
	request := func(target string, rangeHeader string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = remoteAddr
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response.Code
	}

	for rangeIndex := 0; rangeIndex < 10; rangeIndex++ {
		require.Equal(t, http.StatusOK, request("/videos/main/demo.mp4", fmt.Sprintf("bytes=%d-%d", rangeIndex*1024, (rangeIndex+1)*1024-1)))
		require.Equal(t, http.StatusOK, request("/images/poster.png", ""))
		require.Equal(t, http.StatusOK, request("/landing.css", ""))
	}

	// Static traffic did not consume the bucket, while HTML navigation still
	// receives the configured protection.
	require.Equal(t, http.StatusOK, request("/en/guide", ""))
	require.Equal(t, http.StatusTooManyRequests, request("/en/guide", ""))
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
