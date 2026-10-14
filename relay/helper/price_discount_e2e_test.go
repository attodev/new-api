package helper

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestDiscountedTextPreConsumeMatchesSettlement runs the real pre-consume
// (ModelPriceHelper) and settlement (PostTextConsumeQuota) paths with a model
// discount configured and checks both charge the same discounted quota once.
func TestDiscountedTextPreConsumeMatchesSettlement(t *testing.T) {
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
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMain, oldLog)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldRedis, oldBatch, oldLogConsume
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	const userID, channelID = 91, 91
	require.NoError(t, db.Create(&model.User{Id: userID, Username: "disc_e2e", Quota: 1_000_000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "disc_e2e", Key: "sk-test", Status: common.ChannelStatusEnabled}).Error)

	savedRatios := ratio_setting.ModelRatio2JSONString()
	ratios := ratio_setting.GetModelRatioCopy()
	ratios["disc-e2e-model"] = 2
	raw, err := common.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(raw)))
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"disc-e2e-model":30}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		OriginModelName: "disc-e2e-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
	}
	const promptTokens = 1000

	priceData, err := ModelPriceHelper(ctx, info, promptTokens, &types.TokenCountMeta{})
	require.NoError(t, err)
	want := int(promptTokens * 2 * ratio_setting.GetGroupRatio("default") * 0.7)
	require.Equal(t, want, priceData.QuotaToPreConsume)

	service.PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: promptTokens, TotalTokens: promptTokens}, nil)

	var log model.Log
	require.NoError(t, db.Order("id desc").First(&log).Error)
	require.Equal(t, priceData.QuotaToPreConsume, log.Quota)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	require.InDelta(t, 0.7, other["discount_multiplier"], 1e-9)
	require.Equal(t, "model", other["discount_source"])
	require.InDelta(t, ratio_setting.GetGroupRatio("default"), other["group_ratio"], 1e-9)
}
