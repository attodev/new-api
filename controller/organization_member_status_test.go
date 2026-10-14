package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

// Fix round 1 — a suspended member (an admin included) cannot perform any
// write on /api/organization/*; reads keep working and root is unaffected.
func performMemberGuardedRequest(actor model.User, method string, path string, body string) *httptest.ResponseRecorder {
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", actor.Id)
		c.Set("role", actor.Role)
		c.Next()
	})
	engine.Use(RejectDisabledOrganizationMutation())
	engine.GET("/api/organization/subscription/self", GetMyOrganizationSubscription)
	engine.PATCH("/api/organization/users/:id", UpdateOrganizationUser)
	engine.POST("/api/organization/subscription/plans", CreateOrganizationSubscriptionPlan)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestSuspendedOrganizationAdminCannotWrite(t *testing.T) {
	f := setupOrgSecurityTest(t)
	root := model.User{Username: "root_guard", Password: "x", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "aff-root-guard"}
	require.NoError(t, f.db.Create(&root).Error)
	// The owner suspended both the admin and the member.
	for _, id := range []int{f.admin.Id, f.member.Id} {
		require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", id).
			Update("organization_member_status", model.OrganizationMemberStatusSuspended).Error)
	}

	res := performMemberGuardedRequest(f.admin, http.MethodPatch, fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"member_status":1}`)
	require.Equal(t, http.StatusForbidden, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization membership is suspended")
	require.Equal(t, model.OrganizationMemberStatusSuspended, orgSecurityUser(t, f.db, f.member.Id).OrganizationMemberStatus)

	res = performMemberGuardedRequest(f.admin, http.MethodPost, "/api/organization/subscription/plans", `{"title":"Plan","total_amount":10}`)
	require.Equal(t, http.StatusForbidden, res.Code)

	// The suspended member's own reads still work.
	res = performMemberGuardedRequest(f.member, http.MethodGet, "/api/organization/subscription/self", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	// The active owner and root are unaffected.
	res = performMemberGuardedRequest(f.owner, http.MethodPatch, fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"member_status":1}`)
	require.Contains(t, res.Body.String(), `"success":true`)
	res = performMemberGuardedRequest(root, http.MethodPost, fmt.Sprintf("/api/organization/subscription/plans?organization_id=%d", f.org.Id), `{"title":"Plan","total_amount":10}`)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

// Fix round 1 — the member update is scoped to the actor's organization: a
// member who left between the permission check and the write is not touched.
func TestUpdateOrganizationUserIsScopedToTheOrganization(t *testing.T) {
	f := setupOrgSecurityTest(t)
	leaveBeforeUserUpdate(t, f.db, f.member.Id)

	res := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch, "/",
		`{"quota":999,"organization_remark":"late"}`, orgSecurityIdParam(f.member.Id))
	requireOrgSecurityResult(t, res, false, model.ErrOrganizationMembershipChanged.Error())
	fresh := orgSecurityUser(t, f.db, f.member.Id)
	require.Zero(t, fresh.OrganizationId)
	require.Equal(t, 300, fresh.Quota)
	require.Equal(t, "", fresh.OrganizationRemark)
}

// leaveBeforeUserUpdate moves userId out of its organization right before the
// next UPDATE on users runs (inside the same statement's connection), as a
// concurrent membership removal would.
func leaveBeforeUserUpdate(t *testing.T, db *gorm.DB, userId int) {
	t.Helper()
	fired := false
	name := "test:leave_before_update_" + strings.ReplaceAll(t.Name(), "/", "_")
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "users" {
			return
		}
		fired = true
		tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET organization_id = 0 WHERE id = ?", userId)
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(name) })
}

// Review M5 — a non-GET /api/organization/* request reads the actor's
// membership (user row) once: the guard keeps the row it read in the gin context and the handler's
// getOrganizationActor reuses it.
func TestGuardedOrganizationWriteReadsActorRowOnce(t *testing.T) {
	f := setupOrgSecurityTest(t)
	actorReads := 0
	const name = "test:count_actor_user_reads"
	require.NoError(t, f.db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		// Membership reads select organization_id; the audit log's
		// username-only lookup (Redis-cached in production) is not one.
		if tx.Statement.Table == "users" && len(tx.Statement.Vars) > 0 && tx.Statement.Vars[0] == any(f.owner.Id) &&
			strings.Contains(tx.Statement.SQL.String(), "organization_id") {
			actorReads++
		}
	}))
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(name) })

	res := performMemberGuardedRequest(f.owner, http.MethodPatch, fmt.Sprintf("/api/organization/users/%d", f.member.Id), `{"organization_remark":"night shift"}`)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Equal(t, 1, actorReads, "guard and handler must share one read of the actor row")
	require.Equal(t, "night shift", orgSecurityUser(t, f.db, f.member.Id).OrganizationRemark)
}

func TestGetOrganizationActorReusesGuardRow(t *testing.T) {
	f := setupOrgSecurityTest(t)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", f.admin.Id)
	guardRow := f.admin
	guardRow.OrganizationRole = model.OrganizationRoleOwner // differs from the DB row
	ctx.Set(organizationGuardActorKey, &guardRow)

	actor, err := getOrganizationActor(ctx)
	require.NoError(t, err)
	require.Equal(t, model.OrganizationRoleOwner, actor.OrganizationRole, "the guard's row is reused, not re-read")
	actor.OrganizationRole = model.OrganizationRoleMember
	require.Equal(t, model.OrganizationRoleOwner, guardRow.OrganizationRole, "callers get a copy")
}
