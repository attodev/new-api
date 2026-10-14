package service

import (
	"fmt"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	result, err := ImportOrgUsersFromFile(f, orgId, 0, false)
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
	result, err = ImportOrgUsersFromFile(f, orgId, 0, false)
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
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
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
	result, err := ImportOrgUsersFromFile(f, 1, 0, true)
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

func TestImportOrgUsersRemoveAbsentKeepsUnlistedPlatformAdmins(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 0, 1, model.OrganizationRoleMember)
	seedOrganizationUser(t, 22, "plat_admin", 0, 1, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 22).Update("role", common.RoleAdminUser).Error)
	// Neither the owner, the platform admin nor member_21 is listed.
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role"},
		{"someone_else", "member"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, true)
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	require.Equal(t, []string{"member_21"}, result.RemovedUsernames)
	var owner, admin, member model.User
	require.NoError(t, model.DB.First(&owner, 20).Error)
	require.Equal(t, 1, owner.OrganizationId)
	require.NoError(t, model.DB.First(&admin, 22).Error)
	require.Equal(t, 1, admin.OrganizationId, "an unlisted platform admin stays in the organization")
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Zero(t, member.OrganizationId)
}

func TestImportOrgUsersIgnoresGroupColumn(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 0, 1, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 21).Update("group", "default").Error)
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "group", "initial_password"},
		{"member_21", "member", "vip", ""},
		{"brand_new", "member", "vip", "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Updated)
	require.Equal(t, 1, result.Created)
	var existing, created model.User
	require.NoError(t, model.DB.First(&existing, 21).Error)
	require.Equal(t, "default", existing.Group)
	require.NoError(t, model.DB.Where("username = ?", "brand_new").First(&created).Error)
	require.Equal(t, "default", created.Group)
}

// fork: O3 — remove-absent zeroes the member quota cap like every other
// organization exit, so an imported cap never survives as a personal balance.
func TestImportOrgUsersRemoveAbsentZeroesMemberQuota(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 700, 1, model.OrganizationRoleAdmin)
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role"},
		{"owner_20", "member"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, true)
	require.NoError(t, err)
	require.Equal(t, []string{"member_21"}, result.RemovedUsernames)
	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Zero(t, member.OrganizationId)
	require.Empty(t, member.OrganizationRole)
	require.Zero(t, member.Quota)
}

// fork: O3 — the importing admin's own row is rejected (an admin must not raise
// their own cap or change their own status/role through a sheet), and
// remove-absent never removes the importing admin.
func TestImportOrgUsersRejectsTheActorsOwnRow(t *testing.T) {
	truncate(t)
	withDefaultQuotaPerUnit(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 22, "admin_22", 500, 1, model.OrganizationRoleAdmin)
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "quota", "member_status"},
		{"admin_22", model.OrganizationRoleAdmin, "1000000", "suspended"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 22, false)
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0], "line 2 (admin_22): an import cannot change the importing admin's own row")
	require.Zero(t, result.Updated)
	var admin model.User
	require.NoError(t, model.DB.First(&admin, 22).Error)
	require.Equal(t, 500, admin.Quota)
	require.Equal(t, model.OrganizationMemberStatusActive, admin.OrganizationMemberStatus)
}

func TestImportOrgUsersRemoveAbsentNeverRemovesTheActor(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 22, "admin_22", 500, 1, model.OrganizationRoleAdmin)
	seedOrganizationUser(t, 23, "member_23", 300, 1, model.OrganizationRoleMember)
	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role"},
		{"owner_20", "member"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 22, true)
	require.NoError(t, err)
	require.Equal(t, []string{"member_23"}, result.RemovedUsernames)
	var admin model.User
	require.NoError(t, model.DB.First(&admin, 22).Error)
	require.Equal(t, 1, admin.OrganizationId)
	require.Equal(t, 500, admin.Quota)
}

