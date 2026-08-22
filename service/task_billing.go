package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption
// BillingSessionPreConsumeBilling + SettleBilling
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("action %s", info.Action)
	// support per-call billing for tasks
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s, per-call billing", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, calc params: %s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     info.PriceData.Quota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, int64(info.PriceData.Quota))
	UpdateOrganizationUsedQuotaForUser(info.UserId, int64(info.PriceData.Quota))
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// resolveTokenKey TokenId Key Redis
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("failed to get token key (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

func taskIsOrganizationSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceOrganizationSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding delta > 0 delta < 0
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if taskIsOrganizationSubscription(task) {
		sub, err := model.GetOrganizationUserSubscriptionById(task.PrivateData.SubscriptionId)
		if err != nil {
			return err
		}
		if delta > 0 {
			if err := model.PostConsumeOrganizationUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta)); err != nil {
				return err
			}
			if err := model.DecreaseOrganizationQuota(sub.OrganizationId, int64(delta)); err != nil {
				_ = model.PostConsumeOrganizationUserSubscriptionDelta(task.PrivateData.SubscriptionId, -int64(delta))
				return err
			}
			return nil
		}
		if delta < 0 {
			refund := -delta
			if err := model.IncreaseOrganizationQuota(sub.OrganizationId, int64(refund)); err != nil {
				return err
			}
			if err := model.PostConsumeOrganizationUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta)); err != nil {
				_ = model.DecreaseOrganizationQuota(sub.OrganizationId, int64(refund))
				return err
			}
		}
		return nil
	}
	return AdjustWalletQuotaForUser(task.UserId, int64(delta))
}

// taskAdjustTokenQuota delta > 0 delta < 0
// resolveTokenKey key PrivateData
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("failed to adjust token quota (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther task BillingContext Other
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName BillingContext Properties
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// RefundTaskQuota
// quota
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) {
	quota := task.Quota
	if quota == 0 {
		return
	}

	// 1.
	if err := taskAdjustFunding(task, -quota); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("failed to refund funding source for task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 2.
	taskAdjustTokenQuota(ctx, task, -quota)

	// 3. Refunding only restores the wallet/subscription balance and token
	// quota above; without this, used_quota would never come back down and
	// "total quota" (quota + used_quota) inflates further with every refund.
	model.UpdateUserUsedQuota(task.UserId, int64(-quota))
	model.UpdateChannelUsedQuota(task.ChannelId, -quota)

	// 4.
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
}

// RecalculateTaskQuota
// actualQuota (task.Quota)
// reason "token" "adaptor"
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string) {
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("task %s pre-charge accurate (%s, %s)",
			task.TaskID, logger.LogQuota(int64(actualQuota)), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("task %s settlement delta: delta=%s (actual: %s, pre-charged: %s, %s)",
		task.TaskID,
		logger.LogQuota(int64(quotaDelta)),
		logger.LogQuota(int64(actualQuota)),
		logger.LogQuota(int64(preConsumedQuota)),
		reason,
	))

	// adjust funding source
	if err := taskAdjustFunding(task, quotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("settlement funding adjustment failed for task %s: %s", task.TaskID, err.Error()))
		return
	}

	taskAdjustTokenQuota(ctx, task, quotaDelta)

	task.Quota = actualQuota
	if err := task.Update(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("failed to persist settled quota for task %s: %s", task.TaskID, err.Error()))
	}

	// Adjust accumulated usage by the delta regardless of sign: a positive
	// delta is an additional charge, a negative delta is correcting an
	// earlier over-charge back down. Both must move used_quota, or an
	// over-charge correction leaves it permanently inflated. Uses
	// UpdateUserUsedQuota (not ...AndRequestCount) since submission time
	// already counted this request once.
	model.UpdateUserUsedQuota(task.UserId, int64(quotaDelta))
	UpdateOrganizationUsedQuotaForUser(task.UserId, int64(quotaDelta))
	model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)

	var logType int
	var logQuota int
	if quotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = quotaDelta
	} else {
		logType = model.LogTypeRefund
		logQuota = -quotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = preConsumedQuota
	other["actual_quota"] = actualQuota
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
}

// RecalculateTaskQuotaByTokens token
// totalTokens
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	if totalTokens <= 0 {
		return
	}

	modelName := taskModelName(task)

	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// () token
	if !hasRatioSetting || modelRatio <= 0 {
		return
	}

	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}
	// Apply model/vendor discount on the resolved group ratio for the task billing path.
	finalGroupRatio *= ratio_setting.GetEffectiveDiscountMultiplier(modelName)

	// OtherRatios
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// : totalTokens * modelRatio * groupRatio * otherMultiplier
	actualQuota := int(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token recalc: tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	RecalculateTaskQuota(ctx, task, actualQuota, reason)
}
