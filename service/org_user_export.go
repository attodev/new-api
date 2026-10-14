package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

// importedWalletQuota converts an imported USD balance to quota (truncated,
// as before) and rejects a balance outside the wallet ceiling
// common.MaxWalletQuota instead of a bare float->int conversion.
func importedWalletQuota(usd float64) (int, error) {
	if math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0, fmt.Errorf("quota must be a finite number")
	}
	quota, err := common.WalletQuotaFromDecimalStrict(
		decimal.NewFromFloat(usd).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Truncate(0),
	)
	if err != nil {
		return 0, fmt.Errorf("quota exceeds the wallet limit %d", common.MaxWalletQuota)
	}
	return quota, nil
}

// OrgExcelHeaders defines ordered columns for org user export/import.
// The group column is export-only: the import never writes users.group (it sets
// channel access and the billing ratio, which organization admins must not
// choose), so the cell is ignored and new users get the default group.
// member_status and organization_remark are the organization-scoped member
// status and note (plan decision 7); the platform users.status / users.remark
// are root-only and never read from or written to a sheet.
var OrgExcelHeaders = []string{
	"username", "display_name", "organization_role",
	"quota", "used_quota", "group", "member_status", "organization_remark", "initial_password",
}

// orgExcelRetiredColumns are the old platform columns. A sheet still carrying
// one is refused as a whole, so an old template never silently drops edits.
var orgExcelRetiredColumns = []struct{ column, replacement string }{
	{"status", "member_status"},
	{"remark", "organization_remark"},
}

func memberStatusToString(status int) string {
	if status == model.OrganizationMemberStatusSuspended {
		return "suspended"
	}
	return "active"
}

func stringToMemberStatus(s string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "active":
		return model.OrganizationMemberStatusActive, true
	case "suspended":
		return model.OrganizationMemberStatusSuspended, true
	default:
		return 0, false
	}
}

// orgImportMemberFields reads the organization-scoped member columns of a row.
// Only present, non-empty member_status cells and present organization_remark
// columns are returned, so an absent column keeps the stored value.
func orgImportMemberFields(memberStatusCell string, remarkPresent bool, remarkCell string) (map[string]interface{}, error) {
	fields := map[string]interface{}{}
	if memberStatusCell != "" {
		status, ok := stringToMemberStatus(memberStatusCell)
		if !ok {
			return nil, fmt.Errorf("invalid member_status %q (use active or suspended)", memberStatusCell)
		}
		fields["organization_member_status"] = status
	}
	if remarkPresent {
		if utf8.RuneCountInString(remarkCell) > orgImportMaxRemarkLength {
			return nil, fmt.Errorf("organization_remark must be at most %d characters", orgImportMaxRemarkLength)
		}
		fields["organization_remark"] = remarkCell
	}
	return fields, nil
}

const orgImportMaxRemarkLength = 255

// BuildOrgExportFile creates an Excel file of all members in an organization.
func BuildOrgExportFile(organizationId int) (*excelize.File, error) {
	var users []model.User
	err := model.DB.
		Where("organization_id = ? AND role < ?", organizationId, common.RoleAdminUser).
		Order("id asc").
		Find(&users).Error
	if err != nil {
		return nil, err
	}

	const maxExportRows = 10000
	if len(users) > maxExportRows {
		return nil, fmt.Errorf("too many members to export: %d (maximum %d)", len(users), maxExportRows)
	}

	f := excelize.NewFile()
	sheet := "OrgUsers"
	f.SetSheetName("Sheet1", sheet)

	for col, header := range OrgExcelHeaders {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		f.SetCellValue(sheet, cell, header)
	}

	for rowIdx, u := range users {
		row := rowIdx + 2
		values := []interface{}{
			u.Username,
			u.DisplayName,
			u.OrganizationRole,
			float64(u.Quota) / common.QuotaPerUnit,
			float64(u.UsedQuota) / common.QuotaPerUnit,
			u.Group,
			memberStatusToString(u.OrganizationMemberStatus),
			u.OrganizationRemark,
			"",
		}
		for col, val := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			f.SetCellValue(sheet, cell, val)
		}
	}

	return f, nil
}

