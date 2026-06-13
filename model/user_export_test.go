package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAllUsersForExport(t *testing.T) {
	// This test requires a database connection.
	// Run with: go test ./model/ -run TestGetAllUsersForExport -v
	users, err := GetAllUsersForExport()
	require.NoError(t, err)
	assert.NotNil(t, users)
	// Password field must be empty (omitted)
	for _, u := range users {
		assert.Empty(t, u.Password, "password must not be exported")
		assert.Empty(t, u.AccessToken, "access token must not be exported")
	}
}
