package controller

import (
	"errors"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// fork: O3 — admin expiry of abandoned personal top-up orders.

type adminExpirePendingTopUpsRequest struct {
	UserId int `json:"user_id"`
}

// AdminExpirePendingTopUps (POST /api/user/topup/expire, admin) marks pending
// personal top-up orders older than an hour expired — for body user_id, or for
// every user when the body or user_id is omitted — so an abandoned checkout no
// longer refuses the user's organization join. Younger orders are counted in
// skipped_recent; organization orders are never touched.
func AdminExpirePendingTopUps(c *gin.Context) {
	var req adminExpirePendingTopUpsRequest
	if err := common.DecodeJson(c.Request.Body, &req); (err != nil && !errors.Is(err, io.EOF)) || req.UserId < 0 {
		common.ApiErrorT(c, "Invalid parameters") // an empty body means every user
		return
	}

	expired, skippedRecent, err := model.ExpirePendingPersonalTopUps(req.UserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	scope := "all users"
	if req.UserId > 0 {
		scope = fmt.Sprintf("user %d", req.UserId)
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"),
		fmt.Sprintf("Expired %d pending personal top-up orders of %s (%d recent orders kept)", expired, scope, skippedRecent),
		c.ClientIP(), "topup.expire",
		map[string]any{"user_id": req.UserId, "expired": expired, "skipped_recent": skippedRecent},
		auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
	common.ApiSuccess(c, gin.H{"expired": expired, "skipped_recent": skippedRecent})
}
