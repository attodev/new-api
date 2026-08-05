package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTossTransactionReconciliationTest(t *testing.T) {
	t.Helper()
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(
		&model.User{},
		&model.TopUp{},
		&model.SubscriptionOrder{},
		&model.TossPaymentEvent{},
		&model.TossTransactionReconciliationCursor{},
		&model.TossTransactionReconciliationPageCursor{},
		&model.Log{},
	))
	require.NoError(t, model.DB.Create(&model.User{Id: 71, Username: "transaction-reconcile-user", AffCode: "transaction-reconcile-user"}).Error)
}

func TestTossTransactionReconciliationSourceLeaseAllowsOnlyOneProviderPageGET(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_source_lease"
	source := tossTransactionCredentialSource{
		SourceKey: tossTransactionSourceKey("", secretKey),
		SecretKey: secretKey,
	}
	now := time.Unix(model.GetDBTimestamp(), 0).Truncate(time.Second)
	_, err := model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, now.Add(-time.Hour).Unix())
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	firstPageStarted := make(chan struct{})
	releaseFirstPage := make(chan struct{})
	defer func() {
		select {
		case <-releaseFirstPage:
		default:
			close(releaseFirstPage)
		}
	}()
	var pageRequests atomic.Int32
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.EscapedPath() != "/v1/transactions" {
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
		if pageRequests.Add(1) == 1 {
			close(firstPageStarted)
			<-releaseFirstPage
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}

	firstResult := make(chan error, 1)
	go func() {
		firstResult <- reconcileTossTransactionSource(context.Background(), source, now)
	}()
	select {
	case <-firstPageStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first reconciliation did not reach the provider page GET")
	}

	secondErr := reconcileTossTransactionSource(context.Background(), source, now)
	require.ErrorIs(t, secondErr, model.ErrTossTransactionLeaseHeld)
	require.EqualValues(t, 1, pageRequests.Load(), "the losing master must issue no provider request")
	close(releaseFirstPage)
	select {
	case firstErr := <-firstResult:
		require.NoError(t, firstErr)
	case <-time.After(5 * time.Second):
		t.Fatal("first reconciliation did not finish after releasing the provider response")
	}
	require.EqualValues(t, 1, pageRequests.Load())
}

func TestTossTransactionReconciliationRecoversMissedEasyPayCancellation(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey  = "live_sk_transaction_reconcile"
		orderID    = "toss_transaction_cancel"
		paymentKey = "pay_transaction_cancel"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 15000, Money: 10, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderCredential: credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_reconcile"),
		PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		CreateTime: model.GetDBTimestamp() - 3600, CompleteTime: model.GetDBTimestamp() - 3500,
		Status: common.TopUpStatusSuccess,
	}).Error)

	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(model.TossClientKeyFingerprint("live_ck_transaction_reconcile"), secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_reconcile"),
	}
	now := time.Now().Truncate(time.Second)
	_, err = model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, now.Add(-time.Hour).Unix())
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(secretKey+":")), request.Header.Get("Authorization"))
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			require.NotEmpty(t, request.URL.Query().Get("startDate"))
			require.NotEmpty(t, request.URL.Query().Get("endDate"))
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`[{"mId":"mid","transactionKey":"done_tx_reconcile","paymentKey":"pay_transaction_cancel","orderId":"toss_transaction_cancel","method":"간편결제","status":"DONE","transactionAt":"2026-07-11T11:00:00+09:00","currency":"KRW","amount":15000},{"mId":"mid","transactionKey":"cancel_tx_reconcile","paymentKey":"pay_transaction_cancel","orderId":"toss_transaction_cancel","method":"간편결제","status":"CANCELED","transactionAt":"2026-07-11T12:00:00+09:00","currency":"KRW","amount":15000}]`,
			))}, nil
		case "/v1/payments/pay_transaction_cancel":
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_transaction_cancel","type":"NORMAL","orderId":"toss_transaction_cancel","status":"CANCELED","totalAmount":15000,"balanceAmount":0,"currency":"KRW","method":"간편결제","easyPay":{"provider":"TOSSPAY","amount":15000,"discountAmount":0},"cancels":[{"cancelAmount":15000,"refundableAmount":0,"canceledAt":"2026-07-11T12:00:00+09:00","transactionKey":"cancel_tx_reconcile","cancelStatus":"DONE"}]}`,
			))}, nil
		default:
			return nil, errors.New("unexpected Toss reconciliation request: " + request.URL.EscapedPath())
		}
	})}

	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	// The already-credited DONE row is now re-fetched as well; local success
	// alone cannot auto-resolve a legacy event that may contain mismatched money.
	require.Equal(t, 3, requests)

	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_tx_reconcile").First(&event).Error)
	require.Equal(t, model.TossPaymentEventTypeCancellation, event.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
	require.EqualValues(t, 15000, event.CancelAmount)

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status, "post-credit refunds require manual quota reconciliation")

	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.Equal(t, now.Unix(), cursor.CursorTime)
}

func TestTossTransactionReconciliationDoesNotAdvancePastUnverifiedCancellation(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_retry"
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: "toss_transaction_retry",
		ProviderOrderId:    "pay_transaction_retry",
		ProviderCredential: credential,
		PaymentMethod:      model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess,
	}).Error)
	source := tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey}
	now := time.Now().Truncate(time.Second)
	initial := now.Add(-time.Hour).Unix()
	_, err = model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, initial)
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`[{"transactionKey":"cancel_tx_retry","paymentKey":"pay_transaction_retry","orderId":"toss_transaction_retry","status":"CANCELED"}]`,
			))}, nil
		case "/v1/payments/pay_transaction_retry":
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"code":"PROVIDER_ERROR"}`))}, nil
		default:
			return nil, errors.New("unexpected request")
		}
	})}

	require.Error(t, reconcileTossTransactionSource(context.Background(), source, now))
	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.Equal(t, initial, cursor.CursorTime)
}

func TestTossTransactionReconciliationRecentCursorBootstrapsOnceThenAdvancesIncrementally(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_recent_cursor"
	source := tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey}
	now := time.Unix(model.GetDBTimestamp(), 0).Truncate(time.Second)
	initial := now.Add(-100 * 24 * time.Hour).Unix()
	_, err := model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, initial)
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}

	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, tossTransactionReconciliationRecentDays+tossTransactionReconciliationMaxWindows, requests)
	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.EqualValues(t, now.Unix(), cursor.RecentCursorTime)
	require.EqualValues(t, initial+int64(tossTransactionReconciliationMaxWindows)*int64(tossTransactionReconciliationWindow/time.Second), cursor.CursorTime)

	// The historical cursor is still months behind. The next hourly run reads
	// only the incremental recent tail plus its normal three backlog windows; it
	// must not repeat all seven bootstrap days.
	requests = 0
	nextNow := now.Add(time.Hour)
	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, nextNow))
	require.Equal(t, 1+tossTransactionReconciliationMaxWindows, requests)
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.EqualValues(t, nextNow.Unix(), cursor.RecentCursorTime)
	require.EqualValues(t, initial+2*int64(tossTransactionReconciliationMaxWindows)*int64(tossTransactionReconciliationWindow/time.Second), cursor.CursorTime)
}

func TestTossTestTransactionReconciliationClampsExistingCursorToThreeDayRange(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey = "test_sk_transaction_three_day_range"
		aliasKey  = "test-transaction-legacy-alias"
	)
	now := time.Unix(model.GetDBTimestamp(), 0).Truncate(time.Second)
	source := tossTransactionCredentialSource{
		SourceKey: tossTransactionSourceKey("", secretKey),
		SecretKey: secretKey,
		CursorAliases: []model.TossTransactionReconciliationCursorAlias{
			{SourceKey: aliasKey},
		},
	}
	require.NoError(t, model.DB.Create(&[]model.TossTransactionReconciliationCursor{
		{SourceKey: source.SourceKey, CursorTime: now.Add(-30 * 24 * time.Hour).Unix(), UpdateTime: now.Unix()},
		{SourceKey: aliasKey, CursorTime: now.Add(-40 * 24 * time.Hour).Unix(), UpdateTime: now.Unix()},
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	providerFloor := now.Add(-tossTransactionTestEnvironmentLookback).
		Add(tossTransactionTestEnvironmentSafetyMargin)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		requests++
		start, err := time.ParseInLocation("2006-01-02T15:04:05", request.URL.Query().Get("startDate"), seoul)
		require.NoError(t, err)
		require.False(t, start.Before(providerFloor), "test transaction lookup must stay inside Toss's three-day range")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[]`)),
		}, nil
	})}

	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 3, requests)
	var cursors []model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.Where("source_key IN ?", []string{source.SourceKey, aliasKey}).Order("source_key").Find(&cursors).Error)
	require.Len(t, cursors, 2)
	for i := range cursors {
		require.EqualValues(t, now.Unix(), cursors[i].CursorTime, "target and legacy alias must advance together")
	}
}

func TestTossTransactionDurablePageCursorResumesAfterRunBudgetEnds(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_durable_page_resume"
	source := tossTransactionCredentialSource{
		SourceKey: tossTransactionSourceKey("", secretKey),
		SecretKey: secretKey,
	}
	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey:  source.SourceKey,
		CursorTime: cursorTime,
		UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	startingAfter := make([]string, 0, 2)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		startingAfter = append(startingAfter, request.URL.Query().Get("startingAfter"))
		body := `[{"transactionKey":"durable_tx_1","status":"READY"},{"transactionKey":"durable_tx_2","status":"READY"}]`
		if request.URL.Query().Get("startingAfter") == "durable_tx_2" {
			body = `[]`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	_, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 2, 1,
	)
	require.ErrorIs(t, err, errTossTransactionPageLimit)
	var checkpoint model.TossTransactionReconciliationPageCursor
	require.NoError(t, model.DB.Where(
		"source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical,
	).First(&checkpoint).Error)
	require.Equal(t, "durable_tx_2", checkpoint.StartingAfter)

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end.Add(time.Hour), 2, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd,
		"a later scheduler now must not widen a partially scanned persisted interval")
	require.Equal(t, []string{"", "durable_tx_2"}, startingAfter,
		"the next scheduler run must continue after the last fully applied page")
	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount, "a completed provider interval must remove its page checkpoint")
}

func TestTossTransactionDurableCursorCheckpointsEachAppliedRow(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey = "live_sk_transaction_row_checkpoint"
		clientKey = "live_ck_transaction_row_checkpoint"
		orderID   = "toss_transaction_row_checkpoint"
	)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(model.TossClientKeyFingerprint(clientKey), secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: model.TossClientKeyFingerprint(clientKey),
	}
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Money: 1, TradeNo: orderID,
		ProviderClientKeyHash: source.MIDFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}).Error)
	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	transactionCursors := make([]string, 0, 2)
	firstRun := true
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			cursor := request.URL.Query().Get("startingAfter")
			transactionCursors = append(transactionCursors, cursor)
			body := `[{"transactionKey":"row_checkpoint_1","status":"READY"},{"transactionKey":"row_checkpoint_2","paymentKey":"pay_transaction_row_checkpoint","orderId":"toss_transaction_row_checkpoint","status":"DONE"}]`
			if cursor == "row_checkpoint_1" {
				body = `[{"transactionKey":"row_checkpoint_2","status":"READY"}]`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		case "/v1/payments/pay_transaction_row_checkpoint":
			if firstRun {
				firstRun = false
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"paymentKey":`)),
				}, nil
			}
			return nil, errors.New("unexpected repeated payment lookup")
		default:
			return nil, errors.New("unexpected request: " + request.URL.EscapedPath())
		}
	})}

	_, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 2, 1,
	)
	require.Error(t, err)
	var checkpoint model.TossTransactionReconciliationPageCursor
	require.NoError(t, model.DB.Where(
		"source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical,
	).First(&checkpoint).Error)
	require.Equal(t, "row_checkpoint_1", checkpoint.StartingAfter,
		"a later row failure must retain progress through every fully applied earlier row")
	var transientMismatchCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("event_type = ?", model.TossPaymentEventTypeFinancialMismatch).
		Count(&transientMismatchCount).Error)
	require.Zero(t, transientMismatchCount,
		"a malformed/transient Payment response must pin the cursor without being dead-lettered")

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 2, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, []string{"", "row_checkpoint_1"}, transactionCursors)
}

func TestTossTransactionPermanentMismatchIsDeadLetteredBeforeLaterRowSettles(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey          = "live_sk_transaction_permanent_mismatch"
		clientKey          = "live_ck_transaction_permanent_mismatch"
		mismatchOrderID    = "toss_transaction_permanent_mismatch"
		mismatchPaymentKey = "pay_transaction_permanent_mismatch"
		validOrderID       = "toss_transaction_after_mismatch"
		validPaymentKey    = "pay_transaction_after_mismatch"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(midFingerprint, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: midFingerprint,
	}
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 111,
			TradeNo: mismatchOrderID, ProviderOrderId: mismatchOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 321,
			TradeNo: validOrderID, ProviderOrderId: validOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"permanent_mismatch_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"valid_after_mismatch_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				mismatchPaymentKey,
				mismatchOrderID,
				validPaymentKey,
				validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + mismatchPaymentKey:
			// The provider response is authenticated and well-formed, but its paid
			// amount cannot satisfy the immutable local order. Retrying cannot fix it.
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":900,"balanceAmount":900,"currency":"KRW","method":"카드","card":{"amount":900}}`,
				mismatchPaymentKey,
				mismatchOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey,
				validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, 3, requests, "the permanent row and the following valid row must both be verified")

	var mismatchEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "permanent_mismatch_tx").First(&mismatchEvent).Error)
	require.Equal(t, model.TossPaymentEventTypeFinancialMismatch, mismatchEvent.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, mismatchEvent.ReconciliationStatus)
	require.Equal(t, mismatchOrderID, mismatchEvent.OrderId)
	require.Equal(t, mismatchPaymentKey, mismatchEvent.PaymentKey)
	require.Equal(t, "topup_fulfillment_contract_mismatch", mismatchEvent.ResolutionNote)
	require.EqualValues(t, 900, mismatchEvent.OriginalAmount)
	require.EqualValues(t, 900, mismatchEvent.BalanceAmount)
	require.Contains(t, mismatchEvent.ProviderPayload, source.SourceKey)
	require.Contains(t, mismatchEvent.ProviderPayload, `"reason":"topup_fulfillment_contract_mismatch"`)
	require.NotContains(t, mismatchEvent.ProviderPayload, secretKey,
		"dead-letter evidence must identify the credential namespace without storing its secret")

	var mismatchTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", mismatchOrderID).First(&mismatchTopUp).Error)
	require.Equal(t, common.TopUpStatusPending, mismatchTopUp.Status)
	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	require.Equal(t, validPaymentKey, validTopUp.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota, "only the valid second transaction may credit quota")

	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount, "the dead-lettered row must not pin the completed page cursor")
}

