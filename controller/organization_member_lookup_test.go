package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization admins add members by exact username/email lookup,
// and a user who still holds a personal balance or subscription cannot be
// assigned (spec §9 decision 6).

func TestOrganizationMemberLookupIsExactMatchOnly(t *testing.T) {
	f := setupOrgSecurityTest(t)
	free := model.User{Username: "alice-free", AffCode: "aff-alice-free", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&free).Error)
	require.NoError(t, f.db.Create(&model.User{Username: "alice-two", AffCode: "aff-alice-two", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}).Error)

	resp := orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?username=alice", "")
	require.Equal(t, http.StatusNotFound, resp.Code)

	resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?username=alice-free", "")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"username":"alice-free"`)
	require.NotContains(t, resp.Body.String(), `"quota"`)
	require.NotContains(t, resp.Body.String(), `"remark"`)
}

func TestOrganizationMemberLookupByEmailAndScope(t *testing.T) {
	f := setupOrgSecurityTest(t)
	free := model.User{Username: "bob-free", AffCode: "aff-bob-free", Email: "Bob@Example.com", Password: "12345678", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Quota: 77, Remark: "secret remark"}
	require.NoError(t, f.db.Create(&free).Error)
	other := model.Organization{Name: "other", OwnerUserId: free.Id, Status: model.OrganizationStatusEnabled}
	require.NoError(t, f.db.Create(&other).Error)
	elsewhere := model.User{Username: "carol-other", AffCode: "aff-carol-other", Email: "carol@example.com", Password: "12345678", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, OrganizationId: other.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, f.db.Create(&elsewhere).Error)
	platformAdmin := model.User{Username: "plat-admin", AffCode: "aff-plat-admin", Email: "plat@example.com", Password: "12345678", Role: common.RoleAdminUser,
		Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&platformAdmin).Error)

	// Email matches after normalization (trim + lower case), never by prefix.
	resp := orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?email=bob@example.com", "")
	require.Equal(t, http.StatusOK, resp.Code)
	var payload struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(resp.Body.Bytes(), &payload), resp.Body.String())
	require.True(t, payload.Success)
	require.Equal(t, map[string]interface{}{
		"id": float64(free.Id), "username": "bob-free", "display_name": "", "organization_id": float64(0),
	}, payload.Data)
	resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?email=bob@", "")
	require.Equal(t, http.StatusNotFound, resp.Code)

	// A member of the caller's organization is found; another organization's
	// member and platform admins are not.
	resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?username=org_member", "")
	require.Equal(t, http.StatusOK, resp.Code)
	for _, query := range []string{"username=carol-other", "email=carol@example.com", "username=plat-admin", "email=plat@example.com"} {
		resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?"+query, "")
		require.Equal(t, http.StatusNotFound, resp.Code, query)
		require.Contains(t, resp.Body.String(), "user not found", query)
	}

	// Exactly one of username/email is required.
	for _, query := range []string{"", "username=", "username=bob-free&email=bob@example.com"} {
		resp = orgSecurityRequest(LookupOrganizationMember, f.admin, http.MethodGet, "/api/organization/users/lookup?"+query, "")
		requireOrgSecurityResult(t, resp, false, "provide either a username or an email")
	}

	// Plain members cannot look anyone up.
	resp = orgSecurityRequest(LookupOrganizationMember, f.member, http.MethodGet, "/api/organization/users/lookup?username=bob-free", "")
	requireOrgSecurityResult(t, resp, false, "organization admin permission required")
}

func TestAssignRefusesUserWithPersonalBalance(t *testing.T) {
	f := setupOrgSecurityTest(t)
	rich := model.User{Username: "rich", AffCode: "aff-rich", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 5000}
	require.NoError(t, f.db.Create(&rich).Error)

	resp := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", rich.Id), `{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(rich.Id)})
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "personal balance")
	orgId, err := model.GetUserOrganizationId(rich.Id)
	require.NoError(t, err)
	require.Equal(t, 0, orgId)
}

func TestAssignRefusesUserWithActiveSubscription(t *testing.T) {
	f := setupOrgSecurityTest(t)
	sub := model.User{Username: "subscriber", AffCode: "aff-subscriber", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&sub).Error)
	require.NoError(t, f.db.Create(&model.UserSubscription{UserId: sub.Id, Status: "active", EndTime: time.Now().Add(time.Hour).Unix()}).Error)

	resp := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", sub.Id), `{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(sub.Id)})
	require.Contains(t, resp.Body.String(), "personal balance")
}

func TestAssignAcceptsClearedUserAndKeepsRoleChangesForMembers(t *testing.T) {
	f := setupOrgSecurityTest(t)
	// An expired or cancelled subscription is not a personal balance.
	cleared := model.User{Username: "cleared", AffCode: "aff-cleared", Password: "12345678", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&cleared).Error)
	require.NoError(t, f.db.Create(&model.UserSubscription{UserId: cleared.Id, Status: "active", EndTime: time.Now().Add(-time.Hour).Unix()}).Error)
	require.NoError(t, f.db.Create(&model.UserSubscription{UserId: cleared.Id, Status: "cancelled", EndTime: time.Now().Add(time.Hour).Unix()}).Error)

	resp := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, orgSecurityIdParam(cleared.Id))
	requireOrgSecurityResult(t, resp, true, "")
	orgId, err := model.GetUserOrganizationId(cleared.Id)
	require.NoError(t, err)
	require.Equal(t, f.org.Id, orgId)

	// A current member's quota is an organization allowance, not a personal
	// balance: changing the member's role still works.
	require.Positive(t, f.member.Quota)
	resp = orgSecurityRequest(AssignOrganizationUser, f.owner, http.MethodPut, "/", `{"organization_role":"admin"}`, orgSecurityIdParam(f.member.Id))
	requireOrgSecurityResult(t, resp, true, "")
	require.Equal(t, model.OrganizationRoleAdmin, orgSecurityUser(t, f.db, f.member.Id).OrganizationRole)
}
