package router

import (
	"testing"
	"testing/fstest"
)

func TestBuildAssetETags(t *testing.T) {
	files := fstest.MapFS{
		"public/landing.css": {Data: []byte("body { color: white; }")},
		"public/logo.svg":    {Data: []byte("<svg></svg>")},
	}

	etags, err := buildAssetETags(files, "public")
	if err != nil {
		t.Fatalf("buildAssetETags() error = %v", err)
	}

	if etags["/landing.css"] == "" {
		t.Fatal("missing ETag for /landing.css")
	}
	if etags["/logo.svg"] == "" {
		t.Fatal("missing ETag for /logo.svg")
	}
	if etags["/landing.css"] == etags["/logo.svg"] {
		t.Fatal("different contents produced identical ETags")
	}

	changedFiles := fstest.MapFS{
		"public/landing.css": {Data: []byte("body { color: black; }")},
	}
	changedETags, err := buildAssetETags(changedFiles, "public")
	if err != nil {
		t.Fatalf("buildAssetETags() for changed content error = %v", err)
	}
	if changedETags["/landing.css"] == etags["/landing.css"] {
		t.Fatal("changed content did not invalidate the ETag")
	}
}

func TestSPAIndexPage_OIDCPagesAlwaysUseDefaultTheme(t *testing.T) {
	assets := ThemeAssets{DefaultIndexPage: []byte("default"), ClassicIndexPage: []byte("classic")}
	cases := []struct {
		path, theme, want string
	}{
		{"/oauth2/authorize", "classic", "default"},
		{"/oauth2/logout", "classic", "default"},
		{"/oauth2/authorize", "default", "default"},
		{"/oauth2/authorize/x", "classic", "classic"},
		{"/console", "classic", "classic"},
		{"/console", "default", "default"},
	}
	for _, tc := range cases {
		if got := string(spaIndexPage(tc.path, tc.theme, assets)); got != tc.want {
			t.Errorf("spaIndexPage(%q, %q) = %q, want %q", tc.path, tc.theme, got, tc.want)
		}
	}
}
