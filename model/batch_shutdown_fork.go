package model

import "github.com/QuantumNous/new-api/common"

// fork: O12 — 정상 종료 때 배치 큐를 DB에 쓴다. upstream은 큐를 버려 재배포마다
// 최대 BATCH_UPDATE_INTERVAL 분량의 차감이 DB에서 사라졌다(설계 §4.12-1).
func FlushBatchUpdates() {
	if !common.BatchUpdateEnabled {
		return
	}
	common.SysLog(common.LogText("flushing batch updates before shutdown"))
	batchUpdate()
}
