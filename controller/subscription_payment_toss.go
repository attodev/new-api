package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

const tossBillingChargeTimeout = 70 * time.Second

func init() {
	model.SetTossTopUpReconciler(reconcileTossRecordedTopUp)
	model.SetTossSubscriptionOrderReconciler(reconcileTossPendingSubscriptionOrder)
	model.SetTossBillingCharger(tossBillingChargeForModel)
	model.SetTossBillingRevoker(func(ctx context.Context, billingKey, secretKey string) error {
		_, err := deleteTossBillingKeyWithSecret(ctx, billingKey, secretKey)
		return err
	})
}

// tossBillingIssueResponse is the response from /v1/billing/authorizations/issue.
type tossBillingIssueResponse struct {
	BillingKey  string `json:"billingKey"`
	CustomerKey string `json:"customerKey"`
	CardCompany string `json:"cardCompany"`
	CardNumber  string `json:"cardNumber"`
	Card        struct {
		Company    string `json:"company"`
		IssuerCode string `json:"issuerCode"`
		Number     string `json:"number"`
	} `json:"card"`
}

func (r *tossBillingIssueResponse) cardCompanyForStorage() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.Card.Company) != "" {
		return strings.TrimSpace(r.Card.Company)
	}
	if strings.TrimSpace(r.CardCompany) != "" {
		return strings.TrimSpace(r.CardCompany)
	}
	return strings.TrimSpace(r.Card.IssuerCode)
}

func (r *tossBillingIssueResponse) cardNumberForStorage() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.Card.Number) != "" {
		return strings.TrimSpace(r.Card.Number)
	}
	return strings.TrimSpace(r.CardNumber)
}

// tossSubscriptionChargeKRW converts a plan's USD-equivalent price to KRW for Toss.
// Delegates to model.TossPlanKRW as the single conversion source.
func tossSubscriptionChargeKRW(plan *model.SubscriptionPlan) int64 {
	return model.TossPlanKRW(plan.PriceAmount)
}

func tossSubscriptionOrderChargeKRW(order *model.SubscriptionOrder, plan *model.SubscriptionPlan) int64 {
	if order != nil && order.ProviderAmount > 0 {
		currency := strings.TrimSpace(order.ProviderCurrency)
		if currency == "" || strings.EqualFold(currency, "KRW") {
			return order.ProviderAmount
		}
		return 0
	}
	return tossSubscriptionChargeKRW(plan)
}

func tossBillingSecretOrActive(secretKey string) string {
	secretKey = strings.TrimSpace(secretKey)
	if secretKey == "" {
		return setting.TossActiveBillingSecretKey()
	}
	return secretKey
}

func subscriptionTossTradeNo(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if tradeNo := strings.TrimSpace(c.Param("trade_no")); tradeNo != "" {
		return tradeNo
	}
	return strings.TrimSpace(c.Query("trade_no"))
}

// issueTossBillingKey exchanges an authKey for a billing key.
func issueTossBillingKey(ctx context.Context, authKey, customerKey, idempotencyKey string) (*tossBillingIssueResponse, int, error) {
	return issueTossBillingKeyWithSecret(ctx, authKey, customerKey, idempotencyKey, setting.TossActiveBillingSecretKey())
}

func issueTossBillingKeyWithSecret(ctx context.Context, authKey, customerKey, idempotencyKey, secretKey string) (*tossBillingIssueResponse, int, error) {
	secretKey = tossBillingSecretOrActive(secretKey)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{"authKey": authKey, "customerKey": customerKey}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	statusCode, rb, err := doTossAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/billing/authorizations/issue", body, idempotencyKey, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss billing issue failed: %w", err)
	}
	var out tossBillingIssueResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, statusCode, err
	}
	return &out, statusCode, nil
}

// chargeTossBilling charges a billing key. Reuses tossConfirmResponse (status/totalAmount/orderId/currency).
func chargeTossBilling(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	return chargeTossBillingWithSecret(ctx, billingKey, customerKey, setting.TossActiveBillingSecretKey(), orderId, orderName, amount)
}