// fork: O3 (review I7, plan decision 7) — the import writes the
// organization-scoped note only when the sheet has an organization_remark
// column; a sheet without it keeps the existing note. The platform remark is
// never written.
func TestImportOrgUsersKeepsOrganizationRemarkWhenColumnIsAbsent(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 21).Updates(map[string]any{
		"remark": "root note", "organization_remark": "org note",
	}).Error)

	f := newImportWorkbook(t, [][]any{
		{"username", "display_name"},
		{"member_21", "Renamed"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Equal(t, "Renamed", member.DisplayName)
	require.Equal(t, "org note", member.OrganizationRemark)

	// With the column present the note is written (an empty cell clears it).
	f = newImportWorkbook(t, [][]any{
		{"username", "organization_remark", "member_status"},
		{"member_21", "sheet note", "suspended"},
	})
	result, err = ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Equal(t, "sheet note", member.OrganizationRemark)
	require.Equal(t, model.OrganizationMemberStatusSuspended, member.OrganizationMemberStatus)
	require.Equal(t, "root note", member.Remark, "the platform remark stays root-only")
	require.Equal(t, common.UserStatusEnabled, member.Status, "the platform status stays root-only")
}

// Sheets written for the old layout carry platform status/remark columns; the
// whole import is refused instead of silently ignoring them (plan decision 9).
func TestImportOrgUsersRejectsPlatformStatusAndRemarkColumns(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)

	for column, replacement := range map[string]string{"status": "member_status", "remark": "organization_remark"} {
		f := newImportWorkbook(t, [][]any{
			{"username", "display_name", column},
			{"member_21", "Renamed", "x"},
		})
		result, err := ImportOrgUsersFromFile(f, 1, 0, false)
		require.Nil(t, result)
		require.EqualError(t, err, fmt.Sprintf("unsupported column: %s — use %s", column, replacement))
	}
	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.NotEqual(t, "Renamed", member.DisplayName, "a refused sheet changes nothing")
}

func TestImportOrgUsersMemberStatusValuesAndNewUsers(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)

	f := newImportWorkbook(t, [][]any{
		{"username", "member_status", "organization_remark", "initial_password"},
		{"member_21", "disabled", "", ""},
		{"org_new_member", "Suspended", "new hire", "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0], `line 2 (member_21): invalid member_status "disabled" (use active or suspended)`)
	require.Equal(t, 1, result.Created)

	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "org_new_member").First(&created).Error)
	require.Equal(t, common.UserStatusEnabled, created.Status, "new users are always enabled on the platform")
	require.Equal(t, model.OrganizationMemberStatusSuspended, created.OrganizationMemberStatus)
	require.Equal(t, "new hire", created.OrganizationRemark)
	require.Equal(t, "", created.Remark)
}

func TestBuildOrgExportFileWritesMemberStatusAndOrganizationRemark(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 21).Updates(map[string]any{
		"remark": "root note", "organization_remark": "org note",
		"organization_member_status": model.OrganizationMemberStatusSuspended,
	}).Error)

	f, err := BuildOrgExportFile(1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	rows, err := f.GetRows(f.GetSheetName(0))
	require.NoError(t, err)
	require.Equal(t, OrgExcelHeaders, rows[0])
	require.NotContains(t, rows[0], "status")
	require.NotContains(t, rows[0], "remark")
	col := func(name string) int {
		for i, h := range rows[0] {
			if h == name {
				return i
			}
		}
		t.Fatalf("missing column %s", name)
		return -1
	}
	require.Equal(t, "active", rows[1][col("member_status")])
	require.Equal(t, "suspended", rows[2][col("member_status")])
	require.Equal(t, "org note", rows[2][col("organization_remark")])
	require.NotContains(t, rows[2], "root note")
}

// fork: O3 (review M9) — an imported cap is written as one absolute value, not
// as a delta from a quota read earlier in the import.
func TestSetImportedOrganizationMemberQuotaIsAbsolute(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)
	// Concurrent spend after the import read the row: the stored quota is 40 now.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 21).Update("quota", 40).Error)

	require.NoError(t, model.SetOrganizationMemberQuota(21, 1, 500))
	require.Equal(t, 500, getUserQuotaForBillingTest(t, 21))
	// Not a member of organization 2: nothing is written.
	require.NoError(t, model.SetOrganizationMemberQuota(21, 2, 900))
	require.Equal(t, 500, getUserQuotaForBillingTest(t, 21))
}

