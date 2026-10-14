package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — the realtime (WSS) pre-check also covers the organization wallet:
// a member whose spending cap is large enough must still be refused when the
// shared wallet cannot cover the reservation.

func preWssOrganizationContext(t *testing.T, userId int, tokenId int, tokenKey string, modelName string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	return ctx, &relaycommon.RelayInfo{
		UserId:          userId,
		TokenId:         tokenId,
		TokenKey:        "sk-" + tokenKey,
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
}

func TestPreWssConsumeQuotaRefusesWhenOrganizationWalletIsShort(t *testing.T) {
	truncate(t)
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	setDiscountTestModelRatio(t, "org-wss-model", 1)
	seedOrganization(t, 1, 90) // organization wallet 1000
	seedOrganizationUser(t, 90, "owner_90", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 91, "member_91", 100_000, 1, model.OrganizationRoleMember)
	seedToken(t, 91, 91, "orgwsskey", 100_000)

	ctx, info := preWssOrganizationContext(t, 91, 91, "orgwsskey", "org-wss-model")
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 5000}}

	err := PreWssConsumeQuota(ctx, info, usage)
	require.ErrorIs(t, err, ErrInsufficientOrganizationWalletQuota)
	require.Equal(t, 100_000, getUserQuota(t, 91), "a refused reservation charges nothing")
	quota, qerr := model.GetOrganizationQuota(1)
	require.NoError(t, qerr)
	require.EqualValues(t, 1000, quota)
}

func TestPreWssConsumeQuotaIgnoresOrganizationWalletForNonMembers(t *testing.T) {
	truncate(t)
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	setDiscountTestModelRatio(t, "org-wss-model", 1)
	seedUser(t, 92, 100_000)
	seedToken(t, 92, 92, "personalwsskey", 100_000)

	ctx, info := preWssOrganizationContext(t, 92, 92, "personalwsskey", "org-wss-model")
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 5000}}

	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
}

func TestRequireOrganizationWalletFor(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 93) // organization wallet 1000
	seedOrganizationUser(t, 93, "owner_93", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 94, "member_94", 0, 1, model.OrganizationRoleMember)
	seedUser(t, 95, 0)

	require.NoError(t, requireOrganizationWalletFor(94, 1000))
	require.ErrorIs(t, requireOrganizationWalletFor(94, 1001), ErrInsufficientOrganizationWalletQuota)
	require.NoError(t, requireOrganizationWalletFor(95, 1_000_000), "non-members have no organization wallet to check")
}
