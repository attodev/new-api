package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// Ported from upstream a073f74b3 (service/quota_saturation_test.go
// additions). These are single-request charges, so they saturate at the
// per-request bound common.MaxRequestQuota (upstream: common.MaxQuota).

func TestCalcViolationFeeQuotaSaturates(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	require.Equal(t, common.MaxRequestQuota, calcViolationFeeQuota(1e20, 1))
	require.Equal(t, 500_000, calcViolationFeeQuota(1, 1))
	require.Equal(t, 0, calcViolationFeeQuota(0, 1))
}

func TestCalcOpenRouterCacheCreateTokensDoesNotWrap(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	priceData := types.PriceData{
		ModelRatio:         1,
		CacheCreationRatio: 2,
		CacheRatio:         1,
		CompletionRatio:    1,
	}
	require.Equal(t, -1, CalcOpenRouterCacheCreateTokens(dto.Usage{Cost: math.Inf(1)}, priceData))
	require.Equal(t, -1, CalcOpenRouterCacheCreateTokens(dto.Usage{Cost: 1e20}, priceData))

	// In range: cost of 100 prompt tokens written to cache at 2x ratio.
	// quotaPrice = 1/QuotaPerUnit; cost = 100*quotaPrice + 100*quotaPrice => 100 cache-create tokens.
	cost := 200.0 / common.QuotaPerUnit
	require.Equal(t, 100, CalcOpenRouterCacheCreateTokens(dto.Usage{Cost: cost, PromptTokens: 100}, priceData))
}
