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

const (
	tossBillingChargeTimeout = 70 * time.Second
)

var errTossBillingIssueIdempotencyWindowExpired = errors.New("Toss billing ISSUE idempotency window may have expired")

func init() {
	model.SetTossTopUpReconciler(reconcileTossRecordedTopUp)
	model.SetTossSubscriptionOrderReconciler(reconcileTossPendingSubscriptionOrder)
	model.SetTossBillingCharger(tossBillingChargeForModel)
	model.SetTossRenewalPaymentLookup(lookupTossRenewalPayment)
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
		return sanitizeTossCardCompany(r.Card.Company)
	}
	if strings.TrimSpace(r.CardCompany) != "" {
		return sanitizeTossCardCompany(r.CardCompany)
	}
	return sanitizeTossCardCompany(r.Card.IssuerCode)
}

func (r *tossBillingIssueResponse) cardNumberForStorage() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.Card.Number) != "" {
		return sanitizeTossCardNumber(r.Card.Number)
	}
	return sanitizeTossCardNumber(r.CardNumber)
}

func sanitizeTossBillingIssueResponse(result *tossBillingIssueResponse) {
	if result == nil {
		return
	}
	result.CardCompany = sanitizeTossCardCompany(result.CardCompany)
	result.CardNumber = sanitizeTossCardNumber(result.CardNumber)
	result.Card.Company = sanitizeTossCardCompany(result.Card.Company)
	result.Card.IssuerCode = sanitizeTossCardCompany(result.Card.IssuerCode)
	result.Card.Number = sanitizeTossCardNumber(result.Card.Number)
}

// tossSubscriptionChargeKRW converts a plan's USD-equivalent price to KRW for Toss.
// Delegates to model.TossPlanKRW as the single conversion source.
func tossSubscriptionChargeKRW(plan *model.SubscriptionPlan) int64 {
	return model.TossPlanKRW(plan.PriceAmount)
}

func tossSubscriptionChargeKRWWithSnapshot(plan *model.SubscriptionPlan, snapshot setting.TossConfigSnapshot) int64 {
	if plan == nil {
		return 0
	}
	return model.TossPlanKRWWithUnitPrice(plan.PriceAmount, snapshot.UnitPrice)
}

// TossSubscriptionCheckoutSnapshot is the public, non-secret projection of the
// immutable terms that a buyer must review before starting billing auth. The
// fingerprint covers all subscription terms stored in PlanSnapshot (except the
// order-bound trade number), while the explicit fields let the client display
// and compare the monetary terms without interpreting the fingerprint.
type TossSubscriptionCheckoutSnapshot struct {
	PlanId              int     `json:"plan_id"`
	PlanTitle           string  `json:"plan_title"`
	PriceAmount         float64 `json:"price_amount"`
	PriceCurrency       string  `json:"price_currency"`
	ProviderAmount      int64   `json:"provider_amount"`
	ProviderCurrency    string  `json:"provider_currency"`
	SnapshotFingerprint string  `json:"snapshot_fingerprint"`
}

type tossSubscriptionCheckoutFingerprintPayload struct {
	Version          int     `json:"version"`
	PlanId           int     `json:"plan_id"`
	Title            string  `json:"title"`
	PriceAmount      float64 `json:"price_amount"`
	Currency         string  `json:"currency"`
	DurationUnit     string  `json:"duration_unit"`
	DurationValue    int     `json:"duration_value"`
	CustomSeconds    int64   `json:"custom_seconds"`
	MaxPurchaseCount int     `json:"max_purchase_per_user"`
	UpgradeGroup     string  `json:"upgrade_group"`
	TotalAmount      int64   `json:"total_amount"`
	ResetPeriod      string  `json:"quota_reset_period"`
	ResetSeconds     int64   `json:"quota_reset_custom_seconds"`
	ProviderAmount   int64   `json:"provider_amount"`
	ProviderCurrency string  `json:"provider_currency"`
}

func newTossSubscriptionCheckoutSnapshot(plan *model.SubscriptionPlan, providerAmount int64, providerCurrency string) *TossSubscriptionCheckoutSnapshot {
	if plan == nil || plan.Id <= 0 || plan.PriceAmount <= 0 || !model.IsTossCardAmountPayableKRW(providerAmount) {
		return nil
	}
	priceCurrency := strings.ToUpper(strings.TrimSpace(plan.Currency))
	providerCurrency = strings.ToUpper(strings.TrimSpace(providerCurrency))
	if priceCurrency == "" || providerCurrency != "KRW" {
		return nil
	}
	payload, err := common.Marshal(tossSubscriptionCheckoutFingerprintPayload{
		Version:          1,
		PlanId:           plan.Id,
		Title:            plan.Title,
		PriceAmount:      plan.PriceAmount,
		Currency:         priceCurrency,
		DurationUnit:     plan.DurationUnit,
		DurationValue:    plan.DurationValue,
		CustomSeconds:    plan.CustomSeconds,
		MaxPurchaseCount: plan.MaxPurchasePerUser,
		UpgradeGroup:     plan.UpgradeGroup,
		TotalAmount:      plan.TotalAmount,
		ResetPeriod:      plan.QuotaResetPeriod,
		ResetSeconds:     plan.QuotaResetCustomSeconds,
		ProviderAmount:   providerAmount,
		ProviderCurrency: providerCurrency,
	})
	if err != nil {
		return nil
	}
	return &TossSubscriptionCheckoutSnapshot{
		PlanId:              plan.Id,
		PlanTitle:           plan.Title,
		PriceAmount:         plan.PriceAmount,
		PriceCurrency:       priceCurrency,
		ProviderAmount:      providerAmount,
		ProviderCurrency:    providerCurrency,
		SnapshotFingerprint: common.Sha1(payload),
	}
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

// tossBillingIssueAuthorizationExpired enforces the application's checkout
// reservation lifetime; it is not a Toss-documented authKey expiry. Once this
// local window closes, an already-attempted ISSUE may be repeated only to
// discover and delete the key created by the original idempotent request; it
// must never be attached or charged. Use the database clock because workers
// can run on different hosts.
func tossBillingIssueAuthorizationExpired(createTime int64) bool {
	if createTime <= 0 {
		return true
	}
	return createTime <= model.GetDBTimestamp()-model.TossSubscriptionPurchaseReservationMaxAgeSeconds
}

// tossBillingIssueReplayMayBeOutsideIdempotencyWindow prevents an automatic
// POST replay once Toss's documented 15-day idempotency retention can no
// longer be proven. CreateTime precedes the first possible ISSUE request, so
// using it as the lower bound can stop a replay slightly early but can never
// extend the provider guarantee. The encrypted authorization remains durable
// for explicit operator reconciliation instead of risking a second provider
// key or erasing evidence after a newly evaluated authKey rejection.
func tossBillingIssueReplayMayBeOutsideIdempotencyWindow(createTime int64) bool {
	if createTime <= 0 {
		return true
	}
	return createTime <= model.GetDBTimestamp()-model.TossProviderIdempotencyRetentionSeconds
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
	cleanupReplay := model.IsTossBillingIssueCleanupContext(ctx)
	providedSecret := strings.TrimSpace(secretKey)
	if cleanupReplay && (providedSecret == "" || strings.TrimSpace(idempotencyKey) == "") {
		return nil, 0, errors.New("Toss billing ISSUE cleanup requires the exact secret and idempotency key")
	}
	secretKey = tossBillingSecretOrActive(secretKey)
	ctx, cancel := tossPaymentOperationContext(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{"authKey": authKey, "customerKey": customerKey}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	if !cleanupReplay {
		freshConfig, err := model.GetFreshTossConfigSnapshot()
		if err != nil {
			return nil, 0, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
		}
		if !freshConfig.BillingEnabled || !isPaymentComplianceConfirmed() {
			return nil, 0, model.ErrTossBillingOperationallyDisabled
		}
		if model.IsTossWalletAutoRechargeIssueContext(ctx) && !freshConfig.WalletAutoRechargeEnabled {
			return nil, 0, model.ErrTossBillingOperationallyDisabled
		}
	}
	statusCode, rb, err := doTossAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/billing/authorizations/issue", body, idempotencyKey, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss billing issue failed: %w", err)
	}
	var out tossBillingIssueResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, statusCode, err
	}
	sanitizeTossBillingIssueResponse(&out)
	if err := validateTossBillingIssueResponseShape(&out); err != nil {
		return &out, statusCode, fmt.Errorf("invalid Toss billing issue response: %w", err)
	}
	return &out, statusCode, nil
}

func validateTossBillingIssueResponseShape(result *tossBillingIssueResponse) error {
	if result == nil {
		return errors.New("Toss billing issue response is empty")
	}
	if !isValidTossBillingKey(result.BillingKey) {
		return errors.New("Toss billing issue response has an invalid billingKey")
	}
	if result.CustomerKey != strings.TrimSpace(result.CustomerKey) ||
		len(result.CustomerKey) < 2 || len([]rune(result.CustomerKey)) > 300 {
		return errors.New("Toss billing issue response has an invalid customerKey")
	}
	if result.cardCompanyForStorage() == "" || result.cardNumberForStorage() == "" {
		return errors.New("Toss billing issue response is missing card information")
	}
	return nil
}

// chargeTossBilling charges a billing key. Reuses tossConfirmResponse (status/totalAmount/orderId/currency).
func chargeTossBilling(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	return chargeTossBillingWithSecret(ctx, billingKey, customerKey, setting.TossActiveBillingSecretKey(), orderId, orderName, amount)
}

func requireTossWalletAutoRechargeChargeOperational(ctx context.Context, orderId string) error {
	isWalletCharge := strings.HasPrefix(orderId, "wallet_auto_") || model.HasTossWalletOpaqueOrderIDPrefix(orderId) ||
		model.IsTossWalletAutoRechargeChargeContext(ctx)
	if isWalletCharge && !isTossWalletAutoRechargeOperationallyEnabled() {
		return model.ErrTossBillingOperationallyDisabled
	}
	return nil
}

