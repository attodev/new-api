package openwebui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func setupResolveTest(t *testing.T) *model.Token {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setupServiceTestDB(t)
	require.NoError(t, i18n.Init())

	createTestUser(t, 1, "owui-svc", "svc@example.com")
	createTestUser(t, 7, "alice", "alice@example.com")
	createTestUser(t, 8, "bob", "bob@example.com")
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 8).Update("status", common.UserStatusDisabled).Error)

	serviceToken, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled:          true,
		ServiceApiKey:         serviceToken.Key,
		JwtSecret:             testSecret,
		JwtPreviousSecret:     testOldSecret,
		JwtIssuer:             testIssuer,
		JwtLeewaySeconds:      60,
		AllowUnsignedRequests: true,
	}
	t.Cleanup(func() { *system_setting.GetOpenWebUISettings() = original })
	return serviceToken
}

func newRelayContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func userJWT(t *testing.T, secret, sub, email string) string {
	t.Helper()
	now := time.Now()
	return signHS256(t, secret, UserClaims{
		Email: email, Name: "Alice", Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: sub, Issuer: testIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(300 * time.Second)),
		},
	})
}

func TestIsServiceKey(t *testing.T) {
	svc := setupResolveTest(t)
	require.True(t, IsServiceKey(svc.Key))
	require.False(t, IsServiceKey("other"))
	require.False(t, IsServiceKey(""))

	system_setting.GetOpenWebUISettings().RelayEnabled = false
	require.False(t, IsServiceKey(svc.Key))
}

func TestResolve_SubstitutesUserChatToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})

	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, 7, token.UserId)
	require.Equal(t, TokenNameChat, token.Name)
	require.NotEmpty(t, token.Key)
	require.Equal(t, "owui-7", GetSub(c))
	require.Equal(t, "", GetTask(c))
	require.True(t, IsAttributed(c))
	require.False(t, IsUnsigned(c))
}

func TestResolve_TaskHeaderSelectsTaskToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{
		HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com"),
		HeaderTask:    "title_generation",
	})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, TokenNameTask, token.Name)
	require.Equal(t, "title_generation", GetTask(c))
}

func TestResolve_PreviousSecretAccepted(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testOldSecret, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.Nil(t, rej)
}

func TestResolve_UnsignedAllowedBillsServiceToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{
		"X-OpenWebUI-User-Email": "alice@example.com", // plaintext fallback must be ignored
	})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, svc.Id, token.Id)
	require.True(t, IsUnsigned(c))
	require.False(t, IsAttributed(c))
	require.Equal(t, "", GetSub(c))
}

func TestResolve_UnsignedRejectedWhenDisallowed(t *testing.T) {
	svc := setupResolveTest(t)
	system_setting.GetOpenWebUISettings().AllowUnsignedRequests = false
	c := newRelayContext(t, nil)
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUIAuthFailed, rej.MessageKey)
}

func TestResolve_InvalidJWTAlwaysRejected(t *testing.T) {
	svc := setupResolveTest(t)
	// allow_unsigned_requests is true here; a *present but bad* JWT must still fail.
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testWrongKey, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUIAuthFailed, rej.MessageKey)
	require.Contains(t, rej.LogReason, "jwt_invalid")
}

func TestResolve_EmptySecretRejects(t *testing.T) {
	svc := setupResolveTest(t)
	system_setting.GetOpenWebUISettings().JwtSecret = ""
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Contains(t, rej.LogReason, "misconfigured")
}

func TestResolve_EmptyIssuerSettingFallsBackToDefault(t *testing.T) {
	svc := setupResolveTest(t)
	system_setting.GetOpenWebUISettings().JwtIssuer = ""
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.Nil(t, rej, "iss=open-webui must still verify when the setting is blank")
}

func TestResolve_UnlinkedUserGets403WithoutLeakingIdentity(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-x", "nobody@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusForbidden, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUINotLinked, rej.MessageKey)
	require.Contains(t, rej.LogReason, "link_failed: no_user")
	// The user-facing text must not contain the sub or the email.
	msg := i18n.Translate(i18n.LangEn, rej.MessageKey)
	require.NotContains(t, msg, "owui-x")
	require.NotContains(t, msg, "nobody@example.com")
}

func TestResolve_DisabledUserTokenRejected(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)

	// Alice disables her openwebui token from the key list.
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("status", common.TokenStatusDisabled).Error)
	c2 := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	_, rej = Resolve(c2, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgTokenInvalid, rej.MessageKey)
}
