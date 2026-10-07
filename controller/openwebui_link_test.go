package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOpenWebUILinkControllerTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	openwebui.ResetCachesForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.OpenWebUILink{}))
	model.DB = db
	for _, u := range []model.User{
		// users.aff_code has a unique index, so every fixture user needs its own.
		{Id: 7, Username: "alice", Password: "secret-pass", Email: "alice@example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "bob@example.com", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{Id: 100, Username: "root", Password: "secret-pass", AffCode: "aff-root", Status: common.UserStatusEnabled, Role: common.RoleRootUser},
	} {
		require.NoError(t, db.Create(&u).Error)
	}

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled: true, ServiceApiKey: "svc", JwtSecret: strings.Repeat("s", 32),
		JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true,
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		*system_setting.GetOpenWebUISettings() = original
		openwebui.ResetCachesForTest()
	})

	engine := gin.New()
	// Simulate AdminAuth having run as an admin (role 10).
	engine.Use(func(c *gin.Context) {
		c.Set("id", 50)
		c.Set("role", common.RoleAdminUser)
		c.Next()
	})
	engine.GET("/api/openwebui/relay-status", GetOpenWebUIRelayStatus)
	engine.GET("/api/openwebui/links", GetOpenWebUILink)
	engine.POST("/api/openwebui/links", CreateOpenWebUILink)
	engine.DELETE("/api/openwebui/links/:id", DeleteOpenWebUILink)
	return engine
}

func linkCall(t *testing.T, engine *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var parsed map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &parsed), rec.Body.String())
	return rec.Code, parsed
}

func TestOpenWebUIRelayStatus(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	code, res := linkCall(t, engine, http.MethodGet, "/api/openwebui/relay-status", nil)
	require.Equal(t, http.StatusOK, code)
	data := res["data"].(map[string]any)
	require.Equal(t, true, data["relay_enabled"])
	require.Equal(t, true, data["service_key_configured"])
	require.Equal(t, true, data["jwt_secret_configured"])
	require.Equal(t, false, data["jwt_previous_secret_configured"])
	_, leaks := data["service_api_key"]
	require.False(t, leaks)
}

func TestOpenWebUILink_CreateGetDelete(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)

	code, res := linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, res["success"])
	require.Nil(t, res["data"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"], res["message"])
	data := res["data"].(map[string]any)
	require.Equal(t, "owui-7", data["sub"])
	require.Equal(t, "admin", data["linked_by"])
	tokens := data["tokens"].([]any)
	require.Len(t, tokens, 2)
	require.Equal(t, false, tokens[0].(map[string]any)["exists"])

	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?sub=owui-7", nil)
	require.Equal(t, float64(7), res["data"].(map[string]any)["user_id"])

	// Provisioned token shows up in the status list.
	_, err := model.CreateOpenWebUIToken(7, openwebui.TokenNameChat)
	require.NoError(t, err)
	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	tokens = res["data"].(map[string]any)["tokens"].([]any)
	require.Equal(t, true, tokens[0].(map[string]any)["exists"])
	require.Equal(t, float64(common.TokenStatusEnabled), tokens[0].(map[string]any)["status"])

	id := int(data["id"].(float64))
	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, true, res["success"])
	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	require.Nil(t, res["data"])

	// Tokens survive an unlink.
	_, err = model.FindOldestUserTokenByName(7, openwebui.TokenNameChat)
	require.NoError(t, err)

	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_Conflicts(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 8, "sub": "owui-7"})
	require.Equal(t, false, res["success"])
	require.NotEmpty(t, res["message"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-other"})
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_Validation(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "   "})
	require.Equal(t, false, res["success"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 999, "sub": "owui-x"})
	require.Equal(t, false, res["success"])

	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links", nil)
	require.Equal(t, false, res["success"])

	// An admin may not touch a root user's link.
	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 100, "sub": "owui-root"})
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_InvalidatesResolveCache(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"])

	// Warm the resolver cache, then unlink via the API; the next resolve must miss.
	userId, err := openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.NoError(t, err)
	require.Equal(t, 7, userId)

	id := int(res["data"].(map[string]any)["id"].(float64))
	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, true, res["success"])

	_, err = openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.Error(t, err, "cache must have been invalidated")
}

func itoa(i int) string { return strconv.Itoa(i) }

func jwtRegistered(sub string) jwt.RegisteredClaims { return jwt.RegisteredClaims{Subject: sub} }

func TestOpenWebUILink_GetEnforcesTargetRole(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-root", UserId: 100, LinkedBy: model.OpenWebUILinkedByAdmin}))

	for _, path := range []string{"/api/openwebui/links?user_id=100", "/api/openwebui/links?sub=owui-root"} {
		_, res := linkCall(t, engine, http.MethodGet, path, nil)
		require.Equal(t, false, res["success"], path)
		require.Nil(t, res["data"], path)
	}

	// A dangling link (user deleted) is still visible so it can be removed.
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-gone", UserId: 999, LinkedBy: model.OpenWebUILinkedByAdmin}))
	_, res := linkCall(t, engine, http.MethodGet, "/api/openwebui/links?sub=owui-gone", nil)
	require.Equal(t, true, res["success"], res["message"])
}
