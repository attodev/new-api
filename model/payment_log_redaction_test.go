package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentSafeGORMLoggerDropsBoundSecretParameters(t *testing.T) {
	filter, ok := paymentSafeGORMConfig().Logger.(interface {
		ParamsFilter(context.Context, string, ...interface{}) (string, []interface{})
	})
	require.True(t, ok)

	sql, params := filter.ParamsFilter(
		context.Background(),
		"UPDATE options SET value = ? WHERE key = ?",
		"live_sk_must_not_reach_logs",
		"TossSecretKey",
	)
	require.Equal(t, "UPDATE options SET value = ? WHERE key = ?", sql)
	require.Nil(t, params)
}
