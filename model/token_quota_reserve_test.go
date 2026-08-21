package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func insertTokenWithQuota(t *testing.T, remainQuota int) *Token {
	t.Helper()
	tok := &Token{
		UserId:      1,
		Key:         randomTestKey(t),
		Status:      common.TokenStatusEnabled,
		RemainQuota: remainQuota,
	}
	require.NoError(t, DB.Create(tok).Error)
	return tok
}

func randomTestKey(t *testing.T) string {
	t.Helper()
	return t.Name()
}

func TestTryDecreaseTokenQuota_SucceedsWhenEnough(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 100)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, 40)
	require.NoError(t, err)
	require.True(t, ok)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 60, reloaded.RemainQuota)
	require.Equal(t, 40, reloaded.UsedQuota)
}

func TestTryDecreaseTokenQuota_RejectsWhenInsufficient(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 10)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, 40)
	require.NoError(t, err)
	require.False(t, ok)

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 10, reloaded.RemainQuota, "balance must be untouched on rejection")
}

func TestTryDecreaseTokenQuota_RejectsNegativeAmount(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 10)

	ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, -5)
	require.Error(t, err)
	require.False(t, ok)
}

// TestTryDecreaseTokenQuota_ConcurrentRequestsCannotOverspend reproduces the
// race a plain "read remain_quota, check, then decrease" sequence allows:
// many concurrent requests each read the same balance, all pass the check,
// and all decrement - driving the balance negative by far more than any
// single check actually permitted. The guarded UPDATE (remain_quota >= ?)
// must let through only as many callers as the starting balance covers.
func TestTryDecreaseTokenQuota_ConcurrentRequestsCannotOverspend(t *testing.T) {
	truncateTables(t)
	tok := insertTokenWithQuota(t, 100)

	const cost = 30
	const attempts = 10 // only floor(100/30) = 3 of these may succeed

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := TryDecreaseTokenQuota(tok.Id, tok.Key, cost)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 3, successes, "exactly floor(100/30) attempts should have succeeded")

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 100-3*cost, reloaded.RemainQuota)
	require.GreaterOrEqual(t, reloaded.RemainQuota, 0, "balance must never go negative under concurrency")
}
