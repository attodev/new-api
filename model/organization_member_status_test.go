package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization-scoped member status (plan decision 7).

func TestRequireActiveOrganizationMember(t *testing.T) {
	setupOrganizationModelTestDB(t)
	active := User{Id: 2, Username: "active", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "active-i7"}
	suspended := User{Id: 3, Username: "suspended", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, OrganizationMemberStatus: OrganizationMemberStatusSuspended, AffCode: "suspended-i7"}
	outsider := User{Id: 4, Username: "outsider", Password: "password", Role: common.RoleCommonUser, AffCode: "outsider-i7"}
	require.NoError(t, DB.Create(&active).Error)
	require.NoError(t, DB.Create(&suspended).Error)
	require.NoError(t, DB.Create(&outsider).Error)
	// A stray suspended value outside an organization has no effect.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 4).Update("organization_member_status", OrganizationMemberStatusSuspended).Error)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, 2).Error)
	require.Equal(t, OrganizationMemberStatusActive, reloaded.OrganizationMemberStatus, "new rows default to active")

	require.NoError(t, RequireActiveOrganizationMember(2))
	require.ErrorIs(t, RequireActiveOrganizationMember(3), ErrOrganizationMemberSuspended)
	require.NoError(t, RequireActiveOrganizationMember(4))
}

func TestDeleteOrganizationResetsOwnerMemberStatusAndRemark(t *testing.T) {
	setupOrganizationModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&OrganizationUserSubscription{}, &OrganizationSubscriptionPlan{}, &TopUp{}))
	owner := User{Id: 2, Username: "owner", Password: "password", Role: common.RoleCommonUser, AffCode: "owner-i7"}
	require.NoError(t, DB.Create(&owner).Error)
	org := Organization{Id: 1, Name: "acme", OwnerUserId: 2, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(&org).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 2).Updates(map[string]any{
		"organization_id": 1, "organization_role": OrganizationRoleOwner,
		"organization_member_status": OrganizationMemberStatusSuspended, "organization_remark": "note",
	}).Error)

	require.NoError(t, DeleteOrganization(1))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, 2).Error)
	require.Zero(t, reloaded.OrganizationId)
	require.Equal(t, OrganizationMemberStatusActive, reloaded.OrganizationMemberStatus)
	require.Equal(t, "", reloaded.OrganizationRemark)
}

func TestGetUserOrganizationMembershipAndScopedMemberUpdate(t *testing.T) {
	setupOrganizationModelTestDB(t)
	member := User{Id: 2, Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, OrganizationMemberStatus: OrganizationMemberStatusSuspended, AffCode: "member-fix1"}
	outsider := User{Id: 3, Username: "outsider", Password: "password", Role: common.RoleCommonUser, AffCode: "outsider-fix1"}
	require.NoError(t, DB.Create(&member).Error)
	require.NoError(t, DB.Create(&outsider).Error)

	orgId, status, err := GetUserOrganizationMembership(2)
	require.NoError(t, err)
	require.Equal(t, 1, orgId)
	require.Equal(t, OrganizationMemberStatusSuspended, status)
	orgId, status, err = GetUserOrganizationMembership(3)
	require.NoError(t, err)
	require.Zero(t, orgId)
	require.Equal(t, OrganizationMemberStatusActive, status)

	require.NoError(t, UpdateOrganizationMemberFields(2, 1, map[string]interface{}{"organization_remark": "note"}))
	// Writing the same value again is not "membership changed".
	require.NoError(t, UpdateOrganizationMemberFields(2, 1, map[string]interface{}{"organization_remark": "note"}))
	// Another organization's scope writes nothing.
	require.ErrorIs(t, UpdateOrganizationMemberFields(2, 9, map[string]interface{}{"organization_remark": "other"}), ErrOrganizationMembershipChanged)
	require.ErrorIs(t, UpdateOrganizationMemberFields(3, 1, map[string]interface{}{"organization_remark": "other"}), ErrOrganizationMembershipChanged)
	var reloaded User
	require.NoError(t, DB.First(&reloaded, 2).Error)
	require.Equal(t, "note", reloaded.OrganizationRemark)
	var reloadedOutsider User
	require.NoError(t, DB.First(&reloadedOutsider, 3).Error)
	require.Equal(t, "", reloadedOutsider.OrganizationRemark)
}
