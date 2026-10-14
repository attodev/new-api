package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// fork: O3 — async tasks settle against the organization recorded at submission.
func taskIsOrganizationSubscription(task *model.Task) bool {
	return isOrganizationSubscriptionSource(task.PrivateData.BillingSource) && task.PrivateData.SubscriptionId > 0
}

// taskAdjustOrganizationFunding returns handled=false for tasks without an organization.
func taskAdjustOrganizationFunding(task *model.Task, delta int) (bool, error) {
	orgId := task.PrivateData.OrganizationId
	if taskIsOrganizationSubscription(task) {
		subId := task.PrivateData.SubscriptionId
		if err := model.PostConsumeOrganizationUserSubscriptionDelta(subId, int64(delta)); err != nil {
			return true, err
		}
		if orgId <= 0 {
			return true, nil
		}
		var orgErr error
		if delta > 0 {
			orgErr = model.DecreaseOrganizationQuota(orgId, int64(delta))
		} else {
			orgErr = model.IncreaseOrganizationQuota(orgId, int64(-delta))
		}
		if orgErr != nil {
			// Undo the plan step so a retried settlement or refund does not apply it twice.
			if rollbackErr := model.PostConsumeOrganizationUserSubscriptionDelta(subId, -int64(delta)); rollbackErr != nil {
				common.SysLog(common.LogText("error undoing task organization subscription delta (task=%s, subscriptionId=%d, delta=%d): %s", task.TaskID, subId, delta, rollbackErr.Error()))
			}
		}
		return true, orgErr
	}
	if orgId > 0 {
		return true, adjustWalletQuotaForOrganization(task.UserId, orgId, delta)
	}
	return false, nil
}

func taskAdjustOrganizationUsedQuota(task *model.Task, delta int) {
	if task.PrivateData.OrganizationId > 0 && delta != 0 {
		if err := model.UpdateOrganizationUsedQuota(task.PrivateData.OrganizationId, int64(delta)); err != nil {
			common.SysLog(common.LogText("error updating task organization used quota (task=%s, organizationId=%d, delta=%d): %s", task.TaskID, task.PrivateData.OrganizationId, delta, err.Error()))
		}
	}
}