func chargeTossBillingWithSecret(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	secretKey = tossBillingSecretOrActive(secretKey)
	// Toss documents that card auto-billing approval can take up to 60 seconds.
	ctx, cancel := context.WithTimeout(ctx, tossBillingChargeTimeout)
	defer cancel()
	payload := map[string]interface{}{
		"customerKey": customerKey,
		"amount":      amount,
		"orderId":     orderId,
		"orderName":   orderName,
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	statusCode, rb, err := doTossAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/billing/"+url.PathEscape(billingKey), body, orderId, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss billing charge failed: %w", err)
	}
	var out tossConfirmResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, statusCode, err
	}
	return &out, statusCode, nil
}

func getTossPaymentByOrderId(ctx context.Context, orderId string) (*tossConfirmResponse, int, error) {
	return getTossPaymentByOrderIdWithSecret(ctx, orderId, setting.TossActiveBillingSecretKey())
}

func getTossPaymentByOrderIdWithSecret(ctx context.Context, orderId, secretKey string) (*tossConfirmResponse, int, error) {
	secretKey = tossBillingSecretOrActive(secretKey)
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	statusCode, body, err := doTossAPIRequestWithSecret(ctx, http.MethodGet, tossAPIBase+"/v1/payments/orders/"+url.PathEscape(orderId), nil, "", secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss get payment by order failed: %w", err)
	}
	var result tossConfirmResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, statusCode, err
	}
	return &result, statusCode, nil
}

func confirmTossBillingChargeOrLookup(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, error) {
	return confirmTossBillingChargeOrLookupWithSecret(ctx, billingKey, customerKey, setting.TossActiveBillingSecretKey(), orderId, orderName, amount)
}

func confirmTossBillingChargeOrLookupWithSecret(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*tossConfirmResponse, error) {
	result, statusCode, err := chargeTossBillingWithSecret(ctx, billingKey, customerKey, secretKey, orderId, orderName, amount)
	if err == nil {
		return result, nil
	}
	auth, lookupStatus, lookupErr := getTossPaymentByOrderIdWithSecret(ctx, orderId, secretKey)
	if lookupErr == nil && isValidTossBillingCharge(auth, orderId, amount) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing charge response lost but recovered by order lookup order_id=%s", orderId))
		return auth, nil
	}
	if isTossTransientAPIStatus(statusCode) || statusCode == 0 {
		if lookupErr != nil {
			return nil, fmt.Errorf("%w: charge status=%d lookup status=%d err=%v", model.ErrTossBillingChargePending, statusCode, lookupStatus, lookupErr)
		}
		if auth != nil && auth.OrderId == orderId {
			switch auth.Status {
			case "READY", "IN_PROGRESS":
				return nil, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, auth.Status)
			}
		}
		return nil, fmt.Errorf("%w: charge status=%d", model.ErrTossBillingChargePending, statusCode)
	}
	if lookupErr != nil {
		return nil, fmt.Errorf("%w; order lookup failed: %v", err, lookupErr)
	}
	return nil, err
}

func tossBillingChargeForModel(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*model.TossBillingChargeResult, error) {
	res, err := confirmTossBillingChargeOrLookupWithSecret(ctx, billingKey, customerKey, secretKey, orderId, orderName, amount)
	if err != nil {
		return nil, err
	}
	result := &model.TossBillingChargeResult{}
	if res == nil {
		return result, nil
	}
	result.Done = isValidTossBillingCharge(res, orderId, amount)
	result.Total = res.TotalAmount
	result.PaymentKey = res.PaymentKey
	if payload, err := common.Marshal(res); err == nil {
		result.ProviderPayload = string(payload)
	}
	return result, nil
}

func deleteTossBillingKey(ctx context.Context, billingKey string) (int, error) {
	return deleteTossBillingKeyWithSecret(ctx, billingKey, setting.TossActiveBillingSecretKey())
}

