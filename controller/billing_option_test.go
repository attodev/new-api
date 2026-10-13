package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func putOptionForBillingTest(t *testing.T, key, value string) bool {
	t.Helper()
	body, err := common.Marshal(OptionUpdateRequest{Key: key, Value: value})
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
	UpdateOption(c)
	var response struct{ Success bool }
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Success
}

// TestPreConsumePolicyOptionsAndReservation covers configuration persistence,
// validation, reservation and reconciliation through the same public entry
// points used by the relay (ported from upstream 9978ee1e2, SQLite only: the
// change adds no raw SQL).
func TestPreConsumePolicyOptionsAndReservation(t *testing.T) {
	previousConfig := config.GlobalConfig.ExportAllConfigs()
	previousUnit, previousBatch := common.QuotaPerUnit, common.BatchUpdateEnabled
	common.QuotaPerUnit, common.BatchUpdateEnabled = 500000, false
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(previousConfig))
		common.QuotaPerUnit, common.BatchUpdateEnabled = previousUnit, previousBatch
	})

	common.OptionMapRWMutex.Lock()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Token{}, &model.Log{}, &model.Organization{}))

	for key, value := range map[string]string{
		"quota_setting.trust_quota_usd":        "10.5",
		"quota_setting.pre_consume_multiplier": "1.5",
	} {
		require.True(t, putOptionForBillingTest(t, key, value), "%s=%s", key, value)
		var saved model.Option
		require.NoError(t, db.Where(&model.Option{Key: key}).First(&saved).Error)
		assert.Equal(t, value, saved.Value)
	}
	var savedOptions []model.Option
	require.NoError(t, db.Find(&savedOptions).Error)
	stored := map[string]string{}
	for _, option := range savedOptions {
		stored[option.Key] = option.Value
	}
	for range 2 {
		operation_setting.GetQuotaSetting().TrustQuotaUSD = 10
		operation_setting.GetQuotaSetting().PreConsumeMultiplier = 1
		require.NoError(t, config.GlobalConfig.LoadFromDB(stored))
		assert.Equal(t, 10.5, operation_setting.GetQuotaSetting().TrustQuotaUSD)
		assert.Equal(t, 1.5, operation_setting.GetQuotaSetting().PreConsumeMultiplier)
	}

	for _, tc := range []struct{ key, value string }{
		{"trust_quota_usd", "-1"}, {"trust_quota_usd", "NaN"}, {"trust_quota_usd", "+Inf"},
		{"pre_consume_multiplier", "0"}, {"pre_consume_multiplier", "-0.5"},
		{"pre_consume_multiplier", "NaN"}, {"pre_consume_multiplier", "Inf"},
		{"pre_consume_multiplier", "1e309"}, {"pre_consume_multiplier", ""},
	} {
		key := "quota_setting." + tc.key
		assert.False(t, putOptionForBillingTest(t, key, tc.value), "%s=%q", key, tc.value)
		var saved model.Option
		require.NoError(t, db.Where(&model.Option{Key: key}).First(&saved).Error)
		assert.Equal(t, stored[key], saved.Value, "invalid saves must preserve the previous value")
		assert.Error(t, model.UpdateOption(key, tc.value), "model layer must reject %s=%q too", key, tc.value)
		assert.Error(t, model.UpdateOptionsBulk(map[string]string{key: tc.value}), "bulk update must reject %s=%q too", key, tc.value)
		require.NoError(t, db.Where(&model.Option{Key: key}).First(&saved).Error)
		assert.Equal(t, stored[key], saved.Value)
	}
	assert.Equal(t, 10.5, operation_setting.GetQuotaSetting().TrustQuotaUSD)
	assert.Equal(t, 1.5, operation_setting.GetQuotaSetting().PreConsumeMultiplier)
	require.True(t, putOptionForBillingTest(t, "quota_setting.trust_quota_usd", "0"), "trust 0 disables the bypass and must be accepted")

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"policy-model":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"policy-model":"tier(\"base\", p * 3 + c * 15)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))
	cases := []struct {
		name                  string
		threshold, multiplier float64
		wallet                int64
		token                 int
		force, unlimited      bool
		wantHeld              int
	}{
		{"above threshold", 10, 1, 5500000, 5500000, false, false, 0},
		{"custom threshold", 20, 1, 5500000, 5500000, false, false, 1500},
		{"zero disables bypass", 0, 1, 5500000, 5500000, false, false, 1500},
		{"wallet equals threshold", 10, 1, 5000000, 5500000, false, false, 1500},
		{"token equals threshold", 10, 1, 5500000, 5000000, false, false, 1500},
		{"fractional threshold", 10.5, 1, 5250001, 5250001, false, false, 0},
		{"unlimited token", 10, 1, 5500000, 0, false, true, 0},
		{"forced reservation", 10, 1, 5500000, 5500000, true, false, 1500},
		{"half input cost", 0, 0.5, 100000, 100000, false, false, 750},
		{"fractional multiple refunds excess", 0, 2.5, 100000, 100000, false, false, 3750},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, model.UpdateOption("quota_setting.trust_quota_usd", strconv.FormatFloat(tc.threshold, 'f', -1, 64)))
			require.NoError(t, model.UpdateOption("quota_setting.pre_consume_multiplier", strconv.FormatFloat(tc.multiplier, 'f', -1, 64)))
			user := model.User{Username: fmt.Sprintf("policy-user-%d", i), Quota: tc.wallet, Group: "default", AffCode: fmt.Sprintf("policy-aff-%d", i), Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			token := model.Token{UserId: user.Id, Key: fmt.Sprintf("policy-token-%d", i), RemainQuota: tc.token, UnlimitedQuota: tc.unlimited, Status: common.TokenStatusEnabled}
			require.NoError(t, db.Create(&token).Error)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Set("token_quota", tc.token)
			info := &relaycommon.RelayInfo{UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, TokenUnlimited: tc.unlimited, ForcePreConsume: tc.force, OriginModelName: "policy-model", UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1}}
			info.UserSetting.BillingPreference = "wallet_only"
			price, err := helper.ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{MaxTokens: 10000})
			require.NoError(t, err)
			require.Nil(t, service.PreConsumeBilling(ctx, price.QuotaToPreConsume, info))
			assert.Equal(t, tc.wantHeld, info.FinalPreConsumedQuota)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.wallet-int64(tc.wantHeld), user.Quota)
			if !tc.unlimited {
				require.NoError(t, db.First(&token, token.Id).Error)
				assert.Equal(t, tc.token-tc.wantHeld, token.RemainQuota)
			}
			_, actual, _ := service.TryTieredSettle(info, billingexpr.TokenParams{P: 1000, C: 100, Len: 1000})
			assert.Equal(t, 2250, actual)
			require.NoError(t, info.Billing.Settle(actual))
			require.NoError(t, info.Billing.Settle(actual))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, tc.wallet-int64(actual), user.Quota)
			if !tc.unlimited {
				require.NoError(t, db.First(&token, token.Id).Error)
				assert.Equal(t, tc.token-actual, token.RemainQuota)
			}
		})
	}
}
