package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOAuth2OptionTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db

	original := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		*system_setting.GetOAuth2Settings() = original
	})
}

func putOAuth2Option(t *testing.T, key string, value any) string {
	t.Helper()
	body, err := common.Marshal(map[string]any{"key": key, "value": value})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateOption(c)
	return rec.Body.String()
}

func TestUpdateOption_OAuth2_EnableRequiresConfig(t *testing.T) {
	setupOAuth2OptionTest(t)

	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":false`)
	require.False(t, system_setting.GetOAuth2Settings().Enabled)

	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "openwebui"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", "short"), `"success":false`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", strings.Repeat("s", 16)), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":false`, "redirect_uri still missing")

	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "chat.example.com/cb"), `"success":false`, "not absolute")
	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "https://chat.example.com/oauth/oidc/login/callback"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":true`)
	require.True(t, system_setting.GetOAuth2Settings().Enabled)

	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", false), `"success":true`)
}

func TestUpdateOption_OAuth2_OptionalURLs(t *testing.T) {
	setupOAuth2OptionTest(t)
	for _, key := range []string{"oauth2.app_url", "oauth2.post_logout_redirect_uri"} {
		require.Contains(t, putOAuth2Option(t, key, ""), `"success":true`, key)
		require.Contains(t, putOAuth2Option(t, key, "not a url"), `"success":false`, key)
		require.Contains(t, putOAuth2Option(t, key, "https://chat.example.com"), `"success":true`, key)
	}
	require.Equal(t, "https://chat.example.com", system_setting.GetOAuth2Settings().AppUrl)
}

func TestUpdateOption_OAuth2_TrimsWhitespace(t *testing.T) {
	setupOAuth2OptionTest(t)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "  openwebui \t"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", " "+strings.Repeat("s", 16)+" "), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "  https://chat.example.com/cb  "), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.app_url", " https://chat.example.com "), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.post_logout_redirect_uri", "\thttps://chat.example.com/bye\n"), `"success":true`)

	s := system_setting.GetOAuth2Settings()
	require.Equal(t, "openwebui", s.ClientId)
	require.Equal(t, strings.Repeat("s", 16), s.ClientSecret)
	require.Equal(t, "https://chat.example.com/cb", s.RedirectURI)
	require.Equal(t, "https://chat.example.com", s.AppUrl)
	require.Equal(t, "https://chat.example.com/bye", s.PostLogoutRedirectURI)
	require.Equal(t, "https://chat.example.com/cb", common.OptionMap["oauth2.redirect_uri"])
}
