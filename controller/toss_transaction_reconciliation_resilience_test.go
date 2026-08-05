package controller

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunTossTransactionReconciliationIterationAllowsLaterTickAfterPanic(t *testing.T) {
	var calls atomic.Int64
	var running atomic.Bool
	runTossTransactionReconciliationIteration(func() {
		require.True(t, running.CompareAndSwap(false, true))
		defer running.Store(false)
		calls.Add(1)
		panic("sensitive reconciliation state must not escape through the task log")
	})
	require.False(t, running.Load())
	runTossTransactionReconciliationIteration(func() {
		require.True(t, running.CompareAndSwap(false, true))
		defer running.Store(false)
		calls.Add(1)
	})

	require.Equal(t, int64(2), calls.Load())
	require.False(t, running.Load())
}
