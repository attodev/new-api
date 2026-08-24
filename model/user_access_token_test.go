package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateUserAccessToken_PersistsDirectly reproduces a real bug: User.Update
// now Omits "access_token" (to stop a stale in-memory snapshot from
// clobbering concurrently-updated fields), but GenerateAccessToken's
// SetAccessToken+Update(false) pattern relied on Update() actually writing
// that column - so regenerating a token silently stopped persisting it,
// while still reporting success. UpdateUserAccessToken writes the column
// directly, bypassing Update()'s Omit list entirely.
func TestUpdateUserAccessToken_PersistsDirectly(t *testing.T) {
	truncateTables(t)

	u := &User{Username: "access-token-user", Password: "x"}
	require.NoError(t, DB.Create(u).Error)

	require.NoError(t, UpdateUserAccessToken(u.Id, "new-token-value"))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, u.Id).Error)
	require.NotNil(t, reloaded.AccessToken)
	require.Equal(t, "new-token-value", *reloaded.AccessToken)
}

func TestUpdateUserAccessToken_RejectsZeroID(t *testing.T) {
	require.Error(t, UpdateUserAccessToken(0, "x"))
}

func TestUpdateUserAccessToken_RejectsUnknownID(t *testing.T) {
	truncateTables(t)
	require.Error(t, UpdateUserAccessToken(999999, "x"))
}
