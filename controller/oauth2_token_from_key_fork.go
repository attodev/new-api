// fork: O13 — POST /oauth2/token-from-key (API key -> short-lived userinfo-only OIDC access token for the key owner) and POST /oauth2/introspect (RFC 7662) (ported from OpenWebUI controller/oauth2_token_from_key.go; issuance is recorded in develop's audit_logs table instead of the usage log).

package controller

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const (
	tokenFromKeyScope = "openid email profile"
	// tokenFromKeyIPRate bounds issuance per client IP per minute, on top of
	// the configurable per-key limit, so key guessing from one address is slow.
	tokenFromKeyIPRate = 60
	tokenFromKeyWindow = 60 // seconds
	// tokenFromKeyAuditAction is the audit_logs action of an issued token.
	tokenFromKeyAuditAction = "oidc.token_from_key.issue"
)

// tokenFromKeyLimiter counts requests when Redis is off. It is a pointer so
// tests can swap in a fresh limiter without copying its mutex.
var tokenFromKeyLimiter = &common.InMemoryRateLimiter{}

// resetTokenFromKeyLimiterForTest discards the in-process counters.
func resetTokenFromKeyLimiterForTest() {
	tokenFromKeyLimiter = &common.InMemoryRateLimiter{}
}

// tokenFromKeyError answers in the format the Open WebUI MCP side expects
// ({error, message}); error_description carries the same text for RFC 6749
// style clients.
func tokenFromKeyError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, gin.H{"error": code, "message": message, "error_description": message})
}

// allowTokenFromKey counts one request against mark within a fixed one-minute
// window (Redis when enabled, otherwise this process).
func allowTokenFromKey(mark string, max int) bool {
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		key := "rateLimit:OIDCK:" + mark
		n, err := common.RDB.Incr(ctx, key).Result()
		if err != nil {
			// Fail open: the limit is a soft guard, the key itself is the credential.
			common.SysLog("oidc: token-from-key rate limit: " + err.Error())
			return true
		}
		if n == 1 {
			common.RDB.Expire(ctx, key, tokenFromKeyWindow*time.Second)
		}
		return n <= int64(max)
	}
	limiter := tokenFromKeyLimiter
	limiter.Init(common.RateLimitKeyExpirationDuration)
	return limiter.Request("OIDCK:"+mark, max, tokenFromKeyWindow)
}

// bearerAPIKey extracts the bare key from "Bearer sk-<key>[-<channel>]",
// following TokenAuth's parsing. Anything but a Bearer header yields "".
func bearerAPIKey(c *gin.Context) string {
	key := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(key) <= 7 || !strings.EqualFold(key[:7], "Bearer ") {
		return ""
	}
	key = strings.TrimSpace(key[7:])
	key = strings.TrimPrefix(key, "sk-")
	return strings.Split(key, "-")[0]
}

// recordTokenFromKeyAudit writes the issuance to audit_logs as a security
// event of the key owner. It carries ids only: never the key or the token.
func recordTokenFromKeyAudit(c *gin.Context, user *model.User, tokenId int, clientId string, ttl int) {
	params := map[string]any{
		"user_id":     user.Id,
		"token_id":    tokenId,
		"client_id":   clientId,
		"ttl_seconds": ttl,
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId:     user.Id,
		Username:   user.Username,
		ActorRole:  user.Role,
		Category:   model.AuditCategorySecurity,
		Action:     tokenFromKeyAuditAction,
		AuthMethod: "api_key",
		Status:     http.StatusOK,
		Success:    true,
		Content:    fmt.Sprintf("OIDC access token issued from API key (token ID: %d, client ID: %s, lifetime: %ds)", tokenId, clientId, ttl),
		Other:      model.AuditOther{Op: &model.AuditOperation{Action: tokenFromKeyAuditAction, Params: params}},
	})
}

