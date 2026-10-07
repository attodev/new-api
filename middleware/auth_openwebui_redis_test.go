package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// TestTokenAuth_OpenWebUI_RedisEndToEnd runs the relay substitution with
// Redis enabled so the sub, token-key and negative caches (and the model's
// own token/user caches) all live in miniredis instead of process memory.
func TestTokenAuth_OpenWebUI_RedisEndToEnd(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
	})
	// Rebuild the HybridCaches now that common.RDB points at miniredis.
	openwebui.ResetCachesForTest()

	send := func() (int, owuiEcho, string) {
		rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", "sk-"+fx.serviceKey,
			map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
		return rec.Code, echo, rec.Body.String()
	}

	// (1) First request: auto-link by email and provision alice's chat token.
	code, first, body := send()
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, 7, first.Id)
	require.Equal(t, openwebui.TokenNameChat, first.TokenName)
	require.NotEmpty(t, first.TokenKey, "token key must be restored on the substituted token")
	token, err := model.GetTokenById(first.TokenId)
	require.NoError(t, err)
	require.Equal(t, 7, token.UserId)
	require.Equal(t, openwebui.TokenNameChat, token.Name)
	require.True(t, mr.Exists("openwebui:sub:v1:owui-7"), "sub cache must live in Redis: %v", mr.Keys())
	require.True(t, mr.Exists("openwebui:token_key:v1:7:"+openwebui.TokenNameChat), "token-key cache must live in Redis: %v", mr.Keys())

	// (2) Second request: served via the caches, same token.
	code, second, body := send()
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, first.TokenId, second.TokenId)
	require.NotEmpty(t, second.TokenKey, "token key must survive the Redis token hash round trip")

	// (3) Alice deletes her openwebui token: a fresh one is provisioned.
	require.NoError(t, model.DeleteTokenById(first.TokenId, 7))
	code, third, body := send()
	require.Equal(t, http.StatusOK, code, body)
	require.Equal(t, 7, third.Id)
	require.NotEqual(t, first.TokenId, third.TokenId)
	require.NotEmpty(t, third.TokenKey)

	// (4) Admin unlinks and alice's email changes: no link, no auto-link → 403.
	link, err := model.GetOpenWebUILinkBySub("owui-7")
	require.NoError(t, err)
	require.NoError(t, model.DeleteOpenWebUILink(link.Id))
	openwebui.InvalidateSub("owui-7")
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("email", "alice-new@example.com").Error)
	require.False(t, mr.Exists("openwebui:sub:v1:owui-7"), "InvalidateSub must delete the Redis entry")
	code, _, body = send()
	require.Equal(t, http.StatusForbidden, code, body)
	require.True(t, mr.Exists("openwebui:miss:v1:owui-7"), "the failure must be negatively cached in Redis")
}
