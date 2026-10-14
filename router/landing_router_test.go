package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildAssetETags(t *testing.T) {
	files := fstest.MapFS{
		"public/landing.css": {Data: []byte("body { color: white; }")},
		"public/logo.svg":    {Data: []byte("<svg></svg>")},
	}
	etags, err := buildAssetETags(files, "public")
	require.NoError(t, err)
	require.NotEmpty(t, etags["/landing.css"])
	require.NotEmpty(t, etags["/logo.svg"])
	require.NotEqual(t, etags["/landing.css"], etags["/logo.svg"])
	changed, err := buildAssetETags(fstest.MapFS{"public/landing.css": {Data: []byte("body { color: black; }")}}, "public")
	require.NoError(t, err)
	require.NotEqual(t, etags["/landing.css"], changed["/landing.css"])
}

func newLandingTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	assets := WebAssets{LandingTemplatesFS: os.DirFS(".."), LandingPublicFS: os.DirFS("..")}
	router := gin.New()
	registerLandingRoutes(router, assets)
	router.NoRoute(landingStatic(assets), func(c *gin.Context) { c.String(http.StatusNotFound, "spa") })
	return router
}

func get(t *testing.T, router *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestLandingPagesPickLanguageAndRedirectLegacyPaths(t *testing.T) {
	router := newLandingTestEngine(t)
	ko := get(t, router, "/", map[string]string{"Accept-Language": "ko-KR,ko;q=0.9,en;q=0.5"})
	require.Equal(t, http.StatusOK, ko.Code)
	require.Contains(t, ko.Body.String(), `lang="ko"`)
	require.Equal(t, "no-cache, must-revalidate", ko.Header().Get("Cache-Control"))
	en := get(t, router, "/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	require.Contains(t, en.Body.String(), `lang="en"`)
	require.Contains(t, get(t, router, "/en/guide", nil).Body.String(), `lang="en"`)
	legacy := get(t, router, "/guide_en.html", nil)
	require.Equal(t, http.StatusMovedPermanently, legacy.Code)
	require.Equal(t, "/en/guide", legacy.Header().Get("Location"))
}

func TestLandingPagesReflectSessionHint(t *testing.T) {
	router := newLandingTestEngine(t)
	anon := get(t, router, "/en", nil)
	require.Contains(t, anon.Body.String(), `data-logged-in="false"`)
	require.Contains(t, anon.Body.String(), `href="/sign-in"`)
	signed := get(t, router, "/en", map[string]string{"Cookie": service.SessionHintCookieName + "=" + service.SessionHintCookieValue})
	require.Contains(t, signed.Body.String(), `data-logged-in="true"`)
	require.Contains(t, signed.Body.String(), `href="/dashboard"`)
	require.NotContains(t, signed.Body.String(), "profile-menu")
}

func TestLandingAnalyticsFollowsEnv(t *testing.T) {
	router := newLandingTestEngine(t)
	require.NotContains(t, get(t, router, "/en/guide", nil).Body.String(), "googletagmanager")
	t.Setenv("GOOGLE_ANALYTICS_ID", "G-TEST123")
	router = newLandingTestEngine(t)
	body := get(t, router, "/en/guide", nil).Body.String()
	require.Contains(t, body, "gtag/js?id=G-TEST123")
	require.Contains(t, body, `"G-TEST123"`)
}

func TestLandingStaticAssetsRevalidateWithETag(t *testing.T) {
	router := newLandingTestEngine(t)
	first := get(t, router, "/landing.css", nil)
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, "no-cache, must-revalidate", first.Header().Get("Cache-Control"))
	etag := first.Header().Get("ETag")
	require.True(t, strings.HasPrefix(etag, `"`), etag)
	second := get(t, router, "/landing.css", map[string]string{"If-None-Match": etag})
	require.Equal(t, http.StatusNotModified, second.Code)
	require.Equal(t, http.StatusNotFound, get(t, router, "/does-not-exist.css", nil).Code)
	require.Equal(t, "spa", get(t, router, "/does-not-exist.css", nil).Body.String())
}
