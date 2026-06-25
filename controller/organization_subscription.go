package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type organizationSubscriptionPlanRequest struct {
	Title         string `json:"title"`
	Subtitle      string `json:"subtitle"`
	DurationUnit  string `json:"duration_unit"`
	DurationValue int    `json:"duration_value"`
	CustomSeconds int64  `json:"custom_seconds"`
	SortOrder     int    `json:"sort_order"`
	TotalAmount   int64  `json:"total_amount"`
}

type assignOrganizationSubscriptionRequest struct {
	PlanId int `json:"plan_id"`
}

func ListOrganizationSubscriptionPlans(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}
	plans, err := model.ListOrganizationSubscriptionPlans(organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plans)
}

func CreateOrganizationSubscriptionPlan(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}
	var req organizationSubscriptionPlanRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	plan, err := buildOrganizationSubscriptionPlan(organizationId, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Create(plan).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plan)
}

func UpdateOrganizationSubscriptionPlan(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}
	planId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req organizationSubscriptionPlanRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	updates, err := buildOrganizationSubscriptionPlanUpdates(req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(updates) == 0 {
		common.ApiErrorI18n(c, i18n.MsgOrgSubNoFieldsToUpdate)
		return
	}
	if err := model.DB.Model(&model.OrganizationSubscriptionPlan{}).
		Where("id = ? AND organization_id = ?", planId, organizationId).
		Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteOrganizationSubscriptionPlan(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}
	planId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Model(&model.OrganizationSubscriptionPlan{}).
		Where("id = ? AND organization_id = ?", planId, organizationId).
		Update("enabled", false).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func ListOrganizationUserSubscriptions(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}
	items, err := model.ListOrganizationUserSubscriptions(organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func AssignOrganizationUserSubscription(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	organizationId := actor.OrganizationId
	if actor.Role == common.RoleRootUser {
		parsedOrganizationId, ok := resolveOrganizationAdminTarget(c)
		if !ok {
			return
		}
		organizationId = parsedOrganizationId
	} else if organizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
		return
	}

	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, err := model.GetUserById(targetId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target.OrganizationId != organizationId || target.Role >= common.RoleAdminUser {
		common.ApiErrorI18n(c, i18n.MsgOrgTargetPermDenied)
		return
	}

	var req assignOrganizationSubscriptionRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validatePositiveReferenceId("plan_id", req.PlanId); err != nil {
		common.ApiError(c, err)
		return
	}
	sub, err := model.CreateOrganizationUserSubscriptionFromPlan(organizationId, targetId, req.PlanId, actor.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, sub)
}

func CancelOrganizationUserSubscription(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	organizationId := actor.OrganizationId
	if actor.Role == common.RoleRootUser {
		parsedOrganizationId, ok := resolveOrganizationAdminTarget(c)
		if !ok {
			return
		}
		organizationId = parsedOrganizationId
	} else if organizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
		return
	}

	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err := model.CancelActiveOrganizationUserSubscriptionForUser(organizationId, targetId); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetMyOrganizationSubscription(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId <= 0 {
		common.ApiSuccess(c, nil)
		return
	}
	sub, err := model.GetActiveOrganizationUserSubscription(actor.OrganizationId, actor.Id)
	if err != nil {
		common.ApiSuccess(c, nil)
		return
	}
	plan, _ := model.GetOrganizationSubscriptionPlanById(actor.OrganizationId, sub.PlanId)
	common.ApiSuccess(c, model.OrganizationUserSubscriptionSummary{
		Subscription: sub,
		User:         actor,
		Plan:         plan,
	})
}

func buildOrganizationSubscriptionPlan(organizationId int, req organizationSubscriptionPlanRequest) (*model.OrganizationSubscriptionPlan, error) {
	updates, err := buildOrganizationSubscriptionPlanUpdates(req)
	if err != nil {
		return nil, err
	}
	plan := &model.OrganizationSubscriptionPlan{
		OrganizationId: organizationId,
		Enabled:        true,
		DurationUnit:   model.SubscriptionDurationMonth,
		DurationValue:  1,
	}
	for key, value := range updates {
		switch key {
		case "title":
			plan.Title = value.(string)
		case "subtitle":
			plan.Subtitle = value.(string)
		case "duration_unit":
			plan.DurationUnit = value.(string)
		case "duration_value":
			plan.DurationValue = value.(int)
		case "custom_seconds":
			plan.CustomSeconds = value.(int64)
		case "enabled":
			plan.Enabled = value.(bool)
		case "sort_order":
			plan.SortOrder = value.(int)
		case "upgrade_group":
			plan.UpgradeGroup = value.(string)
		case "total_amount":
			plan.TotalAmount = value.(int64)
		case "quota_reset_period":
			plan.QuotaResetPeriod = value.(string)
		case "quota_reset_custom_seconds":
			plan.QuotaResetCustomSeconds = value.(int64)
		}
	}
	return plan, nil
}

func buildOrganizationSubscriptionPlanUpdates(req organizationSubscriptionPlanRequest) (map[string]interface{}, error) {
	title, err := trimAndValidateText("title", req.Title, MaxOrganizationSubscriptionPlanTitleLength, true)
	if err != nil {
		return nil, err
	}
	subtitle, err := trimAndValidateText("subtitle", req.Subtitle, MaxOrganizationSubscriptionPlanSubtitleLength, false)
	if err != nil {
		return nil, err
	}
	if err := validateInt64Range("total_amount", req.TotalAmount, 0, int64(MaxOrganizationQuota)); err != nil {
		return nil, err
	}
	durationUnit := strings.TrimSpace(req.DurationUnit)
	if durationUnit == "" {
		durationUnit = model.SubscriptionDurationMonth
	}
	if err := validateOrganizationDurationUnit(durationUnit); err != nil {
		return nil, err
	}
	durationValue := req.DurationValue
	if durationValue < 0 {
		return nil, errors.New(OrganizationDurationValueRangeError)
	}
	if durationValue > MaxOrganizationSubscriptionDurationValue {
		return nil, errors.New(OrganizationDurationValueRangeError)
	}
	if durationValue == 0 && durationUnit != model.SubscriptionDurationCustom {
		durationValue = 1
	}
	if durationUnit == model.SubscriptionDurationCustom {
		if err := validateInt64Range("custom_seconds", req.CustomSeconds, 1, MaxOrganizationSubscriptionCustomSeconds); err != nil {
			return nil, err
		}
	} else if req.CustomSeconds < 0 || req.CustomSeconds > MaxOrganizationSubscriptionCustomSeconds {
		return nil, fmt.Errorf("custom_seconds must be between 0 and %d", MaxOrganizationSubscriptionCustomSeconds)
	}
	if err := validateIntRange("sort_order", req.SortOrder, -MaxOrganizationSubscriptionSortOrder, MaxOrganizationSubscriptionSortOrder); err != nil {
		return nil, err
	}
	if _, err := model.CalcSubscriptionPlanEndTime(time.Now(), durationUnit, durationValue, req.CustomSeconds); err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"title":                      title,
		"subtitle":                   subtitle,
		"duration_unit":              durationUnit,
		"duration_value":             durationValue,
		"custom_seconds":             req.CustomSeconds,
		"sort_order":                 req.SortOrder,
		"upgrade_group":              "",
		"total_amount":               req.TotalAmount,
		"quota_reset_period":         model.SubscriptionResetNever,
		"quota_reset_custom_seconds": int64(0),
	}
	return updates, nil
}
