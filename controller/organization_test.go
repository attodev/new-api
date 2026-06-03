package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOrganizationControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

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
	require.NoError(t, db.AutoMigrate(&model.Organization{}, &model.User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
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
		fmt.Sprintf(`{"name":"Acme","description":"Customer","owner_user_id":%d}`, owner.Id),
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, owner.Id).Error)
	require.NotZero(t, reloaded.OrganizationId)
	require.Equal(t, model.OrganizationRoleOwner, reloaded.OrganizationRole)
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
