package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func TestTossRecordedTopUpRecoveryRunsInsideApprovalWindow(t *testing.T) {
	const tossDocumentedApprovalWindow = 10 * time.Minute
	const crashedProviderClaimLease = 2 * time.Minute
	// One recovery can spend 8s on the initial GET, 65s on the idempotent
	// confirmation, and 8s on final authoritative verification.
	const providerWorstCase = 82 * time.Second
	const saturatedQueueBatches = 4
	tests := []struct {
		name    string
		batch   int
		workers int
	}{
		{name: "server database", batch: tossPendingCleanupReconcileLimit, workers: model.TossTopUpRecoveryWorkerCount},
		{name: "sqlite", batch: tossPendingCleanupSQLiteReconcileLimit, workers: model.TossTopUpRecoverySQLiteWorkerCount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			waves := (test.batch + test.workers - 1) / test.workers
			worstCaseSaturatedRecovery := crashedProviderClaimLease + tossPendingTopUpRecoveryTickInterval + time.Duration(saturatedQueueBatches*waves)*providerWorstCase
			if worstCaseSaturatedRecovery >= tossDocumentedApprovalWindow {
				t.Fatalf("worst-case saturated recovery = %s, must be inside %s approval window", worstCaseSaturatedRecovery, tossDocumentedApprovalWindow)
			}
		})
	}
}
