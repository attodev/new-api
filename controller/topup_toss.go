package controller

import (
	"context"
	"encoding/base64"
	"errors"
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

var tossAPIBase = "https://api.tosspayments.com"

const tossAPIMaxAttempts = 3

func isTossTransientAPIStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusConflict ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func tossRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		return 0
	}
	return time.Duration(100*(attempt+1)) * time.Millisecond
}

func waitTossRetry(ctx context.Context, attempt int) error {
	delay := tossRetryDelay(attempt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func doTossAPIRequest(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string, acceptedStatuses ...int) (int, []byte, error) {
	return doTossAPIRequestWithSecret(ctx, method, endpoint, body, idempotencyKey, setting.TossActiveSecretKey(), acceptedStatuses...)
}

func doTossAPIRequestWithSecret(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string, secretKey string, acceptedStatuses ...int) (int, []byte, error) {
	accepted := make(map[int]struct{}, len(acceptedStatuses))
	for _, status := range acceptedStatuses {
		accepted[status] = struct{}{}
	}
	var lastStatus int
	var lastBody []byte
	var lastErr error
	for attempt := 0; attempt < tossAPIMaxAttempts; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = strings.NewReader(string(body))
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
		if err != nil {
			return 0, nil, err
		}
		credentials := base64.StdEncoding.EncodeToString([]byte(secretKey + ":"))
		req.Header.Set("Authorization", "Basic "+credentials)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if strings.TrimSpace(idempotencyKey) != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastStatus = 0
			lastBody = nil
			lastErr = err
		} else {
			respBody, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastStatus = resp.StatusCode
			lastBody = respBody
			if readErr != nil {
				return lastStatus, lastBody, readErr
			}
			if _, ok := accepted[resp.StatusCode]; ok {
				return lastStatus, lastBody, nil
			}
			lastErr = fmt.Errorf("toss api failed: method=%s status=%d body=%s", method, resp.StatusCode, string(respBody))
			if !isTossTransientAPIStatus(resp.StatusCode) {
				return lastStatus, lastBody, lastErr
			}
		}

		if attempt == tossAPIMaxAttempts-1 {
			break
		}
		if err := waitTossRetry(ctx, attempt); err != nil {
			return lastStatus, lastBody, err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("toss api failed: method=%s status=%d body=%s", method, lastStatus, string(lastBody))
	}
	return lastStatus, lastBody, lastErr
}

type TossPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
}

type tossPaymentCard struct {
	Company string `json:"company"`
	Number  string `json:"number"`
}

// tossConfirmResponse is the subset of the Toss Payment object we rely on.
type tossConfirmResponse struct {
	PaymentKey  string           `json:"paymentKey"`
	OrderId     string           `json:"orderId"`
	Status      string           `json:"status"`
	TotalAmount int64            `json:"totalAmount"`
	Currency    string           `json:"currency"`
	Method      string           `json:"method"`
	ApprovedAt  string           `json:"approvedAt"`
	Card        *tossPaymentCard `json:"card"`
}

func tossSecretFromCredential(ctx context.Context, encrypted, fallback string) string {
	secret, err := model.DecryptProviderCredential(encrypted)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss provider credential decrypt failed, falling back to active key: %v", err))
		return fallback
	}
	if strings.TrimSpace(secret) == "" {
		return fallback
	}
	return secret
}

// getTossPayMoney returns the KRW amount to charge for the given entered amount.
// It applies both the group top-up ratio and the amount-based discount (keyed on
// the entered amount), mirroring getPayPalPayMoney for provider parity.
// getTossPayMoney converts an entered amount (in display units, same model as PayPal/$)
// to the KRW to charge: chargedKRW = units × TossUnitPrice × groupRatio × discount.
// This keeps Toss internally identical to the USD/unit model (Money = chargedKRW/TossUnitPrice
// = units, quota = Money × QuotaPerUnit) while charging in KRW.
func getTossPayMoney(amountUnits int64, group string) int64 {
	amt := decimal.NewFromInt(amountUnits)
	// When the platform displays quota as raw tokens, the entered amount is tokens; convert to units.
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amt = amt.Div(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amountUnits)]; ok && ds > 0 {
		discount = ds
	}
	unit := setting.TossUnitPrice
	if unit <= 0 {
		return 0
	}
	return amt.
		Mul(decimal.NewFromFloat(unit)).
		Mul(decimal.NewFromFloat(ratio)).
		Mul(decimal.NewFromFloat(discount)).
		Round(0).
		IntPart()
}

