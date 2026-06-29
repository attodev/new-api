package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/thanhpk/randstr"
)

const tossAPIBase = "https://api.tosspayments.com"

type TossPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

// tossConfirmResponse is the subset of the Toss Payment object we rely on.
type tossConfirmResponse struct {
	PaymentKey  string `json:"paymentKey"`
	OrderId     string `json:"orderId"`
	Status      string `json:"status"`
	TotalAmount int64  `json:"totalAmount"`
	Currency    string `json:"currency"`
	Method      string `json:"method"`
	ApprovedAt  string `json:"approvedAt"`
}

// getTossPayMoney returns the KRW amount to charge for the given entered amount.
// It applies both the group top-up ratio and the amount-based discount (keyed on
// the entered amount), mirroring getPayPalPayMoney for provider parity.
func getTossPayMoney(amountKRW int64, group string) int64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amountKRW)]; ok && ds > 0 {
		discount = ds
	}
	return decimal.NewFromInt(amountKRW).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount)).
		Round(0).
		IntPart()
}

// tossUSDEquivalent converts charged KRW to the USD-equivalent stored in Money.
func tossUSDEquivalent(chargedKRW int64) float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		unit = 1
	}
	return decimal.NewFromInt(chargedKRW).Div(decimal.NewFromFloat(unit)).InexactFloat64()
}

func RequestTossAmount(c *gin.Context) {
	var req TossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !isTossTopUpEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if req.Amount < int64(setting.TossMinTopUp) {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": setting.TossMinTopUp})
		return
	}
	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)
	charged := getTossPayMoney(req.Amount, user.Group)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatInt(charged, 10)})
}

// isValidServerAddress reports whether addr is an absolute http(s) URL with a host.
func isValidServerAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func RequestTossPay(c *gin.Context) {
	var req TossPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.PaymentMethod != model.PaymentMethodToss {
		common.ApiErrorI18n(c, i18n.MsgTopupUnsupportedProvider)
		return
	}
	if !isTossTopUpEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss pay blocked: invalid ServerAddress=%q", system_setting.ServerAddress))
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if req.Amount < int64(setting.TossMinTopUp) {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": setting.TossMinTopUp})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	chargedKRW := getTossPayMoney(req.Amount, user.Group)
	if chargedKRW <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
		return
	}

	// Fetch (or create) the stable customerKey first so a failure here does not
	// orphan a pending TopUp order.
	customerKey, err := model.GetOrCreateTossCustomerKey(id)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss get customerKey failed user_id=%d error=%q", id, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	reference := fmt.Sprintf("new-api-toss-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	orderId := "toss_" + common.Sha1([]byte(reference))

	topUp := &model.TopUp{
		UserId:          id,
		TargetType:      getTopUpTargetType(c),
		TargetId:        getTopUpTargetId(c),
		Amount:          chargedKRW,
		Money:           tossUSDEquivalent(chargedKRW),
		TradeNo:         orderId,
		ProviderOrderId: orderId,
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss create topup order failed user_id=%d order_id=%s error=%q", id, orderId, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	serverBase := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":   setting.TossActiveClientKey(),
			"customer_key": customerKey,
			"order_id":     orderId,
			"order_name": fmt.Sprintf("크레딧 충전 %d원", chargedKRW),
			"amount":     chargedKRW,
			"success_url": serverBase + "/api/toss/confirm",
			"fail_url":    serverBase + "/api/toss/fail",
		},
	})
}

// confirmTossPayment calls Toss POST /v1/payments/confirm.
func confirmTossPayment(ctx context.Context, paymentKey, orderId string, amount int64) (*tossConfirmResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{
		"paymentKey": paymentKey,
		"orderId":    orderId,
		"amount":     amount,
	}
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/payments/confirm", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	credentials := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", orderId)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toss confirm failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result tossConfirmResponse
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// getTossPayment fetches the authoritative payment object from Toss.
func getTossPayment(ctx context.Context, paymentKey string) (*tossConfirmResponse, error) {
	// Webhook-only path: stay under Toss's ~10s webhook response window.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tossAPIBase+"/v1/payments/"+paymentKey, nil)
	if err != nil {
		return nil, err
	}
	credentials := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+credentials)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("toss get payment failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	var result tossConfirmResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func tossRedirect(c *gin.Context, path string) {
	c.Redirect(http.StatusFound, path)
}

func validateTossConfirm(topUp *model.TopUp, orderId string, amount int64) error {
	if topUp == nil {
		return fmt.Errorf("toss local order not found order_id=%s", orderId)
	}
	if topUp.PaymentProvider != model.PaymentProviderToss {
		return fmt.Errorf("toss provider mismatch order_id=%s provider=%s", orderId, topUp.PaymentProvider)
	}
	if topUp.Amount != amount {
		return fmt.Errorf("toss amount mismatch order_id=%s expected=%d actual=%d", orderId, topUp.Amount, amount)
	}
	return nil
}