// fork: O3 (spec §9 decision 6) — an unaffiliated user who still holds a
// personal balance or an active subscription joins only after it is cleared;
// the import reports that row and leaves the user out.
func TestImportOrgUsersRefusesUnaffiliatedUserWithPersonalBalance(t *testing.T) {
	truncate(t)
	withDefaultQuotaPerUnit(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 31, "rich_31", 5000, 0, "")
	seedOrganizationUser(t, 32, "subscriber_32", 0, 0, "")
	seedOrganizationUser(t, 33, "cleared_33", 0, 0, "")
	require.NoError(t, model.DB.Create(&model.UserSubscription{UserId: 32, Status: "active", EndTime: common.GetTimestamp() + 3600}).Error)

	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role"},
		{"rich_31", model.OrganizationRoleMember},
		{"subscriber_32", model.OrganizationRoleMember},
		{"cleared_33", model.OrganizationRoleMember},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2, "%v", result.Errors)
	require.Contains(t, result.Errors[0], "line 2 (rich_31): failed to assign: user still has a personal balance, affiliate balance or subscription")
	require.Contains(t, result.Errors[1], "line 3 (subscriber_32): failed to assign: user still has a personal balance, affiliate balance or subscription")
	require.Equal(t, 1, result.Assigned)

	for id, want := range map[int]int{31: 0, 32: 0, 33: 1} {
		organizationId, err := model.GetUserOrganizationId(id)
		require.NoError(t, err)
		require.Equal(t, want, organizationId, "user %d", id)
	}
}

// An account the import itself creates has no prior owner to consent: its
// sign-up bonus (QuotaForNewUser) must not block the row.
func TestImportOrgUsersAssignsNewUsersDespiteSignUpBonus(t *testing.T) {
	truncate(t)
	withDefaultQuotaPerUnit(t)
	previous := common.QuotaForNewUser
	common.QuotaForNewUser = 1234
	t.Cleanup(func() { common.QuotaForNewUser = previous })
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)

	f := newImportWorkbook(t, [][]any{
		{"username", "organization_role", "initial_password"},
		{"bonus_new", model.OrganizationRoleMember, "password123"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
	var created model.User
	require.NoError(t, model.DB.Where("username = ?", "bonus_new").First(&created).Error)
	require.Equal(t, 1, created.OrganizationId)
}

// Fix round 1 — the existing-member update is scoped to the organization: a
// member removed between the row read and the write is not touched.
func TestImportOrgUsersDoesNotWriteAMemberWhoLeft(t *testing.T) {
	truncate(t)
	seedOrganization(t, 1, 20)
	seedOrganizationUser(t, 20, "owner_20", 0, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 21, "member_21", 100, 1, model.OrganizationRoleMember)

	fired := false
	const name = "test:import_leave_before_update"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "users" {
			return
		}
		fired = true
		tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET organization_id = 0 WHERE id = ?", 21)
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove(name) })

	f := newImportWorkbook(t, [][]any{
		{"username", "display_name", "member_status", "organization_remark"},
		{"member_21", "Renamed", "suspended", "late"},
	})
	result, err := ImportOrgUsersFromFile(f, 1, 0, false)
	require.NoError(t, err)
	require.True(t, fired)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0], "line 2 (member_21): failed to update: "+model.ErrOrganizationMembershipChanged.Error())
	require.Zero(t, result.Updated)

	var member model.User
	require.NoError(t, model.DB.First(&member, 21).Error)
	require.Zero(t, member.OrganizationId)
	require.NotEqual(t, "Renamed", member.DisplayName)
	require.Equal(t, model.OrganizationMemberStatusActive, member.OrganizationMemberStatus)
	require.Equal(t, "", member.OrganizationRemark)
}
