package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// fork: O3 — platform (root) administration of organization top-up orders.

// ExpireOrganizationTopUps (POST /api/organizations/:id/topups/expire) marks
// the organization's abandoned pending top-up orders expired so they no longer
// block DeleteOrganization (orders younger than a day are kept unless the
// organization is disabled; expiring a disabled organization's checkout that
// is still paid drops that payment). Settled orders are not touched.
func ExpireOrganizationTopUps(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiErrorT(c, "root permission required")
		return
	}
	organizationId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var organization model.Organization
	if err := model.DB.Select("id").Where("id = ?", organizationId).First(&organization).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("organization not found"))
			return
		}
		common.ApiError(c, err)
		return
	}

	expired, skippedRecent, err := model.ExpirePendingOrganizationTopUps(organizationId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordOrganizationAudit(c, "organization.topup.expire",
		fmt.Sprintf("Expired %d pending top-up orders of organization %d (%d recent orders kept)", expired, organizationId, skippedRecent),
		map[string]any{"organization_id": organizationId, "expired": expired, "skipped_recent": skippedRecent})
	common.ApiSuccess(c, gin.H{"expired": expired, "skipped_recent": skippedRecent})
}
