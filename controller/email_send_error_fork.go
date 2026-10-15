package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O11 — never echo the SMTP error to the user (team 30a3fe6a9).
func apiEmailSendFailed(c *gin.Context, err error) {
	common.SysError(common.LogText("email send failed: %s", err.Error()))
	common.ApiError(c, common.NewMessage("Failed to send email"))
}
