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

	originalDB := DB
	originalLOGDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

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
		DB = originalDB
		LOG_DB = originalLOGDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
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
