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
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

type createOrganizationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerUserId int    `json:"owner_user_id"`
	Quota       *int   `json:"quota,omitempty"`
}

type updateOrganizationUserRequest struct {
	Status *int    `json:"status,omitempty"`
	Quota  *int    `json:"quota,omitempty"`
	Remark *string `json:"remark,omitempty"`
}

type assignOrganizationUserRequest struct {
	OrganizationRole string `json:"organization_role"`
}

type updateOrganizationRequest struct {
	Description *string `json:"description,omitempty"`
	Quota       *int    `json:"quota,omitempty"`
	Status      *int    `json:"status,omitempty"`
}

func CreateOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgOrgRootRequired)
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
	initialQuota := 0
	if req.Quota != nil {
		if err := validateIntRange("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
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
	common.ApiSuccess(c, org)
}

func ListOrganizations(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgOrgRootRequired)
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
		common.ApiErrorI18n(c, i18n.MsgOrgRootRequired)
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
		if err := validateIntRange("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
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
		common.ApiErrorI18n(c, i18n.MsgOrgNoFieldsToUpdate)
		return
	}

	if err := model.UpdateOrganizationFieldsWithBillingLifecycle(organizationId, updates); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func GetOrganizationProfile(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
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
			common.ApiErrorI18n(c, i18n.MsgOrgInvalidStartTs)
			return
		}
	}
	var endTimestamp int64
	endTimestampStr, hasEndTimestamp := c.GetQuery("end_timestamp")
	if hasEndTimestamp {
		endTimestamp, err = strconv.ParseInt(endTimestampStr, 10, 64)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgOrgInvalidEndTs)
			return
		}
	}

	preset := strings.TrimSpace(c.Query("preset"))
	if preset != "" && preset != "custom" {
		if preset != "today" && preset != "7d" && preset != "30d" {
			common.ApiErrorI18n(c, i18n.MsgOrgInvalidPreset)
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
		common.ApiErrorI18n(c, i18n.MsgOrgTimestampOrder)
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

	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := model.GetLogsByUserIDs(userIDs, logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId)
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

	pageInfo := common.GetPageQuery(c)
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
		UserIDs:        userIDs,
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

	pageInfo := common.GetPageQuery(c)
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
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
	pageInfo.SetItems(tasksToDto(items, true))
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
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestAmount(c)
}

func RequestOrganizationEpay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestEpay(c)
}

func RequestOrganizationStripeAmount(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestStripeAmount(c)
}

func RequestOrganizationStripePay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestStripePay(c)
}

func RequestOrganizationPayPalAmount(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestPayPalAmount(c)
}

func RequestOrganizationPayPalPay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestPayPalPay(c)
}

func RequestOrganizationCreemPay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestCreemPay(c)
}

func RequestOrganizationWaffoAmount(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestWaffoAmount(c)
}

func RequestOrganizationWaffoPay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestWaffoPay(c)
}

func RequestOrganizationWaffoPancakeAmount(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestWaffoPancakeAmount(c)
}

func RequestOrganizationWaffoPancakePay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestWaffoPancakePay(c)
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
			common.ApiErrorI18n(c, i18n.MsgOrgIdRequired)
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
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
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
			common.ApiErrorI18n(c, i18n.MsgOrgIdRequired)
			return nil, nil, false
		}
		organizationId = parsedOrganizationId
		var org model.Organization
		if err := model.DB.First(&org, organizationId).Error; err != nil {
			common.ApiError(c, err)
			return nil, nil, false
		}
	} else if organizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
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

	query := model.DB.
		Where("organization_id = ?", organizationId).
		Where("role < ?", common.RoleAdminUser)

	if keyword != "" {
		query = query.Where("username LIKE ? OR display_name LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%")
	}

	var total int64
	if err := query.Model(&model.User{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var users []model.User
	if err := query.Order(orderBy + " " + orderDir).
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
		common.ApiErrorI18n(c, i18n.MsgOrgAdminRequired)
		return
	}

	pageInfo := common.GetPageQuery(c)
	var users []model.User
	query := model.DB.
		Where("role < ?", common.RoleAdminUser).
		Where("id <> ?", actor.Id).
		Where("organization_id = ? OR organization_id = 0", actor.OrganizationId)

	var total int64
	if err := query.Model(&model.User{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&users).Error; err != nil {
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
		common.ApiErrorI18n(c, i18n.MsgOrgTargetPermDenied)
		return
	}
	common.ApiSuccess(c, target)
}

func UpdateOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if !model.CanManageOrganizationTarget(*actor, *target) {
		common.ApiErrorI18n(c, i18n.MsgOrgTargetPermDenied)
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
		if err := validateIntRange("quota", *req.Quota, 0, MaxOrganizationQuota); err != nil {
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
		common.ApiErrorI18n(c, i18n.MsgOrgNoUserFieldsToUpdate)
		return
	}

	if err := model.UpdateUserFieldsWithBillingLifecycle(target.Id, updates); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization user cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func AssignOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if target.Role >= common.RoleAdminUser {
		common.ApiErrorI18n(c, i18n.MsgOrgGlobalAdminUnmanageable)
		return
	}
	if target.Id == actor.Id {
		common.ApiErrorI18n(c, i18n.MsgOrgCannotReassignSelf)
		return
	}

	var req assignOrganizationUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.IsValidOrganizationRole(req.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgInvalidRole)
		return
	}
	if model.HasOrganizationOwnerRole(req.OrganizationRole) && !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgOnlyOwnerCanAssignOwner)
		return
	}

	if err := model.AssignUserToOrganizationWithBillingLifecycle(target.Id, actor.OrganizationId, req.OrganizationRole); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization membership cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func RemoveOrganizationUserMembership(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
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
	if target.Role >= common.RoleAdminUser {
		common.ApiErrorI18n(c, i18n.MsgOrgGlobalAdminUnmanageable)
		return
	}
	if target.OrganizationId != actor.OrganizationId {
		common.ApiErrorI18n(c, i18n.MsgOrgUserNotInOrg)
		return
	}
	if model.HasOrganizationOwnerRole(target.OrganizationRole) {
		common.ApiErrorI18n(c, i18n.MsgOrgOwnerCannotBeRemoved)
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(map[string]interface{}{
		"organization_id":   0,
		"organization_role": "",
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate cache after membership removal: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorI18n(c, i18n.MsgOrgRootRequired)
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.DeleteOrganization(organizationId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgOrgNotFound)})
			return
		}
		common.ApiError(c, err)
		return
	}

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
	result, err := service.ImportOrgUsersFromFile(f, organizationId, removeAbsent)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