func TestTossTransactionDeadLetterWriteFailurePinsCheckpointUntilEvidenceIsDurable(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey  = "live_sk_transaction_dead_letter_failure"
		orderID    = "toss_transaction_dead_letter_failure"
		paymentKey = "pay_transaction_dead_letter_failure"
	)
	sourceMID := model.TossClientKeyFingerprint("live_ck_transaction_dead_letter_source")
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(sourceMID, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: sourceMID,
	}
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: sourceMID,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Migrator().DropTable(&model.TossPaymentEvent{}))

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"dead_letter_failure_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				paymentKey, orderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + paymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"BILLING","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				paymentKey, orderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	_, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.Error(t, err)
	require.Equal(t, 2, requests)
	var checkpoint model.TossTransactionReconciliationPageCursor
	require.NoError(t, model.DB.Where(
		"source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical,
	).First(&checkpoint).Error)
	require.Empty(t, checkpoint.StartingAfter,
		"the transactionKey must not checkpoint until its Payment/Transaction evidence is durable")

	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}))
	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, 4, requests, "the pinned provider row must be fetched again after storage recovers")
	var mismatch model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "dead_letter_failure_tx").First(&mismatch).Error)
	require.Equal(t, "credited_topup_fulfillment_identity_mismatch", mismatch.ResolutionNote)
	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount)
}

func TestTossTransactionIncompleteCancellationPinsUntilCompletedDetailsAreVisible(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey       = "live_sk_transaction_cancel_visibility"
		clientKey       = "live_ck_transaction_cancel_visibility"
		cancelOrderID   = "toss_transaction_cancel_visibility"
		cancelPayment   = "pay_transaction_cancel_visibility"
		validOrderID    = "toss_transaction_after_cancel_visibility"
		validPaymentKey = "pay_transaction_after_cancel_visibility"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(midFingerprint, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: midFingerprint,
	}
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{
			UserId: 71, Amount: 1000, TradeNo: cancelOrderID, ProviderOrderId: cancelPayment,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 321,
			TradeNo: validOrderID, ProviderOrderId: validOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	completedDetailsVisible := false
	cancelLookups := 0
	validLookups := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"cancel_visibility_tx_1","paymentKey":%q,"orderId":%q,"method":"카드","status":"CANCELED","currency":"KRW","amount":1000},{"transactionKey":"after_cancel_visibility_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				cancelPayment, cancelOrderID, validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + cancelPayment:
			cancelLookups++
			cancels := `[{"cancelAmount":500,"transactionKey":"cancel_visibility_part_1","cancelStatus":"DONE"}]`
			if completedDetailsVisible {
				cancels = `[{"cancelAmount":500,"transactionKey":"cancel_visibility_part_1","cancelStatus":"DONE"},{"cancelAmount":500,"transactionKey":"cancel_visibility_part_2","cancelStatus":"DONE"}]`
			}
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":%s}`,
				cancelPayment, cancelOrderID, cancels,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			validLookups++
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	_, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.Error(t, err)
	require.Equal(t, 1, cancelLookups)
	require.Zero(t, validLookups, "later transactions must not pass an incompletely visible cancellation")
	var checkpoint model.TossTransactionReconciliationPageCursor
	require.NoError(t, model.DB.Where(
		"source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical,
	).First(&checkpoint).Error)
	require.Empty(t, checkpoint.StartingAfter)
	var mismatchCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("event_type = ?", model.TossPaymentEventTypeFinancialMismatch).
		Count(&mismatchCount).Error)
	require.Zero(t, mismatchCount, "an incomplete cancel list is retryable replica lag, not a dead letter")

	completedDetailsVisible = true
	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, 2, cancelLookups)
	require.Equal(t, 1, validLookups)
	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota)
}

func TestTossTransactionPartialWalletCancellationDoesNotPinLaterTopUp(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey       = "live_sk_transaction_wallet_partial"
		clientKey       = "live_ck_transaction_wallet_partial"
		walletPayment   = "pay_transaction_wallet_partial"
		validOrderID    = "toss_transaction_after_wallet_partial"
		validPaymentKey = "pay_transaction_after_wallet_partial"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(midFingerprint, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: midFingerprint,
	}
	billingKeyID, err := model.StoreTossBillingKeyWithSecret(
		71, "transaction-wallet-customer", "transaction-wallet-billing-key", "card", "****1111", secretKey,
	)
	require.NoError(t, err)
	activeKey := fmt.Sprintf("%s:%d:%s", model.TopUpTargetTypeUser, 71, model.WalletAutoRechargeTypeScheduled)
	policy := model.WalletAutoRecharge{
		Type: model.WalletAutoRechargeTypeScheduled, TargetType: model.TopUpTargetTypeUser,
		TargetId: 71, OwnerUserId: 71, BillingKeyId: billingKeyID, Amount: 1000,
		IntervalUnit: model.WalletAutoRechargeIntervalMonth, IntervalValue: 1,
		Status: model.WalletAutoRechargeStatusActive, ActiveKey: &activeKey,
		NextChargeTime:        time.Now().Add(time.Hour).Unix(),
		ProviderClientKeyHash: midFingerprint,
	}
	require.NoError(t, model.DB.Create(&policy).Error)
	walletOrderID := fmt.Sprintf("wallet_auto_%d_transaction_partial", policy.Id)
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{
			UserId: 71, TargetType: model.TopUpTargetTypeUser, TargetId: 71,
			Amount: 1000, TradeNo: walletOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 321,
			TradeNo: validOrderID, ProviderOrderId: validOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"wallet_partial_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"PARTIAL_CANCELED","currency":"KRW","amount":1000},{"transactionKey":"after_wallet_partial_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				walletPayment, walletOrderID, validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + walletPayment:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"BILLING","orderId":%q,"status":"PARTIAL_CANCELED","totalAmount":1000,"balanceAmount":300,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":700,"transactionKey":"wallet_partial_cancel_tx","cancelStatus":"DONE"}]}`,
				walletPayment, walletOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where(
		"order_id = ? AND event_type = ?", walletOrderID, model.TossPaymentEventTypeCancellation,
	).First(&cancellation).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
	require.EqualValues(t, 700, cancellation.CancelAmount)
	var reloadedPolicy model.WalletAutoRecharge
	require.NoError(t, model.DB.First(&reloadedPolicy, policy.Id).Error)
	require.Equal(t, model.WalletAutoRechargeStatusFailed, reloadedPolicy.Status)
	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota)
	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount)
}

func TestReservedTossProjectOrderIDRecognitionIsNarrow(t *testing.T) {
	for _, orderID := range []string{
		"toss_" + strings.Repeat("a", 40),
		"toss_sub_" + strings.Repeat("b", 40),
		"trn_" + strings.Repeat("c", 40),
		"twa_" + strings.Repeat("d", 40),
		"toss_sub_renew_12_1760000000",
		"toss_sub_renew_12_1760000000_2",
		"wallet_auto_12_1760000000",
		"wallet_auto_12_2026071216_3",
	} {
		require.True(t, isReservedTossProjectOrderID(orderID), orderID)
	}
	for _, orderID := range []string{
		"toss_shared_foreign_order",
		"toss_" + strings.Repeat("A", 40),
		"toss_" + strings.Repeat("a", 39),
		"toss_sub_renew_invalid",
		"trn_" + strings.Repeat("g", 40),
		"twa_short",
		"wallet_auto_12_foreign",
		"shared_foreign_order_123",
	} {
		require.False(t, isReservedTossProjectOrderID(orderID), orderID)
	}
}

