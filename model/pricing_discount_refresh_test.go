package model

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fork: O6 — the vendor-name snapshot is refreshed in the background once it
// is older than vendorNameRefreshAge, so a vendor change reaches billing even
// when nothing else calls GetPricing; the resolver never waits for it.

// stubVendorNameRefresh replaces the refresh build with a counter that blocks
// until release is called, and restores everything after the in-flight
// refresh (if any) has finished.
func stubVendorNameRefresh(t *testing.T) (calls *atomic.Int32, release func()) {
	t.Helper()
	resetModelVendorNameCache()
	calls = &atomic.Int32{}
	gate := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	original := getPricingForRefresh
	getPricingForRefresh = func() []Pricing {
		calls.Add(1)
		<-gate
		return nil
	}
	t.Cleanup(func() {
		release()
		vendorNameRefreshMu.Lock() // wait for the in-flight refresh
		getPricingForRefresh = original
		vendorNameRefreshMu.Unlock()
		resetModelVendorNameCache()
	})
	return calls, release
}

func backdateModelVendorNames(age time.Duration) {
	modelVendorNameMu.Lock()
	modelVendorNamePublishedAt = time.Now().Add(-age)
	modelVendorNameMu.Unlock()
}

func waitVendorNameRefreshDone() {
	vendorNameRefreshMu.Lock()
	vendorNameRefreshMu.Unlock() //nolint:staticcheck // waiting for the refresh goroutine
}

func TestResolveVendorNameStaleSnapshotRefreshesInBackground(t *testing.T) {
	calls, release := stubVendorNameRefresh(t)
	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})
	backdateModelVendorNames(2 * time.Minute)

	// the refresh is blocked: the resolver still answers from the snapshot
	name, ok := resolveVendorName("gpt-4o")
	require.True(t, ok)
	require.Equal(t, "OpenAI", name)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)

	// a second stale resolve while the refresh runs starts no duplicate, even
	// once the attempt window is cleared (only TryLock stands in the way)
	vendorNameRefreshLast.Store(0)
	name, ok = resolveVendorName("gpt-4o")
	require.True(t, ok)
	require.Equal(t, "OpenAI", name)

	release()
	waitVendorNameRefreshDone()
	require.EqualValues(t, 1, calls.Load())
}

func TestResolveVendorNameFreshSnapshotDoesNotRefresh(t *testing.T) {
	calls, release := stubVendorNameRefresh(t)
	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})
	backdateModelVendorNames(30 * time.Second)

	for i := 0; i < 20; i++ {
		name, ok := resolveVendorName("gpt-4o")
		require.True(t, ok)
		require.Equal(t, "OpenAI", name)
	}
	release()
	waitVendorNameRefreshDone()
	require.EqualValues(t, 0, calls.Load())
}

// A refresh that publishes nothing (pricing build failed) is not retried on
// every billed request: at most one attempt per vendorNameRefreshAge.
func TestResolveVendorNameStaleRefreshAttemptedOncePerWindow(t *testing.T) {
	calls, release := stubVendorNameRefresh(t)
	release() // refreshes return at once without publishing
	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})
	backdateModelVendorNames(2 * time.Minute)

	for i := 0; i < 20; i++ {
		_, ok := resolveVendorName("gpt-4o")
		require.True(t, ok)
		waitVendorNameRefreshDone()
	}
	require.EqualValues(t, 1, calls.Load())
}

// An empty rebuild (vendors or models failed to load) keeps a non-empty
// previous snapshot; the first publish may still be empty.
func TestPublishModelVendorNamesKeepsPreviousOnEmptyRebuild(t *testing.T) {
	t.Cleanup(resetModelVendorNameCache)
	resetModelVendorNameCache()

	publishModelVendorNames(map[string]string{})
	_, built := lookupModelVendorName("gpt-4o")
	require.True(t, built, "an empty first build still publishes")

	publishModelVendorNames(map[string]string{"gpt-4o": "OpenAI"})
	backdateModelVendorNames(2 * time.Minute)
	publishModelVendorNames(map[string]string{})
	name, built := lookupModelVendorName("gpt-4o")
	require.True(t, built)
	require.Equal(t, "OpenAI", name, "an empty rebuild must not drop the previous vendor names")

	modelVendorNameMu.RLock()
	age := time.Since(modelVendorNamePublishedAt)
	modelVendorNameMu.RUnlock()
	require.Less(t, age, time.Minute, "the kept snapshot counts as refreshed")
}
