package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// TestUpdateBillingOrganizationUsedQuota_AppliesNegativeDelta reproduces a
// real bug: RecalculateTaskQuota's settlement step explicitly documents that
// used_quota "must" be adjusted by the delta "regardless of sign" (a
// negative delta corrects an earlier over-charge back down) and does so
// correctly for the user-level model.UpdateUserUsedQuota call right next to
// it - but the organization-level call early-returned on `quota <= 0`,
// silently dropping every refund/correction for an organization-billed task
// and leaving the organization's used_quota permanently inflated by the
// refunded amount.
func TestUpdateBillingOrganizationUsedQuota_AppliesNegativeDelta(t *testing.T) {
	truncate(t)

	const userID, orgID = 30, 30
	org := &model.Organization{Id: orgID, Name: "test_org", OwnerUserId: userID, UsedQuota: 500}
	require.NoError(t, model.DB.Create(org).Error)

	user := &model.User{Id: userID, Username: "test_org_user", Status: common.UserStatusEnabled, OrganizationId: orgID}
	require.NoError(t, model.DB.Create(user).Error)

	UpdateBillingOrganizationUsedQuota(orgID, -200)

	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, orgID).Error)
	require.EqualValues(t, 300, reloaded.UsedQuota,
		"a negative delta (refund/over-charge correction) must reduce the organization's used_quota, not be silently dropped")
}

func TestUpdateBillingOrganizationUsedQuota_AppliesPositiveDelta(t *testing.T) {
	truncate(t)

	const userID, orgID = 31, 31
	org := &model.Organization{Id: orgID, Name: "test_org2", OwnerUserId: userID, UsedQuota: 500}
	require.NoError(t, model.DB.Create(org).Error)

	user := &model.User{Id: userID, Username: "test_org_user2", Status: common.UserStatusEnabled, OrganizationId: orgID}
	require.NoError(t, model.DB.Create(user).Error)

	UpdateBillingOrganizationUsedQuota(orgID, 200)

	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, orgID).Error)
	require.EqualValues(t, 700, reloaded.UsedQuota)
}

// TestTaskRefundUsesPinnedOrganization: an async task refunds the
// organization recorded at submission, even after the member moved.
func TestTaskRefundUsesPinnedOrganization(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 10) // helper from organization_billing_test.go: quota 1000
	second := &model.Organization{Id: 2, Name: "org-two", OwnerUserId: 10, Status: model.OrganizationStatusEnabled, Quota: 1000}
	require.NoError(t, model.DB.Create(second).Error)
	seedOrganizationUser(t, 10, "member", 500, 1, model.OrganizationRoleMember)
	task := &model.Task{UserId: 10, Quota: 100, PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet, OrganizationId: 1}}
	// The member left organization 1 (cap zeroed) and joined organization 2
	// after the task was charged.
	_, err := model.ClearOrganizationMembership(10, 1)
	require.NoError(t, err)
	require.NoError(t, model.AssignUserToOrganization(10, 2, model.OrganizationRoleMember))
	handled, err := taskAdjustOrganizationFunding(task, -100)
	require.True(t, handled)
	require.NoError(t, err)
	q1, _ := model.GetOrganizationQuota(1)
	q2, _ := model.GetOrganizationQuota(2)
	require.EqualValues(t, 1100, q1, "refund goes to the pinned organization")
	require.EqualValues(t, 1000, q2)
	memberQuota, err := model.GetUserQuota(10, true)
	require.NoError(t, err)
	require.Zero(t, memberQuota, "an ex-member's cap is not refunded into a balance")
}

func TestTaskRefundCreditsMemberStillInPinnedOrganization(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 10)
	seedOrganizationUser(t, 10, "member", 500, 1, model.OrganizationRoleMember)
	task := &model.Task{UserId: 10, Quota: 100, PrivateData: model.TaskPrivateData{BillingSource: BillingSourceWallet, OrganizationId: 1}}
	handled, err := taskAdjustOrganizationFunding(task, -100)
	require.True(t, handled)
	require.NoError(t, err)
	q1, _ := model.GetOrganizationQuota(1)
	require.EqualValues(t, 1100, q1)
	memberQuota, err := model.GetUserQuota(10, true)
	require.NoError(t, err)
	require.Equal(t, 600, memberQuota)
}

// M3: used_quota follows the organization pinned on the relay info
// (relayInfo.BillingOrganizationId), not the member's current organization.
func TestTaskConsumptionUsedQuotaGoesToPinnedOrganization(t *testing.T) {
	truncate(t)
	const userID, channelID = 41, 41
	seedChannel(t, channelID)
	seedOrganization(t, 1, userID)
	second := &model.Organization{Id: 2, Name: "org-two", OwnerUserId: userID, Status: model.OrganizationStatusEnabled, Quota: 1000}
	require.NoError(t, model.DB.Create(second).Error)
	// Charged as a member of organization 1, moved to organization 2 before settlement.
	seedOrganizationUser(t, userID, "moved-member", 500, 2, model.OrganizationRoleMember)
	task := makeTask(userID, channelID, 100, 0, BillingSourceWallet, 0)
	info := &relaycommon.RelayInfo{
		UserId:                userID,
		BillingOrganizationId: 1,
		OriginModelName:       "video-model",
		UsingGroup:            "default",
		ChannelMeta:           &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:         &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:             types.PriceData{ModelPrice: 0.02, Quota: 100, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}

	callLogTaskConsumption(t, info, task)

	require.EqualValues(t, 100, getOrganizationForBillingTest(t, 1).UsedQuota)
	require.EqualValues(t, 0, getOrganizationForBillingTest(t, 2).UsedQuota)
}

func TestUpdateBillingOrganizationUsedQuota_NoOrganizationIsNoOp(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 1)
	UpdateBillingOrganizationUsedQuota(0, 100)
	require.EqualValues(t, 0, getOrganizationForBillingTest(t, 1).UsedQuota)
}
