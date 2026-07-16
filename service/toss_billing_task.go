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
	tossBillingTickInterval           = 1 * time.Minute
	tossBillingBatchSize              = 8
	tossBillingWorkerCount            = 8
	tossBillingOperationalGracePeriod = time.Duration(model.TossBillingOperationalGraceSeconds) * time.Second
)

var (
	tossBillingOnce              sync.Once
	tossBillingCryptoWarningOnce sync.Once
	tossBillingRunning           atomic.Bool
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
			runPaymentTaskIteration(runTossBillingOnce)
			for range ticker.C {
				runPaymentTaskIteration(runTossBillingOnce)
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
	batchLimit := paymentBatchWorkerCount(tossBillingBatchSize)
	if err := model.ValidateTossBillingCryptoConfiguration(); err != nil {
		tossBillingCryptoWarningOnce.Do(func() {
			logger.LogWarn(ctx, fmt.Sprintf("toss billing disabled: %v", err))
		})
	}
	if !isTossBillingRemoteCleanupRunnable() {
		expireSubscriptionsWhileTossBillingUnavailable(ctx, false)
		return
	}
	if _, err := model.RetryPendingTossBillingKeyRevocations(ctx, batchLimit); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: retry pending billing-key revocations failed: %v", err))
	}
	if !isTossBillingTaskRunnable() {
		expireSubscriptionsWhileTossBillingUnavailable(ctx, false)
		return
	}
	if !expireStaleTossRenewalsBeforeCharging(ctx, batchLimit) {
		return
	}
	// Subscription due/claim timestamps are DB-clock based. Comparing them to a
	// skewed application-node clock could charge a renewal before it is due.
	now := model.GetDBTimestamp()
	subs, err := model.GetDueTossRenewals(now, batchLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: query due renewals failed: %v", err))
		return
	}
	runBoundedPaymentBatch(subs, paymentBatchWorkerCount(tossBillingWorkerCount), func(sub model.UserSubscription) {
		chargeTossRenewal(ctx, &sub)
	})
}

func expireStaleTossRenewalsBeforeCharging(ctx context.Context, batchLimit int) bool {
	expired, err := model.ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(
		batchLimit,
		int64(tossBillingOperationalGracePeriod/time.Second),
		false,
	)
	if err != nil {
		// Fail closed: if stale rows cannot be separated from currently payable
		// renewals, do not risk charging a very old subscription after downtime.
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: expire stale renewals before charging failed: %v", err))
		return false
	}
	if expired > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: expired %d stale subscription(s) before resuming charges", expired))
		// Due selection and the final pre-POST gate independently exclude every
		// row outside grace. Do not let a large stale backlog block recent valid
		// renewals until they too cross the cutoff.
	}
	return true
}

func expireSubscriptionsWhileTossBillingUnavailable(ctx context.Context, revokeRemote bool) {
	batchLimit := paymentBatchWorkerCount(tossBillingBatchSize)
	expired, err := model.ExpireDueSubscriptions(batchLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: expire non-renewing subscriptions while billing unavailable failed: %v", err))
		return
	}
	if expired > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss billing: expired %d non-renewing subscription(s) while billing unavailable", expired))
	}

	stale, err := model.ExpireDueSubscriptionsIncludingTossAutoRenewAfterGrace(
		batchLimit,
		int64(tossBillingOperationalGracePeriod/time.Second),
		revokeRemote,
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: expire auto-renew subscriptions beyond operational grace failed: %v", err))
		return
	}
	if stale > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: expired %d subscription(s) beyond %s operational grace", stale, tossBillingOperationalGracePeriod))
	}
}

func isTossBillingTaskRunnable() bool {
	tossConfig := setting.GetTossConfigSnapshot()
	clientKey, secretKey := tossConfig.BillingClientKey, tossConfig.BillingSecretKey
	if tossConfig.TestMode {
		clientKey, secretKey = tossConfig.BillingTestClientKey, tossConfig.BillingTestSecretKey
	}
	return operation_setting.IsPaymentComplianceConfirmed() &&
		model.IsTossBillingCryptoConfigurationSafe() &&
		tossConfig.BillingEnabled &&
		tossConfig.UnitPrice > 0 &&
		strings.TrimSpace(clientKey) != "" &&
		strings.TrimSpace(secretKey) != ""
}

func isTossBillingRemoteCleanupRunnable() bool {
	// Deleting an already-disabled provider credential is a safety cleanup, not
	// a new payment operation. Keep revocation retries running even if payment
	// compliance or billing enablement is later switched off.
	return model.IsTossBillingCryptoConfigurationSafe()
}

func chargeTossRenewal(ctx context.Context, sub *model.UserSubscription) {
	// Delegates to model.ProcessTossRenewal which uses the injected charger
	// (set by controller init()) to avoid a service→controller import cycle.
	if err := model.ProcessTossRenewal(ctx, sub.Id, model.TossBillingMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: renewal failed sub=%d: %v", sub.Id, err))
	}
}
