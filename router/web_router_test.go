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