func deleteTossBillingKeyWithSecret(ctx context.Context, billingKey, secretKey string) (int, error) {
	if billingKey == "" {
		return 0, nil
	}
	secretKey = tossBillingSecretOrActive(secretKey)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	statusCode, _, err := doTossAPIRequestWithSecret(ctx, http.MethodDelete, tossAPIBase+"/v1/billing/"+url.PathEscape(billingKey), nil, "", secretKey, http.StatusOK, http.StatusNotFound)
	if statusCode == http.StatusNotFound {
		return statusCode, model.ErrTossBillingKeyAlreadyDeleted
	}
	if err != nil {
		return statusCode, fmt.Errorf("toss billing delete failed: %w", err)
	}
	return statusCode, nil
}

func revokeTossBillingKey(ctx context.Context, billingKey, secretKey string, billingKeyId int, reason string) {
	if billingKeyId > 0 {
		if err := model.MarkTossBillingKeyPendingRevocation(nil, billingKeyId); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing key local pending-revoke failed id=%d reason=%s err=%v", billingKeyId, reason, err))
		}
	}
	if _, err := deleteTossBillingKeyWithSecret(ctx, billingKey, secretKey); err != nil && !errors.Is(err, model.ErrTossBillingKeyAlreadyDeleted) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing key remote delete failed reason=%s err=%v", reason, err))
		return
	}
	if billingKeyId > 0 {
		if err := model.RevokeTossBillingKey(nil, billingKeyId); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing key local revoke failed id=%d reason=%s err=%v", billingKeyId, reason, err))
		}
	}
}

func queueIssuedTossBillingKeyForRevocation(ctx context.Context, userId int, tradeNo, fallbackCustomerKey string, issued *tossBillingIssueResponse, secretKey, reason string) int {
	if userId <= 0 || issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return 0
	}
	customerKey := strings.TrimSpace(issued.CustomerKey)
	if customerKey == "" {
		customerKey = strings.TrimSpace(fallbackCustomerKey)
	}
	keyId, err := model.StoreTossBillingKeyPendingRevocationWithSecret(
		userId,
		customerKey,
		issued.BillingKey,
		issued.cardCompanyForStorage(),
		issued.cardNumberForStorage(),
		secretKey,
	)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: failed to queue issued Toss billing key for revocation trade_no=%s user_id=%d reason=%s customer_key=%s err=%v", tradeNo, userId, reason, customerKey, err))
		model.RecordLog(userId, model.LogTypeTopup, fmt.Sprintf("Toss billing key cleanup queue failed; manual Toss console check required (trade_no=%s, reason=%s)", tradeNo, reason))
		return 0
	}
	return keyId
}

func revokeIssuedTossBillingKey(ctx context.Context, userId int, tradeNo, fallbackCustomerKey string, issued *tossBillingIssueResponse, secretKey, reason string) {
	if issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return
	}
	billingKeyId := queueIssuedTossBillingKeyForRevocation(ctx, userId, tradeNo, fallbackCustomerKey, issued, secretKey, reason)
	revokeTossBillingKey(ctx, issued.BillingKey, secretKey, billingKeyId, reason)
}

func handleTossBillingActivationFailure(ctx context.Context, userId int, tradeNo, billingKey, secretKey string, billingKeyId int, amount int64, err error) {
	logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription first charge DONE but activation failed trade_no=%s user=%d amount=%d err=%v", tradeNo, userId, amount, err))
	model.RecordLog(userId, model.LogTypeTopup, fmt.Sprintf("Toss 구독 첫 결제 승인됨(%d원)이나 구독 활성화 실패 — 수동 정산 필요 (trade_no=%s)", amount, tradeNo))
	revokeTossBillingKey(ctx, billingKey, secretKey, billingKeyId, "activation_failed")
}

func isValidTossBillingCharge(result *tossConfirmResponse, orderId string, amount int64) bool {
	return result != nil &&
		result.Status == "DONE" &&
		result.TotalAmount == amount &&
		result.OrderId == orderId &&
		strings.ToUpper(result.Currency) == "KRW" &&
		result.Card != nil
}

