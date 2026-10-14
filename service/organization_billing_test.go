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

func getUserQuotaForBillingTest(t *testing.T, userId int) int {
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
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 1},
	}

	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
	require.Nil(t, apiErr)
	require.NotNil(t, session)

	require.Equal(t, 75, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 1000, getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(975), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
	require.Equal(t, 1, relayInfo.BillingOrganizationId)
	task := model.InitTask("", relayInfo)
	require.Equal(t, 1, task.PrivateData.OrganizationId)
}

func TestOrganizationWalletFundingPinsOrganizationAcrossMembershipChange(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "owner-one", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganizationUser(t, 3, "owner-two", 1000, 2, model.OrganizationRoleOwner)
	seedOrganization(t, 1, 1)
	second := &model.Organization{Id: 2, Name: "org-two", OwnerUserId: 3, Status: model.OrganizationStatusEnabled, Quota: 1000}
	require.NoError(t, model.DB.Create(second).Error)

	funding := &OrganizationWalletFunding{memberId: 2, organizationId: 1}
	require.NoError(t, funding.PreConsume(25))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 2).Update("organization_id", 2).Error)
	require.NoError(t, funding.Refund())

	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(1000), getOrganizationForBillingTest(t, 1).Quota)
	require.Equal(t, int64(1000), getOrganizationForBillingTest(t, 2).Quota)
}

func TestOrganizationWalletFundingRollsBackMemberWhenOrganizationInsufficient(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("quota", 10).Error)

	funding := &OrganizationWalletFunding{memberId: 2, organizationId: 1}
	require.ErrorIs(t, funding.PreConsume(25), ErrInsufficientWalletQuota)
	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(10), getOrganizationForBillingTest(t, 1).Quota)
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

	require.Equal(t, 90, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 1000, getUserQuotaForBillingTest(t, 1))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(990), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
}

func TestOrganizationWalletBillingReserveIncreasesRetryPreConsume(t *testing.T) {
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
	require.NoError(t, session.Reserve(40))

	require.Equal(t, 40, session.GetPreConsumedQuota())
	require.Equal(t, 60, getUserQuotaForBillingTest(t, 2))
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(960), org.Quota)

	session.Refund(newOrganizationBillingContext())
	require.Eventually(t, func() bool {
		org = getOrganizationForBillingTest(t, 1)
		return getUserQuotaForBillingTest(t, 2) == 100 && org.Quota == 1000
	}, time.Second, 10*time.Millisecond)
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

	require.Equal(t, 975, getUserQuotaForBillingTest(t, 1))
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

	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, 1000, getUserQuotaForBillingTest(t, 1))
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

	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
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

	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
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
	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
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
	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
}

// createOrganizationPlanAndAssign creates a monthly organization plan with the
// given total and assigns it to the user.
func createOrganizationPlanAndAssign(t *testing.T, organizationId int, userId int, total int64) (*model.OrganizationUserSubscription, error) {
	t.Helper()
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: organizationId,
		Title:          "p",
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
		Enabled:        true,
		TotalAmount:    total,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	return model.CreateOrganizationUserSubscriptionFromPlan(organizationId, userId, plan.Id, 0)
}

// Review Focus 1: an organization plan with remaining limit must still be
// rejected when the organization wallet cannot cover the request, without
// touching the member limit or the plan.
func TestOrganizationSubscriptionBillingRejectsWhenOrganizationWalletInsufficient(t *testing.T) {
	truncate(t)

	const orgID = 1
	seedOrganization(t, orgID, 30)
	// plan limit remains, organization wallet is empty
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", orgID).Update("quota", 0).Error)
	seedOrganizationUser(t, 30, "member", 5000, orgID, model.OrganizationRoleMember)
	created, err := createOrganizationPlanAndAssign(t, orgID, 30, 100000)
	require.NoError(t, err)
	require.NotNil(t, created)

	relayInfo := &relaycommon.RelayInfo{
		RequestId:       "org-sub-empty-wallet-req",
		UserId:          30,
		OriginModelName: "gpt-x",
		IsPlayground:    true,
		ForcePreConsume: true,
	}
	session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 100)
	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "organization quota insufficient")

	var member model.User
	require.NoError(t, model.DB.First(&member, 30).Error)
	require.Equal(t, 5000, member.Quota, "member limit untouched")
	sub, err := model.GetActiveOrganizationUserSubscription(orgID, 30)
	require.NoError(t, err)
	require.EqualValues(t, 0, sub.AmountUsed, "plan untouched")
}
