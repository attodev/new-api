package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// fork: O4 — admin Excel export/import of users.

// ExportUsers streams an Excel file of all users to the client.
func ExportUsers(c *gin.Context) {
	f, err := service.BuildExportFile()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer f.Close()

	filename := fmt.Sprintf("users-%d.xlsx", time.Now().Unix())
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	if err := f.Write(c.Writer); err != nil {
		common.ApiError(c, err)
	}
}

// ImportUsers handles Excel file upload and bulk user creation.
func ImportUsers(c *gin.Context) {
	const maxSize = 10 << 20 // 10 MB
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize)

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		common.ApiError(c, fmt.Errorf("failed to read file: %w", err))
		return
	}
	defer file.Close()

	f, err := excelize.OpenReader(file)
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid xlsx file: %w", err))
		return
	}
	defer f.Close()

	result, err := service.ImportUsersFromFile(f, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordUserImportAudit(c, result)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// importAuditUsernameLimit caps how many created usernames one import audit entry lists.
const importAuditUsernameLimit = 50

// recordUserImportAudit records one operation audit entry summarising an Excel
// import, attributed to the importing admin, and marks the request as audited so
// AdminAuth does not add its generic fallback entry. It mirrors
// recordManageAuditFor but supplies its own English content, because
// "user.import" has no entry in the upstream template table (controller/audit.go).
func recordUserImportAudit(c *gin.Context, result *service.ImportResult) {
	usernames := result.CreatedUsernames
	if len(usernames) > importAuditUsernameLimit {
		usernames = usernames[:importAuditUsernameLimit]
	}
	params := map[string]any{
		"created":                 result.Created,
		"skipped":                 result.Skipped,
		"error_count":             len(result.Errors),
		"created_usernames":       usernames,
		"created_usernames_total": len(result.CreatedUsernames),
		"granted_quota":           result.GrantedQuota,
	}
	if len(result.QuotaErrors) > 0 {
		params["quota_errors"] = result.QuotaErrors
	}
	content := fmt.Sprintf("Imported %d users from Excel (skipped %d, errors %d, granted quota %d)",
		result.Created, result.Skipped, len(result.Errors), result.GrantedQuota)
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), content, c.ClientIP(), "user.import", params, auditOperatorInfo(c), nil, c)
	markAuditLogged(c)
}
