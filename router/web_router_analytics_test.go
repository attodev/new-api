package router

import (
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestRenderWebTemplateIncludesGoogleAnalytics(t *testing.T) {
	tmpl := template.Must(template.ParseFS(os.DirFS("../web/default/templates"), "*.tmpl"))
	page := string(renderWebTemplate(tmpl, "guide_en", "guide"))

	if !strings.Contains(page, "gtag/js?id=G-Z73Y7213LF") || !strings.Contains(page, "gtag('config', 'G-Z73Y7213LF')") {
		t.Fatal("Google Analytics tag was not rendered")
	}
}
