// fork: O13 — root manages members of any organization through ?organization_id=
package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Root must be able to add, update and remove members of organizations it does not belong to
// (the Open WebUI console manages organizations with a root session). The rule is the one the
// list/import routes already use via resolveOrganizationAdminTarget: root names the target with
// ?organization_id=, everyone else acts on their own organization.
type membershipFixture struct {
	root, orgAdmin, owner, target, nobody model.User
	org, otherOrg                         model.Organization
}

func seedMembershipFixture(t *testing.T) membershipFixture {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	f := membershipFixture{
		root:     model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"},
		orgAdmin: model.User{Username: "orgadmin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "orgadmin"},
		owner:    model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"},
		target:   model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"},
		nobody:   model.User{Username: "nobody", Password: "password", Role: common.RoleCommonUser, AffCode: "nobody"},
	}
	for _, u := range []*model.User{&f.root, &f.orgAdmin, &f.owner, &f.target, &f.nobody} {
		require.NoError(t, model.DB.Create(u).Error)
	}
	f.org = model.Organization{Id: 1, Name: "Acme", OwnerUserId: f.owner.Id, Quota: 1000, Status: model.OrganizationStatusEnabled}
	f.otherOrg = model.Organization{Id: 2, Name: "Other", OwnerUserId: f.nobody.Id, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&f.org).Error)
	require.NoError(t, model.DB.Create(&f.otherOrg).Error)
	return f
}

func membershipRequest(handler gin.HandlerFunc, actor model.User, method string, target model.User, query string, body string) string {
	res := performOrganizationRequest(
		handler,
		actor,
		method,
		fmt.Sprintf("/api/organization/users/%d/membership%s", target.Id, query),
		body,
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	if res.Code != http.StatusOK {
		return fmt.Sprintf("HTTP %d", res.Code)
	}
	return res.Body.String()
}

func reloadMembershipUser(t *testing.T, id int) model.User {
	t.Helper()
	var u model.User
	require.NoError(t, model.DB.First(&u, id).Error)
	return u
}

func TestOrganizationRootCanAssignMemberToAnyOrganization(t *testing.T) {
	f := seedMembershipFixture(t)
	body := membershipRequest(AssignOrganizationUser, f.root, http.MethodPut, f.target, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), `{"organization_role":"member"}`)
	require.Contains(t, body, `"success":true`)

	after := reloadMembershipUser(t, f.target.Id)
	require.Equal(t, f.otherOrg.Id, after.OrganizationId)
	require.Equal(t, model.OrganizationRoleMember, after.OrganizationRole)
}

func TestOrganizationRootAssignRequiresOrganizationId(t *testing.T) {
	f := seedMembershipFixture(t)
	body := membershipRequest(AssignOrganizationUser, f.root, http.MethodPut, f.target, "", `{"organization_role":"member"}`)
	require.Contains(t, body, `"success":false`)
	require.Contains(t, body, "organization_id is required")
	require.Equal(t, 0, reloadMembershipUser(t, f.target.Id).OrganizationId)
}

func TestOrganizationRootCanUpdateMemberOfAnyOrganization(t *testing.T) {
	f := seedMembershipFixture(t)
	require.NoError(t, model.DB.Model(&f.target).Updates(map[string]any{"organization_id": f.org.Id, "organization_role": model.OrganizationRoleMember}).Error)

	body := membershipRequest(UpdateOrganizationUser, f.root, http.MethodPut, f.target, fmt.Sprintf("?organization_id=%d", f.org.Id), `{"organization_remark":"by root"}`)
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "by root", reloadMembershipUser(t, f.target.Id).OrganizationRemark)
}

func TestOrganizationRootUpdateRefusedWithoutOrganizationIdOrForeignMember(t *testing.T) {
	f := seedMembershipFixture(t)
	require.NoError(t, model.DB.Model(&f.target).Updates(map[string]any{"organization_id": f.org.Id, "organization_role": model.OrganizationRoleMember}).Error)

	body := membershipRequest(UpdateOrganizationUser, f.root, http.MethodPut, f.target, "", `{"organization_remark":"x"}`)
	require.Contains(t, body, "organization_id is required")

	// The named organization must be the member's own: root cannot edit org 1's member via org 2.
	body = membershipRequest(UpdateOrganizationUser, f.root, http.MethodPut, f.target, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), `{"organization_remark":"x"}`)
	require.Contains(t, body, `"success":false`)
	require.Empty(t, reloadMembershipUser(t, f.target.Id).OrganizationRemark)
}

func TestOrganizationRootCanRemoveMemberFromAnyOrganization(t *testing.T) {
	f := seedMembershipFixture(t)
	require.NoError(t, model.DB.Model(&f.target).Updates(map[string]any{"organization_id": f.org.Id, "organization_role": model.OrganizationRoleMember}).Error)

	body := membershipRequest(RemoveOrganizationUserMembership, f.root, http.MethodDelete, f.target, fmt.Sprintf("?organization_id=%d", f.org.Id), "")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, 0, reloadMembershipUser(t, f.target.Id).OrganizationId)
}

