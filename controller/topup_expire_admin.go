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

// AdminExpirePendingTopUps (POST /api/user/topup/expire, admin) marks pending
// personal top-up and subscription orders older than an hour expired — for
// body user_id, or for every user when the body or user_id is omitted — so an
// abandoned checkout no longer refuses the user's organization join. Younger
// orders are counted in skipped_recent / skipped_recent_subscriptions;
// organization top-ups are never touched.
func AdminExpirePendingTopUps(c *gin.Context) {
	var req adminExpirePendingTopUpsRequest
	if err := common.DecodeJson(c.Request.Body, &req); (err != nil && !errors.Is(err, io.EOF)) || req.UserId < 0 {
		common.ApiErrorT(c, "Invalid parameters") // an empty body means every user
		return
	}

	result, err := model.ExpirePendingPersonalTopUps(req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	scope := "all users"
	if req.UserId > 0 {
		scope = fmt.Sprintf("user %d", req.UserId)
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"),
		fmt.Sprintf("Expired %d pending personal top-up orders and %d pending subscription orders of %s (%d and %d recent orders kept)",
			result.ExpiredTopUps, result.ExpiredSubscriptions, scope, result.SkippedRecentTopUps, result.SkippedRecentSubscriptions),
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
