package model

import (
	"strings"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"
)

func TestTossProviderPayloadUsesUnboundedDatabaseTypes(t *testing.T) {
	t.Parallel()

	dialectors := []struct {
		name      string
		dialector interface {
			DataTypeOf(*schema.Field) string
		}
		want string
	}{
		{name: "mysql", dialector: mysql.New(mysql.Config{}), want: "longtext"},
		{name: "postgres", dialector: postgres.New(postgres.Config{}), want: "text"},
		{name: "sqlite", dialector: sqlite.Open(":memory:"), want: "text"},
	}
	models := []struct {
		name  string
		value any
	}{
		{name: "top_up", value: &TopUp{}},
		{name: "subscription_order", value: &SubscriptionOrder{}},
		{name: "toss_payment_event", value: &TossPaymentEvent{}},
	}

	for _, model := range models {
		parsed, err := schema.Parse(model.value, &sync.Map{}, schema.NamingStrategy{})
		require.NoError(t, err)
		field := parsed.LookUpField("ProviderPayload")
		require.NotNil(t, field)
		require.Empty(t, field.TagSettings["TYPE"], "%s must remain dialect-resolved", model.name)

		for _, dialect := range dialectors {
			t.Run(model.name+"_"+dialect.name, func(t *testing.T) {
				require.Equal(t, dialect.want, strings.ToLower(dialect.dialector.DataTypeOf(field)))
			})
		}
	}
}

func TestTossProviderPayloadPersistsTwoMiB(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}, &TossPaymentEvent{}))

	const payloadSize = 2 * 1024 * 1024
	prefix := `{"payload":"`
	suffix := `"}`
	payload := prefix + strings.Repeat("x", payloadSize-len(prefix)-len(suffix)) + suffix
	require.Len(t, payload, payloadSize)

	topUp := &TopUp{TradeNo: "toss_large_payload_topup", ProviderPayload: payload}
	require.NoError(t, DB.Create(topUp).Error)
	order := &SubscriptionOrder{TradeNo: "toss_large_payload_subscription", ProviderPayload: payload}
	require.NoError(t, DB.Create(order).Error)
	event := &TossPaymentEvent{EventKey: "toss_large_payload_event", ProviderPayload: payload}
	require.NoError(t, DB.Create(event).Error)

	var storedTopUp TopUp
	require.NoError(t, DB.First(&storedTopUp, topUp.Id).Error)
	require.Equal(t, payload, storedTopUp.ProviderPayload)
	var storedOrder SubscriptionOrder
	require.NoError(t, DB.First(&storedOrder, order.Id).Error)
	require.Equal(t, payload, storedOrder.ProviderPayload)
	var storedEvent TossPaymentEvent
	require.NoError(t, DB.First(&storedEvent, event.Id).Error)
	require.Equal(t, payload, storedEvent.ProviderPayload)
}
