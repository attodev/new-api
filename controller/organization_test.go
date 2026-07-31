package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
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
	originalCryptoSecret := common.CryptoSecret
	originalTranslateMessage := common.TranslateMessage
	originalTossConfig := setting.GetTossConfigSnapshot()

	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	common.TranslateMessage = func(c *gin.Context, key string, args ...map[string]any) string {
		return i18n.Translate(i18n.LangEn, key, args...)
	}
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	t.Setenv("CRYPTO_SECRET", "organization-controller-test-secret")
	common.CryptoSecret = "organization-controller-test-secret"

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	// Every test database is a distinct authoritative configuration store. Do
	// not carry a revision observed from a previous in-memory database into it.
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(nil, ""))
	require.NoError(t, db.AutoMigrate(
		&model.Organization{},
		&model.User{},
		&model.QuotaData{},
		&model.Log{},
		&model.OrganizationSubscriptionPlan{},
		&model.OrganizationUserSubscription{},
		&model.UserSubscription{},
		&model.UserBillingKey{},
		&model.WalletAutoRecharge{},
	))

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
		common.CryptoSecret = originalCryptoSecret
		common.TranslateMessage = originalTranslateMessage
		restoreTossConfigForOptionTest(t, originalTossConfig)
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

func requireOrganizationApiError(t *testing.T, res *httptest.ResponseRecorder, message string) {
	t.Helper()
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, message, payload.Message)
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
	require.Equal(t, int64(500), org.Quota)
}

func TestOrganizationRootAcceptsCreateBoundaryInput(t *testing.T) {
	for _, tc := range []struct {
		name        string
		orgName     string
		description string
		quota       int64
		ownerId     int
	}{
		{
			name:        "minimum quota and shortest name",
			orgName:     "A",
			description: "",
			quota:       0,
			ownerId:     1,
		},
		{
			name:        "maximum lengths quota and owner id",
			orgName:     strings.Repeat("가", MaxOrganizationNameLength),
			description: strings.Repeat("나", MaxOrganizationDescriptionLength),
			quota:       MaxOrganizationQuota,
			ownerId:     MaxOrganizationReferenceId,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Id: 10, Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-boundary-" + tc.name}
			owner := model.User{Id: tc.ownerId, Username: "owner", Password: "password", Role: common.RoleCommonUser, AffCode: "owner-boundary-" + tc.name}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&owner).Error)

			res := performOrganizationRequest(
				CreateOrganization,
				root,
				http.MethodPost,
				"/api/organizations",
				fmt.Sprintf(`{"name":%q,"description":%q,"owner_user_id":%d,"quota":%d}`, tc.orgName, tc.description, tc.ownerId, tc.quota),
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)

			var org model.Organization
			require.NoError(t, model.DB.First(&org, "owner_user_id = ?", tc.ownerId).Error)
			require.Equal(t, tc.orgName, org.Name)
			require.Equal(t, tc.description, org.Description)
			require.Equal(t, tc.quota, org.Quota)
		})
	}
}

func TestOrganizationRootRejectsInvalidCreateInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "name too long",
			body:    fmt.Sprintf(`{"name":%q,"owner_user_id":1}`, strings.Repeat("a", MaxOrganizationNameLength+1)),
			message: "name must be at most 64 characters",
		},
		{
			name:    "name empty",
			body:    `{"name":"   ","owner_user_id":1}`,
			message: "name is required",
		},
		{
			name:    "description too long",
			body:    fmt.Sprintf(`{"name":"Acme","description":%q,"owner_user_id":1}`, strings.Repeat("a", MaxOrganizationDescriptionLength+1)),
			message: "description must be at most 255 characters",
		},
		{
			name:    "owner id below minimum",
			body:    `{"name":"Acme","owner_user_id":0}`,
			message: "owner_user_id must be greater than 0",
		},
		{
			name:    "owner id above maximum",
			body:    fmt.Sprintf(`{"name":"Acme","owner_user_id":%d}`, MaxOrganizationReferenceId+1),
			message: "owner_user_id must be at most 1000000000",
		},
		{
			name:    "quota below minimum",
			body:    `{"name":"Acme","owner_user_id":1,"quota":-1}`,
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "quota too large",
			body:    fmt.Sprintf(`{"name":"Acme","owner_user_id":1,"quota":%d}`, MaxOrganizationQuota+1),
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "unsupported field",
			body:    `{"name":"Acme","owner_user_id":1,"external_id":"x"}`,
			message: "unsupported field: external_id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Id: 10, Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-" + tc.name}
			owner := model.User{Id: 1, Username: "owner", Password: "password", Role: common.RoleCommonUser, AffCode: "owner-" + tc.name}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&owner).Error)

			res := performOrganizationRequest(
				CreateOrganization,
				root,
				http.MethodPost,
				"/api/organizations",
				tc.body,
			)

			requireOrganizationApiError(t, res, tc.message)
		})
	}
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
	require.Equal(t, int64(750), reloaded.Quota)
}

func TestOrganizationRootAcceptsUpdateBoundaryInput(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		wantQuota   int64
		wantStatus  int
		wantComment string
	}{
		{
			name:        "minimum quota and enabled status",
			body:        `{"description":"","quota":0,"status":1}`,
			wantQuota:   0,
			wantStatus:  model.OrganizationStatusEnabled,
			wantComment: "",
		},
		{
			name:        "maximum description quota and disabled status",
			body:        fmt.Sprintf(`{"description":%q,"quota":%d,"status":2}`, strings.Repeat("다", MaxOrganizationDescriptionLength), MaxOrganizationQuota),
			wantQuota:   MaxOrganizationQuota,
			wantStatus:  model.OrganizationStatusDisabled,
			wantComment: strings.Repeat("다", MaxOrganizationDescriptionLength),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-update-boundary-" + tc.name}
			org := model.Organization{Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				UpdateOrganization,
				root,
				http.MethodPatch,
				fmt.Sprintf("/api/organizations/%d", org.Id),
				tc.body,
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)

			var reloaded model.Organization
			require.NoError(t, model.DB.First(&reloaded, org.Id).Error)
			require.Equal(t, tc.wantQuota, reloaded.Quota)
			require.Equal(t, tc.wantStatus, reloaded.Status)
			require.Equal(t, tc.wantComment, reloaded.Description)
		})
	}
}

