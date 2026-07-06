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
	walletAutoRechargeTickInterval = 1 * time.Minute
	walletAutoRechargeBatchSize    = 100
	walletAutoRechargeMaxFails     = 3
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
			runWalletAutoRechargeOnce()
			for range ticker.C {
				runWalletAutoRechargeOnce()
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
	now := time.Now()

	scheduled, err := model.GetDueScheduledWalletAutoRecharges(now.Unix(), walletAutoRechargeBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query scheduled failed: %v", err))
	} else {
		for i := range scheduled {
			chargeWalletAutoRecharge(ctx, &scheduled[i], now)
		}
	}

	threshold, err := model.GetActiveThresholdWalletAutoRecharges(walletAutoRechargeBatchSize)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: query threshold failed: %v", err))
		return
	}
	for i := range threshold {
		chargeWalletAutoRecharge(ctx, &threshold[i], now)
	}
}

func chargeWalletAutoRecharge(ctx context.Context, policy *model.WalletAutoRecharge, now time.Time) {
	// Delegates to model.ProcessWalletAutoRechargeWithConfiguredCharger so the
	// service layer stays unaware of the Toss charger wiring.
	if err := model.ProcessWalletAutoRechargeWithConfiguredCharger(ctx, policy.Id, now, walletAutoRechargeMaxFails); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge: charge failed policy=%d: %v", policy.Id, err))
	}
}
