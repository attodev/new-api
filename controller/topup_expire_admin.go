package controller

import (
	"errors"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — admin expiry of abandoned personal orders.

type adminExpirePendingTopUpsRequest struct {
	UserId int `json:"user_id"`
}

// AdminExpirePendingTopUps (POST /api/user/topup/expire, admin) marks the
// body user_id's pending personal top-up and subscription orders older than a
// day expired, so an abandoned checkout no longer refuses the user's
// organization join. user_id is required: there is no all-users sweep. Younger
// orders are counted in skipped_recent / skipped_recent_subscriptions;
// organization top-ups are never touched.
func AdminExpirePendingTopUps(c *gin.Context) {
	var req adminExpirePendingTopUpsRequest
	if err := common.DecodeJson(c.Request.Body, &req); (err != nil && !errors.Is(err, io.EOF)) || req.UserId < 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if req.UserId == 0 {
		common.ApiErrorT(c, "user_id is required")
		return
	}

	result, err := model.ExpirePendingPersonalTopUps(req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"),
		fmt.Sprintf("Expired %d pending personal top-up orders and %d pending subscription orders of user %d (%d and %d recent orders kept)",
			result.ExpiredTopUps, result.ExpiredSubscriptions, req.UserId, result.SkippedRecentTopUps, result.SkippedRecentSubscriptions),
		c.ClientIP(), "topup.expire",
		map[string]any{
			"user_id":                      req.UserId,
			"expired":                      result.ExpiredTopUps,
			"skipped_recent":               result.SkippedRecentTopUps,
			"expired_subscriptions":        result.ExpiredSubscriptions,
			"skipped_recent_subscriptions": result.SkippedRecentSubscriptions,
		},
		auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
	common.ApiSuccess(c, result)
}
