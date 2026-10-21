// fork: O13 — Open WebUI attribution keys in usage-log other (ported from OpenWebUI service/log_info_generate.go).

package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// appendOpenWebUIInfo records which Open WebUI user (JWT sub) and, for
// background work, which task a request was attributed to, or that it was
// an unsigned service-key request. Keys are prefixed owui_ so they are easy
// to find in the log detail view and to count for the "unsigned requests"
// operational metric. They are public: the log owner is the attributed user
// (or the service account for unsigned requests), and the sub is that
// user's own Open WebUI id.
func appendOpenWebUIInfo(ctx *gin.Context, other *model.LogOther) {
	if ctx == nil || other == nil {
		return
	}
	if sub := common.GetContextKeyString(ctx, constant.ContextKeyOpenWebUISub); sub != "" {
		other.SetPublic("owui_sub", sub)
		if task := common.GetContextKeyString(ctx, constant.ContextKeyOpenWebUITask); task != "" {
			other.SetPublic("owui_task", task)
		}
	}
	if common.GetContextKeyBool(ctx, constant.ContextKeyOpenWebUIUnsigned) {
		other.SetPublic("owui_unsigned", true)
	}
}
