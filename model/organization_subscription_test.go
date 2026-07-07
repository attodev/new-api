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

func setupOrganizationSubscriptionTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	originalLOGDB := LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	require.NoError(t, DB.AutoMigrate(
		&User{},
		&Organization{},
		&OrganizationSubscriptionPlan{},
		&OrganizationUserSubscription{},
		&OrganizationSubscriptionPreConsumeRecord{},
	))
	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB = originalDB
		LOG_DB = originalLOGDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})
}

func TestCreateOrganizationSubscriptionFromPlanCancelsExistingActiveSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "owner", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleOwner, AffCode: "owner-aff"}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "member-aff"}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 100000, Status: OrganizationStatusEnabled}).Error)

	planA := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Small", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	planB := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Large", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 5000, Enabled: true}
	require.NoError(t, DB.Create(planA).Error)
	require.NoError(t, DB.Create(planB).Error)

	first, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, planA.Id, 1)
	require.NoError(t, err)
	require.Equal(t, "active", first.Status)

	second, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, planB.Id, 1)
	require.NoError(t, err)
	require.Equal(t, planB.Id, second.PlanId)

	var old OrganizationUserSubscription
	require.NoError(t, DB.First(&old, first.Id).Error)
	require.Equal(t, "cancelled", old.Status)
}

func TestCreateOrganizationSubscriptionFromPlanCancelsExpiredActiveSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "expired-active-aff"}).Error)
	planA := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Small", DurationUnit: SubscriptionDurationHour, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	planB := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Large", DurationUnit: SubscriptionDurationHour, DurationValue: 1, TotalAmount: 5000, Enabled: true}
	require.NoError(t, DB.Create(planA).Error)
	require.NoError(t, DB.Create(planB).Error)
	now := time.Now().Unix()
	expiredActive := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: planA.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 7200, EndTime: now - 3600, Status: "active"}
	require.NoError(t, DB.Create(expiredActive).Error)

	second, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, planB.Id, 1)
	require.NoError(t, err)
	require.Equal(t, planB.Id, second.PlanId)

	var activeCount int64
	require.NoError(t, DB.Model(&OrganizationUserSubscription{}).Where("organization_id = ? AND user_id = ? AND status = ?", 1, 2, "active").Count(&activeCount).Error)
	require.Equal(t, int64(1), activeCount)

	var old OrganizationUserSubscription
	require.NoError(t, DB.First(&old, expiredActive.Id).Error)
	require.Equal(t, "cancelled", old.Status)
}

func TestCreateOrganizationUserSubscriptionRejectsOutsideOrganizationUser(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 2, OrganizationRole: OrganizationRoleMember, AffCode: "outside-aff"}).Error)
	require.NoError(t, DB.Create(&Organization{Id: 1, Name: "atto", OwnerUserId: 1, Quota: 100000, Status: OrganizationStatusEnabled}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Small", DurationUnit: SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)

	_, err := CreateOrganizationUserSubscriptionFromPlan(1, 2, plan.Id, 1)
	require.ErrorContains(t, err, "target user is outside organization")
}

func TestCancelOrganizationUserSubscriptionCancelsExpiredActiveSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, Group: "default", OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "cancel-expired-aff"}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Hourly", DurationUnit: SubscriptionDurationHour, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	now := time.Now().Unix()
	expiredActive := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 7200, EndTime: now - 3600, Status: "active"}
	require.NoError(t, DB.Create(expiredActive).Error)

	_, err := CancelActiveOrganizationUserSubscriptionForUser(1, 2)
	require.NoError(t, err)

	var sub OrganizationUserSubscription
	require.NoError(t, DB.First(&sub, expiredActive.Id).Error)
	require.Equal(t, "cancelled", sub.Status)
	require.LessOrEqual(t, sub.EndTime, time.Now().Unix())
}

func TestPreConsumeOrganizationUserSubscriptionResetsPeriodicQuota(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "periodic-aff"}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Daily", DurationUnit: SubscriptionDurationDay, DurationValue: 7, TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetDaily, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	sub := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: 1, EndTime: time.Now().Add(24 * time.Hour).Unix(), Status: "active", LastResetTime: 1, NextResetTime: 2}
	require.NoError(t, DB.Create(sub).Error)

	res, err := PreConsumeOrganizationUserSubscription("req-1", 1, 2, "gpt-test", 100)
	require.NoError(t, err)
	require.Equal(t, sub.Id, res.OrganizationUserSubscriptionId)
	require.Equal(t, int64(100), res.AmountUsedAfter)
}

func TestHasActiveOrganizationUserSubscriptionRenewsExpiredSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "renew-aff"}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Hourly", DurationUnit: SubscriptionDurationHour, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	now := time.Now().Unix()
	sub := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 7200, EndTime: now - 3600, Status: "active"}
	require.NoError(t, DB.Create(sub).Error)

	hasActive, err := HasActiveOrganizationUserSubscription(1, 2)
	require.NoError(t, err)
	require.True(t, hasActive)

	var renewed OrganizationUserSubscription
	require.NoError(t, DB.First(&renewed, sub.Id).Error)
	require.Equal(t, int64(0), renewed.AmountUsed)
	require.Greater(t, renewed.EndTime, now)
	require.Equal(t, int64(1000), renewed.AmountTotal)
}

func TestPreConsumeOrganizationUserSubscriptionRenewsExpiredSubscription(t *testing.T) {
	setupOrganizationSubscriptionTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationId: 1, OrganizationRole: OrganizationRoleMember, AffCode: "renew-preconsume-aff"}).Error)
	plan := &OrganizationSubscriptionPlan{OrganizationId: 1, Title: "Hourly", DurationUnit: SubscriptionDurationHour, DurationValue: 1, TotalAmount: 1000, Enabled: true}
	require.NoError(t, DB.Create(plan).Error)
	now := time.Now().Unix()
	sub := &OrganizationUserSubscription{OrganizationId: 1, UserId: 2, PlanId: plan.Id, AmountTotal: 1000, AmountUsed: 900, StartTime: now - 7200, EndTime: now - 3600, Status: "active"}
	require.NoError(t, DB.Create(sub).Error)

	res, err := PreConsumeOrganizationUserSubscription("req-renew", 1, 2, "gpt-test", 100)
	require.NoError(t, err)
	require.Equal(t, sub.Id, res.OrganizationUserSubscriptionId)
	require.Equal(t, int64(0), res.AmountUsedBefore)
	require.Equal(t, int64(100), res.AmountUsedAfter)
}
