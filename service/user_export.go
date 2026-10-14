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

	const maxExportRows = 10000
	if len(users) > maxExportRows {
		return nil, fmt.Errorf("too many users to export: %d (maximum %d)", len(users), maxExportRows)
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
	// CreatedUsernames lists every account the import inserted, in row order.
	CreatedUsernames []string `json:"created_usernames"`
	// GrantedQuota is the sum of the wallet balances the created accounts ended up with.
	GrantedQuota int `json:"granted_quota"`
	// QuotaErrors lists accounts that were created but whose imported quota could
	// not be applied; they keep the default new-user balance and need a manual fix.
	QuotaErrors []string `json:"quota_errors"`
}

// ImportUsersFromFile parses an Excel file and creates new users.
// Existing usernames are skipped, and rows whose role the caller cannot manage
// (root may import any role below admin; others only roles below their own) are rejected;
// admin and root rows are never imported.
// Returns a result summary.
func ImportUsersFromFile(f *excelize.File, callerRole int) (*ImportResult, error) {
	result := &ImportResult{
		SkippedUsernames: []string{},
		Errors:           []string{},
		CreatedUsernames: []string{},
		QuotaErrors:      []string{},
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

		// Parse quota. A present cell (including "0") is applied exactly; an empty
		// cell keeps the default new-user balance.
		quota := 0
		quotaProvided := false
		if qs := getCell(row, "quota"); qs != "" {
			quotaProvided = true
			if q, err := strconv.Atoi(qs); err == nil {
				quota = q
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota value %q", lineNum, username, qs))
				continue
			}
			if err := common.ValidateWalletQuota(quota); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota value %q: %v", lineNum, username, qs, err))
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
				continue
			}
			if err := common.ValidateWalletQuota(affQuota); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid aff_quota value %q: %v", lineNum, username, qs, err))
				continue
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

		role := stringToRole(getCell(row, "role"))
		// Privileged accounts need CreateUser's step-up proof and audit trail, which an
		// Excel import cannot provide; promote them after import instead.
		if role >= common.RoleAdminUser {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): privileged role %q cannot be imported; promote the account after import", lineNum, username, getCell(row, "role")))
			continue
		}
		if !(callerRole == common.RoleRootUser || callerRole > role) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): role %q is not permitted for the importing admin", lineNum, username, getCell(row, "role")))
			continue
		}

		user := &model.User{
			Username:    username,
			Password:    password,
			DisplayName: displayName,
			Email:       getCell(row, "email"),
			Role:        role,
			Status:      stringToStatus(getCell(row, "status")),
			Group:       group,
			Quota:       0, // Insert() sets QuotaForNewUser; an imported quota is applied below
			AffQuota:    affQuota,
			InviterId:   inviterId,
			Remark:      getCell(row, "remark"),
		}

		// Same struct validation CreateUser applies (username/display name/email length, password length).
		if err := common.Validate.Struct(user); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid user: %v", lineNum, username, err))
			continue
		}

		// Pass inviterId=0 to avoid triggering referral reward logic during import.
		// The InviterId field on the struct is enough to record the relationship.
		if err := user.Insert(0); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		result.Created++
		result.CreatedUsernames = append(result.CreatedUsernames, username)

		// Move the balance from what Insert() stored on the struct to the imported value.
		granted := user.Quota
		if quotaProvided {
			if err := setImportedUserQuota(user.Id, user.Quota, quota); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): user created but quota could not be set: %v", lineNum, username, err))
				result.QuotaErrors = append(result.QuotaErrors, username)
			} else {
				granted = quota
			}
		}
		result.GrantedQuota += granted
	}

	return result, nil
}

// setImportedUserQuota moves a user's balance to target. Increases go through
// IncreaseUserQuota, which enforces common.MaxWalletQuota; decreases through
// DecreaseUserQuota.
func setImportedUserQuota(userId int, current int, target int) error {
	delta := target - current
	if delta > 0 {
		return model.IncreaseUserQuota(userId, delta, true)
	}
	if delta < 0 {
		return model.DecreaseUserQuota(userId, -delta, true)
	}
	return nil
}
