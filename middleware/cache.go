package middleware

import (
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	revalidateCacheControl = "no-cache, must-revalidate"
	immutableCacheControl  = "public, max-age=31536000, immutable"
)

// fingerprintedAsset matches build artifacts whose filename contains a
// content hash. Those URLs change whenever their contents change, so they are
// safe to cache for a long time. Landing-page assets use stable filenames and
// must be revalidated after every deployment instead.
var fingerprintedAsset = regexp.MustCompile(`(?i)(?:^|[.-])[a-f0-9]{8,}(?:[.-]|$)`)

func isFingerprintedBuildAsset(urlPath string) bool {
	return strings.HasPrefix(urlPath, "/static/") && fingerprintedAsset.MatchString(path.Base(urlPath))
}

func Cache(assetETags map[string]string) func(c *gin.Context) {
	return func(c *gin.Context) {
		if isFingerprintedBuildAsset(c.Request.URL.Path) {
			c.Header("Cache-Control", immutableCacheControl)
		} else {
			c.Header("Cache-Control", revalidateCacheControl)
			if etag, ok := assetETags[c.Request.URL.Path]; ok {
				c.Header("ETag", etag)
				if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && etagMatches(c.GetHeader("If-None-Match"), etag) {
					c.Status(http.StatusNotModified)
					c.Abort()
					return
				}
			}
		}
		c.Next()
	}
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}
