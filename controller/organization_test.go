package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOrganizationControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLOGDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Organization{}, &model.User{}, &model.QuotaData{}, &model.Log{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLOGDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})

	return db
}

func performOrganizationRequest(handler gin.HandlerFunc, actor model.User, method string, path string, body string, params ...gin.Param) *httptest.ResponseRecorder {
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

func TestOrganizationRootCanCreateOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, AffCode: "owner"}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		CreateOrganization,
		root,
		http.MethodPost,
		"/api/organizations",
		fmt.Sprintf(`{"name":"Acme","description":"Customer","owner_user_id":%d,"quota":500}`, owner.Id),
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, owner.Id).Error)
	require.NotZero(t, reloaded.OrganizationId)
	require.Equal(t, model.OrganizationRoleOwner, reloaded.OrganizationRole)

	var org model.Organization
	require.NoError(t, model.DB.First(&org, reloaded.OrganizationId).Error)
	require.Equal(t, 500, org.Quota)
}

func TestOrganizationRootCanUpdateOrganizationQuota(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	org := model.Organization{Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPatch,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		`{"quota":750}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.Organization
	require.NoError(t, model.DB.First(&reloaded, org.Id).Error)
	require.Equal(t, 750, reloaded.Quota)
}

func TestOrganizationAdminCanReadOrganizationProfile(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, UsedQuota: 25, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationProfile,
		owner,
		http.MethodGet,
		"/api/organization",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"quota":1000`)
	require.Contains(t, body, `"used_quota":25`)
}

func TestOrganizationOwnerCanReadDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	otherUser := model.User{Username: "other", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleOwner, AffCode: "other"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, UsedQuota: 25, Status: model.OrganizationStatusEnabled}
	otherOrg := model.Organization{Id: 2, Name: "Other", OwnerUserId: 3, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&otherUser).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&otherOrg).Error)

	dayOne := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Unix()
	dayTwo := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).Unix()
	rows := []model.QuotaData{
		{UserID: owner.Id, Username: owner.Username, ModelName: "gpt-4o", CreatedAt: dayOne, Count: 2, Quota: 150, TokenUsed: 10},
		{UserID: member.Id, Username: member.Username, ModelName: "claude-3-5", CreatedAt: dayTwo, Count: 3, Quota: 200, TokenUsed: 20},
		{UserID: otherUser.Id, Username: otherUser.Username, ModelName: "gpt-4o", CreatedAt: dayOne, Count: 9, Quota: 999, TokenUsed: 90},
	}
	require.NoError(t, model.DB.Create(&rows).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		owner,
		http.MethodGet,
		fmt.Sprintf("/api/organization/dashboard?start_timestamp=%d&end_timestamp=%d", dayOne, dayTwo),
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"period_quota":350`)
	require.Contains(t, body, `"period_requests":5`)
	require.NotContains(t, body, `"period_quota":999`)
}

func TestOrganizationAdminCanReadDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		"/api/organization/dashboard",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestOrganizationRootCanReadSelectedOrganizationDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	otherUser := model.User{Username: "outsider", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "outsider"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 2, Quota: 1000, Status: model.OrganizationStatusEnabled}
	otherOrg := model.Organization{Id: 2, Name: "Other", OwnerUserId: 3, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&otherUser).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&otherOrg).Error)

	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).Unix()
	rows := []model.QuotaData{
		{UserID: member.Id, Username: member.Username, ModelName: "gpt-4o", CreatedAt: now, Count: 2, Quota: 150, TokenUsed: 10},
		{UserID: otherUser.Id, Username: otherUser.Username, ModelName: "gpt-4o", CreatedAt: now, Count: 9, Quota: 999, TokenUsed: 90},
	}
	require.NoError(t, model.DB.Create(&rows).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		root,
		http.MethodGet,
		fmt.Sprintf("/api/organization/dashboard?organization_id=%d&start_timestamp=%d&end_timestamp=%d", org.Id, now, now),
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"period_quota":150`)
	require.NotContains(t, body, `"period_quota":999`)
}

func TestOrganizationRootDashboardRequiresOrganizationId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		root,
		http.MethodGet,
		"/api/organization/dashboard",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization_id is required")
}

