package helper

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

// TestModelPriceHelperTieredInputPreConsumeMultiplier pins the reservation
// policy for tiered expressions: only the estimated input cost (C=0) is
// reserved, scaled by quota_setting.pre_consume_multiplier; client output
// limits are ignored, the context-length tier is chosen from the unscaled
// input, and actual settlement is unaffected by the multiplier.
func TestModelPriceHelperTieredInputPreConsumeMultiplier(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"tiered-fallback-model":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"tiered-fallback-model":"len <= 1200 ? tier(\"base\", p * 3 + c * 15) : tier(\"long\", p * 30 + c * 150)"}`,
		"group_ratio_setting.group_ratio": `{"default":1,"free":0}`,
	}))

	cases := []struct {
		name       string
		group      string
		prompt     int
		maxTokens  int
		multiplier float64
		expected   int
	}{
		{"default uses only input", "default", 1000, 0, 1, 1500},
		{"explicit output limit does not increase reservation", "default", 1000, 100, 1, 1500},
		{"fraction below one", "default", 1000, 0, 0.5, 750},
		{"fraction above one preserves context tier", "default", 1000, 0, 2.5, 3750},
		{"small input has no token floor", "default", 100, 0, 1, 150},
		{"zero input", "default", 0, 100, 1, 0},
		{"free group", "free", 1000, 0, 2.5, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.Header.Set("Content-Type", "application/json")
			ctx.Request = req
			ctx.Set("group", tc.group)

			info := &relaycommon.RelayInfo{
				OriginModelName: "tiered-fallback-model",
				UserGroup:       tc.group,
				UsingGroup:      tc.group,
				RequestHeaders:  map[string]string{"Content-Type": "application/json"},
				BillingRequestInput: &billingexpr.RequestInput{
					Headers: map[string]string{"Content-Type": "application/json"},
					Body:    []byte(`{}`),
				},
			}

			operation_setting.GetQuotaSetting().PreConsumeMultiplier = tc.multiplier
			priceData, err := ModelPriceHelper(ctx, info, tc.prompt, &types.TokenCountMeta{MaxTokens: tc.maxTokens})
			require.NoError(t, err)
			assert.Equal(t, tc.expected, priceData.QuotaToPreConsume)
			require.NotNil(t, info.TieredBillingSnapshot)
			assert.Equal(t, "base", info.TieredBillingSnapshot.EstimatedTier)
			assert.Equal(t, 0, info.TieredBillingSnapshot.EstimatedCompletionTokens)
			assert.Equal(t, tc.multiplier, info.TieredBillingSnapshot.PreConsumeMultiplier)
			actual, err := billingexpr.ComputeTieredQuotaWithRequest(info.TieredBillingSnapshot, billingexpr.TokenParams{P: 1000, C: 100, Len: 1000}, *info.BillingRequestInput)
			require.NoError(t, err)
			wantActual := 2250
			if tc.group == "free" {
				wantActual = 0
			}
			assert.Equal(t, wantActual, actual.ActualQuotaAfterGroup, "reservation multiplier must not change settlement")
		})
	}
}

// TestInputPreConsumeMultiplierLegacyAndRequestPrices covers ratio-priced
// (token) models, which reserve estimated input times the multiplier with no
// PreConsumedQuota floor and no max_tokens add, and per-request priced models,
// which the multiplier must not touch.
func TestInputPreConsumeMultiplierLegacyAndRequestPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.GlobalConfig.ExportAllConfigs()
	previousRatios, previousPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(previous))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"legacy-input-policy":1.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"request-input-policy":0.01}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))
	for _, tc := range []struct {
		model        string
		multiplier   float64
		prompt, want int
	}{
		{"legacy-input-policy", 0.5, 100, 75},
		{"legacy-input-policy", 1.5, 1000, 2250},
		{"legacy-input-policy", 2.5, 100, 375},
		{"legacy-input-policy", 1, 0, 0},
		{"request-input-policy", 2.5, 100, 5000},
	} {
		t.Run(tc.model+"/"+fmt.Sprint(tc.multiplier, "/", tc.prompt), func(t *testing.T) {
			operation_setting.GetQuotaSetting().PreConsumeMultiplier = tc.multiplier
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{}}
			price, err := ModelPriceHelper(ctx, info, tc.prompt, &types.TokenCountMeta{MaxTokens: 10000})
			require.NoError(t, err)
			assert.Equal(t, tc.want, price.QuotaToPreConsume)
		})
	}
}

// TestInputPreConsumeMultiplierRejectsInvalidRuntimeValue makes sure a
// multiplier that bypassed option validation fails the request instead of
// reserving nothing (or overflowing).
func TestInputPreConsumeMultiplierRejectsInvalidRuntimeValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := config.GlobalConfig.ExportAllConfigs()
	previousRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(previous))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"legacy-overflow":1.5}`))
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":    `{"policy-overflow":"tiered_expr"}`,
		"billing_setting.billing_expr":    `{"policy-overflow":"tier(\"base\", p * 3)"}`,
		"group_ratio_setting.group_ratio": `{"default":1}`,
	}))
	for _, modelName := range []string{"policy-overflow", "legacy-overflow"} {
		for _, multiplier := range []float64{0, -0.5, math.NaN(), math.Inf(1), math.MaxFloat64} {
			operation_setting.GetQuotaSetting().PreConsumeMultiplier = multiplier
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{OriginModelName: modelName, UserGroup: "default", UsingGroup: "default", BillingRequestInput: &billingexpr.RequestInput{}}
			_, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
			require.Error(t, err, "%s multiplier=%v", modelName, multiplier)
			assert.Nil(t, info.TieredBillingSnapshot)
		}
	}
}
