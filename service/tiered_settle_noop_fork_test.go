package service

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// An auto-group retry can move a request to a differently priced group after
// the first attempt's pre-consume; the snapshot's frozen group ratio must
// follow the group the request actually landed on.
func TestRefreshTieredBillingGroupSyncsSnapshotToCurrentGroupRatio(t *testing.T) {
	expr := `tier("base", p * 2 + c * 10)`
	relayInfo := makeRelayInfo(expr, 1.0, 1000, 500) // pre-consumed at group ratio 1.0

	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 2.0

	snap, err := refreshTieredBillingGroup(relayInfo)
	require.NoError(t, err)
	require.NotNil(t, snap)
	require.Equal(t, 2.0, snap.GroupRatio)

	ok, quota, _ := TryTieredSettle(relayInfo, billingexpr.TokenParams{P: 1000, C: 500})
	require.True(t, ok)
	// cost = 1000*2 + 500*10 = 7000 -> quotaBeforeGroup = 7000/1e6*500000 = 3500
	// at the refreshed group ratio 2.0: 3500*2 = 7000
	require.Equal(t, 7000, quota)
}

func TestRefreshTieredBillingGroupNoOpWhenGroupUnchanged(t *testing.T) {
	expr := `tier("base", p * 2 + c * 10)`
	relayInfo := makeRelayInfo(expr, 1.5, 1000, 500)
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1.5 // same group throughout

	original := *relayInfo.TieredBillingSnapshot
	snap, err := refreshTieredBillingGroup(relayInfo)
	require.NoError(t, err)
	require.Same(t, relayInfo.TieredBillingSnapshot, snap)
	require.Equal(t, original, *snap)
}

func TestRefreshTieredBillingGroupNilWhenNotTieredBilling(t *testing.T) {
	for name, relayInfo := range map[string]*relaycommon.RelayInfo{
		"nil relay info": nil,
		"no snapshot":    {},
		"ratio mode":     {TieredBillingSnapshot: &billingexpr.BillingSnapshot{BillingMode: "ratio"}},
	} {
		t.Run(name, func(t *testing.T) {
			snap, err := refreshTieredBillingGroup(relayInfo)
			require.NoError(t, err)
			require.Nil(t, snap)
		})
	}
}

// The controller-facing entry point runs on every retry attempt regardless of
// billing mode, so it must be a no-op for non-tiered requests.
func TestPrepareTieredBillingForSelectedGroupNoOpWhenNotTiered(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{}
	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))
	require.Equal(t, types.PriceData{}, relayInfo.PriceData)
	require.Nil(t, relayInfo.Billing)
	require.Zero(t, relayInfo.FinalPreConsumedQuota)

	// A non-tiered request keeps its existing price data and reservation untouched.
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	priced := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		PriceData: types.PriceData{
			FreeModel:      true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}
	wantPriceData := priced.PriceData
	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, priced))
	require.Equal(t, wantPriceData, priced.PriceData)
	require.Empty(t, billing.reserveTargets)
	require.Equal(t, 50_000, priced.FinalPreConsumedQuota)
}

func TestPrepareTieredBillingForSelectedGroupClearsFreeModelAfterPaidRetry(t *testing.T) {
	billing := &recordingBillingSettler{preConsumedQuota: 50_000}
	relayInfo := &relaycommon.RelayInfo{
		Billing:               billing,
		FinalPreConsumedQuota: 50_000,
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			ExprString:                `tier("base", p)`,
			ExprHash:                  billingexpr.ExprHashString(`tier("base", p)`),
			GroupRatio:                0.10,
			EstimatedQuotaBeforeGroup: 500_000,
			EstimatedQuotaAfterGroup:  50_000,
			QuotaPerUnit:              testQuotaPerUnit,
		},
		PriceData: types.PriceData{
			FreeModel:      true,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.20},
		},
	}

	require.Nil(t, PrepareTieredBillingForSelectedGroup(nil, relayInfo))
	require.False(t, relayInfo.PriceData.FreeModel)
	require.Equal(t, []int{100_000}, billing.reserveTargets)
	require.Equal(t, 100_000, relayInfo.FinalPreConsumedQuota)
}
