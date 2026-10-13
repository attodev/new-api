package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from upstream a073f74b3 (controller/user_manage_test.go,
// controller/token.go, controller/redemption.go, controller/topup.go wallet
// bounds). Upstream's wallet ceiling MaxWalletQuota (2^53-1) is this fork's
// common.MaxQuota.

func setupQuotaCeilingControllerTestDB(t *testing.T) {
	t.Helper()
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.Redemption{}))
}

func performManageUserRequest(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/manage", body, 1)
	ctx.Set("role", common.RoleRootUser)
	ctx.Set("username", "root")
	ManageUser(ctx)
	var response map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestManageUserQuotaRespectsWalletCeiling(t *testing.T) {
	setupQuotaCeilingControllerTestDB(t)
	user := model.User{
		Username: "managed-quota-user", Password: "password", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", Quota: common.MaxQuota - 1,
	}
	require.NoError(t, model.DB.Create(&user).Error)

	response := performManageUserRequest(t, map[string]any{"id": user.Id, "action": "add_quota", "mode": "add", "value": 2})
	assert.Equal(t, false, response["success"])

	var updated model.User
	require.NoError(t, model.DB.First(&updated, user.Id).Error)
	assert.Equal(t, common.MaxQuota-1, updated.Quota)

	response = performManageUserRequest(t, map[string]any{"id": user.Id, "action": "add_quota", "mode": "add", "value": common.MaxQuota + 1})
	assert.Equal(t, false, response["success"])

	response = performManageUserRequest(t, map[string]any{"id": user.Id, "action": "add_quota", "mode": "override", "value": common.MaxQuota + 1})
	assert.Equal(t, false, response["success"])
	require.NoError(t, model.DB.First(&updated, user.Id).Error)
	assert.Equal(t, common.MaxQuota-1, updated.Quota)

	response = performManageUserRequest(t, map[string]any{"id": user.Id, "action": "add_quota", "mode": "add", "value": 1})
	assert.Equal(t, true, response["success"], fmt.Sprint(response["message"]))
	require.NoError(t, model.DB.First(&updated, user.Id).Error)
	assert.Equal(t, common.MaxQuota, updated.Quota)

	response = performManageUserRequest(t, map[string]any{"id": user.Id, "action": "add_quota", "mode": "override", "value": common.MaxQuota})
	assert.Equal(t, true, response["success"], fmt.Sprint(response["message"]))
}

func TestMaxTokenQuotaCapsAtWalletCeiling(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	common.QuotaPerUnit = 500_000
	assert.Equal(t, common.MaxQuota, maxTokenQuota()) // $1B is exactly the ceiling
	common.QuotaPerUnit = 500
	assert.Equal(t, int64(500_000_000_000), maxTokenQuota())
	common.QuotaPerUnit = 1e12
	assert.Equal(t, common.MaxQuota, maxTokenQuota())
}

func TestGetMinTopupSaturatesAtWalletCeiling(t *testing.T) {
	oldMinTopUp := operation_setting.MinTopUp
	operation_setting.MinTopUp = 1000
	t.Cleanup(func() { operation_setting.MinTopUp = oldMinTopUp })

	withTopUpQuotaSettings(t, 500_000, operation_setting.QuotaDisplayTypeTokens)
	assert.Equal(t, int64(500_000_000), getMinTopup())

	withTopUpQuotaSettings(t, 1e12, operation_setting.QuotaDisplayTypeTokens)
	assert.Equal(t, common.MaxQuota, getMinTopup())

	withTopUpQuotaSettings(t, 1e12, operation_setting.QuotaDisplayTypeUSD)
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
	setupQuotaCeilingControllerTestDB(t)
	confirmPaymentComplianceForTest(t)

	for _, quota := range []int64{0, -5, common.MaxQuota + 1} {
		response := performRedemptionRequest(t, http.MethodPost, map[string]any{"name": "bad", "count": 1, "quota": quota}, AddRedemption)
		assert.Equal(t, false, response["success"], "quota=%d", quota)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Redemption{}).Count(&count).Error)
	assert.Zero(t, count)

	response := performRedemptionRequest(t, http.MethodPost, map[string]any{"name": "ok", "count": 1, "quota": common.MaxQuota}, AddRedemption)
	require.Equal(t, true, response["success"], fmt.Sprint(response["message"]))

	var stored model.Redemption
	require.NoError(t, model.DB.First(&stored).Error)
	response = performRedemptionRequest(t, http.MethodPut, map[string]any{"id": stored.Id, "name": "ok", "quota": common.MaxQuota + 1}, UpdateRedemption)
	assert.Equal(t, false, response["success"])
	response = performRedemptionRequest(t, http.MethodPut, map[string]any{"id": stored.Id, "name": "ok", "quota": 0}, UpdateRedemption)
	assert.Equal(t, false, response["success"])

	require.NoError(t, model.DB.First(&stored, stored.Id).Error)
	assert.Equal(t, common.MaxQuota, int64(stored.Quota))
}

// settleTestQuota computes a single (channel-test) request charge, so it
// saturates at the per-request bound instead of wrapping.
func TestSettleTestQuotaSaturates(t *testing.T) {
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	quota, _ := settleTestQuota(nil, types.PriceData{UsePrice: true, ModelPrice: 1e20}, &dto.Usage{})
	assert.Equal(t, common.MaxRequestQuota, quota)

	quota, _ = settleTestQuota(nil, types.PriceData{ModelRatio: 1e30, CompletionRatio: 1}, &dto.Usage{PromptTokens: 10, CompletionTokens: 10})
	assert.Equal(t, common.MaxRequestQuota, quota)

	quota, _ = settleTestQuota(nil, types.PriceData{ModelRatio: 2, CompletionRatio: 1.5}, &dto.Usage{PromptTokens: 10, CompletionTokens: 3})
	assert.Equal(t, 30, quota) // (10 + round(4.5)=5) * 2

	quota, _ = settleTestQuota(nil, types.PriceData{UsePrice: true, ModelPrice: 0.001}, &dto.Usage{})
	assert.Equal(t, 500, quota)
}