// tossUSDEquivalent converts charged KRW to the USD-equivalent stored in Money.
func tossUSDEquivalent(chargedKRW int64) float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		return 0
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
	if !model.IsTossCardAmountPayableKRW(charged) {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": strconv.FormatInt(charged, 10)})
}

// isValidServerAddress reports whether addr can be used as a Toss callback base.
// Public callback URLs must be HTTPS; HTTP is allowed only for local development.
func isValidServerAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
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
	if !model.IsTossCardAmountPayableKRW(chargedKRW) {
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
	providerCredential, err := model.EncryptProviderCredential(setting.TossActiveSecretKey())
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss encrypt provider credential failed user_id=%d order_id=%s error=%q", id, orderId, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	topUp := &model.TopUp{
		UserId:             id,
		TargetType:         getTopUpTargetType(c),
		TargetId:           getTopUpTargetId(c),
		Amount:             chargedKRW,
		Money:              tossUSDEquivalent(chargedKRW),
		TradeNo:            orderId,
		ProviderOrderId:    orderId,
		ProviderCredential: providerCredential,
		PaymentMethod:      model.PaymentMethodToss,
		PaymentProvider:    model.PaymentProviderToss,
		CreateTime:         time.Now().Unix(),
		Status:             common.TopUpStatusPending,
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
			"order_name":   fmt.Sprintf("크레딧 충전 %d원", chargedKRW),
			"amount":       chargedKRW,
			"success_url":  serverBase + "/api/toss/confirm",
			"fail_url":     serverBase + "/api/toss/fail",
		},
	})
}

// confirmTossPayment calls Toss POST /v1/payments/confirm.
// It returns the HTTP status code so callers can distinguish terminal (4xx)
// rejections from transient (5xx/network) failures.
func confirmTossPayment(ctx context.Context, paymentKey, orderId string, amount int64) (*tossConfirmResponse, int, error) {
	return confirmTossPaymentWithSecret(ctx, paymentKey, orderId, amount, setting.TossActiveSecretKey())
}

func confirmTossPaymentWithSecret(ctx context.Context, paymentKey, orderId string, amount int64, secretKey string) (*tossConfirmResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{
		"paymentKey": paymentKey,
		"orderId":    orderId,
		"amount":     amount,
	}
	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	statusCode, respBody, err := doTossAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/payments/confirm", bodyBytes, orderId, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss confirm failed: %w", err)
	}

	var result tossConfirmResponse
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, statusCode, err
	}
	return &result, statusCode, nil
}

// getTossPaymentWithSecret fetches the authoritative payment object from Toss.
// It returns the HTTP status code so callers can distinguish a definitive
// 404 (no such payment) from transient (network/5xx) failures.
func getTossPaymentWithSecret(ctx context.Context, paymentKey, secretKey string) (*tossConfirmResponse, int, error) {
	// Webhook-only path: stay under Toss's ~10s webhook response window.
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	statusCode, body, err := doTossAPIRequestWithSecret(ctx, http.MethodGet, tossAPIBase+"/v1/payments/"+url.PathEscape(paymentKey), nil, "", secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss get payment failed: %w", err)
	}
	var result tossConfirmResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, statusCode, err
	}
	return &result, statusCode, nil
}

func getTossPayment(ctx context.Context, paymentKey string) (*tossConfirmResponse, int, error) {
	return getTossPaymentWithSecret(ctx, paymentKey, setting.TossActiveSecretKey())
}

