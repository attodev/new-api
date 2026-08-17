package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOAuth2Router_RegistersExpectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetOAuth2Router(engine)

	paths := make(map[string]string)
	for _, r := range engine.Routes() {
		paths[r.Method+" "+r.Path] = r.Handler
	}

	require.Contains(t, paths, "POST /oauth2/session-init")
	require.Contains(t, paths, "POST /oauth2/token")
	require.Contains(t, paths, "GET /oauth2/userinfo")
}
