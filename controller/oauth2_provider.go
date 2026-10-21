// fork: O13 — built-in OIDC provider HTTP endpoints: discovery, JWKS, token, userinfo, the same-origin authorize and end-session APIs behind the SPA pages /oauth2/authorize and /oauth2/logout, and signing-key status/rotation (ported from OpenWebUI controller/oauth2_provider.go; end-session rewritten for develop's login sessions).

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
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Page messages of the authorize API. The SPA translates them (message_key).
const (
	oidcInvalidRequestMessage = "The sign-in request is not valid, please contact your administrator"
	oidcEmailRequiredMessage  = "Register an email address in your profile before signing in to the external app"
)

// oidcEndSessionReason is the revoked_reason of a login session ended by
// the OIDC end-session API.
const oidcEndSessionReason = "oidc_end_session"

// oauth2ProviderConfigured reports whether the provider is on and has a
// usable client registration. A blank redirect_uri counts as unconfigured
// (the option validator lets an admin clear it while the provider is on).
// From a caller's perspective an unconfigured provider must look identical
// to a disabled one.
func oauth2ProviderConfigured(settings *system_setting.OAuth2Settings) bool {
	return settings.Enabled && settings.ClientId != "" && settings.ClientSecret != "" &&
		strings.TrimSpace(settings.RedirectURI) != ""
}

func oauth2NotEnabled(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
}

// OAuth2Discovery serves /.well-known/openid-configuration.
func OAuth2Discovery(c *gin.Context) {
	if !oauth2ProviderConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	c.JSON(http.StatusOK, oidc.DiscoveryDocument())
}

