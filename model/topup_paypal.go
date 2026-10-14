package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// fork: O2 — PayPal settlement, mirrors RechargeWaffo. Money is the USD amount
// after the group ratio, so quota = Money * QuotaPerUnit (same as Stripe).
func RechargePayPal(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}
	var quotaToAdd int
	topUp := &TopUp{}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}
		if topUp.PaymentProvider != PaymentProviderPayPal {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil // idempotent: webhook and capture may both arrive
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}
		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})
	if err != nil {
		common.SysError(common.LogText("paypal topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	if quotaToAdd > 0 {
		syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("paypal topup"))
		RecordTopupLog(topUp.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}",
			map[string]any{"provider": "PayPal", "quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}),
			callerIp, topUp.PaymentMethod, PaymentMethodPayPal)
	}
	return nil
}
