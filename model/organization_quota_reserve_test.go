package model

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTryReserveOrganizationQuotaConcurrentRequestsCannotOverspend(t *testing.T) {
	truncateTables(t)
	organization := &Organization{Name: t.Name(), OwnerUserId: 1, Quota: 100, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(organization).Error)

	const cost int64 = 30
	const attempts = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := TryReserveOrganizationQuota(organization.Id, cost)
			require.NoError(t, err)
			if ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 3, successes)
	var reloaded Organization
	require.NoError(t, DB.First(&reloaded, organization.Id).Error)
	require.Equal(t, int64(10), reloaded.Quota)
}
