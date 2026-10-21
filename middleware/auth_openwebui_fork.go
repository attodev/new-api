// fork: O13 — Open WebUI service-key -> user-token substitution for relay TokenAuth.

package middleware

import (
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"

	"github.com/gin-gonic/gin"
)

// openWebUISubstitute is called by TokenAuth (relay only) after the arriving
// token passed validation and its IP allow-list, and before the user cache is
// read. When key is the configured Open WebUI service key it swaps *token for
// the token of the person identified by X-OpenWebUI-User-Jwt (or keeps the
// service token for an allowed unsigned request), so user status, group,
// SetupContextForToken and billing all run as that person. The substituted
// token's own IP list is deliberately not consulted: the IP check above was
// against the service token. Returns false after aborting the request.
//
// Never call this from TokenAuthReadOnly or the dashboard auth paths.
func openWebUISubstitute(c *gin.Context, key string, token **model.Token) bool {
	if !openwebui.IsServiceKey(key) {
		return true
	}
	substituted, rejectErr := openwebui.Resolve(c, *token)
	if rejectErr != nil {
		abortWithOpenAiMessage(c, rejectErr.Status, i18n.T(c, rejectErr.MessageKey))
		return false
	}
	*token = substituted
	c.Set("id", substituted.UserId)
	return true
}