func TestOrganizationRootRemoveRequiresOrganizationIdAndMatchingOrganization(t *testing.T) {
	f := seedMembershipFixture(t)
	require.NoError(t, model.DB.Model(&f.target).Updates(map[string]any{"organization_id": f.org.Id, "organization_role": model.OrganizationRoleMember}).Error)

	body := membershipRequest(RemoveOrganizationUserMembership, f.root, http.MethodDelete, f.target, "", "")
	require.Contains(t, body, "organization_id is required")
	body = membershipRequest(RemoveOrganizationUserMembership, f.root, http.MethodDelete, f.target, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), "")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, f.org.Id, reloadMembershipUser(t, f.target.Id).OrganizationId)
}

func TestOrganizationAdminStillManagesOnlyOwnOrganization(t *testing.T) {
	// A non-root caller ignores ?organization_id=: resolveOrganizationAdminTarget returns the
	// actor's own organization.
	f := seedMembershipFixture(t)
	body := membershipRequest(AssignOrganizationUser, f.orgAdmin, http.MethodPut, f.target, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), `{"organization_role":"member"}`)
	require.Contains(t, body, `"success":true`)
	require.Equal(t, f.org.Id, reloadMembershipUser(t, f.target.Id).OrganizationId) // org 1, not 2

	// ...and cannot update or remove a member of another organization.
	foreign := model.User{Username: "foreign", Password: "password", Role: common.RoleCommonUser, OrganizationId: f.otherOrg.Id, OrganizationRole: model.OrganizationRoleMember, AffCode: "foreign"}
	require.NoError(t, model.DB.Create(&foreign).Error)
	body = membershipRequest(UpdateOrganizationUser, f.orgAdmin, http.MethodPut, foreign, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), `{"organization_remark":"x"}`)
	require.Contains(t, body, `"success":false`)
	body = membershipRequest(RemoveOrganizationUserMembership, f.orgAdmin, http.MethodDelete, foreign, fmt.Sprintf("?organization_id=%d", f.otherOrg.Id), "")
	require.Contains(t, body, `"success":false`)
	after := reloadMembershipUser(t, foreign.Id)
	require.Equal(t, f.otherOrg.Id, after.OrganizationId)
	require.Empty(t, after.OrganizationRemark)
}

func TestOrganizationCommonUserCannotManageMembers(t *testing.T) {
	f := seedMembershipFixture(t)
	q := fmt.Sprintf("?organization_id=%d", f.org.Id)
	for name, handler := range map[string]gin.HandlerFunc{
		"assign": AssignOrganizationUser,
		"update": UpdateOrganizationUser,
		"remove": RemoveOrganizationUserMembership,
	} {
		body := membershipRequest(handler, f.nobody, http.MethodPut, f.target, q, `{"organization_role":"member","organization_remark":"x"}`)
		require.Contains(t, body, "organization admin permission required", name)
	}
	require.Equal(t, 0, reloadMembershipUser(t, f.target.Id).OrganizationId)
}

func TestOrganizationOwnerRoleCannotBeAssignedByRootOrOwner(t *testing.T) {
	// develop assigns ownership only when the organization is created, so neither root nor the
	// owner can grant the owner role here; an admin is refused too.
	f := seedMembershipFixture(t)
	q := fmt.Sprintf("?organization_id=%d", f.org.Id)
	for name, actor := range map[string]model.User{"root": f.root, "owner": f.owner, "admin": f.orgAdmin} {
		body := membershipRequest(AssignOrganizationUser, actor, http.MethodPut, f.target, q, `{"organization_role":"owner"}`)
		require.Contains(t, body, `"success":false`, name)
		require.Contains(t, body, "ownership is assigned when the organization is created", name)
	}
	require.Equal(t, 0, reloadMembershipUser(t, f.target.Id).OrganizationId)
}

func TestOrganizationRootCannotManageOwnerOrGlobalAdmin(t *testing.T) {
	f := seedMembershipFixture(t)
	q := fmt.Sprintf("?organization_id=%d", f.org.Id)
	body := membershipRequest(UpdateOrganizationUser, f.root, http.MethodPut, f.owner, q, `{"organization_remark":"x"}`)
	require.Contains(t, body, "only the organization owner can manage the owner")
	body = membershipRequest(RemoveOrganizationUserMembership, f.root, http.MethodDelete, f.owner, q, "")
	require.Contains(t, body, "organization owner cannot be removed")
	require.Equal(t, f.org.Id, reloadMembershipUser(t, f.owner.Id).OrganizationId)

	global := model.User{Username: "gadmin", Password: "password", Role: common.RoleAdminUser, OrganizationId: f.org.Id, OrganizationRole: model.OrganizationRoleMember, AffCode: "gadmin"}
	require.NoError(t, model.DB.Create(&global).Error)
	body = membershipRequest(UpdateOrganizationUser, f.root, http.MethodPut, global, q, `{"organization_remark":"x"}`)
	require.Contains(t, body, `"success":false`)
	require.Equal(t, f.org.Id, reloadMembershipUser(t, global.Id).OrganizationId)
}
