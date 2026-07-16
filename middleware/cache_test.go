package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{name: "Korean landing", path: "/", expected: revalidateCacheControl},
		{name: "English landing with query", path: "/en?source=test", expected: revalidateCacheControl},
		{name: "guide HTML", path: "/guide", expected: revalidateCacheControl},
		{name: "stable CSS filename", path: "/landing.css", expected: revalidateCacheControl},
		{name: "stable poster filename", path: "/videos/main/alrouter_ko_poster.png", expected: revalidateCacheControl},
		{name: "non-fingerprinted build asset", path: "/assets/index-main.js", expected: revalidateCacheControl},
		{name: "hyphenated build fingerprint", path: "/static/js/index-a1b2c3d4.js", expected: immutableCacheControl},
		{name: "dotted build fingerprint", path: "/static/css/index.0123456789abcdef.css", expected: immutableCacheControl},
		{name: "fingerprint outside build path", path: "/videos/main/alrouter-deadbeef.mp4", expected: revalidateCacheControl},
		{name: "fingerprint in build directory name only", path: "/static/deadbeef/index.js", expected: revalidateCacheControl},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Cache(nil))
			router.GET("/*path", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			if got := response.Header().Get("Cache-Control"); got != tt.expected {
				t.Fatalf("Cache-Control = %q, want %q", got, tt.expected)
			}
			if got := response.Header().Get("Cache-Version"); got != "" {
				t.Fatalf("Cache-Version = %q, want empty", got)
			}
		})
	}
}

func TestCacheRevalidatesStableAssetWithETag(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const etag = `"content-hash"`
	handlerCalls := 0
	router := gin.New()
	router.Use(Cache(map[string]string{"/landing.css": etag}))
	router.GET("/*path", func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusOK)
	})

	firstRequest := httptest.NewRequest(http.MethodGet, "/landing.css", nil)
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, firstRequest)

	if got := firstResponse.Header().Get("ETag"); got != etag {
		t.Fatalf("ETag = %q, want %q", got, etag)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}

	revalidatedRequest := httptest.NewRequest(http.MethodGet, "/landing.css", nil)
	revalidatedRequest.Header.Set("If-None-Match", etag)
	revalidatedResponse := httptest.NewRecorder()
	router.ServeHTTP(revalidatedResponse, revalidatedRequest)

	if revalidatedResponse.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", revalidatedResponse.Code, http.StatusNotModified)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls after revalidation = %d, want 1", handlerCalls)
	}
}
