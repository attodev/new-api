package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedOrganizationUser(t *testing.T, id int, username string, quota int, organizationId int, organizationRole string) {
	t.Helper()
	user := &model.User{
		Id:               id,
		Username:         username,
		Password:         "password",
		Quota:            quota,
		Status:           common.UserStatusEnabled,
		Role:             common.RoleCommonUser,
		OrganizationId:   organizationId,
		OrganizationRole: organizationRole,
		AffCode:          username,
	}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedOrganization(t *testing.T, id int, ownerUserId int) {
	t.Helper()
	org := &model.Organization{
		Id:          id,
		Name:        "org",
		OwnerUserId: ownerUserId,
		Status:      model.OrganizationStatusEnabled,
	}
	require.NoError(t, model.DB.Create(org).Error)
}

func newOrganizationBillingContext() *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func getUserQuotaForBillingTest(t *testing.T, userId int) int {
	t.Helper()
	quota, err := model.GetUserQuota(userId, true)
	require.NoError(t, err)
	return quota
}

func TestOrganizationWalletBillingPreConsumesMemberLimitAndOwnerWallet(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.Equal(t, 75, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 975, getUserQuotaForBillingTest(t, 1))
}

func TestOrganizationWalletBillingSettlesRefundToMemberLimitAndOwnerWallet(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(10))

	require.Equal(t, 90, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 990, getUserQuotaForBillingTest(t, 1))
}

func TestOrganizationWalletBillingOwnerRequestChargesOnce(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganization(t, 1, 1)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          1,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.Equal(t, 975, getUserQuotaForBillingTest(t, 1))
}

func TestOrganizationWalletBillingRejectsWhenOwnerWalletInsufficient(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 10, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, session)
	require.NotNil(t, apiErr)

	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 10, getUserQuotaForBillingTest(t, 1))
}
