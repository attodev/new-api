# Organization Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add organization-based user management that is separate from existing `User.Group` and global admin roles.

**Architecture:** Add a focused organization domain in the backend with `Organization`, `User.OrganizationId`, and `User.OrganizationRole`. Keep organization authorization behind dedicated helpers and `/api/organization*` routes so organization admins never pass existing global `AdminAuth()` by accident. Add frontend types and views only after backend contracts are tested.

**Tech Stack:** Go 1.22+, Gin, GORM v2, SQLite/MySQL/PostgreSQL-compatible migrations, React 19, TypeScript, Rsbuild, Base UI, Tailwind CSS, Bun.

---

## File Structure

- Create `model/organization.go`: organization model, role/status constants, validation helpers, and DB helpers for organization membership.
- Modify `model/user.go`: add `OrganizationId` and `OrganizationRole` fields, include them in safe user/profile payloads.
- Modify `model/user_cache.go`: include organization fields in `UserBase` so auth and relay contexts can inspect them without extra DB reads.
- Modify `model/main.go`: include `Organization` in both normal and fast migrations.
- Create `controller/organization.go`: root organization CRUD, membership assignment, current organization user listing/detail, and limited user updates.
- Create `controller/organization_test.go`: backend authorization and behavior tests using in-memory SQLite.
- Modify `router/api-router.go`: add `/api/organizations` root routes and `/api/organization` current-organization routes.
- Modify `controller/user.go`: include organization fields in login and self responses so the frontend can read organization permissions from the authenticated user profile.
- Modify `web/default/src/features/profile/types.ts`: add organization fields to the authenticated profile.
- Create `web/default/src/lib/organization-roles.ts`: typed frontend organization role constants and helpers.
- Modify `web/default/src/hooks/use-sidebar-data.ts`: add an organization navigation item outside the global admin group.
- Modify `web/default/src/hooks/use-sidebar-view.ts`: keep the existing global admin group guarded by global admin role and guard the organization group by organization role.
- Modify frontend locale files only for visible strings introduced by the organization UI, following the flat JSON i18n convention.

## Task 1: Add Organization Model And User Fields

**Files:**
- Create: `model/organization.go`
- Modify: `model/user.go`
- Modify: `model/user_cache.go`
- Modify: `model/main.go`
- Test: `model/organization_test.go`

- [ ] **Step 1: Write failing model tests**

Create `model/organization_test.go`:

```go
package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOrganizationModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	LOG_DB = db
	require.NoError(t, db.AutoMigrate(&Organization{}, &User{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestOrganizationRoleValidation(t *testing.T) {
	require.True(t, IsValidOrganizationRole(OrganizationRoleMember))
	require.True(t, IsValidOrganizationRole(OrganizationRoleAdmin))
	require.True(t, IsValidOrganizationRole(OrganizationRoleOwner))
	require.False(t, IsValidOrganizationRole(""))
	require.False(t, IsValidOrganizationRole("root"))
}

func TestCreateOrganizationAssignsOwner(t *testing.T) {
	setupOrganizationModelTestDB(t)

	owner := User{Username: "owner", Password: "password", DisplayName: "Owner", Role: common.RoleCommonUser}
	require.NoError(t, DB.Create(&owner).Error)

	org, err := CreateOrganization("Acme", "Main customer", owner.Id)
	require.NoError(t, err)
	require.Equal(t, "Acme", org.Name)
	require.Equal(t, OrganizationStatusEnabled, org.Status)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, owner.Id).Error)
	require.Equal(t, org.Id, reloaded.OrganizationId)
	require.Equal(t, OrganizationRoleOwner, reloaded.OrganizationRole)
}

func TestCanManageOrganizationTargetRejectsGlobalAdmins(t *testing.T) {
	require.False(t, CanManageOrganizationTarget(
		User{Id: 1, OrganizationId: 7, OrganizationRole: OrganizationRoleOwner, Role: common.RoleCommonUser},
		User{Id: 2, OrganizationId: 7, OrganizationRole: OrganizationRoleMember, Role: common.RoleAdminUser},
	))
	require.False(t, CanManageOrganizationTarget(
		User{Id: 1, OrganizationId: 7, OrganizationRole: OrganizationRoleOwner, Role: common.RoleCommonUser},
		User{Id: 3, OrganizationId: 8, OrganizationRole: OrganizationRoleMember, Role: common.RoleCommonUser},
	))
	require.True(t, CanManageOrganizationTarget(
		User{Id: 1, OrganizationId: 7, OrganizationRole: OrganizationRoleOwner, Role: common.RoleCommonUser},
		User{Id: 4, OrganizationId: 7, OrganizationRole: OrganizationRoleMember, Role: common.RoleCommonUser},
	))
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./model -run 'TestOrganization' -count=1
```