func getTossBillingPayment(ctx context.Context, paymentKey string) (*tossConfirmResponse, int, error) {
	return getTossPaymentWithSecret(ctx, paymentKey, setting.TossActiveBillingSecretKey())
}

func tossRedirect(c *gin.Context, path string) {
	c.Redirect(http.StatusFound, path)
}

func tossFailureRedirectPath(orderId, code, message string) string {
	values := url.Values{}
	if orderId = strings.TrimSpace(orderId); orderId != "" {
		values.Set("toss_order_id", trimTossFailureValue(orderId))
	}
	if code = strings.TrimSpace(code); code != "" {
		values.Set("toss_error_code", trimTossFailureValue(code))
	}
	if message = strings.TrimSpace(message); message != "" {
		values.Set("toss_error_message", trimTossFailureValue(message))
	}
	if len(values) == 0 {
		return "/console/topup"
	}
	return "/console/topup?" + values.Encode()
}

func trimTossFailureValue(value string) string {
	const maxRunes = 300
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
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

func isValidTossTopUpCardPayment(result *tossConfirmResponse, orderId string, amount int64) bool {
	return result != nil &&
		result.Status == "DONE" &&
		result.TotalAmount == amount &&
		result.OrderId == orderId &&
		strings.ToUpper(result.Currency) == "KRW" &&
		result.Card != nil
}

func closeStaleTossPendingTopUp(ctx context.Context, orderId, targetStatus, reason string) (bool, error) {
	if err := model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, targetStatus); err != nil {
		if errors.Is(err, model.ErrTopUpStatusInvalid) || errors.Is(err, model.ErrTopUpNotFound) {
			return true, nil
		}
		return false, err
	}
	logger.LogWarn(ctx, fmt.Sprintf("Toss stale pending top-up closed order_id=%s status=%s reason=%s", orderId, targetStatus, reason))
	return true, nil
}