// OrgImportResult holds the outcome of an org user import operation.
type OrgImportResult struct {
	Assigned         int      `json:"assigned"`
	Created          int      `json:"created"`
	Updated          int      `json:"updated"`
	UpdatedUsernames []string `json:"updated_usernames"`
	Removed          int      `json:"removed"`
	RemovedUsernames []string `json:"removed_usernames"`
	Errors           []string `json:"errors"`
}

// ImportOrgUsersFromFile parses an Excel file and assigns/creates users for an org.
// The group column is ignored (never written); platform admins and the owner are
// never edited or detached, including by removeAbsent. actorId is the importing
// user: their own row is rejected and removeAbsent never removes them, the same
// "organization admins cannot modify themselves" rule the member endpoints apply.
func ImportOrgUsersFromFile(f *excelize.File, organizationId int, actorId int, removeAbsent bool) (*OrgImportResult, error) {
	result := &OrgImportResult{
		UpdatedUsernames: []string{},
		RemovedUsernames: []string{},
		Errors:           []string{},
	}
	listedUsernames := map[string]bool{}

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to read sheet: %w", err)
	}
	if len(rows) < 2 {
		return result, nil
	}

	const maxImportRows = 1000
	if len(rows)-1 > maxImportRows {
		return nil, fmt.Errorf("too many rows: maximum %d, got %d", maxImportRows, len(rows)-1)
	}

	headerIdx := map[string]int{}
	for i, h := range rows[0] {
		headerIdx[strings.TrimSpace(strings.ToLower(h))] = i
	}

	hasColumn := func(key string) bool {
		_, ok := headerIdx[key]
		return ok
	}
	for _, retired := range orgExcelRetiredColumns {
		if hasColumn(retired.column) {
			return nil, fmt.Errorf("unsupported column: %s — use %s", retired.column, retired.replacement)
		}
	}
	getCell := func(row []string, key string) string {
		idx, ok := headerIdx[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[idx])
	}

	for rowNum, row := range rows[1:] {
		lineNum := rowNum + 2

		username := getCell(row, "username")
		if username == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: username is empty", lineNum))
			continue
		}

		// A listed username is never treated as absent by remove-absent, even when
		// its row is rejected below.
		listedUsernames[username] = true

		// Only member and admin can be set by an import. The owner is established by
		// creating or transferring the organization. The sheet has no global role
		// column and an import never changes users.role.
		orgRole := getCell(row, "organization_role")
		if orgRole == "" {
			orgRole = model.OrganizationRoleMember
		}
		if orgRole != model.OrganizationRoleMember && orgRole != model.OrganizationRoleAdmin {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid organization_role %q (only member or admin can be imported)", lineNum, username, orgRole))
			continue
		}

		var existingUser model.User
		dbErr := model.DB.Where("username = ?", username).First(&existingUser).Error
		userExists := existingUser.Id > 0

		if dbErr != nil && !isNotFound(dbErr) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): db error: %v", lineNum, username, dbErr))
			continue
		}

		if userExists && actorId > 0 && existingUser.Id == actorId {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): an import cannot change the importing admin's own row", lineNum, username))
			continue
		}

		if userExists {
			// Mirror CanManageOrganizationTarget: an import never edits platform
			// admins, and never demotes the organization owner.
			if existingUser.Role >= common.RoleAdminUser {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): platform administrators cannot be managed by an import", lineNum, username))
				continue
			}
			if existingUser.OrganizationId == organizationId {
				if model.HasOrganizationOwnerRole(existingUser.OrganizationRole) {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): the organization owner cannot be changed by an import", lineNum, username))
					continue
				}
				// Validate the imported balance before touching the row, so an
				// out-of-range quota rejects the whole row.
				var importedQuota *int
				if qs := getCell(row, "quota"); qs != "" {
					if usd, err := strconv.ParseFloat(qs, 64); err == nil {
						quota, err := importedWalletQuota(usd)
						if err != nil {
							result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota %q: %v", lineNum, username, qs, err))
							continue
						}
						importedQuota = &quota
					}
				}
				updates, err := orgImportMemberFields(getCell(row, "member_status"), hasColumn("organization_remark"), getCell(row, "organization_remark"))
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
					continue
				}
				if dn := getCell(row, "display_name"); dn != "" {
					updates["display_name"] = dn
				}
				if orgRole != existingUser.OrganizationRole {
					updates["organization_role"] = orgRole
				}
				// Scoped to the organization: a member removed since the row was
				// read is not written (ErrOrganizationMembershipChanged).
				if err := model.UpdateOrganizationMemberFields(existingUser.Id, organizationId, updates); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to update: %v", lineNum, username, err))
					continue
				}
				if importedQuota != nil {
					if err := model.SetOrganizationMemberQuota(existingUser.Id, organizationId, *importedQuota); err != nil {
						result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
					}
				}
				if err := model.InvalidateUserCache(existingUser.Id); err != nil {
					common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
				}
				result.Updated++
				result.UpdatedUsernames = append(result.UpdatedUsernames, username)
				continue
			}
			if existingUser.OrganizationId > 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): belongs to another organization", lineNum, username))
				continue
			}
			memberFields, err := orgImportMemberFields(getCell(row, "member_status"), hasColumn("organization_remark"), getCell(row, "organization_remark"))
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
				continue
			}
			if err := model.AssignUserToOrganization(existingUser.Id, organizationId, orgRole); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to assign: %v", lineNum, username, err))
				continue
			}
			if err := model.UpdateOrganizationMemberFields(existingUser.Id, organizationId, memberFields); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set member status or note: %v", lineNum, username, err))
			}
			if err := model.InvalidateUserCache(existingUser.Id); err != nil {
				common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
			}
			result.Assigned++
			continue
		}

		// User does not exist — create then assign
		password := getCell(row, "initial_password")
		if password == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): initial_password is required for new users", lineNum, username))
			continue
		}
		memberFields, err := orgImportMemberFields(getCell(row, "member_status"), hasColumn("organization_remark"), getCell(row, "organization_remark"))
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		var quota int
		quotaProvided := false
		if qs := getCell(row, "quota"); qs != "" {
			usd, err := strconv.ParseFloat(qs, 64)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota %q", lineNum, username, qs))
				continue
			}
			quota, err = importedWalletQuota(usd)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota %q: %v", lineNum, username, qs, err))
				continue
			}
			quotaProvided = true
		}

		displayName := getCell(row, "display_name")
		if displayName == "" {
			displayName = username
		}

		newUser := &model.User{
			Username:    username,
			Password:    password,
			DisplayName: displayName,
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled, // the platform status is root-only
		}

		// Same struct validation CreateUser applies (username/display name length, password length).
		if err := common.Validate.Struct(newUser); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid user: %v", lineNum, username, err))
			continue
		}

		// Insert and assignment share one transaction: a failed assignment
		// leaves no account behind.
		if err := model.CreateUserInOrganization(newUser, organizationId, orgRole); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		if err := model.UpdateOrganizationMemberFields(newUser.Id, organizationId, memberFields); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set member status or note: %v", lineNum, username, err))
		}

		if quotaProvided {
			if err := model.SetOrganizationMemberQuota(newUser.Id, organizationId, quota); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
			}
			if err := model.InvalidateUserCache(newUser.Id); err != nil {
				common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
			}
		}

		result.Created++
	}

	if removeAbsent {
		var currentMembers []model.User
		if err := model.DB.
			Where("organization_id = ? AND organization_role != ? AND role < ?", organizationId, model.OrganizationRoleOwner, common.RoleAdminUser).
			Find(&currentMembers).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove-absent: failed to query members: %v", err))
		} else {
			for _, member := range currentMembers {
				if listedUsernames[member.Username] || member.Id == actorId {
					continue
				}
				// Leaving zeroes the member quota cap (model.ClearOrganizationMembership),
				// so an imported cap never survives as a personal balance.
				if _, err := model.ClearOrganizationMembership(member.Id, organizationId); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("remove-absent (%s): failed to remove: %v", member.Username, err))
					continue
				}
				if err := model.InvalidateUserCache(member.Id); err != nil {
					common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + member.Username)
				}
				result.Removed++
				result.RemovedUsernames = append(result.RemovedUsernames, member.Username)
			}
		}
	}

	return result, nil
}

// isNotFound returns true if the error is a GORM "record not found" error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "record not found")
}
