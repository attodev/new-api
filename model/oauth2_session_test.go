package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupOAuth2TestRedis(t *testing.T) {
	t.Helper()
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
}

func TestStoreAndConsumeAuthCode(t *testing.T) {
	setupOAuth2TestRedis(t)

	code, err := StoreAuthCode(42)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	userId, err := ConsumeAuthCode(code)
	require.NoError(t, err)
	require.Equal(t, 42, userId)
}

func TestConsumeAuthCode_SingleUseOnly(t *testing.T) {
	setupOAuth2TestRedis(t)

	code, err := StoreAuthCode(7)
	require.NoError(t, err)

	_, err = ConsumeAuthCode(code)
	require.NoError(t, err)

	_, err = ConsumeAuthCode(code)
	require.ErrorIs(t, err, ErrOAuth2CodeInvalid, "a second consume of the same code must fail")
}

func TestConsumeAuthCode_UnknownCodeReturnsInvalid(t *testing.T) {
	setupOAuth2TestRedis(t)

	_, err := ConsumeAuthCode("does-not-exist")
	require.ErrorIs(t, err, ErrOAuth2CodeInvalid)
}

func TestIssueOAuth2Token_IsValidatableViaExistingLookupPath(t *testing.T) {
	setupOAuth2TestRedis(t)

	token, err := IssueOAuth2Token(99, "default", "openwebui")
	require.NoError(t, err)
	require.NotEmpty(t, token.Key)
	require.Equal(t, "oauth2-openwebui", token.Name)
	require.Equal(t, 99, token.UserId)
	require.True(t, token.UnlimitedQuota)
	require.Equal(t, common.TokenStatusEnabled, token.Status)

	// The whole point: GetTokenByKey (used by the existing TokenAuth()
	// middleware) must resolve this token with no DB involved.
	looked, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.Equal(t, 99, looked.UserId)
	require.Equal(t, "default", looked.Group)
}

func TestIssueOAuth2Token_ReissueInvalidatesPriorToken(t *testing.T) {
	setupOAuth2TestRedis(t)

	first, err := IssueOAuth2Token(5, "default", "openwebui")
	require.NoError(t, err)

	second, err := IssueOAuth2Token(5, "default", "openwebui")
	require.NoError(t, err)
	require.NotEqual(t, first.Key, second.Key)

	_, err = GetTokenByKey(first.Key, false)
	require.Error(t, err, "the first token must no longer resolve after re-issuance")

	looked, err := GetTokenByKey(second.Key, false)
	require.NoError(t, err)
	require.Equal(t, 5, looked.UserId)
}

func TestRevokeOAuth2Token_InvalidatesLiveToken(t *testing.T) {
	setupOAuth2TestRedis(t)

	token, err := IssueOAuth2Token(11, "default", "openwebui")
	require.NoError(t, err)

	require.NoError(t, RevokeOAuth2Token(11))

	_, err = GetTokenByKey(token.Key, false)
	require.Error(t, err, "token must not resolve after revoke")
}

func TestRevokeOAuth2Token_NoOpWhenNoneExists(t *testing.T) {
	setupOAuth2TestRedis(t)

	require.NoError(t, RevokeOAuth2Token(123456))
}