Expected: FAIL because `Organization`, organization role constants, `CreateOrganization`, and `CanManageOrganizationTarget` are undefined.

- [ ] **Step 3: Add organization model and helpers**

Create `model/organization.go`:

```go
package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	OrganizationStatusEnabled  = 1
	OrganizationStatusDisabled = 2
)

const (
	OrganizationRoleMember = "member"
	OrganizationRoleAdmin  = "admin"
	OrganizationRoleOwner  = "owner"
)

type Organization struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"type:varchar(64);not null;uniqueIndex" validate:"max=64"`
	Description string `json:"description,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	OwnerUserId int    `json:"owner_user_id" gorm:"column:owner_user_id;index"`
	Status      int    `json:"status" gorm:"type:int;default:1"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

func IsValidOrganizationRole(role string) bool {
	return role == OrganizationRoleMember || role == OrganizationRoleAdmin || role == OrganizationRoleOwner
}

func HasOrganizationAdminRole(role string) bool {
	return role == OrganizationRoleAdmin || role == OrganizationRoleOwner
}

func HasOrganizationOwnerRole(role string) bool {
	return role == OrganizationRoleOwner
}

func CreateOrganization(name string, description string, ownerUserId int) (*Organization, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || ownerUserId <= 0 {
		return nil, errors.New("invalid organization parameters")
	}

	org := &Organization{
		Name:        name,
		Description: description,
		OwnerUserId: ownerUserId,
		Status:      OrganizationStatusEnabled,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(org).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).
			Where("id = ?", ownerUserId).
			Updates(map[string]interface{}{
				"organization_id":   org.Id,
				"organization_role": OrganizationRoleOwner,
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return org, nil
}

func CanManageOrganizationTarget(actor User, target User) bool {
	if actor.OrganizationId == 0 || target.OrganizationId == 0 {
		return false
	}
	if actor.OrganizationId != target.OrganizationId {
		return false
	}
	if !HasOrganizationAdminRole(actor.OrganizationRole) {
		return false
	}
	if target.Role >= common.RoleAdminUser {
		return false
	}
	return true
}
```

Then add the missing GORM import to `model/organization.go`:

```go
import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)
```

- [ ] **Step 4: Add organization fields to User and cache**

Modify `model/user.go` `User` struct:

```go
	OrganizationId   int    `json:"organization_id" gorm:"type:int;default:0;column:organization_id;index"`
	OrganizationRole string `json:"organization_role" gorm:"type:varchar(16);default:'';column:organization_role"`
```

Place the fields near `Group` so account segmentation fields stay together:

```go
	Group            string         `json:"group" gorm:"type:varchar(64);default:'default'"`
	OrganizationId   int            `json:"organization_id" gorm:"type:int;default:0;column:organization_id;index"`
	OrganizationRole string         `json:"organization_role" gorm:"type:varchar(16);default:'';column:organization_role"`
```

Modify `User.ToBaseUser()` in `model/user.go`:

```go
	cache := &UserBase{
		Id:               user.Id,
		Group:            user.Group,
		OrganizationId:   user.OrganizationId,
		OrganizationRole: user.OrganizationRole,
		Quota:            user.Quota,
		Status:           user.Status,
		Username:         user.Username,
		Setting:          user.Setting,
		Email:            user.Email,
	}
```

Modify `model/user_cache.go` `UserBase`:

```go
type UserBase struct {
	Id               int    `json:"id"`
	Group            string `json:"group"`
	OrganizationId   int    `json:"organization_id"`
	OrganizationRole string `json:"organization_role"`
	Email            string `json:"email"`
	Quota            int    `json:"quota"`
	Status           int    `json:"status"`
	Username         string `json:"username"`
	Setting          string `json:"setting"`
}
```

- [ ] **Step 5: Add Organization to migrations**

Modify `model/main.go` normal `AutoMigrate` list:

```go
		&User{},
		&Organization{},
		&PasskeyCredential{},
```

Modify `model/main.go` fast migration list:

```go
		{&User{}, "User"},
		{&Organization{}, "Organization"},
		{&PasskeyCredential{}, "PasskeyCredential"},
```

- [ ] **Step 6: Run model tests**

Run:

```bash
go test ./model -run 'TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add model/organization.go model/organization_test.go model/user.go model/user_cache.go model/main.go
git commit -m "feat: add organization model"
```

## Task 2: Add Organization Controller And Authorization Tests

**Files:**
- Create: `controller/organization.go`
- Create: `controller/organization_test.go`
- Modify: `router/api-router.go`

- [ ] **Step 1: Write failing controller tests**

Create `controller/organization_test.go`:

```go
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

func performOrganizationRequest(handler gin.HandlerFunc, actor model.User, method string, path string, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("id", actor.Id)
	ctx.Set("role", actor.Role)
	handler(ctx)
	return recorder
}

func TestRootCanCreateOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	root := model.User{Username: "root", Password: "password", Role: common.RoleRootUser}
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser}
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

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, owner.Id).Error)
	require.NotZero(t, reloaded.OrganizationId)
	require.Equal(t, model.OrganizationRoleOwner, reloaded.OrganizationRole)
}

