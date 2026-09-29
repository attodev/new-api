package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Root must be able to add and remove members of organisations it does not belong to --
// that is how the webui console manages organisations with a root session. The rule is the
// one the list/import routes in this file already use via resolveOrganizationAdminTarget:
// root names the target with ?organization_id=, everyone else acts on their own organisation.
func seedMembershipFixture(t *testing.T) (root, orgAdmin, target model.User, org model.Organization) {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	root = model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	orgAdmin = model.User{Username: "orgadmin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "orgadmin"}
	target = model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	org = model.Organization{Id: 1, Name: "Acme", OwnerUserId: 2, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&orgAdmin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	return
}

func TestOrganizationRootCanAssignMemberToAnyOrganization(t *testing.T) {
	root, _, target, org := seedMembershipFixture(t)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		root,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership?organization_id=%d", target.Id, org.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var after model.User
	require.NoError(t, model.DB.First(&after, target.Id).Error)
	require.Equal(t, org.Id, after.OrganizationId)
	require.Equal(t, model.OrganizationRoleMember, after.OrganizationRole)
}

func TestOrganizationRootAssignRequiresOrganizationId(t *testing.T) {
	root, _, target, _ := seedMembershipFixture(t)
	res := performOrganizationRequest(
		AssignOrganizationUser,
		root,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgOrgIdRequired))
}

func TestOrganizationRootCanRemoveMemberFromAnyOrganization(t *testing.T) {
	root, _, target, org := seedMembershipFixture(t)
	require.NoError(t, model.DB.Model(&target).Updates(map[string]any{"organization_id": org.Id, "organization_role": model.OrganizationRoleMember}).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		root,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/users/%d/membership?organization_id=%d", target.Id, org.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var after model.User
	require.NoError(t, model.DB.First(&after, target.Id).Error)
	require.Equal(t, 0, after.OrganizationId)
}

func TestOrganizationAdminStillManagesOnlyOwnOrganization(t *testing.T) {
	// An organisation admin is unchanged: they add to their own organisation, and a query
	// naming another organisation is ignored (resolveOrganizationAdminTarget returns the
	// actor's own organisation for non-root callers).
	_, orgAdmin, target, org := seedMembershipFixture(t)
	otherOrg := model.Organization{Id: 2, Name: "Other", OwnerUserId: 3, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&otherOrg).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		orgAdmin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership?organization_id=%d", target.Id, otherOrg.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	var after model.User
	require.NoError(t, model.DB.First(&after, target.Id).Error)
	require.Equal(t, org.Id, after.OrganizationId) // their own organisation (1), not 2
}

func TestOrganizationCommonUserCannotAssign(t *testing.T) {
	_, _, target, org := seedMembershipFixture(t)
	nobody := model.User{Username: "nobody", Password: "password", Role: common.RoleCommonUser, AffCode: "nobody"}
	require.NoError(t, model.DB.Create(&nobody).Error)
	res := performOrganizationRequest(
		AssignOrganizationUser,
		nobody,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership?organization_id=%d", target.Id, org.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprint(target.Id)},
	)
	requireOrganizationApiError(t, res, i18n.Translate(i18n.LangEn, i18n.MsgOrgAdminRequired))
}
