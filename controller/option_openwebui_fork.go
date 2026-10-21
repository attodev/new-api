// fork: O13 — options/status API hooks for Open WebUI: hide and protect the OIDC signing keys, validate openwebui.* and oauth2.* saves, oauth2_* status fields (ported from OpenWebUI controller/option.go and misc.go).

package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// oidcHiddenOption reports whether GetOptions must skip key. The signing key
// names also end in "Key" (hidden by the generic suffix filter); this explicit
// check keeps them hidden even if that filter changes.
func oidcHiddenOption(key string) bool {
	return key == oidc.OptionSigningKeyCurrent || key == oidc.OptionSigningKeyPrevious
}

// rejectOIDCSigningKeyWrite refuses any options API write to a signing key and
// reports whether it wrote the error response. Keys change only via service/oidc.
func rejectOIDCSigningKeyWrite(c *gin.Context, key string) bool {
	if !oidcHiddenOption(key) {
		return false
	}
	common.ApiErrorT(c, "Invalid parameters")
	return true
}

// validateOpenWebUIOption validates and normalises openwebui.* and oauth2.*
// option saves; option.Value must already be a string. It returns false after
// writing the error response. Other keys pass through untouched.
func validateOpenWebUIOption(c *gin.Context, option *OptionUpdateRequest) bool {
	switch option.Key {
	case "openwebui.relay_enabled":
		if option.Value == "true" {
			s := system_setting.GetOpenWebUISettings()
			if s.ServiceApiKey == "" || len(s.JwtSecret) < 32 {
				common.ApiErrorT(c, "Set the service key and a JWT secret of at least 32 characters before enabling the Open WebUI relay")
				return false
			}
			// The service token may have been deleted or disabled since the
			// key was saved; never enable the relay on a dead key.
			if _, err := model.ValidateUserToken(s.ServiceApiKey); err != nil {
				common.ApiErrorT(c, "The Open WebUI service key does not match an enabled token")
				return false
			}
		}
	case "openwebui.service_api_key":
		normalized := system_setting.NormalizeOpenWebUIServiceKey(option.Value.(string))
		if normalized == "" {
			if system_setting.GetOpenWebUISettings().RelayEnabled {
				common.ApiErrorT(c, "Disable the Open WebUI relay before clearing the service key or JWT secret")
				return false
			}
		} else if _, err := model.ValidateUserToken(normalized); err != nil {
			common.ApiErrorT(c, "The Open WebUI service key does not match an enabled token")
			return false
		}
		option.Value = normalized
	case "openwebui.jwt_secret", "openwebui.jwt_previous_secret":
		v := option.Value.(string)
		if option.Key == "openwebui.jwt_secret" && strings.TrimSpace(v) == "" &&
			system_setting.GetOpenWebUISettings().RelayEnabled {
			common.ApiErrorT(c, "Disable the Open WebUI relay before clearing the service key or JWT secret")
			return false
		}
		if v != "" && len(v) < 32 {
			common.ApiErrorT(c, "The Open WebUI JWT secret must be at least 32 characters")
			return false
		}
	case "openwebui.jwt_leeway_seconds":
		leeway, err := strconv.Atoi(strings.TrimSpace(option.Value.(string)))
		if err != nil || leeway < 0 || leeway > 600 {
			common.ApiErrorT(c, "Invalid parameters")
			return false
		}
	case "openwebui.jwt_issuer":
		// An empty issuer would disable the iss check; keep the setting non-empty.
		if strings.TrimSpace(option.Value.(string)) == "" {
			common.ApiErrorT(c, "Invalid parameters")
			return false
		}
	case "oauth2.enabled":
		if option.Value == "true" {
			s := system_setting.GetOAuth2Settings()
			if s.ClientId == "" || len(s.ClientSecret) < 16 || !system_setting.IsAbsoluteHTTPURL(s.RedirectURI) {
				common.ApiErrorT(c, "Set a client ID, a client secret of at least 16 characters and an absolute redirect URI before enabling external app access")
				return false
			}
		}
	case "oauth2.client_id":
		option.Value = strings.TrimSpace(option.Value.(string))
	case "oauth2.client_secret":
		option.Value = strings.TrimSpace(option.Value.(string))
		if v := option.Value.(string); v != "" && len(v) < 16 {
			common.ApiErrorT(c, "The client secret must be at least 16 characters")
			return false
		}
	case "oauth2.redirect_uri", "oauth2.app_url", "oauth2.post_logout_redirect_uri":
		option.Value = strings.TrimSpace(option.Value.(string))
		if v := option.Value.(string); v != "" && !system_setting.IsAbsoluteHTTPURL(v) {
			common.ApiErrorT(c, "Must be an absolute http(s) URL")
			return false
		}
	}
	return true
}

// openWebUIStatusFields are the public GetStatus fields the frontend reads for
// the Open WebUI ("external app") entry points. Client credentials stay out.
func openWebUIStatusFields() map[string]any {
	s := system_setting.GetOAuth2Settings()
	return map[string]any{
		"oauth2_enabled":            s.Enabled,
		"oauth2_open_in_new_window": s.OpenInNewWindow,
		"oauth2_replace_playground": s.ReplacePlayground,
		"oauth2_app_url":            s.AppUrl,
	}
}

// addOpenWebUIStatusFields merges openWebUIStatusFields into the status map.
func addOpenWebUIStatusFields(data gin.H) {
	for k, v := range openWebUIStatusFields() {
		data[k] = v
	}
}
