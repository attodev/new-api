package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// The OAuth2 relay token lives only in Redis -- IssueOAuth2Token deliberately
// skips Token.Insert() -- and carries UnlimitedQuota, so there is neither a row
// to update nor quota to move.
//
// Settle() adjusts the token's quota after the user's funding has already
// settled. Against this token that UPDATE matches nothing, so DecreaseTokenQuota
// returns "record not found" and Settle returns an error for a settlement that
// in fact succeeded. The user is billed correctly either way, which is why this
// only ever showed up as a line in the log: every single relayed chat produced
// "error settling billing: record not found".
func TestSettleSkipsTokenQuotaForTheOAuth2SentinelToken(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "oauth2-user", 1000, 0, "")

	relayInfo := &relaycommon.RelayInfo{
		UserId:          1,
		TokenId:         math.MaxInt32, // the sentinel IssueOAuth2Token assigns
		TokenUnlimited:  true,          // IssueOAuth2Token sets UnlimitedQuota
		OriginModelName: "test-model",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
	}

	// Pre-consume is 0 here, matching what a relayed chat actually does -- the
	// failure this pins is in settlement, after the response has already been
	// streamed and the user's funding has already moved.
	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 0)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.NoError(t, session.Settle(40))

	// The user's own quota still moved: skipping the token row must not skip
	// the billing itself.
	require.Equal(t, int64(960), getUserQuotaForBillingTest(t, 1))
}

// A real token still has its quota adjusted -- the skip must be narrow.
func TestSettleStillAdjustsARealTokensQuota(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "token-user", 1000, 0, "")
	require.NoError(t, model.DB.Create(&model.Token{
		Id: 7, UserId: 1, Key: "real-token-key", Name: "real",
		Status: 1, RemainQuota: 500,
	}).Error)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          1,
		TokenId:         7,
		TokenKey:        "real-token-key",
		OriginModelName: "test-model",
		ForcePreConsume: true,
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(40))

	var tok model.Token
	require.NoError(t, model.DB.First(&tok, 7).Error)
	require.Equal(t, 460, tok.RemainQuota)
}

func TestIsOAuth2SentinelTokenId(t *testing.T) {
	require.True(t, model.IsOAuth2SentinelTokenId(math.MaxInt32))
	require.False(t, model.IsOAuth2SentinelTokenId(0))
	require.False(t, model.IsOAuth2SentinelTokenId(7))
	require.False(t, model.IsOAuth2SentinelTokenId(-1))
}