func TestOrganizationRootRejectsInvalidUpdateInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "description too long",
			body:    fmt.Sprintf(`{"description":%q}`, strings.Repeat("a", MaxOrganizationDescriptionLength+1)),
			message: "description must be at most 255 characters",
		},
		{
			name:    "quota below minimum",
			body:    `{"quota":-1}`,
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "quota too large",
			body:    fmt.Sprintf(`{"quota":%d}`, MaxOrganizationQuota+1),
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "status below enum",
			body:    `{"status":0}`,
			message: "status must be one of: 1, 2",
		},
		{
			name:    "invalid status",
			body:    `{"status":99}`,
			message: "status must be one of: 1, 2",
		},
		{
			name:    "unsupported field",
			body:    `{"quota":0,"name":"Acme"}`,
			message: "unsupported field: name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root-update-" + tc.name}
			org := model.Organization{Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				UpdateOrganization,
				root,
				http.MethodPatch,
				fmt.Sprintf("/api/organizations/%d", org.Id),
				tc.body,
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
			)

			requireOrganizationApiError(t, res, tc.message)
		})
	}
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

func TestOrganizationAdminLogsStayWithinOrganizationAndApplyFilters(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-logs"}
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member-logs"}
	otherUser := model.User{Username: "outsider", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, AffCode: "outsider-logs"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	otherOrg := model.Organization{Id: 2, Name: "Other", OwnerUserId: 3, Quota: 500, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&otherUser).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	require.NoError(t, model.DB.Create(&otherOrg).Error)
	now := time.Now().Unix()
	logs := []model.Log{
		{UserId: member.Id, Username: member.Username, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "gpt-4o", Quota: 150, Other: "{}"},
		{UserId: member.Id, Username: member.Username, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "claude-3-5", Quota: 90, Other: "{}"},
		{UserId: otherUser.Id, Username: otherUser.Username, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "gpt-4o", Quota: 999, Other: "{}"},
	}
	require.NoError(t, model.LOG_DB.Create(&logs).Error)

	res := performOrganizationRequest(
		GetOrganizationLogs,
		admin,
		http.MethodGet,
		"/api/organization/logs?model_name=gpt-4o",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, member.Username)
	require.Contains(t, body, `"model_name":"gpt-4o"`)
	require.Contains(t, body, `"total":1`)
	require.NotContains(t, body, "claude-3-5")
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
	require.Equal(t, int64(10), reloaded.Quota)
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
	require.Equal(t, int64(100), reloaded.Quota)
	require.Equal(t, "reviewed", reloaded.Remark)
}

func TestOrganizationAdminAcceptsUserUpdateBoundaryInput(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		wantQuota  int64
		wantStatus int
		wantRemark string
	}{
		{
			name:       "minimum quota and enabled status",
			body:       `{"quota":0,"status":1,"remark":""}`,
			wantQuota:  0,
			wantStatus: common.UserStatusEnabled,
			wantRemark: "",
		},
		{
			name:       "maximum quota remark and disabled status",
			body:       fmt.Sprintf(`{"quota":%d,"status":2,"remark":%q}`, MaxOrganizationQuota, strings.Repeat("라", MaxOrganizationRemarkLength)),
			wantQuota:  MaxOrganizationQuota,
			wantStatus: common.UserStatusDisabled,
			wantRemark: strings.Repeat("라", MaxOrganizationRemarkLength),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-user-boundary-" + tc.name}
			target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 10, Status: common.UserStatusEnabled, AffCode: "target-user-boundary-" + tc.name}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&target).Error)

			res := performOrganizationRequest(
				UpdateOrganizationUser,
				admin,
				http.MethodPatch,
				fmt.Sprintf("/api/organization/users/%d", target.Id),
				tc.body,
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
			)

			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)

			var reloaded model.User
			require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
			require.Equal(t, tc.wantQuota, reloaded.Quota)
			require.Equal(t, tc.wantStatus, reloaded.Status)
			require.Equal(t, tc.wantRemark, reloaded.Remark)
		})
	}
}

func TestOrganizationAdminRejectsInvalidUserUpdateInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "negative quota",
			body:    `{"quota":-1}`,
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "quota too large",
			body:    fmt.Sprintf(`{"quota":%d}`, MaxOrganizationQuota+1),
			message: fmt.Sprintf("quota must be between 0 and %d", MaxOrganizationQuota),
		},
		{
			name:    "invalid status",
			body:    `{"status":99}`,
			message: "status must be one of: 1, 2",
		},
		{
			name:    "status below enum",
			body:    `{"status":0}`,
			message: "status must be one of: 1, 2",
		},
		{
			name:    "remark too long",
			body:    fmt.Sprintf(`{"remark":%q}`, strings.Repeat("a", MaxOrganizationRemarkLength+1)),
			message: "remark must be at most 255 characters",
		},
		{
			name:    "unsupported field",
			body:    `{"quota":0,"role":100}`,
			message: "unsupported field: role",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin-invalid-" + tc.name}
			target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 10, AffCode: "target-invalid-" + tc.name}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&target).Error)

			res := performOrganizationRequest(
				UpdateOrganizationUser,
				admin,
				http.MethodPatch,
				fmt.Sprintf("/api/organization/users/%d", target.Id),
				tc.body,
				gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
			)

			requireOrganizationApiError(t, res, tc.message)
		})
	}
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
	require.Equal(t, int64(10), reloaded.Quota)
}

func TestOrganizationOwnerCanAssignMemberRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "owner-assignment-org", OwnerUserId: owner.Id, Status: model.OrganizationStatusEnabled}).Error)

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

