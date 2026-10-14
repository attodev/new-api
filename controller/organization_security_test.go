package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fork: O3 — organization API hardening (self-service quota, owner targeting,
// hash disclosure, quoted ORDER BY).

type orgSecurityFixture struct {
	db     *gorm.DB
	org    model.Organization
	owner  model.User
	admin  model.User
	member model.User
}

func setupOrgSecurityTest(t *testing.T) *orgSecurityFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/org.db"), &gorm.Config{})
	require.NoError(t, err)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.AuditLog{}, &model.TopUp{}, &model.UserSubscription{}))
	require.NoError(t, model.MigrateForkOrganizationTables(db))

	f := &orgSecurityFixture{db: db}
	mk := func(name string, quota int, group string) model.User {
		u := model.User{Username: name, Password: "secret-hash-" + name, DisplayName: name, Email: name + "@example.com",
			Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: quota, Group: group, AffCode: "aff-" + name}
		require.NoError(t, db.Create(&u).Error)
		return u
	}
	f.owner = mk("org_owner", 100, "default")
	f.admin = mk("org_admin", 200, "vip")
	f.member = mk("org_member", 300, "alpha")
	f.org = model.Organization{Name: "acme", OwnerUserId: f.owner.Id, Status: model.OrganizationStatusEnabled, Quota: 1000}
	require.NoError(t, db.Create(&f.org).Error)
	for _, m := range []struct {
		user *model.User
		role string
	}{{&f.owner, model.OrganizationRoleOwner}, {&f.admin, model.OrganizationRoleAdmin}, {&f.member, model.OrganizationRoleMember}} {
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", m.user.Id).Updates(map[string]any{
			"organization_id": f.org.Id, "organization_role": m.role,
		}).Error)
		m.user.OrganizationId, m.user.OrganizationRole = f.org.Id, m.role
	}
	return f
}

func orgSecurityRequest(handler gin.HandlerFunc, actor model.User, method, path, body string, params ...gin.Param) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	ctx.Set("id", actor.Id)
	ctx.Set("role", actor.Role)
	handler(ctx)
	return recorder
}

func orgSecurityIdParam(id int) gin.Param {
	return gin.Param{Key: "id", Value: strconv.Itoa(id)}
}

func requireOrgSecurityResult(t *testing.T, res *httptest.ResponseRecorder, success bool, message string) {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload), res.Body.String())
	require.Equal(t, success, payload.Success, res.Body.String())
	if message != "" {
		require.Equal(t, message, payload.Message)
	}
}

func orgSecurityUser(t *testing.T, db *gorm.DB, id int) model.User {
	t.Helper()
	var user model.User
	require.NoError(t, db.First(&user, id).Error)
	return user
}

func TestOrganizationAdminCannotUpdateOwnMembership(t *testing.T) {
	f := setupOrgSecurityTest(t)
	for _, actor := range []model.User{f.admin, f.owner} {
		res := orgSecurityRequest(UpdateOrganizationUser, actor, http.MethodPatch, "/", `{"quota":999999}`, orgSecurityIdParam(actor.Id))
		requireOrgSecurityResult(t, res, false, "organization admins cannot modify their own membership")
		require.Equal(t, actor.Quota, orgSecurityUser(t, f.db, actor.Id).Quota)
	}
}

func TestOrganizationAdminCannotRemoveOwnMembership(t *testing.T) {
	f := setupOrgSecurityTest(t)
	res := orgSecurityRequest(RemoveOrganizationUserMembership, f.admin, http.MethodDelete, "/", "", orgSecurityIdParam(f.admin.Id))
	requireOrgSecurityResult(t, res, false, "organization admins cannot modify their own membership")
	require.Equal(t, f.org.Id, orgSecurityUser(t, f.db, f.admin.Id).OrganizationId)
}