func reconcileTossRecordedTopUp(ctx context.Context, topUp model.TopUp) (bool, error) {
	orderId := strings.TrimSpace(topUp.TradeNo)
	paymentKey := strings.TrimSpace(topUp.ProviderOrderId)
	if orderId == "" || paymentKey == "" || paymentKey == orderId {
		return false, nil
	}

	LockOrder(orderId)
	defer UnlockOrder(orderId)

	current := model.GetTopUpByTradeNo(orderId)
	if current == nil {
		return true, nil
	}
	if current.Status != common.TopUpStatusPending {
		return true, nil
	}
	paymentKey = strings.TrimSpace(current.ProviderOrderId)
	if paymentKey == "" || paymentKey == current.TradeNo {
		return false, nil
	}

	secretKey := tossSecretFromCredential(ctx, current.ProviderCredential, setting.TossActiveSecretKey())
	auth, statusCode, err := getTossPaymentWithSecret(ctx, paymentKey, secretKey)
	if err != nil {
		if statusCode == http.StatusNotFound {
			return closeStaleTossPendingTopUp(ctx, orderId, common.TopUpStatusFailed, "payment_not_found")
		}
		return false, err
	}
	if auth.OrderId != orderId {
		return closeStaleTossPendingTopUp(ctx, orderId, common.TopUpStatusFailed, "order_mismatch")
	}
	if auth.Status == "DONE" {
		if isValidTossTopUpCardPayment(auth, orderId, current.Amount) {
			if err := model.RechargeToss(orderId, auth.PaymentKey, "toss-pending-cleanup"); err != nil {
				return false, err
			}
			logger.LogInfo(ctx, fmt.Sprintf("Toss stale pending top-up credited order_id=%s", orderId))
			return true, nil
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss stale pending top-up DONE rejected order_id=%s total=%d currency=%s card_present=%t", orderId, auth.TotalAmount, auth.Currency, auth.Card != nil))
		return closeStaleTossPendingTopUp(ctx, orderId, common.TopUpStatusFailed, "done_payload_mismatch")
	}
	if isTossTerminalFailStatus(auth.Status) || isTossCancelStatus(auth.Status) {
		target := common.TopUpStatusFailed
		if auth.Status == "EXPIRED" {
			target = common.TopUpStatusExpired
		}
		if isTossCancelStatus(auth.Status) {
			paymentMethod := strings.TrimSpace(current.PaymentMethod)
			if paymentMethod == "" {
				paymentMethod = model.PaymentMethodToss
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: %s on uncredited stale pending order order_id=%s user_id=%d amount=%d KRW — verify Toss balance and adjust manually", auth.Status, orderId, current.UserId, current.Amount))
			model.RecordTopupLog(current.UserId, fmt.Sprintf("Toss payment %s before local credit (amount: %d KRW) — manual quota reconciliation required", auth.Status, current.Amount), "toss-pending-cleanup", paymentMethod, "toss-cancel")
		}
		return closeStaleTossPendingTopUp(ctx, orderId, target, strings.ToLower(auth.Status))
	}
	return closeStaleTossPendingTopUp(ctx, orderId, common.TopUpStatusExpired, "approval_window_elapsed_"+strings.ToLower(auth.Status))
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
	secretKey := tossSecretFromCredential(ctx, topUp.ProviderCredential, setting.TossActiveSecretKey())

	// Persist the paymentKey BEFORE approving the payment. This MUST succeed: the stale-pending
	// sweep treats provider_order_id == trade_no as "never approved" and expires such orders, so
	// approving without first recording the paymentKey could let a real paid order be wrongly
	// expired with no credit.
	if err := model.RecordTossPaymentKey(orderId, paymentKey); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss record paymentKey (pre-confirm) failed order_id=%s error=%q — aborting confirm", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}

	result, statusCode, err := confirmTossPaymentWithSecret(ctx, paymentKey, orderId, amount, secretKey)
	if err != nil {
		// Don't infer terminal-vs-transient from the HTTP status (e.g. 409 IDEMPOTENT_REQUEST_PROCESSING
		// is transient, not a rejection). Ask Toss authoritatively what actually happened.
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm API failed order_id=%s status=%d error=%q — re-verifying", orderId, statusCode, err.Error()))
		auth, verifyStatus, gerr := getTossPaymentWithSecret(ctx, paymentKey, secretKey)
		if gerr != nil {
			if verifyStatus == http.StatusNotFound {
				// Toss has no payment for this paymentKey (forged/invalid key, or no payment ever made)
				// → definitively no payment → close so it isn't stranded pending.
				_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
				logger.LogWarn(ctx, fmt.Sprintf("Toss confirm verify: payment not found order_id=%s — closed as failed", orderId))
				tossRedirect(c, "/console/topup")
				return
			}
			// Network / 5xx / other → transient; leave pending for the webhook/retry to resolve.
			logger.LogError(ctx, fmt.Sprintf("Toss confirm verify failed (transient) order_id=%s status=%d error=%q", orderId, verifyStatus, gerr.Error()))
			tossRedirect(c, "/console/topup")
			return
		}
		switch {
		case auth.OrderId != orderId:
			// paymentKey resolves to a different order (tampering) → our order has no payment → close.
			_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm verify: order mismatch order_id=%s auth_order=%s — closed as failed", orderId, auth.OrderId))
			tossRedirect(c, "/console/topup")
			return
		case isValidTossTopUpCardPayment(auth, orderId, amount):
			// Actually approved (confirm response lost / concurrent idempotent confirm). Credit it.
			if rerr := model.RechargeToss(orderId, auth.PaymentKey, c.ClientIP()); rerr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm-verify recharge failed order_id=%s error=%q", orderId, rerr.Error()))
				tossRedirect(c, "/console/topup")
				return
			}
			logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded (confirm-verify) order_id=%s", orderId))
			tossRedirect(c, "/console/log")
			return
		case isTossTerminalFailStatus(auth.Status):
			// EXPIRED / ABORTED → close so it isn't stranded pending.
			target := common.TopUpStatusFailed
			if auth.Status == "EXPIRED" {
				target = common.TopUpStatusExpired
			}
			_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, target)
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm failed, authoritative %s order_id=%s — closed", auth.Status, orderId))
			tossRedirect(c, "/console/topup")
			return
		default:
			// READY / IN_PROGRESS / CANCELED (incl. the HTTP 409 idempotent-processing case) — leave
			// pending; the PAYMENT_STATUS_CHANGED webhook resolves it.
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm failed, authoritative status=%s order_id=%s — left pending", auth.Status, orderId))
			tossRedirect(c, "/console/topup")
			return
		}
	}
	if !isValidTossTopUpCardPayment(result, orderId, amount) || result.PaymentKey != paymentKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed order_id=%s status=%s total=%d resp_order=%s currency=%s card_present=%t", orderId, result.Status, result.TotalAmount, result.OrderId, result.Currency, result.Card != nil))
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
	tossRedirect(c, tossFailureRedirectPath(orderId, code, message))
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

func isTossPaymentWebhookEventType(eventType string) bool {
	switch eventType {
	case "PAYMENT_STATUS_CHANGED":
		return true
	default:
		return false
	}
}

func handleTossSubscriptionPaymentWebhook(c *gin.Context, orderId, paymentKey, status string, isCancel bool) bool {
	ctx := c.Request.Context()
	order := model.GetSubscriptionOrderByTradeNo(orderId)
	if order == nil || order.PaymentProvider != model.PaymentProviderToss {
		return false
	}
	if !isCancel {
		if status != "DONE" {
			c.Status(http.StatusOK)
			return true
		}
		if order.Status == common.TopUpStatusSuccess {
			c.Status(http.StatusOK)
			return true
		}
		if order.Status != common.TopUpStatusPending {
			c.Status(http.StatusOK)
			return true
		}
		if paymentKey == "" {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook missing paymentKey order_id=%s status=%s", orderId, status))
			c.Status(http.StatusOK)
			return true
		}
		if order.BillingKeyId <= 0 {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION: subscription DONE webhook has no billing key order_id=%s user_id=%d", orderId, order.UserId))
			c.Status(http.StatusOK)
			return true
		}
		plan, err := model.GetSubscriptionPlanById(order.PlanId)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription DONE webhook load plan failed order_id=%s plan_id=%d error=%q", orderId, order.PlanId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		chargeKRW := tossSubscriptionOrderChargeKRW(order, plan)
		if !model.IsTossCardAmountPayableKRW(chargeKRW) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook blocked below minimum order_id=%s amount=%d", orderId, chargeKRW))
			c.Status(http.StatusOK)
			return true
		}
		secretKey := tossSecretFromCredential(ctx, order.ProviderCredential, setting.TossActiveBillingSecretKey())
		auth, _, err := getTossPaymentWithSecret(ctx, paymentKey, secretKey)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription DONE webhook get payment failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		if !isValidTossBillingCharge(auth, orderId, chargeKRW) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook verification failed order_id=%s auth_order=%s status=%s amount=%d", orderId, auth.OrderId, auth.Status, auth.TotalAmount))
			c.Status(http.StatusOK)
			return true
		}
		payload, _ := common.Marshal(auth)
		if err := model.CompleteTossBillingOrder(orderId, order.BillingKeyId, string(payload)); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription DONE webhook complete failed order_id=%s error=%q", orderId, err.Error()))
			if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
				errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
				errors.Is(err, model.ErrPaymentMethodMismatch) {
				c.Status(http.StatusOK)
			} else {
				c.Status(http.StatusServiceUnavailable)
			}
			return true
		}
		c.Status(http.StatusOK)
		return true
	}
	if paymentKey == "" {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION: subscription cancel webhook missing paymentKey order_id=%s status=%s", orderId, status))
		c.Status(http.StatusOK)
		return true
	}
	secretKey := tossSecretFromCredential(ctx, order.ProviderCredential, setting.TossActiveBillingSecretKey())
	auth, _, err := getTossPaymentWithSecret(ctx, paymentKey, secretKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss subscription webhook get payment failed (cancel) order_id=%s error=%q", orderId, err.Error()))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if auth.OrderId != orderId {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription webhook order mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
		c.Status(http.StatusOK)
		return true
	}
	if isTossCancelStatus(auth.Status) {
		paymentMethod := strings.TrimSpace(order.PaymentMethod)
		if paymentMethod == "" {
			paymentMethod = "toss"
		}
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment %s after activation order_id=%s user_id=%d amount=%d KRW - local subscription/quota NOT reverted, manual adjustment needed", auth.Status, orderId, order.UserId, auth.TotalAmount))
		model.RecordTopupLog(order.UserId, fmt.Sprintf("Toss subscription payment %s after activation (amount: %d KRW) - manual subscription/quota reconciliation required", auth.Status, auth.TotalAmount), c.ClientIP(), paymentMethod, "toss-subscription-cancel")
	}
	c.Status(http.StatusOK)
	return true
}

