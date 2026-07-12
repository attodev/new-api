package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedStaleTossSubscriptionIssueCleanupControllerTest(t *testing.T, tradeNo string) model.SubscriptionOrder {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.SubscriptionOrder{}))
	order := model.SubscriptionOrder{
		UserId:                       703,
		PlanId:                       19,
		TradeNo:                      tradeNo,
		PaymentMethod:                model.PaymentMethodToss,
		PaymentProvider:              model.PaymentProviderToss,
		Status:                       common.TopUpStatusPending,
		CreateTime:                   model.GetDBTimestamp(),
		BillingKeyId:                 915,
		BillingClaimToken:            "new-owner-token",
		BillingClaimTime:             model.GetDBTimestamp(),
		BillingIssueAuthKey:          "encrypted-auth-snapshot",
		BillingIssueAuthKeyHash:      "auth-snapshot-hash",
		BillingIssueCustomerKey:      "cleanup-customer",
		BillingIssueAttempted:        true,
		ProviderCredential:           "encrypted-provider-credential",
		ProviderClientKeyHash:        "provider-client-key-hash",
		BillingChargeProtocolVersion: 1,
	}
	require.NoError(t, model.DB.Create(&order).Error)
	return order
}

func TestStaleTossSubscriptionIssueCleanupDoesNotReplayOrDeleteNewOwnerKey(t *testing.T) {
	setupTossBillingControllerTestDB(t)

	originalIssuer := subscriptionTossBillingKeyIssuer
	originalCleanup := subscriptionTossIssuedBillingKeyCleanup
	issueCalls := 0
	cleanupCalls := 0
	subscriptionTossBillingKeyIssuer = func(context.Context, string, string, string, string) (*tossBillingIssueResponse, int, error) {
		issueCalls++
		return &tossBillingIssueResponse{BillingKey: "must-not-be-replayed", CustomerKey: "cleanup-customer"}, 200, nil
	}
	subscriptionTossIssuedBillingKeyCleanup = func(context.Context, int, string, string, *tossBillingIssueResponse, string, string) error {
		cleanupCalls++
		return nil
	}
	t.Cleanup(func() {
		subscriptionTossBillingKeyIssuer = originalIssuer
		subscriptionTossIssuedBillingKeyCleanup = originalCleanup
	})

	tests := []struct {
		name string
		run  func(*model.SubscriptionOrder) (bool, bool, error)
	}{
		{
			name: "same-idempotency replay cleanup",
			run: func(order *model.SubscriptionOrder) (bool, bool, error) {
				return terminateAndCleanupClaimedTossSubscriptionBillingIssue(
					context.Background(), order, "old-token", "auth-key", "cleanup-customer", "secret-key", "stale-replay",
				)
			},
		},
		{
			name: "already-issued key cleanup",
			run: func(order *model.SubscriptionOrder) (bool, bool, error) {
				return terminateAndCleanupIssuedTossSubscriptionBillingIssue(
					context.Background(), order, "old-token", "cleanup-customer",
					&tossBillingIssueResponse{BillingKey: "shared-new-owner-key", CustomerKey: "cleanup-customer"},
					"secret-key", "stale-issued",
				)
			},
		},
	}
	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			order := seedStaleTossSubscriptionIssueCleanupControllerTest(t, "toss-sub-stale-cleanup-"+tests[i].name)
			staleSnapshot := order
			staleSnapshot.BillingKeyId = 0
			staleSnapshot.BillingClaimToken = "old-token"

			resolved, retainClaim, err := tests[i].run(&staleSnapshot)
			require.ErrorIs(t, err, model.ErrTossBillingClaimLost)
			require.False(t, resolved)
			require.False(t, retainClaim)

			var got model.SubscriptionOrder
			require.NoError(t, model.DB.First(&got, order.Id).Error)
			require.Equal(t, common.TopUpStatusPending, got.Status)
			require.Equal(t, "new-owner-token", got.BillingClaimToken)
			require.Equal(t, 915, got.BillingKeyId)
		})
	}
	require.Zero(t, issueCalls)
	require.Zero(t, cleanupCalls)
}

func TestSubscriptionTossBillingPreclaimRejectionDoesNotExpireNewIssueOwner(t *testing.T) {
	tests := []struct {
		name           string
		slug           string
		providerAmount int64
		userStatus     int
	}{
		{name: "invalid amount", slug: "invalid_amount", providerAmount: 99, userStatus: common.UserStatusEnabled},
		{name: "inactive user", slug: "inactive_user", providerAmount: 15000, userStatus: common.UserStatusDisabled},
	}
	for i := range tests {
		t.Run(tests[i].name, func(t *testing.T) {
			setupTossBillingControllerTestDB(t)
			require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.SubscriptionPlan{}, &model.SubscriptionOrder{}))
			userID := 730 + i
			customerKey := "preclaim-customer-" + tests[i].slug
			require.NoError(t, model.DB.Create(&model.User{
				Id: userID, Username: "preclaim-" + tests[i].slug,
				Status: tests[i].userStatus, TossCustomerKey: customerKey,
			}).Error)
			plan := model.SubscriptionPlan{
				Id: 830 + i, Title: "Preclaim rejection " + tests[i].slug,
				PriceAmount: 10, Currency: "USD",
				DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, Enabled: true,
			}
			require.NoError(t, model.DB.Create(&plan).Error)
			model.InvalidateSubscriptionPlanCache(plan.Id)
			tradeNo := "toss-sub-preclaim-rejection-" + tests[i].slug
			order := model.SubscriptionOrder{
				UserId: userID, PlanId: plan.Id, Money: plan.PriceAmount, TradeNo: tradeNo,
				PaymentMethod: model.PaymentMethodToss, PaymentProvider: model.PaymentProviderToss,
				Status: common.TopUpStatusPending, CreateTime: model.GetDBTimestamp(),
				ProviderAmount: tests[i].providerAmount, ProviderCurrency: "KRW",
			}
			require.NoError(t, model.DB.Create(&order).Error)

			originalCanceller := subscriptionTossPreclaimOrderCanceller
			cancelCalls := 0
			newOwnerToken := ""
			subscriptionTossPreclaimOrderCanceller = func(gotTradeNo string, gotUserID int) (bool, error) {
				cancelCalls++
				require.Equal(t, tradeNo, gotTradeNo)
				require.Equal(t, userID, gotUserID)
				var claimed bool
				var claimErr error
				newOwnerToken, claimed, claimErr = model.ClaimTossSubscriptionBillingIssue(
					tradeNo, "new-owner-auth-"+tests[i].slug, customerKey,
				)
				require.NoError(t, claimErr)
				require.True(t, claimed)
				return model.CancelUnattemptedTossSubscriptionOrder(gotTradeNo, gotUserID)
			}
			t.Cleanup(func() { subscriptionTossPreclaimOrderCanceller = originalCanceller })

			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet,
				"/api/subscription/toss/confirm?trade_no="+tradeNo+"&authKey=stale-callback-auth&customerKey="+customerKey, nil)
			SubscriptionTossBillingConfirm(c)

			require.Equal(t, http.StatusFound, recorder.Code)
			require.Equal(t, 1, cancelCalls)
			require.NotEmpty(t, newOwnerToken)
			var got model.SubscriptionOrder
			require.NoError(t, model.DB.First(&got, order.Id).Error)
			require.Equal(t, common.TopUpStatusPending, got.Status)
			require.Equal(t, newOwnerToken, got.BillingClaimToken)
			require.NotEmpty(t, got.BillingIssueAuthKey)
			require.Equal(t, customerKey, got.BillingIssueCustomerKey)
		})
	}
}
