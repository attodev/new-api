package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// Epay settlement is driven through model.RechargeEpay directly: EpayNotify
// needs a live epay client and a signed callback, but its only state change is
// this single call, so the transactional contract is what these tests pin.

func setupEpaySettlementTest(t *testing.T, userId int, userQuota int64) string {
	t.Helper()
	setupOrganizationControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}))

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	require.NoError(t, model.DB.Create(&model.User{
		Id:       userId,
		Username: "epay-settle-user",
		Password: "x",
		Role:     common.RoleCommonUser,
		AffCode:  "epay-settle-user",
		Group:    "default",
		Quota:    userQuota,
	}).Error)
	tradeNo := "epay-settle-trade-1"
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId:          userId,
		Amount:          10,
		Money:           10,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}).Error)
	return tradeNo
}

func epayTopUpStatus(t *testing.T, tradeNo string) *model.TopUp {
	t.Helper()
	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", tradeNo).First(&topUp).Error)
	return &topUp
}

func epayUserQuota(t *testing.T, userId int) int64 {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, userId).Error)
	return user.Quota
}

func epayTopUpLogCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeTopup).Count(&count).Error)
	return count
}

func TestRechargeEpaySettlesOrderAndCreditsWalletOnce(t *testing.T) {
	tradeNo := setupEpaySettlementTest(t, 701, 1000)

	quota, err := model.RechargeEpay(tradeNo, "wxpay", "127.0.0.1")
	require.NoError(t, err)
	require.Equal(t, int64(10*500000), quota)

	topUp := epayTopUpStatus(t, tradeNo)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, "wxpay", topUp.PaymentMethod, "actual payment method is recorded")
	require.NotZero(t, topUp.CompleteTime)
	require.Equal(t, int64(1000+10*500000), epayUserQuota(t, 701))
	require.Equal(t, int64(1), epayTopUpLogCount(t))

	// A repeated callback must be ignored and must not credit again.
	_, err = model.RechargeEpay(tradeNo, "wxpay", "127.0.0.1")
	require.Error(t, err)
	require.Equal(t, int64(1000+10*500000), epayUserQuota(t, 701))
	require.Equal(t, int64(1), epayTopUpLogCount(t))
}

func TestRechargeEpayKeepsOrderPendingWhenCreditFails(t *testing.T) {
	tradeNo := setupEpaySettlementTest(t, 702, common.MaxQuota-1)

	_, err := model.RechargeEpay(tradeNo, "wxpay", "127.0.0.1")
	require.ErrorIs(t, err, model.ErrTopUpQuotaCapacityExceeded)

	topUp := epayTopUpStatus(t, tradeNo)
	require.Equal(t, common.TopUpStatusPending, topUp.Status, "a failed credit must leave the order retryable")
	require.Equal(t, "alipay", topUp.PaymentMethod, "payment method change rolls back with the credit")
	require.Zero(t, topUp.CompleteTime)
	require.Equal(t, common.MaxQuota-1, epayUserQuota(t, 702))
	require.Zero(t, epayTopUpLogCount(t))
}

func TestRechargeEpayRejectsOtherProviderOrders(t *testing.T) {
	tradeNo := setupEpaySettlementTest(t, 703, 0)
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("trade_no = ?", tradeNo).
		Update("payment_provider", model.PaymentProviderStripe).Error)

	_, err := model.RechargeEpay(tradeNo, "alipay", "127.0.0.1")
	require.ErrorIs(t, err, model.ErrPaymentMethodMismatch)
	require.Equal(t, common.TopUpStatusPending, epayTopUpStatus(t, tradeNo).Status)
	require.Zero(t, epayUserQuota(t, 703))
}
