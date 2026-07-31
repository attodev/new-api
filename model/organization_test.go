package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

func TestCreateOrganizationRejectsUserAlreadyInOrganization(t *testing.T) {
	setupOrganizationModelTestDB(t)

	owner := User{Username: "owner", Password: "password", DisplayName: "Owner", Role: common.RoleCommonUser, AffCode: "owner"}
	existingMember := User{
		Username:         "existing-member",
		Password:         "password",
		DisplayName:      "Existing Member",
		Role:             common.RoleCommonUser,
		OrganizationId:   7,
		OrganizationRole: OrganizationRoleMember,
		AffCode:          "existing-member",
	}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&existingMember).Error)

	org, err := CreateOrganization("Acme", "Main customer", existingMember.Id)
	require.Error(t, err)
	require.Nil(t, org)
	require.Contains(t, err.Error(), "user already belongs to an organization")
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

func TestGetOrganizationDashboardScopesQuotaDataToOrganization(t *testing.T) {
	setupOrganizationModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}))

	owner := User{Username: "dashboard-owner", Password: "password", Role: common.RoleCommonUser, Quota: 700, UsedQuota: 70, AffCode: "dash-owner"}
	require.NoError(t, DB.Create(&owner).Error)
	org, err := CreateOrganization("Dashboard Org", "Dashboard customer", owner.Id, 1000)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Organization{}).Where("id = ?", org.Id).Update("used_quota", 125).Error)

	member := User{
		Username:         "dashboard-member",
		Password:         "password",
		DisplayName:      "Member Display",
		Role:             common.RoleCommonUser,
		Quota:            500,
		UsedQuota:        55,
		AffCode:          "dash-member",
		OrganizationId:   org.Id,
		OrganizationRole: OrganizationRoleMember,
	}
	require.NoError(t, DB.Create(&member).Error)

	outsideOwner := User{Username: "outside-owner", Password: "password", DisplayName: "Outside Owner", Role: common.RoleCommonUser, AffCode: "outside-owner"}
	require.NoError(t, DB.Create(&outsideOwner).Error)
	outsideOrg, err := CreateOrganization("Outside Org", "Other customer", outsideOwner.Id)
	require.NoError(t, err)
	outsideUser := User{
		Username:         "outside-member",
		Password:         "password",
		DisplayName:      "Outside Member",
		Role:             common.RoleCommonUser,
		AffCode:          "outside-member",
		OrganizationId:   outsideOrg.Id,
		OrganizationRole: OrganizationRoleMember,
	}
	require.NoError(t, DB.Create(&outsideUser).Error)

	dayOne := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).Unix()
	dayTwo := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC).Unix()
	rows := []QuotaData{
		{UserID: owner.Id, Username: owner.Username, ModelName: "gpt-4o", CreatedAt: dayOne, Count: 1, Quota: 100, TokenUsed: 10},
		{UserID: owner.Id, Username: owner.Username, ModelName: "", CreatedAt: dayOne + 3600, Count: 2, Quota: 50, TokenUsed: 5},
		{UserID: member.Id, Username: member.Username, ModelName: "claude-3-5", CreatedAt: dayTwo, Count: 3, Quota: 200, TokenUsed: 20},
		{UserID: outsideUser.Id, Username: outsideUser.Username, ModelName: "gpt-4o", CreatedAt: dayOne, Count: 7, Quota: 999, TokenUsed: 99},
	}
	require.NoError(t, DB.Create(&rows).Error)

	dashboard, err := GetOrganizationDashboard(org.Id, dayOne, dayTwo)
	require.NoError(t, err)
	require.NotNil(t, dashboard)

	require.Equal(t, org.Id, dashboard.Organization.Id)
	require.Equal(t, "Dashboard Org", dashboard.Organization.Name)
	require.Equal(t, int64(1000), dashboard.Organization.Quota)
	require.Equal(t, int64(125), dashboard.Organization.UsedQuota)

	require.Equal(t, 350, dashboard.Summary.PeriodQuota)
	require.Equal(t, 6, dashboard.Summary.PeriodRequests)
	require.Equal(t, 2, dashboard.Summary.MemberCount)
	require.Equal(t, 2, dashboard.Summary.ActiveMemberCount)

	require.Equal(t, []OrganizationDashboardDailyUsage{
		{Date: "2026-01-02", Quota: 150, Requests: 3},
		{Date: "2026-01-03", Quota: 200, Requests: 3},
	}, dashboard.DailyUsage)

	require.Equal(t, []OrganizationDashboardUserUsage{
		{
			UserID:         member.Id,
			Username:       member.Username,
			DisplayName:    "Member Display",
			Quota:          500,
			UsedQuota:      55,
			PeriodQuota:    200,
			PeriodRequests: 3,
		},
		{
			UserID:         owner.Id,
			Username:       owner.Username,
			DisplayName:    owner.Username,
			Quota:          700,
			UsedQuota:      70,
			PeriodQuota:    150,
			PeriodRequests: 3,
		},
	}, dashboard.TopUsers)

	require.Equal(t, []OrganizationDashboardModelUsage{
		{Model: "claude-3-5", Quota: 200, Requests: 3},
		{Model: "gpt-4o", Quota: 100, Requests: 1},
		{Model: "(unknown)", Quota: 50, Requests: 2},
	}, dashboard.TopModels)

	require.Equal(t, []OrganizationDashboardModelDailyUsage{
		{Date: "2026-01-02", Model: "(unknown)", Quota: 50, Requests: 2},
		{Date: "2026-01-02", Model: "gpt-4o", Quota: 100, Requests: 1},
		{Date: "2026-01-03", Model: "claude-3-5", Quota: 200, Requests: 3},
	}, dashboard.ModelUsage)

	require.Equal(t, []OrganizationDashboardUserDailyUsage{
		{Date: "2026-01-02", UserID: owner.Id, Username: owner.Username, DisplayName: owner.Username, Quota: 150, Requests: 3},
		{Date: "2026-01-03", UserID: member.Id, Username: member.Username, DisplayName: "Member Display", Quota: 200, Requests: 3},
	}, dashboard.UserUsage)
}
