package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// An organization order whose credit would overflow the organization wallet
// is rolled back as a whole: the order stays pending, so it can be retried or
// expired instead of being marked paid without a credit.
func TestRechargeEpayOrganizationOrderStaysPendingWhenWalletFull(t *testing.T) {
	cleanupTopUpTargetTest(t)
	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	owner := &User{Id: 806, Username: "epay_org_full_owner", Status: common.UserStatusEnabled, Quota: 10}
	require.NoError(t, DB.Create(owner).Error)
	org := &Organization{Name: "epay-org-full", OwnerUserId: owner.Id, Quota: int64(common.MaxWalletQuota) - 10, Status: OrganizationStatusEnabled}
	require.NoError(t, DB.Create(org).Error)
	order := createEpayTestOrder(t, owner.Id, "EPAYORGFULL", PaymentProviderEpay, common.TopUpStatusPending)
	require.NoError(t, DB.Model(&TopUp{}).Where("id = ?", order.Id).
		Updates(map[string]any{"target_type": TopUpTargetTypeOrganization, "target_id": org.Id}).Error)

	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
	require.False(t, alreadyDone)

	reloaded := GetTopUpByTradeNo(order.TradeNo)
	require.NotNil(t, reloaded)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)
	require.Zero(t, reloaded.CompleteTime)

	var gotOrg Organization
	require.NoError(t, DB.First(&gotOrg, org.Id).Error)
	require.EqualValues(t, int64(common.MaxWalletQuota)-10, gotOrg.Quota)
	var gotOwner User
	require.NoError(t, DB.First(&gotOwner, owner.Id).Error)
	require.Equal(t, 10, gotOwner.Quota)
}
