package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
