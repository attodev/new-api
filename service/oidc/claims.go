package oidc

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Claims are the user attributes exposed to the OIDC client. Open WebUI reads
// them under its default OAUTH_*_CLAIM names, so no client-side mapping is needed.
type Claims struct {
	Sub               string
	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
	Roles             []string
	Groups            []string
}

// BuildClaims maps a new-api user to OIDC claims. orgName is the user's
// organization name ("" when none); callers resolve it with LookupOrganizationName.
func BuildClaims(user *model.User, orgName string) Claims {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	role := "user"
	if user.Role >= common.RoleAdminUser {
		role = "admin"
	}
	groups := []string{user.Group}
	if orgName != "" {
		groups = append(groups, orgName)
	}
	return Claims{
		Sub:               strconv.Itoa(user.Id),
		Email:             strings.ToLower(strings.TrimSpace(user.Email)),
		EmailVerified:     true,
		Name:              name,
		PreferredUsername: user.Username,
		Roles:             []string{role},
		Groups:            groups,
	}
}

// LookupOrganizationName returns the organization's name or "" when the user
// has none or it cannot be found.
func LookupOrganizationName(orgId int) string {
	if orgId <= 0 {
		return ""
	}
	var org model.Organization
	if err := model.DB.Select("name").Where("id = ?", orgId).First(&org).Error; err != nil {
		return ""
	}
	return org.Name
}

// UserInfo renders the full claim set for the userinfo endpoint.
func (c Claims) UserInfo() map[string]any {
	return map[string]any{
		"sub":                c.Sub,
		"email":              c.Email,
		"email_verified":     c.EmailVerified,
		"name":               c.Name,
		"preferred_username": c.PreferredUsername,
		"roles":              c.Roles,
		"groups":             c.Groups,
	}
}
