package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// A paymentKey can only be confirmed for about ten minutes. A short tick
	// keeps scheduler jitter small after the two-minute crashed-owner lease.
	// The atomic iteration guard prevents overlapping scans when a provider is
	// slow; a buffered ticker wake-up resumes draining immediately afterwards.
	tossPendingTopUpRecoveryTickInterval = 15 * time.Second
	tossPendingCleanupTickInterval       = time.Minute
	tossNeverRecordedPendingMaxAge       = 50 * time.Minute // past worst-case legit flow (~30m window + ~10m approval)
	// A recorded paymentKey can be approved for only about ten minutes. Start
	// recovery early; the model-level lease waits two minutes after an active
	// callback and fairly schedules unresolved rows without reusing this cutoff.
	tossRecordedPendingRecoveryAge = 15 * time.Second
	// Reserve only one worker-sized batch. A larger in-memory queue could wait
	// past its durable retry lease before its provider call starts; the buffered
	// 15-second ticker immediately selects the next batch after a slow run.
	tossPendingCleanupReconcileLimit       = model.TossTopUpRecoveryWorkerCount
	tossPendingCleanupSQLiteReconcileLimit = model.TossTopUpRecoverySQLiteWorkerCount
	tossPendingSubscriptionReconcileLimit  = 8
	tossPendingSubscriptionReconcileAge    = 5 * time.Minute // billing issue/charge claims expire after 5m
	tossPendingSubscriptionMaxAge          = 50 * time.Minute
)

var (
	tossPendingCleanupOnce    sync.Once
	tossPendingCleanupRunning atomic.Bool
	tossTopUpRecoveryRunning  atomic.Bool
)

// StartTossPendingCleanupTask periodically expires stale Toss pending top-up orders.
func StartTossPendingCleanupTask() {
	tossPendingCleanupOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("toss pending cleanup task started: cleanup_tick=%s never_recorded_max_age=%s subscription_reconcile_age=%s subscription_max_age=%s", tossPendingCleanupTickInterval, tossNeverRecordedPendingMaxAge, tossPendingSubscriptionReconcileAge, tossPendingSubscriptionMaxAge))
			cleanupTicker := time.NewTicker(tossPendingCleanupTickInterval)
			defer cleanupTicker.Stop()
			runPaymentTaskIteration(runTossPendingCleanupOnce)
			for range cleanupTicker.C {
				runPaymentTaskIteration(runTossPendingCleanupOnce)
			}
		})
		// General-payment confirmation has a hard ten-minute provider window.
		// Keep its queue independent from billing-issue/charge cleanup, whose two
		// sequential provider operations can legitimately take much longer than a
		// single tick. Separate guards prevent either slow queue from suppressing
		// the other while exact DB leases/idempotency serialize each payment row.
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("toss recorded top-up recovery task started: tick=%s recovery_age=%s", tossPendingTopUpRecoveryTickInterval, tossRecordedPendingRecoveryAge))
			recoveryTicker := time.NewTicker(tossPendingTopUpRecoveryTickInterval)
			defer recoveryTicker.Stop()
			runPaymentTaskIteration(runTossRecordedTopUpRecoveryOnce)
			for range recoveryTicker.C {
				runPaymentTaskIteration(runTossRecordedTopUpRecoveryOnce)
			}
		})
	})
}

func runTossPendingCleanupOnce() {
	if !tossPendingCleanupRunning.CompareAndSwap(false, true) {
		return
	}
	defer tossPendingCleanupRunning.Store(false)
	ctx := context.Background()
	// Claims and provider timestamps are written using the database clock. Use
	// that same clock for every cutoff so an API node with a skewed wall clock
	// cannot expire a live checkout or reclaim an active provider attempt early.
	now := model.GetDBTimestamp()
	neverRecordedCutoff := now - int64(tossNeverRecordedPendingMaxAge/time.Second)
	n, err := model.ExpireStaleTossPendingTopUps(neverRecordedCutoff)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: expire never-recorded top-ups failed: %v", err))
	} else if n > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: expired %d stale never-recorded top-up order(s)", n))
	}

	// Recover encrypted one-time billing authorizations as soon as their DB
	// claim can be reclaimed. Waiting until the general 50-minute expiry window
	// would unnecessarily delay same-idempotency recovery of an issue response.
	subscriptionReconcileCutoff := now - int64(tossPendingSubscriptionReconcileAge/time.Second)
	reconcileLimit := paymentBatchWorkerCount(tossPendingSubscriptionReconcileLimit)
	reconciledSubscriptions, err := model.ReconcileStaleTossPendingSubscriptionOrders(ctx, subscriptionReconcileCutoff, reconcileLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: reconcile subscription orders had errors: %v", err))
	}
	if reconciledSubscriptions > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: reconciled %d stale subscription order(s)", reconciledSubscriptions))
	}

	subscriptionExpiryCutoff := now - int64(tossPendingSubscriptionMaxAge/time.Second)
	subscriptions, err := model.ExpireStaleTossPendingSubscriptionOrders(subscriptionExpiryCutoff)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: expire subscription orders failed: %v", err))
	} else if subscriptions > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: expired %d stale subscription order(s)", subscriptions))
	}
}

func runTossRecordedTopUpRecoveryOnce() {
	if !tossTopUpRecoveryRunning.CompareAndSwap(false, true) {
		return
	}
	defer tossTopUpRecoveryRunning.Store(false)
	reconcileTossRecordedTopUps(context.Background(), model.GetDBTimestamp())
}

func reconcileTossRecordedTopUps(ctx context.Context, now int64) {
	reconcileLimit := tossPendingCleanupReconcileLimit
	if common.UsingSQLite {
		reconcileLimit = tossPendingCleanupSQLiteReconcileLimit
	}
	recordedCutoff := now - int64(tossRecordedPendingRecoveryAge/time.Second)
	reconciled, err := model.ReconcileStaleTossRecordedTopUps(ctx, recordedCutoff, reconcileLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: reconcile recorded top-ups had errors: %v", err))
	}
	if reconciled > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: reconciled %d stale recorded top-up order(s)", reconciled))
	}
}
