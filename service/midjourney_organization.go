package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// fork: O3 — legacy Midjourney creates no BillingSession, so the organization
// is pinned here. postConsumeOrganizationQuota then charges the member limit
// and the organization wallet together.

// prepareMidjourneyOrganizationBilling pins the member's organization on the
// relay info. Organization plan members are rejected: legacy Midjourney cannot
// bill a plan and a plan member must not fall back to personal quota.
func prepareMidjourneyOrganizationBilling(relayInfo *relaycommon.RelayInfo) error {
	user, err := model.GetUserById(relayInfo.UserId, false)
	if err != nil {
		return err
	}
	if user.OrganizationId <= 0 {
		return nil
	}
	hasPlan, err := model.HasActiveOrganizationUserSubscription(user.OrganizationId, relayInfo.UserId)
	if err != nil {
		return err
	}
	if hasPlan {
		return errors.New("legacy Midjourney billing does not support organization plans")
	}
	relayInfo.BillingOrganizationId = user.OrganizationId
	return nil
}

// refundMidjourneyOrganizationQuota is the organization half of
// RefundMidjourneyQuota; upstream has already refunded the member limit with
// IncreaseUserQuota, so only the organization wallet and used_quota are
// restored here. The midjourneys row stores no organization id, so the refund
// goes to the member's current organization (the one charged unless the
// member changed organization while the task ran).
func refundMidjourneyOrganizationQuota(userId int, quota int) {
	if quota <= 0 {
		return
	}
	organizationId, err := resolveOrganizationForWallet(userId)
	if err != nil {
		common.SysLog(common.LogText("error resolving organization for Midjourney refund (userId=%d): %s", userId, err.Error()))
		return
	}
	if organizationId <= 0 {
		return
	}
	if err := model.IncreaseOrganizationQuota(organizationId, int64(quota)); err != nil {
		common.SysLog(common.LogText("error refunding organization wallet for Midjourney task (userId=%d, organizationId=%d, quota=%d): %s", userId, organizationId, quota, err.Error()))
	}
	UpdateOrganizationUsedQuotaForUser(userId, -quota)
}
