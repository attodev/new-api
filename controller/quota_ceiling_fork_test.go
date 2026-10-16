package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 3f1cbeeb7 (controller/quota_ceiling_test.go). Team's
// wallet ceiling common.MaxQuota is develop's common.MaxWalletQuota (2^53-1);
// team's per-request bound common.MaxRequestQuota is develop's common.MaxQuota
// (int32). ManageUser ceilings are already covered by develop's
// TestManageUserQuotaTargetsAndWalletBounds, so they are not repeated here.

var walletQuotaExceedsMessage = fmt.Sprintf("wallet quota exceeds %d", common.MaxWalletQuota)

const redemptionQuotaPositiveMessage = "Redemption quota must be positive"

func requireQuotaCeilingRejected(t *testing.T, response map[string]any, message string) {
	t.Helper()
	assert.Equal(t, false, response["success"])
	assert.Equal(t, message, response["message"])
}

func TestMaxTokenQuotaCapsAtWalletCeiling(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	common.QuotaPerUnit = 500
	assert.Equal(t, 500_000_000_000, maxTokenQuota())
	common.QuotaPerUnit = 500_000
	assert.Equal(t, 500_000_000_000_000, maxTokenQuota())
	// $1B straddles the ceiling between these two unit prices.
	common.QuotaPerUnit = 9_007_199
	assert.Equal(t, 9_007_199_000_000_000, maxTokenQuota())
	common.QuotaPerUnit = 9_007_200
	assert.Equal(t, common.MaxWalletQuota, maxTokenQuota())
	common.QuotaPerUnit = 1e12
	assert.Equal(t, common.MaxWalletQuota, maxTokenQuota())
}

func TestGetMinTopupSaturatesAtWalletCeiling(t *testing.T) {
	oldMinTopUp := operation_setting.MinTopUp
	operation_setting.MinTopUp = 1000
	t.Cleanup(func() { operation_setting.MinTopUp = oldMinTopUp })

	withTopUpQuotaSettings(t, 500_000, operation_setting.QuotaDisplayTypeTokens)
	assert.Equal(t, int64(500_000_000), getMinTopup())

	// 1000 * 1e13 = 1e16 exceeds 2^53-1 (team used 1e12 against its lower ceiling).
	withTopUpQuotaSettings(t, 1e13, operation_setting.QuotaDisplayTypeTokens)
	assert.Equal(t, int64(common.MaxWalletQuota), getMinTopup())

	withTopUpQuotaSettings(t, 1e13, operation_setting.QuotaDisplayTypeUSD)
	assert.Equal(t, int64(1000), getMinTopup())
}

func performRedemptionRequest(t *testing.T, method string, body map[string]any, handler func(ctx *gin.Context)) map[string]any {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, method, "/api/redemption/", body, 1)
	handler(ctx)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestRedemptionHandlersRejectQuotaOutsideWalletRange(t *testing.T) {
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Redemption{}))
	confirmPaymentComplianceForTest(t)

	for _, tc := range []struct {
		quota   int64
		message string
	}{
		{quota: 0, message: redemptionQuotaPositiveMessage},
		{quota: -5, message: redemptionQuotaPositiveMessage},
		{quota: common.MaxWalletQuota + 1, message: walletQuotaExceedsMessage},
	} {
		response := performRedemptionRequest(t, http.MethodPost, map[string]any{"name": "bad", "count": 1, "quota": tc.quota}, AddRedemption)
		requireQuotaCeilingRejected(t, response, tc.message)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
	assert.Zero(t, count)

	response := performRedemptionRequest(t, http.MethodPost, map[string]any{"name": "ok", "count": 1, "quota": common.MaxWalletQuota}, AddRedemption)
	require.Equal(t, true, response["success"], fmt.Sprint(response["message"]))

	var stored model.Redemption
	require.NoError(t, model.DB.First(&stored).Error)
	response = performRedemptionRequest(t, http.MethodPut, map[string]any{"id": stored.Id, "name": "ok", "quota": common.MaxWalletQuota + 1}, UpdateRedemption)
	requireQuotaCeilingRejected(t, response, walletQuotaExceedsMessage)
	response = performRedemptionRequest(t, http.MethodPut, map[string]any{"id": stored.Id, "name": "ok", "quota": 0}, UpdateRedemption)
	requireQuotaCeilingRejected(t, response, redemptionQuotaPositiveMessage)

	require.NoError(t, model.DB.First(&stored, stored.Id).Error)
	assert.Equal(t, common.MaxWalletQuota, stored.Quota)
}

// settleTestQuota computes a single (channel-test) request charge, so it
// saturates at the per-request bound instead of wrapping.
func TestSettleTestQuotaSaturates(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	quota, _ := settleTestQuota(nil, hosttypes.PriceData{UsePrice: true, ModelPrice: 1e20}, &dto.Usage{})
	assert.Equal(t, common.MaxQuota, quota)

	quota, _ = settleTestQuota(nil, hosttypes.PriceData{ModelRatio: 1e30, CompletionRatio: 1}, &dto.Usage{PromptTokens: 10, CompletionTokens: 10})
	assert.Equal(t, common.MaxQuota, quota)

	quota, _ = settleTestQuota(nil, hosttypes.PriceData{ModelRatio: 2, CompletionRatio: 1.5}, &dto.Usage{PromptTokens: 10, CompletionTokens: 3})
	assert.Equal(t, 30, quota) // (10 + round(4.5)=5) * 2

	quota, _ = settleTestQuota(nil, hosttypes.PriceData{UsePrice: true, ModelPrice: 0.001}, &dto.Usage{})
	assert.Equal(t, 500, quota)
}