func TestOrganizationRootCanReadSelectedOrganizationLogs(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	otherUser := model.User{Username: "outsider", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "outsider"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 2, Quota: 1000, Status: model.OrganizationStatusEnabled}
	otherOrg := model.Organization{Id: 2, Name: "Other", OwnerUserId: 3, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&otherUser).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&otherOrg).Error)
	logs := []model.Log{
		{UserId: member.Id, Username: member.Username, Type: model.LogTypeConsume, CreatedAt: time.Now().Unix(), ModelName: "gpt-4o", Quota: 150, Other: "{}"},
		{UserId: otherUser.Id, Username: otherUser.Username, Type: model.LogTypeConsume, CreatedAt: time.Now().Unix(), ModelName: "claude-3-5", Quota: 999, Other: "{}"},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)

	res := performOrganizationRequest(
		GetOrganizationLogs,
		root,
		http.MethodGet,
		fmt.Sprintf("/api/organization/logs?organization_id=%d", org.Id),
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, member.Username)
	require.NotContains(t, body, otherUser.Username)
}

func TestOrganizationDashboardAcceptsPresetRanges(t *testing.T) {
	for _, preset := range []string{"today", "30d"} {
		t.Run(preset, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
			org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				GetOrganizationDashboard,
				admin,
				http.MethodGet,
				fmt.Sprintf("/api/organization/dashboard?preset=%s", preset),
				"",
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)
		})
	}
}

func TestOrganizationDashboardRejectsInvalidPreset(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		"/api/organization/dashboard?preset=quarter",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "invalid preset")
}

func TestOrganizationDashboardRejectsMalformedStartTimestamp(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		"/api/organization/dashboard?start_timestamp=bad",
		"",
	)

	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "invalid start_timestamp")
}

func TestOrganizationDashboardRejectsMalformedEndTimestamp(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		"/api/organization/dashboard?end_timestamp=bad",
		"",
	)

	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "invalid end_timestamp")
}

func TestOrganizationMemberCannotReadDashboard(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		member,
		http.MethodGet,
		"/api/organization/dashboard",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "organization admin permission required")
}

func TestOrganizationAdminCannotUpdateOutsideOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, Quota: 10, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"quota":100}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 10, reloaded.Quota)
}

func TestOrganizationAdminCanUpdateQuotaForMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 10, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"quota":100,"status":1,"remark":"reviewed"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 100, reloaded.Quota)
	require.Equal(t, "reviewed", reloaded.Remark)
}

func TestOrganizationAdminCannotUpdateGlobalAdmin(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "global", Password: "password", Role: common.RoleAdminUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 10, AffCode: "global"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"quota":100}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 10, reloaded.Quota)
}

func TestOrganizationOwnerCanAssignMemberRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 1, reloaded.OrganizationId)
	require.Equal(t, model.OrganizationRoleMember, reloaded.OrganizationRole)
}

func TestOrganizationOwnerCanListAssignableUsers(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	unassigned := model.User{Username: "unassigned", Password: "password", Role: common.RoleCommonUser, AffCode: "unassigned"}
	otherOrg := model.User{Username: "other", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "other"}
	globalAdmin := model.User{Username: "global", Password: "password", Role: common.RoleAdminUser, AffCode: "global"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&unassigned).Error)
	require.NoError(t, model.DB.Create(&otherOrg).Error)
	require.NoError(t, model.DB.Create(&globalAdmin).Error)

	res := performOrganizationRequest(
		ListAssignableOrganizationUsers,
		owner,
		http.MethodGet,
		"/api/organization/assignable-users",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.NotContains(t, body, `"username":"owner"`)
	require.Contains(t, body, `"username":"member"`)
	require.Contains(t, body, `"username":"unassigned"`)
	require.NotContains(t, body, `"username":"other"`)
	require.NotContains(t, body, `"username":"global"`)
}

func TestOrganizationOwnerCannotAssignSelf(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", owner.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", owner.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, owner.Id).Error)
	require.Equal(t, model.OrganizationRoleOwner, reloaded.OrganizationRole)
}

func TestOrganizationAdminCannotListAssignableUsers(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		ListAssignableOrganizationUsers,
		admin,
		http.MethodGet,
		"/api/organization/assignable-users",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestOrganizationAdminCannotAssignMembership(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestOrganizationOwnerCannotAssignGlobalAdmin(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	target := model.User{Username: "global", Password: "password", Role: common.RoleAdminUser, AffCode: "global"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Zero(t, reloaded.OrganizationId)
	require.Empty(t, reloaded.OrganizationRole)
}

func TestOrganizationOwnerCannotAssignInvalidRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"root"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}
