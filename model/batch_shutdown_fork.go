package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — 정상 종료 때 배치 큐를 DB에 쓴다. upstream은 큐를 버려 재배포마다
// 최대 BATCH_UPDATE_INTERVAL 분량의 차감이 DB에서 사라졌다(설계 §4.12-1).

const flushBatchUpdatesMaxPasses = 3

// batchRunMu serializes batchUpdate runs, so a shutdown flush waits for a ticker
// batch that is already writing instead of seeing its (swapped-out) empty stores.
var batchRunMu sync.Mutex

// lockBatchRun locks batchRunMu and returns the unlock, for `defer lockBatchRun()()`.
func lockBatchRun() func() {
	batchRunMu.Lock()
	return batchRunMu.Unlock
}

// batchStoresEmpty reports whether every batch store is empty, each under its lock.
func batchStoresEmpty() bool {
	for i := range BatchUpdateTypeCount {
		batchUpdateLocks[i].Lock()
		n := len(batchUpdateStores[i])
		batchUpdateLocks[i].Unlock()
		if n > 0 {
			return false
		}
	}
	return true
}

// FlushBatchUpdates writes the queued deltas. batchUpdate waits for any batch that is
// already running; rows re-queued by a failed write or enqueued meanwhile (e.g. by
// post-response goroutines) get further passes, up to flushBatchUpdatesMaxPasses.
// Afterwards it runs one cache repair pass (O12 §4.12-3), also when batch mode is off,
// because the repair list is in-process and would otherwise be lost on shutdown.
func FlushBatchUpdates() {
	if common.BatchUpdateEnabled {
		flushBatchQueue()
	}
	if common.RedisEnabled {
		repairUserCaches()
	}
}

func flushBatchQueue() {
	common.SysLog(common.LogText("flushing batch updates before shutdown"))
	passes := 0
	for passes < flushBatchUpdatesMaxPasses {
		batchUpdate()
		passes++
		if batchStoresEmpty() {
			break
		}
	}
	common.SysLog(common.LogText("flushed batch updates in %d pass(es), queue empty: %v", passes, batchStoresEmpty()))
}
