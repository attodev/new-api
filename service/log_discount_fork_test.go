package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestAppendDiscountInfoRecordsMultiplierAndBaseGroupRatio(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.63)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
		GroupRatio: 0.63, BaseGroupRatio: 0.9, DiscountMultiplier: 0.7, DiscountSource: "model"}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.InDelta(t, 0.7, snap["discount_multiplier"], 1e-9)
	require.Equal(t, "model", snap["discount_source"])
	require.InDelta(t, 0.9, snap["group_ratio"], 1e-9)
}

func TestAppendDiscountInfo_FullDiscountKeepsBaseGroupRatio(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.0)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{
		GroupRatio: 0, BaseGroupRatio: 1, DiscountMultiplier: 0, DiscountSource: "vendor"}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.InDelta(t, 0.0, snap["discount_multiplier"], 1e-9)
	require.Equal(t, "vendor", snap["discount_source"])
	require.InDelta(t, 1.0, snap["group_ratio"], 1e-9)
}

func TestAppendDiscountInfoNoDiscountWritesNothing(t *testing.T) {
	other := model.NewLogOther()
	other.SetPublic("group_ratio", 0.9)
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 0.9, BaseGroupRatio: 0.9, DiscountMultiplier: 1}}}

	appendDiscountInfo(other, info)

	snap := other.Snapshot()
	require.NotContains(t, snap, "discount_multiplier")
	require.InDelta(t, 0.9, snap["group_ratio"], 1e-9)
}
