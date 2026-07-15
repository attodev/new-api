package router

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// ThemeAssets holds the embedded frontend assets for both themes.
type ThemeAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	ClassicBuildFS   embed.FS
	ClassicIndexPage []byte
	PublicFS         embed.FS
	TemplatesFS      embed.FS
}

// webPageData is the data passed to the shared header/footer partials so
// they can highlight the active nav item and point links at the right page.
type webPageData struct {
	Active string // "about" (index pages) or "guide" (guide pages)
}

// renderWebTemplate executes a named template from tmpl with the given
// active-nav context and returns the resulting bytes, or nil on error.
func renderWebTemplate(tmpl *template.Template, name string, active string) []byte {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, webPageData{Active: active}); err != nil {
		common.SysLog("failed to render web template " + name + ": " + err.Error())
		return nil
	}
	return buf.Bytes()
}

// devWebTemplates re-parses the templates from disk on every call so edits
// show up on refresh without a rebuild. Only used when common.DebugEnabled.
func devWebTemplates() *template.Template {
	return template.Must(template.ParseGlob("web/default/templates/*.tmpl"))
}

// buildAssetETags creates content-based validators for public files that use
// stable URLs. The same content produces the same ETag across instances, while
// a deployment that changes the file immediately invalidates browser caches.
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

func SetWebRouter(router *gin.Engine, assets ThemeAssets) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/default/dist")
	classicFS := common.EmbedFolder(assets.ClassicBuildFS, "web/classic/dist")
	themeFS := common.NewThemeAwareFS(defaultFS, classicFS)

	devMode := common.DebugEnabled
	var publicAssetETags map[string]string
	if !devMode {
		var err error
		publicAssetETags, err = buildAssetETags(assets.PublicFS, "web/default/public")
		if err != nil {
			common.SysLog("failed to build public asset ETags: " + err.Error())
			publicAssetETags = nil
		}
	}

	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.Use(middleware.GlobalWebRateLimit())
	router.Use(middleware.Cache(publicAssetETags))

	if devMode {
		// Dev mode: re-render templates and re-read public/ from disk on every
		// request, so editing guide.html/index.html or the partials is visible
		// on refresh without rebuilding the binary.
		common.SysLog("web dev mode enabled (DEBUG=true): templates/public served live from disk")
		render := func(c *gin.Context, name string, active string) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", renderWebTemplate(devWebTemplates(), name, active))
		}
		router.GET("/", func(c *gin.Context) {
			acceptLang := c.GetHeader("Accept-Language")
			if strings.Contains(strings.ToLower(acceptLang), "en") && !strings.HasPrefix(strings.ToLower(acceptLang), "ko") {
				render(c, "index_en", "about")
			} else {
				render(c, "index_ko", "about")
			}
		})
		router.GET("/en", func(c *gin.Context) { render(c, "index_en", "about") })
		router.GET("/guide", func(c *gin.Context) { render(c, "guide_ko", "guide") })
		router.GET("/en/guide", func(c *gin.Context) { render(c, "guide_en", "guide") })

		// Legacy filename URLs — redirect to the clean equivalents so old
		// bookmarks/links still work but the address bar no longer shows .html
		router.GET("/index.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/") })
		router.GET("/index_en.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/en") })
		router.GET("/guide.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/guide") })
		router.GET("/guide_en.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/en/guide") })

		router.Use(static.Serve("/", static.LocalFile("web/default/public", false)))
		router.Use(static.Serve("/", themeFS))
	} else {
		publicFS := common.EmbedFolder(assets.PublicFS, "web/default/public")
		webTmpl := template.Must(template.ParseFS(assets.TemplatesFS, "web/default/templates/*.tmpl"))

		landingKo := renderWebTemplate(webTmpl, "index_ko", "about")
		landingEn := renderWebTemplate(webTmpl, "index_en", "about")
		guideKo := renderWebTemplate(webTmpl, "guide_ko", "guide")
		guideEn := renderWebTemplate(webTmpl, "guide_en", "guide")

		// Landing page routes — served before the SPA static handler
		router.GET("/", func(c *gin.Context) {
			acceptLang := c.GetHeader("Accept-Language")
			if strings.Contains(strings.ToLower(acceptLang), "en") && !strings.HasPrefix(strings.ToLower(acceptLang), "ko") {
				c.Data(http.StatusOK, "text/html; charset=utf-8", landingEn)
			} else {
				c.Data(http.StatusOK, "text/html; charset=utf-8", landingKo)
			}
		})
		router.GET("/en", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", landingEn)
		})
		router.GET("/guide", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", guideKo)
		})
		router.GET("/en/guide", func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", guideEn)
		})

		// Legacy filename URLs — redirect to the clean equivalents so old
		// bookmarks/links still work but the address bar no longer shows .html
		router.GET("/index.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/") })
		router.GET("/index_en.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/en") })
		router.GET("/guide.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/guide") })
		router.GET("/guide_en.html", func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, "/en/guide") })

		// Serve all other public/ assets (images, SVGs, etc.) — registered before SPA
		router.Use(static.Serve("/", publicFS))
		router.Use(static.Serve("/", themeFS))
	}
	router.NoRoute(func(c *gin.Context) {
		c.Set(middleware.RouteTagKey, "web")
		if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
			controller.RelayNotFound(c)
			return
		}
		c.Header("Cache-Control", "no-cache")
		// Toss Payments recommends this header on the page that opens the payment window so the
		// SDK's popups/redirects aren't blocked in some browsers. The "-allow-popups" variant keeps
		// popups this app opens (e.g. OAuth) working.
		c.Header("Cross-Origin-Opener-Policy", "same-origin-allow-popups")
		if common.GetTheme() == "classic" {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.ClassicIndexPage)
		} else {
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
		}
	})
}
