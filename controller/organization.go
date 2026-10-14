package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type createOrganizationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerUserId int    `json:"owner_user_id"`
	Quota       *int64 `json:"quota,omitempty"`
}

type updateOrganizationUserRequest struct {
	Status *int    `json:"status,omitempty"`
	Quota  *int64  `json:"quota,omitempty"`
	Remark *string `json:"remark,omitempty"`
}

type assignOrganizationUserRequest struct {
	OrganizationRole string `json:"organization_role"`
}

type updateOrganizationRequest struct {
	Description *string `json:"description,omitempty"`
	Quota       *int64  `json:"quota,omitempty"`
	Status      *int    `json:"status,omitempty"`
}

// organizationUserView is the user shape organization admins see (the web
// OrganizationUser type). It never carries password, access token, email,
// payment-customer or setting columns.
type organizationUserView struct {
	Id               int    `json:"id"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Role             int    `json:"role"`
	Status           int    `json:"status"`
	Quota            int    `json:"quota"`
	UsedQuota        int    `json:"used_quota"`
	Group            string `json:"group"`
	OrganizationId   int    `json:"organization_id"`
	OrganizationRole string `json:"organization_role"`
	Remark           string `json:"remark"`
}

var organizationUserViewColumns = []string{
	"id", "username", "display_name", "role", "status", "quota", "used_quota",
	"group", "organization_id", "organization_role", "remark",
}

func newOrganizationUserView(user *model.User) organizationUserView {
	return organizationUserView{
		Id: user.Id, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role,
		Status: user.Status, Quota: user.Quota, UsedQuota: user.UsedQuota, Group: user.Group,
		OrganizationId: user.OrganizationId, OrganizationRole: user.OrganizationRole, Remark: user.Remark,
	}
}

// rejectOrganizationSelfOrOwnerTarget enforces two rules for member mutations:
// an organization admin (the owner included) cannot modify their own
// membership, quota or status here (root does that through /api/organizations),
// and only the owner may act on the member who holds the owner role.
func rejectOrganizationSelfOrOwnerTarget(c *gin.Context, actor *model.User, target *model.User) bool {
	if target.Id == actor.Id {
		common.ApiErrorT(c, "organization admins cannot modify their own membership")
		return true
	}
	return rejectOrganizationOwnerTarget(c, actor, target)
}

// rejectOrganizationOwnerTarget rejects acting on the owner unless the actor is
// that owner.
func rejectOrganizationOwnerTarget(c *gin.Context, actor *model.User, target *model.User) bool {
	if model.HasOrganizationOwnerRole(target.OrganizationRole) && target.Id != actor.Id {
		common.ApiErrorT(c, "only the organization owner can manage the owner")
		return true
	}
	return false
}

func CreateOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorT(c, "root permission required")
		return
	}

	var req createOrganizationRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	var err error
	req.Name, err = trimAndValidateText("name", req.Name, MaxOrganizationNameLength, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	req.Description, err = trimAndValidateText("description", req.Description, MaxOrganizationDescriptionLength, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validatePositiveReferenceId("owner_user_id", req.OwnerUserId); err != nil {
		common.ApiError(c, err)
		return
	}
	var initialQuota int64
	if req.Quota != nil {
		if err := validateInt64Range("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
			common.ApiError(c, err)
			return
		}
		initialQuota = *req.Quota
	}
	org, err := model.CreateOrganization(req.Name, req.Description, req.OwnerUserId, initialQuota)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.create",
		fmt.Sprintf("Created organization %q (owner %d, quota %d)", org.Name, req.OwnerUserId, initialQuota),
		map[string]any{"organization_id": org.Id, "name": org.Name, "owner_user_id": req.OwnerUserId, "quota": initialQuota})
	common.ApiSuccess(c, org)
}

func ListOrganizations(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorT(c, "root permission required")
		return
	}

	pageInfo := common.GetPageQuery(c)
	var organizations []model.Organization
	query := model.DB.Model(&model.Organization{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Order("id desc").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&organizations).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(organizations)
	common.ApiSuccess(c, pageInfo)
}

func UpdateOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorT(c, "root permission required")
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var req updateOrganizationRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	updates := map[string]interface{}{}
	if req.Description != nil {
		description, err := trimAndValidateText("description", *req.Description, MaxOrganizationDescriptionLength, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		updates["description"] = description
	}
	if req.Quota != nil {
		if err := validateInt64Range("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["quota"] = *req.Quota
	}
	if req.Status != nil {
		if err := validateOrganizationStatus(*req.Status); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["status"] = *req.Status
	}
	if len(updates) == 0 {
		common.ApiErrorT(c, "no organization fields to update")
		return
	}

	if err := model.UpdateOrganizationFields(organizationId, updates); err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.update",
		fmt.Sprintf("Updated organization %d (%s)", organizationId, formatOrganizationAuditFields(updates)),
		organizationAuditParams(map[string]any{"organization_id": organizationId}, updates))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetOrganizationProfile(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorT(c, "organization admin permission required")
		return
	}

	var org model.Organization
	if err := model.DB.First(&org, actor.OrganizationId).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, org)
}

func GetOrganizationDashboard(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	var err error
	var startTimestamp int64
	startTimestampStr, hasStartTimestamp := c.GetQuery("start_timestamp")
	if hasStartTimestamp {
		startTimestamp, err = strconv.ParseInt(startTimestampStr, 10, 64)
		if err != nil {
			common.ApiErrorT(c, "invalid start_timestamp")
			return
		}
	}
	var endTimestamp int64
	endTimestampStr, hasEndTimestamp := c.GetQuery("end_timestamp")
	if hasEndTimestamp {
		endTimestamp, err = strconv.ParseInt(endTimestampStr, 10, 64)
		if err != nil {
			common.ApiErrorT(c, "invalid end_timestamp")
			return
		}
	}

	preset := strings.TrimSpace(c.Query("preset"))
	if preset != "" && preset != "custom" {
		if preset != "today" && preset != "7d" && preset != "30d" {
			common.ApiErrorT(c, "invalid preset")
			return
		}
		if !(hasStartTimestamp && hasEndTimestamp) {
			now := time.Now()
			endTimestamp = now.Unix()
			switch preset {
			case "today":
				startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
				startTimestamp = startOfDay.Unix()
			case "7d":
				startTimestamp = now.AddDate(0, 0, -7).Unix()
			case "30d":
				startTimestamp = now.AddDate(0, 0, -30).Unix()
			}
		}
	}

	if startTimestamp > 0 && endTimestamp > 0 && startTimestamp > endTimestamp {
		common.ApiErrorT(c, "start_timestamp must be before end_timestamp")
		return
	}

	dashboard, err := model.GetOrganizationDashboard(organizationId, startTimestamp, endTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, dashboard)
}

func GetOrganizationLogs(c *gin.Context) {
	_, userIDs, ok := getOrganizationLogActorAndUserIDs(c)
	if !ok {
		return
	}

	filter := logFilterFromQuery(c, true)
	startTimestamp, ok := limitOrganizationLogTimeRange(c, filter.StartTimestamp, filter.EndTimestamp, 1)
	if !ok {
		return
	}
	filter.StartTimestamp = startTimestamp

	pageInfo := common.GetPageQuery(c)
	logs, total, err := model.GetLogsByUserIDs(userIDs, filter, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetOrganizationMidjourney(c *gin.Context) {
	_, userIDs, ok := getOrganizationLogActorAndUserIDs(c)
	if !ok {
		return
	}

	// Midjourney submit_time is in milliseconds.
	startMs, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endMs, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	startMs, ok = limitOrganizationLogTimeRange(c, startMs, endMs, 1000)
	if !ok {
		return
	}

	pageInfo := common.GetPageQuery(c)
	if len(userIDs) == 0 { // fork: O3 — an empty user filter would match every user's tasks
		pageInfo.SetItems([]*model.Midjourney{})
		common.ApiSuccess(c, pageInfo)
		return
	}
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
		UserIDs:        userIDs,
	}
	if startMs > 0 {
		queryParams.StartTimestamp = strconv.FormatInt(startMs, 10)
	}

	items := model.GetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.CountAllTasks(queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetOrganizationTask(c *gin.Context) {
	_, userIDs, ok := getOrganizationLogActorAndUserIDs(c)
	if !ok {
		return
	}

	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	startTimestamp, ok = limitOrganizationLogTimeRange(c, startTimestamp, endTimestamp, 1)
	if !ok {
		return
	}

	pageInfo := common.GetPageQuery(c)
	if len(userIDs) == 0 { // fork: O3 — an empty user filter would match every user's tasks
		pageInfo.SetItems([]any{})
		common.ApiSuccess(c, pageInfo)
		return
	}
	queryParams := model.SyncTaskQueryParams{
		Platform:       constant.TaskPlatform(c.Query("platform")),
		TaskID:         c.Query("task_id"),
		Status:         c.Query("status"),
		Action:         c.Query("action"),
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
		ChannelID:      c.Query("channel_id"),
		UserIDs:        userIDs,
	}

	items := model.TaskGetAllTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	total := model.TaskCountAllTasks(queryParams)
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasksToDto(items, true, c.GetInt("role")))
	common.ApiSuccess(c, pageInfo)
}

func GetOrganizationWallet(c *gin.Context) {
	_, org, ok := prepareOrganizationTopUpTarget(c)
	if !ok {
		return
	}
	common.ApiSuccess(c, org)
}

func GetOrganizationTopUps(c *gin.Context) {
	_, org, ok := prepareOrganizationTopUpTarget(c)
	if !ok {
		return
	}

	pageInfo := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchOrganizationTopUps(org.Id, keyword, pageInfo)
	} else {
		topups, total, err = model.GetOrganizationTopUps(org.Id, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

func RequestOrganizationAmount(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestAmount(c)
}

func RequestOrganizationEpay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestEpay(c)
}

func RequestOrganizationStripeAmount(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestStripeAmount(c)
}

func RequestOrganizationStripePay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestStripePay(c)
}

func RequestOrganizationPayPalAmount(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestPayPalAmount(c)
}

func RequestOrganizationPayPalPay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestPayPalPay(c)
}

func RequestOrganizationCreemPay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestCreemPay(c)
}

func RequestOrganizationWaffoAmount(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestWaffoAmount(c)
}

func RequestOrganizationWaffoPay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestWaffoPay(c)
}

func RequestOrganizationWaffoPancakeAmount(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestWaffoPancakeAmount(c)
}

func RequestOrganizationWaffoPancakePay(c *gin.Context) {
	if !prepareOrganizationPaymentTarget(c) {
		return
	}
	RequestWaffoPancakePay(c)
}

// limitOrganizationLogTimeRange applies upstream's non-admin log range limit
// (limitLogTimeRange, as in GetUserLogs) to organization log searches:
// organization admins are ordinary users. Platform admins are not limited.
// unit is 1 for second timestamps and 1000 for Midjourney's milliseconds.
func limitOrganizationLogTimeRange(c *gin.Context, start int64, end int64, unit int64) (int64, bool) {
	if c.GetInt("role") >= common.RoleAdminUser {
		return start, true
	}
	limited, err := limitLogTimeRange(start/unit, end/unit)
	if err != nil {
		common.ApiError(c, err)
		return 0, false
	}
	if start != 0 && limited == start/unit {
		return start, true
	}
	return limited * unit, true
}

func getOrganizationActor(c *gin.Context) (*model.User, error) {
	return model.GetUserById(c.GetInt("id"), false)
}

func resolveOrganizationAdminTarget(c *gin.Context) (int, bool) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return 0, false
	}

	if actor.Role == common.RoleRootUser {
		organizationId, err := strconv.Atoi(strings.TrimSpace(c.Query("organization_id")))
		if err != nil || organizationId <= 0 {
			common.ApiErrorT(c, "organization_id is required")
			return 0, false
		}
		var org model.Organization
		if err := model.DB.First(&org, organizationId).Error; err != nil {
			common.ApiError(c, err)
			return 0, false
		}
		return organizationId, true
	}

	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorT(c, "organization admin permission required")
		return 0, false
	}
	return actor.OrganizationId, true
}

func getOrganizationLogActorAndUserIDs(c *gin.Context) (*model.User, []int, bool) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return nil, nil, false
	}

	organizationId := actor.OrganizationId
	if actor.Role == common.RoleRootUser {
		parsedOrganizationId, err := strconv.Atoi(strings.TrimSpace(c.Query("organization_id")))
		if err != nil || parsedOrganizationId <= 0 {
			common.ApiErrorT(c, "organization_id is required")
			return nil, nil, false
		}
		organizationId = parsedOrganizationId
		var org model.Organization
		if err := model.DB.First(&org, organizationId).Error; err != nil {
			common.ApiError(c, err)
			return nil, nil, false
		}
	} else if organizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorT(c, "organization admin permission required")
		return nil, nil, false
	}

	var users []model.User
	if err := model.DB.Select("id").Where("organization_id = ?", organizationId).Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return nil, nil, false
	}
	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.Id)
	}
	return actor, userIDs, true
}

func ListOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	pageInfo := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	orderBy := c.Query("order_by")
	orderDir := c.Query("order_dir")

	allowedOrderBy := map[string]bool{
		"username": true, "display_name": true, "organization_role": true,
		"quota": true, "used_quota": true, "group": true, "status": true,
	}
	if !allowedOrderBy[orderBy] {
		orderBy = "id"
	}
	if orderDir != "desc" {
		orderDir = "asc"
	}

	query := model.DB.Model(&model.User{}).
		Where("organization_id = ?", organizationId).
		Where("role < ?", common.RoleAdminUser)

	if keyword != "" {
		query = query.Where("username LIKE ? OR display_name LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%")
	}

	query = query.Session(&gorm.Session{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	users := []organizationUserView{}
	if err := query.Select(organizationUserViewColumns).
		Order(clause.OrderByColumn{Column: clause.Column{Name: orderBy}, Desc: orderDir == "desc"}).
		Offset(pageInfo.GetStartIdx()).
		Limit(pageInfo.GetPageSize()).
		Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

func ListAssignableOrganizationUsers(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorT(c, "organization admin permission required")
		return
	}

	pageInfo := common.GetPageQuery(c)
	users := []organizationUserView{}
	query := model.DB.Model(&model.User{}).
		Where("role < ?", common.RoleAdminUser).
		Where("id <> ?", actor.Id).
		Where("organization_id = ? OR organization_id = 0", actor.OrganizationId).
		Session(&gorm.Session{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Select(organizationUserViewColumns).Order("id").Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

func GetOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
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
	if !model.CanManageOrganizationTarget(*actor, *target) {
		common.ApiErrorT(c, "organization target permission denied")
		return
	}
	common.ApiSuccess(c, newOrganizationUserView(target))
}

func UpdateOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if !model.CanManageOrganizationTarget(*actor, *target) {
		common.ApiErrorT(c, "organization target permission denied")
		return
	}
	if rejectOrganizationSelfOrOwnerTarget(c, actor, target) {
		return
	}

	var req updateOrganizationUserRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		if err := validateUserStatus(*req.Status); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["status"] = *req.Status
	}
	if req.Quota != nil {
		if err := validateInt64Range("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["quota"] = *req.Quota
	}
	if req.Remark != nil {
		remark, err := trimAndValidateText("remark", *req.Remark, MaxOrganizationRemarkLength, false)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		updates["remark"] = remark
	}
	if len(updates) == 0 {
		common.ApiErrorT(c, "no organization user fields to update")
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization user cache: " + err.Error())
	}
	recordOrganizationAudit(c, "organization.user.update",
		fmt.Sprintf("Updated organization %d member %d (%s)", actor.OrganizationId, target.Id, formatOrganizationAuditFields(updates)),
		organizationAuditParams(map[string]any{"organization_id": actor.OrganizationId, "user_id": target.Id}, updates))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func AssignOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if target.Role >= common.RoleAdminUser {
		common.ApiErrorT(c, "global admin users cannot be managed by organization admins")
		return
	}
	if target.Id == actor.Id {
		common.ApiErrorT(c, "organization admins cannot reassign themselves")
		return
	}
	if rejectOrganizationOwnerTarget(c, actor, target) {
		return
	}

	var req assignOrganizationUserRequest
	if err := common.DecodeJsonStrict(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.IsValidOrganizationRole(req.OrganizationRole) {
		common.ApiErrorT(c, "invalid organization role")
		return
	}
	if model.HasOrganizationOwnerRole(req.OrganizationRole) {
		common.ApiErrorT(c, "ownership is assigned when the organization is created")
		return
	}

	if err := model.AssignUserToOrganization(target.Id, actor.OrganizationId, req.OrganizationRole); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization membership cache: " + err.Error())
	}
	recordOrganizationAudit(c, "organization.user.assign",
		fmt.Sprintf("Assigned user %d to organization %d as %s (previous organization %d)", target.Id, actor.OrganizationId, req.OrganizationRole, target.OrganizationId),
		map[string]any{"organization_id": actor.OrganizationId, "user_id": target.Id, "organization_role": req.OrganizationRole, "previous_organization_id": target.OrganizationId})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func RemoveOrganizationUserMembership(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if target.Role >= common.RoleAdminUser {
		common.ApiErrorT(c, "global admin users cannot be managed by organization admins")
		return
	}
	if target.Id == actor.Id {
		common.ApiErrorT(c, "organization admins cannot modify their own membership")
		return
	}
	if target.OrganizationId != actor.OrganizationId {
		common.ApiErrorT(c, "user does not belong to your organization")
		return
	}
	if model.HasOrganizationOwnerRole(target.OrganizationRole) {
		common.ApiErrorT(c, "organization owner cannot be removed")
		return
	}

	previousQuota, err := model.ClearOrganizationMembership(target.Id, actor.OrganizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate cache after membership removal: " + err.Error())
	}
	recordOrganizationAudit(c, "organization.user.remove",
		fmt.Sprintf("Removed user %d (role %s) from organization %d and cleared member quota %d", target.Id, target.OrganizationRole, actor.OrganizationId, previousQuota),
		map[string]any{"organization_id": actor.OrganizationId, "user_id": target.Id, "organization_role": target.OrganizationRole, "previous_quota": previousQuota})
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorT(c, "root permission required")
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ownerUserId, ownerPreviousQuota, err := model.DeleteOrganizationClearingOwner(organizationId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "organization not found"})
			return
		}
		common.ApiError(c, err)
		return
	}

	if ownerUserId > 0 {
		if err := model.InvalidateUserCache(ownerUserId); err != nil {
			common.SysLog("failed to invalidate owner cache after organization deletion: " + err.Error())
		}
	}
	recordOrganizationAudit(c, "organization.delete",
		fmt.Sprintf("Deleted organization %d (owner %d, cleared member quota %d)", organizationId, ownerUserId, ownerPreviousQuota),
		map[string]any{"organization_id": organizationId, "owner_user_id": ownerUserId, "previous_quota": ownerPreviousQuota})
	common.ApiSuccess(c, nil)
}

func ExportOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	f, err := service.BuildOrgExportFile(organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer f.Close()

	filename := fmt.Sprintf("org-users-%d-%d.xlsx", organizationId, time.Now().Unix())
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	if err := f.Write(c.Writer); err != nil {
		common.ApiError(c, err)
	}
}

func ImportOrganizationUsers(c *gin.Context) {
	organizationId, ok := resolveOrganizationAdminTarget(c)
	if !ok {
		return
	}

	const maxSize = 10 << 20
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to read file: %w", err))
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid xlsx file: %w", err))
		return
	}
	defer f.Close()

	removeAbsent := c.PostForm("remove_absent") == "true"
	result, err := service.ImportOrgUsersFromFile(f, organizationId, c.GetInt("id"), removeAbsent)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.user.import",
		fmt.Sprintf("Imported organization %d members from Excel (assigned %d, created %d, updated %d, removed %d, errors %d)",
			organizationId, result.Assigned, result.Created, result.Updated, result.Removed, len(result.Errors)),
		map[string]any{"organization_id": organizationId, "remove_absent": removeAbsent, "assigned": result.Assigned, "created": result.Created,
			"updated": result.Updated, "removed": result.Removed, "errors": len(result.Errors)})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