func TestTossTransactionMissingProjectOrderIsDeadLetteredWithoutPaymentLookup(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_missing_local_order"
	missingOrderID := "toss_" + strings.Repeat("a", 40)
	otherMIDOrderID := "toss_" + strings.Repeat("b", 40)
	foreignOrderID := "shared_foreign_order_123"
	sourceMID := model.TossClientKeyFingerprint("live_ck_transaction_missing_local_order")
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(sourceMID, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: sourceMID,
	}
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: otherMIDOrderID,
		ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_other_transaction_mid"),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.EscapedPath() != "/v1/transactions" {
			return nil, errors.New("missing/foreign local-order classification must not issue a Payment GET")
		}
		body := fmt.Sprintf(
			`[{"transactionKey":"missing_local_tx","paymentKey":"pay_missing_local","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"other_mid_local_tx","paymentKey":"pay_other_mid_local","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"foreign_shared_tx","paymentKey":"pay_foreign_shared","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
			missingOrderID, otherMIDOrderID, foreignOrderID,
		)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, 1, requests)

	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_type = ?", model.TossPaymentEventTypeFinancialMismatch).Find(&events).Error)
	require.Len(t, events, 1, "foreign shared-MID orders and local orders proven to another MID stay ignored")
	require.Equal(t, missingOrderID, events[0].OrderId)
	require.Equal(t, "pay_missing_local", events[0].PaymentKey)
	require.Equal(t, "missing_local_tx", events[0].TransactionKey)
	require.Equal(t, "missing_local_order", events[0].ResolutionNote)
	require.Contains(t, events[0].ProviderPayload, `"reason":"missing_local_order"`)
	require.Contains(t, events[0].ProviderPayload, missingOrderID)
	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount)
}

func TestTossTransactionRestoredProjectOrderWithLostDiscriminatorIsDeadLetteredAndDoesNotPin(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey       = "live_sk_transaction_restored_discriminator"
		validOrderID    = "toss_transaction_after_restored_discriminator"
		validPaymentKey = "pay_transaction_after_restored_discriminator"
	)
	corruptTopUpOrderID := "toss_" + strings.Repeat("c", 40)
	corruptSubscriptionOrderID := "trn_" + strings.Repeat("d", 40)
	sourceMID := model.TossClientKeyFingerprint("live_ck_transaction_restored_discriminator")
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(sourceMID, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: sourceMID,
	}

	// Simulate a partial restore that retained immutable project order IDs but
	// lost only the mutable routing discriminator columns. Neither row may be
	// credited automatically from Transaction-only evidence.
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: corruptTopUpOrderID,
		Status: common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 71, PlanId: 1, TradeNo: corruptSubscriptionOrderID,
		Status: common.TopUpStatusPending,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Money: 1, Quota: 321,
		TradeNo: validOrderID, ProviderOrderId: validOrderID,
		ProviderClientKeyHash: sourceMID,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"restored_topup_tx","paymentKey":"pay_restored_topup","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"restored_subscription_tx","paymentKey":"pay_restored_subscription","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"valid_after_restored_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				corruptTopUpOrderID, corruptSubscriptionOrderID, validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)
	require.Equal(t, 2, requests,
		"corrupt restored rows must be dead-lettered without an unscoped Payment GET")

	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Where(
		"resolution_note = ?", "local_order_payment_discriminator_corrupt",
	).Order("transaction_key asc").Find(&events).Error)
	require.Len(t, events, 2)
	require.ElementsMatch(t,
		[]string{corruptTopUpOrderID, corruptSubscriptionOrderID},
		[]string{events[0].OrderId, events[1].OrderId},
	)
	for i := range events {
		require.Equal(t, model.TossReconciliationStatusRequired, events[i].ReconciliationStatus)
		require.Contains(t, events[i].ProviderPayload, `"reason":"local_order_payment_discriminator_corrupt"`)
	}

	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	require.Equal(t, validPaymentKey, validTopUp.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota)
}

func TestTossTransactionLocalOrderDisappearanceAfterAuthoritativeLookupIsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	source := tossTransactionCredentialSource{
		MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_disappeared_order"),
	}

	fulfillment := &tossConfirmResponse{
		PaymentKey:  "pay_transaction_disappeared_fulfillment",
		Type:        "NORMAL",
		OrderId:     "toss_transaction_disappeared_fulfillment",
		Status:      "DONE",
		TotalAmount: 1000, BalanceAmount: 1000,
		Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}
	err := applyTossTransactionFulfillment(context.Background(), source, fulfillment)
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "local_order_disappeared_during_fulfillment", permanent.reason)
	require.Same(t, fulfillment, permanent.payment)
	require.ErrorIs(t, err, model.ErrTopUpNotFound)

	cancellation := &tossConfirmResponse{
		PaymentKey:  "pay_transaction_disappeared_cancellation",
		Type:        "NORMAL",
		OrderId:     "toss_transaction_disappeared_cancellation",
		Status:      "CANCELED",
		TotalAmount: 1000, BalanceAmount: 0,
		Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 1000, TransactionKey: "tx_transaction_disappeared_cancellation", CancelStatus: "DONE",
		}},
	}
	err = applyTossTransactionCancellation(context.Background(), source, cancellation)
	permanent = nil
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "local_order_disappeared_during_cancellation", permanent.reason)
	require.Same(t, cancellation, permanent.payment)
	require.ErrorIs(t, err, model.ErrTopUpNotFound)

	for _, tc := range []struct {
		name       string
		auth       *tossConfirmResponse
		apply      func(*tossConfirmResponse) error
		wantReason string
	}{
		{
			name: "fulfillment",
			auth: &tossConfirmResponse{
				PaymentKey: "pay_transaction_changed_fulfillment", Type: "NORMAL",
				OrderId: "toss_transaction_changed_fulfillment", Status: "DONE",
				TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW", Method: "카드",
				Card: &tossPaymentCard{Amount: 1000},
			},
			apply: func(auth *tossConfirmResponse) error {
				return applyTossTransactionFulfillment(context.Background(), source, auth)
			},
			wantReason: "local_order_payment_discriminator_changed_during_fulfillment",
		},
		{
			name: "cancellation",
			auth: &tossConfirmResponse{
				PaymentKey: "pay_transaction_changed_cancellation", Type: "NORMAL",
				OrderId: "toss_transaction_changed_cancellation", Status: "CANCELED",
				TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW", Method: "카드",
				Card: &tossPaymentCard{Amount: 1000},
				Cancels: []tossPaymentCancel{{
					CancelAmount: 1000, TransactionKey: "tx_transaction_changed_cancellation", CancelStatus: "DONE",
				}},
			},
			apply: func(auth *tossConfirmResponse) error {
				return applyTossTransactionCancellation(context.Background(), source, auth)
			},
			wantReason: "local_order_payment_discriminator_changed_during_cancellation",
		},
	} {
		t.Run("discriminator changed "+tc.name, func(t *testing.T) {
			require.NoError(t, model.DB.Create(&model.TopUp{
				UserId: 71, Amount: 1000, TradeNo: tc.auth.OrderId,
				PaymentMethod: model.PaymentMethodStripe, PaymentProvider: model.PaymentProviderStripe,
				Status: common.TopUpStatusPending,
			}).Error)
			err := tc.apply(tc.auth)
			var permanent *tossTransactionPermanentMismatch
			require.ErrorAs(t, err, &permanent)
			require.Equal(t, tc.wantReason, permanent.reason)
			require.Same(t, tc.auth, permanent.payment)
		})
	}
}

func TestApplyTossTransactionFulfillmentClassifiesExistingPaymentKeyConflictAsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey          = "live_ck_transaction_binding_conflict"
		orderID            = "toss_transaction_binding_conflict"
		boundPaymentKey    = "pay_transaction_already_bound"
		observedPaymentKey = "pay_transaction_new_done"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Money: 1, Quota: 111,
		TradeNo: orderID, ProviderOrderId: boundPaymentKey,
		ProviderClientKeyHash: midFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}).Error)

	err := applyTossTransactionFulfillment(context.Background(), tossTransactionCredentialSource{
		SourceKey:      "source_transaction_binding_conflict",
		SecretKey:      "live_sk_transaction_binding_conflict",
		MIDFingerprint: midFingerprint,
	}, &tossConfirmResponse{
		PaymentKey:    observedPaymentKey,
		Type:          "NORMAL",
		OrderId:       orderID,
		Status:        "DONE",
		TotalAmount:   1000,
		BalanceAmount: 1000,
		Currency:      "KRW",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 1000},
	})
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "topup_fulfillment_payment_key_conflict", permanent.reason)
	require.ErrorIs(t, err, model.ErrTossPaymentKeyConflict)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, boundPaymentKey, stored.ProviderOrderId)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
}

func TestApplyTossTransactionFulfillmentClassifiesMissingSettlementTargetAsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		orderID    = "toss_transaction_missing_settlement_target"
		paymentKey = "pay_transaction_missing_settlement_target"
	)
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_missing_settlement_target")
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 999999, Amount: 1000, Quota: 321,
		TradeNo: orderID, ProviderOrderId: orderID,
		ProviderClientKeyHash: midFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}).Error)
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "DONE", TotalAmount: 1000, BalanceAmount: 1000,
		Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
	}
	source := tossTransactionCredentialSource{MIDFingerprint: midFingerprint}

	for i := 0; i < 2; i++ {
		err := applyTossTransactionFulfillment(context.Background(), source, auth)
		var permanent *tossTransactionPermanentMismatch
		require.ErrorAs(t, err, &permanent)
		require.Equal(t, "topup_fulfillment_settlement_target_missing", permanent.reason)
		require.ErrorIs(t, err, model.ErrTossTopUpSettlementTargetMissing)
	}
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusPending, stored.Status)
	require.Equal(t, orderID, stored.ProviderOrderId)
}

func TestTossTransactionRefundFinalizerInvariantConflictIsPermanentAfterLedgerWrite(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		orderID        = "toss_transaction_refund_finalizer_conflict"
		paymentKey     = "pay_transaction_refund_finalizer_conflict"
		transactionKey = "cancel_transaction_refund_finalizer_conflict"
	)
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_refund_finalizer_conflict")
	topUp := model.TopUp{
		UserId: 71, Amount: 1000, Quota: 321,
		TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: midFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err := model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey:  model.TossTopUpRefundRequiredEventKey(orderID, paymentKey),
		EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId:   orderID, PaymentKey: paymentKey, Status: "DONE",
		OriginalAmount: 1000, BalanceAmount: 1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	// Model a legacy/incomplete-restore invariant violation: quota was already
	// granted despite an active durable refund fence.
	require.NoError(t, model.DB.Model(&model.TopUp{}).Where("id = ?", topUp.Id).
		Update("status", common.TopUpStatusSuccess).Error)
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0,
		Currency: "KRW", Method: "카드", Card: &tossPaymentCard{Amount: 1000},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 1000, TransactionKey: transactionKey, CancelStatus: "DONE",
		}},
	}
	source := tossTransactionCredentialSource{MIDFingerprint: midFingerprint}

	for i := 0; i < 2; i++ {
		err = applyTossTransactionCancellation(context.Background(), source, auth)
		var permanent *tossTransactionPermanentMismatch
		require.ErrorAs(t, err, &permanent)
		require.Equal(t, "refund_fenced_topup_finalization_conflict", permanent.reason)
		require.ErrorIs(t, err, model.ErrTopUpStatusInvalid)
	}
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", transactionKey).First(&cancellation).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
}

func TestTossTransactionTopUpPermanentClassifiersKeepTransientErrorsRetryable(t *testing.T) {
	auth := &tossConfirmResponse{PaymentKey: "pay_classifier", OrderId: "toss_classifier"}
	transientErrors := []error{
		context.DeadlineExceeded,
		gorm.ErrRecordNotFound,
		errors.New("temporary database failure"),
	}
	classifiers := []struct {
		name string
		call func(error) error
	}{
		{
			name: "refund finalizer",
			call: func(err error) error {
				return classifyTossTransactionRefundFinalizationError(auth, err)
			},
		},
		{
			name: "settlement target",
			call: func(err error) error {
				return classifyTossTransactionTopUpSettlementTargetError(auth, err)
			},
		},
	}
	for _, classifier := range classifiers {
		for _, transientErr := range transientErrors {
			t.Run(classifier.name+"/"+transientErr.Error(), func(t *testing.T) {
				err := classifier.call(transientErr)
				require.ErrorIs(t, err, transientErr)
				var permanent *tossTransactionPermanentMismatch
				require.False(t, errors.As(err, &permanent))
			})
		}
	}

	for _, immutableErr := range []error{
		model.ErrTopUpStatusInvalid,
		model.ErrPaymentMethodMismatch,
		model.ErrTossPaymentKeyConflict,
	} {
		err := classifyTossTransactionRefundFinalizationError(
			auth, fmt.Errorf("wrapped immutable conflict: %w", immutableErr),
		)
		var permanent *tossTransactionPermanentMismatch
		require.ErrorAs(t, err, &permanent)
		require.Equal(t, "refund_fenced_topup_finalization_conflict", permanent.reason)
		require.ErrorIs(t, err, immutableErr)
	}

	err := classifyTossTransactionTopUpSettlementTargetError(
		auth, fmt.Errorf("wrapped immutable target: %w", model.ErrTossTopUpSettlementTargetMissing),
	)
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "topup_fulfillment_settlement_target_missing", permanent.reason)
	require.ErrorIs(t, err, model.ErrTossTopUpSettlementTargetMissing)
}

func TestTossTransactionPaymentKeyConflictIsDeadLetteredBeforeLaterRowSettles(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey          = "live_sk_transaction_binding_poison"
		clientKey          = "live_ck_transaction_binding_poison"
		conflictOrderID    = "toss_transaction_binding_poison"
		boundPaymentKey    = "pay_transaction_binding_existing"
		observedPaymentKey = "pay_transaction_binding_observed"
		validOrderID       = "toss_transaction_after_binding_poison"
		validPaymentKey    = "pay_transaction_after_binding_poison"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(midFingerprint, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: midFingerprint,
	}
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 111,
			TradeNo: conflictOrderID, ProviderOrderId: boundPaymentKey,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 321,
			TradeNo: validOrderID, ProviderOrderId: validOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			body := fmt.Sprintf(
				`[{"transactionKey":"binding_poison_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"valid_after_binding_poison_tx","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				observedPaymentKey,
				conflictOrderID,
				validPaymentKey,
				validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + observedPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				observedPaymentKey,
				conflictOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey,
				validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	completedEnd, err := reconcileTossTransactionWindowDurable(
		context.Background(), source, model.TossTransactionPageLaneHistorical,
		cursorTime, start, end, 10, 1,
	)
	require.NoError(t, err)
	require.EqualValues(t, end.Unix(), completedEnd)

	var mismatchEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "binding_poison_tx").First(&mismatchEvent).Error)
	require.Equal(t, "topup_fulfillment_payment_key_conflict", mismatchEvent.ResolutionNote)
	require.Equal(t, observedPaymentKey, mismatchEvent.PaymentKey)
	require.Equal(t, model.TossReconciliationStatusRequired, mismatchEvent.ReconciliationStatus)

	var conflictTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", conflictOrderID).First(&conflictTopUp).Error)
	require.Equal(t, boundPaymentKey, conflictTopUp.ProviderOrderId)
	require.Equal(t, common.TopUpStatusPending, conflictTopUp.Status)
	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	require.Equal(t, validPaymentKey, validTopUp.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota)
}

func TestTossTransactionSubscriptionImmutableContractErrorsArePermanent(t *testing.T) {
	tests := []struct {
		name   string
		status string
		reason string
		apply  func(context.Context, tossTransactionCredentialSource, *tossConfirmResponse) error
	}{
		{
			name:   "fulfillment",
			status: "DONE",
			reason: "subscription_fulfillment_contract_unresolvable",
			apply:  applyTossTransactionFulfillment,
		},
		{
			name:   "cancellation",
			status: "CANCELED",
			reason: "subscription_cancellation_contract_unresolvable",
			apply:  applyTossTransactionCancellation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTossTransactionReconciliationTest(t)
			orderID := "toss_transaction_corrupt_plan_" + tt.name
			midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_corrupt_plan_" + tt.name)
			require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
				UserId: 71, PlanId: 91001, Money: 1, TradeNo: orderID,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusPending, ProviderAmount: 0, PlanSnapshot: "{",
				ProviderClientKeyHash: midFingerprint,
			}).Error)

			err := tt.apply(context.Background(), tossTransactionCredentialSource{
				MIDFingerprint: midFingerprint,
			}, &tossConfirmResponse{
				PaymentKey:  "pay_transaction_corrupt_plan_" + tt.name,
				Type:        "BILLING",
				OrderId:     orderID,
				Status:      tt.status,
				TotalAmount: 1000,
				Currency:    "KRW",
			})
			var permanent *tossTransactionPermanentMismatch
			require.ErrorAs(t, err, &permanent)
			require.Equal(t, tt.reason, permanent.reason)
			require.ErrorIs(t, err, model.ErrSubscriptionPlanSnapshotInvalid)
		})
	}
}

