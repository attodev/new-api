package controller

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O5 landing inquiry form (web/landing/public/landing.js posts these fields).
type contactRequest struct {
	Org          string   `json:"org"`
	Email        string   `json:"email"`
	Phone        string   `json:"phone"`
	Features     []string `json:"features"`
	ScaleType    string   `json:"scale_type"`
	Headcount    string   `json:"headcount"`
	InputTokens  string   `json:"input_tokens"`
	OutputTokens string   `json:"output_tokens"`
	Notes        string   `json:"notes"`
	Message      string   `json:"message"` // legacy clients
}

// sendContactMail is replaced in tests.
var sendContactMail = common.SendEmail

func Contact(c *gin.Context) {
	var req contactRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid request")
		return
	}
	req.Org = strings.TrimSpace(req.Org)
	req.Email = strings.TrimSpace(req.Email)
	if req.Notes == "" {
		req.Notes = req.Message
	}
	if req.Org == "" || req.Email == "" {
		common.ApiErrorT(c, "Organization and email are required")
		return
	}
	if common.Validate.Var(req.Email, "required,email,max=254") != nil {
		common.ApiErrorT(c, "Please enter a valid email address")
		return
	}
	if common.ContactEmail == "" {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Contact email is not configured"))
		return
	}
	subject := fmt.Sprintf("[%s] Enterprise Inquiry — %s", common.SystemName, req.Org)
	if err := sendContactMail(subject, common.ContactEmail, renderContactMail(req)); err != nil {
		common.SysError(fmt.Sprintf("contact email send failed: %v", err))
		common.ApiErrorT(c, "Failed to send the inquiry, please try again later")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func renderContactMail(req contactRequest) string {
	row := func(label, value string) string {
		return fmt.Sprintf(`<tr><td style="padding:8px 12px;font-weight:700;width:160px;background:#f3f4f6;">%s</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;white-space:pre-wrap;">%s</td></tr>`, label, html.EscapeString(value))
	}
	scale := req.Headcount
	scaleLabel := "Headcount"
	if req.ScaleType == "token" {
		scale = fmt.Sprintf("input %s / output %s tokens", req.InputTokens, req.OutputTokens)
		scaleLabel = "Monthly tokens"
	}
	return `<html><body style="font-family:sans-serif;color:#222;"><h2 style="color:#0EA5E9;">Enterprise Inquiry</h2>` +
		`<table style="border-collapse:collapse;width:100%;max-width:640px;">` +
		row("Company / Organization", req.Org) + row("Email", req.Email) + row("Phone", req.Phone) +
		row("Features", strings.Join(req.Features, ", ")) + row(scaleLabel, scale) + row("Notes", req.Notes) +
		`</table></body></html>`
}
