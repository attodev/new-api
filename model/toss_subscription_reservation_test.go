package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func tossReservationOrderForTest(t *testing.T, userID int, plan *SubscriptionPlan, suffix string) *SubscriptionOrder {
	t.Helper()
	order := &SubscriptionOrder{
		UserId: userID, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo: "toss_sub_reservation_" + suffix, PaymentMethod: PaymentMethodToss,
		PaymentProvider: PaymentProviderToss, Status: common.TopUpStatusPending,
		ProviderAmount: 15000, ProviderCurrency: "KRW",
	}
	require.NoError(t, SetTossSubscriptionOrderPlanSnapshot(order, plan))
	return order
}

func TestCreateTossSubscriptionOrderPurchaseReservationConcurrentSingleWinner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	user := User{Id: 8801, Username: "reservation-user", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Id: 8802, Title: "Reservation plan", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 1,
	}
	require.NoError(t, DB.Create(&plan).Error)

	start := make(chan struct{})
	errs := make([]error, 2)
	orders := make([]*SubscriptionOrder, len(errs))
	for i := range orders {
		orders[i] = tossReservationOrderForTest(t, user.Id, &plan, fmt.Sprintf("concurrent_%d", i))
	}
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = CreateTossSubscriptionOrderWithPurchaseReservation(orders[i], &plan)
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	limited := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrSubscriptionPurchaseLimit):
			limited++
		default:
			t.Fatalf("unexpected reservation error: %v", err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, limited)
	var pending int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("user_id = ? AND plan_id = ? AND status = ?", user.Id, plan.Id, common.TopUpStatusPending).
		Count(&pending).Error)
	require.Equal(t, int64(1), pending)
}

func TestTossSubscriptionPurchaseReservationReleasesTerminalAndStaleOrders(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	user := User{Id: 8811, Username: "reservation-release", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Id: 8812, Title: "Reservation release", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 1,
	}
	require.NoError(t, DB.Create(&plan).Error)

	for i, status := range []string{common.TopUpStatusFailed, common.TopUpStatusExpired} {
		order := tossReservationOrderForTest(t, user.Id, &plan, fmt.Sprintf("terminal_%d", i))
		order.Status = status
		require.NoError(t, DB.Create(order).Error)
	}
	stale := tossReservationOrderForTest(t, user.Id, &plan, "stale")
	stale.CreateTime = GetDBTimestamp() - TossSubscriptionPurchaseReservationMaxAgeSeconds - 1
	stale.ProviderCredential = "encrypted-secret"
	stale.ProviderClientKeyHash = "client-key-fingerprint"
	require.NoError(t, DB.Create(stale).Error)

	fresh := tossReservationOrderForTest(t, user.Id, &plan, "fresh")
	require.NoError(t, CreateTossSubscriptionOrderWithPurchaseReservation(fresh, &plan))
	require.Equal(t, tossBillingChargeProtocolDurableAttempt, fresh.BillingChargeProtocolVersion)
	require.Greater(t, fresh.CreateTime, int64(0), "reservation creation must use the transaction's DB timestamp")
	require.NoError(t, DB.First(stale, stale.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, stale.Status)
	require.Empty(t, stale.ProviderCredential)
	require.Empty(t, stale.ProviderClientKeyHash)
	var pending int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).
		Where("user_id = ? AND plan_id = ? AND status = ?", user.Id, plan.Id, common.TopUpStatusPending).
		Count(&pending).Error)
	require.Equal(t, int64(1), pending)
}

func TestTossSubscriptionPurchaseReservationPreservesStaleProviderMarkers(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	user := User{Id: 8815, Username: "reservation-provider-marker", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Id: 8816, Title: "Reservation provider marker", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 1,
	}
	require.NoError(t, DB.Create(&plan).Error)

	stale := tossReservationOrderForTest(t, user.Id, &plan, "stale_provider_marker")
	stale.CreateTime = GetDBTimestamp() - TossSubscriptionPurchaseReservationMaxAgeSeconds - 1
	stale.BillingIssueAttempted = true
	require.NoError(t, DB.Create(stale).Error)

	second := tossReservationOrderForTest(t, user.Id, &plan, "stale_provider_marker_second")
	err := CreateTossSubscriptionOrderWithPurchaseReservation(second, &plan)
	require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)
	require.NoError(t, DB.First(stale, stale.Id).Error)
	require.Equal(t, common.TopUpStatusPending, stale.Status)
}

