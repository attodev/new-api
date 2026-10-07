package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryDocument(t *testing.T) {
	withIssuer(t, "https://newapi.example.com/")
	d := DiscoveryDocument()
	require.Equal(t, "https://newapi.example.com", d["issuer"])
	require.Equal(t, "https://newapi.example.com/oauth2/authorize", d["authorization_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/token", d["token_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/userinfo", d["userinfo_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/jwks", d["jwks_uri"])
	require.Equal(t, "https://newapi.example.com/oauth2/logout", d["end_session_endpoint"])
	require.Equal(t, []string{"code"}, d["response_types_supported"])
	require.Equal(t, []string{"public"}, d["subject_types_supported"])
	require.Equal(t, []string{"RS256"}, d["id_token_signing_alg_values_supported"])
	require.Equal(t, []string{"S256"}, d["code_challenge_methods_supported"])
	require.Equal(t, []string{"client_secret_basic", "client_secret_post"}, d["token_endpoint_auth_methods_supported"])
	require.Equal(t, []string{"authorization_code"}, d["grant_types_supported"])
	require.Equal(t, []string{"openid", "email", "profile"}, d["scopes_supported"])
	require.Equal(t, []string{"sub", "email", "email_verified", "name", "preferred_username", "roles", "groups"}, d["claims_supported"])
}
