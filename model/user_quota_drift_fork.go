package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — 60초 재읽기 때 캐시·DB 잔액을 비교한다. 이 서버에 대기분이 있으면
// 건너뛰고, 캐시 잔액과 차이가 두 관찰에서 그대로이고 0이 아닐 때만 로그를 남긴다.
// 두 관찰은 SYNC_FREQUENCY 이상 떨어져야 한다. 그보다 가까운 관찰은 앞 관찰을
// 그대로 두고 무시한다. 다른 노드의 대기분은 그 간격 안에 반영되어 차이를 바꾸므로
// 걸리지 않는다(설계 §4.7). 값을 고치지는 않는다.

type userQuotaDriftObservation struct {
	cacheQuota int
	diff       int
	at         int64 // unix seconds of the observation
}

var (
	userQuotaDriftMu     sync.Mutex
	userQuotaDriftMemory = map[int]userQuotaDriftObservation{}
)

// userQuotaDriftNow is the observation clock (unix seconds); a var so tests can advance it.
var userQuotaDriftNow = common.GetTimestamp

// userQuotaDriftReporter is a var so tests can capture reports.
var userQuotaDriftReporter = func(userID, cacheQuota, dbQuota, diff int) {
	common.SysError(common.LogText("user quota cache drift: user=%d cache=%d db=%d diff=%d", userID, cacheQuota, dbQuota, diff))
}

func resetUserQuotaDriftMemory() {
	userQuotaDriftMu.Lock()
	userQuotaDriftMemory = map[int]userQuotaDriftObservation{}
	userQuotaDriftMu.Unlock()
}

func observeUserQuotaDrift(userID int, cacheQuota int, dbQuota int) {
	if userQuotaBatchPending(userID) {
		return
	}
	diff := cacheQuota - dbQuota
	userQuotaDriftMu.Lock()
	defer userQuotaDriftMu.Unlock()
	if diff == 0 {
		delete(userQuotaDriftMemory, userID)
		return
	}
	now := userQuotaDriftNow()
	prev, seen := userQuotaDriftMemory[userID]
	if seen && now-prev.at < int64(userCacheTTLSeconds()) {
		// Too close to the remembered observation (e.g. concurrent re-reads at the
		// FreshUntil boundary): another node's delta may still be pending. Keep it.
		return
	}
	userQuotaDriftMemory[userID] = userQuotaDriftObservation{cacheQuota: cacheQuota, diff: diff, at: now}
	if seen && prev.cacheQuota == cacheQuota && prev.diff == diff {
		userQuotaDriftReporter(userID, cacheQuota, dbQuota, diff)
	}
}