func reconcileTossPendingSubscriptionOrder(ctx context.Context, order model.SubscriptionOrder) (bool, error) {
	tradeNo := strings.TrimSpace(order.TradeNo)
	if tradeNo == "" {
		return true, nil
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	current := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if current == nil || current.PaymentProvider != model.PaymentProviderToss {
		return true, nil
	}
	if current.Status == common.TopUpStatusSuccess {
		return true, nil
	}
	if current.Status != common.TopUpStatusPending {
		return true, nil
	}
	if current.BillingKeyId <= 0 {
		return false, nil
	}
	renewalSubId, _, isRenewalOrder := model.ParseTossRenewalTradeNo(tradeNo)
	invalidRenewalOrder := strings.HasPrefix(tradeNo, model.TossRenewalTradeNoPrefix) && !isRenewalOrder

	var plan *model.SubscriptionPlan
	var err error
	if current.ProviderAmount <= 0 {
		plan, err = model.GetSubscriptionPlanById(current.PlanId)
		if err != nil {
			return false, err
		}
	}
	chargeKRW := tossSubscriptionOrderChargeKRW(current, plan)
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring below-minimum order_id=%s amount=%d", tradeNo, chargeKRW))
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo)
	}

	secretKey := tossSecretFromCredential(ctx, current.ProviderCredential, setting.TossActiveBillingSecretKey())
	auth, statusCode, err := getTossPaymentByOrderIdWithSecret(ctx, tradeNo, secretKey)
	if err != nil {
		if statusCode == http.StatusNotFound {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile found no payment order_id=%s", tradeNo))
			if isRenewalOrder {
				return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo)
			}
			if invalidRenewalOrder {
				return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo)
			}
			return expireTossPendingSubscriptionOrderForReconcile(tradeNo)
		}
		return false, err
	}
	if isValidTossBillingCharge(auth, tradeNo, chargeKRW) {
		payload, _ := common.Marshal(auth)
		if isRenewalOrder {
			if err := model.RenewTossSubscription(renewalSubId, tradeNo, current.Money, chargeKRW, string(payload)); err != nil {
				if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
					errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
					errors.Is(err, model.ErrPaymentMethodMismatch) {
					return true, nil
				}
				return false, err
			}
			logger.LogInfo(ctx, fmt.Sprintf("Toss subscription pending renewal reconciled as DONE order_id=%s sub_id=%d", tradeNo, renewalSubId))
			return true, nil
		}
		if invalidRenewalOrder {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending renewal reconcile has invalid trade_no=%s", tradeNo))
			return false, nil
		}
		if err := model.CompleteTossBillingOrder(tradeNo, current.BillingKeyId, string(payload)); err != nil {
			if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
				errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
				errors.Is(err, model.ErrPaymentMethodMismatch) {
				return true, nil
			}
			return false, err
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss subscription pending order reconciled as DONE order_id=%s", tradeNo))
		return true, nil
	}
	if auth == nil {
		return false, nil
	}
	if auth.OrderId != "" && auth.OrderId != tradeNo {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile order mismatch order_id=%s auth_order=%s", tradeNo, auth.OrderId))
		return false, nil
	}
	if isTossTerminalFailStatus(auth.Status) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring terminal order_id=%s status=%s", tradeNo, auth.Status))
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo)
	}
	if isTossCancelStatus(auth.Status) {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment %s before activation order_id=%s user_id=%d amount=%d KRW - local subscription NOT activated, manual adjustment needed", auth.Status, tradeNo, current.UserId, auth.TotalAmount))
		model.RecordTopupLog(current.UserId, fmt.Sprintf("Toss subscription payment %s before activation (amount: %d KRW) - manual subscription/quota reconciliation required", auth.Status, auth.TotalAmount), "toss-pending-cleanup", model.PaymentMethodToss, "toss-subscription-cancel")
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo)
	}
	logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring stale non-terminal order_id=%s status=%s", tradeNo, auth.Status))
	if isRenewalOrder {
		return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo)
	}
	if invalidRenewalOrder {
		return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo)
	}
	return expireTossPendingSubscriptionOrderForReconcile(tradeNo)
}