func TestOrganizationAdminCanListAssignableUsers(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	unassigned := model.User{Username: "unassigned", Password: "password", Role: common.RoleCommonUser, AffCode: "unassigned"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&unassigned).Error)

	res := performOrganizationRequest(
		ListAssignableOrganizationUsers,
		admin,
		http.MethodGet,
		"/api/organization/assignable-users",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Contains(t, res.Body.String(), `"username":"unassigned"`)
}

func TestOrganizationMemberCannotListAssignableUsers(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		ListAssignableOrganizationUsers,
		member,
		http.MethodGet,
		"/api/organization/assignable-users",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestOrganizationAdminCanAssignMemberRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "admin-member-assignment-org", OwnerUserId: admin.Id, Status: model.OrganizationStatusEnabled}).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
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

func TestOrganizationAdminCanAssignAdminRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "admin-role-assignment-org", OwnerUserId: admin.Id, Status: model.OrganizationStatusEnabled}).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"admin"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, model.OrganizationRoleAdmin, reloaded.OrganizationRole)
}

func TestOrganizationAdminCannotAssignOwnerRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "admin-role-change-org", OwnerUserId: admin.Id, Status: model.OrganizationStatusEnabled}).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"owner"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Zero(t, reloaded.OrganizationId)
}

func TestOrganizationAdminCanChangeRoleOfMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 1, Name: "admin-member-role-change-org", OwnerUserId: admin.Id, Status: model.OrganizationStatusEnabled}).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"admin"}`,
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, model.OrganizationRoleAdmin, reloaded.OrganizationRole)
}

func TestOrganizationAdminCanRemoveMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "admin"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "target"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		admin,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", target.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Zero(t, reloaded.OrganizationId)
	require.Empty(t, reloaded.OrganizationRole)
}

func TestOrganizationMemberCannotAssignMembership(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, AffCode: "target"}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		member,
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

// ---------------------------------------------------------------------------
// ListOrganizations
// ---------------------------------------------------------------------------

func TestOrganizationRootCanListOrganizations(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	org1 := model.Organization{Name: "Alpha", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
	org2 := model.Organization{Name: "Beta", OwnerUserId: 1, Quota: 200, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org1).Error)
	require.NoError(t, model.DB.Create(&org2).Error)

	res := performOrganizationRequest(
		ListOrganizations,
		root,
		http.MethodGet,
		"/api/organizations",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"Alpha"`)
	require.Contains(t, body, `"Beta"`)
}

func TestOrganizationNonRootCannotListOrganizations(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		ListOrganizations,
		owner,
		http.MethodGet,
		"/api/organizations",
		"",
	)

	requireOrganizationApiError(t, res, "root permission required")
}

// ---------------------------------------------------------------------------
// DeleteOrganization
// ---------------------------------------------------------------------------

func TestOrganizationRootCanDeleteOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	org := model.Organization{Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		DeleteOrganization,
		root,
		http.MethodDelete,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var count int64
	model.DB.Model(&model.Organization{}).Where("id = ?", org.Id).Count(&count)
	require.Equal(t, int64(0), count)
}

func TestOrganizationRootDeleteNotFound(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser, AffCode: "root"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		DeleteOrganization,
		root,
		http.MethodDelete,
		"/api/organizations/99999",
		"",
		gin.Param{Key: "id", Value: "99999"},
	)

	require.Equal(t, http.StatusNotFound, res.Code)
	require.Contains(t, res.Body.String(), "organization not found")
}

func TestOrganizationNonRootCannotDeleteOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 100, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		DeleteOrganization,
		owner,
		http.MethodDelete,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		"",
		gin.Param{Key: "id", Value: fmt.Sprintf("%d", org.Id)},
	)

	requireOrganizationApiError(t, res, "root permission required")
}

func TestOrganizationDisableCancelsWalletBillingAndQueuesKeyRevocation(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Id: 1, Username: "root-billing-lifecycle", Password: "password", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AffCode: "root-billing-lifecycle"}
	owner := model.User{Id: 7, Username: "owner-billing-lifecycle", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, OrganizationId: 3, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner-billing-lifecycle"}
	org := model.Organization{Id: 3, Name: "Billing Lifecycle Org", OwnerUserId: owner.Id, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)
	keyID, err := model.StoreTossBillingKeyWithSecret(owner.Id, "cust_org_disable", "billing_org_disable", "현대", "433012******1234", "sk_org_disable")
	require.NoError(t, err)
	activeKey := "organization:3:scheduled"
	require.NoError(t, model.DB.Create(&model.WalletAutoRecharge{
		Type:         model.WalletAutoRechargeTypeScheduled,
		TargetType:   model.TopUpTargetTypeOrganization,
		TargetId:     org.Id,
		OwnerUserId:  owner.Id,
		BillingKeyId: keyID,
		Status:       model.WalletAutoRechargeStatusActive,
		ActiveKey:    &activeKey,
	}).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPatch,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		`{"status":2}`,
		gin.Param{Key: "id", Value: strconv.Itoa(org.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.Where("billing_key_id = ?", keyID).First(&policy).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
	require.Nil(t, policy.ActiveKey)
}

func TestOrganizationAdminDisablingUserStopsTossRecurringBilling(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Id: 1, Username: "org-admin-billing", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, OrganizationId: 3, OrganizationRole: model.OrganizationRoleAdmin, AffCode: "org-admin-billing"}
	target := model.User{Id: 7, Username: "org-target-billing", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, OrganizationId: 3, OrganizationRole: model.OrganizationRoleMember, AffCode: "org-target-billing"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	keyID, err := model.StoreTossBillingKeyWithSecret(target.Id, "cust_user_disable", "billing_user_disable", "현대", "433012******1234", "sk_user_disable")
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.UserSubscription{
		Id:           11,
		UserId:       target.Id,
		PlanId:       1,
		Status:       "active",
		AutoRenew:    true,
		BillingKeyId: keyID,
	}).Error)
	activeKey := "user:7:threshold"
	require.NoError(t, model.DB.Create(&model.WalletAutoRecharge{
		Type:         model.WalletAutoRechargeTypeThreshold,
		TargetType:   model.TopUpTargetTypeUser,
		TargetId:     target.Id,
		OwnerUserId:  target.Id,
		BillingKeyId: keyID,
		Status:       model.WalletAutoRechargeStatusActive,
		ActiveKey:    &activeKey,
	}).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"status":2}`,
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusPendingRevocation, key.Status)
	var sub model.UserSubscription
	require.NoError(t, model.DB.First(&sub, 11).Error)
	require.False(t, sub.AutoRenew)
	var policy model.WalletAutoRecharge
	require.NoError(t, model.DB.Where("billing_key_id = ?", keyID).First(&policy).Error)
	require.Equal(t, model.WalletAutoRechargeStatusCancelled, policy.Status)
}

// ---------------------------------------------------------------------------
// GetOrganizationProfile — missing branch: member (non-admin) is rejected
// ---------------------------------------------------------------------------

func TestOrganizationMemberCannotReadProfile(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		GetOrganizationProfile,
		member,
		http.MethodGet,
		"/api/organization",
		"",
	)

	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ---------------------------------------------------------------------------
// ListOrganizationUsers — search / sort / pagination
// ---------------------------------------------------------------------------

func TestOrganizationAdminCanListOrganizationUsers(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	member := model.User{Username: "alice", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "alice"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		ListOrganizationUsers,
		owner,
		http.MethodGet,
		"/api/organization/users",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"success":true`)
	require.Contains(t, body, `"alice"`)
}

