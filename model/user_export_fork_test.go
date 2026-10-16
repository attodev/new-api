package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Ported from team model/user_export_test.go (4728bdc34). Team ran it only
// under the integration build tag against a live database; here it runs on
// the SQLite test database with fixture users that do carry secrets.
func TestGetAllUsersForExportOmitsSecretsAndOrdersById(t *testing.T) {
	setupSQLiteModelTestDB(t)
	tokenA := "exportaccesstoken000000000000002"
	tokenB := "exportaccesstoken000000000000001"
	// Insert the higher id first so ordering comes from the query, not insertion.
	require.NoError(t, DB.Create(&User{Id: 20, Username: "export-b", Password: "hashed-password-b", AffCode: "export-aff-b", Status: common.UserStatusEnabled, AccessToken: &tokenA}).Error)
	require.NoError(t, DB.Create(&User{Id: 10, Username: "export-a", Password: "hashed-password-a", AffCode: "export-aff-a", Status: common.UserStatusEnabled, AccessToken: &tokenB}).Error)

	var stored User
	require.NoError(t, DB.First(&stored, 20).Error)
	require.NotEmpty(t, stored.Password, "fixture must store a password")
	require.NotNil(t, stored.AccessToken, "fixture must store a legacy access token")

	users, err := GetAllUsersForExport()
	require.NoError(t, err)
	require.Len(t, users, 2)
	require.Equal(t, 10, users[0].Id)
	require.Equal(t, 20, users[1].Id)
	for _, u := range users {
		require.Empty(t, u.Password, "password must not be exported")
		if u.AccessToken != nil {
			require.Empty(t, *u.AccessToken, "access token must not be exported")
		}
	}
}