// TossConfirm handles the successUrl redirect: ?paymentKey&orderId&amount.
func TossConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	paymentKey := c.Query("paymentKey")
	orderId := c.Query("orderId")
	amountStr := c.Query("amount")

	if paymentKey == "" || orderId == "" || amountStr == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm missing params client_ip=%s", c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}
	amount, err := strconv.ParseInt(amountStr, 10, 64)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm bad amount=%q client_ip=%s", amountStr, c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}

	LockOrder(orderId)
	defer UnlockOrder(orderId)

	topUp := model.GetTopUpByTradeNo(orderId)
	if err := validateTossConfirm(topUp, orderId, amount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed error=%q client_ip=%s", err.Error(), c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		tossRedirect(c, "/console/log")
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm abnormal status order_id=%s status=%q", orderId, topUp.Status))
		tossRedirect(c, "/console/topup")
		return
	}

	// Persist the paymentKey BEFORE confirming so the order remains reconcilable
	// even if the process dies right after Toss approves the payment.
	if err := model.RecordTossPaymentKey(orderId, paymentKey); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss record paymentKey (pre-confirm) failed order_id=%s error=%q", orderId, err.Error()))
		// non-fatal: continue; RechargeToss also stores it idempotently
	}

	result, err := confirmTossPayment(ctx, paymentKey, orderId, amount)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm API failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if result.Status != "DONE" || result.TotalAmount != amount ||
		result.OrderId != orderId || result.PaymentKey != paymentKey ||
		strings.ToUpper(result.Currency) != "KRW" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed order_id=%s status=%s total=%d resp_order=%s currency=%s", orderId, result.Status, result.TotalAmount, result.OrderId, result.Currency))
		tossRedirect(c, "/console/topup")
		return
	}

	if err := model.RechargeToss(orderId, result.PaymentKey, c.ClientIP()); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss recharge failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded order_id=%s", orderId))
	tossRedirect(c, "/console/log")
}

// TossFail handles the failUrl redirect.
func TossFail(c *gin.Context) {
	ctx := c.Request.Context()
	orderId := c.Query("orderId")
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(ctx, fmt.Sprintf("Toss payment failed order_id=%s code=%s message=%q client_ip=%s", orderId, code, message, c.ClientIP()))
	if orderId != "" {
		// Immediate pending→failed transition is correct for synchronous CARD payments:
		// the fail redirect fires only when the card flow itself fails/is cancelled.
		// When asynchronous methods (e.g. virtual account) are added, this must be gated
		// per payment method so that a later success webhook isn't blocked — RechargeToss
		// credits only orders that are still in pending status.
		_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
	}
	tossRedirect(c, "/console/topup")
}

// isTossTerminalFailStatus reports whether a Toss status means the payment never completed.
func isTossTerminalFailStatus(status string) bool {
	switch status {
	case "EXPIRED", "ABORTED":
		return true
	default:
		return false
	}
}

// isTossCancelStatus reports whether a Toss status means a completed payment was (partially) canceled/refunded.
func isTossCancelStatus(status string) bool {
	switch status {
	case "CANCELED", "PARTIAL_CANCELED":
		return true
	default:
		return false
	}
}

