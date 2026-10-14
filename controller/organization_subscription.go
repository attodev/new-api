package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	recordOrganizationAudit(c, "organization.plan.create",
		fmt.Sprintf("Created organization %d subscription plan %d %q (total amount %d, duration %d %s)", organizationId, plan.Id, plan.Title, plan.TotalAmount, plan.DurationValue, plan.DurationUnit),
		map[string]any{"organization_id": organizationId, "plan_id": plan.Id, "title": plan.Title, "total_amount": plan.TotalAmount,
			"duration_unit": plan.DurationUnit, "duration_value": plan.DurationValue, "custom_seconds": plan.CustomSeconds})
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
		common.ApiErrorT(c, "no subscription plan fields to update")
		return
	}
	if !requireOrganizationSubscriptionPlan(c, organizationId, planId) {
		return
	}
	if err := model.DB.Model(&model.OrganizationSubscriptionPlan{}).
		Where("id = ? AND organization_id = ?", planId, organizationId).
		Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.plan.update",
		fmt.Sprintf("Updated organization %d subscription plan %d %q (total amount %d, duration %v %v)", organizationId, planId, updates["title"], req.TotalAmount, updates["duration_value"], updates["duration_unit"]),
		map[string]any{"organization_id": organizationId, "plan_id": planId, "title": updates["title"], "total_amount": req.TotalAmount,
			"duration_unit": updates["duration_unit"], "duration_value": updates["duration_value"], "custom_seconds": req.CustomSeconds})
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
	if !requireOrganizationSubscriptionPlan(c, organizationId, planId) {
		return
	}
	if err := model.DB.Model(&model.OrganizationSubscriptionPlan{}).
		Where("id = ? AND organization_id = ?", planId, organizationId).
		Update("enabled", false).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.plan.disable",
		fmt.Sprintf("Disabled organization %d subscription plan %d", organizationId, planId),
		map[string]any{"organization_id": organizationId, "plan_id": planId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// requireOrganizationSubscriptionPlan answers 404 (and the caller writes no
// audit row) when planId is not a plan of organizationId. It reads the row
// rather than trusting RowsAffected, which MySQL reports as 0 for an update
// that changes nothing.
func requireOrganizationSubscriptionPlan(c *gin.Context, organizationId int, planId int) bool {
	var count int64
	if err := model.DB.Model(&model.OrganizationSubscriptionPlan{}).
		Where("id = ? AND organization_id = ?", planId, organizationId).
		Count(&count).Error; err != nil {
		common.ApiError(c, err)
		return false
	}
	if count == 0 {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("subscription plan not found"))
		return false
	}
	return true
}

// organizationUserSubscriptionView is the list item organization admins see: the
// user is the restricted organizationUserView, never the full model.User row.
type organizationUserSubscriptionView struct {
	Subscription *model.OrganizationUserSubscription `json:"subscription"`
	User         *organizationUserView               `json:"user"`
	Plan         *model.OrganizationSubscriptionPlan `json:"plan"`
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
	views := make([]organizationUserSubscriptionView, 0, len(items))
	for _, item := range items {
		view := organizationUserSubscriptionView{Subscription: item.Subscription, Plan: item.Plan}
		if item.User != nil {
			userView := newOrganizationUserView(item.User)
			view.User = &userView
		}
		views = append(views, view)
	}
	common.ApiSuccess(c, views)
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
		common.ApiErrorT(c, "organization admin permission required")
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
		common.ApiErrorT(c, "organization target permission denied")
		return
	}
	// Platform root manages any member's plan (the console calls this with
	// ?organization_id=); the self and owner guards apply to organization admins.
	if actor.Role != common.RoleRootUser && (rejectOrganizationSelfSubscription(c, actor, target) || rejectOrganizationOwnerTarget(c, actor, target)) {
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
	recordOrganizationAudit(c, "organization.subscription.assign",
		fmt.Sprintf("Assigned organization %d subscription plan %d to user %d (subscription %d, amount %d)", organizationId, req.PlanId, targetId, sub.Id, sub.AmountTotal),
		map[string]any{"organization_id": organizationId, "user_id": targetId, "plan_id": req.PlanId, "subscription_id": sub.Id, "amount_total": sub.AmountTotal})
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
		common.ApiErrorT(c, "organization admin permission required")
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
	if actor.Role != common.RoleRootUser && rejectOrganizationSelfSubscription(c, actor, target) {
		return
	}
	if actor.Role != common.RoleRootUser && target.OrganizationId == organizationId && rejectOrganizationOwnerTarget(c, actor, target) {
		return
	}
	if _, err := model.CancelActiveOrganizationUserSubscriptionForUser(organizationId, targetId); err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.subscription.cancel",
		fmt.Sprintf("Cancelled the active organization %d subscription of user %d", organizationId, targetId),
		map[string]any{"organization_id": organizationId, "user_id": targetId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// rejectOrganizationSelfSubscription stops an organization admin (the owner
// included) from assigning or cancelling their own plan: a self-assigned
// unlimited plan would bypass the cap the owner set.
func rejectOrganizationSelfSubscription(c *gin.Context, actor *model.User, target *model.User) bool {
	if target.Id == actor.Id {
		common.ApiErrorT(c, "organization admins cannot manage their own subscription")
		return true
	}
	return false
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
	// Same shape as the list items: the caller through organizationUserView,
	// never the full users row.
	userView := newOrganizationUserView(actor)
	userView.OrganizationRemark = "" // the admins' note about the member stays with the admins
	common.ApiSuccess(c, organizationUserSubscriptionView{
		Subscription: sub,
		User:         &userView,
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
	if err := validateInt64Range("total_amount", req.TotalAmount, 0, MaxOrganizationQuota); err != nil {
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
		return nil, common.NewMessage(OrganizationDurationValueRangeError)
	}
	if durationValue > MaxOrganizationSubscriptionDurationValue {
		return nil, common.NewMessage(OrganizationDurationValueRangeError)
	}
	if durationValue == 0 && durationUnit != model.SubscriptionDurationCustom {
		durationValue = 1
	}
	if durationUnit == model.SubscriptionDurationCustom {
		if err := validateInt64Range("custom_seconds", req.CustomSeconds, 1, MaxOrganizationSubscriptionCustomSeconds); err != nil {
			return nil, err
		}
	} else if req.CustomSeconds < 0 || req.CustomSeconds > MaxOrganizationSubscriptionCustomSeconds {
		return nil, common.NewMessage("{{field}} must be between {{min}} and {{max}}", map[string]any{"field": "custom_seconds", "min": 0, "max": MaxOrganizationSubscriptionCustomSeconds})
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