func TestOrganizationUsersKeywordSearch(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	alice := model.User{Username: "alice", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "alice"}
	bob := model.User{Username: "bob", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "bob"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&alice).Error)
	require.NoError(t, model.DB.Create(&bob).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		ListOrganizationUsers,
		owner,
		http.MethodGet,
		"/api/organization/users?keyword=ali",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"alice"`)
	require.NotContains(t, body, `"bob"`)
}

func TestOrganizationUsersSort(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	alice := model.User{Username: "alice", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 100, AffCode: "alice"}
	bob := model.User{Username: "bob", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 200, AffCode: "bob"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&alice).Error)
	require.NoError(t, model.DB.Create(&bob).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	// sort by quota desc — bob (200) should appear before alice (100)
	res := performOrganizationRequest(
		ListOrganizationUsers,
		owner,
		http.MethodGet,
		"/api/organization/users?order_by=quota&order_dir=desc",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	bobPos := strings.Index(body, `"bob"`)
	alicePos := strings.Index(body, `"alice"`)
	require.True(t, bobPos < alicePos, "bob (higher quota) should appear before alice in desc order")
}

func TestOrganizationUsersInvalidOrderByFallsBackToId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner, AffCode: "owner"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	// unknown order_by must not cause an error
	res := performOrganizationRequest(
		ListOrganizationUsers,
		owner,
		http.MethodGet,
		"/api/organization/users?order_by=injected;DROP+TABLE+users--&order_dir=asc",
		"",
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestOrganizationUsersMemberCannotList(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	member := model.User{Username: "member", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, AffCode: "member"}
	org := model.Organization{Id: 1, Name: "Acme", OwnerUserId: 1, Quota: 1000, Status: model.OrganizationStatusEnabled}
	require.NoError(t, model.DB.Create(&member).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		ListOrganizationUsers,
		member,
		http.MethodGet,
		"/api/organization/users",
		"",
	)

	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestRemoveOrganizationUserMembershipSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(member.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(member.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	var payload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	var updated model.User
	require.NoError(t, model.DB.First(&updated, member.Id).Error)
	require.Equal(t, 0, updated.OrganizationId)
	require.Equal(t, "", updated.OrganizationRole)
}

func TestRemoveOrganizationUserMembershipRejectsOwner(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	owner2 := model.User{Username: "owner2", Password: "x", Role: common.RoleCommonUser, AffCode: "o2", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&owner2).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(owner2.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(owner2.Id)},
	)
	requireOrganizationApiError(t, res, "organization owner cannot be removed")
}

func TestRemoveOrganizationUserMembershipRejectsNonMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	outsider := model.User{Username: "outsider", Password: "x", Role: common.RoleCommonUser, AffCode: "out", OrganizationId: 2, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&outsider).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(outsider.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(outsider.Id)},
	)
	requireOrganizationApiError(t, res, "user does not belong to your organization")
}

func TestRemoveOrganizationUserMembershipRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	actor := model.User{Username: "actor", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "member"}
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&actor).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		actor,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(target.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestRemoveOrganizationUserMembershipRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/abc/membership",
		"",
		gin.Param{Key: "id", Value: "abc"},
	)
	requireOrganizationApiError(t, res, `strconv.Atoi: parsing "abc": invalid syntax`)
}

// ── GetOrganizationUser ──────────────────────────────────────────────────────

func TestGetOrganizationUserAdminCanFetchMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		GetOrganizationUser,
		admin,
		http.MethodGet,
		"/organization/users/"+strconv.Itoa(member.Id),
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(member.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestGetOrganizationUserRejectsDifferentOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	other := model.User{Username: "other", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 2, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&other).Error)

	res := performOrganizationRequest(
		GetOrganizationUser,
		admin,
		http.MethodGet,
		"/organization/users/"+strconv.Itoa(other.Id),
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(other.Id)},
	)
	requireOrganizationApiError(t, res, "organization target permission denied")
}

func TestGetOrganizationUserRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		GetOrganizationUser,
		admin,
		http.MethodGet,
		"/organization/users/bad",
		"",
		gin.Param{Key: "id", Value: "bad"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestGetOrganizationUserMemberCannotFetchOtherMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	actor := model.User{Username: "actor", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "member"}
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&actor).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		GetOrganizationUser,
		actor,
		http.MethodGet,
		"/organization/users/"+strconv.Itoa(target.Id),
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	requireOrganizationApiError(t, res, "organization target permission denied")
}

// ── RemoveOrganizationUserMembership (추가 분기) ─────────────────────────────

func TestRemoveOrganizationUserMembershipRejectsGlobalAdmin(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	globalAdmin := model.User{Username: "gadmin", Password: "x", Role: common.RoleAdminUser, AffCode: "g", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&globalAdmin).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(globalAdmin.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(globalAdmin.Id)},
	)
	requireOrganizationApiError(t, res, "global admin users cannot be managed by organization admins")
}

func TestRemoveOrganizationUserMembershipRejectsWrongOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	other := model.User{Username: "other", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: 2, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&other).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		owner,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(other.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(other.Id)},
	)
	requireOrganizationApiError(t, res, "user does not belong to your organization")
}

func TestRemoveOrganizationUserMembershipRejectsOwnerTarget(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		RemoveOrganizationUserMembership,
		admin,
		http.MethodDelete,
		"/organization/users/"+strconv.Itoa(owner.Id)+"/membership",
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(owner.Id)},
	)
	requireOrganizationApiError(t, res, "organization owner cannot be removed")
}

// ── UpdateOrganizationUser (추가 분기) ────────────────────────────────────────

func TestUpdateOrganizationUserNoFields(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		"/organization/users/"+strconv.Itoa(member.Id),
		`{}`,
		gin.Param{Key: "id", Value: strconv.Itoa(member.Id)},
	)
	requireOrganizationApiError(t, res, "no organization user fields to update")
}

func TestUpdateOrganizationUserRejectsDifferentOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "admin"}
	other := model.User{Username: "other", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 2, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&other).Error)

	status := 1
	body := fmt.Sprintf(`{"status":%d}`, status)
	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		"/organization/users/"+strconv.Itoa(other.Id),
		body,
		gin.Param{Key: "id", Value: strconv.Itoa(other.Id)},
	)
	requireOrganizationApiError(t, res, "organization target permission denied")
}

func TestUpdateOrganizationUserMemberCannotUpdate(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	actor := model.User{Username: "actor", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: 1, OrganizationRole: "member"}
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&actor).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		actor,
		http.MethodPatch,
		"/organization/users/"+strconv.Itoa(target.Id),
		`{"status":1}`,
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── AssignOrganizationUser (추가 분기) ────────────────────────────────────────

func TestAssignOrganizationUserRejectsSelfReassign(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: 1, OrganizationRole: "owner"}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		"/organization/users/"+strconv.Itoa(owner.Id)+"/membership",
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(owner.Id)},
	)
	requireOrganizationApiError(t, res, "organization admins cannot reassign themselves")
}

// ── ExportOrganizationUsers ───────────────────────────────────────────────────

func TestExportOrganizationUsersMemberCannotExport(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		ExportOrganizationUsers,
		member,
		http.MethodGet,
		"/organization/users/export",
		"",
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestExportOrganizationUsersAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	org := model.Organization{Name: "TestOrg", Description: "desc", Quota: 100}
	require.NoError(t, model.DB.Create(&org).Error)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: "admin"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		ExportOrganizationUsers,
		admin,
		http.MethodGet,
		"/organization/users/export",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", res.Header().Get("Content-Type"))
	require.Contains(t, res.Header().Get("Content-Disposition"), "attachment")
}

// ── ImportOrganizationUsers ───────────────────────────────────────────────────

func TestImportOrganizationUsersMemberCannotImport(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: 1, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/organization/users/import", nil)
	ctx.Set("id", member.Id)
	ctx.Set("role", member.Role)
	ImportOrganizationUsers(ctx)

	requireOrganizationApiError(t, recorder, "organization admin permission required")
}

// ── CreateOrganization additional input-validation tests ──────────────────────

func TestCreateOrganizationRejectsOwnerUserIdTooLarge(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		CreateOrganization,
		root,
		http.MethodPost,
		"/api/organizations",
		fmt.Sprintf(`{"name":"Acme","owner_user_id":%d}`, MaxOrganizationReferenceId+1),
	)
	requireOrganizationApiError(t, res, "owner_user_id must be at most 1000000000")
}

func TestCreateOrganizationAcceptsZeroQuota(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "owner"}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		CreateOrganization,
		root,
		http.MethodPost,
		"/api/organizations",
		fmt.Sprintf(`{"name":"Acme","owner_user_id":%d,"quota":0}`, owner.Id),
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestCreateOrganizationNameBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		orgName string
		wantOK  bool
		errMsg  string
	}{
		{"64 runes ok", strings.Repeat("a", MaxOrganizationNameLength), true, ""},
		{"65 runes fail", strings.Repeat("a", MaxOrganizationNameLength+1), false, "name must be at most 64 characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
			owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "owner"}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&owner).Error)

			res := performOrganizationRequest(
				CreateOrganization,
				root,
				http.MethodPost,
				"/api/organizations",
				fmt.Sprintf(`{"name":%q,"owner_user_id":%d}`, tc.orgName, owner.Id),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				requireOrganizationApiError(t, res, tc.errMsg)
			}
		})
	}
}

// ── UpdateOrganization additional tests ───────────────────────────────────────

func TestUpdateOrganizationRejectsNonNumericId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
	require.NoError(t, model.DB.Create(&root).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPut,
		"/api/organizations/abc",
		`{"name":"X"}`,
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	body := res.Body.String()
	require.Contains(t, body, `"success":false`)
}

func TestUpdateOrganizationDescriptionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		desc   string
		wantOK bool
		errMsg string
	}{
		{"empty desc ok", "", true, ""},
		{"255 runes ok", strings.Repeat("a", MaxOrganizationDescriptionLength), true, ""},
		{"256 runes fail", strings.Repeat("a", MaxOrganizationDescriptionLength+1), false, "description must be at most 255 characters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				UpdateOrganization,
				root,
				http.MethodPut,
				fmt.Sprintf("/api/organizations/%d", org.Id),
				fmt.Sprintf(`{"description":%q}`, tc.desc),
				gin.Param{Key: "id", Value: strconv.Itoa(org.Id)},
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				requireOrganizationApiError(t, res, tc.errMsg)
			}
		})
	}
}

func TestUpdateOrganizationStatusBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		wantOK bool
	}{
		{"status 1 ok", model.OrganizationStatusEnabled, true},
		{"status 2 ok", model.OrganizationStatusDisabled, true},
		{"status 0 fail", 0, false},
		{"status 3 fail", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&root).Error)
			require.NoError(t, model.DB.Create(&org).Error)

			res := performOrganizationRequest(
				UpdateOrganization,
				root,
				http.MethodPut,
				fmt.Sprintf("/api/organizations/%d", org.Id),
				fmt.Sprintf(`{"status":%d}`, tc.status),
				gin.Param{Key: "id", Value: strconv.Itoa(org.Id)},
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":false`)
				require.Contains(t, res.Body.String(), OrganizationStatusAllowedError)
			}
		})
	}
}

