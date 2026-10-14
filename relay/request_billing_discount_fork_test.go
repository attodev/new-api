package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fork: O6 (review M2) — a 100% discount makes a paid model cost 0 but never
// turns it into a "free model": with free-model pre-consume off, the billing
// session (organization status and member suspension checks) still runs and
// reserves 0.

func setupDiscountGateTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch, oldLogConsume := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	quotaSetting := operation_setting.GetQuotaSetting()
	oldFreePreConsume := quotaSetting.EnableFreeModelPreConsume
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	quotaSetting.EnableFreeModelPreConsume = false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldLogConsume
		quotaSetting.EnableFreeModelPreConsume = oldFreePreConsume
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	require.NoError(t, model.MigrateForkOrganizationTables(db))

	savedRatios := ratio_setting.ModelRatio2JSONString()
	ratios := ratio_setting.GetModelRatioCopy()
	ratios["full-discount-model"] = 2
	raw, err := common.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(raw)))
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"full-discount-model":100}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
	})

	require.NoError(t, db.Create(&model.Organization{Id: 1, Name: "org", OwnerUserId: 1, Status: model.OrganizationStatusEnabled, Quota: 1000}).Error)
	for _, u := range []model.User{
		{Id: 1, Username: "owner", Role: common.RoleCommonUser, OrganizationRole: model.OrganizationRoleOwner},
		{Id: 2, Username: "member", Role: common.RoleCommonUser, OrganizationRole: model.OrganizationRoleMember},
	} {
		u.Password, u.Status, u.Quota, u.OrganizationId, u.AffCode = "password", common.UserStatusEnabled, 100, 1, u.Username
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Create(&model.Channel{Id: 7, Name: "c", Key: "sk-test", Status: common.ChannelStatusEnabled}).Error)
	return db
}

func discountGateRelayInfo(userId int) (*gin.Context, *relaycommon.RelayInfo) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return ctx, &relaycommon.RelayInfo{
		UserId:          userId,
		OriginModelName: "full-discount-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		IsPlayground:    true,
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 7},
	}
}

func TestFullDiscountStillRefusesSuspendedOrganizationMember(t *testing.T) {
	db := setupDiscountGateTest(t)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).
		Update("organization_member_status", model.OrganizationMemberStatusSuspended).Error)

	ctx, info := discountGateRelayInfo(2)
	apiErr := PrepareRequestBilling(ctx, info)
	require.NotNil(t, apiErr, "a 100%% discount must not skip the billing session")
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "organization membership is suspended")
	require.False(t, info.PriceData.FreeModel)
}

func TestFullDiscountBillsActiveOrganizationMemberZero(t *testing.T) {
	db := setupDiscountGateTest(t)

	ctx, info := discountGateRelayInfo(2)
	require.Nil(t, PrepareRequestBilling(ctx, info))
	require.False(t, info.PriceData.FreeModel)
	require.NotNil(t, info.Billing, "the organization billing session runs and reserves 0")
	require.Zero(t, info.PriceData.QuotaToPreConsume)

	service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100}, nil)

	var org model.Organization
	require.NoError(t, db.First(&org, 1).Error)
	require.Equal(t, int64(1000), org.Quota, "nothing is charged to the organization wallet")
	var log model.Log
	require.NoError(t, db.Where("user_id = ?", 2).Order("id desc").First(&log).Error)
	require.Zero(t, log.Quota)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	require.Contains(t, other, "discount_multiplier")
	require.InDelta(t, 0, other["discount_multiplier"], 1e-9)
	require.Equal(t, "model", other["discount_source"])
	require.InDelta(t, ratio_setting.GetGroupRatio("default"), other["group_ratio"], 1e-9)
}