func TestOrganizationAdminCannotUpdateOutsideOrganization(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: model.OrganizationRoleMember, Quota: 10}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"quota":100}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}

func TestOrganizationAdminCanUpdateQuotaForMember(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleMember, Quota: 10}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		UpdateOrganizationUser,
		admin,
		http.MethodPatch,
		fmt.Sprintf("/api/organization/users/%d", target.Id),
		`{"quota":100,"status":1,"remark":"reviewed"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 100, reloaded.Quota)
	require.Equal(t, "reviewed", reloaded.Remark)
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./controller -run 'TestOrganization' -count=1
```

Expected: FAIL because `CreateOrganization` and `UpdateOrganizationUser` are undefined.

- [ ] **Step 3: Implement controller request DTOs and root create endpoint**

Create `controller/organization.go`:

```go
package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type createOrganizationRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerUserId int    `json:"owner_user_id"`
}

type updateOrganizationUserRequest struct {
	Status *int    `json:"status,omitempty"`
	Quota  *int    `json:"quota,omitempty"`
	Remark *string `json:"remark,omitempty"`
}

func CreateOrganization(c *gin.Context) {
	if c.GetInt("role") != common.RoleRootUser {
		common.ApiError(c, "root permission required")
		return
	}

	var req createOrganizationRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	org, err := model.CreateOrganization(req.Name, req.Description, req.OwnerUserId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, org)
}
```

- [ ] **Step 4: Implement current actor lookup and organization user update**

Append to `controller/organization.go`:

```go
func getOrganizationActor(c *gin.Context) (*model.User, error) {
	actorId := c.GetInt("id")
	return model.GetUserById(actorId, false)
}

func UpdateOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.HasOrganizationAdminRole(actor.OrganizationRole) {
		common.ApiError(c, "organization admin permission required")
		return
	}

	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, err := model.GetUserById(targetId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.CanManageOrganizationTarget(*actor, *target) {
		common.ApiError(c, "organization target permission denied")
		return
	}

	var req updateOrganizationUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	updates := map[string]interface{}{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Quota != nil {
		updates["quota"] = *req.Quota
	}
	if req.Remark != nil {
		updates["remark"] = strings.TrimSpace(*req.Remark)
	}
	if len(updates) == 0 {
		common.ApiError(c, "no organization user fields to update")
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(updates).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization user cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
```

- [ ] **Step 5: Implement organization user list/detail endpoints**

Append to `controller/organization.go`:

```go
func ListOrganizationUsers(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.HasOrganizationAdminRole(actor.OrganizationRole) || actor.OrganizationId == 0 {
		common.ApiError(c, "organization admin permission required")
		return
	}

	pageInfo := common.GetPageQuery(c)
	var users []model.User
	query := model.DB.
		Where("organization_id = ?", actor.OrganizationId).
		Where("role < ?", common.RoleAdminUser)

	var total int64
	if err := query.Model(&model.User{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Offset(pageInfo.GetStartIdx()).Limit(pageInfo.GetPageSize()).Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, pageInfo)
}

func GetOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, err := model.GetUserById(targetId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.CanManageOrganizationTarget(*actor, *target) {
		common.ApiError(c, "organization target permission denied")
		return
	}
	common.ApiSuccess(c, target)
}
```

