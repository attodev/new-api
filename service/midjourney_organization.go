package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// fork: O3 — legacy Midjourney creates no BillingSession, so the organization
// is pinned here. postConsumeOrganizationQuota then charges the member limit
// and the organization wallet together.

// CheckMidjourneyOrganizationBilling is the pre-call guard for legacy
// Midjourney (relay/mjproxy_handler.go). Billing is prepared only after the
// upstream call, so organization plan members and empty organization wallets
// must be rejected before the request is sent. Errors reach the Midjourney
// client as the task description, in the language c states.
func CheckMidjourneyOrganizationBilling(c *gin.Context, userId int) error {
	organizationId, memberStatus, err := model.GetUserOrganizationMembership(userId)
	if err != nil {
		return err
	}
	if organizationId <= 0 {
		return nil
	}
	lang := i18n.StatedLang(c)
	if err := model.RequireActiveOrganization(organizationId); err != nil {
		if errors.Is(err, model.ErrOrganizationDisabled) {
			return organizationError(lang, err, i18n.MsgOrganizationDisabled)
		}
		return err
	}
	if memberStatus == model.OrganizationMemberStatusSuspended {
		return organizationError(lang, model.ErrOrganizationMemberSuspended, i18n.MsgOrganizationMemberSuspended)
	}
	hasPlan, err := model.HasActiveOrganizationUserSubscription(organizationId, userId)
	if err != nil {
		return err
	}
	if hasPlan {
		return organizationError(lang, nil, i18n.MsgOrganizationMjPlanUnsupported)
	}
	organizationQuota, err := model.GetOrganizationQuota(organizationId)
	if err != nil {
		return err
	}
	if organizationQuota <= 0 {
		return organizationError(lang, nil, i18n.MsgOrganizationQuotaInsufficient)
	}
	return nil
}

// prepareMidjourneyOrganizationBilling pins the member's organization on the
// relay info and on the task row (midjourneys.organization_id), so a refund
// returns to the organization that was charged. Organization plan members are
// rejected: legacy Midjourney cannot bill a plan and a plan member must not
// fall back to personal quota.
func prepareMidjourneyOrganizationBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney) error {
	task.OrganizationId = 0
	organizationId, err := model.GetUserOrganizationId(relayInfo.UserId)
	if err != nil {
		return err
	}
	if organizationId <= 0 {
		return nil
	}
	hasPlan, err := model.HasActiveOrganizationUserSubscription(organizationId, relayInfo.UserId)
	if err != nil {
		return err
	}
	if hasPlan {
		// Only logged by the Midjourney handler (never sent to the client), so
		// it stays English like the other billing log lines.
		return errors.New("legacy Midjourney billing does not support organization plans")
	}
	relayInfo.BillingOrganizationId = organizationId
	task.OrganizationId = organizationId
	return nil
}

// refundMidjourneyUserQuota replaces upstream's IncreaseUserQuota in
// RefundMidjourneyQuota. A task charged to an organization credits the member
// limit only while the user is still in that organization (refundMemberLimit);
// a personal task refunds the user exactly as upstream does.
func refundMidjourneyUserQuota(task *model.Midjourney, quota int) error {
	_, err := refundMemberLimit(task.UserId, task.OrganizationId, quota)
	return err
}

// refundMidjourneyOrganizationQuota is the organization half of
// RefundMidjourneyQuota: the organization wallet and used_quota of the
// organization stored on the task row are restored. Personal tasks
// (organization_id 0) touch no organization, even if the user joined one since.
func refundMidjourneyOrganizationQuota(task *model.Midjourney, quota int) {
	if quota <= 0 || task.OrganizationId <= 0 {
		return
	}
	if err := model.IncreaseOrganizationQuota(task.OrganizationId, int64(quota)); err != nil {
		common.SysLog(common.LogText("error refunding organization wallet for Midjourney task (userId=%d, organizationId=%d, quota=%d): %s", task.UserId, task.OrganizationId, quota, err.Error()))
	}
	if err := model.UpdateOrganizationUsedQuota(task.OrganizationId, int64(-quota)); err != nil {
		common.SysLog(common.LogText("error updating organization used quota for Midjourney refund (organizationId=%d, quota=%d): %s", task.OrganizationId, quota, err.Error()))
	}
}
