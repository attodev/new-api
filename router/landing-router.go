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
// Hooks: router/web-router.go (landingStatic and landingPages in the NoRoute
// chain, WebAssets fields) and main.go (embeds). Assets live in
// web/landing/{templates,public}.

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

// landingPageActive maps each page template to its active nav item.
var landingPageActive = map[string]string{
	"index_ko": "about", "index_en": "about", "guide_ko": "guide", "guide_en": "guide",
}

// landingPagePaths maps page URLs to templates; "/" picks by Accept-Language.
var landingPagePaths = map[string]string{"/en": "index_en", "/guide": "guide_ko", "/en/guide": "guide_en"}

// landingRedirects keeps the legacy static-file URLs working.
var landingRedirects = map[string]string{
	"/index.html": "/", "/index_en.html": "/en", "/guide.html": "/guide", "/guide_en.html": "/en/guide",
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

// landingPages renders the four pages (GET/HEAD) and answers the legacy .html
// redirects from the NoRoute chain, after the plugin dispatcher, access-token
// audit and web rate limit, then aborts. Every other path falls through to the
// SPA handlers. Without embedded templates (e.g. upstream tests that build
// WebAssets by hand) it is a pass-through.
func landingPages(assets WebAssets) gin.HandlerFunc {
	if assets.LandingTemplatesFS == nil {
		return func(c *gin.Context) {}
	}
	var render func(c *gin.Context, name string) []byte
	if common.DebugEnabled {
		common.SysLog("landing dev mode (DEBUG=true): templates/public served from disk")
		render = func(c *gin.Context, name string) []byte {
			tmpl, err := template.ParseGlob(landingTemplatesDir + "/*.tmpl")
			if err != nil {
				common.SysLog("failed to parse landing templates: " + err.Error())
				return nil
			}
			return renderLandingTemplate(tmpl, name, landingPageDataFor(c, landingPageActive[name]))
		}
	} else {
		tmpl := template.Must(template.ParseFS(assets.LandingTemplatesFS, landingTemplatesDir+"/*.tmpl"))
		anonymous := make(map[string][]byte, len(landingPageActive))
		for name, active := range landingPageActive {
			body := renderLandingTemplate(tmpl, name, landingPageDataFor(nil, active))
			if body == nil {
				panic("failed to render landing template " + name)
			}
			anonymous[name] = body
		}
		render = func(c *gin.Context, name string) []byte {
			data := landingPageDataFor(c, landingPageActive[name])
			if !data.IsLoggedIn {
				return anonymous[name]
			}
			return renderLandingTemplate(tmpl, name, data)
		}
	}
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return
		}
		urlPath := c.Request.URL.Path
		if to, ok := landingRedirects[urlPath]; ok {
			c.Redirect(http.StatusMovedPermanently, to)
			c.Abort()
			return
		}
		name, ok := landingPagePaths[urlPath]
		if urlPath == "/" {
			name, ok = "index_ko", true
			if prefersEnglish(c.GetHeader("Accept-Language")) {
				name = "index_en"
			}
		}
		if !ok {
			return
		}
		c.Header("Cache-Control", landingRevalidate)
		body := render(c, name)
		if body == nil {
			c.String(http.StatusInternalServerError, "failed to render landing page")
			c.Abort()
			return
		}
		c.Data(http.StatusOK, landingHTMLContentType, body)
		c.Abort()
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
