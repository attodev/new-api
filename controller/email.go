package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O5 admin SMTP test mail.
type testEmailRequest struct {
	To string `json:"to"`
}

var sendTestMail = common.SendEmail

func TestEmail(c *gin.Context) {
	var req testEmailRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	req.To = strings.TrimSpace(req.To)
	if req.To == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Recipient address is required"))
		return
	}
	if common.Validate.Var(req.To, "required,email,max=254") != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Recipient address is invalid"))
		return
	}
	subject := fmt.Sprintf("[%s] SMTP test email", common.SystemName)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;padding:24px;"><h2 style="color:#0EA5E9;">SMTP configuration test</h2><p>This is a test email sent from <strong>%s</strong>.</p><p>If you received this message, your SMTP settings are working correctly.</p></body></html>`, common.SystemName)
	if err := sendTestMail(subject, req.To, body); err != nil {
		common.SysError(fmt.Sprintf("test email send failed: %v", err))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Failed to send the test email"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
