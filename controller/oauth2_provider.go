package controller

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// oauth2CredentialsConfigured reports whether the provider is on and has a
// usable client registration. From a caller's perspective an unconfigured
// provider must look identical to a disabled one.
func oauth2CredentialsConfigured(settings *system_setting.OAuth2Settings) bool {
	return settings.Enabled && settings.ClientId != "" && settings.ClientSecret != ""
}

func oauth2NotEnabled(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
}

// OAuth2Discovery serves /.well-known/openid-configuration.
func OAuth2Discovery(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	c.JSON(http.StatusOK, oidc.DiscoveryDocument())
}

// OAuth2JWKS serves the signing public keys.
func OAuth2JWKS(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	jwks, err := oidc.JWKS()
	if err != nil {
		logger.LogError(c, "oidc: jwks: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, jwks)
}

type oauth2AuthorizeRequest struct {
	ResponseType        string `json:"response_type"`
	ClientId            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

func appendQuery(base string, values url.Values) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + values.Encode()
}

// OAuth2Authorize is the same-origin API the SPA page /oauth2/authorize calls.
// It runs behind UserAuth, so an unauthenticated browser gets 401 and the page
// sends it to /sign-in. No consent screen: the only client is first-party.
func OAuth2Authorize(c *gin.Context) {
	var req oauth2AuthorizeRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "invalid_request", "message": common.TranslateMessage(c, i18n.MsgOIDCInvalidRequest)})
		return
	}
	settings := system_setting.GetOAuth2Settings()
	ip := c.ClientIP()

	hardReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected ("+code+") client_id="+req.ClientId+" ip="+ip)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": code, "message": common.TranslateMessage(c, i18n.MsgOIDCInvalidRequest)})
	}
	redirectReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected ("+code+") client_id="+req.ClientId+" ip="+ip)
		q := url.Values{"error": {code}}
		if req.State != "" {
			q.Set("state", req.State)
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "error": code, "data": gin.H{"redirect_url": appendQuery(req.RedirectURI, q)}})
	}

	if !oauth2CredentialsConfigured(settings) || req.ClientId != settings.ClientId {
		hardReject("invalid_client")
		return
	}
	if req.RedirectURI == "" || req.RedirectURI != settings.RedirectURI {
		hardReject("invalid_redirect_uri")
		return
	}
	if req.ResponseType != "code" {
		redirectReject("unsupported_response_type")
		return
	}
	if !common.StringsContains(strings.Fields(req.Scope), "openid") {
		redirectReject("invalid_scope")
		return
	}
	if req.CodeChallenge != "" && req.CodeChallengeMethod != "S256" {
		redirectReject("invalid_request")
		return
	}

	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			redirectReject("access_denied")
			return
		}
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		redirectReject("access_denied")
		return
	}
	if strings.TrimSpace(user.Email) == "" {
		logger.LogWarn(c, "oidc: authorize rejected (email_required) user="+user.Username)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "email_required", "message": common.TranslateMessage(c, i18n.MsgOIDCEmailRequired)})
		return
	}

	code, err := oidc.IssueCode(oidc.AuthCode{
		UserId: user.Id, ClientId: req.ClientId, RedirectURI: req.RedirectURI,
		Nonce: req.Nonce, Scope: req.Scope,
		CodeChallenge: req.CodeChallenge, CodeChallengeMethod: req.CodeChallengeMethod,
		AuthTime: common.GetTimestamp(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	q := url.Values{"code": {code}}
	if req.State != "" {
		q.Set("state", req.State)
	}
	common.ApiSuccess(c, gin.H{"redirect_url": appendQuery(req.RedirectURI, q)})
}

func tokenError(c *gin.Context, status int, code, description string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Basic realm="oauth2"`)
	}
	c.JSON(status, gin.H{"error": code, "error_description": description})
}

// clientCredentials reads client_secret_basic first, then client_secret_post.
func clientCredentials(c *gin.Context) (string, string) {
	if id, secret, ok := c.Request.BasicAuth(); ok {
		return id, secret
	}
	return c.PostForm("client_id"), c.PostForm("client_secret")
}

// OAuth2Token exchanges an authorization code for an access token and id_token.
// Called server-to-server by Open WebUI.
func OAuth2Token(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2CredentialsConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	clientId, clientSecret := clientCredentials(c)
	if clientId != settings.ClientId || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		logger.LogWarn(c, "oidc: token rejected (invalid_client) client_id="+clientId+" ip="+c.ClientIP())
		tokenError(c, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	if c.PostForm("grant_type") != "authorization_code" {
		tokenError(c, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	ac, err := oidc.ConsumeCode(c.PostForm("code"), clientId, c.PostForm("redirect_uri"), c.PostForm("code_verifier"))
	if err != nil {
		if errors.Is(err, oidc.ErrInvalidGrant) {
			logger.LogWarn(c, "oidc: token rejected (invalid_grant) client_id="+clientId+" ip="+c.ClientIP())
			tokenError(c, http.StatusBadRequest, "invalid_grant", "authorization code is invalid, expired or already used")
			return
		}
		logger.LogError(c, "oidc: token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	user, err := model.GetUserById(ac.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "user is not available")
		return
	}
	claims := oidc.BuildClaims(user, oidc.LookupOrganizationName(user.OrganizationId))
	idToken, err := oidc.SignIDToken(claims, clientId, ac.Nonce, ac.AuthTime, time.Now())
	if err != nil {
		logger.LogError(c, "oidc: sign id_token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	access, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: user.Id, ClientId: clientId, Scope: ac.Scope})
	if err != nil {
		logger.LogError(c, "oidc: issue access token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, gin.H{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(oidc.AccessTokenTTL.Seconds()),
		"id_token":     idToken,
		"scope":        ac.Scope,
	})
}

func userinfoUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
}

// OAuth2UserInfo resolves an access token to the user's claims.
func OAuth2UserInfo(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	bearer := strings.TrimSpace(c.GetHeader("Authorization"))
	raw := ""
	if len(bearer) > 7 && strings.EqualFold(bearer[:7], "Bearer ") {
		raw = strings.TrimSpace(bearer[7:])
	}
	at, found, err := oidc.LookupAccessToken(raw)
	if err != nil {
		logger.LogError(c, "oidc: userinfo lookup: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found {
		userinfoUnauthorized(c)
		return
	}
	user, err := model.GetUserById(at.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		oidc.RevokeAccessToken(raw)
		userinfoUnauthorized(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, oidc.BuildClaims(user, oidc.LookupOrganizationName(user.OrganizationId)).UserInfo())
}

type oauth2LogoutRequest struct {
	IDTokenHint           string `json:"id_token_hint"`
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
	State                 string `json:"state"`
}

// OAuth2Logout is the same-origin API behind the SPA page /oauth2/logout
// (the OIDC end_session_endpoint). A verifiable id_token_hint, or no hint at
// all, ends the new-api session; a hint that fails verification is ignored
// so this URL cannot be used to log strangers out.
func OAuth2Logout(c *gin.Context) {
	var req oauth2LogoutRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10) // unauthenticated endpoint
	_ = common.DecodeJson(c.Request.Body, &req)                            // an empty body is a plain logout
	settings := system_setting.GetOAuth2Settings()

	if strings.TrimSpace(req.IDTokenHint) != "" {
		if _, err := oidc.VerifyIDTokenHint(req.IDTokenHint, settings.ClientId); err != nil {
			logger.LogWarn(c, "oidc: end-session ignored: id_token_hint failed verification ip="+c.ClientIP())
			common.ApiSuccess(c, gin.H{"redirect_url": "/"})
			return
		}
	}

	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}

	redirectURL := "/sign-in"
	if settings.PostLogoutRedirectURI != "" && req.PostLogoutRedirectURI == settings.PostLogoutRedirectURI {
		redirectURL = settings.PostLogoutRedirectURI
		if req.State != "" {
			redirectURL = appendQuery(redirectURL, url.Values{"state": {req.State}})
		}
	}
	common.ApiSuccess(c, gin.H{"redirect_url": redirectURL})
}

func signingKeyStatusPayload() (gin.H, error) {
	keys, err := oidc.VerificationKeys()
	if err != nil {
		return nil, err
	}
	payload := gin.H{
		"current":  gin.H{"kid": keys[0].Kid, "created_at": keys[0].CreatedAt},
		"previous": nil,
	}
	if len(keys) > 1 {
		payload["previous"] = gin.H{"kid": keys[1].Kid, "created_at": keys[1].CreatedAt}
	}
	return payload, nil
}

// OAuth2SigningKeyStatus shows the admin which keys are published. Root only.
func OAuth2SigningKeyStatus(c *gin.Context) {
	payload, err := signingKeyStatusPayload()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, payload)
}

// OAuth2RotateSigningKey installs a new signing key; the old one stays in the
// JWKS for oidc.PreviousKeyRetention. Root only.
func OAuth2RotateSigningKey(c *gin.Context) {
	if _, err := oidc.RotateKey(); err != nil {
		common.ApiError(c, err)
		return
	}
	logger.LogInfo(c, "oidc: signing key rotated by user "+strconv.Itoa(c.GetInt("id")))
	payload, err := signingKeyStatusPayload()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, payload)
}
