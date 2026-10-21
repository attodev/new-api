// fork: O13 — Open WebUI attribution in usage-log other (ported from OpenWebUI service/log_info_openwebui_test.go).

package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAppendOpenWebUIInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("attributed request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "title_generation")
		other := model.NewLogOther()
		appendOpenWebUIInfo(c, other)
		snap := other.Snapshot()
		require.Equal(t, "owui-7", snap["owui_sub"])
		require.Equal(t, "title_generation", snap["owui_task"])
		_, hasUnsigned := snap["owui_unsigned"]
		require.False(t, hasUnsigned)
	})

	t.Run("chat request has no task key", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "")
		other := model.NewLogOther()
		appendOpenWebUIInfo(c, other)
		snap := other.Snapshot()
		require.Equal(t, "owui-7", snap["owui_sub"])
		_, hasTask := snap["owui_task"]
		require.False(t, hasTask)
	})

	t.Run("unsigned request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, true)
		other := model.NewLogOther()
		appendOpenWebUIInfo(c, other)
		snap := other.Snapshot()
		require.Equal(t, true, snap["owui_unsigned"])
		_, hasSub := snap["owui_sub"]
		require.False(t, hasSub)
	})

	t.Run("ordinary request adds nothing", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		other := model.NewLogOther()
		appendOpenWebUIInfo(c, other)
		require.Empty(t, other.Snapshot())
		appendOpenWebUIInfo(nil, other)
		appendOpenWebUIInfo(c, nil)
	})
}

// The hook in GenerateTextOtherInfo puts the keys in the public scope, so the
// log owner (the attributed user) sees them in the log detail view.
func TestGenerateTextOtherInfo_IncludesOpenWebUIInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
	common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "tags_generation")

	relayInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	other := GenerateTextOtherInfo(c, relayInfo, 1, 1, 1, 0, 0, 0, 1)
	snap := other.Snapshot()
	require.Equal(t, "owui-7", snap["owui_sub"])
	require.Equal(t, "tags_generation", snap["owui_task"])
	if admin, ok := snap["admin_info"].(map[string]any); ok {
		_, inAdmin := admin["owui_sub"]
		require.False(t, inAdmin)
	}
}