func TestUpdateOrganizationRejectsUnknownField(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPut,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		`{"unknown_field":"value"}`,
		gin.Param{Key: "id", Value: strconv.Itoa(org.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "unsupported field")
}

func TestUpdateOrganizationRejectsNoFields(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "x", Role: common.RoleRootUser, AffCode: "root"}
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&root).Error)
	require.NoError(t, model.DB.Create(&org).Error)

	res := performOrganizationRequest(
		UpdateOrganization,
		root,
		http.MethodPut,
		fmt.Sprintf("/api/organizations/%d", org.Id),
		`{}`,
		gin.Param{Key: "id", Value: strconv.Itoa(org.Id)},
	)
	requireOrganizationApiError(t, res, "no organization fields to update")
}

// ── GetOrganizationDashboard additional tests ─────────────────────────────────

func TestGetOrganizationDashboardStartAfterEnd(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	now := time.Now().Unix()
	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		fmt.Sprintf("/api/organization/dashboard?start_timestamp=%d&end_timestamp=%d", now, now-3600),
		"",
	)
	requireOrganizationApiError(t, res, "start_timestamp must be before end_timestamp")
}

func TestGetOrganizationDashboardCustomPreset(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		GetOrganizationDashboard,
		admin,
		http.MethodGet,
		"/api/organization/dashboard?preset=custom",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestGetOrganizationDashboardEmptyPreset(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

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

// ── AssignOrganizationUser additional tests ───────────────────────────────────

func TestAssignOrganizationUserRejectsEmptyRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleOwner}
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/organization/users/%d/membership", target.Id),
		`{"organization_role":""}`,
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	requireOrganizationApiError(t, res, "invalid organization role")
}

func TestAssignOrganizationUserRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleOwner}
	require.NoError(t, model.DB.Create(&owner).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		"/organization/users/abc/membership",
		`{"organization_role":"member"}`,
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestAssignOrganizationUserRejectsInvalidJSON(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	owner := model.User{Username: "owner", Password: "x", Role: common.RoleCommonUser, AffCode: "o", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleOwner}
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/organization/users/%d/membership", target.Id),
		`{invalid`,
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

// ── CreateOrganizationSubscriptionPlan tests ──────────────────────────────────

func TestCreateOrganizationSubscriptionPlanAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		admin,
		http.MethodPost,
		"/api/organization/subscription-plans",
		`{"title":"Basic","duration_unit":"month","duration_value":1}`,
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestCreateOrganizationSubscriptionPlanRejectsMissingTitle(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		admin,
		http.MethodPost,
		"/api/organization/subscription-plans",
		`{"title":"  ","duration_unit":"month"}`,
	)
	requireOrganizationApiError(t, res, "title is required")
}

func TestCreateOrganizationSubscriptionPlanTitleBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		title  string
		wantOK bool
	}{
		{"128 runes ok", strings.Repeat("a", MaxOrganizationSubscriptionPlanTitleLength), true},
		{"129 runes fail", strings.Repeat("a", MaxOrganizationSubscriptionPlanTitleLength+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":%q,"duration_unit":"month"}`, tc.title),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				requireOrganizationApiError(t, res, "title must be at most 128 characters")
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanSubtitleBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		subtitle string
		wantOK   bool
	}{
		{"255 runes ok", strings.Repeat("a", MaxOrganizationSubscriptionPlanSubtitleLength), true},
		{"256 runes fail", strings.Repeat("a", MaxOrganizationSubscriptionPlanSubtitleLength+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","subtitle":%q,"duration_unit":"month"}`, tc.subtitle),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				requireOrganizationApiError(t, res, "subtitle must be at most 255 characters")
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanTotalAmountBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		totalAmount int64
		wantOK      bool
	}{
		{"zero ok", 0, true},
		{"max ok", int64(MaxOrganizationQuota), true},
		{"negative fail", -1, false},
		{"max+1 fail", int64(MaxOrganizationQuota) + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","total_amount":%d,"duration_unit":"month"}`, tc.totalAmount),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":false`)
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanDurationUnits(t *testing.T) {
	for _, unit := range []string{
		model.SubscriptionDurationYear,
		model.SubscriptionDurationMonth,
		model.SubscriptionDurationDay,
		model.SubscriptionDurationHour,
	} {
		t.Run(unit, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","duration_unit":%q,"duration_value":1}`, unit),
			)
			require.Equal(t, http.StatusOK, res.Code)
			require.Contains(t, res.Body.String(), `"success":true`)
		})
	}

	t.Run("custom", func(t *testing.T) {
		setupOrganizationControllerTestDB(t)
		org := model.Organization{Name: "Org", Description: "d", Quota: 0}
		require.NoError(t, model.DB.Create(&org).Error)
		admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
		require.NoError(t, model.DB.Create(&admin).Error)

		res := performOrganizationRequest(
			CreateOrganizationSubscriptionPlan,
			admin,
			http.MethodPost,
			"/api/organization/subscription-plans",
			fmt.Sprintf(`{"title":"Plan","duration_unit":"custom","custom_seconds":%d}`, 86400),
		)
		require.Equal(t, http.StatusOK, res.Code)
		require.Contains(t, res.Body.String(), `"success":true`)
	})
}

func TestCreateOrganizationSubscriptionPlanRejectsInvalidDurationUnit(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		admin,
		http.MethodPost,
		"/api/organization/subscription-plans",
		`{"title":"Plan","duration_unit":"week"}`,
	)
	requireOrganizationApiError(t, res, OrganizationDurationUnitAllowedError)
}