func TestOrganizationRemovalZeroesMemberQuotaAndAuditsPreviousQuota(t *testing.T) {
	f := setupOrgSecurityTest(t)
	res := orgSecurityRequest(RemoveOrganizationUserMembership, f.admin, http.MethodDelete, "/", "", orgSecurityIdParam(f.member.Id))
	requireOrgSecurityResult(t, res, true, "")

	user := orgSecurityUser(t, f.db, f.member.Id)
	require.Zero(t, user.OrganizationId)
	require.Empty(t, user.OrganizationRole)
	require.Zero(t, user.Quota)

	var audits []model.AuditLog
	require.NoError(t, f.db.Where("action = ?", "organization.user.remove").Find(&audits).Error)
	require.Len(t, audits, 1)
	require.NotNil(t, audits[0].Other.Op)
	requireOrgSecurityAuditParam(t, audits[0], "previous_quota", "300")

	// A stale clear scoped to this organization must not touch a user who has
	// meanwhile joined another organization.
	require.NoError(t, f.db.Model(&model.User{}).Where("id = ?", f.admin.Id).
		Updates(map[string]any{"organization_id": f.org.Id + 1, "organization_role": model.OrganizationRoleMember}).Error)
	_, err := model.ClearOrganizationMembership(f.admin.Id, f.org.Id)
	require.ErrorIs(t, err, model.ErrOrganizationMembershipChanged)
	require.EqualError(t, err, "user is no longer a member of this organization")
	moved := orgSecurityUser(t, f.db, f.admin.Id)
	require.Equal(t, f.org.Id+1, moved.OrganizationId)
	require.Equal(t, model.OrganizationRoleMember, moved.OrganizationRole)
	require.Equal(t, 200, moved.Quota)
}

func TestOrganizationDeleteZeroesOwnerQuota(t *testing.T) {
	f := setupOrgSecurityTest(t)
	require.NoError(t, f.db.Model(&model.User{}).Where("id IN ?", []int{f.admin.Id, f.member.Id}).
		Updates(map[string]any{"organization_id": 0, "organization_role": ""}).Error)
	root := model.User{Id: 9999, Role: common.RoleRootUser}
	res := orgSecurityRequest(DeleteOrganization, root, http.MethodDelete, "/", "", orgSecurityIdParam(f.org.Id))
	requireOrgSecurityResult(t, res, true, "")

	owner := orgSecurityUser(t, f.db, f.owner.Id)
	require.Zero(t, owner.OrganizationId)
	require.Zero(t, owner.Quota)
	var audits []model.AuditLog
	require.NoError(t, f.db.Where("action = ?", "organization.delete").Find(&audits).Error)
	require.Len(t, audits, 1)
	requireOrgSecurityAuditParam(t, audits[0], "previous_quota", "100")
}

func TestOrganizationUserListsDoNotDiscloseSecrets(t *testing.T) {
	f := setupOrgSecurityTest(t)
	outsider := model.User{Username: "free_user", Password: "secret-hash-free", DisplayName: "free", Email: "free@example.com",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "aff-free"}
	require.NoError(t, f.db.Create(&outsider).Error)

	for name, request := range map[string]struct {
		handler gin.HandlerFunc
		path    string
	}{
		"list":   {ListOrganizationUsers, "/?p=1&page_size=10"},
		"lookup": {LookupOrganizationMember, "/?username=free_user"},
	} {
		res := orgSecurityRequest(request.handler, f.admin, http.MethodGet, request.path, "")
		requireOrgSecurityResult(t, res, true, "")
		body := res.Body.String()
		require.Contains(t, body, `"username"`, name)
		require.NotContains(t, body, `"password"`, name)
		require.NotContains(t, body, `"email"`, name)
		require.NotContains(t, body, "secret-hash", name)
		require.NotContains(t, body, "@example.com", name)
	}
}

