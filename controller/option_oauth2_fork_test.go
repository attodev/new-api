// fork: O13 — ported from OpenWebUI controller/option_oauth2_test.go.

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
	"gorm.io/gorm/logger"
)

func setupOAuth2OptionTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.AuditLog{}))
	model.DB, model.LOG_DB = db, db

	original := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
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
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser) // develop's option audit log needs an actor role
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

func TestUpdateOption_OAuth2_EnableRequiresServerAddress(t *testing.T) {
	setupOAuth2OptionTest(t)
	originalServerAddress := system_setting.ServerAddress
	t.Cleanup(func() { system_setting.ServerAddress = originalServerAddress })

	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "openwebui"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", strings.Repeat("s", 16)), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "https://chat.example.com/cb"), `"success":true`)

	// The server address is the OIDC issuer; blank (or blank after trimming
	// spaces and slashes) refuses the enable.
	for _, addr := range []string{"", "  ", "/"} {
		system_setting.ServerAddress = addr
		body := putOAuth2Option(t, "oauth2.enabled", true)
		require.Contains(t, body, `"success":false`, "server address %q", addr)
		require.Contains(t, body, "Set the server address before enabling external app access")
		require.False(t, system_setting.GetOAuth2Settings().Enabled)
	}

	system_setting.ServerAddress = "https://api.example.com"
	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":true`)
	require.True(t, system_setting.GetOAuth2Settings().Enabled)
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

// Ported from OpenWebUI controller/option_oauth2_test.go (bf236c829).
func TestUpdateOption_OAuth2_TokenFromKey(t *testing.T) {
	setupOAuth2OptionTest(t)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "openwebui"), `"success":true`)

	body := putOAuth2Option(t, "oauth2.token_from_key_client_id", "openwebui")
	require.Contains(t, body, `"success":false`, "must differ from client_id")
	require.Contains(t, body, "The API key token client ID must not be empty and must differ from the Open WebUI client ID")
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_client_id", "  "), `"success":false`)
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_client_id", " openwebui-mcp "), `"success":true`)
	require.Equal(t, "openwebui-mcp", system_setting.GetOAuth2Settings().TokenFromKeyClientId)
	require.Equal(t, "openwebui-mcp", common.OptionMap["oauth2.token_from_key_client_id"])
	body = putOAuth2Option(t, "oauth2.client_id", " openwebui-mcp ")
	require.Contains(t, body, `"success":false`, "client_id cannot take the API key token client_id")
	require.Contains(t, body, "The API key token client ID must not be empty and must differ from the Open WebUI client ID")
	require.Equal(t, "openwebui", system_setting.GetOAuth2Settings().ClientId)

	for _, bad := range []any{"30", "59", "901", "abc", "", 3.5} {
		body := putOAuth2Option(t, "oauth2.token_from_key_ttl_seconds", bad)
		require.Contains(t, body, `"success":false`, "ttl %v", bad)
		require.Contains(t, body, "The API key token lifetime must be between 60 and 900 seconds")
	}
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_ttl_seconds", " 300 "), `"success":true`)
	require.Equal(t, 300, system_setting.GetOAuth2Settings().TokenFromKeyTTLSeconds)
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_ttl_seconds", 60), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_ttl_seconds", 900), `"success":true`)

	for _, bad := range []any{"0", 601, "-1", "x"} {
		body := putOAuth2Option(t, "oauth2.token_from_key_rate_limit", bad)
		require.Contains(t, body, `"success":false`, "rate %v", bad)
		require.Contains(t, body, "The API key token rate limit must be between 1 and 600 per minute")
	}
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_rate_limit", 20), `"success":true`)
	require.Equal(t, 20, system_setting.GetOAuth2Settings().TokenFromKeyRateLimit)

	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_enabled", true), `"success":true`)
	require.True(t, system_setting.GetOAuth2Settings().TokenFromKeyEnabled)
}

// With the stored API key token client_id blank (a fresh install's default
// is used), the main client_id still cannot take the default.
func TestUpdateOption_OAuth2_ClientIdAvoidsDefaultTokenFromKeyClientId(t *testing.T) {
	setupOAuth2OptionTest(t)
	require.Equal(t, "", system_setting.GetOAuth2Settings().TokenFromKeyClientId)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", system_setting.DefaultTokenFromKeyClientId), `"success":false`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", ""), `"success":true`, "clearing the client_id stays allowed")
}

// The client_id pair is compared like the runtime guard: case and
// surrounding spaces do not make two ids different.
func TestUpdateOption_OAuth2_ClientIdPairIgnoresCase(t *testing.T) {
	setupOAuth2OptionTest(t)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "openwebui"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_client_id", "OpenWebUI"), `"success":false`)
	require.Contains(t, putOAuth2Option(t, "oauth2.token_from_key_client_id", "mcp"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", " MCP "), `"success":false`)
}
