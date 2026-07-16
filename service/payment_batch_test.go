package service

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPaymentBatchWorkerCountSerializesSQLiteWrites(t *testing.T) {
	original := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = original })
	common.UsingSQLite = true
	require.Equal(t, 1, paymentBatchWorkerCount(8))
	common.UsingSQLite = false
	require.Equal(t, 8, paymentBatchWorkerCount(8))
}

func TestRunBoundedPaymentBatchEnforcesWorkerLimit(t *testing.T) {
	items := make([]int, 12)
	started := make(chan struct{}, len(items))
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int64

	go func() {
		runBoundedPaymentBatch(items, 4, func(int) {
			calls.Add(1)
			started <- struct{}{}
			<-release
		})
		close(done)
	}()

	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("worker limit exceeded before a slot was released")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bounded batch did not finish")
	}
	require.Equal(t, int64(len(items)), calls.Load())
}

func TestRunBoundedPaymentBatchRecoversItemPanicAndContinues(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var attempted atomic.Int64
	var completed atomic.Int64

	runBoundedPaymentBatch(items, 2, func(item int) {
		attempted.Add(1)
		if item == 2 {
			panic("sensitive payment detail must not escape through the batch log")
		}
		completed.Add(1)
	})

	require.Equal(t, int64(len(items)), attempted.Load())
	require.Equal(t, int64(len(items)-1), completed.Load())
}

func TestRunPaymentTaskIterationRecoversAndAllowsLaterTick(t *testing.T) {
	var calls atomic.Int64
	var running atomic.Bool
	runPaymentTaskIteration(func() {
		require.True(t, running.CompareAndSwap(false, true))
		defer running.Store(false)
		calls.Add(1)
		panic("sensitive task state must not escape through the periodic task log")
	})
	require.False(t, running.Load(), "a panicking iteration must release its running flag")
	runPaymentTaskIteration(func() {
		require.True(t, running.CompareAndSwap(false, true), "the next tick must be able to claim the task")
		defer running.Store(false)
		calls.Add(1)
	})

	require.Equal(t, int64(2), calls.Load())
	require.False(t, running.Load())
}

func TestRunBoundedPaymentBatchDoesNotDeadlockAfterAbnormalWorkerExit(t *testing.T) {
	done := make(chan struct{})
	go func() {
		runBoundedPaymentBatch([]int{1, 2}, 1, func(item int) {
			if item == 1 {
				runtime.Goexit()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("payment batch producer deadlocked after its only worker exited")
	}
}
