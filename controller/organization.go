package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
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

func getOrganizationActor(c *gin.Context) (*model.User, error) {
	return model.GetUserById(c.GetInt("id"), false)
}

func ListOrganizationUsers(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiError(c, errors.New("organization admin permission required"))
		return
	}

	pageInfo := common.GetPageQuery(c)
	var users []model.User
	query := model.DB.
		Where("organization_id = ?", actor.OrganizationId).
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