func TestTossTransactionSubscriptionCancellationIdentityConflictsArePermanent(t *testing.T) {
	tests := []struct {
		name     string
		orderID  string
		mutate   func(*model.SubscriptionOrder)
		expected error
	}{
		{
			name:    "partial renewal association",
			orderID: "toss_transaction_renewal_identity_conflict",
			mutate: func(order *model.SubscriptionOrder) {
				subscriptionID := 901
				order.RenewalSubscriptionId = &subscriptionID
			},
			expected: model.ErrTossRenewalIdentityConflict,
		},
		{
			name:     "opaque renewal without association",
			orderID:  "trn_" + strings.Repeat("e", 40),
			mutate:   func(*model.SubscriptionOrder) {},
			expected: model.ErrTossRecurringOrderIDEvidenceCorrupt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTossTransactionReconciliationTest(t)
			midFingerprint := model.TossClientKeyFingerprint("live_ck_" + strings.ReplaceAll(tt.name, " ", "_"))
			order := model.SubscriptionOrder{
				UserId: 71, PlanId: 1, TradeNo: tt.orderID,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusSuccess, ProviderAmount: 1000, ProviderCurrency: "KRW",
				ProviderClientKeyHash: midFingerprint,
			}
			tt.mutate(&order)
			require.NoError(t, model.DB.Create(&order).Error)

			err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
				MIDFingerprint: midFingerprint,
			}, &tossConfirmResponse{
				PaymentKey:  "pay_" + strings.ReplaceAll(tt.name, " ", "_"),
				Type:        "BILLING",
				OrderId:     tt.orderID,
				Status:      "CANCELED",
				TotalAmount: 1000,
				Currency:    "KRW",
				Method:      "카드",
				Card:        &tossPaymentCard{Amount: 1000},
				Cancels: []tossPaymentCancel{{
					CancelAmount:   1000,
					TransactionKey: "cancel_" + strings.ReplaceAll(tt.name, " ", "_"),
					CancelStatus:   "DONE",
				}},
			})
			var permanent *tossTransactionPermanentMismatch
			require.ErrorAs(t, err, &permanent)
			require.Equal(t, "subscription_cancellation_identity_conflict", permanent.reason)
			require.ErrorIs(t, err, tt.expected)
			var event model.TossPaymentEvent
			require.NoError(t, model.DB.Where(
				"order_id = ? AND event_type = ?", tt.orderID, model.TossPaymentEventTypeCancellation,
			).First(&event).Error)
			require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
		})
	}
}

func TestTossTransactionLegacyMissingSubscriptionPlanIsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}))
	const orderID = "toss_transaction_missing_legacy_plan"
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_missing_legacy_plan")
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 71, PlanId: 91999, Money: 1, TradeNo: orderID,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: 0,
		ProviderClientKeyHash: midFingerprint,
	}).Error)

	err := applyTossTransactionFulfillment(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: midFingerprint,
	}, &tossConfirmResponse{
		PaymentKey: "pay_transaction_missing_legacy_plan", Type: "BILLING", OrderId: orderID,
		Status: "DONE", TotalAmount: 1000, BalanceAmount: 1000, Currency: "KRW",
	})
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "subscription_fulfillment_contract_unresolvable", permanent.reason)
}

func TestTossTransactionCancellationClassifiesOnlyImpossibleEvidenceAsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		orderID    = "toss_transaction_cancel_evidence"
		paymentKey = "pay_transaction_cancel_evidence"
	)
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_cancel_evidence")
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: midFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusSuccess,
	}).Error)
	source := tossTransactionCredentialSource{MIDFingerprint: midFingerprint}
	auth := &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW",
		Cancels: []tossPaymentCancel{
			{CancelAmount: 500, TransactionKey: "duplicate_cancel_tx", CancelStatus: "DONE"},
			{CancelAmount: 500, TransactionKey: "duplicate_cancel_tx", CancelStatus: "DONE"},
		},
	}

	err := applyTossTransactionCancellation(context.Background(), source, auth)
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "topup_cancellation_evidence_mismatch", permanent.reason)
	require.ErrorIs(t, err, errTossCancellationEvidenceInvalid)

	// A cancellation row may lead the Payment resource's completed cancel list.
	// Preserve that eventually-consistent case as retryable and cursor-pinning.
	auth.Cancels = []tossPaymentCancel{{
		CancelAmount: 500, TransactionKey: "visible_cancel_tx", CancelStatus: "DONE",
	}}
	err = applyTossTransactionCancellation(context.Background(), source, auth)
	require.Error(t, err)
	permanent = nil
	require.False(t, errors.As(err, &permanent))
	require.NotErrorIs(t, err, errTossCancellationEvidenceInvalid)
}

func TestTossTransactionCancellationEventKeyConflictIsPermanent(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		orderID        = "toss_transaction_cancel_event_conflict"
		paymentKey     = "pay_transaction_cancel_event_conflict"
		transactionKey = "cancel_transaction_event_conflict"
	)
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_cancel_event_conflict")
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: midFingerprint,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		Status:                common.TopUpStatusSuccess,
	}).Error)
	_, _, err := persistTossCancellationEvents(&tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 500, Currency: "KRW",
		Cancels: []tossPaymentCancel{{
			CancelAmount: 500, TransactionKey: transactionKey, CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)

	err = applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: midFingerprint,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW",
		Cancels: []tossPaymentCancel{{
			CancelAmount: 1000, TransactionKey: transactionKey, CancelStatus: "DONE",
		}},
	})
	var permanent *tossTransactionPermanentMismatch
	require.ErrorAs(t, err, &permanent)
	require.Equal(t, "topup_cancellation_evidence_mismatch", permanent.reason)
	require.ErrorIs(t, err, model.ErrTossPaymentEventKeyConflict)
}

