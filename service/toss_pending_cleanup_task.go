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
	tossPendingCleanupTickInterval = 10 * time.Minute
	tossPendingMaxAge              = 35 * time.Minute // Toss payment window (~30m) + buffer
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
			logger.LogInfo(context.Background(), fmt.Sprintf("toss pending cleanup task started: tick=%s maxAge=%s", tossPendingCleanupTickInterval, tossPendingMaxAge))
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
	cutoff := time.Now().Add(-tossPendingMaxAge).Unix()
	n, err := model.ExpireStaleTossPendingTopUps(cutoff)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("toss pending cleanup task failed: %v", err))
		return
	}
	if n > 0 {
		logger.LogInfo(context.Background(), fmt.Sprintf("toss pending cleanup: expired %d stale pending order(s)", n))
	}
}