func chargeTossBillingWithSecret(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	secretKey = tossBillingSecretOrActive(secretKey)
	// Toss documents that card auto-billing approval can take up to 60 seconds.
	ctx, cancel := tossPaymentOperationContext(ctx, tossBillingChargeTimeout)
	defer cancel()
	payload := map[string]interface{}{
		"customerKey":        customerKey,
		"amount":             amount,
		"orderId":            orderId,
		"orderName":          orderName,
		"taxFreeAmount":      int64(0),
		"taxExemptionAmount": int64(0),
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	freshConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
	}
	if !freshConfig.BillingEnabled || !isPaymentComplianceConfirmed() {
		return nil, 0, model.ErrTossBillingOperationallyDisabled
	}
	// Recheck the separate non-subscription contract at the HTTP boundary. The
	// scheduler persists its attempt marker first; an administrator can disable
	// wallet auto recharge in the short interval before this provider POST.
	isWalletCharge := strings.HasPrefix(orderId, "wallet_auto_") || model.HasTossWalletOpaqueOrderIDPrefix(orderId) ||
		model.IsTossWalletAutoRechargeChargeContext(ctx)
	if isWalletCharge && !freshConfig.WalletAutoRechargeEnabled {
		return nil, 0, model.ErrTossBillingOperationallyDisabled
	}
	statusCode, rb, err := doTossPaymentProcessingAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/billing/"+url.PathEscape(billingKey), body, orderId, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss billing charge failed: %w", err)
	}
	var out tossConfirmResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, statusCode, err
	}
	sanitizeTossPaymentResponse(&out)
	if err := validateTossBillingChargeResponseShape(&out); err != nil {
		return &out, statusCode, fmt.Errorf("invalid Toss billing charge response: %w", err)
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
	sanitizeTossPaymentResponse(&result)
	if err := validateTossPaymentResponseShape(&result); err != nil {
		return nil, statusCode, fmt.Errorf("invalid Toss payment order lookup response: %w", err)
	}
	if result.Type != "BILLING" {
		return &result, statusCode, errors.New("Toss billing payment order lookup returned an unexpected payment type")
	}
	if result.OrderId != orderId {
		return &result, statusCode, errors.New("Toss payment order lookup returned a different orderId")
	}
	return &result, statusCode, nil
}

func getTossPaymentByOrderIdWithCredentialForMID(ctx context.Context, orderId, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret string) (*tossConfirmResponse, int, error) {
	candidates := tossCredentialCandidatesForMID(ctx, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret)
	return getTossPaymentByOrderIdWithCredentialCandidates(ctx, orderId, candidates)
}

func getTossPaymentByOrderIdWithCredentialCandidates(ctx context.Context, orderId string, candidates []string) (*tossConfirmResponse, int, error) {
	if len(candidates) == 0 {
		return nil, 0, errors.New("Toss billing secret key is unavailable")
	}
	var result *tossConfirmResponse
	var status int
	var err error
	for i := range candidates {
		result, status, err = getTossPaymentByOrderIdWithSecret(ctx, orderId, candidates[i])
		if err == nil || !isTossCredentialRejection(status, err) || i == len(candidates)-1 {
			return result, status, err
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss stored billing credential rejected during order lookup; retrying with current key order_id=%s", orderId))
	}
	return result, status, err
}

// tossSubscriptionPaymentCredentialCandidates keeps webhook verification tied
// to the credential namespace that actually performed the subscription charge.
// BillingAttemptCredential is authoritative for the POST attempt and must be
// tried before the order-time credential. The active credential is appended by
// tossCredentialCandidatesForMID only when it belongs to the same MID snapshot.
func tossSubscriptionPaymentCredentialCandidates(ctx context.Context, order *model.SubscriptionOrder, activeClientKey, activeSecret string) []string {
	if order == nil {
		return nil
	}
	candidates := make([]string, 0, 3)
	if encrypted := strings.TrimSpace(order.BillingAttemptCredential); encrypted != "" {
		attempt, err := model.DecryptProviderCredential(encrypted)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing attempt credential decrypt failed for subscription payment lookup order_id=%s: %v", order.TradeNo, err))
		} else if attempt = strings.TrimSpace(attempt); attempt != "" {
			candidates = append(candidates, attempt)
		}
	}
	for _, candidate := range tossCredentialCandidatesForMID(ctx, order.ProviderCredential, order.ProviderClientKeyHash, activeClientKey, activeSecret) {
		duplicate := false
		for _, existing := range candidates {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

// getTossSubscriptionPaymentForWebhook is deliberately lookup-only. In
// particular, falling back after an expired attempt credential must never
// repeat a POST under another API-key idempotency namespace.
func getTossSubscriptionPaymentForWebhook(ctx context.Context, paymentKey string, order *model.SubscriptionOrder) (*tossConfirmResponse, int, error) {
	activeClientKey, activeSecret := setting.TossActiveBillingKeyPair()
	candidates := tossSubscriptionPaymentCredentialCandidates(ctx, order, activeClientKey, activeSecret)
	lookupCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, status, err := getTossPaymentWithCredentialCandidates(lookupCtx, paymentKey, candidates)
	if err == nil && result.Type != "BILLING" {
		return result, status, errors.New("Toss subscription payment lookup returned an unexpected payment type")
	}
	return result, status, err
}

func confirmTossBillingChargeOrLookup(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, error) {
	return confirmTossBillingChargeOrLookupWithSecret(ctx, billingKey, customerKey, setting.TossActiveBillingSecretKey(), orderId, orderName, amount)
}

func confirmTossBillingChargeOrLookupWithSecret(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*tossConfirmResponse, error) {
	result, statusCode, err := chargeTossBillingWithSecret(ctx, billingKey, customerKey, secretKey, orderId, orderName, amount)
	if err == nil {
		return classifyTossBillingChargeResponseWithContext(ctx, result, orderId, amount)
	}
	auth, lookupStatus, lookupErr := getTossPaymentByOrderIdWithSecret(ctx, orderId, secretKey)
	if lookupErr == nil {
		auth, authErr := classifyTossBillingChargeResponseWithContext(ctx, auth, orderId, amount)
		if authErr == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing charge response lost but recovered by order lookup order_id=%s", orderId))
		}
		return auth, authErr
	}
	if isTossAmbiguousPaymentOutcome(statusCode, err) {
		return nil, fmt.Errorf("%w: charge status=%d lookup status=%d err=%v", model.ErrTossBillingChargePending, statusCode, lookupStatus, lookupErr)
	}
	return nil, fmt.Errorf("%w; order lookup failed: %v", err, lookupErr)
}

// classifyTossBillingChargeResponse converts every syntactically successful
// Payment object into an explicit financial state. Only a strictly matching
// DONE response is success; all other states must keep or close the durable
// attempt deliberately instead of being mistaken for a completed charge.
func classifyTossBillingChargeResponse(result *tossConfirmResponse, orderId string, amount int64) (*tossConfirmResponse, error) {
	return classifyTossBillingChargeResponseWithContext(context.Background(), result, orderId, amount)
}

func classifyTossBillingChargeResponseWithContext(ctx context.Context, result *tossConfirmResponse, orderId string, amount int64) (*tossConfirmResponse, error) {
	if result == nil {
		return nil, fmt.Errorf("%w: empty Toss billing payment", model.ErrTossBillingChargePending)
	}
	if result.OrderId != orderId {
		return result, errors.New("Toss billing payment identity mismatch")
	}
	status := strings.ToUpper(strings.TrimSpace(result.Status))
	switch status {
	case "DONE":
		if !isValidTossBillingCharge(result, orderId, amount) {
			return result, errors.New("Toss billing payment DONE payload mismatch")
		}
		return result, nil
	case "CANCELED", "PARTIAL_CANCELED":
		if !isValidTossCardCancellation(result, orderId, amount) {
			return result, errors.New("Toss billing cancellation amount or payment method mismatch")
		}
		if _, _, err := persistExpectedTossCancellationEvents(ctx, result, orderId, amount); err != nil {
			return result, fmt.Errorf("%w: persist Toss billing cancellation: %v", model.ErrTossBillingChargePending, err)
		}
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeCanceled, status)
	case "ABORTED", "EXPIRED":
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeTerminal, status)
	case "READY", "IN_PROGRESS":
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, status)
	default:
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, status)
	}
}

func tossBillingChargeForModel(ctx context.Context, billingKey, customerKey, secretKey, orderId, orderName string, amount int64) (*model.TossBillingChargeResult, error) {
	// Wallet auto recharge shares the billing HTTP adapter with subscriptions,
	// but its non-subscription contract approval is separate. This is the final
	// boundary before the adapter can perform a new provider POST. Read-only
	// lookup/reconciliation uses a different callback and remains available.
	if err := requireTossWalletAutoRechargeChargeOperational(ctx, orderId); err != nil {
		return nil, err
	}
	res, err := confirmTossBillingChargeOrLookupWithSecret(ctx, billingKey, customerKey, secretKey, orderId, orderName, amount)
	result := &model.TossBillingChargeResult{}
	if res == nil {
		return result, err
	}
	result.Done = isValidTossBillingCharge(res, orderId, amount)
	result.ProviderStatus = res.Status
	result.Total = res.TotalAmount
	result.BalanceAmount = res.BalanceAmount
	result.PaymentKey = res.PaymentKey
	if payload, err := common.Marshal(res); err == nil {
		result.ProviderPayload = string(payload)
	}
	return result, err
}

func lookupTossRenewalPayment(ctx context.Context, secretKey, orderId string, amount int64) (*model.TossBillingChargeResult, error) {
	res, statusCode, err := getTossPaymentByOrderIdWithSecret(ctx, orderId, secretKey)
	if err != nil {
		if statusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: order_id=%s", model.ErrTossBillingPaymentNotFound, orderId)
		}
		return nil, fmt.Errorf("Toss renewal order lookup failed: status=%d err=%w", statusCode, err)
	}
	if res == nil {
		return nil, errors.New("Toss renewal order lookup returned an empty payment")
	}
	result := &model.TossBillingChargeResult{
		ProviderStatus: res.Status,
		Total:          res.TotalAmount,
		BalanceAmount:  res.BalanceAmount,
		PaymentKey:     res.PaymentKey,
	}
	if payload, marshalErr := common.Marshal(res); marshalErr == nil {
		result.ProviderPayload = string(payload)
	}
	switch strings.ToUpper(strings.TrimSpace(res.Status)) {
	case "DONE":
		result.Done = isValidTossBillingCharge(res, orderId, amount)
		if !result.Done {
			return result, errors.New("Toss renewal order lookup payload mismatch")
		}
		return result, nil
	case "READY", "IN_PROGRESS":
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, res.Status)
	case "CANCELED":
		if !isValidTossCardCancellation(res, orderId, amount) {
			return result, errors.New("Toss renewal cancellation amount or payment method mismatch")
		}
		if _, _, persistErr := persistExpectedTossCancellationEvents(ctx, res, orderId, amount); persistErr != nil {
			return result, fmt.Errorf("persist Toss renewal cancellation: %w", persistErr)
		}
		if res.BalanceAmount != 0 {
			return result, fmt.Errorf("Toss renewal terminal payment has non-zero balance: status=%s balance=%d", res.Status, res.BalanceAmount)
		}
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeCanceled, res.Status)
	case "ABORTED", "EXPIRED":
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeTerminal, res.Status)
	case "PARTIAL_CANCELED":
		// A partial cancellation still has provider-side money movement. Keep
		// the attempt claimed for reconciliation instead of charging again.
		if !isValidTossCardCancellation(res, orderId, amount) {
			return result, errors.New("Toss renewal partial cancellation amount or payment method mismatch")
		}
		if _, _, persistErr := persistExpectedTossCancellationEvents(ctx, res, orderId, amount); persistErr != nil {
			return result, fmt.Errorf("persist Toss renewal partial cancellation: %w", persistErr)
		}
		return result, fmt.Errorf("%w: payment status=%s balance=%d", model.ErrTossBillingChargeCanceled, res.Status, res.BalanceAmount)
	default:
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, res.Status)
	}
}

