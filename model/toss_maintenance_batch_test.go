package model

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRunBoundedTossMaintenanceUsesEightWorkersOnServerDatabases(t *testing.T) {
	originalSQLite := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = originalSQLite })
	common.UsingSQLite = false

	items := make([]int, tossMaintenanceWorkerCount+3)
	started := make(chan struct{}, len(items))
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int64
	go func() {
		runBoundedTossMaintenance(items, func(int) {
			calls.Add(1)
			started <- struct{}{}
			<-release
		})
		close(done)
	}()

	for i := 0; i < tossMaintenanceWorkerCount; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("maintenance worker did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("maintenance worker limit exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance batch did not finish")
	}
	require.Equal(t, int64(len(items)), calls.Load())
}

func TestRunBoundedTossMaintenanceSerializesSQLite(t *testing.T) {
	originalSQLite := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = originalSQLite })
	common.UsingSQLite = true

	items := []int{1, 2}
	started := make(chan struct{}, len(items))
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		runBoundedTossMaintenance(items, func(int) {
			started <- struct{}{}
			<-release
		})
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("SQLite maintenance worker did not start")
	}
	select {
	case <-started:
		t.Fatal("SQLite maintenance must use one worker")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SQLite maintenance batch did not finish")
	}
}

func TestRunBoundedTossMaintenanceUsesDeadlineQueueWorkerOverrides(t *testing.T) {
	tests := []struct {
		name          string
		sqlite        bool
		serverWorkers int
		sqliteWorkers int
		expected      int
	}{
		{name: "server", serverWorkers: TossTopUpRecoveryWorkerCount, sqliteWorkers: TossTopUpRecoverySQLiteWorkerCount, expected: TossTopUpRecoveryWorkerCount},
		{name: "sqlite", sqlite: true, serverWorkers: TossTopUpRecoveryWorkerCount, sqliteWorkers: TossTopUpRecoverySQLiteWorkerCount, expected: TossTopUpRecoverySQLiteWorkerCount},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			originalSQLite := common.UsingSQLite
			t.Cleanup(func() { common.UsingSQLite = originalSQLite })
			common.UsingSQLite = test.sqlite

			items := make([]int, test.expected+1)
			started := make(chan struct{}, len(items))
			release := make(chan struct{})
			done := make(chan struct{})
			go func() {
				runBoundedTossMaintenanceWithWorkers(items, test.serverWorkers, test.sqliteWorkers, func(int) {
					started <- struct{}{}
					<-release
				})
				close(done)
			}()

			for i := 0; i < test.expected; i++ {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("deadline queue worker did not start")
				}
			}
			select {
			case <-started:
				t.Fatal("deadline queue worker override exceeded")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("deadline queue batch did not finish")
			}
		})
	}
}

func TestRunBoundedTossMaintenanceRecoversItemPanicAndContinues(t *testing.T) {
	originalSQLite := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = originalSQLite })
	common.UsingSQLite = false

	items := []int{1, 2, 3, 4, 5}
	var attempted atomic.Int64
	var completed atomic.Int64

	runBoundedTossMaintenance(items, func(item int) {
		attempted.Add(1)
		if item == 3 {
			panic("provider identifier must not escape through the maintenance log")
		}
		completed.Add(1)
	})

	require.Equal(t, int64(len(items)), attempted.Load())
	require.Equal(t, int64(len(items)-1), completed.Load())
}

func TestRunBoundedTossMaintenanceDoesNotDeadlockAfterAbnormalWorkerExit(t *testing.T) {
	originalSQLite := common.UsingSQLite
	t.Cleanup(func() { common.UsingSQLite = originalSQLite })
	common.UsingSQLite = true

	done := make(chan struct{})
	go func() {
		runBoundedTossMaintenance([]int{1, 2}, func(item int) {
			if item == 1 {
				runtime.Goexit()
			}
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Toss maintenance producer deadlocked after its only worker exited")
	}
}
