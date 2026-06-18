package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
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
		c.JSON(http.StatusBadRequest, gin.H{"message": "잘못된 요청입니다."})
		return
	}

	if req.Org == "" || req.Email == "" || req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "회사명, 이메일, 문의내용은 필수입니다."})
		return
	}

	if common.ContactEmail == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"message": "문의 수신 이메일이 설정되지 않았습니다."})
		return
	}

	subject := fmt.Sprintf("[%s] 기업 도입 문의 — %s", common.SystemName, req.Org)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;">
<h2 style="color:#0EA5E9;">기업 도입 문의</h2>
<table style="border-collapse:collapse;width:100%%;max-width:560px;">
<tr><td style="padding:8px 12px;font-weight:700;width:120px;background:#f3f4f6;">회사/기관</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">이메일</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">전화번호</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;">%s</td></tr>
<tr><td style="padding:8px 12px;font-weight:700;background:#f3f4f6;">문의내용</td><td style="padding:8px 12px;white-space:pre-wrap;">%s</td></tr>
</table>
</body></html>`,
		req.Org, req.Email, req.Phone, req.Message,
	)

	if err := common.SendEmail(subject, common.ContactEmail, body); err != nil {
		common.SysError(fmt.Sprintf("contact email send failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"message": "이메일 전송에 실패했습니다. 잠시 후 다시 시도해 주세요."})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
