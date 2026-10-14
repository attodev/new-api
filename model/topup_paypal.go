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
// charged by PayPal: amount × PayPalUnitPrice × topup group ratio × amount
// discount (getPayPalPayMoney), and the credit is quota = Money × QuotaPerUnit.
// This differs from Stripe, whose Money is amount × group ratio, so there the unit
// price and discount change only the charge; here they scale both the charge and
// the credited balance. Kept deliberately; Phase E (O11) decides whether to align.
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
		return creditTopUpTarget(tx, topUp, quotaToAdd, nil) // fork: O3
	})
	if err != nil {
		common.SysError(common.LogText("paypal topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	if quotaToAdd > 0 {
		syncCreditTopUpCache(topUp, quotaToAdd, common.LogText("paypal topup")) // fork: O3
		RecordTopupLog(topUp.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}",
			map[string]any{"provider": "PayPal", "quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}),
			callerIp, topUp.PaymentMethod, PaymentMethodPayPal)
	}
	return nil
}