func isTossProviderCredentialRejection(err error) bool {
	switch tossAPIErrorCode(err) {
	case "INVALID_API_KEY", "UNAUTHORIZED_KEY", "INCORRECT_BASIC_AUTH_FORMAT":
		return true
	default:
		return false
	}
}

// lookupTossBillingAttemptWithMIDFallback uses a rotated current secret only
// for an authoritative GET and only when the order's durable client-key
// fingerprint proves that the secret belongs to the same MID. A caller must
// never turn usedFallback+404 into a POST: Toss includes the API key in its
// idempotency scope, so the new secret is a different retry namespace.
func lookupTossBillingAttemptWithMIDFallback(ctx context.Context, order *model.SubscriptionOrder, exactAttemptSecret string, amount int64) (charge *model.TossBillingChargeResult, usedFallback bool, err error) {
	if order == nil {
		return nil, false, errors.New("Toss subscription order is unavailable")
	}
	charge, err = lookupTossRenewalPayment(ctx, exactAttemptSecret, order.TradeNo, amount)
	if !isTossProviderCredentialRejection(err) {
		return charge, false, err
	}
	storedClientKeyHash := order.ProviderClientKeyHash
	activeClientKey, activeSecret := setting.TossActiveBillingKeyPair()
	activeSecret = strings.TrimSpace(activeSecret)
	if !model.IsValidTossClientKeyFingerprint(storedClientKeyHash) || activeSecret == "" || activeSecret == strings.TrimSpace(exactAttemptSecret) ||
		storedClientKeyHash != model.TossBillingClientKeyFingerprint(activeClientKey) {
		return charge, false, err
	}
	charge, err = lookupTossRenewalPayment(ctx, activeSecret, order.TradeNo, amount)
	return charge, true, err
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

func revokeTossBillingKey(ctx context.Context, billingKeyId int, reason string) {
	if billingKeyId <= 0 {
		// Issued keys must first be represented by a durable local row. A raw
		// provider DELETE cannot prove that the same provider key is not already
		// active through another legacy duplicate row.
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: billing key cleanup has no durable identity reason=%s", reason))
		return
	}
	if err := model.RevokeStoredTossBillingKey(ctx, billingKeyId); err != nil {
		// RevokeStored leaves the key pending for the background retry worker.
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing key identity-aware revoke failed id=%d reason=%s err=%v", billingKeyId, reason, err))
	}
}

func queueIssuedTossBillingKeyForRevocation(ctx context.Context, userId int, tradeNo, fallbackCustomerKey string, issued *tossBillingIssueResponse, secretKey, reason string) int {
	if userId <= 0 || issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return 0
	}
	// The request-side customerKey is the authoritative namespace. A malformed
	// provider echo must not exceed local storage bounds or split the same
	// provider billing key into a different identity group during cleanup.
	customerKey := strings.TrimSpace(fallbackCustomerKey)
	if customerKey == "" {
		customerKey = strings.TrimSpace(issued.CustomerKey)
	}
	providerClientKeyHash, hashErr := model.GetTossProviderClientKeyHashByTradeNo(tradeNo)
	if hashErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss issued billing key MID snapshot lookup failed trade_no=%s reason=%s err=%v", tradeNo, reason, hashErr))
	}
	keyId, err := model.StoreTossBillingKeyPendingRevocationWithProviderSnapshot(
		userId,
		customerKey,
		issued.BillingKey,
		issued.cardCompanyForStorage(),
		issued.cardNumberForStorage(),
		secretKey,
		providerClientKeyHash,
	)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: failed to queue issued Toss billing key for revocation trade_no=%s user_id=%d reason=%s err=%v", tradeNo, userId, reason, err))
		model.RecordLog(userId, model.LogTypeTopup, fmt.Sprintf("Toss billing key cleanup queue failed; manual Toss console check required (trade_no=%s, reason=%s)", tradeNo, reason))
		return 0
	}
	return keyId
}

type tossSubscriptionIssuedBillingKeyCleanup func(
	ctx context.Context,
	userID int,
	tradeNo, fallbackCustomerKey string,
	issued *tossBillingIssueResponse,
	secretKey, reason string,
) error

func durablyRevokeTossSubscriptionIssuedBillingKey(
	ctx context.Context,
	userID int,
	tradeNo, fallbackCustomerKey string,
	issued *tossBillingIssueResponse,
	secretKey, reason string,
) error {
	if issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return errors.New("Toss subscription issued billing key is unavailable")
	}
	billingKeyID := queueIssuedTossBillingKeyForRevocation(ctx, userID, tradeNo, fallbackCustomerKey, issued, secretKey, reason)
	if billingKeyID <= 0 {
		return errors.New("Toss subscription billing key cleanup was not durably queued")
	}
	// The durable pending-revocation row is the retry boundary. DELETE is best
	// effort here because the background revoker can safely retry it.
	revokeTossBillingKey(ctx, billingKeyID, reason)
	return nil
}

var subscriptionTossIssuedBillingKeyCleanup tossSubscriptionIssuedBillingKeyCleanup = durablyRevokeTossSubscriptionIssuedBillingKey
var subscriptionTossPreclaimOrderCanceller = model.CancelUnattemptedTossSubscriptionOrder

func handleTossBillingActivationFailure(ctx context.Context, userId int, tradeNo string, billingKeyId int, amount int64, result *tossConfirmResponse, providerPayload string, activationErr error, financialMismatch bool) {
	logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription first charge DONE but activation failed trade_no=%s user=%d amount=%d err=%v", tradeNo, userId, amount, activationErr))
	if err := model.PreservePaidTossSubscriptionOrderWithContext(ctx, tradeNo, billingKeyId, providerPayload); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss paid subscription state preserve failed trade_no=%s err=%v", tradeNo, err))
	}
	paymentKey := ""
	originalAmount := amount
	balanceAmount := amount
	if result != nil {
		paymentKey = result.PaymentKey
		if financialMismatch {
			originalAmount = result.TotalAmount
			balanceAmount = result.BalanceAmount
		}
	}
	eventType := model.TossPaymentEventTypeFulfillment
	eventKey := "toss_fulfillment_" + common.Sha1([]byte(tradeNo))
	if financialMismatch {
		eventType = model.TossPaymentEventTypeFinancialMismatch
		eventKey = model.TossFinancialMismatchEventKey(tradeNo, paymentKey, "DONE", originalAmount, balanceAmount)
	}
	created, eventErr := model.RecordTossPaymentEventWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             eventKey,
		EventType:            eventType,
		OrderId:              tradeNo,
		PaymentKey:           paymentKey,
		Status:               "DONE",
		OriginalAmount:       originalAmount,
		BalanceAmount:        balanceAmount,
		ProviderPayload:      providerPayload,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	if eventErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss paid subscription reconciliation event persist failed trade_no=%s err=%v", tradeNo, eventErr))
	}
	// Keep the billing key active and the order pending. The stale-order
	// reconciler will retry local activation from the authoritative orderId.
	if created {
		model.RecordLogWithContext(ctx, userId, model.LogTypeTopup, fmt.Sprintf("Toss 구독 첫 결제 승인됨(%d원), 구독 활성화 재시도 중 (trade_no=%s)", amount, tradeNo))
	}
}

// recordTossSubscriptionFinancialMismatchEvent stores cancellation-shaped
// provider evidence that failed strict identity/amount/card validation. It is
// deliberately a financial-mismatch event, not an
// authoritative cancellation event: the malformed payload must block a
// replacement purchase without being allowed to reverse local state
// automatically.
func recordTossSubscriptionFinancialMismatchEvent(
	ctx context.Context,
	tradeNo, paymentKey, providerStatus string,
	providerTotal, balanceAmount int64,
	providerPayload, reason string,
) error {
	tradeNo = strings.TrimSpace(tradeNo)
	providerStatus = strings.ToUpper(strings.TrimSpace(providerStatus))
	if tradeNo == "" || (providerStatus != "CANCELED" && providerStatus != "PARTIAL_CANCELED") {
		return errors.New("invalid Toss subscription financial mismatch")
	}
	originalAmount := providerTotal
	_, err := model.RecordTossPaymentEventWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             model.TossFinancialMismatchEventKey(tradeNo, paymentKey, providerStatus, originalAmount, balanceAmount),
		EventType:            model.TossPaymentEventTypeFinancialMismatch,
		OrderId:              tradeNo,
		PaymentKey:           strings.TrimSpace(paymentKey),
		Status:               providerStatus,
		OriginalAmount:       originalAmount,
		BalanceAmount:        balanceAmount,
		ProviderPayload:      providerPayload,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
		ResolutionNote:       strings.TrimSpace(reason),
	})
	return err
}

