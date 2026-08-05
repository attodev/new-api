package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet                   = "wallet"
	BillingSourceSubscription             = "subscription"
	BillingSourceOrganizationSubscription = "organization_subscription"
)

// PreConsumeBilling BillingSession
// relayInfo.Billing Settle / Refund
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling —
// ---------------------------------------------------------------------------

// SettleBilling RelayInfo BillingSession session
// PostConsumeQuota
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("post-charge after pre-charge: %s (actual: %s, pre-charged: %s)",
				logger.FormatQuota(int64(delta)),
				logger.FormatQuota(int64(actualQuota)),
				logger.FormatQuota(int64(preConsumed)),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("refund after pre-charge: %s (actual: %s, pre-charged: %s)",
				logger.FormatQuota(-int64(delta)),
				logger.FormatQuota(int64(actualQuota)),
				logger.FormatQuota(int64(preConsumed)),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("pre-charge matches actual consumption, no adjustment needed: %s (per-call billing)",
				logger.FormatQuota(int64(actualQuota)),
			))
		}

		if err := relayInfo.Billing.Settle(actualQuota); err != nil {
			return err
		}

		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription || relayInfo.BillingSource == BillingSourceOrganizationSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// BillingSession
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		return PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true)
	}
	return nil
}
