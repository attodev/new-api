// fork: O13 — ported from OpenWebUI controller/option_openwebui_test.go.

package controller

import (
	"io"
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

func setupOpenWebUIOptionTest(t *testing.T) *model.Token {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalRedis, originalMaster := common.RedisEnabled, common.IsMasterNode
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.IsMasterNode = false
	// model.UpdateOption writes into common.OptionMap, which is nil until
	// InitOptionMap runs at server start.
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Option{}, &model.AuditLog{}))
	model.DB = db
	// develop's commonKeyCol (used by model.GetTokenByKey) is set by
	// InitLogDB; with LOG_SQL_DSN empty it only points LOG_DB at DB.
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "svc", Password: "secret-pass", AffCode: "aff-svc", Status: common.UserStatusEnabled}).Error)
	token, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true}

	// Negative paths make model.ValidateUserToken log "record not found" via
	// common.SysLog; keep the test output quiet.
	common.LogWriterMu.Lock()
	originalWriter := gin.DefaultWriter
	gin.DefaultWriter = io.Discard
	common.LogWriterMu.Unlock()

	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = originalWriter
		common.LogWriterMu.Unlock()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.RedisEnabled, common.IsMasterNode = originalRedis, originalMaster
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
	c.Set("id", 1)
	c.Set("role", common.RoleRootUser) // develop's option audit log needs an actor role
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

// A secret padded to length with spaces is still too short.
func TestUpdateOption_OpenWebUI_SecretTrimmedLength(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)
	for _, key := range []string{"openwebui.jwt_secret", "openwebui.jwt_previous_secret"} {
		_, body := putOption(t, key, strings.Repeat(" ", 32))
		require.Contains(t, body, `"success":false`, key)
		_, body = putOption(t, key, " short"+strings.Repeat(" ", 30))
		require.Contains(t, body, `"success":false`, key)
	}
	require.Equal(t, "", system_setting.GetOpenWebUISettings().JwtSecret)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().JwtPreviousSecret)

	// With the relay on, an all-space current secret counts as clearing it.
	enableOpenWebUIRelay(t, token)
	_, body := putOption(t, "openwebui.jwt_secret", strings.Repeat(" ", 40))
	require.Contains(t, body, `"success":false`)
	require.Contains(t, body, "Disable the Open WebUI relay before clearing the service key or JWT secret")
	require.Equal(t, strings.Repeat("s", 32), system_setting.GetOpenWebUISettings().JwtSecret)
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
