package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleGroupRatioFoldsEffectiveDiscount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"gpt-4":30}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateModelDiscountByJSONString(`{}`) })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "gpt-4", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{}}

	got := HandleGroupRatio(c, info)

	require.InDelta(t, 0.7, got.DiscountMultiplier, 1e-9)
	require.Equal(t, "model", got.DiscountSource)
	require.InDelta(t, got.BaseGroupRatio*0.7, got.GroupRatio, 1e-9)
}

func TestHandleGroupRatioWithoutDiscountIsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "claude-3", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{}}

	got := HandleGroupRatio(c, info)

	require.InDelta(t, 1.0, got.DiscountMultiplier, 1e-9)
	require.Equal(t, "", got.DiscountSource)
	require.InDelta(t, got.BaseGroupRatio, got.GroupRatio, 1e-9)
}

func TestFoldDiscountFullDiscountZeroesGroupRatio(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"free-model":100}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateModelDiscountByJSONString(`{}`) })
	info := hosttypes.GroupRatioInfo{GroupRatio: 1.5}

	foldDiscountIntoGroupRatio(&info, "free-model", "free-model")

	require.Equal(t, 0.0, info.GroupRatio)
	require.Equal(t, 0.0, info.DiscountMultiplier)
	require.Equal(t, 1.5, info.BaseGroupRatio)
	require.Equal(t, "model", info.DiscountSource)
}

func TestFoldDiscountVendorOnlyDiscount(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateVendorDiscountByJSONString(`{"acme-test-vendor":25}`))
	prev := ratio_setting.SetVendorResolver(func(name string) (string, bool) {
		if name == "acme-model" {
			return "acme-test-vendor", true
		}
		return "", false
	})
	t.Cleanup(func() {
		ratio_setting.SetVendorResolver(prev)
		_ = ratio_setting.UpdateVendorDiscountByJSONString(`{}`)
	})
	info := hosttypes.GroupRatioInfo{GroupRatio: 2}

	foldDiscountIntoGroupRatio(&info, "acme-model", "acme-model")

	require.InDelta(t, 0.75, info.DiscountMultiplier, 1e-9)
	require.Equal(t, "vendor", info.DiscountSource)
	require.InDelta(t, 1.5, info.GroupRatio, 1e-9)
	require.InDelta(t, 2.0, info.BaseGroupRatio, 1e-9)
}

func TestHandleGroupRatioResolvesDiscountByOriginThenBillingName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	t.Cleanup(func() { _ = ratio_setting.UpdateModelDiscountByJSONString(`{}`) })
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName:  "claude-sonnet-4-5-thinking",
			BillingModelName: "claude-sonnet-4-5",
			UsingGroup:       "default",
			ChannelMeta:      &relaycommon.ChannelMeta{},
		}
	}

	// discount listed only on the origin (marketplace) name
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"claude-sonnet-4-5-thinking":30}`))
	got := HandleGroupRatio(c, newInfo())
	require.InDelta(t, 0.7, got.DiscountMultiplier, 1e-9)
	require.InDelta(t, got.BaseGroupRatio*0.7, got.GroupRatio, 1e-9)

	// discount listed only on the billing name, origin has none
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"claude-sonnet-4-5":20}`))
	got = HandleGroupRatio(c, newInfo())
	require.InDelta(t, 0.8, got.DiscountMultiplier, 1e-9)
	require.InDelta(t, got.BaseGroupRatio*0.8, got.GroupRatio, 1e-9)

	// origin wins when both are set
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"claude-sonnet-4-5-thinking":30,"claude-sonnet-4-5":20}`))
	got = HandleGroupRatio(c, newInfo())
	require.InDelta(t, 0.7, got.DiscountMultiplier, 1e-9)
}

// Review M2: the free-model gate reads the pre-discount group ratio.
func TestFreeModelGroupRatioIgnoresDiscount(t *testing.T) {
	require.Zero(t, freeModelGroupRatio(hosttypes.GroupRatioInfo{GroupRatio: 0, BaseGroupRatio: 0, DiscountMultiplier: 1}),
		"a free group stays free")
	require.Equal(t, 1.5, freeModelGroupRatio(hosttypes.GroupRatioInfo{GroupRatio: 0, BaseGroupRatio: 1.5, DiscountMultiplier: 0, DiscountSource: "model"}),
		"a 100% discount is not a free model")
	require.Equal(t, 2.0, freeModelGroupRatio(hosttypes.GroupRatioInfo{GroupRatio: 2}),
		"PriceData not built by HandleGroupRatio keeps its group ratio")
}

func TestFullDiscountPerCallModelIsNotFree(t *testing.T) {
	gin.SetMode(gin.TestMode)
	quotaSetting := operation_setting.GetQuotaSetting()
	oldFreePreConsume := quotaSetting.EnableFreeModelPreConsume
	quotaSetting.EnableFreeModelPreConsume = false
	savedPrices := ratio_setting.ModelPrice2JSONString()
	prices := ratio_setting.GetModelPriceCopy()
	prices["full-discount-per-call"] = 0.04
	raw, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(raw)))
	require.NoError(t, ratio_setting.UpdateModelDiscountByJSONString(`{"full-discount-per-call":100}`))
	t.Cleanup(func() {
		quotaSetting.EnableFreeModelPreConsume = oldFreePreConsume
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		_ = ratio_setting.UpdateModelDiscountByJSONString(`{}`)
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "full-discount-per-call", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{}}

	priceData, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	require.False(t, priceData.FreeModel)
	require.Zero(t, priceData.Quota)
}
