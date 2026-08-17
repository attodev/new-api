package controller

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// oauth2Configured reports whether the OAuth2 provider feature is not just
// enabled but actually usable. An admin can flip settings.Enabled to true
// without filling in client_id/client_secret/redirect_uri (they default to
// ""), which would otherwise let an empty client_secret from a caller match
// an empty configured one. From the caller's perspective, an unconfigured
// feature must look identical to a disabled one.
func oauth2Configured(settings *system_setting.OAuth2Settings) bool {
	return settings.Enabled && settings.ClientId != "" && settings.ClientSecret != "" && settings.RedirectURI != ""
}

// OAuth2SessionInit is called from new-api's own frontend (same-origin,
// authenticated via the normal session cookie through middleware.UserAuth()).
// It mints a short-lived authorization code and returns a ready-to-navigate
// URL pointing at the external app's callback. There is no browser-facing
// /oauth2/authorize redirect in this design — see the design spec's
// "Revision note" for why.
func OAuth2SessionInit(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2Configured(settings) {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	userId := c.GetInt("id")
	code, err := model.StoreAuthCode(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization code"})
		return
	}

	state, err := common.GenerateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
		return
	}

	redirectURL := settings.RedirectURI + "?code=" + code + "&state=" + state
	c.JSON(http.StatusOK, gin.H{"redirect_url": redirectURL})
}

// OAuth2Token exchanges a single-use authorization code for an access
// token. Called server-to-server by the external app's backend.
func OAuth2Token(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2Configured(settings) {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	if c.PostForm("grant_type") != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}

	clientId := c.PostForm("client_id")
	clientSecret := c.PostForm("client_secret")
	if clientId != settings.ClientId ||
		subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}

	code := c.PostForm("code")
	userId, err := model.ConsumeAuthCode(code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}

	userCache, err := model.GetUserCache(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	token, err := model.IssueOAuth2Token(userId, userCache.Group, clientId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": "sk-" + token.Key,
		"token_type":   "Bearer",
		"expires_in":   86400,
	})
}

// OAuth2UserInfo resolves an access token issued by OAuth2Token back to
// the underlying new-api user's identity. Called server-to-server.
func OAuth2UserInfo(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	bearer := c.Request.Header.Get("Authorization")
	key := strings.TrimPrefix(bearer, "Bearer ")
	key = strings.TrimPrefix(key, "sk-")

	token, err := model.ValidateUserToken(key)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	expectedName := "oauth2-" + settings.ClientId
	if token.Name != expectedName {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	userCache, err := model.GetUserCache(token.UserId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sub":      strconv.Itoa(token.UserId),
		"email":    userCache.Email,
		"name":     userCache.Username,
		"is_admin": model.IsAdmin(token.UserId),
	})
}
