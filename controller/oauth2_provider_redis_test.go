package controller

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// TestOIDC_FullCodeFlow_Redis runs authorize → token → userinfo → code reuse
// with the code and access-token stores in Redis (miniredis), the way a
// multi-node deployment runs them.
func TestOIDC_FullCodeFlow_Redis(t *testing.T) {
	env := setupOIDCProviderTest(t)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
		oidc.ResetStoresForTest()
	})
	// The stores capture common.RDB when built, so rebuild them now.
	oidc.ResetStoresForTest()

	ck := env.login(t, "7")
	verifier := "redis-flow-code-verifier-string-0123456789"
	status, res := env.authorize(t, ck, "7", baseAuthorizeParams(pkceChallenge(verifier)))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, res["success"], res)
	code := redirectURL(t, res).Query().Get("code")
	require.Len(t, code, 48)
	require.NotEmpty(t, mr.Keys(), "the code must be stored in Redis")

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {verifier}}
	status, tok, _ := env.token(t, form, true)
	require.Equal(t, http.StatusOK, status, tok)
	access := tok["access_token"].(string)
	require.NotEmpty(t, tok["id_token"])
	hasAccessKey := false
	for _, k := range mr.Keys() {
		hasAccessKey = hasAccessKey || strings.Contains(k, "oauth2:access")
	}
	require.True(t, hasAccessKey, "the access token must be stored in Redis: %v", mr.Keys())

	status, info, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status, info)
	require.Equal(t, "7", info["sub"])
	require.Equal(t, "alice@example.com", info["email"])

	status, reuse, _ := env.token(t, form, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", reuse["error"])
}
