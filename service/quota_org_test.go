package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fork: O3 — the realtime (WSS) pre-check also covers the organization wallet
// pinned at session start (relayInfo.BillingOrganizationId, the organization
// the charge goes to): a member whose spending cap is large enough must still
// be refused when that wallet cannot cover the charge.

func preWssOrganizationContext(t *testing.T, userId int, organizationId int, tokenId int, tokenKey string, modelName string) (*gin.Context, *relaycommon.RelayInfo) {
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

		BillingOrganizationId: organizationId,
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

	ctx, info := preWssOrganizationContext(t, 91, 1, 91, "orgwsskey", "org-wss-model")
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

	ctx, info := preWssOrganizationContext(t, 92, 0, 92, "personalwsskey", "org-wss-model")
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 5000}}

	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))
}

func TestRequireOrganizationWalletFor(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 93) // organization wallet 1000

	require.NoError(t, requireOrganizationWalletFor(1, 1000))
	require.ErrorIs(t, requireOrganizationWalletFor(1, 1001), ErrInsufficientOrganizationWalletQuota)
	require.NoError(t, requireOrganizationWalletFor(0, 1_000_000), "no pinned organization: nothing to check")

	// The pinned organization is checked, not the user's current membership:
	// no users row is read at all.
	queries := 0
	const name = "test:wss_wallet_reads"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) { queries++ }))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(name) })
	require.NoError(t, requireOrganizationWalletFor(0, 5))
	require.Zero(t, queries, "a request without an organization reads nothing")
}
