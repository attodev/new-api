package service

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// TestRefreshTieredBillingGroup_SyncsSnapshotToCurrentGroupRatio reproduces a
// real bug: an auto-group retry can move a request from one group to a
// differently-priced group mid-flight (relay/controller updates
// PriceData.GroupRatioInfo.GroupRatio on each attempt via getChannel), but
// the group ratio frozen into TieredBillingSnapshot at the *first* attempt's
// pre-consume was never refreshed - settlement would bill the initial
// group's ratio regardless of which group the request actually landed on.
func TestRefreshTieredBillingGroup_SyncsSnapshotToCurrentGroupRatio(t *testing.T) {
	expr := `tier("base", p * 2 + c * 10)`
	relayInfo := makeRelayInfo(expr, 1.0, 1000, 500) // pre-consumed at group ratio 1.0

	// Auto-group retry moved this request to a group priced at 2x.
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 2.0

	snap, err := refreshTieredBillingGroup(relayInfo)
	require.NoError(t, err)
	require.NotNil(t, snap)
	require.Equal(t, 2.0, snap.GroupRatio, "snapshot's group ratio must follow the group the request actually landed on")

	ok, quota, _ := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1000, C: 500})
	require.True(t, ok)
	// cost = 1000*2 + 500*10 = 7000 -> quotaBeforeGroup = 7000/1e6*500000 = 3500
	// at the refreshed group ratio 2.0: 3500*2 = 7000
	require.Equal(t, 7000, quota, "settlement must use the final group's ratio, not the one frozen at pre-consume")
}

func TestRefreshTieredBillingGroup_NoOpWhenGroupUnchanged(t *testing.T) {
	expr := `tier("base", p * 2 + c * 10)`
	relayInfo := makeRelayInfo(expr, 1.5, 1000, 500)
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1.5 // same group throughout

	original := *relayInfo.TieredBillingSnapshot
	snap, err := refreshTieredBillingGroup(relayInfo)
	require.NoError(t, err)
	require.Equal(t, original, *snap)
}

func TestRefreshTieredBillingGroup_NilWhenNotTieredBilling(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{}
	snap, err := refreshTieredBillingGroup(relayInfo)
	require.NoError(t, err)
	require.Nil(t, snap)
}

// TestPrepareTieredBillingForSelectedGroup_NoOpWhenNotTiered ensures the
// controller-facing entry point is a safe no-op for non-tiered-billing
// requests (the overwhelming majority), since it now runs on every retry
// attempt regardless of billing mode.
func TestPrepareTieredBillingForSelectedGroup_NoOpWhenNotTiered(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{}
	apiErr := PrepareTieredBillingForSelectedGroup(nil, relayInfo)
	require.Nil(t, apiErr)
	require.Equal(t, types.PriceData{}, relayInfo.PriceData)
}
