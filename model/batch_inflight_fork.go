package model

// fork: O12 — 배치가 큐를 꺼내 DB에 쓰는 동안(운영 기준 3~5초) 그 사용자는
// 여전히 "DB에 반영되지 않은 차감"을 가진다. 큐를 꺼내는 순간 같은 락 안에서
// 사용자를 반영 중으로 표시하고, 행을 쓴 직후 한 사용자씩 푼다.
// 설계 §4.11. userQuotaInFlight는 batchUpdateLocks[BatchUpdateTypeUserQuota]로 보호한다.

var userQuotaInFlight = map[int]struct{}{}

// takeBatchStoreLocked returns the store for type i; the caller holds batchUpdateLocks[i].
// For the user quota type it marks every queued user as in flight under that same lock,
// so no reader can observe the user as neither queued nor in flight.
func takeBatchStoreLocked(i int) map[int]int {
	store := batchUpdateStores[i]
	if i == BatchUpdateTypeUserQuota {
		for id := range store {
			userQuotaInFlight[id] = struct{}{}
		}
	}
	return store
}

func clearUserQuotaInFlight(id int) {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	delete(userQuotaInFlight, id)
}

// userQuotaBatchPending reports whether this process still owes the database a
// quota delta for the user: queued for the next batch, or being written now.
func userQuotaBatchPending(id int) bool {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	_, queued := batchUpdateStores[BatchUpdateTypeUserQuota][id]
	_, inFlight := userQuotaInFlight[id]
	return queued || inFlight
}

func flushUserQuotaRow(id int, quota int, usedQuota int, requestCount int) {
	defer clearUserQuotaInFlight(id)
	updateUserQuotaUsedQuotaAndRequestCount(id, quota, usedQuota, requestCount)
}
