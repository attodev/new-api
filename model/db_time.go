package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type deadlineDBContextKey struct{}

const (
	sqliteDeadlineReserve        = 100 * time.Millisecond
	sqliteDeadlineBusyTimeoutCap = 500 * time.Millisecond
)

func dbWithContext(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return DB
	}
	if bound, ok := ctx.Value(deadlineDBContextKey{}).(*gorm.DB); ok && bound != nil {
		return bound.WithContext(ctx)
	}
	return DB.WithContext(ctx)
}

// BindDeadlineDBContext reserves one SQLite connection and caps that
// connection's busy timeout to the request deadline. modernc SQLite does not
// interrupt a busy-handler sleep when QueryContext is canceled, so WithContext
// alone can exceed a webhook SLA by the process-wide _busy_timeout (normally
// 30s). Keeping the PRAGMA and all request DB calls on the same connection makes
// the deadline effective without leaving a timed-out mutation running in a
// detached goroutine.
//
// MySQL and PostgreSQL drivers honor context cancellation directly, so they do
// not need a reserved connection.
func BindDeadlineDBContext(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ctx, func() {}, err
	}
	if !common.UsingSQLite {
		return ctx, func() {}, nil
	}
	if _, ok := ctx.Value(deadlineDBContextKey{}).(*gorm.DB); ok {
		return ctx, func() {}, nil
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return ctx, func() {}, nil
	}
	bound, release, err := bindSQLiteDeadlineConnection(ctx, DB, deadline)
	if err != nil {
		return ctx, func() {}, err
	}
	return context.WithValue(ctx, deadlineDBContextKey{}, bound), release, nil
}

func bindSQLiteDeadlineConnection(ctx context.Context, source *gorm.DB, deadline time.Time) (*gorm.DB, func(), error) {
	if source == nil {
		return nil, func() {}, fmt.Errorf("database is not initialized")
	}
	sqlDB, err := source.DB()
	if err != nil {
		return nil, func() {}, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, func() {}, err
	}
	bound := source.Session(&gorm.Session{NewDB: true}).WithContext(ctx)
	bound.Statement.ConnPool = conn

	previousBusyTimeout := 0
	if err := bound.Raw("PRAGMA busy_timeout").Scan(&previousBusyTimeout).Error; err != nil {
		_ = conn.Close()
		return nil, func() {}, err
	}
	remaining := time.Until(deadline) - sqliteDeadlineReserve
	if remaining <= 0 {
		_ = conn.Close()
		if err := ctx.Err(); err != nil {
			return nil, func() {}, err
		}
		return nil, func() {}, context.DeadlineExceeded
	}
	if remaining > sqliteDeadlineBusyTimeoutCap {
		remaining = sqliteDeadlineBusyTimeoutCap
	}
	timeoutMillis := remaining.Milliseconds()
	if timeoutMillis < 1 {
		timeoutMillis = 1
	}
	if err := bound.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", timeoutMillis)).Error; err != nil {
		_ = conn.Close()
		return nil, func() {}, err
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			// PRAGMA is connection-local and does not touch database pages, so it is
			// safe to restore after the request context has elapsed.
			restoreDB := bound.WithContext(context.Background())
			_ = restoreDB.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", previousBusyTimeout)).Error
			_ = conn.Close()
		})
	}
	return bound, release, nil
}

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
func GetDBTimestamp() int64 {
	return getDBTimestampTx(nil)
}

// GetDBTimestampWithContext keeps database-clock reads under the caller's
// deadline. As with GetDBTimestamp, an unavailable database falls back to the
// application clock; subsequent writes still observe the same context.
func GetDBTimestampWithContext(ctx context.Context) int64 {
	return getDBTimestampTx(dbWithContext(ctx))
}

func getDBTimestampTx(tx *gorm.DB) int64 {
	var ts int64
	var err error
	query := DB
	if tx != nil {
		query = tx
	}
	switch {
	case common.UsingPostgreSQL:
		err = query.Raw("SELECT EXTRACT(EPOCH FROM NOW())::bigint").Scan(&ts).Error
	case common.UsingSQLite:
		err = query.Raw("SELECT strftime('%s','now')").Scan(&ts).Error
	default:
		err = query.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	}
	if err != nil || ts <= 0 {
		return common.GetTimestamp()
	}
	return ts
}
