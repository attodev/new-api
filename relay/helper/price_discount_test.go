package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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