// OAuth2JWKS serves the signing public keys.
func OAuth2JWKS(c *gin.Context) {
	if !oauth2ProviderConfigured(system_setting.GetOAuth2Settings()) {
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

// oidcPageError answers the SPA with a non-redirectable error: the stable
// OIDC error code plus a translatable page message.
func oidcPageError(c *gin.Context, code, message string) {
	common.ApiErrorStatus(c, http.StatusOK, common.NewMessage(message), gin.H{"error": code})
}

// pkceAcceptable allows no PKCE at all, or S256 with a challenge. "plain"
// (also implied by a challenge without a method) is refused before a code
// exists; the code store refuses it again at exchange.
func pkceAcceptable(challenge, method string) bool {
	if challenge == "" && method == "" {
		return true
	}
	return challenge != "" && method == "S256"
}

// OAuth2Authorize is the same-origin API the SPA page /oauth2/authorize calls.
// It runs behind UserAuth (dashboard login session only), so a browser that
// is not signed in gets 401 and the page sends it to /sign-in. No consent
// screen: the only client is first-party.
func OAuth2Authorize(c *gin.Context) {
	var req oauth2AuthorizeRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		oidcPageError(c, "invalid_request", oidcInvalidRequestMessage)
		return
	}
	settings := system_setting.GetOAuth2Settings()
	ip := c.ClientIP()

	hardReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected (%s) client_id=%q ip=%s", code, req.ClientId, ip)
		oidcPageError(c, code, oidcInvalidRequestMessage)
	}
	redirectReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected (%s) client_id=%q ip=%s", code, req.ClientId, ip)
		q := url.Values{"error": {code}}
		if req.State != "" {
			q.Set("state", req.State)
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "error": code, "data": gin.H{"redirect_url": appendQuery(req.RedirectURI, q)}})
	}

	if !oauth2ProviderConfigured(settings) || req.ClientId != settings.ClientId {
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
	if !pkceAcceptable(req.CodeChallenge, req.CodeChallengeMethod) {
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
		logger.LogWarn(c, "oidc: authorize rejected (email_required) user=%q", user.Username)
		oidcPageError(c, "email_required", oidcEmailRequiredMessage)
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
// RFC 6749 §2.3.1: basic credentials are form-urlencoded before base64, so
// both halves are decoded; a value that is not valid encoding is used as-is.
func clientCredentials(c *gin.Context) (string, string) {
	if id, secret, ok := c.Request.BasicAuth(); ok {
		return formDecodeOrRaw(id), formDecodeOrRaw(secret)
	}
	return c.PostForm("client_id"), c.PostForm("client_secret")
}

func formDecodeOrRaw(v string) string {
	if decoded, err := url.QueryUnescape(v); err == nil {
		return decoded
	}
	return v
}

// OAuth2Token exchanges an authorization code for an access token and id_token.
// Called server-to-server by Open WebUI.
func OAuth2Token(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2ProviderConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	clientId, clientSecret := clientCredentials(c)
	if clientId != settings.ClientId || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		logger.LogWarn(c, "oidc: token rejected (invalid_client) client_id=%q ip=%s", clientId, c.ClientIP())
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
			logger.LogWarn(c, "oidc: token rejected (invalid_grant) client_id=%q ip=%s", clientId, c.ClientIP())
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
	if !oauth2ProviderConfigured(system_setting.GetOAuth2Settings()) {
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
	// Confirmed is set by the SPA page after the user explicitly agreed to
	// sign out of new-api (RP-Initiated Logout §2: without a valid hint the
	// OP must ask the End-User before logging them out).
	Confirmed bool `json:"confirmed"`
}

// endSession is the develop login session an end-session request would end.
// The SPA sends its in-memory access token as Bearer; the refresh cookie is
// scoped to /api/user/auth, so browsers do not send it here, but it is used
// when present (same rules as AuthLogout).
type endSession struct {
	userID     int    // session user; 0 when the request carries none
	bearerSID  string // session of a verified Bearer access token
	refresh    string // raw refresh cookie, when sent
	refreshSID string
}

func readEndSession(c *gin.Context) endSession {
	var s endSession
	if raw, ok := dashboardBearer(c.GetHeader("Authorization")); ok {
		if identity, err := service.ParseAccessToken(raw); err == nil {
			s.userID, s.bearerSID = identity.UserID, identity.SessionID
		}
	}
	if raw, err := c.Cookie(service.RefreshCookieName); err == nil && raw != "" {
		if sid, ok := service.RefreshTokenSID(raw); ok {
			s.refresh, s.refreshSID = raw, sid
		}
	}
	if s.bearerSID == "" && s.refreshSID != "" {
		if session, err := model.GetUserSessionCached(s.refreshSID); err == nil {
			s.userID = session.UserID
		}
	}
	return s
}

// end revokes the session the way AuthLogout does. A Bearer session is
// revoked by id; the refresh cookie is revoked (secret-checked) and deleted
// when it belongs to that session, kept when it belongs to another one, and
// deleted when the browser did not send it, so this browser is signed out.
// Without a Bearer token only a sent refresh cookie identifies the session.
func (s endSession) end(c *gin.Context) error {
	if s.bearerSID != "" {
		if _, err := model.RevokeUserSession(s.userID, s.bearerSID, oidcEndSessionReason); err != nil {
			return err
		}
		switch {
		case s.refresh == "":
			service.ClearRefreshCookie(c)
		case s.refreshSID == s.bearerSID:
			if err := service.RevokeByRefreshToken(s.refresh, s.bearerSID, oidcEndSessionReason); err != nil {
				return err
			}
			service.ClearRefreshCookie(c)
		}
		return nil
	}
	if s.refresh != "" {
		if err := service.RevokeByRefreshToken(s.refresh, "", oidcEndSessionReason); err != nil {
			return err
		}
		service.ClearRefreshCookie(c)
	}
	return nil
}

// OAuth2Logout is the same-origin API behind the SPA page /oauth2/logout
// (the OIDC end_session_endpoint). It ends the current login session only
// when the request carries an id_token_hint that verifies against our keys
// AND names the user of that session, or when the user confirmed on the page
// (confirmed=true). Otherwise (no hint, a hint that fails verification, a
// hint for another user or no session user, or the provider is off) it
// answers needs_confirmation and changes nothing, so a link to this URL,
// even one carrying someone's real id_token, cannot sign anyone out without
// a click (RP-Initiated Logout §2). A provider that is off never touches the
// signing keys. After a logout the browser goes to the registered
// post_logout_redirect_uri (exact match, with state) or else /sign-in.
func OAuth2Logout(c *gin.Context) {
	setAuthNoStore(c)
	var req oauth2LogoutRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10) // no auth middleware in front
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		req = oauth2LogoutRequest{} // an empty or unreadable body is an unconfirmed logout
	}
	settings := system_setting.GetOAuth2Settings()
	session := readEndSession(c)

	hintValid := false
	if oauth2ProviderConfigured(settings) && strings.TrimSpace(req.IDTokenHint) != "" {
		claims, err := oidc.VerifyIDTokenHint(req.IDTokenHint, settings.ClientId)
		switch {
		case err != nil:
			logger.LogWarn(c, "oidc: end-session: id_token_hint failed verification ip=%s", c.ClientIP())
		case session.userID <= 0 || claims.Subject != strconv.Itoa(session.userID):
			logger.LogWarn(c, "oidc: end-session: id_token_hint is not for the session user ip=%s", c.ClientIP())
		default:
			hintValid = true
		}
	}
	if !hintValid && !req.Confirmed {
		common.ApiSuccess(c, gin.H{"redirect_url": nil, "needs_confirmation": true})
		return
	}

	if err := session.end(c); err != nil {
		writeAuthSessionError(c, err)
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