- [ ] **Step 6: Add routes**

Modify `router/api-router.go` near the user/admin route sections:

```go
		organizationsRoute := apiRouter.Group("/organizations")
		organizationsRoute.Use(middleware.RootAuth())
		{
			organizationsRoute.POST("/", controller.CreateOrganization)
		}

		organizationRoute := apiRouter.Group("/organization")
		organizationRoute.Use(middleware.UserAuth())
		{
			organizationRoute.GET("/users", controller.ListOrganizationUsers)
			organizationRoute.GET("/users/:id", controller.GetOrganizationUser)
			organizationRoute.PATCH("/users/:id", controller.UpdateOrganizationUser)
		}
```

- [ ] **Step 7: Run controller tests**

Run:

```bash
go test ./controller -run 'TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add controller/organization.go controller/organization_test.go router/api-router.go
git commit -m "feat: add organization user APIs"
```

## Task 3: Add Organization Owner Membership Management

**Files:**
- Modify: `controller/organization.go`
- Modify: `controller/organization_test.go`

- [ ] **Step 1: Write failing owner membership tests**

Append to `controller/organization_test.go`:

```go
func TestOrganizationOwnerCanAssignMemberRole(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	owner := model.User{Username: "owner", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleOwner}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser}
	require.NoError(t, model.DB.Create(&owner).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		owner,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":true`)

	var reloaded model.User
	require.NoError(t, model.DB.First(&reloaded, target.Id).Error)
	require.Equal(t, 1, reloaded.OrganizationId)
	require.Equal(t, model.OrganizationRoleMember, reloaded.OrganizationRole)
}