func TestTossSubscriptionReplacementBlockedByUnresolvedFinancialState(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		status    string
		balance   int64
		blocked   bool
	}{
		{
			name:      "partial cancellation blocks",
			eventType: TossPaymentEventTypeCancellation, status: "PARTIAL_CANCELED", balance: 5000, blocked: true,
		},
		{
			name:      "generic cancellation mismatch blocks",
			eventType: TossPaymentEventTypeFinancialMismatch, status: "CANCELED", balance: 0, blocked: true,
		},
		{
			name:      "safe full cancellation does not block",
			eventType: TossPaymentEventTypeCancellation, status: "CANCELED", balance: 0, blocked: false,
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupTossBillingModelTestDB(t)
			require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
			user := User{Id: 8900 + i, Username: fmt.Sprintf("financial-barrier-%d", i), Status: common.UserStatusEnabled, Group: "default"}
			require.NoError(t, DB.Create(&user).Error)
			plan := SubscriptionPlan{
				Id: 8950 + i, Title: "Unlimited replacement plan", PriceAmount: 10, Currency: "USD",
				DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 0,
			}
			require.NoError(t, DB.Create(&plan).Error)
			terminal := tossReservationOrderForTest(t, user.Id, &plan, fmt.Sprintf("financial_terminal_%d", i))
			terminal.Status = common.TopUpStatusFailed
			require.NoError(t, DB.Create(terminal).Error)
			created, err := RecordTossPaymentEvent(&TossPaymentEvent{
				EventKey: fmt.Sprintf("financial-barrier-%d", i), EventType: test.eventType,
				OrderId: terminal.TradeNo, PaymentKey: fmt.Sprintf("pay-financial-%d", i), Status: test.status,
				OriginalAmount: 10000, BalanceAmount: test.balance,
				ReconciliationStatus: TossReconciliationStatusRequired,
			})
			require.NoError(t, err)
			require.True(t, created)

			replacement := tossReservationOrderForTest(t, user.Id, &plan, fmt.Sprintf("financial_replacement_%d", i))
			err = CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan)
			if !test.blocked {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrPersonalTossBillingInFlight)
			var event TossPaymentEvent
			require.NoError(t, DB.Where("event_key = ?", fmt.Sprintf("financial-barrier-%d", i)).First(&event).Error)
			_, err = ResolveTossPaymentEventByAdmin(event.Id, 1, "financial state reviewed")
			require.NoError(t, err)
			require.NoError(t, CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan))
		})
	}
}

func TestTossSubscriptionReplacementBlockedByPendingProviderAttemptWithoutPurchaseCap(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	user := User{Id: 8991, Username: "pending-attempt-barrier", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Id: 8992, Title: "Unlimited pending attempt", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 0,
	}
	require.NoError(t, DB.Create(&plan).Error)
	pending := tossReservationOrderForTest(t, user.Id, &plan, "pending_provider_attempt")
	pending.BillingAttempted = true
	require.NoError(t, DB.Create(pending).Error)

	replacement := tossReservationOrderForTest(t, user.Id, &plan, "pending_provider_replacement")
	require.ErrorIs(t, CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan), ErrPersonalTossBillingInFlight)
	var count int64
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestTossSubscriptionReservationTreatsRenewalPrefixLiterally(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	user := User{Id: 8821, Username: "reservation-like", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	plan := SubscriptionPlan{
		Id: 8822, Title: "Reservation LIKE", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true, MaxPurchasePerUser: 1,
	}
	require.NoError(t, DB.Create(&plan).Error)
	reservation := tossReservationOrderForTest(t, user.Id, &plan, "like")
	// This matches an unescaped toss_sub_renew_% SQL pattern because '_' is a
	// wildcard, but it is not a renewal order and must reserve the purchase.
	reservation.TradeNo = "tossXsubYrenewZnot_a_renewal"
	reservation.CreateTime = GetDBTimestamp()
	require.NoError(t, DB.Create(reservation).Error)

	second := tossReservationOrderForTest(t, user.Id, &plan, "like_second")
	err := CreateTossSubscriptionOrderWithPurchaseReservation(second, &plan)
	require.ErrorIs(t, err, ErrSubscriptionPurchaseLimit)
}

func TestCancelUnattemptedTossSubscriptionOrderReleasesReservation(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	plan := SubscriptionPlan{
		Id: 8832, Title: "Reservation cancellation", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(&plan).Error)
	order := tossReservationOrderForTest(t, 8831, &plan, "cancel")
	order.ProviderCredential = "encrypted-secret"
	order.ProviderClientKeyHash = "client-key-fingerprint"
	require.NoError(t, DB.Create(order).Error)

	cancelled, err := CancelUnattemptedTossSubscriptionOrder(order.TradeNo, order.UserId)
	require.NoError(t, err)
	require.True(t, cancelled)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusExpired, order.Status)
	require.Greater(t, order.CompleteTime, int64(0))
	require.Empty(t, order.ProviderCredential)
	require.Empty(t, order.ProviderClientKeyHash)

	cancelled, err = CancelUnattemptedTossSubscriptionOrder(order.TradeNo, order.UserId)
	require.NoError(t, err)
	require.False(t, cancelled, "cancellation must be idempotent")
}

