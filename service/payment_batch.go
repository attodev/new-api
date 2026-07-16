package service

import (
	"context"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

func paymentBatchWorkerCount(configured int) int {
	// SQLite serializes writers at the database level. Running multiple payment
	// settlement transactions concurrently only turns provider successes into
	// avoidable SQLITE_BUSY failures; server databases use the configured pool.
	if common.UsingSQLite {
		return 1
	}
	return configured
}

// runBoundedPaymentBatch runs independent payment jobs with a small fixed
// worker pool. Individual jobs own their durable DB claim/idempotency key, so a
// slow Toss response cannot head-of-line block the rest of a billing batch.
func runBoundedPaymentBatch[T any](items []T, concurrency int, run func(T)) {
	if len(items) == 0 || run == nil {
		return
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}

	// Buffer the already-bounded input so the producer cannot deadlock even if
	// an abnormal worker exit (for example runtime.Goexit inside an injected
	// callback) removes every consumer. Ordinary panics are recovered per item.
	jobs := make(chan T, len(items))
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer workers.Done()
			for item := range jobs {
				runPaymentBatchItem(item, run)
			}
		}()
	}
	for i := range items {
		jobs <- items[i]
	}
	close(jobs)
	workers.Wait()
}

func runPaymentBatchItem[T any](item T, run func(T)) {
	defer func() {
		if recover() != nil {
			// Panic values and items may embed payment identifiers or credentials.
			// Keep the log deliberately generic; durable per-item claims let a later
			// worker retry the interrupted operation safely.
			logger.LogError(context.Background(), "payment batch item panic recovered; remaining items will continue")
		}
	}()
	run(item)
}

func runPaymentTaskIteration(run func()) {
	if run == nil {
		return
	}
	defer func() {
		if recover() != nil {
			// The outer gopool also recovers, but recovery there terminates the
			// ticker goroutine permanently. Recover one iteration here so later
			// billing and cleanup ticks continue. Keep panic values out of logs.
			logger.LogError(context.Background(), "periodic payment task iteration panic recovered; later ticks will continue")
		}
	}()
	run()
}
