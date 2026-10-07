package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAppendOpenWebUIInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("attributed request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "title_generation")
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, "owui-7", other["owui_sub"])
		require.Equal(t, "title_generation", other["owui_task"])
		_, hasUnsigned := other["owui_unsigned"]
		require.False(t, hasUnsigned)
	})

	t.Run("chat request has no task key", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "")
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, "owui-7", other["owui_sub"])
		_, hasTask := other["owui_task"]
		require.False(t, hasTask)
	})

	t.Run("unsigned request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, true)
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, true, other["owui_unsigned"])
		_, hasSub := other["owui_sub"]
		require.False(t, hasSub)
	})

	t.Run("ordinary request adds nothing", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Empty(t, other)
		appendOpenWebUIInfo(nil, other)
		appendOpenWebUIInfo(c, nil)
	})
}