func stopInitialTossSubscriptionAfterCancellationResult(
	ctx context.Context,
	order *model.SubscriptionOrder,
	billingKeyID int,
	status string,
	total, balance int64,
	paymentKey, providerPayload string,
	strictCancellation bool,
) (safeFullCancellation bool, err error) {
	if order == nil {
		return false, errors.New("Toss subscription cancellation order is unavailable")
	}
	status = strings.ToUpper(strings.TrimSpace(status))
	safeFullCancellation = strictCancellation && status == "CANCELED" && balance == 0
	if !safeFullCancellation && !strictCancellation {
		if eventErr := recordTossSubscriptionFinancialMismatchEvent(
			ctx,
			order.TradeNo,
			paymentKey,
			status,
			total,
			balance,
			providerPayload,
			"manual reconciliation required: provider cancellation response did not match the immutable initial subscription order",
		); eventErr != nil {
			return false, eventErr
		}
	}
	if stopErr := model.StopTossSubscriptionBillingAfterCancellationWithContext(ctx, order.TradeNo, providerPayload); stopErr != nil {
		return false, stopErr
	}
	if billingKeyID > 0 {
		revokeTossBillingKey(ctx, billingKeyID, "first_charge_canceled")
	}
	if !safeFullCancellation {
		return false, errors.New("Toss subscription payment requires financial reconciliation")
	}
	return true, nil
}

func isValidTossBillingCharge(result *tossConfirmResponse, orderId string, amount int64) bool {
	return result != nil &&
		strings.TrimSpace(result.PaymentKey) != "" &&
		result.Type == "BILLING" &&
		strings.TrimSpace(result.Method) == "카드" &&
		result.Status == "DONE" &&
		result.TotalAmount == amount &&
		result.BalanceAmount == result.TotalAmount &&
		result.OrderId == orderId &&
		strings.ToUpper(result.Currency) == "KRW" &&
		hasExpectedTossTaxableNonEscrowContract(result) &&
		result.Card != nil && result.Card.Amount == result.TotalAmount
}

func isTossSubscriptionFirstChargeAllowed() bool {
	// Existing issue-time credentials remain usable across secret rotation, so
	// this check intentionally observes only the operational kill switches.
	return setting.GetTossConfigSnapshot().BillingEnabled && isPaymentComplianceConfirmed()
}

func isTossBillingIssueResponseUncertain(issued *tossBillingIssueResponse, statusCode int, err error) bool {
	if err == nil {
		return false
	}
	// A syntactically valid billingKey is enough to perform idempotent DELETE,
	// even when other 2xx response fields (for example card metadata or the
	// echoed customerKey) fail validation. Retaining the authKey forever would
	// strand a known provider credential instead of queuing safe cleanup.
	if issued != nil && isValidTossBillingKey(issued.BillingKey) {
		return false
	}
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return true
	}
	// Toss documents PROVIDER_ERROR as a temporary failure. Treat it, and an
	// idempotent "already processed" response, like the other ambiguous API
	// outcomes so the encrypted authKey snapshot is retained for a same-order
	// retry instead of potentially orphaning an issued billing key.
	return isTossAmbiguousPaymentOutcome(statusCode, err)
}

// isTossBillingIssueRecoveryUncertain distinguishes a definitive rejection of
// the first ISSUE request from a credential rejection observed while replaying
// an older, response-lost ISSUE. In the latter case the expired API key proves
// only that we can no longer read Toss's idempotent response; it does not prove
// that the earlier request failed before creating a billing key. Billing keys
// cannot be queried after issuance, so erasing the encrypted authKey snapshot
// here would permanently orphan a provider credential that may still exist.
func isTossBillingIssueRecoveryUncertain(issued *tossBillingIssueResponse, statusCode int, err error, previouslyAttempted bool) bool {
	if isTossBillingIssueResponseUncertain(issued, statusCode, err) {
		return true
	}
	return previouslyAttempted && isTossCredentialRejection(statusCode, err)
}

func expireClaimedTossSubscriptionBillingIssue(ctx context.Context, tradeNo, claimToken, reason string) error {
	if err := model.ExpireClaimedTossSubscriptionBillingIssue(tradeNo, claimToken); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing issue definitive cleanup failed trade_no=%s reason=%s err=%v", tradeNo, reason, err))
		return err
	}
	return nil
}

// cleanupClaimedTossSubscriptionBillingIssue repeats only the idempotent
// billing-key issue request, recovers the possibly orphaned key, and queues it
// for deletion. It never attaches or charges the key, and it retains the
// encrypted authorization on every unresolved outcome.
func cleanupClaimedTossSubscriptionBillingIssue(
	ctx context.Context,
	order *model.SubscriptionOrder,
	claimToken, authKey, customerKey, secretKey, reason string,
) (resolved bool, retainClaim bool, err error) {
	if order == nil || !order.BillingIssueAttempted {
		return false, false, errors.New("Toss subscription issue cleanup has no provider-attempt marker")
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	if tossBillingIssueReplayMayBeOutsideIdempotencyWindow(order.CreateTime) {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription ISSUE cleanup exceeded provider idempotency window trade_no=%s", tradeNo))
		return false, true, errTossBillingIssueIdempotencyWindowExpired
	}
	issued, statusCode, issueErr := subscriptionTossBillingKeyIssuer(
		model.WithTossBillingIssueCleanupContext(ctx), authKey, customerKey, tradeNo, secretKey,
	)
	if issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		if issueErr == nil {
			issueErr = errors.New("Toss billing issue cleanup returned no billing key")
		}
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription issue cleanup unresolved trade_no=%s reason=%s status=%d err=%v", tradeNo, reason, statusCode, issueErr))
		return false, true, issueErr
	}
	if cleanupErr := subscriptionTossIssuedBillingKeyCleanup(ctx, order.UserId, tradeNo, customerKey, issued, secretKey, reason); cleanupErr != nil {
		return false, true, cleanupErr
	}
	if finishErr := model.FinishClaimedTossSubscriptionBillingIssueCleanup(tradeNo, claimToken); finishErr != nil {
		return false, true, finishErr
	}
	return true, false, nil
}

// terminateAndCleanupClaimedTossSubscriptionBillingIssue first atomically
// removes an old authorization from every user-visible/chargeable path while
// proving that this worker still owns the pending, keyless request. The exact
// ISSUE snapshot and lease remain available until provider cleanup completes.
func terminateAndCleanupClaimedTossSubscriptionBillingIssue(
	ctx context.Context,
	order *model.SubscriptionOrder,
	claimToken, authKey, customerKey, secretKey, reason string,
) (resolved bool, retainClaim bool, err error) {
	if order == nil {
		return false, false, errors.New("Toss subscription issue cleanup order is unavailable")
	}
	if transitionErr := model.TransitionClaimedTossSubscriptionBillingIssueToCleanup(order.TradeNo, claimToken); transitionErr != nil {
		return false, !errors.Is(transitionErr, model.ErrTossBillingClaimLost), transitionErr
	}
	return cleanupClaimedTossSubscriptionBillingIssue(
		ctx, order, claimToken, authKey, customerKey, secretKey, reason,
	)
}

// terminateAndCleanupIssuedTossSubscriptionBillingIssue is the equivalent
// ownership fence when this worker already has the ISSUE response. No provider
// key is queued or deleted until the pending/keyless claim CAS succeeds.
func terminateAndCleanupIssuedTossSubscriptionBillingIssue(
	ctx context.Context,
	order *model.SubscriptionOrder,
	claimToken, customerKey string,
	issued *tossBillingIssueResponse,
	secretKey, reason string,
) (resolved bool, retainClaim bool, err error) {
	if order == nil || issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return false, false, errors.New("Toss subscription issued-key cleanup state is unavailable")
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	if transitionErr := model.TransitionClaimedTossSubscriptionBillingIssueToCleanup(tradeNo, claimToken); transitionErr != nil {
		return false, !errors.Is(transitionErr, model.ErrTossBillingClaimLost), transitionErr
	}
	if cleanupErr := subscriptionTossIssuedBillingKeyCleanup(ctx, order.UserId, tradeNo, customerKey, issued, secretKey, reason); cleanupErr != nil {
		return false, true, cleanupErr
	}
	if finishErr := model.FinishClaimedTossSubscriptionBillingIssueCleanup(tradeNo, claimToken); finishErr != nil {
		return false, true, finishErr
	}
	return true, false, nil
}

