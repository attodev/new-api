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
}

type updateOrganizationUserRequest struct {
	Status *int    `json:"status,omitempty"`
	Quota  *int    `json:"quota,omitempty"`
	Remark *string `json:"remark,omitempty"`
}

type assignOrganizationUserRequest struct {
	OrganizationRole string `json:"organization_role"`
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

	org, err := model.CreateOrganization(req.Name, req.Description, req.OwnerUserId)
	if err != nil {
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
