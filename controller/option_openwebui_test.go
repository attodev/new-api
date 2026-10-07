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

func setupOpenWebUIOptionTest(t *testing.T) *model.Token {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	// model.UpdateOption writes into common.OptionMap, which is nil until
	// InitOptionMap runs at server start.
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Option{}))
	model.DB = db
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "svc", Password: "secret-pass", AffCode: "aff-svc", Status: common.UserStatusEnabled}).Error)
	token, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		*system_setting.GetOpenWebUISettings() = original
	})
	return token
}

func putOption(t *testing.T, key string, value any) (int, string) {
	t.Helper()
	body, err := common.Marshal(map[string]any{"key": key, "value": value})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateOption(c)
	return rec.Code, rec.Body.String()
}

func TestUpdateOption_OpenWebUI_SecretTooShort(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	for _, key := range []string{"openwebui.jwt_secret", "openwebui.jwt_previous_secret"} {
		_, body := putOption(t, key, "short")
		require.Contains(t, body, `"success":false`, key)
		require.Equal(t, "", system_setting.GetOpenWebUISettings().JwtSecret)
	}
}

func TestUpdateOption_OpenWebUI_SecretAccepted(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	secret := strings.Repeat("s", 32)
	_, body := putOption(t, "openwebui.jwt_secret", secret)
	require.Contains(t, body, `"success":true`)
	require.Equal(t, secret, system_setting.GetOpenWebUISettings().JwtSecret)

	// Clearing is allowed.
	_, body = putOption(t, "openwebui.jwt_previous_secret", "")
	require.Contains(t, body, `"success":true`)
}

func TestUpdateOption_OpenWebUI_ServiceKeyMustBeEnabledToken(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)

	_, body := putOption(t, "openwebui.service_api_key", "sk-does-not-exist")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().ServiceApiKey)

	_, body = putOption(t, "openwebui.service_api_key", "  sk-"+token.Key+" ")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, token.Key, system_setting.GetOpenWebUISettings().ServiceApiKey, "normalised without sk- prefix")

	_, body = putOption(t, "openwebui.service_api_key", "")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().ServiceApiKey)
}

func TestUpdateOption_OpenWebUI_IssuerMustNotBeEmpty(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	_, body := putOption(t, "openwebui.jwt_issuer", "   ")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, "open-webui", system_setting.GetOpenWebUISettings().JwtIssuer)

	_, body = putOption(t, "openwebui.jwt_issuer", "my-issuer")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "my-issuer", system_setting.GetOpenWebUISettings().JwtIssuer)
}

func TestUpdateOption_OpenWebUI_RelayEnabledRequiresConfig(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)

	_, body := putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":false`)
	require.False(t, system_setting.GetOpenWebUISettings().RelayEnabled)

	_, _ = putOption(t, "openwebui.service_api_key", token.Key)
	_, body = putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":false`, "secret still missing")

	_, _ = putOption(t, "openwebui.jwt_secret", strings.Repeat("s", 32))
	_, body = putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":true`)
	require.True(t, system_setting.GetOpenWebUISettings().RelayEnabled)

	// Turning it off never needs validation.
	_, body = putOption(t, "openwebui.relay_enabled", false)
	require.Contains(t, body, `"success":true`)
}

// enableOpenWebUIRelay configures a valid service key + secret and turns the
// relay on, failing the test if any step is refused.
func enableOpenWebUIRelay(t *testing.T, token *model.Token) {
	t.Helper()
	for _, kv := range []struct {
		key   string
		value any
	}{
		{"openwebui.service_api_key", token.Key},
		{"openwebui.jwt_secret", strings.Repeat("s", 32)},
		{"openwebui.relay_enabled", true},
	} {
		_, body := putOption(t, kv.key, kv.value)
		require.Contains(t, body, `"success":true`, kv.key)
	}
}

func TestUpdateOption_OpenWebUI_CannotClearWhileEnabled(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)
	enableOpenWebUIRelay(t, token)

	_, body := putOption(t, "openwebui.service_api_key", "  ")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, token.Key, system_setting.GetOpenWebUISettings().ServiceApiKey)

	_, body = putOption(t, "openwebui.jwt_secret", "")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, strings.Repeat("s", 32), system_setting.GetOpenWebUISettings().JwtSecret)

	// The previous secret is optional and may always be cleared.
	_, body = putOption(t, "openwebui.jwt_previous_secret", "")
	require.Contains(t, body, `"success":true`)

	// Once disabled, both can be cleared.
	_, body = putOption(t, "openwebui.relay_enabled", false)
	require.Contains(t, body, `"success":true`)
	_, body = putOption(t, "openwebui.service_api_key", "")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().ServiceApiKey)
	_, body = putOption(t, "openwebui.jwt_secret", "")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().JwtSecret)
}

func TestUpdateOption_OpenWebUI_EnableRevalidatesServiceKey(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)
	_, body := putOption(t, "openwebui.service_api_key", token.Key)
	require.Contains(t, body, `"success":true`)
	_, body = putOption(t, "openwebui.jwt_secret", strings.Repeat("s", 32))
	require.Contains(t, body, `"success":true`)

	// The service token is deleted after the key was saved.
	require.NoError(t, model.DeleteTokenById(token.Id, token.UserId))

	_, body = putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":false`)
	require.False(t, system_setting.GetOpenWebUISettings().RelayEnabled)
}

func TestUpdateOption_OpenWebUI_LeewayRange(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	for _, bad := range []any{601, -1, "abc"} {
		_, body := putOption(t, "openwebui.jwt_leeway_seconds", bad)
		require.Contains(t, body, `"success":false`, bad)
		require.Equal(t, 60, system_setting.GetOpenWebUISettings().JwtLeewaySeconds)
	}
	for _, good := range []int{0, 600} {
		_, body := putOption(t, "openwebui.jwt_leeway_seconds", good)
		require.Contains(t, body, `"success":true`, good)
		require.Equal(t, good, system_setting.GetOpenWebUISettings().JwtLeewaySeconds)
	}
}
