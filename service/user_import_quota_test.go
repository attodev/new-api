package service

import (
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// Imported balances are new quota: they must respect the wallet ceiling
// common.MaxQuota (upstream a073f74b3 wallet bound, fork ceiling).

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

func withDefaultQuotaPerUnit(t *testing.T) {
	t.Helper()
	old := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = old })
}

func TestImportOrgUsersRejectsQuotaAboveWalletCeiling(t *testing.T) {
	truncate(t)
	withDefaultQuotaPerUnit(t)
	const orgId = 77
	user := &model.User{
		Username: "org_member", Password: "x", DisplayName: "before", Quota: common.MaxQuota - 1,
		Status: common.UserStatusEnabled, OrganizationId: orgId, OrganizationRole: model.OrganizationRoleMember,
	}
	require.NoError(t, model.DB.Create(user).Error)

	// $1,000,000,001 at QuotaPerUnit=500000 is MaxQuota + 500000.
	f := newImportWorkbook(t, [][]any{
		{"username", "display_name", "organization_role", "quota", "initial_password"},
		{"org_member", "after", model.OrganizationRoleMember, "1000000001", ""},
		{"org_new_member", "", model.OrganizationRoleMember, "1000000001", "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, orgId, false)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (org_member): invalid quota")
	require.Contains(t, result.Errors[0], fmt.Sprintf("quota exceeds the wallet limit %d", common.MaxQuota))
	require.Contains(t, result.Errors[1], "line 3 (org_new_member): invalid quota")
	require.Zero(t, result.Updated)
	require.Zero(t, result.Created)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, user.Id).Error)
	require.Equal(t, common.MaxQuota-1, reloaded.Quota)
	require.Equal(t, "before", reloaded.DisplayName, "a rejected row must not be partially applied")

	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "org_new_member").Count(&count).Error)
	require.Zero(t, count)

	// Exactly the ceiling is accepted and credited through the grant path.
	f = newImportWorkbook(t, [][]any{
		{"username", "organization_role", "quota"},
		{"org_member", model.OrganizationRoleMember, "1000000000"},
	})
	result, err = ImportOrgUsersFromFile(f, orgId, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Updated)
	require.NoError(t, model.DB.First(&reloaded, user.Id).Error)
	require.Equal(t, common.MaxQuota, reloaded.Quota)
}

func TestImportUsersRejectsQuotaAboveWalletCeiling(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "quota", "aff_quota"},
		{"import_quota_over", "password123", fmt.Sprintf("%d", common.MaxQuota+1), ""},
		{"import_aff_over", "password123", "", fmt.Sprintf("%d", common.MaxQuota+1)},
	})
	result, err := ImportUsersFromFile(f)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (import_quota_over): invalid quota value")
	require.Contains(t, result.Errors[0], fmt.Sprintf("wallet quota exceeds %d", common.MaxQuota))
	require.Contains(t, result.Errors[1], "line 3 (import_aff_over): invalid aff_quota value")
	require.Contains(t, result.Errors[1], fmt.Sprintf("wallet quota exceeds %d", common.MaxQuota))
	require.Zero(t, result.Created)

	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username IN ?", []string{"import_quota_over", "import_aff_over"}).Count(&count).Error)
	require.Zero(t, count)
}

func TestImportedWalletQuota(t *testing.T) {
	withDefaultQuotaPerUnit(t)
	q, err := importedWalletQuota(1.5)
	require.NoError(t, err)
	require.Equal(t, int64(750_000), q)
	q, err = importedWalletQuota(0.29) // decimal product, no float truncation to 144999
	require.NoError(t, err)
	require.Equal(t, int64(145_000), q)
	_, err = importedWalletQuota(1_000_000_001)
	require.Error(t, err)
	_, err = importedWalletQuota(math.Inf(1))
	require.Error(t, err)
	_, err = importedWalletQuota(math.NaN())
	require.Error(t, err)
}
