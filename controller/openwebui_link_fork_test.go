// fork: O13 — ported from OpenWebUI controller/openwebui_link_test.go; link and unlink now need develop's admin step-up proof.

package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type openWebUILinkTestEnv struct {
	engine   *gin.Engine
	identity service.AuthIdentity // the operator's login session (admin, id 50)
}

func setupOpenWebUILinkControllerTest(t *testing.T) *openWebUILinkTestEnv {
	t.Helper()
	db := setupOIDCTestDB(t, &model.Token{}, &model.AuthFlow{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.PasskeyCredential{}, &model.UserOAuthBinding{})
	require.NoError(t, model.MigrateForkOpenWebUITables(db))
	openwebui.ResetCachesForTest()

	password, err := common.Password2Hash("operator-password")
	require.NoError(t, err)
	for _, u := range []model.User{
		// users.aff_code has a unique index, so every fixture user needs its own.
		{Id: 7, Username: "alice", Password: "secret-pass", Email: "alice@example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AuthVersion: 1},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "bob@example.com", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, AuthVersion: 1},
		{Id: 50, Username: "operator", Password: password, AffCode: "aff-operator", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, AuthVersion: 1},
		{Id: 100, Username: "root", Password: "secret-pass", AffCode: "aff-root", Status: common.UserStatusEnabled, Role: common.RoleRootUser, AuthVersion: 1},
	} {
		require.NoError(t, db.Create(&u).Error)
		require.NoError(t, model.PublishUserAuthCache(u.Id))
	}
	bundle, err := service.CreateLoginSession(50, "password", "127.0.0.1", "openwebui-link-test")
	require.NoError(t, err)
	identity, err := service.ParseAccessToken(bundle.AccessToken)
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled: true, ServiceApiKey: "svc", JwtSecret: strings.Repeat("s", 32),
		JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true,
	}
	t.Cleanup(func() {
		*system_setting.GetOpenWebUISettings() = original
		openwebui.ResetCachesForTest()
	})

	engine := gin.New()
	// Stand-in for AdminAuth on a dashboard session (admin, role 10).
	engine.Use(func(c *gin.Context) {
		c.Set("id", identity.UserID)
		c.Set("username", "operator")
		c.Set("role", common.RoleAdminUser)
		c.Set("session_id", identity.SessionID)
		c.Set("auth_version", identity.UserAuthVersion)
		c.Set("session_version", identity.SessionVersion)
		c.Next()
	})
	engine.GET("/api/openwebui/relay-status", GetOpenWebUIRelayStatus)
	engine.GET("/api/openwebui/links", GetOpenWebUILink)
	engine.POST("/api/openwebui/links", CreateOpenWebUILink)
	engine.DELETE("/api/openwebui/links/:id", DeleteOpenWebUILink)
	return &openWebUILinkTestEnv{engine: engine, identity: identity}
}

// linkProof is the step-up proof the admin UI obtains before linking userId.
func (e *openWebUILinkTestEnv) linkProof(t *testing.T, userId int) string {
	t.Helper()
	return issueSecurityEnrollmentProof(t, e.identity, service.VerificationOperation{
		Scope: service.VerificationScopeAdminUserUpdate, Context: []byte(fmt.Sprintf(`{"user_id":%d}`, userId)),
	}, service.VerificationMethodPassword)
}

// unlinkProof is the step-up proof for removing userId's Open WebUI link.
func (e *openWebUILinkTestEnv) unlinkProof(t *testing.T, userId int) string {
	t.Helper()
	return issueSecurityEnrollmentProof(t, e.identity, service.VerificationOperation{
		Scope: service.VerificationScopeAdminUserBindingClear, Context: []byte(fmt.Sprintf(`{"user_id":%d,"binding_type":"openwebui"}`, userId)),
	}, service.VerificationMethodPassword)
}

func (e *openWebUILinkTestEnv) call(t *testing.T, method, path string, body any, proof string) (int, map[string]any) {
	t.Helper()
	reader := strings.NewReader("")
	if body != nil {
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if proof != "" {
		req.Header.Set("X-Security-Proof", proof)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var parsed map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &parsed), rec.Body.String())
	return rec.Code, parsed
}

func (e *openWebUILinkTestEnv) link(t *testing.T, userId int, sub string) map[string]any {
	t.Helper()
	_, res := e.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": userId, "sub": sub}, e.linkProof(t, userId))
	return res
}

func countOpenWebUILinks(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, model.DB.Model(&model.OpenWebUILink{}).Count(&n).Error)
	return n
}

func TestOpenWebUIRelayStatus(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)
	code, res := env.call(t, http.MethodGet, "/api/openwebui/relay-status", nil, "")
	require.Equal(t, http.StatusOK, code)
	data := res["data"].(map[string]any)
	require.Equal(t, true, data["relay_enabled"])
	require.Equal(t, true, data["service_key_configured"])
	require.Equal(t, true, data["jwt_secret_configured"])
	require.Equal(t, false, data["jwt_previous_secret_configured"])
	require.Equal(t, "open-webui", data["jwt_issuer"])
	require.Equal(t, float64(60), data["jwt_leeway_seconds"])
	require.Equal(t, true, data["allow_unsigned_requests"])
	for _, secret := range []string{"service_api_key", "jwt_secret", "jwt_previous_secret"} {
		_, leaks := data[secret]
		require.False(t, leaks, secret)
	}
}

