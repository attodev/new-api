package oidc

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestBuildClaims_UserAndAdminGroupsAndNames(t *testing.T) {
	user := &model.User{Id: 7, Username: "alice", DisplayName: "Alice Kim", Email: "Alice@Example.com", Role: common.RoleCommonUser, Group: "default"}
	c := BuildClaims(user, "")
	require.Equal(t, "7", c.Sub)
	require.Equal(t, "alice@example.com", c.Email)
	require.True(t, c.EmailVerified)
	require.Equal(t, "Alice Kim", c.Name)
	require.Equal(t, "alice", c.PreferredUsername)
	require.Equal(t, []string{"user"}, c.Roles)
	require.Equal(t, []string{"default"}, c.Groups)

	admin := &model.User{Id: 8, Username: "bob", Role: common.RoleAdminUser, Group: "vip", Email: "bob@example.com", OrganizationId: 3}
	c = BuildClaims(admin, "Acme")
	require.Equal(t, "bob", c.Name, "display name falls back to username")
	require.Equal(t, []string{"admin"}, c.Roles)
	require.Equal(t, []string{"vip", "Acme"}, c.Groups)

	root := &model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Group: "default", Email: "root@example.com"}
	require.Equal(t, []string{"admin"}, BuildClaims(root, "").Roles)
}

func TestBuildClaims_SkipsEmptyGroup(t *testing.T) {
	user := &model.User{Id: 7, Username: "alice", Email: "a@example.com", Role: common.RoleCommonUser, Group: ""}
	require.Equal(t, []string{}, BuildClaims(user, "").Groups)
	require.Equal(t, []string{"Acme"}, BuildClaims(user, "Acme").Groups)
	require.NotNil(t, BuildClaims(user, "").UserInfo()["groups"], "groups marshals as [] not null")
}

func TestClaims_UserInfoMap(t *testing.T) {
	c := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Role: common.RoleCommonUser, Group: "default"}, "")
	m := c.UserInfo()
	require.Equal(t, "7", m["sub"])
	require.Equal(t, "a@example.com", m["email"])
	require.Equal(t, true, m["email_verified"])
	require.Equal(t, "alice", m["name"])
	require.Equal(t, "alice", m["preferred_username"])
	require.Equal(t, []string{"user"}, m["roles"])
	require.Equal(t, []string{"default"}, m["groups"])
}

func TestLookupOrganizationName(t *testing.T) {
	setupOIDCTestDB(t)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 3, Name: "Acme", OwnerUserId: 1}).Error)
	require.Equal(t, "Acme", LookupOrganizationName(3))
	require.Equal(t, "", LookupOrganizationName(0))
	require.Equal(t, "", LookupOrganizationName(99))
}