func TestOrganizationAdminCannotAssignMembership(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	admin := model.User{Username: "admin", Password: "password", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: model.OrganizationRoleAdmin}
	target := model.User{Username: "target", Password: "password", Role: common.RoleCommonUser}
	require.NoError(t, model.DB.Create(&admin).Error)
	require.NoError(t, model.DB.Create(&target).Error)

	res := performOrganizationRequest(
		AssignOrganizationUser,
		admin,
		http.MethodPut,
		fmt.Sprintf("/api/organization/users/%d/membership", target.Id),
		`{"organization_role":"member"}`,
	)

	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"success":false`)
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
go test ./controller -run 'TestOrganizationOwner|TestOrganizationAdminCannotAssign' -count=1
```

Expected: FAIL because `AssignOrganizationUser` is undefined.

- [ ] **Step 3: Implement owner membership endpoint**

Append to `controller/organization.go`:

```go
type assignOrganizationUserRequest struct {
	OrganizationRole string `json:"organization_role"`
}

func AssignOrganizationUser(c *gin.Context) {
	actor, err := getOrganizationActor(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if actor.OrganizationId == 0 || !model.HasOrganizationOwnerRole(actor.OrganizationRole) {
		common.ApiError(c, "organization owner permission required")
		return
	}

	targetId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, err := model.GetUserById(targetId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if target.Role >= common.RoleAdminUser {
		common.ApiError(c, "global admin users cannot be managed by organization owners")
		return
	}

	var req assignOrganizationUserRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if !model.IsValidOrganizationRole(req.OrganizationRole) {
		common.ApiError(c, "invalid organization role")
		return
	}

	if err := model.DB.Model(&model.User{}).Where("id = ?", target.Id).Updates(map[string]interface{}{
		"organization_id":   actor.OrganizationId,
		"organization_role": req.OrganizationRole,
	}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(target.Id); err != nil {
		common.SysLog("failed to invalidate organization membership cache: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
```

- [ ] **Step 4: Add membership route**

Modify `router/api-router.go` organization route:

```go
			organizationRoute.PUT("/users/:id/membership", controller.AssignOrganizationUser)
```

- [ ] **Step 5: Run controller organization tests**

Run:

```bash
go test ./controller -run 'TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add controller/organization.go controller/organization_test.go router/api-router.go
git commit -m "feat: add organization membership management"
```

## Task 4: Expose Organization Fields In Auth And Profile Responses

**Files:**
- Modify: `controller/user.go`
- Modify: `web/default/src/features/profile/types.ts`
- Modify: `web/default/src/lib/organization-roles.ts`

- [ ] **Step 1: Add profile type constants**

Create `web/default/src/lib/organization-roles.ts`:

```ts
export const ORGANIZATION_ROLE = {
  MEMBER: 'member',
  ADMIN: 'admin',
  OWNER: 'owner',
} as const

export type OrganizationRole =
  (typeof ORGANIZATION_ROLE)[keyof typeof ORGANIZATION_ROLE]

export function hasOrganizationAdminRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.ADMIN || role === ORGANIZATION_ROLE.OWNER
}

export function hasOrganizationOwnerRole(role?: string): boolean {
  return role === ORGANIZATION_ROLE.OWNER
}
```

- [ ] **Step 2: Update frontend profile type**

Modify `web/default/src/features/profile/types.ts` `UserProfile`:

```ts
  /** Organization ID, 0 means no organization */
  organization_id?: number
  /** Organization role: member, admin, owner */
  organization_role?: string
```

Place the fields after `group`.

- [ ] **Step 3: Expose organization fields in login response**

Modify `setupLogin` in `controller/user.go`:

```go
			"group":             user.Group,
			"organization_id":   user.OrganizationId,
			"organization_role": user.OrganizationRole,
```

- [ ] **Step 4: Expose organization fields in self/profile response**

Find the `GetSelf` response map in `controller/user.go` and include:

```go
		"organization_id":   user.OrganizationId,
		"organization_role": user.OrganizationRole,
```

- [ ] **Step 5: Run focused checks**

Run:

```bash
go test ./controller -run 'TestOrganization' -count=1
```

Expected: PASS.

Run:

```bash
cd web/default && bun run build
```

Expected: build completes without TypeScript errors.

- [ ] **Step 6: Commit**

```bash
git add controller/user.go web/default/src/features/profile/types.ts web/default/src/lib/organization-roles.ts
git commit -m "feat: expose organization profile fields"
```

## Task 5: Add Organization Admin Frontend Surface

**Files:**
- Create: `web/default/src/features/organizations/api.ts`
- Create: `web/default/src/features/organizations/types.ts`
- Create: `web/default/src/features/organizations/components/organization-users-table.tsx`
- Create: `web/default/src/routes/_authenticated/organization/index.tsx`
- Modify: `web/default/src/hooks/use-sidebar-data.ts`
- Modify: `web/default/src/hooks/use-sidebar-view.ts`
- Modify: `web/default/src/i18n/static-keys.ts`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/vi.json`

- [ ] **Step 1: Add organization API client**

Create `web/default/src/features/organizations/api.ts`:

```ts
import { API } from '@/utils'
import type {
  OrganizationUserUpdatePayload,
  OrganizationUsersResponse,
} from './types'

export async function getOrganizationUsers(params: {
  page?: number
  size?: number
}) {
  const search = new URLSearchParams()
  if (params.page) search.set('p', String(params.page))
  if (params.size) search.set('page_size', String(params.size))
  const suffix = search.toString() ? `?${search.toString()}` : ''
  return API.get<OrganizationUsersResponse>(`/api/organization/users${suffix}`)
}

export async function updateOrganizationUser(
  userId: number,
  payload: OrganizationUserUpdatePayload
) {
  return API.patch(`/api/organization/users/${userId}`, payload)
}
```

- [ ] **Step 2: Add organization frontend types**

Create `web/default/src/features/organizations/types.ts`:

```ts
export type OrganizationRole = 'member' | 'admin' | 'owner'

export interface OrganizationUser {
  id: number
  username: string
  display_name: string
  role: number
  status: number
  quota: number
  used_quota: number
  group: string
  organization_id: number
  organization_role: OrganizationRole
  remark?: string
}

export interface OrganizationUsersResponse {
  success: boolean
  message?: string
  data?: {
    items: OrganizationUser[]
    total: number
  }
}

export interface OrganizationUserUpdatePayload {
  status?: number
  quota?: number
  remark?: string
}
```

- [ ] **Step 3: Add organization users table**

Create `web/default/src/features/organizations/components/organization-users-table.tsx`:

```tsx
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getOrganizationUsers, updateOrganizationUser } from '../api'
import type { OrganizationUser } from '../types'

