package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// A real token has its quota adjusted on settlement -- only the playground's
// synthetic token skips the token row.
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
