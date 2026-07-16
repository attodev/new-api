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
	walletAutoRechargeTickInterval      = 1 * time.Minute
	walletAutoRechargePendingAuthMaxAge = 50 * time.Minute
	walletAutoRechargeBatchSize         = 8
	walletAutoRechargeWorkerCount       = 8
	walletAutoRechargeMaxFails          = 3
)

var (
	walletAutoRechargeOnce    sync.Once
	walletAutoRechargeRunning atomic.Bool
)

// StartWalletAutoRechargeTask periodically processes scheduled and threshold wallet auto recharges.
func StartWalletAutoRechargeTask() {
	walletAutoRechargeOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("wallet auto recharge task started: tick=%s", walletAutoRechargeTickInterval))
			ticker := time.NewTicker(walletAutoRechargeTickInterval)
			defer ticker.Stop()
			runPaymentTaskIteration(runWalletAutoRechargeOnce)
			for range ticker.C {
				runPaymentTaskIteration(runWalletAutoRechargeOnce)
			}
		})
	})
}

func runWalletAutoRechargeOnce() {
	if !walletAutoRechargeRunning.CompareAndSwap(false, true) {
		return
	}
	defer walletAutoRechargeRunning.Store(false)

	ctx := context.Background()
	// Due times, provider claims, and retry cutoffs are persisted using database
	// timestamps. Use the same clock here so skewed application nodes cannot
	// charge a schedule early or delay another node's claim expiry.
	// Use UTC as the worker-facing location as well as a database-sourced
	// instant. The model keeps the legacy order-identity bridge separate, while
	// threshold daily limits must never depend on a node's process timezone.
	now := time.Unix(model.GetDBTimestamp(), 0).UTC()
	batchLimit := paymentBatchWorkerCount(walletAutoRechargeBatchSize)
	processed := make(map[int]struct{})

	staleAuthCutoff := now.Add(-walletAutoRechargePendingAuthMaxAge).Unix()
	if expired, err := model.ExpireStaleUnattemptedWalletAutoRecharges(staleAuthCutoff); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: expire abandoned billing auth failed: %v", err))
	} else if expired > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("wallet auto recharge: expired %d abandoned billing auth reservation(s)", expired))
	}

	pending, err := model.GetPendingWalletAutoRechargeSettlements(batchLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query pending settlements failed: %v", err))
		return
	}
	for i := range pending {
		processed[pending[i].Id] = struct{}{}
	}
	runBoundedPaymentBatch(pending, paymentBatchWorkerCount(walletAutoRechargeWorkerCount), func(policy model.WalletAutoRecharge) {
		chargeWalletAutoRecharge(ctx, &policy, now)
	})
	if advanced, err := model.AdvanceStaleScheduledWalletAutoRecharges(now.Unix(), batchLimit); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: advance schedules beyond operational grace failed: %v", err))
	} else if advanced > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: skipped %d stale scheduled charge(s) after extended outage", advanced))
	}

	// Disabling automatic billing blocks only new remote charges. Orders that
	// may already be paid were settled above. Billing-key cleanup must still run
	// while disabled because it cannot create a new charge.
	if !isWalletAutoRechargeTaskRunnable() {
		reconcileWalletAutoRechargeBillingIssues(ctx, batchLimit)
		return
	}

	scheduled, err := model.GetDueScheduledWalletAutoRechargesExcluding(
		now.Unix(),
		batchLimit,
		walletAutoRechargeProcessedIDs(processed),
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query scheduled failed: %v", err))
	} else {
		scheduledBatch := make([]model.WalletAutoRecharge, 0, len(scheduled))
		for i := range scheduled {
			if _, ok := processed[scheduled[i].Id]; ok {
				continue
			}
			processed[scheduled[i].Id] = struct{}{}
			scheduledBatch = append(scheduledBatch, scheduled[i])
		}
		runBoundedPaymentBatch(scheduledBatch, paymentBatchWorkerCount(walletAutoRechargeWorkerCount), func(policy model.WalletAutoRecharge) {
			chargeWalletAutoRecharge(ctx, &policy, now)
		})
	}

	threshold, err := model.GetActiveThresholdWalletAutoRechargesExcluding(
		batchLimit,
		walletAutoRechargeProcessedIDs(processed),
	)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query threshold failed: %v", err))
		return
	}
	thresholdBatch := make([]model.WalletAutoRecharge, 0, len(threshold))
	for i := range threshold {
		if _, ok := processed[threshold[i].Id]; ok {
			continue
		}
		processed[threshold[i].Id] = struct{}{}
		thresholdBatch = append(thresholdBatch, threshold[i])
	}
	runBoundedPaymentBatch(thresholdBatch, paymentBatchWorkerCount(walletAutoRechargeWorkerCount), func(policy model.WalletAutoRecharge) {
		chargeWalletAutoRecharge(ctx, &policy, now)
	})

	// Keep uncertain billing-key issue cleanup in a separate tail queue so a
	// slow DELETE/issue recovery cannot head-of-line block due wallet charges.
	reconcileWalletAutoRechargeBillingIssues(ctx, batchLimit)
}

func reconcileWalletAutoRechargeBillingIssues(ctx context.Context, batchLimit int) {
	if reconciled, err := model.ReconcileStaleWalletAutoRechargeBillingIssues(ctx, batchLimit); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: reconcile billing-key issues failed: %v", err))
	} else if reconciled > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("wallet auto recharge: reconciled %d billing-key issue(s)", reconciled))
	}
}

func walletAutoRechargeProcessedIDs(processed map[int]struct{}) []int {
	if len(processed) == 0 {
		return nil
	}
	ids := make([]int, 0, len(processed))
	for id := range processed {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func isWalletAutoRechargeTaskRunnable() bool {
	snapshot := setting.GetTossConfigSnapshot()
	clientKey := snapshot.BillingClientKey
	secretKey := snapshot.BillingSecretKey
	if snapshot.TestMode {
		clientKey = snapshot.BillingTestClientKey
		secretKey = snapshot.BillingTestSecretKey
	}
	return operation_setting.IsPaymentComplianceConfirmed() &&
		snapshot.BillingEnabled &&
		snapshot.WalletAutoRechargeEnabled &&
		snapshot.UnitPrice > 0 &&
		model.IsTossBillingCryptoConfigurationSafe() &&
		strings.TrimSpace(clientKey) != "" &&
		strings.TrimSpace(secretKey) != ""
}

func chargeWalletAutoRecharge(ctx context.Context, policy *model.WalletAutoRecharge, now time.Time) {
	// Delegates to model.ProcessWalletAutoRechargeWithConfiguredCharger so the
	// service layer stays unaware of the Toss charger wiring. Refresh the DB
	// clock per callback because the bounded pending/scheduled queues can cross
	// an hour after the iteration-level timestamp was captured. The model still
	// performs the transactional hour guard that closes the final boundary race.
	now = time.Unix(model.GetDBTimestamp(), 0).UTC()
	if err := model.ProcessWalletAutoRechargeWithConfiguredCharger(ctx, policy.Id, now, walletAutoRechargeMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: charge failed policy=%d: %v", policy.Id, err))
	}
}
