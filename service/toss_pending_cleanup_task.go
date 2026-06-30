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
	tossPendingCleanupTickInterval   = 10 * time.Minute
	tossNeverRecordedPendingMaxAge   = 50 * time.Minute // past worst-case legit flow (~30m window + ~10m approval)
	tossRecordedPendingMaxAge        = 15 * time.Minute // successUrl reached; Toss approval must complete within ~10m
	tossPendingCleanupReconcileLimit = 100
	tossPendingSubscriptionMaxAge    = 50 * time.Minute
)

var (
	tossPendingCleanupOnce    sync.Once
	tossPendingCleanupRunning atomic.Bool
)

// StartTossPendingCleanupTask periodically expires stale Toss pending top-up orders.
func StartTossPendingCleanupTask() {
	tossPendingCleanupOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("toss pending cleanup task started: tick=%s never_recorded_max_age=%s recorded_max_age=%s subscription_max_age=%s", tossPendingCleanupTickInterval, tossNeverRecordedPendingMaxAge, tossRecordedPendingMaxAge, tossPendingSubscriptionMaxAge))
			ticker := time.NewTicker(tossPendingCleanupTickInterval)
			defer ticker.Stop()
			runTossPendingCleanupOnce()
			for range ticker.C {
				runTossPendingCleanupOnce()
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
	now := time.Now()
	neverRecordedCutoff := now.Add(-tossNeverRecordedPendingMaxAge).Unix()
	n, err := model.ExpireStaleTossPendingTopUps(neverRecordedCutoff)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: expire never-recorded top-ups failed: %v", err))
	} else if n > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: expired %d stale never-recorded top-up order(s)", n))
	}

	recordedCutoff := now.Add(-tossRecordedPendingMaxAge).Unix()
	reconciled, err := model.ReconcileStaleTossRecordedTopUps(ctx, recordedCutoff, tossPendingCleanupReconcileLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: reconcile recorded top-ups had errors: %v", err))
	}
	if reconciled > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: reconciled %d stale recorded top-up order(s)", reconciled))
	}

	subscriptionCutoff := now.Add(-tossPendingSubscriptionMaxAge).Unix()
	reconciledSubscriptions, err := model.ReconcileStaleTossPendingSubscriptionOrders(ctx, subscriptionCutoff, tossPendingCleanupReconcileLimit)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: reconcile subscription orders had errors: %v", err))
	}
	if reconciledSubscriptions > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: reconciled %d stale subscription order(s)", reconciledSubscriptions))
	}

	subscriptions, err := model.ExpireStaleTossPendingSubscriptionOrders(subscriptionCutoff)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss pending cleanup: expire subscription orders failed: %v", err))
	} else if subscriptions > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("toss pending cleanup: expired %d stale subscription order(s)", subscriptions))
	}
}
