package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

// ExcelHeaders defines the ordered column headers for user export/import.
var ExcelHeaders = []string{
	"id", "username", "display_name", "email",
	"role", "status", "group", "quota",
	"used_quota", "request_count", "remark",
	"aff_quota", "inviter_id", "initial_password", "created_at",
}

// roleToString converts integer role to human-readable string.
func roleToString(role int) string {
	switch role {
	case 100:
		return "root"
	case 10:
		return "admin"
	default:
		return "user"
	}
}

// stringToRole converts role string to integer. Defaults to 1 (user).
func stringToRole(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "root":
		return 100
	case "admin":
		return 10
	default:
		return 1
	}
}

// statusToString converts integer status to human-readable string.
func statusToString(status int) string {
	if status == 2 {
		return "disabled"
	}
	return "enabled"
}

// stringToStatus converts status string to integer. Defaults to 1 (enabled).
func stringToStatus(s string) int {
	if strings.ToLower(strings.TrimSpace(s)) == "disabled" {
		return 2
	}
	return 1
}

// BuildExportFile creates an Excel file from all users and returns it.
func BuildExportFile() (*excelize.File, error) {
	users, err := model.GetAllUsersForExport()
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	sheet := "Users"
	f.SetSheetName("Sheet1", sheet)

	// Write header row
	for col, header := range ExcelHeaders {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	// Write data rows
	for rowIdx, u := range users {
		row := rowIdx + 2 // 1-indexed, row 1 is header
		values := []interface{}{
			u.Id,
			u.Username,
			u.DisplayName,
			u.Email,
			roleToString(u.Role),
			statusToString(u.Status),
			u.Group,
			u.Quota,
			u.UsedQuota,
			u.RequestCount,
			u.Remark,
			u.AffQuota,
			u.InviterId,
			"", // initial_password: always empty on export
			u.CreatedAt,
		}
		for col, val := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			f.SetCellValue(sheet, cell, val)
		}
	}

	return f, nil
}

// ImportResult holds the outcome of a user import operation.
type ImportResult struct {
	Created          int      `json:"created"`
	Skipped          int      `json:"skipped"`
	SkippedUsernames []string `json:"skipped_usernames"`
	Errors           []string `json:"errors"`
}

// ImportUsersFromFile parses an Excel file and creates new users.
// Existing usernames are skipped. Returns a result summary.
func ImportUsersFromFile(f *excelize.File) (*ImportResult, error) {
	result := &ImportResult{
		SkippedUsernames: []string{},
		Errors:           []string{},
	}

	// Find the sheet — use first sheet regardless of name
	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet: %w", err)
	}

	if len(rows) < 2 {
		// No data rows (only header or empty)
		return result, nil
	}

	const maxImportRows = 1000
	if len(rows)-1 > maxImportRows {
		return nil, fmt.Errorf("too many rows: maximum %d, got %d", maxImportRows, len(rows)-1)
	}

	// Build header index map from first row
	headerIdx := map[string]int{}
	for i, h := range rows[0] {
		headerIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	getCell := func(row []string, key string) string {
		idx, ok := headerIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for rowNum, row := range rows[1:] { // skip header
		lineNum := rowNum + 2 // human-readable line number

		username := getCell(row, "username")
		if username == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: username is empty", lineNum))
			continue
		}

		password := getCell(row, "initial_password")
		if password == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): initial_password is empty", lineNum, username))
			continue
		}

		// Check for duplicate username
		exists, err := model.CheckUserExistOrDeleted(username, "")
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): db error: %v", lineNum, username, err))
			continue
		}
		if exists {
			result.Skipped++
			result.SkippedUsernames = append(result.SkippedUsernames, username)
			continue
		}

		// Parse quota
		quota := 0
		if qs := getCell(row, "quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				quota = q
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota value %q", lineNum, username, qs))
				continue
			}
		}

		// Parse aff_quota
		affQuota := 0
		if qs := getCell(row, "aff_quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				affQuota = q
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid aff_quota value %q", lineNum, username, qs))
			}
		}

		// Parse inviter_id
		inviterId := 0
		if is := getCell(row, "inviter_id"); is != "" {
			if id, err := strconv.Atoi(is); err == nil {
				inviterId = id
			}
		}

		group := getCell(row, "group")
		if group == "" {
			group = "default"
		}

		displayName := getCell(row, "display_name")
		if displayName == "" {
			displayName = username
		}

		user := &model.User{
			Username:    username,
			Password:    password,
			DisplayName: displayName,
			Email:       getCell(row, "email"),
			Role:        stringToRole(getCell(row, "role")),
			Status:      stringToStatus(getCell(row, "status")),
			Group:       group,
			Quota:       0, // Insert() will set QuotaForNewUser; we override below if needed
			AffQuota:    affQuota,
			InviterId:   inviterId,
			Remark:      getCell(row, "remark"),
		}

		// Pass inviterId=0 to avoid triggering referral reward logic during import.
		// The InviterId field on the struct is enough to record the relationship.
		if err := user.Insert(0); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		// Override quota with imported value if it differs from the default applied by Insert().
		if quota != 0 {
			delta := quota - int(common.QuotaForNewUser)
			if delta > 0 {
				_ = model.IncreaseUserQuota(user.Id, delta, true)
			} else if delta < 0 {
				_ = model.DecreaseUserQuota(user.Id, -delta, true)
			}
		}

		result.Created++
	}

	return result, nil
}
