package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// setDiscountTestModelRatio registers a model ratio for the test and restores
// the previous ratio map and discount maps afterwards.
func setDiscountTestModelRatio(t *testing.T, modelName string, ratio float64) {
	t.Helper()
	saved := ratio_setting.ModelRatio2JSONString()
	ratios := ratio_setting.GetModelRatioCopy()
	ratios[modelName] = ratio
	raw, err := common.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(raw)))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(saved))
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
	})
}

func TestPreWssConsumeQuotaAppliesDiscount(t *testing.T) {
	truncate(t)
	// GetTokenByKey needs the dialect's quoted key column, which InitLogDB sets
	// (same seam as controller/responses_websocket_test.go).
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	const userID, tokenID = 81, 81
	const initQuota = 100_000
	setDiscountTestModelRatio(t, "disc-wss-model", 1)
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"disc-wss-model":30}`))
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "discwsskey", initQuota)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		TokenKey:        "sk-discwsskey",
		OriginModelName: "disc-wss-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 1000}}

	require.NoError(t, PreWssConsumeQuota(ctx, info, usage))

	want := int(1000 * ratio_setting.GetGroupRatio("default") * 0.7)
	require.Equal(t, initQuota-want, getUserQuota(t, userID))
}

func TestRecalculateTaskQuotaByTokensAppliesDiscount(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 82, 82, 82
	const initQuota, preConsumed, tokens = 100_000, 1000, 1000
	setDiscountTestModelRatio(t, "disc-task-model", 1)
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"disc-task-model":30}`))
	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-disc-task", initQuota)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Properties.OriginModelName = "disc-task-model"
	task.PrivateData.BillingContext.OriginModelName = "disc-task-model"

	require.True(t, RecalculateTaskQuotaByTokens(context.Background(), task, tokens))

	want := int(tokens * ratio_setting.GetGroupRatio("default") * 0.7)
	require.Equal(t, want, task.Quota)
}

func TestDiscountBaseGroupRatioForLogContent(t *testing.T) {
	discounted := &relaycommon.RelayInfo{PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{
		GroupRatio: 0.63, BaseGroupRatio: 0.9, DiscountMultiplier: 0.7, DiscountSource: "model"}}}
	require.InDelta(t, 0.9, discountBaseGroupRatio(discounted), 1e-9)

	full := &relaycommon.RelayInfo{PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{
		GroupRatio: 0, BaseGroupRatio: 1.2, DiscountMultiplier: 0, DiscountSource: "vendor"}}}
	require.InDelta(t, 1.2, discountBaseGroupRatio(full), 1e-9)

	// PriceData not built by HandleGroupRatio (no BaseGroupRatio): the raw ratio is shown
	raw := &relaycommon.RelayInfo{PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1.5}}}
	require.InDelta(t, 1.5, discountBaseGroupRatio(raw), 1e-9)
}