func TestTossTransactionVirtualAccountDoneRegressionIsDurableAndDoesNotPinLaterRows(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey          = "live_sk_transaction_va_regression"
		clientKey          = "live_ck_transaction_va_regression"
		revertedOrderID    = "toss_transaction_va_reverted"
		revertedPaymentKey = "pay_transaction_va_reverted"
		validOrderID       = "toss_transaction_after_va_revert"
		validPaymentKey    = "pay_transaction_after_va_revert"
	)
	midFingerprint := model.TossClientKeyFingerprint(clientKey)
	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(midFingerprint, secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: midFingerprint,
	}
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 71).Update("quota", 111).Error)
	require.NoError(t, model.DB.Create(&[]model.TopUp{
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 111,
			TradeNo: revertedOrderID, ProviderOrderId: revertedPaymentKey,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusSuccess,
		},
		{
			UserId: 71, Amount: 1000, Money: 1, Quota: 321,
			TradeNo: validOrderID, ProviderOrderId: validOrderID,
			ProviderClientKeyHash: midFingerprint,
			PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusPending,
		},
	}).Error)

	start := time.Date(2026, time.July, 11, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	cursorTime := start.Add(tossTransactionReconciliationOverlap).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: cursorTime, UpdateTime: cursorTime,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	var transactionCursors []string
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			transactionCursors = append(transactionCursors, request.URL.Query().Get("startingAfter"))
			body := fmt.Sprintf(
				`[{"transactionKey":"va_done_before_bank_error","paymentKey":%q,"orderId":%q,"method":"가상계좌","status":"DONE","currency":"KRW","amount":1000},{"transactionKey":"valid_after_va_bank_error","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":1000}]`,
				revertedPaymentKey, revertedOrderID, validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + revertedPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"WAITING_FOR_DEPOSIT","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"가상계좌"}`,
				revertedPaymentKey, revertedOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		case "/v1/payments/" + validPaymentKey:
			body := fmt.Sprintf(
				`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				validPaymentKey, validOrderID,
			)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
		default:
			return nil, errors.New("unexpected provider request: " + request.URL.EscapedPath())
		}
	})}

	// Repeat the exact historical window. The mismatch event must be idempotent,
	// the later payment must remain settled exactly once, and neither pass may
	// leave the page checkpoint pinned on the regressed virtual-account row.
	for attempt := 0; attempt < 2; attempt++ {
		completedEnd, err := reconcileTossTransactionWindowDurable(
			context.Background(), source, model.TossTransactionPageLaneHistorical,
			cursorTime, start, end, 10, 1,
		)
		require.NoError(t, err)
		require.EqualValues(t, end.Unix(), completedEnd)
	}
	require.Equal(t, 6, requests)
	require.Equal(t, []string{"", ""}, transactionCursors,
		"a completed replay starts from the window boundary instead of a pinned row")

	var mismatchEvents []model.TossPaymentEvent
	require.NoError(t, model.DB.Where(
		"transaction_key = ? AND event_type = ?", "va_done_before_bank_error", model.TossPaymentEventTypeFinancialMismatch,
	).Find(&mismatchEvents).Error)
	require.Len(t, mismatchEvents, 1, "repeating the same provider evidence must not duplicate the operator incident")
	require.Equal(t, model.TossReconciliationStatusRequired, mismatchEvents[0].ReconciliationStatus)
	require.Equal(t, "virtual_account_deposit_reverted_to_waiting", mismatchEvents[0].ResolutionNote)
	require.Equal(t, "WAITING_FOR_DEPOSIT", mismatchEvents[0].Status)
	require.EqualValues(t, 1000, mismatchEvents[0].OriginalAmount)
	require.EqualValues(t, 1000, mismatchEvents[0].BalanceAmount)

	var validTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&validTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, validTopUp.Status)
	var revertedTopUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", revertedOrderID).First(&revertedTopUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, revertedTopUp.Status,
		"the durable unresolved event, not an unsafe automatic debit, owns the historical credit discrepancy")
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(432), user.Quota, "the later valid payment may credit quota only once across the replay")
	var checkpointCount int64
	require.NoError(t, model.DB.Model(&model.TossTransactionReconciliationPageCursor{}).
		Where("source_key = ? AND lane = ?", source.SourceKey, model.TossTransactionPageLaneHistorical).
		Count(&checkpointCount).Error)
	require.Zero(t, checkpointCount)
}

func TestTossTransactionSourceBudgetPreservesProviderTimeoutContract(t *testing.T) {
	budget, ok := tossTransactionSourceBudget(20*time.Minute, 20)
	require.True(t, ok)
	require.Equal(t, tossTransactionMinimumSourceBudget, budget,
		"too many sources must defer a rotating suffix instead of shrinking every source below the provider timeout")

	budget, ok = tossTransactionSourceBudget(20*time.Minute, 2)
	require.True(t, ok)
	require.Equal(t, 10*time.Minute, budget)

	budget, ok = tossTransactionSourceBudget(tossTransactionMinimumSourceBudget-time.Nanosecond, 1)
	require.False(t, ok)
	require.Zero(t, budget)
}

func TestTossTransactionReconciliationPartialRecentBootstrapCheckpointsAndResumes(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_recent_partial"
	source := tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey}
	now := time.Unix(model.GetDBTimestamp(), 0).Truncate(time.Second)
	initial := now.Add(-100 * 24 * time.Hour).Unix()
	_, err := model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, initial)
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	failBootstrap := true
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		requests++
		body := `[]`
		if failBootstrap && requests == 4 {
			body = `{"partial":`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	require.Error(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 4, requests)
	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	recentCutoff := now.Add(-tossTransactionReconciliationRecentDays * 24 * time.Hour).Unix()
	require.EqualValues(t, recentCutoff+3*int64(tossTransactionReconciliationWindow/time.Second), cursor.RecentCursorTime,
		"each fully scanned bootstrap window must survive a later timeout")
	require.EqualValues(t, initial, cursor.CursorTime, "backlog must not advance past a failed recent bootstrap")

	// A later successful run resumes at the fourth day rather than repeating the
	// completed prefix, then continues the bounded historical drain.
	failBootstrap = false
	requests = 0
	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 4+tossTransactionReconciliationMaxWindows, requests)
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.EqualValues(t, now.Unix(), cursor.RecentCursorTime)
}

func TestTossTransactionReconciliationPartialRecentIncrementCheckpointsAndResumes(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_transaction_recent_increment_partial"
	source := tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey}
	now := time.Unix(model.GetDBTimestamp(), 0).Truncate(time.Second)
	initial := now.Add(-100 * 24 * time.Hour).Unix()
	recent := now.Add(-36 * time.Hour).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: source.SourceKey, CursorTime: initial, RecentCursorTime: recent, UpdateTime: initial,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	failIncrement := true
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		requests++
		body := `[]`
		if failIncrement && requests == 2 {
			body = `{"partial":`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	require.Error(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 2, requests)
	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	checkpoint := recent + int64(tossTransactionReconciliationWindow/time.Second)
	require.EqualValues(t, checkpoint, cursor.RecentCursorTime, "a completed incremental day must be checkpointed")
	require.EqualValues(t, initial, cursor.CursorTime)

	failIncrement = false
	requests = 0
	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 1+tossTransactionReconciliationMaxWindows, requests,
		"resume must scan only the remaining recent tail before historical windows")
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.EqualValues(t, now.Unix(), cursor.RecentCursorTime)
}

func TestTossTransactionCancellationRequiresTypeForLocalPaymentClass(t *testing.T) {
	t.Run("subscription rejects NORMAL", func(t *testing.T) {
		setupTossTransactionReconciliationTest(t)
		const orderID = "toss_transaction_subscription_type"
		require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
			UserId: 71, PlanId: 1, TradeNo: orderID,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status: common.TopUpStatusSuccess, ProviderAmount: 1000, ProviderCurrency: "KRW",
			ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_subscription_type"),
		}).Error)

		err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
			MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_subscription_type"),
		}, &tossConfirmResponse{
			PaymentKey: "pay_subscription_type", Type: "NORMAL", OrderId: orderID,
			Status: "CANCELED", TotalAmount: 1000, Currency: "KRW", Method: "카드",
			Card: &tossPaymentCard{Amount: 1000},
		})
		require.ErrorContains(t, err, "unexpected payment type")
		var eventCount int64
		require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Count(&eventCount).Error)
		require.Zero(t, eventCount)
	})

	t.Run("wallet rejects NORMAL", func(t *testing.T) {
		setupTossTransactionReconciliationTest(t)
		const orderID = "wallet_auto_81_type_guard"
		require.NoError(t, model.DB.Create(&model.WalletAutoRecharge{
			Id: 81, OwnerUserId: 71, Status: model.WalletAutoRechargeStatusActive,
			ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_wallet_type"),
		}).Error)
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, Amount: 1000, TradeNo: orderID,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status:                common.TopUpStatusSuccess,
			ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_wallet_type"),
		}).Error)

		err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
			MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_wallet_type"),
		}, &tossConfirmResponse{
			PaymentKey: "pay_wallet_type", Type: "NORMAL", OrderId: orderID,
			Status: "CANCELED", TotalAmount: 1000, Currency: "KRW", Method: "카드",
			Card: &tossPaymentCard{Amount: 1000},
		})
		require.ErrorContains(t, err, "unexpected payment type")
	})

	t.Run("general top-up rejects BILLING", func(t *testing.T) {
		setupTossTransactionReconciliationTest(t)
		const orderID = "toss_transaction_topup_type"
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, Amount: 1000, TradeNo: orderID,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			Status:                common.TopUpStatusSuccess,
			ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_topup_type"),
		}).Error)

		err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
			MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_topup_type"),
		}, &tossConfirmResponse{
			PaymentKey: "pay_topup_type", Type: "BILLING", OrderId: orderID,
			Status: "CANCELED", TotalAmount: 1000, Currency: "KRW", Method: "카드",
			Card: &tossPaymentCard{Amount: 1000},
		})
		require.ErrorContains(t, err, "does not match the local order")
	})
}

func TestTossTransactionPartialCancellationBlocksInitialSubscriptionReplacement(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionPlan{}))
	const (
		orderID = "toss_transaction_partial_initial_subscription"
		midKey  = "live_ck_transaction_partial_initial"
	)
	plan := model.SubscriptionPlan{
		Id: 7201, Title: "Transaction partial cancellation plan", PriceAmount: 15, Currency: "USD",
		DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
	}
	require.NoError(t, model.DB.Create(&plan).Error)
	order := model.SubscriptionOrder{
		UserId: 71, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: orderID,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: 15000, ProviderCurrency: "KRW",
		BillingAttempted: true, ProviderClientKeyHash: model.TossClientKeyFingerprint(midKey),
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(&order, &plan))
	require.NoError(t, model.DB.Create(&order).Error)

	err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: model.TossClientKeyFingerprint(midKey),
	}, &tossConfirmResponse{
		PaymentKey: "pay_transaction_partial_initial", Type: "BILLING", OrderId: orderID,
		Status: "PARTIAL_CANCELED", TotalAmount: 15000, BalanceAmount: 5000, Currency: "KRW", Method: "카드",
		Card: &tossPaymentCard{Amount: 15000},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 10000, TransactionKey: "cancel_transaction_partial_initial", CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)
	require.NoError(t, model.DB.First(&order, order.Id).Error)
	require.Equal(t, common.TopUpStatusFailed, order.Status)

	replacement := &model.SubscriptionOrder{
		UserId: 71, PlanId: plan.Id, Money: plan.PriceAmount,
		TradeNo:       "toss_transaction_partial_initial_replacement",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending, ProviderAmount: 15000, ProviderCurrency: "KRW",
	}
	require.NoError(t, model.SetTossSubscriptionOrderPlanSnapshot(replacement, &plan))
	require.ErrorIs(t, model.CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan), model.ErrPersonalTossBillingInFlight)

	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeCancellation).First(&event).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, event.ReconciliationStatus)
	_, err = model.ResolveTossPaymentEventByAdmin(event.Id, 1, "partial cancellation reviewed")
	require.NoError(t, err)
	require.NoError(t, model.CreateTossSubscriptionOrderWithPurchaseReservation(replacement, &plan))
}

func TestTossTransactionReconciliationRecoversDONECheckoutWithoutCallbackOrWebhook(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey  = "live_sk_transaction_done_recovery"
		orderID    = "toss_transaction_done_recovery"
		paymentKey = "pay_transaction_done_recovery"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 15000, Money: 15, Quota: 321, TradeNo: orderID,
		ProviderOrderId: orderID, ProviderCredential: credential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint("live_ck_transaction_done_recovery"),
		PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		CreateTime: model.GetDBTimestamp() - 7200, CompleteTime: model.GetDBTimestamp() - 3600,
		Status: common.TopUpStatusExpired,
	}).Error)

	source := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(model.TossClientKeyFingerprint("live_ck_transaction_done_recovery"), secretKey),
		SecretKey:      secretKey,
		MIDFingerprint: model.TossClientKeyFingerprint("live_ck_transaction_done_recovery"),
	}
	now := time.Now().Truncate(time.Second)
	initial := now.Add(-time.Hour).Unix()
	_, err = model.GetOrCreateTossTransactionReconciliationCursor(source.SourceKey, initial)
	require.NoError(t, err)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(secretKey+":")), request.Header.Get("Authorization"))
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`[{"mId":"mid","transactionKey":"done_tx_recovery","paymentKey":"pay_transaction_done_recovery","orderId":"toss_transaction_done_recovery","method":"카드","status":"DONE","transactionAt":"2026-07-11T12:00:00+09:00","currency":"KRW","amount":15000}]`,
			))}, nil
		case "/v1/payments/pay_transaction_done_recovery":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_transaction_done_recovery","type":"NORMAL","orderId":"toss_transaction_done_recovery","status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000,"issuerCode":"11","acquirerCode":"11","number":"433012******1234","installmentPlanMonths":0,"isInterestFree":false,"approveNo":"00000000","useCardPoint":false,"cardType":"신용","ownerType":"개인","acquireStatus":"READY","receiptUrl":"https://example.invalid"}}`,
			))}, nil
		default:
			return nil, errors.New("unexpected Toss reconciliation request: " + request.URL.EscapedPath())
		}
	})}

	require.NoError(t, reconcileTossTransactionSource(context.Background(), source, now))
	require.Equal(t, 2, requests)

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
	require.Equal(t, paymentKey, topUp.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Equal(t, int64(321), user.Quota)

	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", source.SourceKey).Error)
	require.Equal(t, now.Unix(), cursor.CursorTime)
}

func TestTossTransactionReconciliationRejectsTestMIDPaymentForLiveTopUp(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		liveClientKey = "live_ck_cross_mid_target"
		liveSecretKey = "live_sk_cross_mid_target"
		testClientKey = "test_ck_cross_mid_attacker"
		testSecretKey = "test_sk_cross_mid_attacker"
		orderID       = "toss_cross_mid_same_order"
		paymentKey    = "pay_cross_mid_test_payment"
	)
	liveCredential, err := model.EncryptProviderCredential(liveSecretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 15000, Money: 15, Quota: 777, TradeNo: orderID,
		ProviderOrderId: orderID, ProviderCredential: liveCredential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(liveClientKey),
		PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		CreateTime: model.GetDBTimestamp() - 7200, Status: common.TopUpStatusExpired,
	}).Error)

	testSource := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(model.TossClientKeyFingerprint(testClientKey), testSecretKey),
		SecretKey:      testSecretKey,
		MIDFingerprint: model.TossClientKeyFingerprint(testClientKey),
	}

	// The in-lock defense must reject the cross-MID Payment even if a future
	// caller bypasses the Transaction-page prefilter.
	err = applyTossTransactionFulfillment(context.Background(), testSource, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 15000, BalanceAmount: 15000, Currency: "KRW", Method: "카드",
		Card: &tossPaymentCard{Amount: 15000},
	})
	require.ErrorIs(t, err, errTossTransactionCredentialNamespaceMismatch)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath(),
			"a cross-MID collision must be rejected before the authoritative Payment GET")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`[{"mId":"test-mid","transactionKey":"done_cross_mid","paymentKey":"pay_cross_mid_test_payment","orderId":"toss_cross_mid_same_order","method":"카드","status":"DONE","currency":"KRW","amount":15000}]`,
		))}, nil
	})}

	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, reconcileTossTransactionWindowWithLimits(context.Background(), testSource, start, start.Add(time.Hour), 10, 2))
	require.Equal(t, 1, requests)

	var topUp model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
	require.Equal(t, common.TopUpStatusExpired, topUp.Status)
	require.Equal(t, orderID, topUp.ProviderOrderId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 71).Error)
	require.Zero(t, user.Quota, "a test-key payment must not grant live-order quota")
}

func TestTossTransactionPartialCancellationKeepsRefundFencedTopUpRecoverable(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey  = "live_ck_transaction_refund_partial"
		orderID    = "toss_transaction_refund_partial"
		paymentKey = "pay_transaction_refund_partial"
	)
	mid := model.TossClientKeyFingerprint(clientKey)
	topUp := model.TopUp{
		UserId: 71, Amount: 1000, Quota: 777, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderClientKeyHash: mid,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}
	require.NoError(t, model.DB.Create(&topUp).Error)
	_, err := model.RecordTossTopUpRefundRequirementWithContext(context.Background(), &model.TossPaymentEvent{
		EventKey: model.TossTopUpRefundRequiredEventKey(orderID, paymentKey), EventType: model.TossPaymentEventTypeRefundRequired,
		OrderId: orderID, PaymentKey: paymentKey, Status: "DONE",
		OriginalAmount: 1000, BalanceAmount: 1000, ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	err = applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: mid,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 300, Currency: "KRW", Method: "카드",
		Card: &tossPaymentCard{Amount: 1000},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 700, RefundableAmount: 300, TransactionKey: "cancel_transaction_refund_partial", CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
}

func TestTossTransactionPartialCancellationQueuesRemainingUncreditedBalance(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey  = "live_ck_transaction_uncredited_partial"
		orderID    = "toss_transaction_uncredited_partial"
		paymentKey = "pay_transaction_uncredited_partial"
	)
	mid := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Quota: 777, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderClientKeyHash: mid,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)

	err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: mid,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "PARTIAL_CANCELED", TotalAmount: 1000, BalanceAmount: 300, Currency: "KRW", Method: "카드",
		Card: &tossPaymentCard{Amount: 1000},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 700, RefundableAmount: 300, TransactionKey: "cancel_transaction_uncredited_partial", CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	require.LessOrEqual(t, stored.ProviderRetryTime, model.GetDBTimestamp())

	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	require.EqualValues(t, 300, refundEvent.BalanceAmount)

	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_transaction_uncredited_partial").First(&cancellation).Error)
	require.EqualValues(t, 700, cancellation.CancelAmount)
}

func TestTossTransactionVirtualAccountPendingRefundQueuesUncreditedFence(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey  = "live_ck_transaction_va_refund_pending"
		orderID    = "toss_transaction_va_refund_pending"
		paymentKey = "pay_transaction_va_refund_pending"
	)
	mid := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: mid, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusPending,
	}).Error)

	err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: mid,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW",
		Method: "가상계좌", ApprovedAt: "2026-07-12T10:00:00+09:00",
		VirtualAccount: &tossPaymentVirtualAccount{RefundStatus: "PENDING"},
		Cancels: []tossPaymentCancel{{
			CancelAmount: 1000, RefundableAmount: 0, TransactionKey: "cancel_transaction_va_refund_pending", CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, model.TossTopUpStatusRefundPending, stored.Status)
	var refundEvent model.TossPaymentEvent
	require.NoError(t, model.DB.Where("event_key = ?", model.TossTopUpRefundRequiredEventKey(orderID, paymentKey)).First(&refundEvent).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, refundEvent.ReconciliationStatus)
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_transaction_va_refund_pending").First(&cancellation).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
}

