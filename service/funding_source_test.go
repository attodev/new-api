package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// TestUpdateOrganizationUsedQuotaForUser_AppliesNegativeDelta reproduces a
// real bug: RecalculateTaskQuota's settlement step explicitly documents that
// used_quota "must" be adjusted by the delta "regardless of sign" (a
// negative delta corrects an earlier over-charge back down) and does so
// correctly for the user-level model.UpdateUserUsedQuota call right next to
// it - but the organization-level call, UpdateOrganizationUsedQuotaForUser,
// early-returns on `quota <= 0`, silently dropping every refund/correction
// for an organization-billed task and leaving the organization's used_quota
// permanently inflated by the refunded amount.
func TestUpdateOrganizationUsedQuotaForUser_AppliesNegativeDelta(t *testing.T) {
	truncate(t)

	const userID, orgID = 30, 30
	org := &model.Organization{Id: orgID, Name: "test_org", OwnerUserId: userID, UsedQuota: 500}
	require.NoError(t, model.DB.Create(org).Error)

	user := &model.User{Id: userID, Username: "test_org_user", Status: common.UserStatusEnabled, OrganizationId: orgID}
	require.NoError(t, model.DB.Create(user).Error)

	UpdateOrganizationUsedQuotaForUser(userID, -200)

	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, orgID).Error)
	require.EqualValues(t, 300, reloaded.UsedQuota,
		"a negative delta (refund/over-charge correction) must reduce the organization's used_quota, not be silently dropped")
}

func TestUpdateOrganizationUsedQuotaForUser_AppliesPositiveDelta(t *testing.T) {
	truncate(t)

	const userID, orgID = 31, 31
	org := &model.Organization{Id: orgID, Name: "test_org2", OwnerUserId: userID, UsedQuota: 500}
	require.NoError(t, model.DB.Create(org).Error)

	user := &model.User{Id: userID, Username: "test_org_user2", Status: common.UserStatusEnabled, OrganizationId: orgID}
	require.NoError(t, model.DB.Create(user).Error)

	UpdateOrganizationUsedQuotaForUser(userID, 200)

	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, orgID).Error)
	require.EqualValues(t, 700, reloaded.UsedQuota)
}