func expireTossPendingRenewalOrderForReconcile(subId int, tradeNo string) (bool, error) {
	if _, err := model.ExpireTossPendingRenewalOrderAndMarkFailure(subId, tradeNo, 1); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
			errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
			errors.Is(err, model.ErrPaymentMethodMismatch) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo string) (bool, error) {
	if err := model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
			errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
			errors.Is(err, model.ErrPaymentMethodMismatch) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func expireTossPendingSubscriptionOrderForReconcile(tradeNo string) (bool, error) {
	if err := model.ExpireTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
			errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
			errors.Is(err, model.ErrPaymentMethodMismatch) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// --- Subscription billing handlers ---

type SubscriptionTossPayRequest struct {
	PlanId int `json:"plan_id"`
}

// SubscriptionRequestTossBilling starts the billing-auth flow for a subscription plan.
func SubscriptionRequestTossBilling(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionTossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgSubPaymentInvalidParams)
		return
	}
	if !isTossBillingEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionNotEnabled)
		return
	}
	chargeKRW := tossSubscriptionChargeKRW(plan)
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		common.ApiErrorI18n(c, i18n.MsgPaymentAmountTooLow)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil || user == nil {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if plan.MaxPurchasePerUser > 0 {
		cnt, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if cnt >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorI18n(c, i18n.MsgSubscriptionPurchaseMax)
			return
		}
	}
	customerKey, err := model.GetOrCreateTossCustomerKey(userId)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	reference := fmt.Sprintf("new-api-toss-sub-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(4))
	tradeNo := "toss_sub_" + common.Sha1([]byte(reference))
	providerCredential, err := model.EncryptProviderCredential(setting.TossActiveBillingSecretKey())
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing encrypt provider credential failed user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	order := &model.SubscriptionOrder{
		UserId:             userId,
		PlanId:             plan.Id,
		Money:              plan.PriceAmount,
		TradeNo:            tradeNo,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		ProviderAmount:     chargeKRW,
		ProviderCurrency:   "KRW",
		ProviderCredential: providerCredential,
		CreateTime:         time.Now().Unix(),
		Status:             common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":   setting.TossActiveBillingClientKey(),
			"customer_key": customerKey,
			"trade_no":     tradeNo,
			"success_url":  base + "/api/subscription/toss/confirm/" + url.PathEscape(tradeNo),
			"fail_url":     base + "/api/subscription/toss/fail/" + url.PathEscape(tradeNo),
		},
	})
}