func TestTossTransactionCreditedLegacyMethodCancellationPersistsLedger(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey  = "live_ck_transaction_credited_legacy_cancel"
		orderID    = "toss_transaction_credited_legacy_cancel"
		paymentKey = "pay_transaction_credited_legacy_cancel"
	)
	mid := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: orderID, ProviderOrderId: paymentKey,
		ProviderClientKeyHash: mid, PaymentMethod: model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss, Status: common.TopUpStatusSuccess,
	}).Error)

	err := applyTossTransactionCancellation(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: mid,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID,
		Status: "CANCELED", TotalAmount: 1000, BalanceAmount: 0, Currency: "KRW",
		Method: "계좌이체", TaxFreeAmount: 100,
		Cancels: []tossPaymentCancel{{
			CancelAmount: 1000, RefundableAmount: 0, TransactionKey: "cancel_transaction_credited_legacy", CancelStatus: "DONE",
		}},
	})
	require.NoError(t, err)

	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status, "credited quota requires separate manual reconciliation")
	var cancellation model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_transaction_credited_legacy").First(&cancellation).Error)
	require.Equal(t, model.TossPaymentEventTypeCancellation, cancellation.EventType)
	require.Equal(t, model.TossReconciliationStatusRequired, cancellation.ReconciliationStatus)
}

func TestTossTransactionLegacyCreditedContractMismatchDoesNotQueueRefund(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey  = "live_ck_transaction_legacy_credited"
		orderID    = "toss_transaction_legacy_credited"
		paymentKey = "pay_transaction_legacy_credited"
	)
	mid := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Quota: 777, TradeNo: orderID,
		ProviderOrderId: paymentKey, ProviderClientKeyHash: mid,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess,
	}).Error)

	err := applyTossTransactionFulfillment(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: mid,
	}, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "DONE",
		TotalAmount: 1000, BalanceAmount: 1000, TaxFreeAmount: 100,
		Currency: "KRW", Method: "계좌이체",
	})
	require.NoError(t, err, "a historical credited contract mismatch must not pin the transaction cursor")

	var refundCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).
		Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeRefundRequired).
		Count(&refundCount).Error)
	require.Zero(t, refundCount)
	var mismatch model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFinancialMismatch).First(&mismatch).Error)
	require.Equal(t, model.TossReconciliationStatusRequired, mismatch.ReconciliationStatus)
	var stored model.TopUp
	require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&stored).Error)
	require.Equal(t, common.TopUpStatusSuccess, stored.Status)
}

func TestTossTransactionReconciliationRejectsTestMIDCancellationForLiveTopUp(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		liveClientKey = "live_ck_cross_mid_cancel_target"
		testClientKey = "test_ck_cross_mid_cancel_attacker"
		testSecretKey = "test_sk_cross_mid_cancel_attacker"
		orderID       = "toss_cross_mid_cancel_order"
		paymentKey    = "pay_cross_mid_cancel_payment"
	)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 15000, Money: 15, Quota: 777, TradeNo: orderID,
		ProviderOrderId:       paymentKey,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(liveClientKey),
		PaymentMethod:         model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess,
	}).Error)
	testSource := tossTransactionCredentialSource{
		SourceKey:      tossTransactionSourceKey(model.TossClientKeyFingerprint(testClientKey), testSecretKey),
		SecretKey:      testSecretKey,
		MIDFingerprint: model.TossClientKeyFingerprint(testClientKey),
	}

	err := applyTossTransactionCancellation(context.Background(), testSource, &tossConfirmResponse{
		PaymentKey: paymentKey, Type: "NORMAL", OrderId: orderID, Status: "CANCELED",
		TotalAmount: 15000, BalanceAmount: 0, Currency: "KRW", Method: "카드",
		Card:    &tossPaymentCard{Amount: 15000},
		Cancels: []tossPaymentCancel{{CancelAmount: 15000, TransactionKey: "cancel_cross_mid", CancelStatus: "DONE"}},
	})
	require.ErrorIs(t, err, errTossTransactionCredentialNamespaceMismatch)

	var eventCount int64
	require.NoError(t, model.DB.Model(&model.TossPaymentEvent{}).Where("order_id = ?", orderID).Count(&eventCount).Error)
	require.Zero(t, eventCount, "a cancellation from another MID must not create a local reconciliation event")
}

func TestTossTransactionReconciliationSkipsUnprovableLocalOrderAndContinuesSourceWindow(t *testing.T) {
	for _, tc := range []struct {
		name               string
		providerCredential string
	}{
		{name: "snapshot removed"},
		{name: "snapshot corrupt", providerCredential: "not-an-encrypted-provider-credential"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTossTransactionReconciliationTest(t)
			orderID := "toss_unprovable_" + strings.ReplaceAll(tc.name, " ", "_")
			require.NoError(t, model.DB.Create(&model.TopUp{
				UserId: 71, Amount: 15000, Money: 15, Quota: 777, TradeNo: orderID,
				ProviderOrderId: orderID, ProviderCredential: tc.providerCredential,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusExpired,
			}).Error)

			source := tossTransactionCredentialSource{
				SourceKey: tossTransactionSourceKey("", "test_sk_unprovable_source"),
				SecretKey: "test_sk_unprovable_source",
			}
			validOrderID := orderID + "_valid"
			validPaymentKey := "pay_unprovable_source_valid"
			validCredential, err := model.EncryptProviderCredential(source.SecretKey)
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.TopUp{
				UserId: 71, Amount: 15000, Money: 15, Quota: 321, TradeNo: validOrderID,
				ProviderOrderId: validOrderID, ProviderCredential: validCredential,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusPending,
			}).Error)
			originalBase := tossAPIBase
			originalClient := http.DefaultClient
			t.Cleanup(func() {
				tossAPIBase = originalBase
				http.DefaultClient = originalClient
			})
			tossAPIBase = "https://api.test.tosspayments.local"
			requests := 0
			http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				switch request.URL.EscapedPath() {
				case "/v1/transactions":
					body := fmt.Sprintf(
						`[{"transactionKey":"done_unprovable","paymentKey":"pay_unprovable_source","orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":15000},{"transactionKey":"done_after_unprovable","paymentKey":%q,"orderId":%q,"method":"카드","status":"DONE","currency":"KRW","amount":15000}]`,
						orderID,
						validPaymentKey,
						validOrderID,
					)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
				case "/v1/payments/" + validPaymentKey:
					body := fmt.Sprintf(
						`{"paymentKey":%q,"type":"NORMAL","orderId":%q,"status":"DONE","totalAmount":15000,"balanceAmount":15000,"currency":"KRW","method":"카드","card":{"amount":15000}}`,
						validPaymentKey,
						validOrderID,
					)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
				default:
					t.Fatalf("unexpected provider request %s", request.URL.EscapedPath())
					return nil, errors.New("unexpected provider request")
				}
			})}

			start := time.Now().Add(-time.Hour).Truncate(time.Second)
			require.NoError(t, reconcileTossTransactionWindowWithLimits(
				context.Background(), source, start, start.Add(time.Hour), 10, 2,
			))
			require.Equal(t, 2, requests,
				"the unprovable row must not trigger Payment GET, while the following valid row still settles")
			var mismatch model.TossPaymentEvent
			require.NoError(t, model.DB.Where("transaction_key = ?", "done_unprovable").First(&mismatch).Error)
			require.Equal(t, model.TossPaymentEventTypeFinancialMismatch, mismatch.EventType)
			require.Equal(t, "local_credential_namespace_unprovable", mismatch.ResolutionNote)
			require.Equal(t, model.TossReconciliationStatusRequired, mismatch.ReconciliationStatus)

			var topUp model.TopUp
			require.NoError(t, model.DB.Where("trade_no = ?", orderID).First(&topUp).Error)
			require.Equal(t, common.TopUpStatusExpired, topUp.Status)
			var user model.User
			require.NoError(t, model.DB.First(&user, 71).Error)
			require.Equal(t, int64(321), user.Quota)
			topUp = model.TopUp{}
			require.NoError(t, model.DB.Where("trade_no = ?", validOrderID).First(&topUp).Error)
			require.Equal(t, common.TopUpStatusSuccess, topUp.Status)
			require.Equal(t, validPaymentKey, topUp.ProviderOrderId)
		})
	}
}

func TestTossTransactionLegacyNamespaceRequiresExactStoredCredential(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		checkoutSecret = "live_sk_legacy_checkout_namespace"
		attemptSecret  = "live_sk_legacy_attempt_namespace"
		wrongSecret    = "test_sk_legacy_wrong_namespace"
	)
	checkoutCredential, err := model.EncryptProviderCredential(checkoutSecret)
	require.NoError(t, err)
	attemptCredential, err := model.EncryptProviderCredential(attemptSecret)
	require.NoError(t, err)

	matches, err := tossTransactionSourceMatchesStoredNamespace(
		tossTransactionCredentialSource{SecretKey: checkoutSecret}, "", checkoutCredential,
	)
	require.NoError(t, err)
	require.True(t, matches)

	matches, err = tossTransactionSourceMatchesStoredNamespace(
		tossTransactionCredentialSource{SecretKey: wrongSecret}, "", checkoutCredential,
	)
	require.NoError(t, err)
	require.False(t, matches)

	for _, corruptFingerprint := range []string{
		"not-a-sha256-fingerprint",
		strings.ToUpper(model.TossClientKeyFingerprint("live_ck_uppercase_corrupt_fingerprint")),
		" " + model.TossClientKeyFingerprint("live_ck_whitespace_corrupt_fingerprint") + " ",
	} {
		require.False(t, model.IsValidTossClientKeyFingerprint(corruptFingerprint))
		matches, err = tossTransactionSourceMatchesStoredNamespace(
			tossTransactionCredentialSource{SecretKey: checkoutSecret}, corruptFingerprint, checkoutCredential,
		)
		require.NoError(t, err)
		require.True(t, matches, "a corrupt fingerprint must fall back to the exact stored credential")
		matches, err = tossTransactionSourceMatchesStoredNamespace(
			tossTransactionCredentialSource{SecretKey: wrongSecret}, corruptFingerprint, checkoutCredential,
		)
		require.NoError(t, err)
		require.False(t, matches)
	}

	_, err = tossTransactionSourceMatchesStoredNamespace(
		tossTransactionCredentialSource{SecretKey: checkoutSecret}, "", "",
	)
	require.ErrorContains(t, err, "cannot be proven")

	legacyOrder := &model.SubscriptionOrder{
		ProviderCredential: checkoutCredential, BillingAttemptCredential: attemptCredential,
	}
	matches, err = tossTransactionSourceMatchesSubscriptionOrder(
		tossTransactionCredentialSource{SecretKey: checkoutSecret}, legacyOrder,
	)
	require.NoError(t, err)
	require.False(t, matches, "the checkout credential must not override the exact credential pinned for the provider POST")
	matches, err = tossTransactionSourceMatchesSubscriptionOrder(
		tossTransactionCredentialSource{SecretKey: attemptSecret}, legacyOrder,
	)
	require.NoError(t, err)
	require.True(t, matches)

	const legacyWalletOrderID = "wallet_auto_91_legacy_namespace"
	require.NoError(t, model.DB.Create(&model.WalletAutoRecharge{
		Id: 91, OwnerUserId: 71, Status: model.WalletAutoRechargeStatusActive,
	}).Error)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, TradeNo: legacyWalletOrderID,
		ProviderCredential: checkoutCredential,
		PaymentMethod:      model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)
	status, local, err := localTossTransactionOrderStatus(
		context.Background(), tossTransactionCredentialSource{SecretKey: checkoutSecret}, legacyWalletOrderID,
	)
	require.NoError(t, err)
	require.True(t, local)
	require.Equal(t, common.TopUpStatusPending, status,
		"a legacy policy with no namespace columns may rely on its charge TopUp's exact credential proof")

	_, local, err = localTossTransactionOrderStatus(
		context.Background(), tossTransactionCredentialSource{SecretKey: wrongSecret}, legacyWalletOrderID,
	)
	require.NoError(t, err)
	require.False(t, local, "the legacy policy exception must never bypass the charge TopUp proof")

	require.NoError(t, model.DB.Model(&model.WalletAutoRecharge{}).Where("id = ?", 91).
		Update("provider_client_key_hash", model.TossClientKeyFingerprint("live_ck_other_wallet_namespace")).Error)
	_, local, err = localTossTransactionOrderStatus(
		context.Background(), tossTransactionCredentialSource{SecretKey: checkoutSecret}, legacyWalletOrderID,
	)
	require.NoError(t, err)
	require.False(t, local, "once the policy has namespace evidence it must independently match")
}

