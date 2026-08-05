package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedOrganizationUser(t *testing.T, id int, username string, quota int64, organizationId int, organizationRole string) {
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
		Quota:       1000,
	}
	require.NoError(t, model.DB.Create(org).Error)
}

func getOrganizationForBillingTest(t *testing.T, organizationId int) model.Organization {
	t.Helper()
	var org model.Organization
	require.NoError(t, model.DB.First(&org, organizationId).Error)
	return org
}

func newOrganizationBillingContext() *gin.Context {
	c, _ := gin.CreateTestContext(nil)
	c.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func getUserQuotaForBillingTest(t *testing.T, userId int) int64 {
	t.Helper()
	quota, err := model.GetUserQuota(userId, true)
	require.NoError(t, err)
	return quota
}

func TestOrganizationWalletBillingPreConsumesMemberLimitAndOrganizationWallet(t *testing.T) {
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

	require.Equal(t, int64(75), getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(1000), getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(975), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
}

func TestOrganizationWalletBillingSettlesRefundToMemberLimitAndOrganizationWallet(t *testing.T) {
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

	require.Equal(t, int64(90), getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(1000), getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(990), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
}

func TestOrganizationWalletBillingOwnerRequestUsesOrganizationWallet(t *testing.T) {
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

	require.Equal(t, int64(975), getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(975), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
}

func TestOrganizationWalletBillingRejectsWhenOrganizationWalletInsufficient(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("quota", 10).Error)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, session)
	require.NotNil(t, apiErr)

	require.Equal(t, int64(100), getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(1000), getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(10), org.Quota)
}

func TestOrganizationSubscriptionBillingPreConsumesSubscriptionAndOrganizationWallet(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: 1,
		Title:          "Team Plan",
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
		TotalAmount:    80,
		Enabled:        true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	sub, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "org-sub-req-1",
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.Equal(t, int64(100), getUserQuotaForBillingTest(t, 2))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(975), org.Quota)

	var reloaded model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&reloaded, sub.Id).Error)
	require.Equal(t, int64(25), reloaded.AmountUsed)
	require.Equal(t, BillingSourceOrganizationSubscription, relayInfo.BillingSource)
	require.Equal(t, sub.Id, relayInfo.SubscriptionId)
}

func TestOrganizationSubscriptionBillingRejectsWhenPlanLimitInsufficient(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: 1,
		Title:          "Small Plan",
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
		TotalAmount:    10,
		Enabled:        true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	_, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "org-sub-req-2",
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, session)
	require.NotNil(t, apiErr)

	require.Equal(t, int64(100), getUserQuotaForBillingTest(t, 2))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(1000), org.Quota)
}

func TestOrganizationSubscriptionBillingSettlesRefundToSubscriptionAndOrganizationWallet(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: 1,
		Title:          "Team Plan",
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
		TotalAmount:    80,
		Enabled:        true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	sub, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "org-sub-settle-refund-req",
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NoError(t, session.Settle(10))

	var reloaded model.OrganizationUserSubscription
	require.NoError(t, model.DB.First(&reloaded, sub.Id).Error)
	require.Equal(t, int64(10), reloaded.AmountUsed)
	require.Equal(t, int64(100), getUserQuotaForBillingTest(t, 2))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(990), org.Quota)
}

func TestOrganizationSubscriptionBillingRefundRestoresSubscriptionAndOrganizationWallet(t *testing.T) {
	truncate(t)

	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: 1,
		Title:          "Team Plan",
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
		TotalAmount:    80,
		Enabled:        true,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	sub, err := model.CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.NoError(t, err)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "org-sub-refund-req",
		UserId:          2,
		OriginModelName: "test-model",
		IsPlayground:    true,
		ForcePreConsume: true,
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	session.Refund(newOrganizationBillingContext())

	require.Eventually(t, func() bool {
		var reloaded model.OrganizationUserSubscription
		if err := model.DB.First(&reloaded, sub.Id).Error; err != nil {
			return false
		}
		org := getOrganizationForBillingTest(t, 1)
		return reloaded.AmountUsed == 0 && org.Quota == 1000
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, int64(100), getUserQuotaForBillingTest(t, 2))
}
