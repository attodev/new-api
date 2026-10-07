package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

const (
	owuiTestSecret = "0123456789abcdef0123456789abcdef"
	owuiTestIssuer = "open-webui"
)

type owuiFixture struct {
	serviceKey string
}

type owuiEcho struct {
	Id         int    `json:"id"`
	TokenId    int    `json:"token_id"`
	TokenName  string `json:"token_name"`
	Sub        string `json:"sub"`
	Task       string `json:"task"`
	Attributed bool   `json:"attributed"`
	Unsigned   bool   `json:"unsigned"`
}

func setupOpenWebUIMiddlewareTest(t *testing.T) (*gin.Engine, owuiFixture) {
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

	mkUser := func(id int, name, email string, status int) {
		// users.aff_code has a unique index, so every fixture user needs its own.
		require.NoError(t, db.Create(&model.User{
			Id: id, Username: name, Password: "secret-pass", Email: email, AffCode: "aff-" + name,
			Status: status, Role: common.RoleCommonUser, Group: "default",
		}).Error)
	}
	mkUser(1, "owui-svc", "svc@example.com", common.UserStatusEnabled)
	mkUser(7, "alice", "alice@example.com", common.UserStatusEnabled)
	mkUser(8, "bob", "bob@example.com", common.UserStatusDisabled)

	serviceToken, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	originalSettings := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled:          true,
		ServiceApiKey:         serviceToken.Key,
		JwtSecret:             owuiTestSecret,
		JwtIssuer:             owuiTestIssuer,
		JwtLeewaySeconds:      60,
		AllowUnsignedRequests: true,
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		*system_setting.GetOpenWebUISettings() = originalSettings
		openwebui.ResetCachesForTest()
	})

	engine := gin.New()
	engine.Use(TokenAuth())
	echo := func(c *gin.Context) {
		c.JSON(http.StatusOK, owuiEcho{
			Id:         c.GetInt("id"),
			TokenId:    c.GetInt("token_id"),
			TokenName:  c.GetString("token_name"),
			Sub:        openwebui.GetSub(c),
			Task:       openwebui.GetTask(c),
			Attributed: openwebui.IsAttributed(c),
			Unsigned:   openwebui.IsUnsigned(c),
		})
	}
	engine.POST("/v1/chat/completions", echo)
	engine.GET("/v1/models", echo)
	return engine, owuiFixture{serviceKey: serviceToken.Key}
}

func owuiJWT(t *testing.T, sub, email string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": sub, "email": email, "name": "Test", "role": "user",
		"iss": owuiTestIssuer, "iat": now.Unix(), "exp": now.Add(300 * time.Second).Unix(),
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(owuiTestSecret))
	require.NoError(t, err)
	return raw
}

func owuiDo(t *testing.T, engine *gin.Engine, method, path, bearer string, headers map[string]string) (*httptest.ResponseRecorder, owuiEcho) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.RemoteAddr = "10.0.0.5:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var echo owuiEcho
	if rec.Code == http.StatusOK {
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &echo))
	}
	return rec, echo
}

func TestTokenAuth_OpenWebUI_SubstitutesUserToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", "sk-"+fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, openwebui.TokenNameChat, echo.TokenName)
	require.Equal(t, "owui-7", echo.Sub)
	require.True(t, echo.Attributed)

	tok, err := model.GetTokenById(echo.TokenId)
	require.NoError(t, err)
	require.Equal(t, 7, tok.UserId)
}

func TestTokenAuth_OpenWebUI_TaskHeaderUsesTaskToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey, map[string]string{
		openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com"),
		openwebui.HeaderTask:    "title_generation",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, openwebui.TokenNameTask, echo.TokenName)
	require.Equal(t, "title_generation", echo.Task)
}

func TestTokenAuth_OpenWebUI_UnsignedBillsServiceToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{"X-OpenWebUI-User-Email": "alice@example.com"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, echo.Id, "billed to the service account, plaintext header ignored")
	require.Equal(t, "openwebui-service", echo.TokenName)
	require.True(t, echo.Unsigned)
	require.False(t, echo.Attributed)
}

func TestTokenAuth_OpenWebUI_UnsignedRejectedWhenDisallowed(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	system_setting.GetOpenWebUISettings().AllowUnsignedRequests = false
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"new_api_error"`)
}

func TestTokenAuth_OpenWebUI_BadJWTIs401EvenWhenUnsignedAllowed(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: "garbage.jwt.value"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NotContains(t, rec.Body.String(), "garbage")
}

func TestTokenAuth_OpenWebUI_UnlinkedIs403WithoutIdentity(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-zz", "nobody@example.com")})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "owui-zz")
	require.NotContains(t, rec.Body.String(), "nobody@example.com")
}

func TestTokenAuth_OpenWebUI_DisabledUserIs403(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-8", "bob@example.com")})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTokenAuth_OpenWebUI_ServiceTokenIPLimitAppliesBeforeJWT(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	svc, err := model.GetTokenByKey(fx.serviceKey, true)
	require.NoError(t, err)
	allow := "192.168.1.0/24"
	svc.AllowIps = &allow
	require.NoError(t, model.DB.Save(svc).Error)

	// Even an unparseable JWT must not be the reason: IP rejection comes first with 403.
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: "garbage"})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "allowed access list")
}

func TestTokenAuth_OpenWebUI_RelayDisabledFallsBackToNormalAuth(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	system_setting.GetOpenWebUISettings().RelayEnabled = false
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, echo.Id, "JWT ignored: plain token auth")
	require.Equal(t, "", echo.Sub)
}

func TestTokenAuth_OpenWebUI_OrdinaryKeysUnaffected(t *testing.T) {
	engine, _ := setupOpenWebUIMiddlewareTest(t)
	own, err := model.CreateOpenWebUIToken(7, "my-key")
	require.NoError(t, err)
	rec, echo := owuiDo(t, engine, http.MethodGet, "/v1/models", "sk-"+own.Key,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-8", "bob@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, "my-key", echo.TokenName)
	require.False(t, echo.Attributed)
}

func TestTokenAuth_OpenWebUI_ModelsEndpointRunsAsUser(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodGet, "/v1/models", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, openwebui.TokenNameChat, echo.TokenName)
}