// TossWebhook handles Toss payment/billing events for card payments.
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
// Payment actions are re-verified by re-fetching the authoritative payment from Toss, so
// payment credit/cancel decisions are never made from the unauthenticated webhook payload alone.
// Subscription payment cancel events are also re-verified and logged for manual reconciliation.
// BILLING_DELETED is matched against the encrypted local billing key before local revocation.
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
		EventType  string `json:"eventType"`
		BillingKey string `json:"billingKey"`
		Reason     string `json:"reason"`
		Data       struct {
			OrderId     string `json:"orderId"`
			PaymentKey  string `json:"paymentKey"`
			BillingKey  string `json:"billingKey"`
			CustomerKey string `json:"customerKey"`
			Status      string `json:"status"`
			TotalAmount int64  `json:"totalAmount"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &event); err != nil {
		logger.LogError(ctx, "Toss webhook parse failed: "+err.Error())
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	if event.EventType == "BILLING_DELETED" {
		billingKey := strings.TrimSpace(event.BillingKey)
		if billingKey == "" {
			billingKey = strings.TrimSpace(event.Data.BillingKey)
		}
		if billingKey == "" {
			logger.LogWarn(ctx, "Toss billing deleted webhook missing billingKey")
			c.Status(http.StatusOK)
			return
		}
		revoked, err := model.RevokeTossBillingKeyByPlain(event.Data.CustomerKey, billingKey)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss billing deleted webhook revoke failed customer_key=%s error=%q", event.Data.CustomerKey, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if revoked {
			logger.LogInfo(ctx, fmt.Sprintf("Toss billing key revoked from BILLING_DELETED webhook customer_key=%s", event.Data.CustomerKey))
		}
		c.Status(http.StatusOK)
		return
	}
	if !isTossPaymentWebhookEventType(event.EventType) {
		c.Status(http.StatusOK)
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
		if handleTossSubscriptionPaymentWebhook(c, orderId, event.Data.PaymentKey, status, isCancel) {
			return
		}
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
			secretKey := tossSecretFromCredential(ctx, topUp.ProviderCredential, setting.TossActiveSecretKey())
			auth, _, err := getTossPaymentWithSecret(ctx, event.Data.PaymentKey, secretKey)
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
	secretKey := tossSecretFromCredential(ctx, topUp.ProviderCredential, setting.TossActiveSecretKey())
	auth, _, err := getTossPaymentWithSecret(ctx, event.Data.PaymentKey, secretKey)
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
		if !isValidTossTopUpCardPayment(auth, orderId, topUp.Amount) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss webhook DONE authoritative mismatch order_id=%s total=%d currency=%s card_present=%t", orderId, auth.TotalAmount, auth.Currency, auth.Card != nil))
			c.Status(http.StatusOK)
			return
		}
		// Persist the paymentKey before crediting (committed independently of the credit txn) so
		// the stale-pending sweep can't expire this approved order if RechargeToss rolls back or a
		// webhook retry is delayed past the sweep window. provider_order_id must != trade_no first.
		if err := model.RecordTossPaymentKey(orderId, auth.PaymentKey); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss webhook record paymentKey failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
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