// OAuth2TokenFromKey trades a new-api API key for a short-lived opaque access
// token that only userinfo and introspection accept. The token carries the
// key owner's identity exactly as a web OIDC login would, so Open WebUI's
// OAuth token exchange can map it to the existing account.
func OAuth2TokenFromKey(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2ProviderConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	if !settings.TokenFromKeyEnabled {
		tokenFromKeyError(c, http.StatusForbidden, "feature_disabled", "issuing tokens from API keys is disabled")
		return
	}
	ip := c.ClientIP()
	if !allowTokenFromKey("ip:"+ip, tokenFromKeyIPRate) {
		logger.LogWarn(c, "oidc: token-from-key rejected (rate_limited) ip=%s", ip)
		tokenFromKeyError(c, http.StatusTooManyRequests, "rate_limited", "too many requests")
		return
	}
	token, err := model.ValidateTokenForIdentity(bearerAPIKey(c))
	if err != nil {
		if errors.Is(err, model.ErrDatabase) {
			logger.LogError(c, "oidc: token-from-key: "+err.Error())
			tokenFromKeyError(c, http.StatusInternalServerError, "server_error", "temporary failure")
			return
		}
		logger.LogWarn(c, "oidc: token-from-key rejected (invalid_key) ip=%s", ip)
		tokenFromKeyError(c, http.StatusUnauthorized, "invalid_key", "the API key is missing, unknown, disabled or expired")
		return
	}
	if allowIps := token.GetIpLimits(); len(allowIps) > 0 {
		parsed := net.ParseIP(ip)
		if parsed == nil || !common.IsIpInCIDRList(parsed, allowIps) {
			logger.LogWarn(c, "oidc: token-from-key rejected (ip_not_allowed) token_id=%d ip=%s", token.Id, ip)
			tokenFromKeyError(c, http.StatusForbidden, "ip_not_allowed", "your IP is not in the API key's allowed list")
			return
		}
	}
	if !allowTokenFromKey("key:"+strconv.Itoa(token.Id), settings.EffectiveTokenFromKeyRate()) {
		logger.LogWarn(c, "oidc: token-from-key rejected (rate_limited) token_id=%d ip=%s", token.Id, ip)
		tokenFromKeyError(c, http.StatusTooManyRequests, "rate_limited", "too many requests for this API key")
		return
	}
	user, err := model.GetUserById(token.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		logger.LogWarn(c, "oidc: token-from-key rejected (user_disabled) token_id=%d ip=%s", token.Id, ip)
		tokenFromKeyError(c, http.StatusForbidden, "user_disabled", "the API key owner's account is disabled")
		return
	}
	if strings.TrimSpace(user.Email) == "" {
		logger.LogWarn(c, "oidc: token-from-key rejected (email_required) token_id=%d ip=%s", token.Id, ip)
		tokenFromKeyError(c, http.StatusForbidden, "email_required", "the API key owner has no email address")
		return
	}
	clientId := settings.EffectiveTokenFromKeyClientId()
	ttl := settings.EffectiveTokenFromKeyTTL()
	access, err := oidc.IssueAccessTokenWithTTL(oidc.AccessToken{
		UserId: user.Id, ClientId: clientId, Scope: tokenFromKeyScope, TokenId: token.Id,
	}, time.Duration(ttl)*time.Second)
	if err != nil {
		logger.LogError(c, "oidc: token-from-key issue: "+err.Error())
		tokenFromKeyError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	recordTokenFromKeyAudit(c, user, token.Id, clientId, ttl)
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, gin.H{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   ttl,
	})
}

// OAuth2Introspect implements RFC 7662 for the opaque access tokens. Only the
// registered Open WebUI client may call it, and it reports tokens issued to
// any client_id (web login and token-from-key alike).
func OAuth2Introspect(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2ProviderConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	clientId, clientSecret := clientCredentials(c)
	if clientId != settings.ClientId || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		logger.LogWarn(c, "oidc: introspect rejected (invalid_client) client_id=%q ip=%s", clientId, c.ClientIP())
		tokenError(c, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	raw := c.PostForm("token")
	at, found, err := oidc.LookupAccessToken(raw)
	if err != nil {
		logger.LogError(c, "oidc: introspect lookup: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	if !found {
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}
	user, err := model.GetUserById(at.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		oidc.RevokeAccessToken(raw)
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}
	res := gin.H{
		"active":     true,
		"client_id":  at.ClientId,
		"scope":      at.Scope,
		"sub":        strconv.Itoa(user.Id),
		"username":   user.Username,
		"token_type": "Bearer",
		"iss":        oidc.Issuer(),
	}
	if at.ExpiresAt != 0 {
		res["exp"] = at.ExpiresAt
		res["iat"] = at.IssuedAt
	}
	c.JSON(http.StatusOK, res)
}