// processClaimedTossSubscriptionBillingIssue uses only the durable authKey,
// customerKey and provider credential returned by the model claim. This makes
// both the browser callback and the stale-order worker follow the same safe
// issue -> attach -> first-charge path.
func processClaimedTossSubscriptionBillingIssue(
	ctx context.Context,
	order *model.SubscriptionOrder,
	plan *model.SubscriptionPlan,
	claimToken, authKey, customerKey, secretKey string,
) (resolved bool, retainClaim bool, err error) {
	if order == nil || plan == nil {
		return false, false, errors.New("Toss billing issue order or plan is unavailable")
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	issueWasAttempted := order.BillingIssueAttempted
	if planErr := model.ValidateTossSubscriptionBillingPlan(plan); planErr != nil {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "invalid_billing_period_cleanup")
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue blocked for invalid billing period trade_no=%s err=%v", tradeNo, planErr))
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "invalid_billing_period")
	}
	if tossBillingIssueAuthorizationExpired(order.CreateTime) {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "stale_issue_cleanup")
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue authorization expired before provider request trade_no=%s", tradeNo))
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "stale_authorization")
	}
	chargeKRW := tossSubscriptionOrderChargeKRW(order, plan)
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "below_minimum_cleanup")
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue blocked below minimum trade_no=%s amount=%d", tradeNo, chargeKRW))
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "below_minimum")
	}

	canonical, canonicalErr := model.GetOrCreateTossCustomerKey(order.UserId)
	if canonicalErr != nil {
		return false, false, fmt.Errorf("load canonical Toss customerKey: %w", canonicalErr)
	}
	if canonical != customerKey {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "customer_key_mismatch_cleanup")
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue snapshot customerKey mismatch trade_no=%s", tradeNo))
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "customer_key_mismatch")
	}
	billingUser, userErr := model.GetUserById(order.UserId, false)
	if userErr != nil {
		return false, false, fmt.Errorf("load Toss billing user: %w", userErr)
	}
	if billingUser == nil || billingUser.Status != common.UserStatusEnabled || billingUser.OrganizationId > 0 {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "user_inactive_cleanup")
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue blocked for inactive user trade_no=%s user_id=%d", tradeNo, order.UserId))
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "user_inactive")
	}
	if !isTossSubscriptionFirstChargeAllowed() {
		if order.BillingIssueAttempted {
			return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "billing_disabled_cleanup")
		}
		return false, true, model.ErrTossBillingOperationallyDisabled
	}
	tossConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		return false, issueWasAttempted, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
	}
	activeBillingClient, activeBillingSecret := setting.TossActiveBillingKeyPairFromSnapshot(tossConfig)
	if err := model.MarkTossSubscriptionBillingIssueAttempt(tradeNo, claimToken); err != nil {
		return false, false, err
	}
	// Keep the in-memory marker aligned with the durable state. If the age
	// boundary is crossed here, cleanupClaimed... may repeat only the same
	// idempotent ISSUE and will delete, rather than attach, its result.
	order.BillingIssueAttempted = true
	if tossBillingIssueAuthorizationExpired(order.CreateTime) {
		return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "stale_issue_cleanup")
	}
	if !isTossSubscriptionFirstChargeAllowed() {
		return false, true, model.ErrTossBillingOperationallyDisabled
	}

	issued, issueStatus, issueErr := subscriptionTossBillingKeyIssuer(ctx, authKey, customerKey, tradeNo, secretKey)
	// A rejection from this process's first ISSUE request proves that the old
	// API-key namespace created no billing key. If the fresh configuration has a
	// different secret for the same durable MID, promote that exact credential
	// and reset the attempt marker transactionally before one retry. Prior-attempt
	// recovery must never enter this branch: its original response may have been
	// lost after successfully creating a key in the old namespace.
	if !issueWasAttempted &&
		(issued == nil || strings.TrimSpace(issued.BillingKey) == "") &&
		isTossCredentialRejection(issueStatus, issueErr) {
		activeBillingClient = strings.TrimSpace(activeBillingClient)
		activeBillingSecret = strings.TrimSpace(activeBillingSecret)
		if activeBillingClient != "" && activeBillingSecret != "" && activeBillingSecret != strings.TrimSpace(secretKey) &&
			model.IsValidTossClientKeyFingerprint(order.ProviderClientKeyHash) &&
			order.ProviderClientKeyHash == model.TossBillingClientKeyFingerprint(activeBillingClient) {
			if promotionErr := model.PromoteClaimedTossSubscriptionBillingIssueCredentialAfterRejection(
				tradeNo,
				claimToken,
				secretKey,
				activeBillingClient,
				activeBillingSecret,
			); promotionErr != nil {
				return false, !errors.Is(promotionErr, model.ErrTossBillingClaimLost), promotionErr
			}
			secretKey = activeBillingSecret
			order.BillingIssueAttempted = false
			if tossBillingIssueAuthorizationExpired(order.CreateTime) {
				logger.LogWarn(ctx, fmt.Sprintf("Toss billing issue authorization expired during credential rotation trade_no=%s", tradeNo))
				return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "stale_authorization_after_credential_rotation")
			}
			if !isTossSubscriptionFirstChargeAllowed() {
				return false, false, model.ErrTossBillingOperationallyDisabled
			}
			if err := model.RequireFreshTossConfig(); err != nil {
				return false, false, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
			}
			if err := model.MarkTossSubscriptionBillingIssueAttempt(tradeNo, claimToken); err != nil {
				return false, false, err
			}
			order.BillingIssueAttempted = true
			if tossBillingIssueAuthorizationExpired(order.CreateTime) {
				return terminateAndCleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "stale_issue_cleanup")
			}
			if !isTossSubscriptionFirstChargeAllowed() {
				return false, true, model.ErrTossBillingOperationallyDisabled
			}
			issued, issueStatus, issueErr = subscriptionTossBillingKeyIssuer(ctx, authKey, customerKey, tradeNo, secretKey)
		}
	}
	if issueErr != nil || issued == nil || strings.TrimSpace(issued.BillingKey) == "" || issued.CustomerKey != customerKey {
		if isTossBillingIssueRecoveryUncertain(issued, issueStatus, issueErr, issueWasAttempted) {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: billing key issue response uncertain trade_no=%s user_id=%d status=%d err=%v; retaining encrypted authorization for same-idempotency recovery", tradeNo, order.UserId, issueStatus, issueErr))
			if !issueWasAttempted {
				model.RecordLog(order.UserId, model.LogTypeTopup, fmt.Sprintf("Toss billing key issue uncertain; automatic same-order recovery scheduled (trade_no=%s, status=%d)", tradeNo, issueStatus))
			}
			if issueErr == nil {
				issueErr = errors.New("Toss billing issue returned an uncertain empty response")
			}
			return false, true, issueErr
		}
		logger.LogError(ctx, fmt.Sprintf("Toss billing issue failed definitively trade_no=%s status=%d err=%v", tradeNo, issueStatus, issueErr))
		if issued != nil && strings.TrimSpace(issued.BillingKey) != "" {
			return terminateAndCleanupIssuedTossSubscriptionBillingIssue(ctx, order, claimToken, customerKey, issued, secretKey, "issue_validation_failed")
		}
		return true, false, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "provider_rejected_issue")
	}

	if !isTossSubscriptionFirstChargeAllowed() {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing key issued after billing was disabled; revoking trade_no=%s user_id=%d", tradeNo, order.UserId))
		return terminateAndCleanupIssuedTossSubscriptionBillingIssue(ctx, order, claimToken, customerKey, issued, secretKey, "billing_disabled_after_issue")
	}
	if tossBillingIssueAuthorizationExpired(order.CreateTime) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing key issued after authorization expired; revoking trade_no=%s user_id=%d", tradeNo, order.UserId))
		return terminateAndCleanupIssuedTossSubscriptionBillingIssue(ctx, order, claimToken, customerKey, issued, secretKey, "stale_issue_cleanup")
	}

	// Store the issued key and attach it to the claimed order in one transaction
	// while locking/revalidating the user. This prevents a concurrent disable
	// from missing a just-issued active key.
	billingKeyID, storeErr := model.StoreAndAttachClaimedTossSubscriptionBillingKey(
		tradeNo,
		claimToken,
		customerKey,
		issued.BillingKey,
		issued.cardCompanyForStorage(),
		issued.cardNumberForStorage(),
		secretKey,
		order.ProviderClientKeyHash,
	)
	if storeErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing atomic store/attach failed trade_no=%s err=%v", tradeNo, storeErr))
		if errors.Is(storeErr, model.ErrTossBillingClaimLost) {
			// A newer lease owner may be attaching or charging this exact
			// idempotent ISSUE result. Deleting the shared provider key here would
			// invalidate the winner's work, so the stale worker must simply stand
			// down without cleanup or terminalizing the order.
			return false, false, nil
		}
		// The attachment transaction may have rolled back a newly inserted row, or
		// billingKeyID may name an older active identity used by another contract.
		// Fence the claim before queueing the exact issued token; a newer owner may
		// have attached this same idempotent ISSUE result during the failure pause.
		return terminateAndCleanupIssuedTossSubscriptionBillingIssue(ctx, order, claimToken, customerKey, issued, secretKey, "store_attach_failed")
	}

	// Recheck both the operational kill switch and durable lifecycle state just
	// before charging. Keep the claim until this function returns so cleanup
	// cannot race the first provider request.
	readinessErr := model.ValidateClaimedTossSubscriptionFirstCharge(tradeNo, claimToken, billingKeyID, secretKey)
	issueExpired := tossBillingIssueAuthorizationExpired(order.CreateTime)
	if !isTossSubscriptionFirstChargeAllowed() || readinessErr != nil || issueExpired {
		if errors.Is(readinessErr, model.ErrTossBillingCrossCredentialRetryUnsafe) {
			// The provider namespace cannot be proved. Terminalizing the row or
			// revoking its key could hide an old-binary charge that already moved
			// money, so preserve the claim and exact evidence for reconciliation.
			return false, true, readinessErr
		}
		reason := "first_charge_not_allowed"
		if readinessErr != nil {
			reason = "first_charge_lifecycle_changed"
		} else if issueExpired {
			reason = "stale_issue_before_first_charge"
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge blocked after attach trade_no=%s user_id=%d reason=%s err=%v", tradeNo, order.UserId, reason, readinessErr))
		if expireErr := model.ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo, claimToken); expireErr != nil {
			return false, true, expireErr
		}
		revokeTossBillingKey(ctx, billingKeyID, reason)
		return true, false, nil
	}

	orderName := model.TossSubscriptionOrderName(plan.Title, false)
	charge, chargeErr := confirmTossBillingChargeOrLookupWithSecret(ctx, issued.BillingKey, customerKey, secretKey, tradeNo, orderName, chargeKRW)
	if chargeErr != nil || !isValidTossBillingCharge(charge, tradeNo, chargeKRW) {
		if errors.Is(chargeErr, model.ErrTossBillingChargePending) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge still processing trade_no=%s err=%v", tradeNo, chargeErr))
			return false, false, chargeErr
		}
		if charge != nil && strings.EqualFold(strings.TrimSpace(charge.Status), "DONE") {
			payload, _ := common.Marshal(charge)
			mismatchErr := errors.New("Toss billing first charge DONE payload mismatch")
			handleTossBillingActivationFailure(ctx, order.UserId, tradeNo, billingKeyID, chargeKRW, charge, string(payload), mismatchErr, true)
			return false, false, mismatchErr
		}
		if charge != nil && isTossCancelStatus(strings.ToUpper(strings.TrimSpace(charge.Status))) {
			payload, _ := common.Marshal(charge)
			safeFullCancellation, cancellationErr := stopInitialTossSubscriptionAfterCancellationResult(
				ctx,
				order,
				billingKeyID,
				charge.Status,
				charge.TotalAmount,
				charge.BalanceAmount,
				charge.PaymentKey,
				string(payload),
				errors.Is(chargeErr, model.ErrTossBillingChargeCanceled),
			)
			if cancellationErr != nil {
				return true, false, cancellationErr
			}
			if safeFullCancellation {
				return true, false, nil
			}
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge not done trade_no=%s err=%v", tradeNo, chargeErr))
		if expireErr := model.ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo, claimToken); expireErr != nil {
			return false, true, expireErr
		}
		revokeTossBillingKey(ctx, billingKeyID, "first_charge_failed")
		return true, false, nil
	}

	payload, _ := common.Marshal(charge)
	if completeErr := model.CompleteTossBillingOrder(tradeNo, billingKeyID, string(payload)); completeErr != nil {
		handleTossBillingActivationFailure(ctx, order.UserId, tradeNo, billingKeyID, chargeKRW, charge, string(payload), completeErr, false)
		return false, false, completeErr
	}
	if resolveErr := model.ResolveTossPaymentEvents(tradeNo, model.TossPaymentEventTypeFulfillment); resolveErr != nil {
		return false, false, resolveErr
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss subscription activated trade_no=%s plan=%d user=%d", tradeNo, plan.Id, order.UserId))
	return true, false, nil
}

