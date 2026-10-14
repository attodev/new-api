package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestPrepareMidjourneyOrganizationBillingPinsOrganization(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 500, 1, model.OrganizationRoleMember)
	seedOrganizationUser(t, 3, "plan-member", 500, 1, model.OrganizationRoleMember)
	seedUser(t, 4, 500)
	seedOrganization(t, 1, 2)
	_, err := createOrganizationPlanAndAssign(t, 1, 3, 1000)
	require.NoError(t, err)

	// Member without a plan: the organization is pinned and the task is billed.
	info := &relaycommon.RelayInfo{UserId: 2}
	task := &model.Midjourney{UserId: 2}
	prepared, err := PrepareMidjourneyTaskBilling(info, task, 100, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.Equal(t, 1, info.BillingOrganizationId)
	require.Equal(t, 100, task.Quota)

	// Member with an organization plan: legacy Midjourney cannot bill a plan.
	planInfo := &relaycommon.RelayInfo{UserId: 3}
	planTask := &model.Midjourney{UserId: 3}
	prepared, err = PrepareMidjourneyTaskBilling(planInfo, planTask, 100, true)
	require.Error(t, err)
	require.False(t, prepared)
	require.Equal(t, 0, planTask.Quota)
	require.Equal(t, 0, planInfo.BillingOrganizationId)

	// Not an organization member: upstream behaviour.
	soloInfo := &relaycommon.RelayInfo{UserId: 4}
	prepared, err = PrepareMidjourneyTaskBilling(soloInfo, &model.Midjourney{UserId: 4}, 100, true)
	require.NoError(t, err)
	require.True(t, prepared)
	require.Equal(t, 0, soloInfo.BillingOrganizationId)
}

func TestSettleMidjourneyChargesMemberLimitAndOrganizationWallet(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 500, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)

	info := &relaycommon.RelayInfo{UserId: 2, IsPlayground: true}
	task := &model.Midjourney{UserId: 2, MjId: "mj-settle"}
	prepared, err := PrepareMidjourneyTaskBilling(info, task, 100, true)
	require.NoError(t, err)
	require.NoError(t, task.Insert())
	applied, err := SettleMidjourneyTaskBilling(info, task, prepared)
	require.NoError(t, err)
	require.True(t, applied)

	require.Equal(t, 400, getUserQuotaForBillingTest(t, 2))
	require.Equal(t, int64(900), getOrganizationForBillingTest(t, 1).Quota)
}

func TestRefundMidjourneyOrganizationQuota(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 400, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)
	// State after a billed 100-quota task: org wallet 900, org used 100.
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).
		Updates(map[string]any{"quota": 900, "used_quota": 100}).Error)

	task := &model.Midjourney{UserId: 2, MjId: "mj-refund", Quota: 100}
	require.NoError(t, task.Insert())
	require.True(t, RefundMidjourneyQuota(context.Background(), task, "failed"))

	require.Equal(t, 500, getUserQuotaForBillingTest(t, 2), "member refunded exactly once")
	org := getOrganizationForBillingTest(t, 1)
	require.Equal(t, int64(1000), org.Quota)
	require.Equal(t, int64(0), org.UsedQuota)
}