func TestDiscoverTossTransactionCredentialSourcesKeepsEveryReadableLegacySecret(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretA = "live_sk_legacy_transaction_a"
		secretB = "live_sk_legacy_transaction_b"
		secretC = "live_sk_fingerprinted_transaction_c"
		secretD = "live_sk_corrupt_fingerprint_transaction_d"
		secretE = "live_sk_corrupt_fingerprint_transaction_e"
	)
	encryptedA1, err := model.EncryptProviderCredential(secretA)
	require.NoError(t, err)
	encryptedA2, err := model.EncryptProviderCredential(secretA)
	require.NoError(t, err)
	encryptedB, err := model.EncryptProviderCredential(secretB)
	require.NoError(t, err)

	for i, credential := range []string{encryptedA1, encryptedA2, encryptedB, "corrupt-legacy-credential"} {
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, TradeNo: fmt.Sprintf("toss_legacy_source_%d", i), Amount: 1000,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			ProviderCredential: credential, ProviderClientKeyHash: "", CreateTime: int64(100 + i),
			Status: common.TopUpStatusSuccess,
		}).Error)
	}
	encryptedC, err := model.EncryptProviderCredential(secretC)
	require.NoError(t, err)
	fingerprintedHash := model.TossClientKeyFingerprint("live_ck_fingerprinted_transaction_c")
	for i, credential := range []string{encryptedC, "corrupt-newest-fingerprinted-credential"} {
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, TradeNo: fmt.Sprintf("toss_fingerprinted_source_%d", i), Amount: 1000,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			ProviderCredential: credential, ProviderClientKeyHash: fingerprintedHash, CreateTime: int64(200 + i),
			Status: common.TopUpStatusSuccess,
		}).Error)
	}
	encryptedD, err := model.EncryptProviderCredential(secretD)
	require.NoError(t, err)
	encryptedE, err := model.EncryptProviderCredential(secretE)
	require.NoError(t, err)
	for i, credential := range []string{encryptedD, encryptedE} {
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, TradeNo: fmt.Sprintf("toss_corrupt_fingerprint_source_%d", i), Amount: 1000,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			ProviderCredential: credential, ProviderClientKeyHash: "restored-invalid-shared-hash",
			CreateTime: int64(300 + i), Status: common.TopUpStatusSuccess,
		}).Error)
	}

	sources, err := discoverTossTransactionCredentialSources(context.Background())
	require.NoError(t, err)
	found := make(map[string]int)
	initial := make(map[string]int64)
	secrets := make(map[string]string)
	midFingerprints := make(map[string]string)
	for _, source := range sources {
		found[source.SourceKey]++
		initial[source.SourceKey] = source.InitialCursor
		secrets[source.SourceKey] = source.SecretKey
		midFingerprints[source.SourceKey] = source.MIDFingerprint
	}
	require.Equal(t, 1, found[tossTransactionSourceKey("", secretA)], "duplicate ciphertexts for one secret must collapse to one source")
	require.Equal(t, 1, found[tossTransactionSourceKey("", secretB)], "a second historical MID/secret must not be hidden by the newest legacy row")
	require.EqualValues(t, 100, initial[tossTransactionSourceKey("", secretA)])
	require.EqualValues(t, 102, initial[tossTransactionSourceKey("", secretB)])
	require.Equal(t, secretC, secrets[fingerprintedHash], "an unreadable newest row must fall back to an older readable credential for the same MID")
	require.Equal(t, fingerprintedHash, midFingerprints[fingerprintedHash])
	require.Equal(t, 1, found[tossTransactionSourceKey("", secretD)],
		"a malformed non-empty fingerprint must not merge one exact historical credential into another")
	require.Equal(t, 1, found[tossTransactionSourceKey("", secretE)])
	require.EqualValues(t, 300, initial[tossTransactionSourceKey("", secretD)])
	require.EqualValues(t, 301, initial[tossTransactionSourceKey("", secretE)])
	require.Empty(t, midFingerprints[tossTransactionSourceKey("", secretD)])
	require.Empty(t, midFingerprints[tossTransactionSourceKey("", secretE)])
	require.Empty(t, midFingerprints[tossTransactionSourceKey("", secretA)],
		"an unmapped legacy secret must not be represented as a proven MID fingerprint")
	require.EqualValues(t, 200, initial[fingerprintedHash])
}

func TestDiscoverTossTransactionCredentialSourcesUsesExactBillingAttemptCredential(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		clientKey            = "live_ck_promoted_transaction_mid"
		checkoutSecret       = "live_sk_checkout_transaction_namespace"
		attemptSecret        = "live_sk_promoted_transaction_namespace"
		legacyCheckoutSecret = "live_sk_legacy_checkout_transaction_namespace"
		legacyAttemptSecret  = "live_sk_legacy_attempt_transaction_namespace"
		fingerprintedOrderID = "toss_promoted_transaction_credential"
		legacyOrderID        = "toss_legacy_promoted_transaction_credential"
	)
	checkoutCredential, err := model.EncryptProviderCredential(checkoutSecret)
	require.NoError(t, err)
	attemptCredential, err := model.EncryptProviderCredential(attemptSecret)
	require.NoError(t, err)
	legacyCheckoutCredential, err := model.EncryptProviderCredential(legacyCheckoutSecret)
	require.NoError(t, err)
	legacyAttemptCredential, err := model.EncryptProviderCredential(legacyAttemptSecret)
	require.NoError(t, err)

	midHash := model.TossClientKeyFingerprint(clientKey)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 71, TradeNo: fingerprintedOrderID, Money: 1,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		ProviderCredential: checkoutCredential, ProviderClientKeyHash: midHash,
		BillingAttempted: true, BillingAttemptCredential: attemptCredential,
		BillingAttemptTime: 220, CreateTime: 110, Status: common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 71, TradeNo: legacyOrderID, Money: 1,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		ProviderCredential: legacyCheckoutCredential, ProviderClientKeyHash: "",
		BillingAttempted: true, BillingAttemptCredential: legacyAttemptCredential,
		BillingAttemptTime: 330, CreateTime: 120, Status: common.TopUpStatusSuccess,
	}).Error)

	sources, err := discoverTossTransactionCredentialSources(context.Background())
	require.NoError(t, err)
	byKey := make(map[string]tossTransactionCredentialSource, len(sources))
	for _, source := range sources {
		byKey[source.SourceKey] = source
	}
	require.Equal(t, attemptSecret, byKey[midHash].SecretKey,
		"the source must use the credential durably pinned for the actual provider POST")
	require.Equal(t, midHash, byKey[midHash].MIDFingerprint)
	require.EqualValues(t, 110, byKey[midHash].InitialCursor)
	legacyAttemptSource := byKey[tossTransactionSourceKey("", legacyAttemptSecret)]
	require.Equal(t, legacyAttemptSecret, legacyAttemptSource.SecretKey,
		"pre-fingerprint rows must retain a promoted attempt namespace after config rotation")
	require.Empty(t, legacyAttemptSource.MIDFingerprint)
	require.EqualValues(t, 330, legacyAttemptSource.InitialCursor)
}

func TestOrderTossTransactionSourcesForRunRotatesFirstSourceHourly(t *testing.T) {
	sources := []tossTransactionCredentialSource{
		{SourceKey: "source-c"},
		{SourceKey: "source-a"},
		{SourceKey: "source-b"},
	}
	base := time.Unix(0, 0)
	first := make(map[string]bool, len(sources))
	for hour := 0; hour < len(sources); hour++ {
		ordered := orderTossTransactionSourcesForRun(sources, base.Add(time.Duration(hour)*tossTransactionReconciliationInterval))
		require.Len(t, ordered, len(sources))
		first[ordered[0].SourceKey] = true
	}
	require.Equal(t, map[string]bool{
		"source-a": true,
		"source-b": true,
		"source-c": true,
	}, first)
	require.Equal(t, "source-c", sources[0].SourceKey, "source ordering must not mutate the discovery result")
}

func tossTransactionSnapshotValues(snapshot setting.TossConfigSnapshot) map[string]string {
	return map[string]string{
		"TossEnabled":                   strconv.FormatBool(snapshot.Enabled),
		"TossBillingEnabled":            strconv.FormatBool(snapshot.BillingEnabled),
		"TossWalletAutoRechargeEnabled": strconv.FormatBool(snapshot.WalletAutoRechargeEnabled),
		"TossTestMode":                  strconv.FormatBool(snapshot.TestMode),
		"TossClientKey":                 snapshot.ClientKey,
		"TossSecretKey":                 snapshot.SecretKey,
		"TossTestClientKey":             snapshot.TestClientKey,
		"TossTestSecretKey":             snapshot.TestSecretKey,
		"TossBillingClientKey":          snapshot.BillingClientKey,
		"TossBillingSecretKey":          snapshot.BillingSecretKey,
		"TossBillingTestClientKey":      snapshot.BillingTestClientKey,
		"TossBillingTestSecretKey":      snapshot.BillingTestSecretKey,
		"TossUnitPrice":                 strconv.FormatFloat(snapshot.UnitPrice, 'f', -1, 64),
		"TossMinTopUp":                  strconv.Itoa(snapshot.MinTopUp),
	}
}

func TestDiscoverTossTransactionCredentialSourcesBridgesLegacyCursorAcrossMIDBackfillAndSecretRotation(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	originalSnapshot := setting.GetTossConfigSnapshot()
	t.Cleanup(func() {
		require.NoError(t, setting.ApplyTossOptionValues(tossTransactionSnapshotValues(originalSnapshot)))
	})

	const (
		clientKey = "live_ck_cursor_bridge_mid"
		oldSecret = "live_sk_cursor_bridge_old"
		newSecret = "live_sk_cursor_bridge_new"
	)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossTestMode":             "false",
		"TossClientKey":            clientKey,
		"TossSecretKey":            oldSecret,
		"TossTestClientKey":        "",
		"TossTestSecretKey":        "",
		"TossBillingClientKey":     "",
		"TossBillingSecretKey":     "",
		"TossBillingTestClientKey": "",
		"TossBillingTestSecretKey": "",
	}))

	credential, err := model.EncryptProviderCredential(oldSecret)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, TradeNo: "toss_legacy_cursor_bridge", Amount: 1000,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		ProviderCredential: credential, ProviderClientKeyHash: "", CreateTime: 100,
		Status: common.TopUpStatusSuccess,
	}).Error)

	midSourceKey := model.TossClientKeyFingerprint(clientKey)
	legacySourceKey := tossTransactionSourceKey("", oldSecret)
	require.NoError(t, model.DB.Create(&[]model.TossTransactionReconciliationCursor{
		{SourceKey: midSourceKey, CursorTime: 200, UpdateTime: 200},
		{SourceKey: legacySourceKey, CursorTime: 50, UpdateTime: 50},
	}).Error)

	findMIDSource := func(sources []tossTransactionCredentialSource) tossTransactionCredentialSource {
		t.Helper()
		for i := range sources {
			if sources[i].SourceKey == midSourceKey {
				return sources[i]
			}
		}
		t.Fatalf("MID transaction source %s not found", midSourceKey)
		return tossTransactionCredentialSource{}
	}

	sources, err := discoverTossTransactionCredentialSources(context.Background())
	require.NoError(t, err)
	source := findMIDSource(sources)
	require.Equal(t, oldSecret, source.SecretKey)
	require.Equal(t, midSourceKey, source.MIDFingerprint)
	require.EqualValues(t, 100, source.InitialCursor)
	for i := range sources {
		require.NotEqual(t, legacySourceKey, sources[i].SourceKey,
			"an exactly mapped legacy credential must not remain a second provider source")
	}
	cursor, aliases, err := model.GetOrCreateTossTransactionReconciliationCursorWithAliases(source.SourceKey, source.InitialCursor, source.CursorAliases)
	require.NoError(t, err)
	require.EqualValues(t, 50, cursor.CursorTime, "the older deployed legacy cursor must win the one-time merge")
	require.Equal(t, []string{legacySourceKey}, aliases)
	require.NoError(t, model.AdvanceTossTransactionReconciliationCursorWithAliases(source.SourceKey, aliases, 50, 75))

	// Simulate the atomic pre-rotation fingerprint backfill, then rotate only the
	// secret for the same client key/MID. The stable source must keep the merged
	// cursor and retain the old credential cursor as an alias while using the new
	// active secret for provider reads.
	require.NoError(t, model.DB.Model(&model.TopUp{}).
		Where("trade_no = ?", "toss_legacy_cursor_bridge").
		Update("provider_client_key_hash", midSourceKey).Error)
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{"TossSecretKey": newSecret}))

	sources, err = discoverTossTransactionCredentialSources(context.Background())
	require.NoError(t, err)
	source = findMIDSource(sources)
	require.Equal(t, newSecret, source.SecretKey)
	require.Equal(t, midSourceKey, source.MIDFingerprint)
	cursor, aliases, err = model.GetOrCreateTossTransactionReconciliationCursorWithAliases(source.SourceKey, source.InitialCursor, source.CursorAliases)
	require.NoError(t, err)
	require.EqualValues(t, 75, cursor.CursorTime)
	require.Equal(t, []string{legacySourceKey}, aliases)
	require.NoError(t, model.AdvanceTossTransactionReconciliationCursorWithAliases(source.SourceKey, aliases, 75, 125))

	var stored []model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.Where("source_key IN ?", []string{midSourceKey, legacySourceKey}).Find(&stored).Error)
	require.Len(t, stored, 2)
	for i := range stored {
		require.EqualValues(t, 125, stored[i].CursorTime)
	}
}