func reconcileTossSubscriptionBillingIssue(ctx context.Context, order *model.SubscriptionOrder) (resolved bool, err error) {
	if order == nil {
		return true, nil
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	claimToken, claimed, err := model.ClaimStoredTossSubscriptionBillingIssue(tradeNo)
	if err != nil || !claimed {
		return false, err
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken); err == nil && releaseErr != nil {
				err = releaseErr
			}
		}
	}()
	authKey, customerKey, secretKey, snapshotErr := model.GetClaimedTossSubscriptionBillingIssue(tradeNo, claimToken)
	if snapshotErr != nil {
		if errors.Is(snapshotErr, model.ErrTossBillingIssueSnapshotInvalid) {
			claimedOrder, stateErr := model.GetClaimedTossSubscriptionBillingIssueState(tradeNo, claimToken)
			if stateErr != nil {
				retainClaim = true
				return false, errors.Join(snapshotErr, stateErr)
			}
			if claimedOrder.BillingIssueAttempted {
				// Decryption/integrity failure does not prove that the prior ISSUE
				// POST was absent. Preserve the one-time authorization and its claim
				// for a node with the repaired crypto key or explicit operator cleanup.
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: attempted subscription billing issue snapshot is unreadable trade_no=%s user_id=%d; preserving authorization and claim", tradeNo, claimedOrder.UserId))
				return false, snapshotErr
			}
			return true, expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "invalid_snapshot")
		}
		return false, snapshotErr
	}
	claimedOrder, stateErr := model.GetClaimedTossSubscriptionBillingIssueState(tradeNo, claimToken)
	if stateErr != nil {
		return false, stateErr
	}
	plan, planErr := model.ResolveTossSubscriptionOrderPlan(claimedOrder)
	if planErr != nil {
		// A malformed/mismatched immutable snapshot is an operator-visible
		// reconciliation condition. Do not replace it with mutable plan terms or
		// expire the pending order automatically.
		return false, planErr
	}
	resolved, retainClaim, err = processClaimedTossSubscriptionBillingIssue(ctx, claimedOrder, plan, claimToken, authKey, customerKey, secretKey)
	return resolved, err
}

func reconcileTerminalTossSubscriptionBillingIssueCleanup(ctx context.Context, order *model.SubscriptionOrder) (resolved bool, err error) {
	if order == nil {
		return true, nil
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	claimToken, claimed, err := model.ClaimStoredTossSubscriptionBillingIssueCleanup(tradeNo)
	if err != nil || !claimed {
		return false, err
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken); err == nil && releaseErr != nil {
				err = releaseErr
			}
		}
	}()
	authKey, customerKey, secretKey, snapshotErr := model.GetClaimedTossSubscriptionBillingIssueCleanup(tradeNo, claimToken)
	if snapshotErr != nil {
		// A terminal uncertain issue may already have produced a provider key.
		// Never erase a corrupt cleanup snapshot automatically.
		retainClaim = true
		return false, snapshotErr
	}
	resolved, retainClaim, err = cleanupClaimedTossSubscriptionBillingIssue(ctx, order, claimToken, authKey, customerKey, secretKey, "terminal_order_cleanup")
	return resolved, err
}

// reconcileTossSubscriptionFirstCharge recovers an initial billing charge
// after the issuing worker durably marked its POST intent but did not settle the
// local order. A distributed lease allows only one worker to act. Recovery
// always performs an authoritative GET with the exact attempt secret first and
// repeats POST only after Toss returns 404, using that same secret, orderId and
// Idempotency-Key namespace.
func reconcileTossSubscriptionFirstCharge(ctx context.Context, order *model.SubscriptionOrder, plan *model.SubscriptionPlan, chargeKRW int64) (resolved bool, err error) {
	if order == nil || plan == nil {
		return false, errors.New("Toss first-charge reconciliation input is unavailable")
	}
	tradeNo := strings.TrimSpace(order.TradeNo)
	claimToken, claimed, err := model.ClaimTossSubscriptionFirstChargeOrder(tradeNo)
	if err != nil || !claimed {
		return false, err
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken); err == nil && releaseErr != nil {
				err = releaseErr
			}
		}
	}()
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken)
	}

	attemptSecret, err := model.GetClaimedTossSubscriptionFirstChargeAttemptSecret(tradeNo, claimToken)
	if err != nil {
		retainClaim = true
		return false, err
	}
	charge, lookupUsedFallback, lookupErr := lookupTossBillingAttemptWithMIDFallback(ctx, order, attemptSecret, chargeKRW)
	retryingPost := false
	switch {
	case lookupErr == nil:
		// The previous POST already created the payment. Settle it below without
		// issuing another provider request.
	case errors.Is(lookupErr, model.ErrTossBillingPaymentNotFound):
		// At this age no new provider POST is permitted under any credential.
		// Close the durable order even when only a rotated same-MID key can prove
		// the 404; retaining it would create an unrecoverable five-minute loop.
		if tossBillingIssueAuthorizationExpired(order.CreateTime) {
			return expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken)
		}
		if lookupUsedFallback {
			retainClaim = true
			return false, fmt.Errorf("%w: same-MID fallback lookup returned 404 for order_id=%s", model.ErrTossBillingCrossCredentialRetryUnsafe, tradeNo)
		}
		// The exact credential proves that no payment exists in the original
		// idempotency namespace. Once the checkout recovery window has elapsed,
		// another POST is forbidden; close the order and key atomically instead of
		// retaining a claim that would fail the same final gate every five minutes.
		retryingPost = true
		if !isTossSubscriptionFirstChargeAllowed() {
			retainClaim = true
			return false, model.ErrTossBillingOperationallyDisabled
		}
		billingKey, customerKey, _, keyErr := model.GetTossBillingKeyPlainWithSecret(order.BillingKeyId)
		if keyErr != nil {
			retainClaim = true
			return false, keyErr
		}
		// This transaction revalidates the pending order, enabled user and active
		// key immediately before the same-idempotency POST.
		if validateErr := model.ValidateClaimedTossSubscriptionFirstCharge(tradeNo, claimToken, order.BillingKeyId, attemptSecret); validateErr != nil {
			retainClaim = true
			return false, validateErr
		}
		orderName := model.TossSubscriptionOrderName(plan.Title, false)
		charge, err = tossBillingChargeForModel(ctx, billingKey, customerKey, attemptSecret, tradeNo, orderName, chargeKRW)
	case errors.Is(lookupErr, model.ErrTossBillingChargeCanceled):
		if charge == nil {
			retainClaim = true
			return false, errors.New("Toss first-charge cancellation lookup returned no payment")
		}
		return true, model.StopTossSubscriptionBillingAfterCancellation(tradeNo, charge.ProviderPayload)
	case charge != nil && isTossCancelStatus(strings.ToUpper(strings.TrimSpace(charge.ProviderStatus))):
		// The lookup returned a cancellation-shaped Payment but strict validation
		// failed. Persist generic evidence and terminalize this exact order so a
		// later checkout cannot replace potentially outstanding provider money.
		_, stopErr := stopInitialTossSubscriptionAfterCancellationResult(
			ctx,
			order,
			order.BillingKeyId,
			charge.ProviderStatus,
			charge.Total,
			charge.BalanceAmount,
			charge.PaymentKey,
			charge.ProviderPayload,
			false,
		)
		return true, stopErr
	case errors.Is(lookupErr, model.ErrTossBillingChargeTerminal):
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken)
	case errors.Is(lookupErr, model.ErrTossBillingChargePending):
		retainClaim = true
		return false, nil
	case charge != nil && strings.EqualFold(strings.TrimSpace(charge.ProviderStatus), "DONE"):
		// Preserve a financially completed but structurally mismatched response
		// for the durable reconciliation path below.
		err = lookupErr
	default:
		// A credential or transport failure does not prove the previous POST was
		// absent. Keep the exact attempt snapshot for the next stale-claim owner.
		retainClaim = true
		return false, lookupErr
	}

	if errors.Is(err, model.ErrTossBillingChargeCanceled) {
		if charge == nil {
			retainClaim = true
			return false, errors.New("Toss first-charge cancellation returned no payment")
		}
		return true, model.StopTossSubscriptionBillingAfterCancellation(tradeNo, charge.ProviderPayload)
	}
	if charge != nil && isTossCancelStatus(strings.ToUpper(strings.TrimSpace(charge.ProviderStatus))) {
		_, stopErr := stopInitialTossSubscriptionAfterCancellationResult(
			ctx,
			order,
			order.BillingKeyId,
			charge.ProviderStatus,
			charge.Total,
			charge.BalanceAmount,
			charge.PaymentKey,
			charge.ProviderPayload,
			false,
		)
		return true, stopErr
	}
	if errors.Is(err, model.ErrTossBillingChargeTerminal) {
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken)
	}
	if errors.Is(err, model.ErrTossBillingChargePending) {
		retainClaim = true
		return false, nil
	}
	if charge != nil && strings.EqualFold(strings.TrimSpace(charge.ProviderStatus), "DONE") && (!charge.Done || charge.Total != chargeKRW) {
		var auth tossConfirmResponse
		if unmarshalErr := common.UnmarshalJsonStr(charge.ProviderPayload, &auth); unmarshalErr != nil {
			auth.PaymentKey = charge.PaymentKey
			auth.OrderId = tradeNo
			auth.Status = charge.ProviderStatus
			auth.TotalAmount = charge.Total
		}
		handleTossBillingActivationFailure(ctx, order.UserId, tradeNo, order.BillingKeyId, chargeKRW, &auth, charge.ProviderPayload, errors.New("Toss billing first charge DONE payload mismatch"), true)
		retainClaim = true
		return false, errors.New("Toss billing first charge DONE payload mismatch; reconciliation required")
	}
	if err != nil || charge == nil || !charge.Done || charge.Total != chargeKRW {
		if !retryingPost {
			retainClaim = true
			if err == nil {
				err = errors.New("Toss first-charge lookup returned an invalid payment")
			}
			return false, err
		}
		// GET proved the old attempt absent. A definitive same-idempotency POST
		// failure can safely close the order and queue its billing key for cleanup.
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken)
	}
	if err := model.CompleteTossBillingOrder(tradeNo, order.BillingKeyId, charge.ProviderPayload); err != nil {
		var auth tossConfirmResponse
		_ = common.UnmarshalJsonStr(charge.ProviderPayload, &auth)
		handleTossBillingActivationFailure(ctx, order.UserId, tradeNo, order.BillingKeyId, chargeKRW, &auth, charge.ProviderPayload, err, false)
		retainClaim = true
		return false, err
	}
	if err := model.ResolveTossPaymentEvents(tradeNo, model.TossPaymentEventTypeFulfillment); err != nil {
		return false, err
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss subscription first charge recovered order_id=%s", tradeNo))
	return true, nil
}

