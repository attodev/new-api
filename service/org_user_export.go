/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

// OrgExcelHeaders defines ordered columns for org user export/import.
var OrgExcelHeaders = []string{
	"username", "display_name", "organization_role",
	"quota", "used_quota", "group", "status", "remark", "initial_password",
}

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
			u.Quota,
			u.UsedQuota,
			u.Group,
			statusToString(u.Status),
			u.Remark,
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
func ImportOrgUsersFromFile(f *excelize.File, organizationId int, removeAbsent bool) (*OrgImportResult, error) {
	result := &OrgImportResult{
		UpdatedUsernames: []string{},
		RemovedUsernames: []string{},
		Errors:           []string{},
	}
	presentUsernames := map[string]bool{}

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

		orgRole := getCell(row, "organization_role")
		if orgRole == "" {
			orgRole = model.OrganizationRoleMember
		}
		if !model.IsValidOrganizationRole(orgRole) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid organization_role %q", lineNum, username, orgRole))
			continue
		}

		var existingUser model.User
		dbErr := model.DB.Where("username = ?", username).First(&existingUser).Error
		userExists := existingUser.Id > 0

		if dbErr != nil && !isNotFound(dbErr) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): db error: %v", lineNum, username, dbErr))
			continue
		}

		if userExists {
			if existingUser.OrganizationId == organizationId {
				updates := map[string]interface{}{}
				if dn := getCell(row, "display_name"); dn != "" {
					updates["display_name"] = dn
				}
				if orgRole != existingUser.OrganizationRole {
					updates["organization_role"] = orgRole
				}
				if grp := getCell(row, "group"); grp != "" {
					updates["group"] = grp
				}
				if st := getCell(row, "status"); st != "" {
					updates["status"] = stringToStatus(st)
				}
				updates["remark"] = getCell(row, "remark")
				if err := model.DB.Model(&model.User{}).Where("id = ?", existingUser.Id).Updates(updates).Error; err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to update: %v", lineNum, username, err))
					continue
				}
				if qs := getCell(row, "quota"); qs != "" {
					if newQuota, err := strconv.Atoi(qs); err == nil {
						delta := newQuota - int(existingUser.Quota)
						if delta > 0 {
							_ = model.IncreaseUserQuota(existingUser.Id, delta, true)
						} else if delta < 0 {
							_ = model.DecreaseUserQuota(existingUser.Id, -delta, true)
						}
					}
				}
				if err := model.InvalidateUserCache(existingUser.Id); err != nil {
					common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
				}
				presentUsernames[username] = true
				result.Updated++
				result.UpdatedUsernames = append(result.UpdatedUsernames, username)
				continue
			}
			if existingUser.OrganizationId > 0 {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): belongs to another organization", lineNum, username))
				continue
			}
			if err := model.DB.Model(&model.User{}).
				Select("organization_id", "organization_role").
				Where("id = ?", existingUser.Id).
				Updates(model.User{
					OrganizationId:   organizationId,
					OrganizationRole: orgRole,
				}).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to assign: %v", lineNum, username, err))
				continue
			}
			if err := model.InvalidateUserCache(existingUser.Id); err != nil {
				common.SysLog("ImportOrgUsersFromFile: failed to invalidate cache for user " + username)
			}
			presentUsernames[username] = true
			result.Assigned++
			continue
		}

		// User does not exist — create then assign
		password := getCell(row, "initial_password")
		if password == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): initial_password is required for new users", lineNum, username))
			continue
		}

		quota := 0
		if qs := getCell(row, "quota"); qs != "" {
			if q, err := strconv.Atoi(qs); err == nil {
				quota = q
			} else {
				result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): invalid quota %q", lineNum, username, qs))
				continue
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

		newUser := &model.User{
			Username:    username,
			Password:    password,
			DisplayName: displayName,
			Role:        common.RoleCommonUser,
			Status:      stringToStatus(getCell(row, "status")),
			Group:       group,
			Remark:      getCell(row, "remark"),
		}

		if err := newUser.Insert(0); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): %v", lineNum, username, err))
			continue
		}

		if err := model.DB.Model(&model.User{}).
			Select("organization_id", "organization_role").
			Where("id = ?", newUser.Id).
			Updates(model.User{
				OrganizationId:   organizationId,
				OrganizationRole: orgRole,
			}).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set org membership: %v", lineNum, username, err))
			continue
		}

		if quota != 0 {
			delta := quota - int(common.QuotaForNewUser)
			if delta > 0 {
				if err := model.IncreaseUserQuota(newUser.Id, delta, true); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
				}
			} else if delta < 0 {
				if err := model.DecreaseUserQuota(newUser.Id, -delta, true); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): failed to set quota: %v", lineNum, username, err))
				}
			}
		}

		presentUsernames[username] = true
		result.Created++
	}

	if removeAbsent {
		var currentMembers []model.User
		if err := model.DB.
			Where("organization_id = ? AND organization_role != ?", organizationId, "owner").
			Find(&currentMembers).Error; err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("remove-absent: failed to query members: %v", err))
		} else {
			for _, member := range currentMembers {
				if presentUsernames[member.Username] {
					continue
				}
				if err := model.DB.Model(&model.User{}).
					Select("organization_id", "organization_role").
					Where("id = ?", member.Id).
					Updates(model.User{OrganizationId: 0, OrganizationRole: ""}).Error; err != nil {
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