export function OrganizationUsersTable() {
  const { t } = useTranslation()
  const [users, setUsers] = useState<OrganizationUser[]>([])
  const [loading, setLoading] = useState(false)

  async function loadUsers() {
    setLoading(true)
    try {
      const res = await getOrganizationUsers({ page: 1, size: 20 })
      if (res.data?.success && res.data.data?.items) {
        setUsers(res.data.data.items)
      } else {
        toast.error(res.data?.message || t('Failed to load organization users'))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadUsers()
  }, [])

  async function saveQuota(user: OrganizationUser, quota: number) {
    const res = await updateOrganizationUser(user.id, { quota })
    if (res.data?.success) {
      toast.success(t('Organization user updated'))
      await loadUsers()
    } else {
      toast.error(res.data?.message || t('Failed to update organization user'))
    }
  }

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between'>
        <h1 className='text-xl font-semibold'>{t('Organization Users')}</h1>
        <Button variant='outline' onClick={() => void loadUsers()} disabled={loading}>
          {t('Refresh')}
        </Button>
      </div>
      <div className='overflow-x-auto rounded-md border'>
        <table className='w-full text-sm'>
          <thead className='bg-muted/50'>
            <tr>
              <th className='px-3 py-2 text-left'>{t('Username')}</th>
              <th className='px-3 py-2 text-left'>{t('Role')}</th>
              <th className='px-3 py-2 text-left'>{t('Status')}</th>
              <th className='px-3 py-2 text-left'>{t('Quota')}</th>
              <th className='px-3 py-2 text-right'>{t('Actions')}</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id} className='border-t'>
                <td className='px-3 py-2'>{user.username}</td>
                <td className='px-3 py-2'>{user.organization_role}</td>
                <td className='px-3 py-2'>{user.status}</td>
                <td className='px-3 py-2'>
                  <Input
                    type='number'
                    defaultValue={user.quota}
                    onBlur={(event) => {
                      const value = Number(event.currentTarget.value)
                      if (Number.isFinite(value) && value !== user.quota) {
                        void saveQuota(user, value)
                      }
                    }}
                  />
                </td>
                <td className='px-3 py-2 text-right'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      void updateOrganizationUser(user.id, {
                        status: user.status === 1 ? 2 : 1,
                      }).then(loadUsers)
                    }
                  >
                    {user.status === 1 ? t('Disable') : t('Enable')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Add organization route**

Create `web/default/src/routes/_authenticated/organization/index.tsx`:

```tsx
import { createFileRoute, redirect } from '@tanstack/react-router'
import { OrganizationUsersTable } from '@/features/organizations/components/organization-users-table'
import { hasOrganizationAdminRole } from '@/lib/organization-roles'

export const Route = createFileRoute('/_authenticated/organization/')({
  beforeLoad: ({ context }) => {
    const user = context.auth.user
    if (!user || !hasOrganizationAdminRole(user.organization_role)) {
      throw redirect({ to: '/' })
    }
  },
  component: OrganizationPage,
})

function OrganizationPage() {
  return <OrganizationUsersTable />
}
```

- [ ] **Step 5: Register navigation**

Modify `web/default/src/hooks/use-sidebar-data.ts` imports to include `Building2`:

```ts
  Building2,
```

Add a new root nav group before the existing `admin` group:

```tsx
      {
        id: 'organization',
        title: t('Organization'),
        items: [
          {
            title: t('Organization Users'),
            url: '/organization',
            icon: Building2,
          },
        ],
      },
```

Modify `web/default/src/hooks/use-sidebar-view.ts` to import organization helper:

```ts
import { hasOrganizationAdminRole } from '@/lib/organization-roles'
```

Change the root group filter to keep global admin filtering separate from organization filtering:

```ts
  const rootNavGroups = useMemo<NavGroup[]>(() => {
    const userRole = auth.user?.role
    const isAdmin = userRole !== undefined && userRole >= ROLE.ADMIN
    const isOrganizationAdmin = hasOrganizationAdminRole(
      auth.user?.organization_role
    )

    return configFilteredRoot.filter((group) => {
      if (group.id === 'admin') return isAdmin
      if (group.id === 'organization') return isOrganizationAdmin
      return true
    })
  }, [auth.user?.organization_role, auth.user?.role, configFilteredRoot])
```

- [ ] **Step 6: Add i18n keys**

Run from `web/default`:

```bash
bun run i18n:sync
```

Then add translations for the new visible keys in all supported locale JSON files:

```json
{
  "Organization": "Organization",
  "Organization Users": "Organization Users",
  "Failed to load organization users": "Failed to load organization users",
  "Organization user updated": "Organization user updated",
  "Failed to update organization user": "Failed to update organization user"
}
```

Use these translations:

```json
{
  "en": {
    "Organization": "Organization",
    "Organization Users": "Organization Users",
    "Failed to load organization users": "Failed to load organization users",
    "Organization user updated": "Organization user updated",
    "Failed to update organization user": "Failed to update organization user"
  },
  "zh": {
    "Organization": "组织",
    "Organization Users": "组织用户",
    "Failed to load organization users": "加载组织用户失败",
    "Organization user updated": "组织用户已更新",
    "Failed to update organization user": "更新组织用户失败"
  },
  "fr": {
    "Organization": "Organisation",
    "Organization Users": "Utilisateurs de l'organisation",
    "Failed to load organization users": "Impossible de charger les utilisateurs de l'organisation",
    "Organization user updated": "Utilisateur de l'organisation mis à jour",
    "Failed to update organization user": "Impossible de mettre à jour l'utilisateur de l'organisation"
  },
  "ru": {
    "Organization": "Организация",
    "Organization Users": "Пользователи организации",
    "Failed to load organization users": "Не удалось загрузить пользователей организации",
    "Organization user updated": "Пользователь организации обновлен",
    "Failed to update organization user": "Не удалось обновить пользователя организации"
  },
  "ja": {
    "Organization": "組織",
    "Organization Users": "組織ユーザー",
    "Failed to load organization users": "組織ユーザーを読み込めませんでした",
    "Organization user updated": "組織ユーザーを更新しました",
    "Failed to update organization user": "組織ユーザーを更新できませんでした"
  },
  "vi": {
    "Organization": "Tổ chức",
    "Organization Users": "Người dùng tổ chức",
    "Failed to load organization users": "Không thể tải người dùng tổ chức",
    "Organization user updated": "Đã cập nhật người dùng tổ chức",
    "Failed to update organization user": "Không thể cập nhật người dùng tổ chức"
  }
}
```

- [ ] **Step 7: Build frontend**

Run:

```bash
cd web/default && bun run build
```

Expected: build succeeds.

- [ ] **Step 8: Commit**

```bash
git add web/default/src/features/organizations web/default/src/routes/_authenticated/organization web/default/src/lib/organization-roles.ts web/default/src/i18n
git commit -m "feat: add organization admin UI"
```

## Task 6: Full Verification

**Files:**
- Review all files changed by Tasks 1-5.

- [ ] **Step 1: Run backend focused tests**

Run:

```bash
go test ./model ./controller -run 'TestOrganization' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests for touched packages**

Run:

```bash
go test ./model ./controller ./middleware -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
cd web/default && bun run build
```

Expected: PASS.

- [ ] **Step 4: Manual API smoke test**

Start the server using the repo's normal local dev command. Log in as root, create an organization, assign an owner, log in as the owner, and confirm:

- owner can list `/api/organization/users`
- owner can assign a common user into the organization
- admin can update quota/status for a common organization member
- admin cannot update a global admin user
- admin cannot see global admin UI in the frontend

- [ ] **Step 5: Final status check**

Run:

```bash
git status --short
```

Expected: no uncommitted files except intentional local runtime artifacts.

## Self-Review

Spec coverage:

- Separate organization domain: Task 1.
- Organization roles `member`, `admin`, `owner`: Tasks 1 and 3.
- Root-only organization creation: Task 2.
- Organization-specific APIs: Tasks 2 and 3.
- Limited organization admin user operations: Task 2.
- Owner membership management: Task 3.
- Frontend explicit organization permission checks: Tasks 4 and 5.
- Existing `Group` behavior unchanged: Tasks avoid modifying group pricing/access code.
- Cross-database migration compatibility: Task 1 uses GORM migration and simple int/string columns.
- Tests: Tasks 1, 2, 3, and 6.

Placeholder scan:

- The plan contains no `TBD`, `TODO`, or undefined future requirements.

Type consistency:

- Backend role constants are `OrganizationRoleMember`, `OrganizationRoleAdmin`, and `OrganizationRoleOwner`.
- Frontend role strings are `member`, `admin`, and `owner`.
- User fields are `organization_id` and `organization_role` in JSON and `OrganizationId` and `OrganizationRole` in Go.