func TestOpenWebUILink_CreateGetDelete(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)

	code, res := env.call(t, http.MethodGet, "/api/openwebui/links?user_id=7", nil, "")
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, res["success"])
	require.Nil(t, res["data"])

	res = env.link(t, 7, " owui-7 ")
	require.Equal(t, true, res["success"], res["message"])
	data := res["data"].(map[string]any)
	require.Equal(t, "owui-7", data["sub"])
	require.Equal(t, "admin", data["linked_by"])
	require.Equal(t, "alice@example.com", data["email"])
	tokens := data["tokens"].([]any)
	require.Len(t, tokens, 2)
	require.Equal(t, false, tokens[0].(map[string]any)["exists"])

	_, res = env.call(t, http.MethodGet, "/api/openwebui/links?sub=owui-7", nil, "")
	require.Equal(t, float64(7), res["data"].(map[string]any)["user_id"])

	// Provisioned token shows up in the status list.
	_, err := model.CreateOpenWebUIToken(7, openwebui.TokenNameChat)
	require.NoError(t, err)
	_, res = env.call(t, http.MethodGet, "/api/openwebui/links?user_id=7", nil, "")
	tokens = res["data"].(map[string]any)["tokens"].([]any)
	require.Equal(t, true, tokens[0].(map[string]any)["exists"])
	require.Equal(t, float64(common.TokenStatusEnabled), tokens[0].(map[string]any)["status"])

	id := int(data["id"].(float64))
	_, res = env.call(t, http.MethodDelete, "/api/openwebui/links/"+strconv.Itoa(id), nil, env.unlinkProof(t, 7))
	require.Equal(t, true, res["success"], res)
	_, res = env.call(t, http.MethodGet, "/api/openwebui/links?user_id=7", nil, "")
	require.Nil(t, res["data"])

	// Tokens survive an unlink.
	_, err = model.FindOldestUserTokenByName(7, openwebui.TokenNameChat)
	require.NoError(t, err)

	_, res = env.call(t, http.MethodDelete, "/api/openwebui/links/"+strconv.Itoa(id), nil, env.unlinkProof(t, 7))
	require.Equal(t, false, res["success"])
	require.Equal(t, "Open WebUI link not found", res["message"])

	// Both mutations are in the operation audit log with the sub and user.
	for _, action := range []string{"openwebui.link.create", "openwebui.link.delete"} {
		var audit model.AuditLog
		require.NoError(t, model.LOG_DB.Where("action = ?", action).Last(&audit).Error, action)
		require.NotNil(t, audit.Other.Op, action)
		params, err := common.Marshal(audit.Other.Op.Params)
		require.NoError(t, err)
		require.JSONEq(t, `{"link_id":1,"sub":"owui-7","user_id":7,"target_user_id":7,"verification_method":"password"}`, string(params), action)
	}
}

// Linking decides whose wallet an Open WebUI account bills, so it needs the
// same step-up proof as other sensitive admin user changes; unlinking needs
// the binding-clear proof. Neither proof is accepted for another user.
func TestOpenWebUILink_RequiresStepUpProof(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)

	code, res := env.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"}, "")
	require.Equal(t, http.StatusForbidden, code)
	require.Equal(t, "SECURITY_PROOF_REQUIRED", res["code"])
	require.Zero(t, countOpenWebUILinks(t))

	code, res = env.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"}, env.linkProof(t, 8))
	require.Equal(t, http.StatusForbidden, code)
	require.Equal(t, "SECURITY_PROOF_CONTEXT_MISMATCH", res["code"])
	require.Zero(t, countOpenWebUILinks(t))

	res = env.link(t, 7, "owui-7")
	require.Equal(t, true, res["success"], res)
	path := "/api/openwebui/links/" + strconv.Itoa(int(res["data"].(map[string]any)["id"].(float64)))

	for name, proof := range map[string]string{
		"no proof":     "",
		"link proof":   env.linkProof(t, 7),
		"other user":   env.unlinkProof(t, 8),
		"garbage":      "not-a-proof",
		"wrong action": issueSecurityEnrollmentProof(t, env.identity, service.VerificationOperation{Scope: service.VerificationScopeAdminUserBindingClear, Context: []byte(`{"user_id":7,"binding_type":"github"}`)}, service.VerificationMethodPassword),
	} {
		code, res = env.call(t, http.MethodDelete, path, nil, proof)
		require.Equal(t, http.StatusForbidden, code, name)
		require.Equal(t, false, res["success"], name)
		require.EqualValues(t, 1, countOpenWebUILinks(t), name)
	}

	// A proof is single use.
	proof := env.linkProof(t, 8)
	res = env.link(t, 8, "owui-8")
	require.Equal(t, true, res["success"], res)
	_, res = env.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 8, "sub": "owui-8b"}, proof)
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_Conflicts(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)
	res := env.link(t, 7, "owui-7")
	require.Equal(t, true, res["success"])

	// Conflicts are reported before the proof is consumed.
	proof := env.linkProof(t, 8)
	_, res = env.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 8, "sub": "owui-7"}, proof)
	require.Equal(t, false, res["success"])
	require.Equal(t, "This Open WebUI account or new-api user is already linked", res["message"])
	require.Equal(t, "This Open WebUI account or new-api user is already linked", res["message_key"])

	res = env.link(t, 7, "owui-other")
	require.Equal(t, false, res["success"])
	require.Equal(t, "This Open WebUI account or new-api user is already linked", res["message"])

	_, res = env.call(t, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 8, "sub": "owui-8"}, proof)
	require.Equal(t, true, res["success"], "the proof survived the conflict: %v", res)
}

