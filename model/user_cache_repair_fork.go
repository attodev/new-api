package model

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

// fork: O12 — 커밋 뒤 캐시 반영에 실패했거나 Redis 오류로 DB에서만 차감한 사용자는
// 해시가 실제보다 큰 잔액을 들고 있을 수 있다. 대기분이 없을 때 해시를 지워
// 다음 요청이 DB에서 새로 읽게 한다. 값을 고치지는 않는다(설계 §4.12-3).

var (
	userCacheRepairMu  sync.Mutex
	userCacheRepairIDs = map[int]struct{}{}
)

// noteUserCacheSyncFailure lists the user for repair when err is non-nil and returns err unchanged.
func noteUserCacheSyncFailure(userID int, err error) error {
	if err == nil || !common.RedisEnabled || userID <= 0 {
		return err
	}
	userCacheRepairMu.Lock()
	userCacheRepairIDs[userID] = struct{}{}
	userCacheRepairMu.Unlock()
	return err
}

// takeUserCacheRepairIDs atomically snapshots and clears the repair list, so a
// failure noted while a pass is running lands in the fresh map and is never lost.
func takeUserCacheRepairIDs() []int {
	userCacheRepairMu.Lock()
	defer userCacheRepairMu.Unlock()
	ids := make([]int, 0, len(userCacheRepairIDs))
	for id := range userCacheRepairIDs {
		ids = append(ids, id)
	}
	userCacheRepairIDs = map[int]struct{}{}
	return ids
}

func requeueUserCacheRepair(userID int) {
	userCacheRepairMu.Lock()
	userCacheRepairIDs[userID] = struct{}{}
	userCacheRepairMu.Unlock()
}

// repairUserCaches deletes the hash of each listed user with no pending batch delta.
// Users still pending, or whose delete fails, are put back for the next pass.
func repairUserCaches() {
	repairUserCachesFor(takeUserCacheRepairIDs())
}

func repairUserCachesFor(ids []int) {
	for _, id := range ids {
		if userQuotaBatchPending(id) {
			requeueUserCacheRepair(id)
			continue
		}
		if err := invalidateUserCache(id); err != nil {
			requeueUserCacheRepair(id)
			common.SysLog(common.LogText("user cache repair for %d deferred: %s", id, err.Error()))
		}
	}
}

// StartUserCacheSyncRepair runs repairUserCaches periodically; independent of batch mode.
func StartUserCacheSyncRepair() {
	if !common.RedisEnabled {
		return
	}
	interval := time.Duration(max(common.BatchUpdateInterval, 1)) * time.Second
	gopool.Go(func() {
		for {
			time.Sleep(interval)
			repairUserCaches()
		}
	})
}
