package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — 60초 재읽기 때 캐시·DB 잔액을 비교한다. 이 서버에 대기분이 있으면
// 건너뛰고, 캐시 잔액과 차이가 두 관찰에서 그대로이고 0이 아닐 때만 로그를 남긴다.
// 다른 노드의 대기분은 두 관찰 사이에 반영되어 차이를 바꾸므로 걸리지 않는다
// (설계 §4.7). 값을 고치지는 않는다.

type userQuotaDriftObservation struct {
	cacheQuota int
	diff       int
}

var (
	userQuotaDriftMu     sync.Mutex
	userQuotaDriftMemory = map[int]userQuotaDriftObservation{}
)

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
	prev, seen := userQuotaDriftMemory[userID]
	userQuotaDriftMemory[userID] = userQuotaDriftObservation{cacheQuota: cacheQuota, diff: diff}
	if seen && prev.cacheQuota == cacheQuota && prev.diff == diff {
		userQuotaDriftReporter(userID, cacheQuota, dbQuota, diff)
	}
}