func TestOrganizationUsersOrderByGroup(t *testing.T) {
	f := setupOrgSecurityTest(t)
	res := orgSecurityRequest(ListOrganizationUsers, f.admin, http.MethodGet, "/?order_by=group&order_dir=desc", "")
	requireOrgSecurityResult(t, res, true, "")
	var payload struct {
		Data struct {
			Items []organizationUserView `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	groups := []string{}
	for _, item := range payload.Data.Items {
		groups = append(groups, item.Group)
	}
	require.Equal(t, []string{"vip", "default", "alpha"}, groups)
}

func TestOrganizationAdminCannotActOnOwner(t *testing.T) {
	f := setupOrgSecurityTest(t)
	ownerParam := orgSecurityIdParam(f.owner.Id)
	const denied = "only the organization owner can manage the owner"

	res := orgSecurityRequest(UpdateOrganizationUser, f.admin, http.MethodPatch, "/", `{"status":2}`, ownerParam)
	requireOrgSecurityResult(t, res, false, denied)
	res = orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"member"}`, ownerParam)
	requireOrgSecurityResult(t, res, false, denied)
	res = orgSecurityRequest(RemoveOrganizationUserMembership, f.admin, http.MethodDelete, "/", "", ownerParam)
	requireOrgSecurityResult(t, res, false, "organization owner cannot be removed")
	res = orgSecurityRequest(AssignOrganizationUserSubscription, f.admin, http.MethodPut, "/", `{"plan_id":1}`, ownerParam)
	requireOrgSecurityResult(t, res, false, denied)
	res = orgSecurityRequest(CancelOrganizationUserSubscription, f.admin, http.MethodDelete, "/", "", ownerParam)
	requireOrgSecurityResult(t, res, false, denied)

	owner := orgSecurityUser(t, f.db, f.owner.Id)
	require.Equal(t, common.UserStatusEnabled, owner.Status)
	require.Equal(t, model.OrganizationRoleOwner, owner.OrganizationRole)
}

func TestOrganizationOwnerRoleCannotBeGranted(t *testing.T) {
	f := setupOrgSecurityTest(t)
	for _, actor := range []model.User{f.owner, f.admin} {
		res := orgSecurityRequest(AssignOrganizationUser, actor, http.MethodPut, "/", `{"organization_role":"owner"}`, orgSecurityIdParam(f.member.Id))
		requireOrgSecurityResult(t, res, false, "ownership is assigned when the organization is created")
	}
	require.Equal(t, model.OrganizationRoleMember, orgSecurityUser(t, f.db, f.member.Id).OrganizationRole)

	// Admins may still toggle member <-> admin.
	res := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"admin"}`, orgSecurityIdParam(f.member.Id))
	requireOrgSecurityResult(t, res, true, "")
	require.Equal(t, model.OrganizationRoleAdmin, orgSecurityUser(t, f.db, f.member.Id).OrganizationRole)
}

func TestOrganizationAssignRejectsUnknownFields(t *testing.T) {
	f := setupOrgSecurityTest(t)
	res := orgSecurityRequest(AssignOrganizationUser, f.admin, http.MethodPut, "/", `{"organization_role":"admin","quota":5}`, orgSecurityIdParam(f.member.Id))
	requireOrgSecurityResult(t, res, false, "unsupported field: quota")
}

func TestOrganizationPaymentWrapperRejectsWithPaymentErrorShape(t *testing.T) {
	f := setupOrgSecurityTest(t)
	res := orgSecurityRequest(RequestOrganizationAmount, f.member, http.MethodPost, "/", `{"amount":10}`)
	require.Equal(t, http.StatusOK, res.Code)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.Equal(t, "error", payload["message"])
	require.Equal(t, "organization owner permission required", payload["data"])
}

func requireOrgSecurityAuditParam(t *testing.T, audit model.AuditLog, key string, wantJSON string) {
	t.Helper()
	require.NotNil(t, audit.Other.Op)
	raw, err := common.Marshal(audit.Other.Op.Params[key])
	require.NoError(t, err)
	require.JSONEq(t, wantJSON, string(raw))
}

func TestOrganizationRootCanManageOwnerSubscription(t *testing.T) {
	f := setupOrgSecurityTest(t)
	root := model.User{Username: "platform_root", Password: "x", DisplayName: "root", Role: common.RoleRootUser,
		Status: common.UserStatusEnabled, AffCode: "aff-root"}
	require.NoError(t, f.db.Create(&root).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: f.org.Id, Title: "team", Enabled: true,
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 50, QuotaResetPeriod: model.SubscriptionResetNever}
	require.NoError(t, f.db.Create(&plan).Error)
	ownerParam := orgSecurityIdParam(f.owner.Id)
	query := "/?organization_id=" + strconv.Itoa(f.org.Id)
	body := `{"plan_id":` + strconv.Itoa(plan.Id) + `}`

	// A non-owner organization admin is still rejected.
	res := orgSecurityRequest(AssignOrganizationUserSubscription, f.admin, http.MethodPut, "/", body, ownerParam)
	requireOrgSecurityResult(t, res, false, "only the organization owner can manage the owner")

	res = orgSecurityRequest(AssignOrganizationUserSubscription, root, http.MethodPut, query, body, ownerParam)
	requireOrgSecurityResult(t, res, true, "")
	sub, err := model.GetActiveOrganizationUserSubscription(f.org.Id, f.owner.Id)
	require.NoError(t, err)
	require.Equal(t, plan.Id, sub.PlanId)

	res = orgSecurityRequest(CancelOrganizationUserSubscription, f.admin, http.MethodDelete, "/", "", ownerParam)
	requireOrgSecurityResult(t, res, false, "only the organization owner can manage the owner")
	res = orgSecurityRequest(CancelOrganizationUserSubscription, root, http.MethodDelete, query, "", ownerParam)
	requireOrgSecurityResult(t, res, true, "")
	_, err = model.GetActiveOrganizationUserSubscription(f.org.Id, f.owner.Id)
	require.Error(t, err)
}