func TestTossTransactionConfiguredMIDHashForSecretRejectsAmbiguousPairing(t *testing.T) {
	const sharedSecret = "live_sk_shared_across_mids"
	snapshot := setting.TossConfigSnapshot{
		ClientKey:            "live_ck_first_mid",
		SecretKey:            sharedSecret,
		BillingClientKey:     "live_ck_second_mid",
		BillingSecretKey:     sharedSecret,
		BillingTestClientKey: "test_ck_unrelated_mid",
		BillingTestSecretKey: "test_sk_unrelated_mid",
	}
	require.Empty(t, tossTransactionConfiguredMIDHashForSecret(snapshot, sharedSecret))
	require.Equal(t,
		model.TossClientKeyFingerprint(snapshot.BillingTestClientKey),
		tossTransactionConfiguredMIDHashForSecret(snapshot, snapshot.BillingTestSecretKey),
	)
}

func TestCollectLegacyTossTransactionCredentialsDoesNotUseDatabaseTextCollation(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	for i, credential := range []string{"CaseSensitiveCipher", "casesensitivecipher"} {
		require.NoError(t, model.DB.Create(&model.TopUp{
			UserId: 71, TradeNo: fmt.Sprintf("toss_case_source_%d", i), Amount: 1000,
			PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
			ProviderCredential: credential, ProviderClientKeyHash: "", CreateTime: int64(300 + i),
			Status: common.TopUpStatusSuccess,
		}).Error)
	}

	decoded := map[string]string{
		"CaseSensitiveCipher": "live_sk_case_source_a",
		"casesensitivecipher": "live_sk_case_source_b",
	}
	seen := make(map[string]int64)
	err := collectLegacyTossTransactionCredentialSources(
		context.Background(),
		model.DB.Model(&model.TopUp{}),
		func(value string) (string, error) {
			secret, ok := decoded[value]
			if !ok {
				return "", errors.New("unexpected test credential")
			}
			return secret, nil
		},
		func(sourceKey, _ string, initialCursor int64) {
			seen[sourceKey] = initialCursor
		},
	)
	require.NoError(t, err)
	require.EqualValues(t, 300, seen[tossTransactionSourceKey("", "live_sk_case_source_a")])
	require.EqualValues(t, 301, seen[tossTransactionSourceKey("", "live_sk_case_source_b")])
}

func TestTossTransactionDenseWindowSplitsInsteadOfPoisoningCursor(t *testing.T) {
	const secretKey = "live_sk_dense_transaction_window"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	seoul := time.FixedZone("Asia/Seoul", 9*60*60)
	requests := 0
	halfWindows := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		require.Equal(t, "2", request.URL.Query().Get("limit"))
		start, err := time.ParseInLocation("2006-01-02T15:04:05", request.URL.Query().Get("startDate"), seoul)
		require.NoError(t, err)
		end, err := time.ParseInLocation("2006-01-02T15:04:05", request.URL.Query().Get("endDate"), seoul)
		require.NoError(t, err)
		requests++
		transactions := []tossTransaction{{TransactionKey: fmt.Sprintf("ready_%d_a", requests), Status: "READY"}}
		if end.Sub(start) > 2*time.Hour {
			transactions = append(transactions, tossTransaction{TransactionKey: fmt.Sprintf("ready_%d_b", requests), Status: "READY"})
		} else {
			halfWindows++
		}
		body, err := common.Marshal(transactions)
		require.NoError(t, err)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}

	start := time.Date(2026, time.July, 11, 0, 0, 0, 0, seoul)
	err := reconcileTossTransactionWindowWithLimits(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start, start.Add(4*time.Hour), 2, 2,
	)
	require.NoError(t, err)
	require.Equal(t, 4, requests, "two full parent pages must be followed by both completed half-windows")
	require.Equal(t, 2, halfWindows)
}

func TestTossTransactionPageRejectsUnsafeTransactionKeyBeforeCheckpoint(t *testing.T) {
	const secretKey = "live_sk_unsafe_transaction_key"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(
			`[{"transactionKey":"unsafe cursor","status":"READY"}]`,
		))}, nil
	})}
	checkpoints := 0
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	err := reconcileTossTransactionWindowPagesFrom(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start,
		start.Add(time.Hour),
		10,
		1,
		"",
		func(_, _ string) error {
			checkpoints++
			return nil
		},
	)
	require.Error(t, err)
	require.Equal(t, 1, requests)
	require.Zero(t, checkpoints, "malformed provider identity must never enter the durable pagination cursor")
}

func TestTossTransactionPageRejectsMissingStatusBeforeCheckpoint(t *testing.T) {
	const secretKey = "live_sk_missing_transaction_status"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`[{"transactionKey":"missing_status_tx"}]`)),
		}, nil
	})}

	checkpoints := 0
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	err := reconcileTossTransactionWindowPagesFrom(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start, start.Add(time.Hour), 10, 1, "",
		func(_, _ string) error {
			checkpoints++
			return nil
		},
	)
	require.ErrorContains(t, err, "invalid status")
	require.Zero(t, checkpoints, "a malformed Transaction must not advance the durable startingAfter cursor")
}

func TestTossTransactionPageRejectsUnknownStatusBeforeCheckpoint(t *testing.T) {
	const secretKey = "live_sk_unknown_transaction_status"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`[{"transactionKey":"unknown_status_tx","paymentKey":"pay_unknown_status","orderId":"foreign_unknown_status","status":"FUTURE_FINANCIAL_STATE"}]`,
			)),
		}, nil
	})}

	checkpoints := 0
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	err := reconcileTossTransactionWindowPagesFrom(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start, start.Add(time.Hour), 10, 1, "",
		func(_, _ string) error {
			checkpoints++
			return nil
		},
	)
	require.ErrorContains(t, err, "unsupported status")
	require.Zero(t, checkpoints, "an unknown future financial state must fail closed")
}

func TestTossTransactionLookupRejectsJSONNullInsteadOfCompletingWindow(t *testing.T) {
	const secretKey = "live_sk_null_transaction_page"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	body := "null"
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	transactions, err := getTossTransactionsWithLimit(
		context.Background(), secretKey, start, start.Add(time.Hour), "", 10,
	)
	require.ErrorContains(t, err, "non-array response")
	require.Nil(t, transactions)

	body = "[]"
	transactions, err = getTossTransactionsWithLimit(
		context.Background(), secretKey, start, start.Add(time.Hour), "", 10,
	)
	require.NoError(t, err)
	require.NotNil(t, transactions)
	require.Empty(t, transactions)
}

func TestTossTransactionOversizedPageShrinksLimitAndAdvancesCursor(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_oversized_transaction_page"
	sourceKey := tossTransactionSourceKey("", secretKey)
	now := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	initialCursor := now.Add(-time.Hour).Unix()
	require.NoError(t, model.DB.Create(&model.TossTransactionReconciliationCursor{
		SourceKey: sourceKey, CursorTime: initialCursor,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	limits := make([]string, 0, 2)
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath())
		limit := request.URL.Query().Get("limit")
		limits = append(limits, limit)
		body := "[]"
		if len(limits) == 1 {
			body = strings.Repeat("x", int(tossAPIResponseMaxBodyBytes)+1)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	err := reconcileTossTransactionSource(context.Background(), tossTransactionCredentialSource{
		SourceKey: sourceKey, SecretKey: secretKey,
	}, now)
	require.NoError(t, err)
	require.Equal(t, []string{"1000", "500"}, limits)
	var cursor model.TossTransactionReconciliationCursor
	require.NoError(t, model.DB.First(&cursor, "source_key = ?", sourceKey).Error)
	require.Equal(t, now.Unix(), cursor.CursorTime, "oversized response must not pin the durable cursor")
}

func TestTossTransactionReconciliationSkipsUnknownSharedMIDOrderWithoutPaymentLookup(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const secretKey = "live_sk_shared_mid_unknown_order"
	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "/v1/transactions", request.URL.EscapedPath(), "an unrelated shared-MID order must not trigger a Payment GET")
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
			`[{"transactionKey":"shared_mid_done_tx","paymentKey":"shared_mid_payment_key","orderId":"another_app_order","status":"DONE"}]`,
		))}, nil
	})}

	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	err := reconcileTossTransactionWindowWithLimits(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start, start.Add(time.Hour), 10, 2,
	)
	require.NoError(t, err)
	require.Equal(t, 1, requests)
}

func TestTossTransactionCancellationRowIsNotSkippedAfterEarlierDONELookup(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const (
		secretKey  = "live_sk_done_then_cancel_visibility"
		orderID    = "toss_done_then_cancel_visibility"
		paymentKey = "pay_done_then_cancel_visibility"
	)
	credential, err := model.EncryptProviderCredential(secretKey)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUp{
		UserId: 71, Amount: 1000, Money: 1, Quota: 100, TradeNo: orderID,
		ProviderOrderId: orderID, ProviderCredential: credential,
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusPending,
	}).Error)

	originalBase := tossAPIBase
	originalClient := http.DefaultClient
	t.Cleanup(func() {
		tossAPIBase = originalBase
		http.DefaultClient = originalClient
	})
	tossAPIBase = "https://api.test.tosspayments.local"
	paymentLookups := 0
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.EscapedPath() {
		case "/v1/transactions":
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`[{"transactionKey":"done_visibility_tx","paymentKey":"pay_done_then_cancel_visibility","orderId":"toss_done_then_cancel_visibility","status":"DONE"},{"transactionKey":"cancel_visibility_tx","paymentKey":"pay_done_then_cancel_visibility","orderId":"toss_done_then_cancel_visibility","status":"CANCELED"}]`,
			))}, nil
		case "/v1/payments/pay_done_then_cancel_visibility":
			paymentLookups++
			if paymentLookups == 1 {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
					`{"paymentKey":"pay_done_then_cancel_visibility","type":"NORMAL","orderId":"toss_done_then_cancel_visibility","status":"DONE","totalAmount":1000,"balanceAmount":1000,"currency":"KRW","method":"카드","card":{"amount":1000}}`,
				))}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(
				`{"paymentKey":"pay_done_then_cancel_visibility","type":"NORMAL","orderId":"toss_done_then_cancel_visibility","status":"CANCELED","totalAmount":1000,"balanceAmount":0,"currency":"KRW","method":"카드","card":{"amount":1000},"cancels":[{"cancelAmount":1000,"refundableAmount":0,"transactionKey":"cancel_visibility_tx","cancelStatus":"DONE"}]}`,
			))}, nil
		default:
			return nil, errors.New("unexpected request: " + request.URL.EscapedPath())
		}
	})}

	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	err = reconcileTossTransactionWindowWithLimits(
		context.Background(),
		tossTransactionCredentialSource{SourceKey: tossTransactionSourceKey("", secretKey), SecretKey: secretKey},
		start, start.Add(time.Hour), 10, 2,
	)
	require.NoError(t, err)
	require.Equal(t, 2, paymentLookups, "the cancellation transaction must re-fetch even when the approval lookup saw DONE")
	var event model.TossPaymentEvent
	require.NoError(t, model.DB.Where("transaction_key = ?", "cancel_visibility_tx").First(&event).Error)
	require.Equal(t, model.TossPaymentEventTypeCancellation, event.EventType)
}

func TestTossTransactionFulfillmentDoesNotCreateManualEventForSettledSubscription(t *testing.T) {
	setupTossTransactionReconciliationTest(t)
	const orderID = "toss_transaction_subscription_done"
	midFingerprint := model.TossClientKeyFingerprint("live_ck_transaction_subscription_done")
	require.NoError(t, model.DB.Create(&model.SubscriptionOrder{
		UserId: 71, TradeNo: orderID, ProviderAmount: 1000, ProviderCurrency: "KRW",
		PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
		Status: common.TopUpStatusSuccess, ProviderClientKeyHash: midFingerprint,
	}).Error)
	created, err := model.RecordTossPaymentEvent(&model.TossPaymentEvent{
		EventKey:             "settled-subscription-fulfillment",
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              orderID,
		PaymentKey:           "pay_transaction_subscription_done",
		Status:               "DONE",
		OriginalAmount:       1000,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)
	require.True(t, created)

	err = applyTossTransactionFulfillment(context.Background(), tossTransactionCredentialSource{
		MIDFingerprint: midFingerprint,
	}, &tossConfirmResponse{
		PaymentKey:    "pay_transaction_subscription_done",
		Type:          "BILLING",
		OrderId:       orderID,
		Status:        "DONE",
		TotalAmount:   1000,
		BalanceAmount: 1000,
		Currency:      "KRW",
		Method:        "카드",
		Card:          &tossPaymentCard{Amount: 1000},
	})
	require.NoError(t, err)

	var events []model.TossPaymentEvent
	require.NoError(t, model.DB.Where("order_id = ? AND event_type = ?", orderID, model.TossPaymentEventTypeFulfillment).Find(&events).Error)
	require.Len(t, events, 1)
	require.Equal(t, model.TossReconciliationStatusResolved, events[0].ReconciliationStatus)
}
