package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/stretchr/testify/require"
)

func TestEnforceOIDCEmailDomain(t *testing.T) {
	oldEnabled, oldList := common.EmailDomainRestrictionEnabled, common.EmailDomainWhitelist
	common.EmailDomainRestrictionEnabled = true
	common.EmailDomainWhitelist = []string{"example.com"}
	t.Cleanup(func() { common.EmailDomainRestrictionEnabled, common.EmailDomainWhitelist = oldEnabled, oldList })

	oidc := &oauth.OIDCProvider{}
	require.NoError(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "ok@Example.com"}))
	require.Error(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "no@other.com"}))
	require.Error(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "malformed"}))
	// Only OIDC is restricted; other providers keep upstream behaviour.
	require.NoError(t, enforceOIDCEmailDomain(&oauth.GitHubProvider{}, &oauth.OAuthUser{Email: "no@other.com"}))

	common.EmailDomainRestrictionEnabled = false
	require.NoError(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "no@other.com"}))
}