func reconcileTossPendingSubscriptionOrder(ctx context.Context, order model.SubscriptionOrder) (bool, error) {
	tradeNo := strings.TrimSpace(order.TradeNo)
	if tradeNo == "" {
		return true, nil
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	current, lookupErr := model.GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if errors.Is(lookupErr, model.ErrSubscriptionOrderNotFound) {
		return true, nil
	}
	if lookupErr != nil {
		return false, lookupErr
	}
	if current.PaymentProvider != model.PaymentProviderToss {
		if model.HasTossRenewalOpaqueOrderIDPrefix(current.TradeNo) {
			// trn_ is reserved for the opaque Toss renewal protocol. A partial
			// restore can null only the provider discriminator; treating that row as
			// unrelated would resolve the worker item and permit a replacement POST.
			return false, model.ErrTossRecurringOrderIDEvidenceCorrupt
		}
		return true, nil
	}
	if current.Status == common.TopUpStatusSuccess {
		// Do not resolve from local status alone. Older writers classified some
		// strict amount/card mismatches as fulfillment events; only a currently
		// verified provider response may auto-resolve that legacy evidence.
		return true, nil
	}
	if current.Status != common.TopUpStatusPending {
		if (current.Status == common.TopUpStatusFailed || current.Status == common.TopUpStatusExpired) &&
			current.BillingKeyId <= 0 && current.BillingIssueAttempted && strings.TrimSpace(current.BillingIssueAuthKey) != "" {
			return reconcileTerminalTossSubscriptionBillingIssueCleanup(ctx, current)
		}
		return true, nil
	}
	if current.BillingKeyId <= 0 {
		return reconcileTossSubscriptionBillingIssue(ctx, current)
	}
	renewalSubId, _, _, isRenewalOrder, identityErr := model.ResolveTossRenewalOrderIdentity(current)
	if identityErr != nil {
		return false, identityErr
	}
	invalidRenewalOrder := (strings.HasPrefix(tradeNo, model.TossRenewalTradeNoPrefix) ||
		model.HasTossRenewalOpaqueOrderIDPrefix(tradeNo)) && !isRenewalOrder
	potentialLegacyAttempt := model.IsPotentialLegacyTossSubscriptionCharge(current)
	if isRenewalOrder && (current.BillingAttempted || potentialLegacyAttempt) {
		// A previous renewal POST may already have moved money. Re-enter the
		// distributed renewal claim protocol so recovery decrypts the exact
		// BillingAttemptCredential, performs GET first, and repeats a POST only
		// after a same-secret 404. The generic ProviderCredential lookup below is
		// unsafe after secret/MID rotation.
		if err := model.ProcessTossRenewal(ctx, renewalSubId, model.TossBillingMaxFails); err != nil {
			return false, err
		}
		reloaded, err := model.GetSubscriptionOrderByTradeNoWithError(tradeNo)
		if err != nil {
			return false, err
		}
		return reloaded.Status != common.TopUpStatusPending, nil
	}

	plan, err := model.ResolveTossSubscriptionOrderPlan(current)
	if err != nil {
		return false, err
	}
	chargeKRW := tossSubscriptionOrderChargeKRW(current, plan)
	if !isRenewalOrder && !invalidRenewalOrder && (current.BillingAttempted || potentialLegacyAttempt) {
		return reconcileTossSubscriptionFirstCharge(ctx, current, plan, chargeKRW)
	}

	// From this point onward the order has no durable provider-POST marker. Take
	// a DB lease and reload it before any lookup or terminal transition. This is
	// the cross-node counterpart of LockOrder: an old order may have just entered
	// a fresh browser callback on another process even though create_time is old.
	var reconcileClaimToken string
	var claimed bool
	var claimErr error
	if isRenewalOrder {
		reconcileClaimToken, claimed, claimErr = model.ClaimTossUnattemptedRenewalChargeOrder(tradeNo)
	} else {
		reconcileClaimToken, claimed, claimErr = model.ClaimTossUnattemptedSubscriptionChargeOrder(tradeNo)
	}
	if claimErr != nil || !claimed {
		return false, claimErr
	}
	defer func() {
		_ = model.ReleaseTossSubscriptionBillingClaim(tradeNo, reconcileClaimToken)
	}()
	current, err = model.GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if err != nil {
		return false, err
	}
	if current.Status != common.TopUpStatusPending || current.BillingClaimToken != reconcileClaimToken || current.BillingAttempted {
		return false, nil
	}
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring below-minimum order_id=%s amount=%d", tradeNo, chargeKRW))
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo, reconcileClaimToken)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, reconcileClaimToken)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, reconcileClaimToken)
	}

	activeBillingClient, activeBillingSecret := setting.TossActiveBillingKeyPair()
	auth, statusCode, err := getTossPaymentByOrderIdWithCredentialForMID(ctx, tradeNo, current.ProviderCredential, current.ProviderClientKeyHash, activeBillingClient, activeBillingSecret)
	if err != nil {
		if statusCode == http.StatusNotFound {
			// BillingAttempted is committed before the provider POST. A lost/malformed
			// successful response followed by an immediately-invisible GET must never
			// be converted into a terminal local failure: money may already have moved.
			// Keep the deterministic order pending for authoritative retry/manual
			// reconciliation instead of allowing a fresh orderId to be charged.
			if current.BillingAttempted {
				return false, fmt.Errorf("%w: attempted subscription payment not found yet order_id=%s", model.ErrTossBillingChargePending, tradeNo)
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile found no payment order_id=%s", tradeNo))
			if isRenewalOrder {
				return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo, reconcileClaimToken)
			}
			if invalidRenewalOrder {
				return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, reconcileClaimToken)
			}
			return expireTossPendingSubscriptionOrderForReconcile(tradeNo, reconcileClaimToken)
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
				_, _ = persistTossFulfillmentEvent(auth)
				return false, err
			}
			if err := model.ResolveTossPaymentEvents(tradeNo, model.TossPaymentEventTypeFulfillment); err != nil {
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
			handleTossBillingActivationFailure(ctx, current.UserId, tradeNo, current.BillingKeyId, chargeKRW, auth, string(payload), err, false)
			return false, err
		}
		if err := model.ResolveTossPaymentEvents(tradeNo, model.TossPaymentEventTypeFulfillment); err != nil {
			return false, err
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss subscription pending order reconciled as DONE order_id=%s", tradeNo))
		return true, nil
	}
	if auth == nil {
		return false, nil
	}
	if auth.OrderId != tradeNo {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile order mismatch order_id=%s auth_order=%s", tradeNo, auth.OrderId))
		return false, nil
	}
	if strings.EqualFold(strings.TrimSpace(auth.Status), "DONE") {
		if _, persistErr := persistTossFinancialMismatchEvent(auth, "subscription pending reconciliation DONE payload mismatch"); persistErr != nil {
			return false, persistErr
		}
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment DONE payload mismatch order_id=%s user_id=%d expected_amount=%d actual_amount=%d currency=%s card_present=%t", tradeNo, current.UserId, chargeKRW, auth.TotalAmount, auth.Currency, auth.Card != nil))
		return false, errors.New("Toss subscription DONE payload mismatch; reconciliation required")
	}
	if isTossTerminalFailStatus(auth.Status) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring terminal order_id=%s status=%s", tradeNo, auth.Status))
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo, reconcileClaimToken)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, reconcileClaimToken)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, reconcileClaimToken)
	}
	if isTossCancelStatus(auth.Status) {
		created, canceledAmount, persistErr := persistExpectedTossCancellationEvents(ctx, auth, tradeNo, chargeKRW)
		if persistErr != nil {
			return false, persistErr
		}
		if created > 0 {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment %s before activation order_id=%s user_id=%d canceled=%d KRW balance=%d KRW - local subscription NOT activated", auth.Status, tradeNo, current.UserId, canceledAmount, auth.BalanceAmount))
			model.RecordTopupLog(current.UserId, fmt.Sprintf("Toss subscription payment %s before activation (canceled: %d KRW, balance: %d KRW) - manual subscription/quota reconciliation required", auth.Status, canceledAmount, auth.BalanceAmount), "toss-pending-cleanup", model.PaymentMethodToss, "toss-subscription-cancel")
		}
		if isRenewalOrder {
			return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo, reconcileClaimToken)
		}
		if invalidRenewalOrder {
			return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, reconcileClaimToken)
		}
		return expireTossPendingSubscriptionOrderForReconcile(tradeNo, reconcileClaimToken)
	}
	logger.LogWarn(ctx, fmt.Sprintf("Toss subscription pending reconcile expiring stale non-terminal order_id=%s status=%s", tradeNo, auth.Status))
	if isRenewalOrder {
		return expireTossPendingRenewalOrderForReconcile(renewalSubId, tradeNo, reconcileClaimToken)
	}
	if invalidRenewalOrder {
		return expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, reconcileClaimToken)
	}
	return expireTossPendingSubscriptionOrderForReconcile(tradeNo, reconcileClaimToken)
}