// SubscriptionTossBillingConfirm is the billingAuth successUrl: issues + stores the
// billing key, charges the first period, and activates the subscription.
func SubscriptionTossBillingConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	authKey := c.Query("authKey")
	customerKey := c.Query("customerKey")
	tradeNo := subscriptionTossTradeNo(c)
	if authKey == "" || customerKey == "" || tradeNo == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm missing params trade_no=%q", tradeNo))
		tossRedirect(c, "/console/topup")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	order := model.GetSubscriptionOrderByTradeNo(tradeNo)
	if order == nil || order.PaymentProvider != model.PaymentProviderToss {
		tossRedirect(c, "/console/topup")
		return
	}
	if order.Status == common.TopUpStatusSuccess {
		tossRedirect(c, "/console/topup")
		return
	}
	if order.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm abnormal status trade_no=%s status=%q", tradeNo, order.Status))
		tossRedirect(c, "/console/topup")
		return
	}
	plan, err := model.GetSubscriptionPlanById(order.PlanId)
	if err != nil {
		tossRedirect(c, "/console/topup")
		return
	}
	chargeKRW := tossSubscriptionOrderChargeKRW(order, plan)
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm blocked below minimum trade_no=%s amount=%d", tradeNo, chargeKRW))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}

	// Bind the callback customerKey to the order user's canonical key so a billing key
	// can't be associated under a mismatched/forged customerKey.
	canonical, err := model.GetOrCreateTossCustomerKey(order.UserId)
	if err != nil || canonical != customerKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing customerKey mismatch trade_no=%s", tradeNo))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}
	secretKey := tossSecretFromCredential(ctx, order.ProviderCredential, setting.TossActiveBillingSecretKey())

	issued, issueStatus, err := issueTossBillingKeyWithSecret(ctx, authKey, customerKey, tradeNo, secretKey)
	if err != nil || issued == nil || issued.BillingKey == "" || issued.CustomerKey != customerKey {
		if err != nil && (issueStatus == 0 || isTossTransientAPIStatus(issueStatus)) && (issued == nil || issued.BillingKey == "") {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: billing key issue response uncertain trade_no=%s user_id=%d status=%d err=%v; expiring local order and requiring manual Toss console check", tradeNo, order.UserId, issueStatus, err))
			model.RecordLog(order.UserId, model.LogTypeTopup, fmt.Sprintf("Toss billing key issue uncertain; manual Toss console check required (trade_no=%s, status=%d)", tradeNo, issueStatus))
			if expireErr := model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss); expireErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss billing issue uncertain local expire failed trade_no=%s err=%v", tradeNo, expireErr))
			}
			tossRedirect(c, "/console/topup")
			return
		}
		logger.LogError(ctx, fmt.Sprintf("Toss billing issue failed trade_no=%s status=%d err=%v", tradeNo, issueStatus, err))
		if issued != nil && issued.BillingKey != "" {
			revokeIssuedTossBillingKey(ctx, order.UserId, tradeNo, customerKey, issued, secretKey, "issue_validation_failed")
		}
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}
	billingKeyId, err := model.StoreTossBillingKeyWithSecret(order.UserId, customerKey, issued.BillingKey, issued.cardCompanyForStorage(), issued.cardNumberForStorage(), secretKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing store failed trade_no=%s err=%v", tradeNo, err))
		revokeIssuedTossBillingKey(ctx, order.UserId, tradeNo, customerKey, issued, secretKey, "store_failed")
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}
	if err := model.AttachTossBillingKeyToOrder(tradeNo, billingKeyId); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing key attach failed trade_no=%s err=%v", tradeNo, err))
		revokeTossBillingKey(ctx, issued.BillingKey, secretKey, billingKeyId, "attach_failed")
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}

	orderName := model.TossSubscriptionOrderName(plan.Title, false)
	result, err := confirmTossBillingChargeOrLookupWithSecret(ctx, issued.BillingKey, customerKey, secretKey, tradeNo, orderName, chargeKRW)
	if err != nil || !isValidTossBillingCharge(result, tradeNo, chargeKRW) {
		if errors.Is(err, model.ErrTossBillingChargePending) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge still processing trade_no=%s err=%v", tradeNo, err))
			tossRedirect(c, "/console/topup")
			return
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge not done trade_no=%s err=%v", tradeNo, err))
		revokeTossBillingKey(ctx, issued.BillingKey, secretKey, billingKeyId, "first_charge_failed")
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}

	payload, _ := common.Marshal(result)
	if err := model.CompleteTossBillingOrder(tradeNo, billingKeyId, string(payload)); err != nil {
		handleTossBillingActivationFailure(ctx, order.UserId, tradeNo, issued.BillingKey, secretKey, billingKeyId, chargeKRW, err)
		tossRedirect(c, "/console/topup")
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss subscription activated trade_no=%s plan=%d user=%d", tradeNo, plan.Id, order.UserId))
	tossRedirect(c, "/console/topup")
}

// SubscriptionTossBillingFail is the billingAuth failUrl.
func SubscriptionTossBillingFail(c *gin.Context) {
	tradeNo := subscriptionTossTradeNo(c)
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("Toss billing auth failed trade_no=%s code=%s", tradeNo, code))
	if tradeNo != "" {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
	}
	tossRedirect(c, tossFailureRedirectPath(tradeNo, code, message))
}

// CancelTossAutoRenew disables auto-renew for the user's active Toss subscriptions and revokes the key.
func CancelTossAutoRenew(c *gin.Context) {
	userId := c.GetInt("id")
	if err := model.CancelTossAutoRenewForUser(c.Request.Context(), userId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
