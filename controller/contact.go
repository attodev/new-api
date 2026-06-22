package controller

import (
	"fmt"
	"html"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
)

type contactRequest struct {
	Org     string `json:"org"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Message string `json:"message"`
}

func Contact(c *gin.Context) {
	var req contactRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgContactInvalidRequest)
		return
	}

	if req.Org == "" || req.Email == "" || req.Message == "" {
		common.ApiErrorI18n(c, i18n.MsgContactRequiredFields)
		return
	}

	if common.ContactEmail == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "contact recipient email is not configured"})
		return
	}

	subject := fmt.Sprintf("[%s] Enterprise Inquiry — %s", common.SystemName, req.Org)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;">
<h2 style="color:#0EA5E9;">Enterprise Inquiry</h2>
<table style="border-collapse:collapse;width:100%%;max-width:560px;">
<tr><td style="padding:8px 12px;font-weight:700;width:120px;background:#f3f4f6;">Company / Organization</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">Email</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">Phone</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">Inquiry</td><td style="padding:8px 12px;white-space:pre-wrap;">%s</td></tr>
</table>
</body></html>`,
		html.EscapeString(req.Org),
		html.EscapeString(req.Email),
		html.EscapeString(req.Phone),
		html.EscapeString(req.Message),
	)

	if err := common.SendEmail(subject, common.ContactEmail, body); err != nil {
		common.SysError(fmt.Sprintf("contact email send failed: %v", err))
		common.ApiErrorI18n(c, i18n.MsgContactSendFailed)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": i18n.T(c, i18n.MsgContactSuccess)})
}
