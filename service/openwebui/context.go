package openwebui

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// SetIdentity records that this request was attributed to the Open WebUI
// user sub (and, when non-empty, the background task that triggered it).
func SetIdentity(c *gin.Context, sub, task string) {
	common.SetContextKey(c, constant.ContextKeyOpenWebUISub, sub)
	common.SetContextKey(c, constant.ContextKeyOpenWebUITask, task)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIAttributed, true)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, false)
}

// SetUnsigned records that the request carried the service key but no JWT
// and is being billed to the service token itself.
func SetUnsigned(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyOpenWebUIAttributed, false)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, true)
}

func GetSub(c *gin.Context) string {
	return common.GetContextKeyString(c, constant.ContextKeyOpenWebUISub)
}

func GetTask(c *gin.Context) string {
	return common.GetContextKeyString(c, constant.ContextKeyOpenWebUITask)
}

func IsAttributed(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyOpenWebUIAttributed)
}

func IsUnsigned(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyOpenWebUIUnsigned)
}