// TossWebhook handles Toss PAYMENT_STATUS_CHANGED events for card payments.
//
//   - DONE → credit the order (re-verified against the authoritative payment, idempotent).
//   - EXPIRED / ABORTED → close the lingering pending order (re-verified).
//   - CANCELED / PARTIAL_CANCELED on an already-credited order → flag for manual
//     reconciliation (re-verified). We deliberately do NOT auto-claw back quota
//     (spent quota and partial refunds make automatic reversal unsafe).
//
// Card payments are normally credited synchronously in TossConfirm; this endpoint
// provides defensive crediting, closure of pending orders whose approval/window
// lapsed (Toss emits EXPIRED for card payments when the 30-min/10-min windows lapse),
// and post-credit refund detection.
//
// Every action is re-verified by re-fetching the authoritative payment from Toss, so
// nothing is ever decided from the unauthenticated webhook payload alone.
//
// This endpoint does NOT implement Toss's virtual-account DEPOSIT_CALLBACK shape
// (top-level "secret"/"status"/"orderId") nor store the deposit secret; that is
// deferred until virtual-account support lands.
func TossWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	var event struct {
		EventType string `json:"eventType"`
		Data      struct {
			OrderId     string `json:"orderId"`
			PaymentKey  string `json:"paymentKey"`
			Status      string `json:"status"`
			TotalAmount int64  `json:"totalAmount"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &event); err != nil {
		logger.LogError(ctx, "Toss webhook parse failed: "+err.Error())
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	orderId := event.Data.OrderId
	if orderId == "" {
		c.Status(http.StatusOK)
		return
	}

	status := event.Data.Status
	isDone := status == "DONE"
	isCancel := isTossCancelStatus(status)
	isFailTerminal := isTossTerminalFailStatus(status)
	if !isDone && !isCancel && !isFailTerminal {
		// Intermediate states (READY / IN_PROGRESS / WAITING_FOR_DEPOSIT / ...) — nothing to do.
		c.Status(http.StatusOK)
		return
	}

	LockOrder(orderId)
	defer UnlockOrder(orderId)

	topUp := model.GetTopUpByTradeNo(orderId)
	if topUp == nil || topUp.PaymentProvider != model.PaymentProviderToss {
		c.Status(http.StatusOK)
		return
	}

	// Already credited: only a genuine post-credit cancel/refund needs attention.
	if topUp.Status == common.TopUpStatusSuccess {
		if isCancel {
			if event.Data.PaymentKey == "" {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION: cancel webhook on credited order with no paymentKey order_id=%s status=%s", orderId, status))
				c.Status(http.StatusOK)
				return
			}
			auth, err := getTossPayment(ctx, event.Data.PaymentKey)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss webhook get payment failed (cancel) order_id=%s error=%q", orderId, err.Error()))
				c.Status(http.StatusServiceUnavailable) // retry so a real refund isn't missed
				return
			}
			if auth.OrderId == orderId && isTossCancelStatus(auth.Status) {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: payment %s after credit order_id=%s user_id=%d amount=%d KRW — local quota NOT reverted, manual adjustment needed", auth.Status, orderId, topUp.UserId, topUp.Amount))
				model.RecordTopupLog(topUp.UserId, fmt.Sprintf("Toss payment %s after credit (amount: %d KRW) — manual quota reconciliation required", auth.Status, topUp.Amount), c.ClientIP(), topUp.PaymentMethod, "toss-cancel")
			}
		}
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		c.Status(http.StatusOK) // already closed (failed/expired)
		return
	}

	// Pending order: never act on the unauthenticated payload alone — re-fetch the authoritative payment.
	if event.Data.PaymentKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook missing paymentKey order_id=%s status=%s", orderId, status))
		c.Status(http.StatusOK)
		return
	}
	auth, err := getTossPayment(ctx, event.Data.PaymentKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss webhook get payment failed order_id=%s error=%q", orderId, err.Error()))
		c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
		return
	}
	if auth.OrderId != orderId {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook order mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
		c.Status(http.StatusOK)
		return
	}

	if auth.Status == "DONE" {
		if auth.TotalAmount != topUp.Amount {
			logger.LogWarn(ctx, fmt.Sprintf("Toss webhook DONE authoritative mismatch order_id=%s total=%d", orderId, auth.TotalAmount))
			c.Status(http.StatusOK)
			return
		}
		if err := model.RechargeToss(orderId, auth.PaymentKey, c.ClientIP()); err != nil {
			c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
			return
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded (webhook) order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}

	// EXPIRED / ABORTED on a still-pending order: payment never completed (no money) → close quietly.
	if isTossTerminalFailStatus(auth.Status) {
		target := common.TopUpStatusFailed
		if auth.Status == "EXPIRED" {
			target = common.TopUpStatusExpired
		}
		if err := model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, target); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss webhook close pending order_id=%s status=%s error=%q", orderId, auth.Status, err.Error()))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("Toss webhook closed pending order order_id=%s status=%s", orderId, auth.Status))
		}
		c.Status(http.StatusOK)
		return
	}

	// CANCELED / PARTIAL_CANCELED on a still-pending order: the payment was completed at Toss
	// (PARTIAL_CANCELED may retain a balance) but we never credited it. Do NOT close silently —
	// flag for manual reconciliation, then mark failed so it doesn't linger.
	if isTossCancelStatus(auth.Status) {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: %s on uncredited pending order order_id=%s user_id=%d amount=%d KRW — verify Toss balance and adjust manually", auth.Status, orderId, topUp.UserId, topUp.Amount))
		model.RecordTopupLog(topUp.UserId, fmt.Sprintf("Toss %s on uncredited order (amount: %d KRW) — manual reconciliation required", auth.Status, topUp.Amount), c.ClientIP(), topUp.PaymentMethod, "toss-cancel")
		_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
		c.Status(http.StatusOK)
		return
	}

	c.Status(http.StatusOK)
}

func RequestOrganizationTossPay(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestTossPay(c)
}

func RequestOrganizationTossAmount(c *gin.Context) {
	if _, _, ok := prepareOrganizationTopUpTarget(c); !ok {
		return
	}
	RequestTossAmount(c)
}
