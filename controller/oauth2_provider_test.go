package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOAuth2ProviderTestDB gives model.GetUserCache/model.IsAdmin — both
// called by OAuth2Token and OAuth2UserInfo — a real user row to find. Every
// test below that exercises OAuth2Token or OAuth2UserInfo needs this, not
// just the userinfo ones, since OAuth2Token itself calls GetUserCache to
// resolve the issuing user's group.
func setupOAuth2ProviderTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalUsingSQLite := common.UsingSQLite
	common.UsingSQLite = true

	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "alice",
		Email:    "alice@example.com",
		Role:     common.RoleCommonUser,
	}).Error)
	model.DB = db

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalUsingSQLite
	})
}

func setupOAuth2ProviderTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true

	originalOAuth2 := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{
		Enabled:      true,
		ClientId:     "openwebui",
		ClientSecret: "test-secret",
		RedirectURI:  "https://chat.example.com/auth/newapi/callback",
	}

	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
		*system_setting.GetOAuth2Settings() = originalOAuth2
	})
}

func TestOAuth2SessionInit_ReturnsRedirectURLForAuthenticatedUser(t *testing.T) {
	setupOAuth2ProviderTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/session-init", nil)
	ctx.Set("id", 42)

	OAuth2SessionInit(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "https://chat.example.com/auth/newapi/callback?code=")
}

func TestOAuth2SessionInit_DisabledFeatureReturns404(t *testing.T) {
	setupOAuth2ProviderTest(t)
	system_setting.GetOAuth2Settings().Enabled = false

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/session-init", nil)
	ctx.Set("id", 42)

	OAuth2SessionInit(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func postForm(t *testing.T, handler gin.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler(ctx)
	return recorder
}

func TestOAuth2Token_ValidCodeReturnsAccessToken(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	recorder := postForm(t, OAuth2Token, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"test-secret"},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"token_type":"Bearer"`)
	require.Contains(t, recorder.Body.String(), `"expires_in":86400`)
}

func TestOAuth2Token_WrongSecretReturnsInvalidClient(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	recorder := postForm(t, OAuth2Token, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"wrong-secret"},
	})

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_client")
}

func TestOAuth2Token_ReusedCodeReturnsInvalidGrant(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"test-secret"},
	}
	first := postForm(t, OAuth2Token, form)
	require.Equal(t, http.StatusOK, first.Code)

	second := postForm(t, OAuth2Token, form)
	require.Equal(t, http.StatusBadRequest, second.Code)
	require.Contains(t, second.Body.String(), "invalid_grant")
}

func TestOAuth2UserInfo_ValidTokenReturnsIdentity(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	token, err := model.IssueOAuth2Token(7, "default", "openwebui")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	ctx.Request.Header.Set("Authorization", "Bearer "+token.Key)

	OAuth2UserInfo(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"sub":"`+strconv.Itoa(7)+`"`)
}

func TestOAuth2UserInfo_InvalidTokenReturns401(t *testing.T) {
	setupOAuth2ProviderTest(t)
	// Deviation from the brief: model.ValidateUserToken falls through to
	// model.DB on a Redis cache miss (see model/token.go GetTokenByKey), so
	// this test needs a real DB too, or it panics on a nil model.DB rather
	// than returning gorm.ErrRecordNotFound.
	setupOAuth2ProviderTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	ctx.Request.Header.Set("Authorization", "Bearer sk-does-not-exist")

	OAuth2UserInfo(ctx)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
