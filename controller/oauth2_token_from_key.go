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
)

var tokenFromKeyLimiter common.InMemoryRateLimiter

func tokenFromKeyError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": code, "message": message})
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
			// fail open: the per-key check is a soft guard, the key itself is the credential
			common.SysLog("oidc: token-from-key rate limit: " + err.Error())
			return true
		}
		if n == 1 {
			common.RDB.Expire(ctx, key, tokenFromKeyWindow*time.Second)
		}
		return n <= int64(max)
	}
	tokenFromKeyLimiter.Init(common.RateLimitKeyExpirationDuration)
	return tokenFromKeyLimiter.Request("OIDCK:"+mark, max, tokenFromKeyWindow)
}

// bearerAPIKey extracts the bare key from "Bearer sk-<key>[-<channel>]",
// following TokenAuth's parsing.
func bearerAPIKey(c *gin.Context) string {
	key := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(key) > 7 && strings.EqualFold(key[:7], "Bearer ") {
		key = strings.TrimSpace(key[7:])
	} else {
		return ""
	}
	key = strings.TrimPrefix(key, "sk-")
	return strings.Split(key, "-")[0]
}

// OAuth2TokenFromKey trades a new-api API key for a short-lived opaque access
// token that only userinfo and introspection accept. The token carries the
// key owner's identity exactly as a web OIDC login would, so Open WebUI's
// OAuth token exchange can map it to the existing account.
func OAuth2TokenFromKey(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2CredentialsConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	if !settings.TokenFromKeyEnabled {
		tokenFromKeyError(c, http.StatusForbidden, "feature_disabled", "issuing tokens from API keys is disabled")
		return
	}
	ip := c.ClientIP()
	if !allowTokenFromKey("ip:"+ip, tokenFromKeyIPRate) {
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
		logger.LogWarn(c, "oidc: token-from-key rejected (invalid_key) ip="+ip)
		tokenFromKeyError(c, http.StatusUnauthorized, "invalid_key", "the API key is missing, unknown, disabled or expired")
		return
	}
	if allowIps := token.GetIpLimits(); len(allowIps) > 0 {
		parsed := net.ParseIP(ip)
		if parsed == nil || !common.IsIpInCIDRList(parsed, allowIps) {
			logger.LogWarn(c, fmt.Sprintf("oidc: token-from-key rejected (ip_not_allowed) token_id=%d ip=%s", token.Id, ip))
			tokenFromKeyError(c, http.StatusForbidden, "ip_not_allowed", "your IP is not in the API key's allowed list")
			return
		}
	}
	rate := settings.TokenFromKeyRateLimit
	if rate <= 0 {
		rate = system_setting.DefaultTokenFromKeyRate
	}
	if !allowTokenFromKey("key:"+strconv.Itoa(token.Id), rate) {
		tokenFromKeyError(c, http.StatusTooManyRequests, "rate_limited", "too many requests for this API key")
		return
	}
	user, err := model.GetUserById(token.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		logger.LogWarn(c, fmt.Sprintf("oidc: token-from-key rejected (user_disabled) token_id=%d ip=%s", token.Id, ip))
		tokenFromKeyError(c, http.StatusForbidden, "user_disabled", "the API key owner's account is disabled")
		return
	}
	if strings.TrimSpace(user.Email) == "" {
		tokenFromKeyError(c, http.StatusForbidden, "email_required", "the API key owner has no email address")
		return
	}
	clientId := settings.TokenFromKeyClientId
	if clientId == "" {
		clientId = system_setting.DefaultTokenFromKeyClientId
	}
	ttl := settings.TokenFromKeyTTLSeconds
	if ttl < system_setting.MinTokenFromKeyTTL || ttl > system_setting.MaxTokenFromKeyTTL {
		ttl = system_setting.DefaultTokenFromKeyTTL
	}
	access, err := oidc.IssueAccessTokenWithTTL(oidc.AccessToken{
		UserId: user.Id, ClientId: clientId, Scope: tokenFromKeyScope, TokenId: token.Id,
	}, time.Duration(ttl)*time.Second)
	if err != nil {
		logger.LogError(c, "oidc: token-from-key issue: "+err.Error())
		tokenFromKeyError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	model.RecordLog(user.Id, model.LogTypeSystem,
		fmt.Sprintf("OIDC access token issued from API key: token_id=%d client_id=%s ip=%s", token.Id, clientId, ip))
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
	if !oauth2CredentialsConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	clientId, clientSecret := clientCredentials(c)
	if clientId != settings.ClientId || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		logger.LogWarn(c, fmt.Sprintf("oidc: introspect rejected (invalid_client) client_id=%q ip=%s", clientId, c.ClientIP()))
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
