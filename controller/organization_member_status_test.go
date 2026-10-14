package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization admins manage an organization-scoped member status
// and note (users.organization_member_status / users.organization_remark),
// never the platform users.status / users.remark (plan decision 7).

func TestSuspendedOrganizationMemberRelayRejectedButSelfReadsAllowed(t *testing.T) {
	f := setupOrgSecurityTest(t)
	idParam := gin.Param{Key: "id", Value: strconv.Itoa(f.member.Id)}

	resp := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"member_status":2}`, idParam)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"success":true`)

	fresh, err := model.GetUserById(f.member.Id, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, fresh.Status) // the platform status is untouched
	require.Equal(t, model.OrganizationMemberStatusSuspended, fresh.OrganizationMemberStatus)

	require.ErrorIs(t, model.RequireActiveOrganizationMember(f.member.Id), model.ErrOrganizationMemberSuspended)

	resp = orgSecurityRequest(GetMyOrganizationSubscription, f.member, http.MethodGet, "/api/organization/subscription/self", "")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `"success":true`)

	// Resume.
	resp = orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"member_status":1}`, idParam)
	require.Contains(t, resp.Body.String(), `"success":true`)
	require.NoError(t, model.RequireActiveOrganizationMember(f.member.Id))
}

func TestOrganizationAdminCannotWritePlatformStatusOrRemark(t *testing.T) {
	f := setupOrgSecurityTest(t)
	idParam := gin.Param{Key: "id", Value: strconv.Itoa(f.member.Id)}
	resp := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"status":1}`, idParam)
	require.Contains(t, resp.Body.String(), "unsupported field")
	resp = orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"remark":"x"}`, idParam)
	require.Contains(t, resp.Body.String(), "unsupported field")
}

func TestOrganizationAdminWritesOrganizationRemarkOnly(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", f.member.Id).Update("remark", "root note").Error)
	idParam := gin.Param{Key: "id", Value: strconv.Itoa(f.member.Id)}

	resp := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch, "/",
		`{"organization_remark":"  team lead  "}`, idParam)
	requireOrgSecurityResult(t, resp, true, "")
	fresh := orgSecurityUser(t, f.db, f.member.Id)
	require.Equal(t, "team lead", fresh.OrganizationRemark)
	require.Equal(t, "root note", fresh.Remark, "the platform remark stays root-only")

	resp = orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch, "/",
		fmt.Sprintf(`{"organization_remark":%q}`, strings.Repeat("a", MaxOrganizationRemarkLength+1)), idParam)
	requireOrgSecurityResult(t, resp, false, "organization_remark must be at most 255 characters")

	for _, body := range []string{`{"member_status":0}`, `{"member_status":3}`} {
		resp = orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch, "/", body, idParam)
		requireOrgSecurityResult(t, resp, false, OrganizationMemberStatusAllowedError)
	}
}

func TestOrganizationUserViewShowsMemberStatusAndOrganizationRemark(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", f.member.Id).Updates(map[string]any{
		"remark": "root note", "organization_member_status": model.OrganizationMemberStatusSuspended, "organization_remark": "org note",
	}).Error)

	resp := orgSecurityRequest(ListOrganizationUsers, f.admin, http.MethodGet, "/api/organization/users/", "")
	body := resp.Body.String()
	require.Contains(t, body, `"member_status":2`)
	require.Contains(t, body, `"organization_remark":"org note"`)
	require.NotContains(t, body, "root note")

	resp = orgSecurityRequest(GetOrganizationUser, f.admin, http.MethodGet, "/", "", orgSecurityIdParam(f.member.Id))
	body = resp.Body.String()
	require.Contains(t, body, `"member_status":2`)
	require.Contains(t, body, `"organization_remark":"org note"`)
	require.NotContains(t, body, "root note")
}

func TestClearMembershipResetsMemberStatusAndRemark(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", f.member.Id).
		Updates(map[string]any{"organization_member_status": 2, "organization_remark": "late"}).Error)
	_, err := model.ClearOrganizationMembership(f.member.Id, f.org.Id)
	require.NoError(t, err)
	fresh, err := model.GetUserById(f.member.Id, false)
	require.NoError(t, err)
	require.Equal(t, model.OrganizationMemberStatusActive, fresh.OrganizationMemberStatus)
	require.Equal(t, "", fresh.OrganizationRemark)
}