func expireTossPendingRenewalOrderForReconcile(subId int, tradeNo, claimToken string) (bool, error) {
	if _, err := model.ExpireClaimedTossPendingRenewalOrderAndMarkFailure(subId, tradeNo, claimToken, 1); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
			errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
			errors.Is(err, model.ErrPaymentMethodMismatch) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func expireTossPendingSubscriptionOrderOnlyForReconcile(tradeNo, claimToken string) (bool, error) {
	if err := model.ExpireClaimedTossSubscriptionOrder(tradeNo, claimToken); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
			errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
			errors.Is(err, model.ErrPaymentMethodMismatch) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func expireTossPendingSubscriptionOrderForReconcile(tradeNo, claimToken string) (bool, error) {
	if err := model.ExpireClaimedTossPendingSubscriptionOrderAndMarkBillingKeyPendingRevocation(tradeNo, claimToken); err != nil {
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

var subscriptionTossBillingKeyIssuer = issueTossBillingKeyWithSecret

// SubscriptionRequestTossBilling starts the billing-auth flow for a subscription plan.
func SubscriptionRequestTossBilling(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}
	var req SubscriptionTossPayRequest
	if err := decodeTossPaymentRequestJSON(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgSubPaymentInvalidParams)
		return
	}
	if req.PlanId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgSubPaymentInvalidParams)
		return
	}
	if !isTossBillingEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	tossConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isFreshTossBillingCheckoutEnabled(tossConfig) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	plan, err := model.GetSubscriptionPlanByIdForPayment(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorI18n(c, i18n.MsgSubscriptionNotEnabled)
		return
	}
	if err := model.ValidateTossSubscriptionBillingPlan(plan); err != nil {
		common.ApiError(c, err)
		return
	}
	chargeKRW := tossSubscriptionChargeKRWWithSnapshot(plan, tossConfig)
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
	if err != nil || user == nil || user.Status != common.UserStatusEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if user.OrganizationId > 0 {
		common.ApiErrorI18n(c, i18n.MsgPaymentPersonalBillingOrgActive)
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
	if !isValidTossSDKCustomerKey(customerKey) {
		// Existing billing keys may legitimately retain a wider legacy
		// server-API customerKey, but payment({customerKey}) in SDK v2 rejects
		// anything outside its 2..50-character browser boundary. Do not create a
		// checkout reservation that the client can never open.
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss subscription billing auth blocked by invalid SDK customer key user_id=%d", userId))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	reference := fmt.Sprintf("new-api-toss-sub-%d-%d-%s", userId, time.Now().UnixMilli(), randstr.String(16))
	tradeNo := "toss_sub_" + common.Sha1([]byte(reference))
	billingClientKey, billingSecretKey := setting.TossActiveBillingKeyPairFromSnapshot(tossConfig)
	if strings.TrimSpace(billingClientKey) == "" || strings.TrimSpace(billingSecretKey) == "" {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	providerCredential, err := model.EncryptProviderCredential(billingSecretKey)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing encrypt provider credential failed user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	order := &model.SubscriptionOrder{
		UserId:                userId,
		PlanId:                plan.Id,
		Money:                 plan.PriceAmount,
		TradeNo:               tradeNo,
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		ProviderAmount:        chargeKRW,
		ProviderCurrency:      "KRW",
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(billingClientKey),
		Status:                common.TopUpStatusPending,
	}
	if err := model.SetTossSubscriptionOrderPlanSnapshot(order, plan); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing snapshot creation failed user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	orderPlan, err := model.ResolveTossSubscriptionOrderPlan(order)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing snapshot resolution failed user_id=%d trade_no=%s error=%q", userId, tradeNo, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	checkoutSnapshot := newTossSubscriptionCheckoutSnapshot(orderPlan, order.ProviderAmount, order.ProviderCurrency)
	if checkoutSnapshot == nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing public snapshot creation failed user_id=%d trade_no=%s", userId, tradeNo))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	if err := model.CreateTossSubscriptionOrderWithPurchaseReservation(order, plan); err != nil {
		if errors.Is(err, model.ErrSubscriptionPurchaseLimit) {
			common.ApiErrorI18n(c, i18n.MsgSubscriptionPurchaseMax)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":    billingClientKey,
			"customer_key":  customerKey,
			"trade_no":      tradeNo,
			"success_url":   base + "/api/subscription/toss/confirm/" + url.PathEscape(tradeNo),
			"fail_url":      base + "/api/subscription/toss/fail/" + url.PathEscape(tradeNo),
			"toss_checkout": checkoutSnapshot,
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
	if authKey == "" || len(authKey) > model.TossBillingIssueAuthKeyMaxBytes ||
		customerKey == "" || len(strings.TrimSpace(customerKey)) > model.TossBillingIssueCustomerKeyMaxBytes ||
		!isValidTossOrderId(tradeNo) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm missing params trade_no=%q", tradeNo))
		tossRedirect(c, "/console/topup")
		return
	}
	if err := model.ValidateTossBillingCryptoConfiguration(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing confirm blocked by unsafe credential encryption trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	order, lookupErr := model.GetSubscriptionOrderByTradeNoWithError(tradeNo)
	if lookupErr != nil || order.PaymentProvider != model.PaymentProviderToss {
		if lookupErr != nil && !errors.Is(lookupErr, model.ErrSubscriptionOrderNotFound) {
			logger.LogError(ctx, fmt.Sprintf("Toss billing confirm local order lookup failed trade_no=%s err=%v", tradeNo, lookupErr))
		}
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
	plan, err := model.ResolveTossSubscriptionOrderPlan(order)
	if err != nil {
		tossRedirect(c, "/console/topup")
		return
	}
	chargeKRW := tossSubscriptionOrderChargeKRW(order, plan)
	if !model.IsTossCardAmountPayableKRW(chargeKRW) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm blocked below minimum trade_no=%s amount=%d", tradeNo, chargeKRW))
		if _, cancelErr := subscriptionTossPreclaimOrderCanceller(tradeNo, order.UserId); cancelErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss billing below-minimum reservation cleanup failed trade_no=%s err=%v", tradeNo, cancelErr))
		}
		tossRedirect(c, "/console/topup")
		return
	}

	// Bind the callback customerKey to the order user's canonical key so a billing key
	// can't be associated under a mismatched/forged customerKey.
	canonical, err := model.GetOrCreateTossCustomerKey(order.UserId)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing canonical customerKey lookup failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}
	if canonical != customerKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing customerKey mismatch trade_no=%s", tradeNo))
		// Callback parameters are browser-controlled. A forged/malformed callback
		// must not destroy a valid recoverable authorization snapshot.
		tossRedirect(c, "/console/topup")
		return
	}
	billingUser, err := model.GetUserById(order.UserId, false)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing user lookup failed trade_no=%s user_id=%d err=%v", tradeNo, order.UserId, err))
		tossRedirect(c, "/console/topup")
		return
	}
	if billingUser == nil || billingUser.Status != common.UserStatusEnabled || billingUser.OrganizationId > 0 {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm blocked for inactive user trade_no=%s user_id=%d", tradeNo, order.UserId))
		if _, cancelErr := subscriptionTossPreclaimOrderCanceller(tradeNo, order.UserId); cancelErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss billing inactive-user reservation cleanup failed trade_no=%s user_id=%d err=%v", tradeNo, order.UserId, cancelErr))
		}
		tossRedirect(c, "/console/topup")
		return
	}
	claimToken, claimed, err := model.ClaimTossSubscriptionBillingIssue(tradeNo, authKey, customerKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing confirm claim failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}
	if !claimed {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing confirm skipped because another worker owns or completed the claim trade_no=%s", tradeNo))
		tossRedirect(c, "/console/topup")
		return
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseTossSubscriptionBillingClaim(tradeNo, claimToken); releaseErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss billing confirm claim release failed trade_no=%s err=%v", tradeNo, releaseErr))
			}
		}
	}()
	storedAuthKey, storedCustomerKey, storedSecretKey, snapshotErr := model.GetClaimedTossSubscriptionBillingIssue(tradeNo, claimToken)
	if snapshotErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing confirm snapshot load failed trade_no=%s err=%v", tradeNo, snapshotErr))
		if errors.Is(snapshotErr, model.ErrTossBillingIssueSnapshotInvalid) {
			claimedOrder, stateErr := model.GetClaimedTossSubscriptionBillingIssueState(tradeNo, claimToken)
			if stateErr != nil {
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("Toss billing confirm claimed state reload failed trade_no=%s err=%v", tradeNo, stateErr))
				tossRedirect(c, "/console/topup")
				return
			}
			if claimedOrder.BillingIssueAttempted {
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: attempted subscription billing issue snapshot is unreadable trade_no=%s user_id=%d; preserving authorization and claim", tradeNo, claimedOrder.UserId))
			} else {
				_ = expireClaimedTossSubscriptionBillingIssue(ctx, tradeNo, claimToken, "invalid_snapshot")
			}
		}
		tossRedirect(c, "/console/topup")
		return
	}
	claimedOrder, stateErr := model.GetClaimedTossSubscriptionBillingIssueState(tradeNo, claimToken)
	if stateErr != nil {
		retainClaim = true
		logger.LogError(ctx, fmt.Sprintf("Toss billing confirm claimed state reload failed trade_no=%s err=%v", tradeNo, stateErr))
		tossRedirect(c, "/console/topup")
		return
	}
	_, retainClaim, err = processClaimedTossSubscriptionBillingIssue(ctx, claimedOrder, plan, claimToken, storedAuthKey, storedCustomerKey, storedSecretKey)
	if err != nil && !retainClaim {
		logger.LogError(ctx, fmt.Sprintf("Toss billing confirm processing failed trade_no=%s err=%v", tradeNo, err))
	}
	tossRedirect(c, "/console/topup")
}

// SubscriptionTossBillingFail is the billingAuth failUrl.
func SubscriptionTossBillingFail(c *gin.Context) {
	tradeNo := subscriptionTossTradeNo(c)
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("Toss billing auth failed trade_no=%q code=%s", tradeNo, sanitizeTossAPIErrorCode(code)))
	if !isValidTossOrderId(tradeNo) {
		tossRedirect(c, "/console/topup")
		return
	}
	// The redirect parameters are browser-controlled, so never overwrite an
	// order that has any durable provider activity. The model CAS closes only a
	// pristine reservation; a concurrent success callback either owns the claim
	// already or loses the race before it can contact Toss.
	LockOrder(tradeNo)
	_, err := model.CancelUnattemptedTossSubscriptionOrder(tradeNo, 0)
	UnlockOrder(tradeNo)
	if err != nil && !errors.Is(err, model.ErrSubscriptionOrderNotFound) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss billing auth failure cleanup failed trade_no=%s err=%v", tradeNo, err))
	}
	tossRedirect(c, tossFailureRedirectPath(tradeNo, code, message))
}

// CancelPendingTossSubscriptionOrder releases a reservation when the SDK is
// closed or fails before redirecting to Toss's success/fail URL.
func CancelPendingTossSubscriptionOrder(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	userID := c.GetInt("id")
	if userID <= 0 || !isValidTossOrderId(tradeNo) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	LockOrder(tradeNo)
	cancelled, err := model.CancelUnattemptedTossSubscriptionOrder(tradeNo, userID)
	UnlockOrder(tradeNo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"cancelled": cancelled})
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
