package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// TestUserUpdate_DoesNotClobberQuotaFromStaleSnapshot reproduces a real race:
// a caller loads a user (capturing whatever quota/aff fields were current at
// that moment), a concurrent operation changes the balance, and then the
// original caller saves a routine profile edit built from its now-stale
// snapshot. Update must not silently overwrite the balance that changed in
// between - only the fields a profile edit actually owns should be written.
func TestUserUpdate_DoesNotClobberQuotaFromStaleSnapshot(t *testing.T) {
	truncateTables(t)

	u := &User{Username: "quota-omit-user", Password: "x", Quota: 500, AffCount: 1, AffQuota: 10, AffHistoryQuota: 10}
	require.NoError(t, DB.Create(u).Error)

	// Caller loads a snapshot before editing their profile.
	snapshot, err := GetUserById(u.Id, false)
	require.NoError(t, err)

	// Concurrently, a billing event changes the balance and affiliate stats.
	require.NoError(t, IncreaseUserQuota(u.Id, 600, true))
	require.NoError(t, inviteUser(u.Id))

	// The caller now saves a routine display-name edit built from the stale snapshot.
	snapshot.DisplayName = "New Display Name"
	require.NoError(t, snapshot.Update(false))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, u.Id).Error)
	require.Equal(t, "New Display Name", reloaded.DisplayName, "the intended edit must still apply")
	require.Equal(t, int64(1100), reloaded.Quota, "quota must reflect the concurrent billing event, not the stale snapshot")
	require.Equal(t, 2, reloaded.AffCount)
	require.Equal(t, int64(10)+common.QuotaForInviter, reloaded.AffQuota)
}
