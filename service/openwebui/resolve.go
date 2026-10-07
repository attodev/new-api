package openwebui

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const (
	// DefaultIssuer is the iss claim Open WebUI's _mint_forward_user_jwt() sets.
	DefaultIssuer = "open-webui"
	// HeaderUserJWT carries the per-request HS256 identity minted by Open WebUI.
	HeaderUserJWT = "X-OpenWebUI-User-Jwt"
	// HeaderTask is Open WebUI's connection-header template "{{TASK}}": empty
	// for ordinary chat, a task name (title_generation, ...) for background work.
	HeaderTask = "X-Task"
)

// RejectError is what Resolve returns instead of a token. MessageKey is the
// i18n key for the (deliberately vague) user-facing text; LogReason is the
// precise cause and must never be sent to the client.
type RejectError struct {
	Status     int
	MessageKey string
	LogReason  string
}

func (e *RejectError) Error() string {
	return e.LogReason
}

// IsServiceKey reports whether the parsed bearer key is the configured Open
// WebUI service key and the relay is enabled. Constant-time comparison.
func IsServiceKey(key string) bool {
	s := system_setting.GetOpenWebUISettings()
	if !s.RelayEnabled || s.ServiceApiKey == "" || key == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(s.ServiceApiKey)) == 1
}

// Resolve is the single entry point middleware.TokenAuth uses once it has
// established that the arriving key is the service key and that the service
// token's IP limits pass. It returns the token the rest of the request
// should run as:
//
//   - a valid JWT → the mapped user's "openwebui" or "openwebui-task" token
//   - no JWT and AllowUnsignedRequests → the service token itself
//   - anything else → a RejectError
func Resolve(c *gin.Context, serviceToken *model.Token) (*model.Token, *RejectError) {
	s := system_setting.GetOpenWebUISettings()
	raw := strings.TrimSpace(c.GetHeader(HeaderUserJWT))

	if raw == "" {
		if !s.AllowUnsignedRequests {
			return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "unsigned_request_rejected")
		}
		SetUnsigned(c)
		logger.LogInfo(c, fmt.Sprintf("openwebui: unsigned request billed to service token | path=%s", requestPath(c)))
		return serviceToken, nil
	}

	if s.JwtSecret == "" {
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "misconfigured: empty_secret")
	}

	// An empty issuer would disable jwt/v5's iss check entirely, so fall
	// back to the value Open WebUI always sends.
	issuer := s.JwtIssuer
	if issuer == "" {
		issuer = DefaultIssuer
	}
	claims, err := VerifyUserJWT(raw, JWTConfig{
		Secret:         s.JwtSecret,
		PreviousSecret: s.JwtPreviousSecret,
		Issuer:         issuer,
		Leeway:         time.Duration(s.JwtLeewaySeconds) * time.Second,
	})
	if err != nil {
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "jwt_invalid: "+err.Error())
	}

	userId, err := ResolveUserId(claims)
	if err != nil {
		var linkErr *LinkError
		if errors.As(err, &linkErr) {
			return nil, reject(c, http.StatusForbidden, i18n.MsgOpenWebUINotLinked,
				fmt.Sprintf("link_failed: %s | sub=%s email=%s", linkErr.Reason, claims.Subject, claims.Email))
		}
		return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError, "link_lookup_error: "+err.Error())
	}

	task := strings.TrimSpace(c.GetHeader(HeaderTask))
	token, err := GetOrCreateUserToken(userId, TokenNameForTask(task))
	if err != nil {
		return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError,
			fmt.Sprintf("provision_error: user=%d: %v", userId, err))
	}

	// Run the substituted token through the same validity checks a directly
	// presented key gets, so a user who disabled their openwebui token is
	// refused with the ordinary token error.
	validated, err := model.ValidateUserToken(token.Key)
	if err != nil {
		if errors.Is(err, model.ErrDatabase) {
			return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError, "user_token_db_error: "+err.Error())
		}
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgTokenInvalid,
			fmt.Sprintf("user_token_invalid: user=%d token=%d: %v", userId, token.Id, err))
	}
	if validated.Key == "" {
		validated.Key = token.Key
	}

	SetIdentity(c, claims.Subject, task)
	return validated, nil
}

func reject(c *gin.Context, status int, messageKey, logReason string) *RejectError {
	logger.LogWarn(c, fmt.Sprintf("openwebui: rejected (%d) %s | path=%s ip=%s", status, logReason, requestPath(c), c.ClientIP()))
	return &RejectError{Status: status, MessageKey: messageKey, LogReason: logReason}
}

func requestPath(c *gin.Context) string {
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}
