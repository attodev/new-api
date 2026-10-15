package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O12 — 배치가 큐를 꺼내 DB에 쓰는 동안(운영 기준 3~5초) 그 사용자는
// 여전히 "DB에 반영되지 않은 차감"을 가진다. 큐를 꺼내는 순간 같은 락 안에서
// 사용자를 반영 중으로 표시하고, 행을 쓴 직후 한 사용자씩 푼다.
// 설계 §4.11. userQuotaInFlight는 batchUpdateLocks[BatchUpdateTypeUserQuota]로 보호한다.

// userQuotaInFlight is a refcount: overlapping batchUpdate callers (serialized by
// batchRunMu since Task 6; the refcount remains as a safety net) may hold the same
// user, and one batch's clear must not erase the other's mark.
var userQuotaInFlight = map[int]int{}

// takeBatchStoreLocked returns the store for type i; the caller holds batchUpdateLocks[i].
// For the user quota type it marks every queued user as in flight under that same lock,
// so no reader can observe the user as neither queued nor in flight.
func takeBatchStoreLocked(i int) map[int]int {
	store := batchUpdateStores[i]
	if i == BatchUpdateTypeUserQuota {
		for id := range store {
			userQuotaInFlight[id]++
		}
	}
	return store
}

func clearUserQuotaInFlight(id int) {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	if userQuotaInFlight[id] <= 1 {
		delete(userQuotaInFlight, id)
		return
	}
	userQuotaInFlight[id]--
}

// userQuotaBatchPending reports whether this process still owes the database a
// quota delta for the user: queued for the next batch, or being written now.
func userQuotaBatchPending(id int) bool {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	_, queued := batchUpdateStores[BatchUpdateTypeUserQuota][id]
	return queued || userQuotaInFlight[id] > 0
}

// flushUserQuotaRow is upstream updateUserQuotaUsedQuotaAndRequestCount with the
// error returned: a failed write is re-queued instead of dropped (design §4.12-2).
// upstream 복사본, 병합 때 원본 변경 반영.
//
// quotaStore is the user-quota store this batch took. Only ids in it were marked in
// flight (takeBatchStoreLocked), so only those are cleared; an id that appears only in
// the used-quota or request-count store must not erase another batch's mark.
// The re-queue happens before the deferred clear, so the user never reads as
// not-pending in between.
func flushUserQuotaRow(id int, quotaStore map[int]int, usedQuota int, requestCount int) {
	quota, taken := quotaStore[id]
	if taken {
		defer clearUserQuotaInFlight(id)
	}
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return
	}
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]any{
			"quota":         gorm.Expr("quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
			"request_count": gorm.Expr("request_count + ?", requestCount),
		},
	).Error
	if err == nil {
		return
	}
	// The amounts let an operator reconcile an ambiguous failure (the UPDATE applied
	// but the error came back, so the retry applies it twice; design §6).
	common.SysError(common.LogText("failed to batch update user %d quota, requeued quota=%d used=%d requests=%d: %s",
		id, quota, usedQuota, requestCount, err.Error()))
	if quota != 0 {
		addNewRecord(BatchUpdateTypeUserQuota, id, quota)
	}
	if usedQuota != 0 {
		addNewRecord(BatchUpdateTypeUsedQuota, id, usedQuota)
	}
	if requestCount != 0 {
		addNewRecord(BatchUpdateTypeRequestCount, id, requestCount)
	}
}
