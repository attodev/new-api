package common

import (
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// The classic theme serves the default theme's /oauth2/* pages, so the
// default bundle under /static/ must stay reachable while classic is active.
func TestThemeAwareFS_ClassicFallsBackToDefaultStaticBundle(t *testing.T) {
	original := GetTheme()
	t.Cleanup(func() { SetTheme(original) })

	defaultFS := &embedFileSystem{FileSystem: http.FS(fstest.MapFS{
		"static/js/index.js": {Data: []byte("default-js")},
		"logo.png":           {Data: []byte("default-logo")},
	})}
	classicFS := &embedFileSystem{FileSystem: http.FS(fstest.MapFS{
		"assets/index.js": {Data: []byte("classic-js")},
		"logo.png":        {Data: []byte("classic-logo")},
	})}
	fsys := NewThemeAwareFS(defaultFS, classicFS)

	SetTheme("classic")
	require.True(t, fsys.Exists("/", "/static/js/index.js"))
	f, err := fsys.Open("/static/js/index.js")
	require.NoError(t, err)
	_ = f.Close()
	require.True(t, fsys.Exists("/", "/assets/index.js"))
	require.True(t, fsys.Exists("/", "/logo.png"))
	require.False(t, fsys.Exists("/", "/missing.js"), "only /static/ falls back")

	SetTheme("default")
	require.True(t, fsys.Exists("/", "/static/js/index.js"))
	require.False(t, fsys.Exists("/", "/assets/index.js"), "default theme never reads the classic bundle")
}