func TestCreateOrganizationSubscriptionPlanDurationValueBoundary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		durationValue int
		durationUnit  string
		wantOK        bool
	}{
		{"0 ok (defaults to 1)", 0, "month", true},
		{"1200 ok", MaxOrganizationSubscriptionDurationValue, "month", true},
		{"-1 fail", -1, "month", false},
		{"1201 fail", MaxOrganizationSubscriptionDurationValue + 1, "month", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","duration_unit":%q,"duration_value":%d}`, tc.durationUnit, tc.durationValue),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				requireOrganizationApiError(t, res, OrganizationDurationValueRangeError)
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanCustomSeconds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		customSeconds int64
		wantOK        bool
	}{
		{"1 ok", 1, true},
		{"max ok", MaxOrganizationSubscriptionCustomSeconds, true},
		{"0 fail", 0, false},
		{"max+1 fail", MaxOrganizationSubscriptionCustomSeconds + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","duration_unit":"custom","custom_seconds":%d}`, tc.customSeconds),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":false`)
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanSortOrderBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		sortOrder int
		wantOK    bool
	}{
		{"min ok", -MaxOrganizationSubscriptionSortOrder, true},
		{"max ok", MaxOrganizationSubscriptionSortOrder, true},
		{"min-1 fail", -MaxOrganizationSubscriptionSortOrder - 1, false},
		{"max+1 fail", MaxOrganizationSubscriptionSortOrder + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			require.NoError(t, model.DB.Create(&admin).Error)

			res := performOrganizationRequest(
				CreateOrganizationSubscriptionPlan,
				admin,
				http.MethodPost,
				"/api/organization/subscription-plans",
				fmt.Sprintf(`{"title":"Plan","duration_unit":"month","sort_order":%d}`, tc.sortOrder),
			)
			if tc.wantOK {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":true`)
			} else {
				require.Equal(t, http.StatusOK, res.Code)
				require.Contains(t, res.Body.String(), `"success":false`)
			}
		})
	}
}

func TestCreateOrganizationSubscriptionPlanRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		member,
		http.MethodPost,
		"/api/organization/subscription-plans",
		`{"title":"Plan","duration_unit":"month"}`,
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestCreateOrganizationSubscriptionPlanRejectsUnknownField(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		CreateOrganizationSubscriptionPlan,
		admin,
		http.MethodPost,
		"/api/organization/subscription-plans",
		`{"title":"Plan","unknown_field":"value"}`,
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
	require.Contains(t, res.Body.String(), "unsupported field")
}

// ── UpdateOrganizationSubscriptionPlan tests ─────────────────────────────────

func TestUpdateOrganizationSubscriptionPlanAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: org.Id, Title: "Old", DurationUnit: "month", DurationValue: 1}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/subscription-plans/%d", plan.Id),
		`{"title":"New","duration_unit":"month","duration_value":3}`,
		gin.Param{Key: "id", Value: strconv.Itoa(plan.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestUpdateOrganizationSubscriptionPlanRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		admin,
		http.MethodPut,
		"/api/organization/subscription-plans/abc",
		`{"title":"New","duration_unit":"month"}`,
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestUpdateOrganizationSubscriptionPlanRejectsMissingTitle(t *testing.T) {
	// buildOrganizationSubscriptionPlanUpdates validates title as required,
	// so test that passing an empty-whitespace title correctly errors
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: org.Id, Title: "Plan", DurationUnit: "month", DurationValue: 1}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/subscription-plans/%d", plan.Id),
		`{"title":"  ","duration_unit":"year","duration_value":1}`,
		gin.Param{Key: "id", Value: strconv.Itoa(plan.Id)},
	)
	requireOrganizationApiError(t, res, "title is required")
}

func TestUpdateOrganizationSubscriptionPlanRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		UpdateOrganizationSubscriptionPlan,
		member,
		http.MethodPut,
		"/api/organization/subscription-plans/1",
		`{"title":"New","duration_unit":"month"}`,
		gin.Param{Key: "id", Value: "1"},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── DeleteOrganizationSubscriptionPlan tests ──────────────────────────────────

func TestDeleteOrganizationSubscriptionPlanAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: org.Id, Title: "Plan", DurationUnit: "month", DurationValue: 1, Enabled: true}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		DeleteOrganizationSubscriptionPlan,
		admin,
		http.MethodDelete,
		fmt.Sprintf("/api/organization/subscription-plans/%d", plan.Id),
		"",
		gin.Param{Key: "id", Value: strconv.Itoa(plan.Id)},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestDeleteOrganizationSubscriptionPlanRejectsInvalidId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		DeleteOrganizationSubscriptionPlan,
		admin,
		http.MethodDelete,
		"/api/organization/subscription-plans/abc",
		"",
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestDeleteOrganizationSubscriptionPlanRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		DeleteOrganizationSubscriptionPlan,
		member,
		http.MethodDelete,
		"/api/organization/subscription-plans/1",
		"",
		gin.Param{Key: "id", Value: "1"},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── AssignOrganizationUserSubscription tests ─────────────────────────────────

func TestAssignOrganizationUserSubscriptionPlanIdBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		planId int
		errMsg string
	}{
		{"zero fail", 0, "plan_id must be greater than 0"},
		{"max+1 fail", MaxOrganizationReferenceId + 1, "plan_id must be at most 1000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupOrganizationControllerTestDB(t)
			org := model.Organization{Name: "Org", Description: "d", Quota: 0}
			require.NoError(t, model.DB.Create(&org).Error)
			admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
			target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
			require.NoError(t, model.DB.Create(&admin).Error)
			require.NoError(t, model.DB.Create(&target).Error)

			res := performOrganizationRequest(
				AssignOrganizationUserSubscription,
				admin,
				http.MethodPost,
				fmt.Sprintf("/api/organization/users/%d/subscription", target.Id),
				fmt.Sprintf(`{"plan_id":%d}`, tc.planId),
				gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
			)
			requireOrganizationApiError(t, res, tc.errMsg)
		})
	}
}

