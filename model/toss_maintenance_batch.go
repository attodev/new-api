package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const tossMaintenanceWorkerCount = 8

// runBoundedTossMaintenance prevents slow provider cleanup calls from blocking
// every later key/order in the maintenance queue. Callers reserve at most one
// worker-sized batch before entering this helper, so their short DB leases do
// not expire while rows wait in a large in-memory queue.
func runBoundedTossMaintenance[T any](items []T, run func(T)) {
	runBoundedTossMaintenanceWithWorkers(items, tossMaintenanceWorkerCount, 1, run)
}

// runBoundedTossMaintenanceWithWorkers lets a deadline-bound queue reserve a
// larger provider-call pool without changing the conservative concurrency used
// by long-lived billing-key and subscription cleanup. sqliteWorkers remains
// explicitly bounded: SQLite serializes the short durable writes, while a few
// independent network calls may proceed so one slow provider response cannot
// consume an entire payment confirmation window.
func runBoundedTossMaintenanceWithWorkers[T any](items []T, serverWorkers, sqliteWorkers int, run func(T)) {
	if len(items) == 0 || run == nil {
		return
	}
	workersCount := serverWorkers
	if common.UsingSQLite {
		workersCount = sqliteWorkers
	}
	if workersCount <= 0 {
		workersCount = 1
	}
	if workersCount > len(items) {
		workersCount = len(items)
	}
	// The selected maintenance batch is already bounded. Buffer it so an
	// abnormal callback exit cannot strand the producer on an unbuffered send;
	// ordinary callback panics are recovered independently below.
	jobs := make(chan T, len(items))
	var workers sync.WaitGroup
	workers.Add(workersCount)
	for i := 0; i < workersCount; i++ {
		go func() {
			defer workers.Done()
			for item := range jobs {
				runTossMaintenanceItem(item, run)
			}
		}()
	}
	for i := range items {
		jobs <- items[i]
	}
	close(jobs)
	workers.Wait()
}

func runTossMaintenanceItem[T any](item T, run func(T)) {
	defer func() {
		if recover() != nil {
			// Reconciler panic values and input rows may contain provider payment
			// identifiers or credentials. Emit only a fixed operational signal;
			// the row's durable retry lease makes the interrupted item recoverable.
			common.SysError("Toss maintenance item panic recovered; remaining items will continue")
		}
	}()
	run(item)
}
