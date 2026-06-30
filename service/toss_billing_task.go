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
	tossBillingTickInterval = 1 * time.Minute
	tossBillingBatchSize    = 100
	tossBillingMaxFails     = 3
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

func chargeTossRenewal(ctx context.Context, sub *model.UserSubscription) {
	// Delegates to model.ProcessTossRenewal which uses the injected charger
	// (set by controller init()) to avoid a service→controller import cycle.
	if err := model.ProcessTossRenewal(ctx, sub.Id, tossBillingMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("toss billing: renewal failed sub=%d: %v", sub.Id, err))
	}
}
