package router

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// fork: O1 — server-rendered landing/guide pages and their static assets.
// Hooks: router/web-router.go (registerLandingRoutes, landingStatic, WebAssets
// fields) and main.go (embeds). Assets live in web/landing/{templates,public}.

const (
	landingTemplatesDir    = "web/landing/templates"
	landingPublicDir       = "web/landing/public"
	landingRevalidate      = "no-cache, must-revalidate"
	landingHTMLContentType = "text/html; charset=utf-8"
)

type landingPageData struct {
	Active            string // "about" (index pages) or "guide"
	IsLoggedIn        bool   // from upstream's session hint cookie only
	GoogleAnalyticsID string // GOOGLE_ANALYTICS_ID, same env as the SPA
}

var landingPages = []struct{ name, active string }{
	{"index_ko", "about"}, {"index_en", "about"}, {"guide_ko", "guide"}, {"guide_en", "guide"},
}

func landingPageDataFor(c *gin.Context, active string) landingPageData {
	data := landingPageData{Active: active, GoogleAnalyticsID: os.Getenv("GOOGLE_ANALYTICS_ID")}
	if c != nil {
		if v, err := c.Cookie(service.SessionHintCookieName); err == nil && v == service.SessionHintCookieValue {
			data.IsLoggedIn = true
		}
	}
	return data
}

func renderLandingTemplate(tmpl *template.Template, name string, data landingPageData) []byte {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		common.SysLog("failed to render landing template " + name + ": " + err.Error())
		return nil
	}
	return buf.Bytes()
}

func prefersEnglish(acceptLanguage string) bool {
	lower := strings.ToLower(acceptLanguage)
	return strings.Contains(lower, "en") && !strings.HasPrefix(lower, "ko")
}

// buildAssetETags hashes every file under root so stable-name landing assets
// can be revalidated with If-None-Match after a deploy.
func buildAssetETags(fileSystem fs.FS, root string) (map[string]string, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(fileSystem, root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		file, err := fileSystem.Open(filePath)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		urlPath := "/" + strings.TrimPrefix(strings.TrimPrefix(filePath, root), "/")
		etags[urlPath] = `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
		return nil
	})
	return etags, err
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

// registerLandingRoutes registers the four pages and the legacy .html redirects.
// handlers (gzip, web rate limit) come from SetWebRouter so the pages share the
// SPA's protections. Without embedded templates (e.g. upstream tests that build
// WebAssets by hand) no landing routes are registered.
func registerLandingRoutes(router *gin.Engine, assets WebAssets, handlers ...gin.HandlerFunc) {
	if assets.LandingTemplatesFS == nil {
		return
	}
	group := router.Group("", handlers...)
	var render func(c *gin.Context, name, active string)
	if common.DebugEnabled {
		common.SysLog("landing dev mode (DEBUG=true): templates/public served from disk")
		render = func(c *gin.Context, name, active string) {
			tmpl, err := template.ParseGlob(landingTemplatesDir + "/*.tmpl")
			if err != nil {
				c.String(http.StatusInternalServerError, "landing templates: %v", err)
				return
			}
			c.Data(http.StatusOK, landingHTMLContentType, renderLandingTemplate(tmpl, name, landingPageDataFor(c, active)))
		}
	} else {
		tmpl := template.Must(template.ParseFS(assets.LandingTemplatesFS, landingTemplatesDir+"/*.tmpl"))
		anonymous := make(map[string][]byte, len(landingPages))
		for _, page := range landingPages {
			anonymous[page.name] = renderLandingTemplate(tmpl, page.name, landingPageDataFor(nil, page.active))
		}
		render = func(c *gin.Context, name, active string) {
			data := landingPageDataFor(c, active)
			if !data.IsLoggedIn {
				c.Data(http.StatusOK, landingHTMLContentType, anonymous[name])
				return
			}
			c.Data(http.StatusOK, landingHTMLContentType, renderLandingTemplate(tmpl, name, data))
		}
	}
	page := func(name, active string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", landingRevalidate)
			render(c, name, active)
		}
	}
	group.GET("/", func(c *gin.Context) {
		if prefersEnglish(c.GetHeader("Accept-Language")) {
			page("index_en", "about")(c)
			return
		}
		page("index_ko", "about")(c)
	})
	group.GET("/en", page("index_en", "about"))
	group.GET("/guide", page("guide_ko", "guide"))
	group.GET("/en/guide", page("guide_en", "guide"))
	for from, to := range map[string]string{"/index.html": "/", "/index_en.html": "/en", "/guide.html": "/guide", "/guide_en.html": "/en/guide"} {
		group.GET(from, func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, to) })
	}
}

// landingStatic serves web/landing/public with ETag revalidation and aborts;
// any other path falls through to the SPA handlers. Without an embedded public
// FS it is a pass-through.
func landingStatic(assets WebAssets) gin.HandlerFunc {
	if assets.LandingPublicFS == nil {
		return func(c *gin.Context) {}
	}
	var publicFS static.ServeFileSystem
	etags := map[string]string{}
	if common.DebugEnabled {
		publicFS = static.LocalFile(landingPublicDir, false)
	} else {
		publicFS = newLandingFileSystem(assets.LandingPublicFS)
		built, err := buildAssetETags(assets.LandingPublicFS, landingPublicDir)
		if err != nil {
			common.SysLog("failed to build landing asset ETags: " + err.Error())
		} else {
			etags = built
		}
	}
	fileServer := http.FileServer(publicFS)
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if strings.HasSuffix(urlPath, "/") || !publicFS.Exists("/", urlPath) {
			return
		}
		c.Header("Cache-Control", landingRevalidate)
		if etag, ok := etags[urlPath]; ok {
			c.Header("ETag", etag)
			if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && etagMatches(c.GetHeader("If-None-Match"), etag) {
				c.Status(http.StatusNotModified)
				c.Abort()
				return
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

// landingFileSystem adapts any fs.FS rooted at the repo (embed.FS in production,
// os.DirFS in tests) to gin-contrib/static. common.EmbedFolder only takes embed.FS.
type landingFileSystem struct{ http.FileSystem }

func (l landingFileSystem) Exists(prefix string, path string) bool {
	f, err := l.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func newLandingFileSystem(root fs.FS) static.ServeFileSystem {
	sub, err := fs.Sub(root, landingPublicDir)
	if err != nil {
		panic(err)
	}
	return landingFileSystem{http.FS(sub)}
}
