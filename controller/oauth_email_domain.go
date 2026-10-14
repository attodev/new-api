package controller

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
)

// fork: O8 — the email domain whitelist also applies to OIDC sign-in, for new
// and existing accounts alike (team a8d74c438). Other providers are unchanged.
func enforceOIDCEmailDomain(provider oauth.Provider, user *oauth.OAuthUser) error {
	if _, isOIDC := provider.(*oauth.OIDCProvider); !isOIDC {
		return nil
	}
	if !common.EmailDomainRestrictionEnabled {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(user.Email), "@")
	if len(parts) != 2 || parts[1] == "" {
		return errors.New("Please enter a valid email address")
	}
	domain := strings.ToLower(parts[1])
	allowed := slices.ContainsFunc(common.EmailDomainWhitelist, func(entry string) bool {
		return strings.EqualFold(strings.TrimSpace(entry), domain)
	})
	if !allowed {
		return errors.New("This email address is not allowed by the administrator's email policy.")
	}
	return nil
}