func TestAssignOrganizationUserSubscriptionRejectsInvalidUserId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		admin,
		http.MethodPost,
		"/api/organization/users/abc/subscription",
		`{"plan_id":1}`,
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestAssignOrganizationUserSubscriptionRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		member,
		http.MethodPost,
		"/api/organization/users/1/subscription",
		`{"plan_id":1}`,
		gin.Param{Key: "id", Value: "1"},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

func TestAssignOrganizationUserSubscriptionRejectsDifferentOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org1 := model.Organization{Name: "Org1", Description: "d", Quota: 0}
	org2 := model.Organization{Name: "Org2", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org1).Error)
	require.NoError(t, model.DB.Create(&org2).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org1.Id, OrganizationRole: model.OrganizationRoleAdmin}
	// target belongs to a different org
	target := model.User{Username: "target", Password: "x", Role: common.RoleCommonUser, AffCode: "t", OrganizationId: org2.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: org1.Id, Title: "Plan", DurationUnit: "month", DurationValue: 1}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		AssignOrganizationUserSubscription,
		admin,
		http.MethodPost,
		fmt.Sprintf("/api/organization/users/%d/subscription", target.Id),
		fmt.Sprintf(`{"plan_id":%d}`, plan.Id),
		gin.Param{Key: "id", Value: strconv.Itoa(target.Id)},
	)
	requireOrganizationApiError(t, res, "organization target permission denied")
}

// ── CancelOrganizationUserSubscription tests ─────────────────────────────────

func TestCancelOrganizationUserSubscriptionRejectsInvalidUserId(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		admin,
		http.MethodDelete,
		"/api/organization/users/abc/subscription",
		"",
		gin.Param{Key: "id", Value: "abc"},
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestCancelOrganizationUserSubscriptionRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		CancelOrganizationUserSubscription,
		member,
		http.MethodDelete,
		"/api/organization/users/1/subscription",
		"",
		gin.Param{Key: "id", Value: "1"},
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── ListOrganizationSubscriptionPlans tests ───────────────────────────────────

func TestListOrganizationSubscriptionPlansAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)
	plan := model.OrganizationSubscriptionPlan{OrganizationId: org.Id, Title: "Basic", DurationUnit: "month", DurationValue: 1}
	require.NoError(t, model.DB.Create(&plan).Error)

	res := performOrganizationRequest(
		ListOrganizationSubscriptionPlans,
		admin,
		http.MethodGet,
		"/api/organization/subscription-plans",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Contains(t, res.Body.String(), "Basic")
}

func TestListOrganizationSubscriptionPlansRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		ListOrganizationSubscriptionPlans,
		member,
		http.MethodGet,
		"/api/organization/subscription-plans",
		"",
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── ListOrganizationUserSubscriptions tests ───────────────────────────────────

func TestListOrganizationUserSubscriptionsAdminSuccess(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleAdmin}
	require.NoError(t, model.DB.Create(&admin).Error)

	res := performOrganizationRequest(
		ListOrganizationUserSubscriptions,
		admin,
		http.MethodGet,
		"/api/organization/user-subscriptions",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
}

func TestListOrganizationUserSubscriptionsRejectsMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	org := model.Organization{Name: "Org", Description: "d", Quota: 0}
	require.NoError(t, model.DB.Create(&org).Error)
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: model.OrganizationRoleMember}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		ListOrganizationUserSubscriptions,
		member,
		http.MethodGet,
		"/api/organization/user-subscriptions",
		"",
	)
	requireOrganizationApiError(t, res, "organization admin permission required")
}

// ── GetMyOrganizationSubscription ────────────────────────────────────────────

func TestGetMyOrganizationSubscriptionNoOrg(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	user := model.User{Username: "user", Password: "x", Role: common.RoleCommonUser, AffCode: "u"}
	require.NoError(t, model.DB.Create(&user).Error)

	res := performOrganizationRequest(
		GetMyOrganizationSubscription,
		user,
		http.MethodGet,
		"/organization/subscription/me",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Contains(t, res.Body.String(), `"data":null`)
}

func TestGetMyOrganizationSubscriptionNoActiveSub(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	org := model.Organization{Name: "TestOrg", Description: "d", Quota: 100}
	require.NoError(t, model.DB.Create(&org).Error)

	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&member).Error)

	res := performOrganizationRequest(
		GetMyOrganizationSubscription,
		member,
		http.MethodGet,
		"/organization/subscription/me",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Contains(t, res.Body.String(), `"data":null`)
}

func TestGetMyOrganizationSubscriptionWithActiveSub(t *testing.T) {
	setupOrganizationControllerTestDB(t)

	org := model.Organization{Name: "TestOrg", Description: "d", Quota: 100}
	require.NoError(t, model.DB.Create(&org).Error)

	admin := model.User{Username: "admin", Password: "x", Role: common.RoleCommonUser, AffCode: "a", OrganizationId: org.Id, OrganizationRole: "admin"}
	member := model.User{Username: "member", Password: "x", Role: common.RoleCommonUser, AffCode: "m", OrganizationId: org.Id, OrganizationRole: "member"}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&member).Error)

	plan := model.OrganizationSubscriptionPlan{
		OrganizationId: org.Id,
		Title:          "Basic",
		DurationUnit:   "month",
		DurationValue:  1,
		Enabled:        true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)

	sub := model.OrganizationUserSubscription{
		OrganizationId: org.Id,
		UserId:         member.Id,
		PlanId:         plan.Id,
		Status:         "active",
		StartTime:      1,
		EndTime:        9999999999,
	}
	require.NoError(t, model.DB.Create(&sub).Error)

	res := performOrganizationRequest(
		GetMyOrganizationSubscription,
		member,
		http.MethodGet,
		"/organization/subscription/me",
		"",
	)
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)
	require.Contains(t, res.Body.String(), `"subscription"`)
	require.Contains(t, res.Body.String(), `"plan"`)
}
