package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func newImportWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	sheet := f.GetSheetName(0)
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow(sheet, cell, &row))
	}
	return f
}

func TestImportUsersRejectsQuotaAboveWalletCeiling(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "quota", "aff_quota"},
		{"import_quota_over", "password123", fmt.Sprintf("%d", common.MaxWalletQuota+1), ""},
		{"import_aff_over", "password123", "", fmt.Sprintf("%d", common.MaxWalletQuota+1)},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (import_quota_over): invalid quota value")
	require.Contains(t, result.Errors[0], fmt.Sprintf("wallet quota exceeds %d", common.MaxWalletQuota))
	require.Contains(t, result.Errors[1], "line 3 (import_aff_over): invalid aff_quota value")
	require.Zero(t, result.Created)
}

func TestImportUsersSetsImportedQuotaAbove32Bit(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "quota"},
		{"import_big_wallet", "password123", "5000000000"},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "import_big_wallet").First(&user).Error)
	require.Equal(t, 5_000_000_000, user.Quota)
}

func TestImportUsersRefusesPrivilegedRolesForAnyCaller(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "role"},
		{"import_as_root", "password123", "root"},
		{"import_as_admin", "password123", "admin"},
		{"import_as_user", "password123", "user"},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (import_as_root): privileged role")
	require.Contains(t, result.Errors[0], "promote the account after import")
	require.Contains(t, result.Errors[1], "line 3 (import_as_admin): privileged role")
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username IN ?", []string{"import_as_root", "import_as_admin"}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "import_as_user").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestImportUsersAdminCallerCanImportCommonUsers(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "role"},
		{"import_by_admin", "password123", "user"},
	})
	result, err := ImportUsersFromFile(f, common.RoleAdminUser)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
}

func TestImportUsersRejectsRowsFailingUserValidation(t *testing.T) {
	truncate(t)
	longName := strings.Repeat("a", 25)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password"},
		{"import_short_pw", "12345"},
		{longName, "password123"},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (import_short_pw): invalid user")
	require.Contains(t, result.Errors[1], "line 3 ("+longName+"): invalid user")
	require.Zero(t, result.Created)
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username IN ?", []string{"import_short_pw", longName}).Count(&count).Error)
	require.Zero(t, count)
}

func TestExportThenImportRoundTrip(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{Username: "export_me", DisplayName: "Export Me", Group: "vip", Quota: 123, Status: common.UserStatusEnabled, Role: common.RoleCommonUser}).Error)
	f, err := BuildExportFile()
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	rows, err := f.GetRows(f.GetSheetName(0))
	require.NoError(t, err)
	require.Equal(t, ExcelHeaders, rows[0])
	require.Len(t, rows, 2)
	require.Equal(t, "export_me", rows[1][1])

	// Re-import under a new name with a password filled in.
	rows[1][1] = "export_me_copy"
	rows[1][13] = "password123" // initial_password column
	imported := newImportWorkbook(t, [][]any{anyRow(rows[0]), anyRow(rows[1])})
	result, err := ImportUsersFromFile(imported, common.RoleRootUser)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
	var copy model.User
	require.NoError(t, model.DB.Where("username = ?", "export_me_copy").First(&copy).Error)
	require.Equal(t, "vip", copy.Group)
	require.Equal(t, 123, copy.Quota)
}

func anyRow(cells []string) []any {
	out := make([]any, len(cells))
	for i, c := range cells {
		out[i] = c
	}
	return out
}