func TestCancelUnattemptedTossSubscriptionOrderPreservesProviderActivity(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	plan := SubscriptionPlan{
		Id: 8842, Title: "Reservation activity", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(&plan).Error)

	tests := []struct {
		name   string
		mutate func(*SubscriptionOrder)
	}{
		{name: "live claim", mutate: func(order *SubscriptionOrder) {
			order.BillingClaimToken = "claim-token"
			order.BillingClaimTime = GetDBTimestamp()
		}},
		{name: "authorization snapshot", mutate: func(order *SubscriptionOrder) {
			order.BillingIssueAuthKey = "encrypted-auth-key"
			order.BillingIssueAuthKeyHash = "auth-key-hash"
			order.BillingIssueCustomerKey = "customer-key"
		}},
		{name: "issue request attempted", mutate: func(order *SubscriptionOrder) {
			order.BillingIssueAttempted = true
		}},
		{name: "billing key attached", mutate: func(order *SubscriptionOrder) {
			order.BillingKeyId = 42
		}},
		{name: "charge request attempted", mutate: func(order *SubscriptionOrder) {
			order.BillingAttempted = true
			order.BillingAttemptCredential = "encrypted-attempt-secret"
		}},
		{name: "provider response persisted", mutate: func(order *SubscriptionOrder) {
			order.ProviderPayload = `{"paymentKey":"redacted"}`
		}},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			order := tossReservationOrderForTest(t, 8841, &plan, fmt.Sprintf("activity_%d", i))
			tc.mutate(order)
			require.NoError(t, DB.Create(order).Error)

			cancelled, err := CancelUnattemptedTossSubscriptionOrder(order.TradeNo, order.UserId)
			require.NoError(t, err)
			require.False(t, cancelled)
			require.NoError(t, DB.First(order, order.Id).Error)
			require.Equal(t, common.TopUpStatusPending, order.Status)
		})
	}
}

func TestCancelUnattemptedTossSubscriptionOrderEnforcesOwner(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	plan := SubscriptionPlan{
		Id: 8852, Title: "Reservation owner", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(&plan).Error)
	order := tossReservationOrderForTest(t, 8851, &plan, "owner")
	require.NoError(t, DB.Create(order).Error)

	cancelled, err := CancelUnattemptedTossSubscriptionOrder(order.TradeNo, order.UserId+1)
	require.ErrorIs(t, err, ErrSubscriptionOrderNotFound)
	require.False(t, cancelled)
	require.NoError(t, DB.First(order, order.Id).Error)
	require.Equal(t, common.TopUpStatusPending, order.Status)
}

func TestCancelUnattemptedTossSubscriptionOrderRacesBillingIssueClaim(t *testing.T) {
	setupTossBillingModelTestDB(t)
	require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}, &SubscriptionOrder{}))
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	// Keep SQLite's in-memory schema on one connection while still exercising
	// the ordering of the two independent conditional updates.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	plan := SubscriptionPlan{
		Id: 8862, Title: "Reservation race", PriceAmount: 10, Currency: "USD",
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, DB.Create(&plan).Error)
	order := tossReservationOrderForTest(t, 8861, &plan, "race")
	require.NoError(t, DB.Create(order).Error)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var cancelled, claimed bool
	var cancelErr, claimErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		cancelled, cancelErr = CancelUnattemptedTossSubscriptionOrder(order.TradeNo, order.UserId)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, claimed, claimErr = ClaimTossSubscriptionBillingIssue(order.TradeNo, "auth-race", "customer-race")
	}()
	close(start)
	wg.Wait()

	require.NoError(t, cancelErr)
	require.NotEqual(t, cancelled, claimed, "exactly one provider-activity owner must win")
	if cancelled {
		require.ErrorIs(t, claimErr, ErrSubscriptionOrderStatusInvalid)
	} else {
		require.NoError(t, claimErr)
	}
	require.NoError(t, DB.First(order, order.Id).Error)
	if cancelled {
		require.Equal(t, common.TopUpStatusExpired, order.Status)
		require.Empty(t, order.BillingIssueAuthKey)
	} else {
		require.Equal(t, common.TopUpStatusPending, order.Status)
		require.NotEmpty(t, order.BillingIssueAuthKey)
		require.NotEmpty(t, order.BillingClaimToken)
	}
}
