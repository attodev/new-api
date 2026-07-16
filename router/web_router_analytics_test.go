package router

import (
	"html/template"
	"os"
	"strings"
	"testing"
)

func TestRenderWebTemplateInjectsGoogleTagManager(t *testing.T) {
	t.Setenv("GOOGLE_TAG_MANAGER_ID", "GTM-TEST123")
	tmpl := template.Must(template.ParseFS(os.DirFS("../web/default/templates"), "*.tmpl"))
	page := string(renderWebTemplate(tmpl, "guide_en", "guide"))

	if !strings.Contains(page, "gtm.js?id='+i+dl") || !strings.Contains(page, "ns.html?id=GTM-TEST123") {
		t.Fatal("Google Tag Manager snippets were not rendered")
	}
}
