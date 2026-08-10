package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRecordTossPaymentEventWithContextRejectsCanceledWrite(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TossPaymentEvent{}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	created, err := RecordTossPaymentEventWithContext(ctx, &TossPaymentEvent{
		EventKey:             "toss_context_canceled_event",
		EventType:            TossPaymentEventTypeFulfillment,
		OrderId:              "toss_context_canceled_order",
		PaymentKey:           "pay_context_canceled",
		Status:               "DONE",
		ReconciliationStatus: TossReconciliationStatusRequired,
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, created)

	var count int64
	require.NoError(t, DB.Model(&TossPaymentEvent{}).Where("event_key = ?", "toss_context_canceled_event").Count(&count).Error)
	require.Zero(t, count)
}

func TestRechargeTossWithContextDoesNotSettleAfterCancellation(t *testing.T) {
	setupTossSettlementCASTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       9101,
		Username: "toss-context-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
		Quota:    17,
		Group:    "default",
		AffCode:  "toss-context-user-aff",
	}).Error)
	require.NoError(t, DB.Create(&TopUp{
		UserId:          9101,
		TargetType:      TopUpTargetTypeUser,
		TargetId:        9101,
		Amount:          1000,
		Quota:           1000,
		TradeNo:         "toss_context_canceled_settlement",
		ProviderOrderId: "pay_context_canceled_settlement",
		PaymentProvider: PaymentProviderToss,
		PaymentMethod:   PaymentMethodToss,
		Status:          common.TopUpStatusPending,
	}).Error)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, RechargeTossWithContext(ctx, "toss_context_canceled_settlement", "pay_context_canceled_settlement", "test"))

	var topUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", "toss_context_canceled_settlement").First(&topUp).Error)
	require.Equal(t, common.TopUpStatusPending, topUp.Status)
	var user User
	require.NoError(t, DB.First(&user, 9101).Error)
	require.Equal(t, int64(17), user.Quota)
}

func TestRecordLogWithContextBoundsSeparateSQLiteLogDatabaseLock(t *testing.T) {
	setupTossBillingModelTestDB(t)
	originalLogDB := LOG_DB
	originalLogSQLType := common.LogSqlType
	dsn := filepath.Join(t.TempDir(), "toss-webhook-log-context.db") + "?_pragma=busy_timeout(10000)"
	logDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlLogDB, err := logDB.DB()
	require.NoError(t, err)
	sqlLogDB.SetMaxOpenConns(4)
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	LOG_DB = logDB
	common.LogSqlType = common.DatabaseTypeSQLite
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.LogSqlType = originalLogSQLType
		_ = sqlLogDB.Close()
	})

	lockConn, err := sqlLogDB.Conn(context.Background())
	require.NoError(t, err)
	defer lockConn.Close()
	_, err = lockConn.ExecContext(context.Background(), "BEGIN EXCLUSIVE")
	require.NoError(t, err)
	locked := true
	defer func() {
		if locked {
			_, _ = lockConn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	RecordLogWithContext(ctx, 0, LogTypeTopup, "bounded separate Toss webhook log")
	require.Less(t, time.Since(started), time.Second, "separate SQLite log writes must not inherit a long process busy timeout")

	_, err = lockConn.ExecContext(context.Background(), "ROLLBACK")
	require.NoError(t, err)
	locked = false
	var count int64
	require.NoError(t, logDB.Model(&Log{}).Where("content = ?", "bounded separate Toss webhook log").Count(&count).Error)
	require.Zero(t, count)
}
