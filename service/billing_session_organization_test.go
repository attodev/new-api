package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// fork: O3 — a member an organization admin suspended
// (users.organization_member_status = 2) cannot relay; nothing is charged.

func suspendOrganizationMemberForTest(t *testing.T, userId int) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userId).
		Update("organization_member_status", model.OrganizationMemberStatusSuspended).Error)
}

func TestSuspendedOrganizationMemberRelayIsRefused(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganizationUser(t, 3, "plan-member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	_, err := createOrganizationPlanAndAssign(t, 1, 3, 1000)
	require.NoError(t, err)
	suspendOrganizationMemberForTest(t, 2)
	suspendOrganizationMemberForTest(t, 3)

	for _, userId := range []int{2, 3} {
		relayInfo := &relaycommon.RelayInfo{UserId: userId, OriginModelName: "test-model", IsPlayground: true, ForcePreConsume: true}
		session, apiErr := NewBillingSession(newOrganizationBillingContext(), relayInfo, 25)
		require.Nil(t, session)
		require.NotNil(t, apiErr)
		require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
		require.Equal(t, types.ErrorCodeAccessDenied, apiErr.GetErrorCode())
		require.Contains(t, apiErr.Error(), "organization membership is suspended")
	}
	require.Equal(t, 100, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(1000), getOrganizationForBillingTest(t, 1).Quota)
	sub, err := model.GetActiveOrganizationUserSubscription(1, 3)
	require.NoError(t, err)
	require.Zero(t, sub.AmountUsed)

	// The owner (active) still relays.
	session, apiErr := NewBillingSession(newOrganizationBillingContext(),
		&relaycommon.RelayInfo{UserId: 1, OriginModelName: "test-model", IsPlayground: true, ForcePreConsume: true}, 25)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
}

func TestSuspendedOrganizationMemberMidjourneyIsRefused(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 500, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)
	suspendOrganizationMemberForTest(t, 2)

	require.ErrorIs(t, CheckMidjourneyOrganizationBilling(2), model.ErrOrganizationMemberSuspended)
}
