package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	tossBillingTickInterval = 1 * time.Minute
	tossBillingBatchSize    = 100
)

var (
	tossBillingOnce    sync.Once
	tossBillingRunning atomic.Bool
)

// StartTossBillingTask periodically charges due Toss auto-renew subscriptions.
func StartTossBillingTask() {
	tossBillingOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("toss billing task started: tick=%s", tossBillingTickInterval))
			ticker := time.NewTicker(tossBillingTickInterval)
			defer ticker.Stop()
			runTossBillingOnce()
			for range ticker.C {
				runTossBillingOnce()
			}
		})
	})
}

func runTossBillingOnce() {
	if !tossBillingRunning.CompareAndSwap(false, true) {
		return
	}
	defer tossBillingRunning.Store(false)
	ctx := context.Background()
	if !isTossBillingRemoteCleanupRunnable() {
		expired, err := model.ExpireDueSubscriptionsIncludingTossAutoRenewLocalOnly(tossBillingBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("toss billing: expire overdue subscriptions locally while remote cleanup unavailable failed: %v", err))
			return
		}
		if expired > 0 {
			logger.LogWarn(ctx, fmt.Sprintf("toss billing: expired %d overdue subscription(s) locally while remote cleanup unavailable", expired))
		}
		return
	}
	if _, err := model.RetryPendingTossBillingKeyRevocations(ctx, tossBillingBatchSize); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: retry pending billing-key revocations failed: %v", err))
	}
	if !isTossBillingTaskRunnable() {
		expired, err := model.ExpireDueSubscriptionsIncludingTossAutoRenew(tossBillingBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("toss billing: expire overdue subscriptions while billing unavailable failed: %v", err))
			return
		}
		if expired > 0 {
			logger.LogWarn(ctx, fmt.Sprintf("toss billing: expired %d overdue subscription(s) while billing unavailable", expired))
		}
		return
	}
	now := time.Now().Unix()
	subs, err := model.GetDueTossRenewals(now, tossBillingBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: query due renewals failed: %v", err))
		return
	}
	for i := range subs {
		chargeTossRenewal(ctx, &subs[i])
	}
}

func isTossBillingTaskRunnable() bool {
	clientKey, secretKey := setting.TossExplicitActiveBillingKeyPair()
	return operation_setting.IsPaymentComplianceConfirmed() &&
		setting.TossBillingEnabled &&
		setting.TossUnitPrice > 0 &&
		strings.TrimSpace(clientKey) != "" &&
		strings.TrimSpace(secretKey) != ""
}

func isTossBillingRemoteCleanupRunnable() bool {
	return operation_setting.IsPaymentComplianceConfirmed()
}

func chargeTossRenewal(ctx context.Context, sub *model.UserSubscription) {
	// Delegates to model.ProcessTossRenewal which uses the injected charger
	// (set by controller init()) to avoid a service→controller import cycle.
	if err := model.ProcessTossRenewal(ctx, sub.Id, model.TossBillingMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: renewal failed sub=%d: %v", sub.Id, err))
	}
}
