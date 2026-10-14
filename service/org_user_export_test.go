package service

import (
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// Imported balances are new quota: they must respect the wallet ceiling
// common.MaxWalletQuota.

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
		Username: "org_member", Password: "x", DisplayName: "before", Quota: common.MaxWalletQuota - 1,
		Status: common.UserStatusEnabled, OrganizationId: orgId, OrganizationRole: model.OrganizationRoleMember,
	}
	require.NoError(t, model.DB.Create(user).Error)

	// 18014398510 USD at QuotaPerUnit=500000 is just above MaxWalletQuota (2^53-1).
	const overUSD = "18014398510"
	f := newImportWorkbook(t, [][]any{
		{"username", "display_name", "organization_role", "quota", "initial_password"},
		{"org_member", "after", model.OrganizationRoleMember, overUSD, ""},
		{"org_new_member", "", model.OrganizationRoleMember, overUSD, "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, orgId, false)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (org_member): invalid quota")
	require.Contains(t, result.Errors[0], fmt.Sprintf("quota exceeds the wallet limit %d", common.MaxWalletQuota))
	require.Contains(t, result.Errors[1], "line 3 (org_new_member): invalid quota")
	require.Zero(t, result.Updated)
	require.Zero(t, result.Created)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, user.Id).Error)
	require.Equal(t, common.MaxWalletQuota-1, reloaded.Quota)
	require.Equal(t, "before", reloaded.DisplayName, "a rejected row must not be partially applied")

	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "org_new_member").Count(&count).Error)
	require.Zero(t, count)

	// The largest whole-USD balance under the ceiling is accepted.
	f = newImportWorkbook(t, [][]any{
		{"username", "organization_role", "quota"},
		{"org_member", model.OrganizationRoleMember, "18014398509"},
	})
	result, err = ImportOrgUsersFromFile(f, orgId, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Updated)
	require.NoError(t, model.DB.First(&reloaded, user.Id).Error)
	require.Equal(t, 18014398509*500_000, reloaded.Quota)
}

func TestImportedWalletQuota(t *testing.T) {
	withDefaultQuotaPerUnit(t)
	q, err := importedWalletQuota(1.5)
	require.NoError(t, err)
	require.Equal(t, 750_000, q)
	q, err = importedWalletQuota(0.29) // decimal product, no float truncation to 144999
	require.NoError(t, err)
	require.Equal(t, 145_000, q)
	_, err = importedWalletQuota(18014398510)
	require.Error(t, err)
	_, err = importedWalletQuota(math.Inf(1))
	require.Error(t, err)
	_, err = importedWalletQuota(math.NaN())
	require.Error(t, err)
}

func TestImportOrgUsersNeverEscalatesRoles(t *testing.T) {
	truncate(t)
	withDefaultQuotaPerUnit(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 0, 1, model.OrganizationRoleMember)
	// A global "role" column is not part of the format and must be ignored.
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "quota", "role", "initial_password"},
		{"member_21", "owner", "1", "root", ""},
		{"brand_new", "admin", "1", "admin", "password123"},
		{"brand_owner", "owner", "1", "", "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, false)
	require.NoError(t, err)

	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Equal(t, model.OrganizationRoleMember, member.OrganizationRole, "owner role cannot be granted by import")
	require.Equal(t, common.RoleCommonUser, member.Role, "global role untouched")

	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "brand_new").First(&created).Error)
	require.Equal(t, common.RoleCommonUser, created.Role, "a created user never gets a privileged global role")
	require.Equal(t, model.OrganizationRoleAdmin, created.OrganizationRole)
	require.Equal(t, 1, created.OrganizationId)
	require.Equal(t, 500_000, created.Quota)

	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "brand_owner").Count(&count).Error)
	require.Zero(t, count, "an owner row must not create an account")

	require.Len(t, result.Errors, 2, "the owner rows must be reported, not silently applied")
	require.Contains(t, result.Errors[0], "line 2 (member_21)")
	require.Contains(t, result.Errors[1], "line 4 (brand_owner)")
	require.Equal(t, 1, result.Created)
}

func TestImportOrgUsersDoesNotDemoteOwnerOrTouchPlatformAdmins(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 22, "plat_admin", 0, 1, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 22).Update("role", common.RoleAdminUser).Error)
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "display_name"},
		{"owner_20", "member", "x"},
		{"plat_admin", "member", "x"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, true)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	var owner, admin model.User
	require.NoError(t, model.DB.First(&owner, 20).Error)
	require.Equal(t, model.OrganizationRoleOwner, owner.OrganizationRole)
	require.NoError(t, model.DB.First(&admin, 22).Error)
	require.Equal(t, 1, admin.OrganizationId)
	require.Equal(t, common.RoleAdminUser, admin.Role)
	require.Zero(t, result.Removed, "remove-absent never drops the owner")
}