func TestOpenWebUILink_Validation(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)
	for _, tc := range []struct {
		body    any
		message string
	}{
		{map[string]any{"user_id": 7, "sub": "   "}, "Open WebUI user id (sub) is required"},
		{map[string]any{"user_id": 0, "sub": "owui-x"}, "Invalid parameters"},
		{map[string]any{"user_id": 999, "sub": "owui-x"}, "User does not exist"},
		{"not an object", "Invalid parameters"},
		// An admin may not touch a root user's link.
		{map[string]any{"user_id": 100, "sub": "owui-root"}, "No permission to update users of same or higher permission level"},
	} {
		_, res := env.call(t, http.MethodPost, "/api/openwebui/links", tc.body, "")
		require.Equal(t, false, res["success"], tc.body)
		require.Equal(t, tc.message, res["message"], tc.body)
	}
	require.Zero(t, countOpenWebUILinks(t))

	_, res := env.call(t, http.MethodGet, "/api/openwebui/links", nil, "")
	require.Equal(t, "Invalid parameters", res["message"])
	_, res = env.call(t, http.MethodGet, "/api/openwebui/links?user_id=x", nil, "")
	require.Equal(t, "Invalid parameters", res["message"])
	_, res = env.call(t, http.MethodDelete, "/api/openwebui/links/x", nil, "")
	require.Equal(t, "Invalid parameters", res["message"])
}

func jwtRegistered(sub string) jwt.RegisteredClaims { return jwt.RegisteredClaims{Subject: sub} }

func TestOpenWebUILink_InvalidatesResolveCache(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)

	// Create: a sub that was remembered as unknown resolves right after the
	// admin links it (no email, so it can never auto-link).
	_, err := openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.Error(t, err)
	res := env.link(t, 7, "owui-7")
	require.Equal(t, true, res["success"], res)
	userId, err := openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.NoError(t, err, "create must drop the cached miss")
	require.Equal(t, 7, userId)

	// Delete: the warm sub -> user entry is dropped.
	id := int(res["data"].(map[string]any)["id"].(float64))
	_, res = env.call(t, http.MethodDelete, "/api/openwebui/links/"+strconv.Itoa(id), nil, env.unlinkProof(t, 7))
	require.Equal(t, true, res["success"])
	_, err = openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.Error(t, err, "delete must drop the cached link")
}

func TestOpenWebUILink_GetEnforcesTargetRole(t *testing.T) {
	env := setupOpenWebUILinkControllerTest(t)
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-root", UserId: 100, LinkedBy: model.OpenWebUILinkedByAdmin}))

	for _, path := range []string{"/api/openwebui/links?user_id=100", "/api/openwebui/links?sub=owui-root"} {
		_, res := env.call(t, http.MethodGet, path, nil, "")
		require.Equal(t, false, res["success"], path)
		require.Equal(t, "No permission to access users of same or higher level", res["message"], path)
		require.Nil(t, res["data"], path)
	}
	link, err := model.GetOpenWebUILinkBySub("owui-root")
	require.NoError(t, err)
	_, res := env.call(t, http.MethodDelete, "/api/openwebui/links/"+strconv.Itoa(link.Id), nil, env.unlinkProof(t, 100))
	require.Equal(t, "No permission to update users of same or higher permission level", res["message"])

	// A dangling link (user deleted) is still visible and can be removed.
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-gone", UserId: 999, LinkedBy: model.OpenWebUILinkedByAdmin}))
	_, res = env.call(t, http.MethodGet, "/api/openwebui/links?sub=owui-gone", nil, "")
	require.Equal(t, true, res["success"], res["message"])
	id := int(res["data"].(map[string]any)["id"].(float64))
	_, res = env.call(t, http.MethodDelete, "/api/openwebui/links/"+strconv.Itoa(id), nil, env.unlinkProof(t, 999))
	require.Equal(t, true, res["success"], res)
}
