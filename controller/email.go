package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

type testEmailRequest struct {
	To string `json:"to"`
}

func TestEmail(c *gin.Context) {
	var req testEmailRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid request"})
		return
	}

	if req.To == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Recipient email is required"})
		return
	}

	subject := fmt.Sprintf("[%s] SMTP test email", common.SystemName)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;padding:24px;">
<h2 style="color:#0EA5E9;">SMTP configuration test</h2>
<p>This is a test email sent from <strong>%s</strong>.</p>
<p>If you received this message, your SMTP settings are working correctly.</p>
</body></html>`, common.SystemName)

	if err := common.SendEmail(subject, req.To, body); err != nil {
		common.SysError(fmt.Sprintf("test email send failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
