package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — organization admins add members by exact username or email
// (spec §9 decision 6). There is no listing of unaffiliated users, and the
// lookup returns identity only: no quota, used quota or remark.

type organizationMemberLookupView struct {
	Id             int    `json:"id"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	OrganizationId int    `json:"organization_id"`
}

// LookupOrganizationMember handles GET /api/organization/users/lookup with
// exactly one of ?username= or ?email=. It finds an unaffiliated user or a
// member of the caller's organization; anything else is 404.
func LookupOrganizationMember(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiErrorT(c, "organization admin permission required")
		return
	}

	username := strings.TrimSpace(c.Query("username"))
	email := model.NormalizeEmail(c.Query("email"))
	if (username == "") == (email == "") {
		common.ApiErrorT(c, "provide either a username or an email")
		return
	}

	query := model.DB.Model(&model.User{}).
		Where("role < ?", common.RoleAdminUser).
		Where("organization_id = ? OR organization_id = 0", actor.OrganizationId)
	if username != "" {
		query = query.Where("username = ?", username)
	} else {
		query = query.Where("LOWER(email) = ?", email)
	}
	var users []organizationMemberLookupView
	if err := query.Select("id", "username", "display_name", "organization_id").
		Order("id").Limit(2).Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if len(users) != 1 {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("user not found"))
		return
	}
	common.ApiSuccess(c, users[0])
}
