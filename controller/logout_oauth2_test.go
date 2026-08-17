package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogout_RevokesOAuth2Token(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Set up test database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
	})

	// Set up test Redis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
	})

	token, err := model.IssueOAuth2Token(55, "default", "openwebui")
	require.NoError(t, err)

	engine := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	engine.Use(sessions.Sessions("session", store))
	engine.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 55)
		require.NoError(t, session.Save())
		Logout(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)

	_, err = model.GetTokenByKey(token.Key, false)
	require.Error(t, err, "OAuth2 token must be invalidated after logout")
}
