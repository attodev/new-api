package model

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Ported from team model/user_quota_reserve_test.go (ba6afab57) and
// model/token_quota_reserve_test.go (a1c25c3a4). With Redis disabled the
// reserve paths are a single guarded UPDATE (quota >= cost); concurrent
// callers must never spend more than the starting balance covers. The team
// cache-pending, sync-delta and TTL cases are not ported (O12 residency).

const (
	reserveConcurrencyBalance  = 100
	reserveConcurrencyCost     = 30
	reserveConcurrencyAttempts = 10
)

// runConcurrentReserves starts every attempt behind one barrier and returns
// the number of successful reservations. Any returned error fails the test.
func runConcurrentReserves(t *testing.T, attempts int, reserve func() (bool, error)) int64 {
	t.Helper()
	var (
		ready     sync.WaitGroup
		done      sync.WaitGroup
		successes atomic.Int64
	)
	start := make(chan struct{})
	errs := make(chan error, attempts)
	ready.Add(attempts)
	done.Add(attempts)
	for range attempts {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			ok, err := reserve()
			if err != nil {
				errs <- err
				return
			}
			if ok {
				successes.Add(1)
			}
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	return successes.Load()
}

func TestTryReserveUserQuotaConcurrentRequestsCannotOverspend(t *testing.T) {
	setupSQLiteModelTestDB(t)
	user := &User{Username: "reserve-concurrent", Password: "password", Status: common.UserStatusEnabled, Quota: reserveConcurrencyBalance}
	require.NoError(t, DB.Create(user).Error)

	successes := runConcurrentReserves(t, reserveConcurrencyAttempts, func() (bool, error) {
		return TryReserveUserQuota(user.Id, reserveConcurrencyCost)
	})

	require.Equal(t, int64(reserveConcurrencyBalance/reserveConcurrencyCost), successes)
	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, reserveConcurrencyBalance-3*reserveConcurrencyCost, reloaded.Quota)
}

func TestTryReserveUserQuotaRejectsNegativeAmount(t *testing.T) {
	setupSQLiteModelTestDB(t)
	user := &User{Username: "reserve-negative", Password: "password", Status: common.UserStatusEnabled, Quota: 10}
	require.NoError(t, DB.Create(user).Error)

	ok, err := TryReserveUserQuota(user.Id, -5)
	require.Error(t, err)
	require.False(t, ok)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.Equal(t, 10, reloaded.Quota)
}

func insertReserveConcurrencyToken(t *testing.T, key string, remainQuota int, unlimited bool) *Token {
	t.Helper()
	tok := &Token{
		UserId:         1,
		Key:            key,
		Name:           key,
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    remainQuota,
		UnlimitedQuota: unlimited,
	}
	require.NoError(t, DB.Create(tok).Error)
	return tok
}

func TestTryReserveTokenQuotaConcurrentRequestsCannotOverspend(t *testing.T) {
	setupSQLiteModelTestDB(t)
	tok := insertReserveConcurrencyToken(t, "reserve-concurrent-limited", reserveConcurrencyBalance, false)

	successes := runConcurrentReserves(t, reserveConcurrencyAttempts, func() (bool, error) {
		return TryReserveTokenQuota(tok.Id, tok.Key, reserveConcurrencyCost, false)
	})

	require.Equal(t, int64(reserveConcurrencyBalance/reserveConcurrencyCost), successes)
	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, reserveConcurrencyBalance-3*reserveConcurrencyCost, reloaded.RemainQuota)
	require.Equal(t, 3*reserveConcurrencyCost, reloaded.UsedQuota)
}

func TestTryReserveTokenQuotaConcurrentUnlimitedAlwaysSucceeds(t *testing.T) {
	setupSQLiteModelTestDB(t)
	tok := insertReserveConcurrencyToken(t, "reserve-concurrent-unlimited", reserveConcurrencyBalance, true)

	successes := runConcurrentReserves(t, reserveConcurrencyAttempts, func() (bool, error) {
		return TryReserveTokenQuota(tok.Id, tok.Key, reserveConcurrencyCost, true)
	})

	require.Equal(t, int64(reserveConcurrencyAttempts), successes)
	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	// Unlimited tokens skip the balance check but still keep remain/used
	// accounting, so the remaining quota may go negative.
	require.Equal(t, reserveConcurrencyAttempts*reserveConcurrencyCost, reloaded.UsedQuota)
	require.Equal(t, reserveConcurrencyBalance-reserveConcurrencyAttempts*reserveConcurrencyCost, reloaded.RemainQuota)
}

func TestTryReserveTokenQuotaRejectsNegativeAmount(t *testing.T) {
	setupSQLiteModelTestDB(t)
	tok := insertReserveConcurrencyToken(t, "reserve-negative-token", 10, false)

	for _, unlimited := range []bool{false, true} {
		ok, err := TryReserveTokenQuota(tok.Id, tok.Key, -5, unlimited)
		require.Error(t, err, "unlimited=%t", unlimited)
		require.False(t, ok, "unlimited=%t", unlimited)
	}

	var reloaded Token
	require.NoError(t, DB.First(&reloaded, tok.Id).Error)
	require.Equal(t, 10, reloaded.RemainQuota)
	require.Equal(t, 0, reloaded.UsedQuota)
}
