package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Quota       *int    `json:"quota,omitempty"`
	Status      *int    `json:"status,omitempty"`
}

func CreateOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiError(c, errors.New("root permission required"))
		return
	}

	var req createOrganizationRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	initialQuota := 0
	if req.Quota != nil {
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
		common.ApiError(c, errors.New("root permission required"))
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
		common.ApiError(c, errors.New("root permission required"))
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var req updateOrganizationRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	updates := map[string]interface{}{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			common.ApiError(c, errors.New("organization name cannot be empty"))
			return
		}
		updates["name"] = name
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Quota != nil {
		if *req.Quota < 0 {
			common.ApiError(c, errors.New("quota cannot be negative"))
			return
		}
		updates["quota"] = *req.Quota
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if len(updates) == 0 {
		common.ApiError(c, errors.New("no organization fields to update"))
		return
	}

	if err := model.DB.Model(&model.Organization{}).Where("id = ?", organizationId).Updates(updates).Error; err != nil {
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
		common.ApiError(c, errors.New("organization admin permission required"))
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
			common.ApiError(c, errors.New("invalid start_timestamp"))
			return
		}
	}
	var endTimestamp int64
	endTimestampStr, hasEndTimestamp := c.GetQuery("end_timestamp")
	if hasEndTimestamp {
		endTimestamp, err = strconv.ParseInt(endTimestampStr, 10, 64)
		if err != nil {
			common.ApiError(c, errors.New("invalid end_timestamp"))
			return
		}
	}

	preset := strings.TrimSpace(c.Query("preset"))
	if preset != "" && preset != "custom" {
		if preset != "today" && preset != "7d" && preset != "30d" {
			common.ApiError(c, errors.New("invalid preset"))
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
		common.ApiError(c, errors.New("start_timestamp must be before end_timestamp"))
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
			common.ApiError(c, errors.New("organization_id is required"))
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
		common.ApiError(c, errors.New("organization admin permission required"))
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
			common.ApiError(c, errors.New("organization_id is required"))
			return nil, nil, false
		}
		organizationId = parsedOrganizationId
		var org model.Organization
		if err := model.DB.First(&org, organizationId).Error; err != nil {
			common.ApiError(c, err)
			return nil, nil, false
		}
	} else if organizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization admin permission required"))
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
	var users []model.User
	query := model.DB.
		Where("organization_id = ?", organizationId).
		Where("role < ?", common.RoleAdminUser)

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

func ListAssignableOrganizationUsers(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization owner permission required"))
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
		common.ApiError(c, errors.New("organization target permission denied"))
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
		common.ApiError(c, errors.New("organization admin permission required"))
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
		common.ApiError(c, errors.New("organization target permission denied"))
		return
	}

	var req updateOrganizationUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Quota != nil {
		updates["quota"] = *req.Quota
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	if len(updates) == 0 {
		common.ApiError(c, errors.New("no organization user fields to update"))
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(updates).Error; err != nil {
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
	if actor.OrganizationId == 0 || !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization owner permission required"))
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
		common.ApiError(c, errors.New("global admin users cannot be managed by organization owners"))
		return
	}
	if target.Id == actor.Id {
		common.ApiError(c, errors.New("organization owners cannot reassign themselves"))
		return
	}

	var req assignOrganizationUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.IsValidOrganizationRole(req.OrganizationRole) {
		common.ApiError(c, errors.New("invalid organization role"))
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(map[string]interface{}{
		"organization_id":   actor.OrganizationId,
		"organization_role": req.OrganizationRole,
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization membership cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func DeleteOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiError(c, errors.New("root permission required"))
		return
	}

	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if err := model.DeleteOrganization(organizationId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "organization not found"})
			return
		}
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, nil)
}
