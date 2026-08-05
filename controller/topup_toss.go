package controller

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

var tossAPIBase = "https://api.tosspayments.com"

const tossAPIMaxAttempts = 3
const tossRecordedTopUpTerminalAge = 15 * time.Minute
const tossNeverRecordedTopUpTerminalAge = 50 * time.Minute
const tossRetryAfterMaxDelay = 5 * time.Second
const tossPaymentProcessingMinimumAttemptBudget = 60 * time.Second
const tossTopUpRefundManualRetryDelay = 24 * time.Hour

// Toss recommends a 60-second read timeout for payment-processing APIs. Keep a
// small scheduling margin above that bound while idempotency and GET recovery
// protect an outcome whose response is still lost.
const tossPaymentConfirmTimeout = 65 * time.Second

const tossWebhookMaxBodyBytes int64 = 1 << 20
const tossAPIResponseMaxBodyBytes int64 = 2 << 20
const tossWebhookProcessingTimeout = 8 * time.Second

// Commit the response before Toss's documented ten-second deadline while
// leaving one second after the processing budget for the final socket write.
const tossWebhookResponseTimeout = 9 * time.Second

type tossAPIError struct {
	Method     string
	StatusCode int
	Code       string
}

// tossAPITransportError preserves the original error for errors.Is/As while
// keeping its string form free of request URLs. net/http's *url.Error embeds
// the full URL, and Toss resource paths can contain paymentKey or billingKey.
type tossAPITransportError struct {
	Method string
	Err    error
}

var (
	errTossAPIResponseTooLarge               = errors.New("toss api response body too large")
	errTossCancellationEvidenceInvalid       = errors.New("Toss cancellation evidence is internally inconsistent")
	errTossTopUpRefundAccountRequired        = errors.New("Toss virtual-account refund requires buyer account information")
	errTossTopUpVirtualAccountRefundPending  = errors.New("Toss virtual-account bank refund is pending")
	errTossTopUpVirtualAccountRefundUnproved = errors.New("Toss virtual-account bank refund completion is unproved")
)

func (e *tossAPITransportError) Error() string {
	if e == nil {
		return "toss api transport failed"
	}
	kind := "network"
	if errors.Is(e.Err, context.DeadlineExceeded) {
		kind = "timeout"
	} else if errors.Is(e.Err, context.Canceled) {
		kind = "canceled"
	} else if timeout, ok := e.Err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		kind = "timeout"
	}
	return fmt.Sprintf("toss api transport failed: method=%s kind=%s", e.Method, kind)
}

func (e *tossAPITransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *tossAPIError) Error() string {
	if e == nil {
		return "toss api failed"
	}
	return fmt.Sprintf("toss api failed: method=%s status=%d code=%s", e.Method, e.StatusCode, e.Code)
}

func newTossAPIError(method string, statusCode int, body []byte) error {
	var providerError struct {
		Code string `json:"code"`
		// Toss API v2 resource endpoints wrap the same provider code under
		// `error`. Core payment endpoints currently use the v1 top-level shape,
		// but accepting both keeps credential/ambiguous-outcome classification
		// correct when a merchant uses either documented response envelope.
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = common.Unmarshal(body, &providerError)
	code := providerError.Code
	if strings.TrimSpace(code) == "" {
		code = providerError.Error.Code
	}
	return &tossAPIError{
		Method:     method,
		StatusCode: statusCode,
		Code:       sanitizeTossAPIErrorCode(code),
	}
}

func sanitizeTossAPIErrorCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "UNKNOWN"
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return "UNKNOWN"
	}
	return value
}

func tossAPIErrorCode(err error) string {
	var providerError *tossAPIError
	if errors.As(err, &providerError) {
		return strings.ToUpper(strings.TrimSpace(providerError.Code))
	}
	return ""
}

const tossPaymentKeyMaxRunes = 200
const tossTransactionKeyMaxRunes = 64
const tossOrderIdMinBytes = 6
const tossOrderIdMaxBytes = 64
const tossSDKCustomerKeyMaxBytes = 50

// tossPaymentOperationContext keeps money-moving provider POSTs independent
// from a browser/proxy disconnect or short request-middleware deadline while
// retaining request values for tracing. Every operation has its own hard
// server-side maximum so detachment can never create an unbounded task.
func tossPaymentOperationContext(parent context.Context, maximum time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	base := context.WithoutCancel(parent)
	return context.WithTimeout(base, maximum)
}

func isTossTransientAPIStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusConflict ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

// isTossAmbiguousAPIOutcome reports responses after which provider-side money
// movement is still possible. In particular, a 2xx response whose body could
// not be read or decoded means Toss accepted the request even though the local
// process could not prove the resulting payment state.
func isTossAmbiguousAPIOutcome(status int) bool {
	return status == 0 || isTossTransientAPIStatus(status) ||
		(status >= http.StatusOK && status < http.StatusMultipleChoices)
}

func isTossAmbiguousPaymentOutcome(status int, err error) bool {
	if isTossAmbiguousAPIOutcome(status) {
		return true
	}
	// A non-2xx status is definitive only when its provider error code was read
	// and parsed. A truncated/oversized/malformed error body could have contained
	// ALREADY_PROCESSED_PAYMENT or another money-moving ambiguity. Resetting the
	// durable payment binding in that state would change the next idempotency
	// namespace and permit a second payment while the first becomes visible.
	var transportErr *tossAPITransportError
	if errors.As(err, &transportErr) || errors.Is(err, errTossAPIResponseTooLarge) {
		return true
	}
	code := tossAPIErrorCode(err)
	if code == "UNKNOWN" {
		var providerErr *tossAPIError
		if errors.As(err, &providerErr) {
			return true
		}
	}
	switch code {
	case "ALREADY_PROCESSED_PAYMENT", "ALREADY_COMPLETED_PAYMENT", "DUPLICATED_ORDER_ID", "PROVIDER_ERROR":
		return true
	}
	if isKnownDefinitiveTossPaymentRejection(code) {
		return false
	}
	// Toss can add a 4xx code before this binary is upgraded. Treating every
	// syntactically valid but unknown code as terminal could erase the durable
	// payment/idempotency binding even when the new code describes an accepted
	// or still-processing request. Only the documented pre-processing failures
	// below are allowed to release that binding; everything else fails closed.
	var providerErr *tossAPIError
	return errors.As(err, &providerErr)
}

func isKnownDefinitiveTossPaymentRejection(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "INVALID_API_KEY",
		"UNAUTHORIZED_KEY",
		"INCORRECT_BASIC_AUTH_FORMAT",
		"FORBIDDEN_REQUEST",
		"INVALID_IDEMPOTENCY_KEY",
		"INVALID_REQUEST",
		"INVALID_REQUIRED_PARAM",
		"INVALID_PAYMENT_KEY",
		"INVALID_ORDER_ID",
		"INVALID_ORDER_NAME",
		"INVALID_CURRENCY",
		"INVALID_CUSTOMER_KEY",
		"INVALID_CLIENT_KEY",
		"INVALID_AUTHORIZE_AUTH",
		"INVALID_AUTHORIZE_AUTH_TYPE_GIFT_CERTIFICATE",
		"INVALID_BILL_KEY_REQUEST",
		"INVALID_BILLING_AUTH",
		"NOT_MATCHES_CUSTOMER_KEY",
		"NOT_FOUND_PAYMENT",
		"NOT_FOUND_PAYMENT_SESSION",
		"NOT_FOUND_BILLING",
		"NOT_FOUND_BILLING_KEY",
		"NOT_FOUND_CUSTOMER",
		"NOT_FOUND_METHOD",
		"NOT_FOUND_METHOD_OWNERSHIP",
		"BELOW_MINIMUM_AMOUNT",
		"BELOW_ZERO_AMOUNT",
		"EXCEED_MAX_CARD_INSTALLMENT_PLAN",
		"EXCEED_MAX_DAILY_PAYMENT_COUNT",
		"EXCEED_MAX_PAYMENT_AMOUNT",
		"EXCEED_MAX_AMOUNT",
		"EXCEED_MAX_MONTHLY_PAYMENT_AMOUNT",
		"EXCEED_MAX_AUTH_COUNT",
		"EXCEED_MAX_ONE_DAY_AMOUNT",
		"EXCEED_MAX_ONE_DAY_WITHDRAW_AMOUNT",
		"EXCEED_MAX_ONE_TIME_WITHDRAW_AMOUNT",
		"NOT_ALLOWED_POINT_USE",
		"NOT_AVAILABLE_BANK",
		"NOT_AVAILABLE_PAYMENT",
		"NOT_FOUND_TERMINAL_ID",
		"NOT_REGISTERED_BUSINESS",
		"NOT_SUPPORTED_METHOD",
		"NOT_SUPPORTED_CARD_TYPE",
		"NOT_SUPPORTED_INSTALLMENT_PLAN_CARD_OR_MERCHANT",
		"NOT_SUPPORTED_MONTHLY_INSTALLMENT_PLAN",
		"NOT_SUPPORTED_MONTHLY_INSTALLMENT_PLAN_BELOW_AMOUNT",
		"NOT_SUPPORTED_BILLING_MERCHANT",
		"REQUIRED_BILLING_TERMS",
		"REQUIRED_AMOUNT",
		"RESTRICTED_TRANSFER_ACCOUNT",
		"MAINTAINED_METHOD",
		"PAY_PROCESS_ABORTED",
		"PAY_PROCESS_CANCELED",
		"CARD_PROCESSING_ERROR",
		"UNAPPROVED_ORDER_ID",
		"INVALID_PASSWORD",
		"INVALID_REJECT_CARD",
		"INVALID_CARD",
		"INVALID_CARD_COMPANY",
		"INVALID_CARD_NUMBER",
		"INVALID_CARD_PASSWORD",
		"INVALID_CARD_EXPIRATION",
		"INVALID_CARD_IDENTITY",
		"INVALID_CARD_INSTALLMENT_PLAN",
		"INVALID_CARD_INSTALLMENT_AMOUNT",
		"INVALID_CARD_INFO_RE_REGISTER",
		"INVALID_ACCOUNT_INFO_RE_REGISTER",
		"INVALID_CARD_LOST_OR_STOLEN",
		"INVALID_STOPPED_CARD",
		"INVALID_STOPPED_ACCOUNT",
		"INVALID_LEGAL_REGISTERED_ACCOUNT",
		"INVALID_BIRTH_DAY_FORMAT",
		"INVALID_BANK",
		"INVALID_EASY_PAY",
		"INVALID_EMAIL",
		"INVALID_FLOW_MODE_PARAMETERS",
		"INVALID_IDENTIFICATION_TYPE",
		"INVALID_ISO_DATE_FORMAT",
		"INVALID_UNREGISTERED_SUBMALL",
		"NOT_REGISTERED_CARD_COMPANY",
		"REJECT_ACCOUNT_PAYMENT",
		"REJECT_CARD_PAYMENT",
		"REJECT_CARD_COMPANY",
		"REJECT_TOSSPAY_INVALID_ACCOUNT",
		"SUSPECTED_PHISHING_PAYMENT",
		"FDS_ERROR",
		"FAILED_PAYMENT_CONFIRM":
		return true
	default:
		return false
	}
}

func isValidTossOrderId(orderId string) bool {
	if len(orderId) < tossOrderIdMinBytes || len(orderId) > tossOrderIdMaxBytes {
		return false
	}
	for i := 0; i < len(orderId); i++ {
		ch := orderId[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func isValidTossPaymentKey(paymentKey string) bool {
	if paymentKey != strings.TrimSpace(paymentKey) {
		return false
	}
	length := 0
	for _, r := range paymentKey {
		length++
		if length > tossPaymentKeyMaxRunes || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return length > 0
}

func isValidTossTransactionKey(transactionKey string) bool {
	if transactionKey != strings.TrimSpace(transactionKey) {
		return false
	}
	length := 0
	for _, r := range transactionKey {
		length++
		if length > tossTransactionKeyMaxRunes || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return length > 0
}

// isValidTossSDKCustomerKey follows payment({ customerKey }) in the JavaScript
// SDK v2, which accepts at most 50 characters. The server-side Billing API
// separately documents a 300-character maximum, but that wider contract does
// not make a key valid for payment({ customerKey }) in the browser. Keep this
// stricter boundary on every session handed to the SDK.
func isValidTossSDKCustomerKey(customerKey string) bool {
	if len(customerKey) < 2 || len(customerKey) > tossSDKCustomerKeyMaxBytes ||
		customerKey != strings.TrimSpace(customerKey) {
		return false
	}
	hasSpecial := false
	for i := 0; i < len(customerKey); i++ {
		ch := customerKey[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '-', ch == '_', ch == '=', ch == '.', ch == '@':
			hasSpecial = true
		default:
			return false
		}
	}
	return hasSpecial
}

func isValidTossBillingKey(billingKey string) bool {
	return model.IsValidTossBillingKey(billingKey)
}

func isSafeTossAPIEnum(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func tossRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		return 0
	}
	return time.Duration(100*(attempt+1)) * time.Millisecond
}

func tossRetryAfterDelay(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(tossRetryAfterMaxDelay/time.Second) {
			return tossRetryAfterMaxDelay
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	delay := when.Sub(now)
	if delay > tossRetryAfterMaxDelay {
		return tossRetryAfterMaxDelay
	}
	return delay
}

func tossRetryWaitDelay(attempt int, retryAfter string, now time.Time) time.Duration {
	delay := tossRetryDelay(attempt)
	if providerDelay := tossRetryAfterDelay(retryAfter, now); providerDelay > delay {
		delay = providerDelay
	}
	if delay > tossRetryAfterMaxDelay {
		return tossRetryAfterMaxDelay
	}
	return delay
}

func waitTossRetry(ctx context.Context, attempt int, retryAfter string) error {
	delay := tossRetryWaitDelay(attempt, retryAfter, time.Now())
	if delay <= 0 {
		return nil
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// Do not begin another provider attempt when the advertised backoff alone
		// consumes the entire caller budget. Returning early also keeps webhook
		// processing inside its strict whole-handler deadline.
		if remaining <= 0 || delay >= remaining {
			return context.DeadlineExceeded
		}
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

func secureTossHTTPTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	transport, ok := base.(*http.Transport)
	if !ok {
		// Preserve explicit custom RoundTrippers used by integrations and tests;
		// there is no generic way to inspect or clone their TLS policy.
		return base
	}
	clone := transport.Clone()
	if transport.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	// TLS_INSECURE_SKIP_VERIFY is a legacy global relay option. Toss requests
	// carry Basic API credentials and payment identifiers, so they must never
	// inherit that process-wide opt-out. Only the per-request client clone is
	// hardened; the caller's/global transport remains unchanged.
	clone.TLSClientConfig.InsecureSkipVerify = false
	if clone.TLSClientConfig.MinVersion < tls.VersionTLS12 {
		clone.TLSClientConfig.MinVersion = tls.VersionTLS12
	}
	// Toss payment processing can legitimately take up to 60 seconds. A short
	// process-wide ResponseHeaderTimeout intended for ordinary upstream calls
	// must not preempt the explicit per-operation contexts below (8/15/65/70s).
	// Zero leaves the request context as the single authoritative deadline.
	clone.ResponseHeaderTimeout = 0
	return clone
}

func doTossHTTPRequest(req *http.Request) (*http.Response, error) {
	baseClient := http.DefaultClient
	if baseClient == nil {
		baseClient = &http.Client{}
	}
	client := *baseClient
	client.Transport = secureTossHTTPTransport(client.Transport)
	// As with ResponseHeaderTimeout, a shared http.DefaultClient.Timeout may be
	// tuned for unrelated providers. Every Toss call has its own bounded context,
	// so inheriting a shorter global timeout would violate Toss's documented
	// payment-processing read-timeout requirement and create avoidable ambiguous
	// outcomes after the provider has already accepted a charge.
	client.Timeout = 0
	// Toss API endpoints are credential-bearing and are never expected to
	// redirect. Returning the 3xx response prevents POST-to-GET rewriting and
	// guarantees Basic Authorization is not forwarded to another location.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client.Do(req)
}

func doTossAPIRequestWithSecret(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string, secretKey string, acceptedStatuses ...int) (int, []byte, error) {
	return doTossAPIRequestWithSecretAndMinimumAttemptBudget(
		ctx, method, endpoint, body, idempotencyKey, secretKey, 0, acceptedStatuses...,
	)
}

// doTossPaymentProcessingAPIRequestWithSecret keeps every provider attempt at
// Toss's documented 60-second Read Timeout. Callers intentionally use a
// slightly larger operation context (65/70 seconds), but that context is shared
// by the bounded retry loop. Without an attempt-level guard, a transient first
// response could cause the next money-moving request to start with less than
// 60 seconds remaining and manufacture an avoidable ambiguous outcome.
func doTossPaymentProcessingAPIRequestWithSecret(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string, secretKey string, acceptedStatuses ...int) (int, []byte, error) {
	return doTossAPIRequestWithSecretAndMinimumAttemptBudget(
		ctx, method, endpoint, body, idempotencyKey, secretKey,
		tossPaymentProcessingMinimumAttemptBudget, acceptedStatuses...,
	)
}

func tossAPIContextHasMinimumAttemptBudget(ctx context.Context, minimum time.Duration) bool {
	if minimum <= 0 || ctx == nil {
		return true
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) >= minimum
}

func doTossAPIRequestWithSecretAndMinimumAttemptBudget(ctx context.Context, method, endpoint string, body []byte, idempotencyKey string, secretKey string, minimumAttemptBudget time.Duration, acceptedStatuses ...int) (int, []byte, error) {
	accepted := make(map[int]struct{}, len(acceptedStatuses))
	for _, status := range acceptedStatuses {
		accepted[status] = struct{}{}
	}
	var lastStatus int
	var lastBody []byte
	var lastErr error
	var retryAfter string
	for attempt := 0; attempt < tossAPIMaxAttempts; attempt++ {
		if !tossAPIContextHasMinimumAttemptBudget(ctx, minimumAttemptBudget) {
			// Preserve the previous authenticated/transient provider outcome when a
			// retry no longer has the documented read budget. First-attempt budget
			// failures have no provider outcome and surface as a deadline error.
			if lastErr != nil {
				return lastStatus, lastBody, lastErr
			}
			return 0, nil, fmt.Errorf(
				"%w: Toss API attempt requires at least %s",
				context.DeadlineExceeded,
				minimumAttemptBudget,
			)
		}
		retryAfter = ""
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

		resp, err := doTossHTTPRequest(req)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			lastStatus = 0
			lastBody = nil
			lastErr = &tossAPITransportError{Method: method, Err: err}
		} else {
			retryAfter = resp.Header.Get("Retry-After")
			respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, tossAPIResponseMaxBodyBytes+1))
			_ = resp.Body.Close()
			lastStatus = resp.StatusCode
			lastBody = respBody
			if readErr != nil {
				lastErr = &tossAPITransportError{Method: method, Err: readErr}
				if !isTossAmbiguousAPIOutcome(resp.StatusCode) {
					return lastStatus, lastBody, lastErr
				}
			} else if int64(len(respBody)) > tossAPIResponseMaxBodyBytes {
				return lastStatus, nil, errTossAPIResponseTooLarge
			} else if _, ok := accepted[resp.StatusCode]; ok {
				return lastStatus, lastBody, nil
			} else {
				lastErr = newTossAPIError(method, resp.StatusCode, respBody)
				if !isTossTransientAPIStatus(resp.StatusCode) {
					return lastStatus, lastBody, lastErr
				}
			}
		}

		if attempt == tossAPIMaxAttempts-1 {
			break
		}
		if err := waitTossRetry(ctx, attempt, retryAfter); err != nil {
			return lastStatus, lastBody, err
		}
	}
	if lastErr == nil {
		lastErr = newTossAPIError(method, lastStatus, lastBody)
	}
	return lastStatus, lastBody, lastErr
}

type TossPayRequest struct {
	Amount        int64  `json:"amount"`
	AmountMode    string `json:"amount_mode"`
	PaymentMethod string `json:"payment_method"`
}

func isValidTossTopUpAmountModeInput(amountMode string) bool {
	switch amountMode {
	case "", model.TossTopUpAmountModeKRW, model.TossTopUpAmountModeQuota:
		return true
	default:
		return false
	}
}

type tossPaymentCard struct {
	Company string `json:"company"`
	Number  string `json:"number"`
	Amount  int64  `json:"amount"`
}

const (
	tossCardCompanyMaxRunes = 32
	tossCardSuffixMaxRunes  = 4
	tossEasyPayMaxRunes     = 64
)

// sanitizeTossCardCompany bounds provider-controlled display text before it
// can reach logs, JSON audit payloads, or varchar(32) storage.
func sanitizeTossCardCompany(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	result := make([]rune, 0, tossCardCompanyMaxRunes)
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		result = append(result, r)
		if len(result) == tossCardCompanyMaxRunes {
			break
		}
	}
	return strings.TrimSpace(string(result))
}

// sanitizeTossCardNumber never retains more than the masked final four card
// symbols. A malformed provider response containing a full PAN is reduced to
// ****1234 before any response object can be persisted as ProviderPayload.
func sanitizeTossCardNumber(value string) string {
	runes := []rune(strings.TrimSpace(value))
	suffixReversed := make([]rune, 0, tossCardSuffixMaxRunes)
	for i := len(runes) - 1; i >= 0 && len(suffixReversed) < tossCardSuffixMaxRunes; i-- {
		r := runes[i]
		switch {
		case r >= '0' && r <= '9':
			suffixReversed = append(suffixReversed, r)
		case r == '*' || r == 'x' || r == 'X' || r == '•' || r == '●':
			suffixReversed = append(suffixReversed, '*')
		}
	}
	if len(suffixReversed) == 0 {
		return ""
	}
	suffix := make([]rune, len(suffixReversed))
	for i := range suffixReversed {
		suffix[len(suffixReversed)-1-i] = suffixReversed[i]
	}
	return "****" + string(suffix)
}

type tossPaymentEasyPay struct {
	Provider       string `json:"provider"`
	Amount         int64  `json:"amount"`
	DiscountAmount int64  `json:"discountAmount"`
}

type tossPaymentVirtualAccount struct {
	RefundStatus string `json:"refundStatus"`
}

func sanitizeTossEasyPayProvider(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	result := make([]rune, 0, tossEasyPayMaxRunes)
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		result = append(result, r)
		if len(result) == tossEasyPayMaxRunes {
			break
		}
	}
	return strings.TrimSpace(string(result))
}

func sanitizeTossPaymentResponse(result *tossConfirmResponse) {
	if result == nil {
		return
	}
	if result.Card != nil {
		result.Card.Company = sanitizeTossCardCompany(result.Card.Company)
		result.Card.Number = sanitizeTossCardNumber(result.Card.Number)
	}
	if result.EasyPay != nil {
		result.EasyPay.Provider = sanitizeTossEasyPayProvider(result.EasyPay.Provider)
	}
}

type tossPaymentCancel struct {
	CancelAmount           int64   `json:"cancelAmount"`
	CancelReason           string  `json:"cancelReason"`
	TaxFreeAmount          int64   `json:"taxFreeAmount"`
	TaxExemptionAmount     int64   `json:"taxExemptionAmount"`
	RefundableAmount       int64   `json:"refundableAmount"`
	CardDiscountAmount     int64   `json:"cardDiscountAmount"`
	TransferDiscountAmount int64   `json:"transferDiscountAmount"`
	EasyPayDiscountAmount  int64   `json:"easyPayDiscountAmount"`
	CanceledAt             string  `json:"canceledAt"`
	TransactionKey         string  `json:"transactionKey"`
	ReceiptKey             *string `json:"receiptKey"`
	CancelStatus           string  `json:"cancelStatus"`
	CancelRequestId        *string `json:"cancelRequestId"`
}

// tossConfirmResponse is the subset of the Toss Payment object we rely on.
type tossConfirmResponse struct {
	PaymentKey         string                     `json:"paymentKey"`
	Type               string                     `json:"type"`
	OrderId            string                     `json:"orderId"`
	Status             string                     `json:"status"`
	TotalAmount        int64                      `json:"totalAmount"`
	BalanceAmount      int64                      `json:"balanceAmount"`
	SuppliedAmount     int64                      `json:"suppliedAmount"`
	Vat                int64                      `json:"vat"`
	TaxFreeAmount      int64                      `json:"taxFreeAmount"`
	TaxExemptionAmount int64                      `json:"taxExemptionAmount"`
	Currency           string                     `json:"currency"`
	Method             string                     `json:"method"`
	UseEscrow          bool                       `json:"useEscrow"`
	CultureExpense     bool                       `json:"cultureExpense"`
	ApprovedAt         string                     `json:"approvedAt"`
	Card               *tossPaymentCard           `json:"card"`
	EasyPay            *tossPaymentEasyPay        `json:"easyPay"`
	VirtualAccount     *tossPaymentVirtualAccount `json:"virtualAccount"`
	Cancels            []tossPaymentCancel        `json:"cancels"`
}

func validateTossPaymentResponseShape(result *tossConfirmResponse) error {
	if result == nil {
		return errors.New("Toss payment response is empty")
	}
	if !isValidTossPaymentKey(result.PaymentKey) {
		return errors.New("Toss payment response has an invalid paymentKey")
	}
	switch result.Type {
	case "NORMAL", "BILLING", "BRANDPAY":
	default:
		return errors.New("Toss payment response has an invalid type")
	}
	if !isValidTossOrderId(result.OrderId) {
		return errors.New("Toss payment response has an invalid orderId")
	}
	if !isSafeTossAPIEnum(result.Status) {
		return errors.New("Toss payment response has an invalid status")
	}
	if result.Currency != "" && !isSafeTossAPIEnum(result.Currency) {
		return errors.New("Toss payment response has an invalid currency")
	}
	if result.TotalAmount < 0 || result.BalanceAmount < 0 || result.BalanceAmount > result.TotalAmount ||
		result.SuppliedAmount < 0 || result.Vat < 0 || result.TaxFreeAmount < 0 || result.TaxExemptionAmount < 0 ||
		result.SuppliedAmount > result.TotalAmount || result.Vat > result.TotalAmount ||
		result.TaxFreeAmount > result.TotalAmount || result.TaxExemptionAmount > result.TotalAmount {
		return errors.New("Toss payment response has an invalid monetary breakdown")
	}
	if result.EasyPay != nil {
		if strings.TrimSpace(result.EasyPay.Provider) == "" {
			return errors.New("Toss payment response has an invalid easyPay provider")
		}
		if result.EasyPay.Amount < 0 || result.EasyPay.DiscountAmount < 0 {
			return errors.New("Toss payment response has an invalid easyPay amount")
		}
	}
	return nil
}

func validateTossBillingChargeResponseShape(result *tossConfirmResponse) error {
	if err := validateTossPaymentResponseShape(result); err != nil {
		return err
	}
	if result.Type != "BILLING" {
		return errors.New("Toss billing charge response has an unexpected payment type")
	}
	if result.TotalAmount <= 0 {
		return errors.New("Toss billing charge response is missing totalAmount")
	}
	if strings.TrimSpace(result.Currency) == "" {
		return errors.New("Toss billing charge response is missing currency")
	}
	if strings.TrimSpace(result.Method) != "카드" {
		return errors.New("Toss billing charge response has an unexpected payment method")
	}
	if result.Card == nil || result.Card.Amount != result.TotalAmount {
		return errors.New("Toss billing charge response has invalid card amount")
	}
	if strings.EqualFold(strings.TrimSpace(result.Status), "DONE") && result.BalanceAmount != result.TotalAmount {
		return errors.New("Toss billing charge response has invalid balanceAmount")
	}
	return nil
}

func tossCancellationEventKey(paymentKey string, cancel tossPaymentCancel, status string, balanceAmount int64) string {
	transactionKey := strings.TrimSpace(cancel.TransactionKey)
	seedParts := []string{strings.TrimSpace(paymentKey)}
	if transactionKey != "" {
		// transactionKey is the provider's stable identifier for an individual
		// cancellation and must remain stable as later partial cancels change the
		// Payment balance/status.
		seedParts = append(seedParts, transactionKey)
	} else if canceledAt := strings.TrimSpace(cancel.CanceledAt); canceledAt != "" {
		seedParts = append(seedParts, canceledAt, strconv.FormatInt(cancel.CancelAmount, 10))
	} else {
		seedParts = append(seedParts,
			strconv.FormatInt(cancel.CancelAmount, 10),
			strings.TrimSpace(status),
			strconv.FormatInt(balanceAmount, 10),
		)
	}
	seed := strings.Join(seedParts, "\x00")
	return "toss_cancel_" + common.Sha1([]byte(seed))
}

// persistTossCancellationEvents stores every provider cancellation transaction
// before the webhook is acknowledged. It returns only the amount introduced by
// newly inserted events, so webhook redelivery does not duplicate audit logs.
func persistTossCancellationEvents(auth *tossConfirmResponse) (createdCount int, newlyCanceled int64, err error) {
	return persistTossCancellationEventsWithContext(context.Background(), auth)
}

// hasSupportedTossTopUpFundingSource mirrors the SDK v2 CARD integration.
// CARD opens the card/easy-pay unified window: a registered card returns card,
// while a linked account, money balance, or points-only payment can validly
// return only easyPay. Billing charges remain card-only and keep their stricter
// validators below.
func hasSupportedTossTopUpFundingSource(auth *tossConfirmResponse) bool {
	if auth == nil || auth.TotalAmount <= 0 {
		return false
	}
	// Credits are sold as ordinary taxable, non-escrow digital goods. These
	// values originate in the public browser SDK and can be altered independently
	// of orderId/amount, so the authenticated Payment object must match the
	// server contract before quota is granted.
	if auth.TaxFreeAmount != 0 || auth.TaxExemptionAmount != 0 || auth.UseEscrow || auth.CultureExpense {
		return false
	}

	cardAmount := int64(0)
	if auth.Card != nil {
		cardAmount = auth.Card.Amount
		if cardAmount < 0 || cardAmount > auth.TotalAmount {
			return false
		}
	}

	easyPayAmount := int64(0)
	easyPayDiscountAmount := int64(0)
	if auth.EasyPay != nil {
		if strings.TrimSpace(auth.EasyPay.Provider) == "" {
			return false
		}
		easyPayAmount = auth.EasyPay.Amount
		easyPayDiscountAmount = auth.EasyPay.DiscountAmount
		if easyPayAmount < 0 || easyPayDiscountAmount < 0 ||
			easyPayAmount > auth.TotalAmount || easyPayDiscountAmount > auth.TotalAmount {
			return false
		}
	}

	// A successful SDK v2 CARD checkout must be either a direct card payment
	// (Payment.method == "카드") or a card/easy-pay payment
	// (Payment.method == "간편결제"). Requiring the corresponding response
	// object prevents a differently contracted payment method from being
	// credited even if a malformed payload happens to include a card field.
	switch strings.TrimSpace(auth.Method) {
	case "카드":
		if auth.Card == nil || auth.EasyPay != nil {
			return false
		}
	case "간편결제":
		if auth.EasyPay == nil {
			return false
		}
	default:
		return false
	}

	// Toss documents totalAmount as the exact sum of the selected funding
	// sources: card.amount, easyPay.amount (linked account/money), and
	// easyPay.discountAmount (points/coupons). Compare by subtraction so a
	// hostile or malformed provider payload cannot overflow an int64 sum.
	remaining := auth.TotalAmount
	for _, component := range []int64{cardAmount, easyPayAmount, easyPayDiscountAmount} {
		if component > remaining {
			return false
		}
		remaining -= component
	}
	if remaining != 0 {
		return false
	}
	return auth.Card != nil || auth.EasyPay != nil
}

func isValidTossCancellationIdentity(auth *tossConfirmResponse, orderId string, amount int64) bool {
	if auth == nil || !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
		return false
	}
	return strings.TrimSpace(auth.PaymentKey) != "" &&
		auth.OrderId == orderId &&
		auth.TotalAmount == amount &&
		strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW")
}

func hasExpectedTossTaxableNonEscrowContract(auth *tossConfirmResponse) bool {
	return auth != nil &&
		auth.TaxFreeAmount == 0 &&
		auth.TaxExemptionAmount == 0 &&
		!auth.UseEscrow &&
		!auth.CultureExpense
}

// A cancellation never grants entitlement. Preserve authenticated, monotonic
// cancellation evidence even when the original Billing DONE response violates
// today's tax/escrow fulfillment contract: dropping that evidence could allow
// a retry after money already moved at the provider. Identity, payment class,
// card amount, currency, and total amount remain mandatory.
func isValidTossCardCancellation(auth *tossConfirmResponse, orderId string, amount int64) bool {
	return isValidTossCancellationIdentity(auth, orderId, amount) &&
		auth.Type == "BILLING" &&
		strings.TrimSpace(auth.Method) == "카드" &&
		auth.Card != nil && auth.Card.Amount == auth.TotalAmount
}

func isValidTossTopUpCancellation(auth *tossConfirmResponse, orderId string, amount int64) bool {
	return isValidTossCancellationIdentity(auth, orderId, amount) && auth.Type == "NORMAL" && hasSupportedTossTopUpFundingSource(auth)
}

func persistExpectedTossCancellationEvents(ctx context.Context, auth *tossConfirmResponse, orderId string, amount int64) (int, int64, error) {
	if !isValidTossCardCancellation(auth, orderId, amount) {
		return 0, 0, errors.New("Toss cancellation does not match the expected card payment")
	}
	return persistTossCancellationEventsWithContext(ctx, auth)
}

func persistExpectedTossTopUpCancellationEvents(ctx context.Context, auth *tossConfirmResponse, orderId string, amount int64) (int, int64, error) {
	if !isValidTossTopUpCancellation(auth, orderId, amount) {
		return 0, 0, errors.New("Toss cancellation does not match the expected top-up payment")
	}
	return persistTossCancellationEventsWithContext(ctx, auth)
}

func persistTossCancellationEventsWithContext(ctx context.Context, auth *tossConfirmResponse) (createdCount int, newlyCanceled int64, err error) {
	if auth == nil || strings.TrimSpace(auth.PaymentKey) == "" || strings.TrimSpace(auth.OrderId) == "" {
		return 0, 0, fmt.Errorf("%w: invalid payment identity", errTossCancellationEvidenceInvalid)
	}
	status := strings.ToUpper(strings.TrimSpace(auth.Status))
	if auth.TotalAmount <= 0 || auth.BalanceAmount < 0 || auth.BalanceAmount > auth.TotalAmount {
		return 0, 0, fmt.Errorf("%w: invalid amount state", errTossCancellationEvidenceInvalid)
	}
	switch status {
	case "CANCELED":
		if auth.BalanceAmount != 0 {
			return 0, 0, fmt.Errorf("%w: fully canceled payment has a non-zero balance", errTossCancellationEvidenceInvalid)
		}
	case "PARTIAL_CANCELED":
		if auth.BalanceAmount <= 0 || auth.BalanceAmount >= auth.TotalAmount {
			return 0, 0, fmt.Errorf("%w: partially canceled payment has an invalid balance", errTossCancellationEvidenceInvalid)
		}
	default:
		return 0, 0, fmt.Errorf("%w: invalid cancellation status", errTossCancellationEvidenceInvalid)
	}
	expectedCanceled := auth.TotalAmount - auth.BalanceAmount
	if expectedCanceled <= 0 {
		return 0, 0, fmt.Errorf("%w: cancellation has no canceled amount", errTossCancellationEvidenceInvalid)
	}

	// Validate the complete provider state before inserting any row. Otherwise a
	// malformed later entry could leave a partial audit set committed.
	cancels := make([]tossPaymentCancel, 0, len(auth.Cancels))
	legacyCumulative := len(auth.Cancels) == 0
	completedTotal := int64(0)
	transactionKeys := make(map[string]struct{}, len(auth.Cancels))
	for i := range auth.Cancels {
		cancel := auth.Cancels[i]
		cancelStatus := strings.ToUpper(strings.TrimSpace(cancel.CancelStatus))
		transactionKey := cancel.TransactionKey
		if cancelStatus == "" || transactionKey == "" {
			// Older responses can omit status/key details. Do not infer which
			// individual rows completed; record one cumulative manual event below.
			legacyCumulative = true
			continue
		}
		if !isValidTossTransactionKey(transactionKey) {
			return 0, 0, fmt.Errorf("%w: cancellation transactionKey is invalid", errTossCancellationEvidenceInvalid)
		}
		if cancelStatus != "DONE" {
			continue
		}
		if cancel.CancelAmount <= 0 || cancel.CancelAmount > expectedCanceled-completedTotal {
			return 0, 0, fmt.Errorf("%w: completed cancellation amount exceeds the payment balance", errTossCancellationEvidenceInvalid)
		}
		if _, duplicate := transactionKeys[transactionKey]; duplicate {
			return 0, 0, fmt.Errorf("%w: duplicate cancellation transactionKey", errTossCancellationEvidenceInvalid)
		}
		transactionKeys[transactionKey] = struct{}{}
		cancel.TransactionKey = transactionKey
		cancels = append(cancels, cancel)
		completedTotal += cancel.CancelAmount
	}
	if legacyCumulative {
		// Preserve the provider's cumulative amount and label it explicitly so
		// operators never sum successive legacy snapshots as independent refunds.
		// The delta/backwards check runs under the order-row lock in the model.
		cancels = []tossPaymentCancel{{CancelAmount: expectedCanceled}}
	} else {
		if len(cancels) == 0 {
			// A cancellation transaction can become visible just before the
			// authoritative Payment resource exposes its DONE cancel detail. Keep
			// this retryable instead of classifying normal replica lag as a
			// permanent provider contradiction.
			return 0, 0, errors.New("Toss payment has no completed cancellation transaction")
		}
		if completedTotal != expectedCanceled {
			// A lower completed total can likewise be an eventually-consistent
			// cancel list. Impossible per-row amounts and duplicate identities were
			// rejected above as permanent evidence defects.
			return 0, 0, errors.New("Toss completed cancellation total does not match the payment balance")
		}
	}

	payload, err := common.Marshal(auth)
	if err != nil {
		return 0, 0, err
	}
	events := make([]*model.TossPaymentEvent, 0, len(cancels))
	for i := range cancels {
		cancel := cancels[i]
		events = append(events, &model.TossPaymentEvent{
			EventKey:               tossCancellationEventKey(auth.PaymentKey, cancel, auth.Status, auth.BalanceAmount),
			EventType:              model.TossPaymentEventTypeCancellation,
			OrderId:                auth.OrderId,
			PaymentKey:             auth.PaymentKey,
			Status:                 auth.Status,
			TransactionKey:         cancel.TransactionKey,
			CancelAmount:           cancel.CancelAmount,
			CancelAmountCumulative: legacyCumulative,
			BalanceAmount:          auth.BalanceAmount,
			OriginalAmount:         auth.TotalAmount,
			ProviderPayload:        string(payload),
			ReconciliationStatus:   model.TossReconciliationStatusRequired,
		})
	}
	createdCount, createdAmount, recordErr := model.RecordTossCancellationEventsForOrderWithContext(ctx, auth.OrderId, events)
	if recordErr != nil {
		return 0, 0, recordErr
	}
	newlyCanceled = createdAmount
	return createdCount, newlyCanceled, nil
}

// persistTossFulfillmentEvent durably records that Toss has taken payment but
// the corresponding local credit/subscription fulfillment still needs work.
// The stable order-based key makes repeated callbacks and cleanup runs safe.
func persistTossFulfillmentEvent(auth *tossConfirmResponse) (bool, error) {
	return persistTossFulfillmentEventWithContext(context.Background(), auth)
}

func persistTossFulfillmentEventWithContext(ctx context.Context, auth *tossConfirmResponse) (bool, error) {
	if auth == nil || strings.TrimSpace(auth.PaymentKey) == "" || strings.TrimSpace(auth.OrderId) == "" {
		return false, errors.New("invalid Toss paid payment")
	}
	payload, err := common.Marshal(auth)
	if err != nil {
		return false, err
	}
	return model.RecordTossPaymentEventWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             "toss_fulfillment_" + common.Sha1([]byte(strings.TrimSpace(auth.OrderId))),
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              auth.OrderId,
		PaymentKey:           auth.PaymentKey,
		Status:               auth.Status,
		BalanceAmount:        auth.BalanceAmount,
		OriginalAmount:       auth.TotalAmount,
		ProviderPayload:      string(payload),
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
}

// persistTossFinancialMismatchEvent records authenticated provider evidence
// that cannot be safely applied to the immutable local order. It deliberately
// uses a different key and event type from paid_pending_fulfillment: a later
// successful local settlement may auto-resolve a fulfillment retry, but it
// must not erase an amount, currency, instrument, or identity discrepancy that
// still requires an operator's explicit review.
func persistTossFinancialMismatchEvent(auth *tossConfirmResponse, reason string) (bool, error) {
	return persistTossFinancialMismatchEventWithContext(context.Background(), auth, reason)
}

func persistTossFinancialMismatchEventWithContext(ctx context.Context, auth *tossConfirmResponse, reason string) (bool, error) {
	if auth == nil || strings.TrimSpace(auth.PaymentKey) == "" || strings.TrimSpace(auth.OrderId) == "" {
		return false, errors.New("invalid Toss financial mismatch payment")
	}
	payload, err := common.Marshal(auth)
	if err != nil {
		return false, err
	}
	orderID := strings.TrimSpace(auth.OrderId)
	paymentKey := strings.TrimSpace(auth.PaymentKey)
	status := strings.ToUpper(strings.TrimSpace(auth.Status))
	return model.RecordTossPaymentEventWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             model.TossFinancialMismatchEventKey(orderID, paymentKey, status, auth.TotalAmount, auth.BalanceAmount),
		EventType:            model.TossPaymentEventTypeFinancialMismatch,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               status,
		BalanceAmount:        auth.BalanceAmount,
		OriginalAmount:       auth.TotalAmount,
		ProviderPayload:      string(payload),
		ReconciliationStatus: model.TossReconciliationStatusRequired,
		ResolutionNote:       strings.TrimSpace(reason),
	})
}

func queueTossTopUpRefundRequirementWithContext(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse, reason string) (bool, error) {
	if topUp == nil || auth == nil || topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss {
		return false, errors.New("invalid Toss top-up refund request")
	}
	orderID := strings.TrimSpace(auth.OrderId)
	paymentKey := strings.TrimSpace(auth.PaymentKey)
	if orderID != strings.TrimSpace(topUp.TradeNo) || paymentKey == "" ||
		auth.Type != "NORMAL" || auth.TotalAmount != topUp.Amount ||
		!strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
		return false, errors.New("Toss top-up refund evidence does not match the local order")
	}
	payload, err := common.Marshal(auth)
	if err != nil {
		return false, err
	}
	return model.RecordTossTopUpRefundRequirementWithContext(ctx, &model.TossPaymentEvent{
		EventKey:             model.TossTopUpRefundRequiredEventKey(orderID, paymentKey),
		EventType:            model.TossPaymentEventTypeRefundRequired,
		OrderId:              orderID,
		PaymentKey:           paymentKey,
		Status:               strings.ToUpper(strings.TrimSpace(auth.Status)),
		BalanceAmount:        auth.BalanceAmount,
		OriginalAmount:       auth.TotalAmount,
		ProviderPayload:      string(payload),
		ReconciliationStatus: model.TossReconciliationStatusRequired,
		ResolutionNote:       strings.TrimSpace(reason),
	})
}

// persistUncreditedTossTopUpCancellationWithContext records authenticated
// cancellation evidence for a top-up that was never credited locally. The
// original purchase contract may have been browser-mutated or tightened since
// the order was created, so method/tax/escrow fields must not prevent recovery:
// exact NORMAL identity, amount, and currency are sufficient to cancel money
// that cannot be fulfilled. A partial cancellation installs the durable refund
// fence only after its cancellation transaction is safely in the ledger. The
// same fence is required for CANCELED/balance=0 virtual-account payments while
// the separate bank refund is pending or otherwise unproved.
func persistUncreditedTossTopUpCancellationWithContext(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse, reason string) (createdCount int, newlyCanceled int64, err error) {
	if topUp == nil || auth == nil || topUp.Status == common.TopUpStatusSuccess ||
		topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss {
		return 0, 0, errors.New("invalid uncredited Toss top-up cancellation")
	}
	orderID := strings.TrimSpace(topUp.TradeNo)
	paymentKey := strings.TrimSpace(auth.PaymentKey)
	if auth.Type != "NORMAL" || strings.TrimSpace(auth.OrderId) != orderID || paymentKey == "" ||
		auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") ||
		!isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
		return 0, 0, errors.New("Toss uncredited cancellation evidence does not match the local order")
	}
	createdCount, newlyCanceled, err = persistTossCancellationEventsWithContext(ctx, auth)
	if err != nil {
		return 0, 0, err
	}
	if !tossTopUpCancellationNeedsRefundFence(auth) {
		return createdCount, newlyCanceled, nil
	}
	refundRequired, err := model.HasRequiredTossTopUpRefundWithContext(ctx, orderID, paymentKey)
	if err != nil {
		return createdCount, newlyCanceled, err
	}
	if !refundRequired {
		if _, err := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, reason); err != nil {
			return createdCount, newlyCanceled, err
		}
	}
	return createdCount, newlyCanceled, nil
}

func recordTossTopUpSettlementFailure(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse, settlementErr error) error {
	if errors.Is(settlementErr, model.ErrTopUpQuotaCapacityExceeded) {
		_, err := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "quota capacity prevented local fulfillment")
		return err
	}
	if errors.Is(settlementErr, model.ErrTossRefundRequiredPrecedesFulfillment) {
		// The durable refund event already owns this payment. A duplicate browser
		// callback must not create a competing fulfillment work item.
		return nil
	}
	_, err := persistTossFulfillmentEventWithContext(ctx, auth)
	return err
}

type tossVirtualAccountRefundState int

const (
	tossVirtualAccountRefundNotApplicable tossVirtualAccountRefundState = iota
	tossVirtualAccountRefundUnfunded
	tossVirtualAccountRefundCompleted
	tossVirtualAccountRefundPending
	tossVirtualAccountRefundUnproved
)

func classifyTossVirtualAccountRefund(auth *tossConfirmResponse) tossVirtualAccountRefundState {
	if auth == nil {
		return tossVirtualAccountRefundUnproved
	}
	if auth.Method != "가상계좌" {
		if isKnownSynchronousTossRefundMethod(auth.Method) {
			return tossVirtualAccountRefundNotApplicable
		}
		return tossVirtualAccountRefundUnproved
	}
	if auth.VirtualAccount == nil {
		return tossVirtualAccountRefundUnproved
	}
	status := auth.VirtualAccount.RefundStatus
	// Do not normalize provider enums here. Missing values, surrounding
	// whitespace, lowercase spellings, and future values are all intentionally
	// fail-closed until their financial meaning is reviewed.
	if status == "" || status != strings.TrimSpace(status) {
		return tossVirtualAccountRefundUnproved
	}
	switch status {
	case "COMPLETED":
		return tossVirtualAccountRefundCompleted
	case "PENDING":
		return tossVirtualAccountRefundPending
	case "FAILED", "PARTIAL_FAILED":
		return tossVirtualAccountRefundUnproved
	case "NONE":
		// A CANCELED virtual account with an exactly empty/null approval timestamp
		// never received money, so no bank refund is needed. Whitespace or any
		// non-empty value is malformed/funded and cannot prove that funds returned.
		if auth.ApprovedAt == "" {
			return tossVirtualAccountRefundUnfunded
		}
		return tossVirtualAccountRefundUnproved
	default:
		return tossVirtualAccountRefundUnproved
	}
}

func isKnownSynchronousTossRefundMethod(method string) bool {
	if method == "" || method != strings.TrimSpace(method) {
		return false
	}
	switch method {
	case "카드", "간편결제", "계좌이체", "휴대폰", "문화상품권", "도서문화상품권", "게임문화상품권":
		return true
	default:
		return false
	}
}

func tossVirtualAccountRefundWaitError(auth *tossConfirmResponse) error {
	if auth == nil || !strings.EqualFold(strings.TrimSpace(auth.Status), "CANCELED") || auth.BalanceAmount != 0 {
		return nil
	}
	switch classifyTossVirtualAccountRefund(auth) {
	case tossVirtualAccountRefundPending:
		return errTossTopUpVirtualAccountRefundPending
	case tossVirtualAccountRefundCompleted, tossVirtualAccountRefundUnfunded, tossVirtualAccountRefundNotApplicable:
		return nil
	default:
		return errTossTopUpVirtualAccountRefundUnproved
	}
}

func tossTopUpCancellationNeedsRefundFence(auth *tossConfirmResponse) bool {
	if auth == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(auth.Status), "PARTIAL_CANCELED") && auth.BalanceAmount > 0 {
		return true
	}
	waitErr := tossVirtualAccountRefundWaitError(auth)
	return errors.Is(waitErr, errTossTopUpVirtualAccountRefundPending) ||
		errors.Is(waitErr, errTossTopUpVirtualAccountRefundUnproved)
}

func validateTossTopUpRefundPOSTState(auth *tossConfirmResponse) error {
	if auth == nil || auth.TotalAmount <= 0 {
		return errors.New("Toss refund provider state is unavailable")
	}
	switch auth.Status {
	case "DONE", "WAITING_FOR_DEPOSIT":
		if auth.BalanceAmount != auth.TotalAmount {
			return errors.New("Toss refund provider state has an inconsistent full balance")
		}
	case "PARTIAL_CANCELED":
		if auth.BalanceAmount <= 0 || auth.BalanceAmount >= auth.TotalAmount {
			return errors.New("Toss refund provider state has an inconsistent partial balance")
		}
	default:
		return fmt.Errorf("Toss refund provider status %q does not authorize cancellation", auth.Status)
	}
	return nil
}

func isFullyCanceledTossTopUp(auth *tossConfirmResponse, topUp *model.TopUp) bool {
	baseCanceled := auth != nil && topUp != nil && auth.Type == "NORMAL" &&
		strings.EqualFold(strings.TrimSpace(auth.Status), "CANCELED") &&
		strings.TrimSpace(auth.OrderId) == strings.TrimSpace(topUp.TradeNo) &&
		strings.TrimSpace(auth.PaymentKey) == strings.TrimSpace(topUp.ProviderOrderId) &&
		auth.TotalAmount == topUp.Amount && auth.BalanceAmount == 0 &&
		strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW")
	if !baseCanceled {
		return false
	}
	switch classifyTossVirtualAccountRefund(auth) {
	case tossVirtualAccountRefundNotApplicable, tossVirtualAccountRefundCompleted, tossVirtualAccountRefundUnfunded:
		return true
	default:
		return false
	}
}

func finalizeQueuedTossTopUpRefundIfCanceled(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse) (bool, error) {
	if !isFullyCanceledTossTopUp(auth, topUp) {
		return false, nil
	}
	required, err := model.HasRequiredTossTopUpRefundWithContext(ctx, topUp.TradeNo, auth.PaymentKey)
	if err != nil || !required {
		return false, err
	}
	return true, cancelRequiredTossTopUp(ctx, topUp, auth)
}

func isUnfundedTerminalTossTopUp(auth *tossConfirmResponse, topUp *model.TopUp) bool {
	if auth == nil || topUp == nil || auth.Type != "NORMAL" ||
		strings.TrimSpace(auth.OrderId) != strings.TrimSpace(topUp.TradeNo) ||
		strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(topUp.ProviderOrderId) ||
		auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
		return false
	}
	status := strings.ToUpper(strings.TrimSpace(auth.Status))
	return status == "EXPIRED" || status == "ABORTED"
}

func finalizeQueuedTossTopUpRefundIfTerminal(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse) (bool, error) {
	if handled, err := finalizeQueuedTossTopUpRefundIfCanceled(ctx, topUp, auth); handled || err != nil {
		return handled, err
	}
	if !isUnfundedTerminalTossTopUp(auth, topUp) {
		return false, nil
	}
	required, err := model.HasRequiredTossTopUpRefundWithContext(ctx, topUp.TradeNo, auth.PaymentKey)
	if err != nil || !required {
		return false, err
	}
	payload, err := common.Marshal(auth)
	if err != nil {
		return true, err
	}
	return true, model.FinalizeUnfundedTossTopUpRefundRequirementWithContext(
		ctx,
		topUp.TradeNo,
		auth.PaymentKey,
		auth.Status,
		string(payload),
	)
}

// handleRefundFencedTossTopUpWebhook authenticates the current provider state
// for an order already owned by the automatic-refund workflow. Webhooks never
// issue the cancellation POST: they only finalize authoritative no-balance
// states or persist partial-cancellation evidence, leaving provider writes to
// the bounded recovery worker.
func handleRefundFencedTossTopUpWebhook(c *gin.Context, topUp *model.TopUp, eventPaymentKey, claimedStatus string) bool {
	if c == nil || topUp == nil {
		return false
	}
	ctx := c.Request.Context()
	orderID := strings.TrimSpace(topUp.TradeNo)
	paymentKey := strings.TrimSpace(topUp.ProviderOrderId)
	if orderID == "" || paymentKey == "" || paymentKey == orderID {
		return false
	}
	required, err := model.HasRequiredTossTopUpRefundWithContext(ctx, orderID, paymentKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss refund-fenced webhook lookup failed order_id=%s error=%q", orderID, err.Error()))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if !required {
		return false
	}
	if supplied := strings.TrimSpace(eventPaymentKey); supplied != "" && supplied != paymentKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss refund-fenced webhook paymentKey mismatch order_id=%s", orderID))
		c.Status(http.StatusOK)
		return true
	}
	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	auth, _, err := getTossPaymentWithCredentialForMID(
		ctx,
		paymentKey,
		topUp.ProviderCredential,
		topUp.ProviderClientKeyHash,
		activeClientKey,
		activeSecretKey,
	)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss refund-fenced webhook verification failed order_id=%s error=%q", orderID, err.Error()))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if auth == nil || auth.Type != "NORMAL" || auth.OrderId != orderID || strings.TrimSpace(auth.PaymentKey) != paymentKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss refund-fenced webhook identity mismatch order_id=%s", orderID))
		c.Status(http.StatusOK)
		return true
	}
	laggingFinalCancellation := shouldRetryTossCancellationWebhook(claimedStatus, auth, orderID, paymentKey)
	if laggingFinalCancellation && !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, topUp, auth); handled || finalizeErr != nil {
		if finalizeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss refund-fenced webhook finalization failed order_id=%s error=%q", orderID, finalizeErr.Error()))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		c.Status(http.StatusOK)
		return true
	}
	if isTossCancelStatus(auth.Status) {
		if auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
			if _, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "refund-fenced cancellation amount or currency mismatch"); persistErr != nil {
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss refund-fenced cancellation amount or currency mismatch order_id=%s", orderID))
			if laggingFinalCancellation {
				c.Status(http.StatusServiceUnavailable)
			} else {
				c.Status(http.StatusOK)
			}
			return true
		}
		// The active fence can exist precisely because method/tax/escrow fields do
		// not satisfy today's purchase contract. Exact authenticated identity and
		// amount are enough to preserve later cancellation transactions.
		if _, _, persistErr := persistTossCancellationEventsWithContext(ctx, auth); persistErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss refund-fenced cancellation persist failed order_id=%s error=%q", orderID, persistErr.Error()))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
	}
	if laggingFinalCancellation {
		// The partial state and active fence are now durable, but retain webhook
		// delivery until the final cancellation is visible as well.
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	// DONE, WAITING_FOR_DEPOSIT, and PARTIAL_CANCELED with a remaining balance
	// are already durable work. A retrying webhook cannot improve them.
	c.Status(http.StatusOK)
	return true
}

// cancelRequiredTossTopUp performs the queued provider write only from the
// background reconciliation lane, after it has fetched the authoritative
// Payment object. The refund fence durably pins the idempotency key and first
// attempt time for each remaining balance. Ambiguous retries therefore keep the
// exact key for Toss's full 15-day retention window, including across hourly
// scheduler boundaries. A lower authoritative balance proves that an earlier
// cancellation completed and starts a new operation immediately; an unchanged
// balance may rotate only after the documented retention period has elapsed.
// The exact order-time credential is attempted first. A definitively rejected
// old key may fall forward only within the proven same MID: unlike a charge, a
// full cancellation is monotonic and cannot refund more than the provider
// balance observed immediately before the POST.
func cancelRequiredTossTopUp(ctx context.Context, topUp *model.TopUp, auth *tossConfirmResponse) error {
	if topUp == nil || auth == nil {
		return errors.New("Toss refund payment is unavailable")
	}
	orderID := strings.TrimSpace(topUp.TradeNo)
	paymentKey := strings.TrimSpace(topUp.ProviderOrderId)
	if auth.OrderId != orderID || auth.PaymentKey != paymentKey || auth.Type != "NORMAL" ||
		auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
		return errors.New("Toss refund payment identity mismatch")
	}
	refundRequired, err := model.HasRequiredTossTopUpRefundWithContext(ctx, orderID, paymentKey)
	if err != nil {
		return err
	}
	if !refundRequired {
		return errors.New("Toss refund requirement is unavailable")
	}

	result := auth
	persistCancellation := func(payment *tossConfirmResponse) error {
		if payment == nil || !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(payment.Status))) {
			return nil
		}
		_, _, persistErr := persistTossCancellationEventsWithContext(ctx, payment)
		return persistErr
	}
	// A previously observed partial cancellation is financial evidence in its
	// own right. Persist it before attempting the remaining balance so a crash or
	// later provider outage cannot erase the completed transactionKey/amount.
	if err := persistCancellation(result); err != nil {
		return err
	}
	if waitErr := tossVirtualAccountRefundWaitError(result); waitErr != nil {
		// CANCELED/balance=0 only proves that Toss accepted the cancellation.
		// A deposited virtual account remains refund-owned until the bank refund
		// status itself proves completion; never POST cancel again in the meantime.
		return waitErr
	}
	if !isFullyCanceledTossTopUp(result, topUp) {
		if err := validateTossTopUpRefundPOSTState(result); err != nil {
			return err
		}
		virtualAccountStatus := strings.ToUpper(strings.TrimSpace(result.Status))
		if strings.TrimSpace(result.Method) == "가상계좌" &&
			(virtualAccountStatus == "DONE" || virtualAccountStatus == "PARTIAL_CANCELED") && result.BalanceAmount > 0 {
			// Toss requires refundReceiveAccount after a virtual-account deposit.
			// This service never stores a buyer bank account, so repeated blind API
			// calls cannot succeed; retain the durable event for an operator-assisted
			// refund and let the scheduler revisit it at a low frequency.
			return errTossTopUpRefundAccountRequired
		}
		activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
		credentialCandidates := tossCredentialCandidatesForMID(
			ctx,
			topUp.ProviderCredential,
			topUp.ProviderClientKeyHash,
			activeClientKey,
			activeSecretKey,
		)
		if len(credentialCandidates) == 0 {
			return errors.New("Toss refund credential is unavailable")
		}
		if result.BalanceAmount <= 0 {
			return errors.New("Toss refund has no positive authoritative balance")
		}
		body, err := common.Marshal(map[string]interface{}{
			"cancelReason": "Automatic full refund for an uncredited top-up",
		})
		if err != nil {
			return err
		}
		operationCtx, cancel := tossPaymentOperationContext(ctx, tossPaymentConfirmTimeout)
		defer cancel()
		operation, err := model.PrepareTossTopUpRefundOperationWithContext(ctx, orderID, paymentKey, result.BalanceAmount)
		if err != nil {
			return err
		}
		idempotencyKey := operation.IdempotencyKey
		if operation.Rotated {
			logger.LogWarn(ctx, fmt.Sprintf("Toss refund provider operation rotated after balance progress or idempotency expiry order_id=%s balance=%d", orderID, result.BalanceAmount))
		}
		var responseBody []byte
		var requestErr error
		for index, secretKey := range credentialCandidates {
			var statusCode int
			statusCode, responseBody, requestErr = doTossPaymentProcessingAPIRequestWithSecret(
				operationCtx,
				http.MethodPost,
				tossAPIBase+"/v1/payments/"+url.PathEscape(paymentKey)+"/cancel",
				body,
				idempotencyKey,
				secretKey,
				http.StatusOK,
			)
			if requestErr == nil || !isTossCredentialRejection(statusCode, requestErr) || index == len(credentialCandidates)-1 {
				break
			}
			logger.LogWarn(ctx, "Toss stored refund credential rejected; retrying with current same-MID key")
		}
		if requestErr != nil {
			return fmt.Errorf("Toss automatic top-up refund failed: %w", requestErr)
		}
		var canceled tossConfirmResponse
		if err := common.Unmarshal(responseBody, &canceled); err != nil {
			return err
		}
		sanitizeTossPaymentResponse(&canceled)
		if err := validateTossPaymentResponseShape(&canceled); err != nil {
			return fmt.Errorf("invalid Toss refund response: %w", err)
		}
		// The cancellation endpoint is paymentKey-scoped, but no malformed or
		// misrouted 2xx response may create cancellation precedence for another
		// local payment. In particular, the virtual-account pending-refund branch
		// persists its cancellation ledger before final bank completion, so bind
		// the response to the exact requested payment before entering that branch.
		if canceled.Type != "NORMAL" || canceled.OrderId != orderID || canceled.PaymentKey != paymentKey ||
			canceled.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(canceled.Currency), "KRW") {
			return errors.New("Toss automatic top-up refund returned a different payment identity")
		}
		result = &canceled
		// A cancellation request can legitimately complete only part of the
		// remaining balance. Commit every completed transaction before reporting
		// that the full-refund objective still needs another reconciliation pass.
		if err := persistCancellation(result); err != nil {
			return err
		}
	}
	if waitErr := tossVirtualAccountRefundWaitError(result); waitErr != nil {
		return waitErr
	}
	if !isFullyCanceledTossTopUp(result, topUp) {
		return errors.New("Toss automatic top-up refund did not fully cancel the payment")
	}
	payload, err := common.Marshal(result)
	if err != nil {
		return err
	}
	return model.FinalizeFullyRefundedTossTopUpWithContext(ctx, orderID, paymentKey, string(payload))
}

// tossCredentialCandidatesForMID keeps an order in the Toss MID namespace it
// was created under. The encrypted order-time secret is always attempted first.
// A current secret is eligible only when the order has a non-empty client-key
// fingerprint and it matches the currently active client key. Legacy orders
// without a fingerprint therefore never cross into a new idempotency namespace.
func tossCredentialCandidatesForMID(ctx context.Context, encrypted, storedClientKeyHash, activeClientKey, activeSecret string) []string {
	candidates := make([]string, 0, 2)
	stored, err := model.DecryptProviderCredential(encrypted)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss provider credential decrypt failed: %v", err))
	} else if stored = strings.TrimSpace(stored); stored != "" {
		candidates = append(candidates, stored)
	}

	activeClientKeyHash := model.TossClientKeyFingerprint(activeClientKey)
	activeSecret = strings.TrimSpace(activeSecret)
	if !model.IsValidTossClientKeyFingerprint(storedClientKeyHash) || activeClientKeyHash == "" ||
		storedClientKeyHash != activeClientKeyHash || activeSecret == "" {
		return candidates
	}
	if len(candidates) == 0 || candidates[0] != activeSecret {
		candidates = append(candidates, activeSecret)
	}
	return candidates
}

func isTossCredentialRejection(status int, err error) bool {
	if err == nil || (status != http.StatusBadRequest && status != http.StatusUnauthorized && status != http.StatusForbidden) {
		return false
	}
	// A 403 is commonly a real card/FDS/limit decline, and payment lookup can
	// also return FORBIDDEN_CONSECUTIVE_REQUEST. Retrying those with a rotated
	// secret is unsafe because Toss scopes idempotency by API key as well as the
	// Idempotency-Key. Fall back only for explicit authentication error codes.
	switch tossAPIErrorCode(err) {
	case "INVALID_API_KEY", "UNAUTHORIZED_KEY", "INCORRECT_BASIC_AUTH_FORMAT":
		return true
	default:
		return false
	}
}

func getTossPayMoney(amountKRW int64, group string) int64 {
	return model.TossTopUpChargedKRW(amountKRW, group)
}

func getTossTopUpQuote(amount int64, amountMode string, group string) model.TossTopUpQuote {
	return model.QuoteTossTopUp(amount, amountMode, group)
}

func getTossTopUpQuoteWithSnapshot(amount int64, amountMode string, group string, snapshot setting.TossConfigSnapshot) model.TossTopUpQuote {
	return model.QuoteTossTopUpWithUnitPrice(amount, amountMode, group, snapshot.UnitPrice)
}

func tossTopUpQuoteBelowConfiguredMinimum(quote model.TossTopUpQuote) bool {
	return quote.ChargeKRW < int64(setting.TossEffectiveGeneralTopUp())
}

func tossTopUpSnapshotMinimum(snapshot setting.TossConfigSnapshot) int {
	minimum := snapshot.MinTopUp
	if minimum < int(setting.TossGeneralPaymentMinimumAmountKRW) {
		minimum = int(setting.TossGeneralPaymentMinimumAmountKRW)
	}
	return minimum
}

func tossTopUpQuoteBelowSnapshotMinimum(quote model.TossTopUpQuote, snapshot setting.TossConfigSnapshot) bool {
	return quote.ChargeKRW < int64(tossTopUpSnapshotMinimum(snapshot))
}

func RequestTossAmount(c *gin.Context) {
	var req TossPayRequest
	if err := decodeTossPaymentRequestJSON(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !isValidTossTopUpAmountModeInput(req.AmountMode) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !isTossTopUpEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	tossConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isFreshTossTopUpCheckoutEnabled(tossConfig) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if getTopUpTargetType(c) == model.TopUpTargetTypeUser && user.OrganizationId > 0 {
		common.ApiErrorI18n(c, i18n.MsgPaymentPersonalBillingOrgActive)
		return
	}
	quote := getTossTopUpQuoteWithSnapshot(req.Amount, req.AmountMode, user.Group, tossConfig)
	if quote.ChargeKRW < setting.TossGeneralPaymentMinimumAmountKRW {
		common.ApiErrorI18n(c, i18n.MsgPaymentAmountTooLow)
		return
	}
	if tossTopUpQuoteBelowSnapshotMinimum(quote, tossConfig) {
		minimum := tossTopUpSnapshotMinimum(tossConfig)
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": minimum})
		return
	}
	if quote.ChargeKRW <= 0 || quote.CreditQuota <= 0 {
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote})
}

// isValidServerAddress reports whether addr can be used as a Toss callback base.
// Public callback URLs must be HTTPS; HTTP is allowed only for local development.
func isValidServerAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if strings.IndexFunc(addr, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
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
	if err := decodeTossPaymentRequestJSON(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !isValidTossTopUpAmountModeInput(req.AmountMode) {
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
	tossConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isFreshTossTopUpCheckoutEnabled(tossConfig) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	activeClientKey, activeSecretKey := setting.TossActiveKeyPairFromSnapshot(tossConfig)
	if strings.TrimSpace(activeClientKey) == "" || strings.TrimSpace(activeSecretKey) == "" {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		logger.LogError(c.Request.Context(), "Toss pay blocked: invalid ServerAddress configuration")
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}

	id := c.GetInt("id")
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}
	if getTopUpTargetType(c) == model.TopUpTargetTypeUser && user.OrganizationId > 0 {
		common.ApiErrorI18n(c, i18n.MsgPaymentPersonalBillingOrgActive)
		return
	}

	quote := getTossTopUpQuoteWithSnapshot(req.Amount, req.AmountMode, user.Group, tossConfig)
	chargedKRW := quote.ChargeKRW
	if chargedKRW < setting.TossGeneralPaymentMinimumAmountKRW {
		common.ApiErrorI18n(c, i18n.MsgPaymentAmountTooLow)
		return
	}
	if tossTopUpQuoteBelowSnapshotMinimum(quote, tossConfig) {
		minimum := tossTopUpSnapshotMinimum(tossConfig)
		common.ApiErrorI18n(c, i18n.MsgTopupAmountTooSmall, map[string]any{"Min": minimum})
		return
	}
	if chargedKRW <= 0 || quote.CreditQuota <= 0 {
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
	if !isValidTossSDKCustomerKey(customerKey) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss pay blocked: stored customerKey is incompatible with SDK v2 user_id=%d", id))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	reference := fmt.Sprintf("new-api-toss-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(16))
	orderId := "toss_" + common.Sha1([]byte(reference))
	providerCredential, err := model.EncryptProviderCredential(activeSecretKey)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss encrypt provider credential failed user_id=%d order_id=%s error=%q", id, orderId, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	topUp := &model.TopUp{
		UserId:                id,
		TargetType:            getTopUpTargetType(c),
		TargetId:              getTopUpTargetId(c),
		Amount:                chargedKRW,
		Money:                 quote.CreditAmount,
		Quota:                 quote.CreditQuota,
		TradeNo:               orderId,
		ProviderOrderId:       orderId,
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: model.TossClientKeyFingerprint(activeClientKey),
		PaymentMethod:         model.PaymentMethodToss,
		PaymentProvider:       model.PaymentProviderToss,
		// Cleanup cutoffs use the database clock; snapshot creation with that same
		// clock so a skewed API node cannot make a fresh checkout look stale.
		CreateTime: model.GetDBTimestamp(),
		Status:     common.TopUpStatusPending,
	}
	if err := model.CreatePendingTossTopUpCheckout(topUp); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Toss create topup order failed user_id=%d order_id=%s error=%q", id, orderId, err.Error()))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	serverBase := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":    activeClientKey,
			"customer_key":  customerKey,
			"order_id":      orderId,
			"order_name":    fmt.Sprintf("크레딧 충전 %d원", chargedKRW),
			"amount":        chargedKRW,
			"charge_amount": chargedKRW,
			"credit_amount": quote.CreditAmount,
			"credit_quota":  quote.CreditQuota,
			"unit_price":    quote.UnitPrice,
			"amount_mode":   quote.AmountMode,
			"success_url":   serverBase + "/api/toss/confirm",
			"fail_url":      serverBase + "/api/toss/fail",
		},
	})
}

func confirmTossPaymentWithSecret(ctx context.Context, paymentKey, orderId string, amount int64, secretKey, idempotencyKey string) (*tossConfirmResponse, int, error) {
	ctx, cancel := tossPaymentOperationContext(ctx, tossPaymentConfirmTimeout)
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
	freshConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		return nil, 0, fmt.Errorf("Toss top-up configuration is stale: %w", err)
	}
	if !freshConfig.Enabled || !isPaymentComplianceConfirmed() {
		return nil, 0, errors.New("Toss top-up is operationally disabled")
	}

	statusCode, respBody, err := doTossPaymentProcessingAPIRequestWithSecret(ctx, http.MethodPost, tossAPIBase+"/v1/payments/confirm", bodyBytes, idempotencyKey, secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss confirm failed: %w", err)
	}

	var result tossConfirmResponse
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, statusCode, err
	}
	sanitizeTossPaymentResponse(&result)
	if err := validateTossPaymentResponseShape(&result); err != nil {
		return &result, statusCode, fmt.Errorf("invalid Toss payment confirmation response: %w", err)
	}
	if result.Type != "NORMAL" {
		return &result, statusCode, errors.New("Toss payment confirmation returned an unexpected payment type")
	}
	return &result, statusCode, nil
}

func confirmTossPaymentWithCredentialForMID(ctx context.Context, paymentKey, orderId string, amount int64, claimToken, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret string) (*tossConfirmResponse, int, error) {
	candidates := tossConfirmCredentialCandidatesForMID(ctx, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret)
	return confirmTossPaymentWithCredentialCandidates(ctx, paymentKey, orderId, amount, claimToken, candidates)
}

// tossConfirmCredentialCandidatesForMID deliberately keeps only the exact
// order-time credential. Toss scopes Idempotency-Key by API key as well as the
// request URL/method, so repeating POST /confirm with a rotated key would be a
// different idempotency namespace and could duplicate an earlier ambiguous
// attempt. A proven same-MID current key remains eligible for GET verification
// through tossCredentialCandidatesForMID, never for a provider POST.
func tossConfirmCredentialCandidatesForMID(ctx context.Context, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret string) []string {
	stored, err := model.DecryptProviderCredential(encryptedCredential)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirmation credential decrypt failed: %v", err))
		return nil
	}
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return nil
	}
	return []string{stored}
}

func confirmTossPaymentWithCredentialCandidates(ctx context.Context, paymentKey, orderId string, amount int64, claimToken string, candidates []string) (*tossConfirmResponse, int, error) {
	if len(candidates) == 0 {
		return nil, 0, errors.New("Toss secret key is unavailable")
	}
	if !isTossTopUpOperationallyEnabled() {
		return nil, 0, errors.New("Toss top-up is operationally disabled")
	}
	if err := model.RequireFreshTossConfig(); err != nil {
		return nil, 0, fmt.Errorf("Toss top-up configuration is stale: %w", err)
	}
	secretKey := candidates[0]
	idempotencyKey, err := model.PrepareClaimedPendingTossTopUpConfirm(orderId, paymentKey, amount, claimToken, secretKey)
	if err != nil {
		return nil, 0, err
	}
	return confirmTossPaymentWithSecret(ctx, paymentKey, orderId, amount, secretKey, idempotencyKey)
}

// getTossPaymentWithSecret fetches the authoritative payment object from Toss.
// It returns the HTTP status code so callers can distinguish a definitive
// 404 (no such payment) from transient (network/5xx) failures.
func getTossPaymentWithSecret(ctx context.Context, paymentKey, secretKey string) (*tossConfirmResponse, int, error) {
	// Webhook-only path: stay under Toss's ~10s webhook response window.
	ctx, cancel := context.WithTimeout(ctx, tossWebhookProcessingTimeout)
	defer cancel()
	statusCode, body, err := doTossAPIRequestWithSecret(ctx, http.MethodGet, tossAPIBase+"/v1/payments/"+url.PathEscape(paymentKey), nil, "", secretKey, http.StatusOK)
	if err != nil {
		return nil, statusCode, fmt.Errorf("toss get payment failed: %w", err)
	}
	var result tossConfirmResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, statusCode, err
	}
	sanitizeTossPaymentResponse(&result)
	if err := validateTossPaymentResponseShape(&result); err != nil {
		return nil, statusCode, fmt.Errorf("invalid Toss payment lookup response: %w", err)
	}
	if result.PaymentKey != paymentKey {
		return &result, statusCode, errors.New("Toss payment lookup returned a different paymentKey")
	}
	return &result, statusCode, nil
}

func getTossPaymentWithCredentialForMID(ctx context.Context, paymentKey, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret string) (*tossConfirmResponse, int, error) {
	candidates := tossCredentialCandidatesForMID(ctx, encryptedCredential, storedClientKeyHash, activeClientKey, activeSecret)
	return getTossPaymentWithCredentialCandidates(ctx, paymentKey, candidates)
}

func getTossPaymentWithCredentialCandidates(ctx context.Context, paymentKey string, candidates []string) (*tossConfirmResponse, int, error) {
	if len(candidates) == 0 {
		return nil, 0, errors.New("Toss secret key is unavailable")
	}
	var result *tossConfirmResponse
	var status int
	var err error
	for i := range candidates {
		result, status, err = getTossPaymentWithSecret(ctx, paymentKey, candidates[i])
		if err == nil || !isTossCredentialRejection(status, err) || i == len(candidates)-1 {
			return result, status, err
		}
		logger.LogWarn(ctx, "Toss stored credential rejected during payment lookup; retrying with current key")
	}
	return result, status, err
}

func getTossPayment(ctx context.Context, paymentKey string) (*tossConfirmResponse, int, error) {
	result, status, err := getTossPaymentWithSecret(ctx, paymentKey, setting.TossActiveSecretKey())
	if err == nil && result.Type != "NORMAL" {
		return result, status, errors.New("Toss top-up payment lookup returned an unexpected payment type")
	}
	return result, status, err
}

func getTossBillingPayment(ctx context.Context, paymentKey string) (*tossConfirmResponse, int, error) {
	result, status, err := getTossPaymentWithSecret(ctx, paymentKey, setting.TossActiveBillingSecretKey())
	if err == nil && result.Type != "BILLING" {
		return result, status, errors.New("Toss billing payment lookup returned an unexpected payment type")
	}
	return result, status, err
}

func tossRedirect(c *gin.Context, path string) {
	// Toss callback URLs contain one-time/payment identifiers. Do not let the
	// browser cache them or forward the callback URL as a referrer to console
	// assets or any later navigation.
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(http.StatusFound, path)
}

func tossFailureRedirectPath(orderId, code, message string) string {
	return tossFailureRedirectPathForTarget(false, orderId, code, message)
}

func tossFailureRedirectPathForTarget(organization bool, orderId, code, message string) string {
	basePath := "/console/topup"
	if organization {
		// `/wallet` is understood by both frontends. Default dispatches the
		// authenticated organization owner to OrganizationWallet, while Classic
		// rewrites it to its existing `/console/topup` page.
		basePath = "/wallet"
	}
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
		return basePath
	}
	return basePath + "?" + values.Encode()
}

func tossTopUpFailureRedirectPath(ctx context.Context, orderId, code, message string) string {
	trimmedOrderID := strings.TrimSpace(orderId)
	organization := false
	if strings.HasPrefix(trimmedOrderID, "toss_") && isValidTossOrderId(trimmedOrderID) {
		// failUrl parameters are untrusted and never authorize a state change.
		// A read-only local lookup is used only to preserve the wallet context;
		// missing, malformed, cross-provider, and database-error cases all retain
		// the longstanding personal-wallet fallback.
		if topUp, err := model.GetTopUpByTradeNoWithErrorContext(ctx, trimmedOrderID); err == nil {
			organization = topUp.PaymentProvider == model.PaymentProviderToss &&
				topUp.PaymentMethod == model.PaymentMethodToss &&
				topUp.TargetType == model.TopUpTargetTypeOrganization &&
				topUp.TargetId > 0
		}
	}
	return tossFailureRedirectPathForTarget(organization, orderId, code, message)
}

func tossTopUpSuccessRedirectPath(topUp *model.TopUp) string {
	if topUp != nil && topUp.TargetType == model.TopUpTargetTypeOrganization {
		// `/wallet` is the cross-theme entry point: Default dispatches an
		// organization owner to OrganizationWallet, while Classic rewrites it to
		// `/console/topup` and preserves the query. A Default-only
		// `/organization/wallet` redirect would break callbacks if the frontend
		// theme changes while the Toss window is open.
		return "/wallet?show_history=true"
	}
	return "/console/log"
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
	if topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss {
		return fmt.Errorf("toss payment integration mismatch order_id=%s provider=%s method=%s", orderId, topUp.PaymentProvider, topUp.PaymentMethod)
	}
	if topUp.Amount != amount {
		return fmt.Errorf("toss amount mismatch order_id=%s expected=%d actual=%d", orderId, topUp.Amount, amount)
	}
	return nil
}

func isValidTossTopUpPayment(result *tossConfirmResponse, orderId string, amount int64) bool {
	return result != nil &&
		strings.TrimSpace(result.PaymentKey) != "" &&
		result.Type == "NORMAL" &&
		result.Status == "DONE" &&
		result.TotalAmount == amount &&
		result.BalanceAmount == result.TotalAmount &&
		result.OrderId == orderId &&
		strings.ToUpper(result.Currency) == "KRW" &&
		hasSupportedTossTopUpFundingSource(result)
}

func closeStaleTossPendingTopUp(ctx context.Context, topUp *model.TopUp, targetStatus, reason string) (bool, error) {
	return closeStaleTossPendingTopUpWithClaim(ctx, topUp, targetStatus, reason, "")

}

func closeStaleTossPendingTopUpWithClaim(ctx context.Context, topUp *model.TopUp, targetStatus, reason, claimToken string) (bool, error) {
	if topUp == nil {
		return false, model.ErrTopUpNotFound
	}
	closed, err := model.ClosePendingTossTopUpForReconcile(
		topUp.TradeNo,
		topUp.ProviderOrderId,
		targetStatus,
		claimToken,
		topUp.ProviderOrderTime,
		topUp.ProviderRetryTime,
	)
	if err != nil {
		return false, err
	}
	if !closed {
		logger.LogInfo(ctx, fmt.Sprintf("Toss stale pending top-up close deferred for newer worker order_id=%s target_status=%s reason=%s", topUp.TradeNo, targetStatus, reason))
		return false, nil
	}
	logger.LogWarn(ctx, fmt.Sprintf("Toss stale pending top-up closed order_id=%s status=%s reason=%s", topUp.TradeNo, targetStatus, reason))
	return true, nil
}

// recoverRejectedTossConfirmBinding removes an attacker-controlled paymentKey
// only after an authenticated provider result proves it cannot move money for
// this order. Scoped idempotency makes the restore safe because a real callback
// uses another provider namespace. Legacy attempts cannot be restored after a
// POST; retain their historical terminal-close behavior instead.
func recoverRejectedTossConfirmBinding(ctx context.Context, topUp *model.TopUp, paymentKey string, amount int64, claimToken, reason string) {
	if topUp == nil {
		return
	}
	reset, err := model.ResetClaimedPendingTossTopUpPaymentBinding(topUp.TradeNo, paymentKey, amount, claimToken)
	switch {
	case err == nil && reset:
		logger.LogWarn(ctx, fmt.Sprintf("Toss rejected callback paymentKey binding restored order_id=%s reason=%s", topUp.TradeNo, reason))
	case errors.Is(err, model.ErrTossTopUpBindingResetLegacy):
		closed, closeErr := closeStaleTossPendingTopUpWithClaim(ctx, topUp, common.TopUpStatusFailed, reason+"_legacy_idempotency", claimToken)
		if closeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss rejected legacy callback close failed order_id=%s reason=%s error=%q", topUp.TradeNo, reason, closeErr.Error()))
		} else if !closed {
			logger.LogInfo(ctx, fmt.Sprintf("Toss rejected legacy callback close lost to newer worker order_id=%s reason=%s", topUp.TradeNo, reason))
		}
	case errors.Is(err, model.ErrTossTopUpConfirmClaimLost):
		logger.LogInfo(ctx, fmt.Sprintf("Toss rejected callback binding changed before restore order_id=%s reason=%s", topUp.TradeNo, reason))
	case err != nil:
		// Provider payload/event evidence or a database failure must never be
		// erased merely to make a new browser callback possible.
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: rejected callback binding restore blocked order_id=%s reason=%s error=%q", topUp.TradeNo, reason, err.Error()))
	default:
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: rejected callback binding restore did not commit order_id=%s reason=%s", topUp.TradeNo, reason))
	}
}

func isTossRecordedTopUpPastApprovalWindow(topUp *model.TopUp) bool {
	if topUp == nil {
		return false
	}
	recordedAt := topUp.ProviderOrderTime
	if recordedAt <= 0 {
		recordedAt = topUp.CreateTime
	}
	return recordedAt > 0 && recordedAt <= model.GetDBTimestamp()-int64(tossRecordedTopUpTerminalAge/time.Second)
}

// isTossConfirmProviderPostWindowElapsed is the request-path backstop for a
// missed or disabled cleanup worker. A recorded paymentKey gets a short grace
// beyond Toss's ten-minute authentication window; a pristine checkout keeps
// the full payment-window allowance used by the periodic expiry job. After
// either bound, browser callbacks may still authorize a read-only GET and local
// settlement of an already-DONE payment, but must never create a provider POST
// whose 15-day idempotency record may no longer exist.
func isTossConfirmProviderPostWindowElapsed(topUp *model.TopUp) bool {
	if topUp == nil {
		return false
	}
	providerOrderID := strings.TrimSpace(topUp.ProviderOrderId)
	tradeNo := strings.TrimSpace(topUp.TradeNo)
	if providerOrderID != "" && providerOrderID != tradeNo {
		return isTossRecordedTopUpPastApprovalWindow(topUp)
	}
	return topUp.CreateTime > 0 &&
		topUp.CreateTime <= model.GetDBTimestamp()-int64(tossNeverRecordedTopUpTerminalAge/time.Second)
}

// retryTossGeneralTopUpConfirmAfterLookupMiss closes the response-loss gap
// between confirmation and lookup. Toss documents that retrying the identical
// POST with the same Idempotency-Key is safe (and retains that key for 15
// days). A paymentKey lookup can still return 404 immediately after an
// ambiguous POST, so a stale cleanup must repeat that original operation
// before declaring the local order failed.
//
// The DB claim is the cross-process authorization gate. The retry uses only
// the exact secret captured when the order was created, together with the same
// paymentKey/orderId/amount and the persisted idempotency key. Legacy attempts
// retain orderId; rollout-gated attempts use the exact paymentKey namespace.
func retryTossGeneralTopUpConfirmAfterLookupMiss(ctx context.Context, current *model.TopUp, paymentKey string) (auth *tossConfirmResponse, resolved bool, handled bool, err error) {
	if current == nil || !strings.HasPrefix(current.TradeNo, "toss_") {
		return nil, false, false, nil
	}
	if !isTossTopUpOperationallyEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("Toss stale top-up idempotent POST blocked by operational kill switch order_id=%s — left pending", current.TradeNo))
		return nil, false, true, nil
	}
	if isTossRecordedTopUpPastApprovalWindow(current) {
		// Toss authentication is no longer confirmable, and the provider's
		// Idempotency-Key result is retained for only 15 days. Never replay a POST
		// indefinitely after an authenticated GET proved the payment is absent.
		closed, closeErr := closeStaleTossPendingTopUp(ctx, current, common.TopUpStatusExpired, "approval_window_elapsed_payment_not_found")
		return nil, closed, true, closeErr
	}
	orderId := strings.TrimSpace(current.TradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	token, claimedTopUp, claimed, claimErr := model.ClaimPendingTossTopUpConfirmRetry(orderId, paymentKey, current.Amount)
	if claimErr != nil {
		return nil, false, true, claimErr
	}
	if !claimed {
		if claimedTopUp != nil && claimedTopUp.Status != common.TopUpStatusPending {
			return nil, true, true, nil
		}
		// Another node owns the live retry lease. Leave the order pending until
		// that owner records the provider result.
		return nil, false, true, nil
	}
	defer func() {
		if releaseErr := model.ReleasePendingTossTopUpConfirmRetry(orderId, token); releaseErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss top-up confirm retry claim release failed order_id=%s error=%q", orderId, releaseErr.Error()))
			if err == nil {
				err = releaseErr
			}
		}
	}()

	authorized, err := model.ValidatePendingTossTopUpConfirmClaim(orderId, paymentKey, current.Amount, token)
	if err != nil {
		if errors.Is(err, model.ErrTossTopUpConfirmClaimLost) {
			return nil, false, true, nil
		}
		if errors.Is(err, model.ErrTossTopUpLifecycleInactive) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss stale top-up provider POST blocked by inactive lifecycle order_id=%s error=%q", orderId, err.Error()))
			closed, closeErr := closeStaleTossPendingTopUpWithClaim(ctx, claimedTopUp, common.TopUpStatusFailed, "charge_lifecycle_inactive", token)
			return nil, closed, true, closeErr
		}
		return nil, false, true, err
	}
	secretKey, err := model.DecryptProviderCredential(authorized.ProviderCredential)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: exact Toss top-up confirmation credential cannot be decrypted order_id=%s error=%q — active secret is lookup-only", orderId, err.Error()))
		return nil, false, true, nil
	}
	if strings.TrimSpace(secretKey) == "" {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: exact Toss top-up confirmation credential is empty order_id=%s — active secret is lookup-only", orderId))
		return nil, false, true, nil
	}
	// Repeat the exact-secret snapshot update immediately before the recovery
	// POST. Besides preserving the credential namespace, the model revalidates
	// the user/organization lifecycle under the live claim.
	idempotencyKey, prepareErr := model.PrepareClaimedPendingTossTopUpConfirm(orderId, paymentKey, authorized.Amount, token, secretKey)
	if prepareErr != nil {
		if errors.Is(prepareErr, model.ErrTossTopUpConfirmClaimLost) {
			return nil, false, true, nil
		}
		if errors.Is(prepareErr, model.ErrTossTopUpLifecycleInactive) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss stale top-up provider POST blocked by lifecycle change order_id=%s error=%q", orderId, prepareErr.Error()))
			closed, closeErr := closeStaleTossPendingTopUpWithClaim(ctx, authorized, common.TopUpStatusFailed, "charge_lifecycle_changed", token)
			return nil, closed, true, closeErr
		}
		return nil, false, true, prepareErr
	}

	auth, statusCode, confirmErr := confirmTossPaymentWithSecret(ctx, paymentKey, orderId, authorized.Amount, secretKey, idempotencyKey)
	if confirmErr == nil {
		return auth, false, true, nil
	}
	outcomeUncertain := isTossAmbiguousPaymentOutcome(statusCode, confirmErr)
	credentialRejected := isTossCredentialRejection(statusCode, confirmErr)
	logger.LogWarn(ctx, fmt.Sprintf("Toss stale top-up idempotent confirm retry failed order_id=%s status=%d error=%q — re-verifying", orderId, statusCode, confirmErr.Error()))
	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	auth, verifyStatus, verifyErr := getTossPaymentWithCredentialForMID(ctx, paymentKey, authorized.ProviderCredential, authorized.ProviderClientKeyHash, activeClientKey, activeSecretKey)
	if verifyErr == nil {
		return auth, false, true, nil
	}
	if verifyStatus == http.StatusNotFound {
		if credentialRejected {
			// Toss includes the API key in its idempotency namespace. A rotated
			// same-MID key is safe for GET verification, but it must not authorize
			// a new recovery POST after the exact attempt key was rejected.
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: exact Toss top-up confirmation key rejected and same-MID lookup found no payment order_id=%s — left pending", orderId))
			return nil, false, true, nil
		}
		if outcomeUncertain {
			logger.LogWarn(ctx, fmt.Sprintf("Toss stale top-up remains invisible after ambiguous idempotent retry order_id=%s — left pending", orderId))
			return nil, false, true, nil
		}
		recoverRejectedTossConfirmBinding(ctx, authorized, paymentKey, authorized.Amount, token, "confirm_retry_terminal_rejection")
		refreshed, refreshErr := model.GetTopUpByTradeNoWithError(orderId)
		if refreshErr != nil {
			return nil, false, true, refreshErr
		}
		return nil, refreshed.Status != common.TopUpStatusPending, true, nil
	}
	return nil, false, true, verifyErr
}

func recoverRecordedTossBindingAfterIdentityMismatch(ctx context.Context, current *model.TopUp, paymentKey, reason string) (bool, error) {
	if current == nil {
		return false, model.ErrTopUpNotFound
	}
	token, claimedTopUp, claimed, err := model.ClaimPendingTossTopUpConfirmRetry(current.TradeNo, paymentKey, current.Amount)
	if err != nil {
		return false, err
	}
	if !claimed {
		return claimedTopUp != nil && claimedTopUp.Status != common.TopUpStatusPending, nil
	}
	defer func() {
		if releaseErr := model.ReleasePendingTossTopUpConfirmRetry(current.TradeNo, token); releaseErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss mismatched reconciliation claim release failed order_id=%s error=%q", current.TradeNo, releaseErr.Error()))
		}
	}()
	recoverRejectedTossConfirmBinding(ctx, claimedTopUp, paymentKey, current.Amount, token, reason)
	refreshed, err := model.GetTopUpByTradeNoWithError(current.TradeNo)
	if err != nil {
		return false, err
	}
	return refreshed.Status != common.TopUpStatusPending, nil
}

func reconcileTossConfirmWithoutProviderPOST(c *gin.Context, topUp *model.TopUp, paymentKey, orderId string, amount int64, blockReason string) {
	ctx := c.Request.Context()
	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	auth, statusCode, err := getTossPaymentWithCredentialForMID(ctx, paymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
	if err != nil {
		// A lookup miss or transient lookup failure is not proof that the payment
		// failed. Keep the recorded order pending while the kill switch is active.
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm POST blocked (%s); lookup unresolved order_id=%s status=%d error=%q", blockReason, orderId, statusCode, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if auth == nil || auth.Type != "NORMAL" {
		logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm lookup returned an unexpected payment type order_id=%s", orderId))
		tossRedirect(c, "/console/topup")
		return
	}
	if auth.PaymentKey != paymentKey || auth.OrderId != orderId {
		if auth.OrderId == orderId && strings.EqualFold(strings.TrimSpace(auth.Status), "DONE") {
			_, _ = persistTossFinancialMismatchEvent(auth, "read-only confirm lookup identity mismatch")
		}
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: read-only confirm lookup identity mismatch order_id=%s payment_key_match=%t auth_order=%s", orderId, auth.PaymentKey == paymentKey, auth.OrderId))
		tossRedirect(c, "/console/topup")
		return
	}
	if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, topUp, auth); handled || finalizeErr != nil {
		if finalizeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm refunded-order finalization failed order_id=%s error=%q", orderId, finalizeErr.Error()))
		}
		tossRedirect(c, "/console/topup")
		return
	}
	if isValidTossTopUpPayment(auth, orderId, amount) {
		// The read-only lookup has now authenticated the browser paymentKey and
		// proved that Toss captured the money. Persist that identity before local
		// settlement even while the provider POST kill switch is active. If credit
		// fails, the recorded-payment reconciler can then recover the paid order
		// instead of letting the pristine marker fall into never-approved expiry.
		if err := model.RecordTossPaymentKeyWithContext(ctx, orderId, paymentKey); err != nil {
			_ = recordTossTopUpSettlementFailure(ctx, topUp, auth, err)
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm authenticated paymentKey persist failed order_id=%s error=%q", orderId, err.Error()))
			tossRedirect(c, "/console/topup")
			return
		}
		if err := model.RechargeToss(orderId, paymentKey, c.ClientIP()); err != nil {
			_ = recordTossTopUpSettlementFailure(ctx, topUp, auth, err)
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm lookup recharge failed order_id=%s error=%q", orderId, err.Error()))
			tossRedirect(c, "/console/topup")
			return
		}
		if err := model.ResolveTossPaymentEvents(orderId, model.TossPaymentEventTypeFulfillment); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm reconciliation resolve failed order_id=%s error=%q", orderId, err.Error()))
		}
		tossRedirect(c, tossTopUpSuccessRedirectPath(topUp))
		return
	}
	if strings.EqualFold(strings.TrimSpace(auth.Status), "DONE") {
		var persistErr error
		if auth.TotalAmount == amount && strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
			_, persistErr = queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "read-only confirm DONE contract mismatch")
		} else {
			_, persistErr = persistTossFinancialMismatchEvent(auth, "read-only confirm DONE payload mismatch")
		}
		if persistErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm paid mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
		}
		tossRedirect(c, "/console/topup")
		return
	}
	if isTossTerminalFailStatus(auth.Status) {
		target := common.TopUpStatusFailed
		if auth.Status == "EXPIRED" {
			target = common.TopUpStatusExpired
		}
		_ = model.UpdatePendingTopUpStatusForMethod(orderId, model.PaymentProviderToss, model.PaymentMethodToss, target)
		tossRedirect(c, "/console/topup")
		return
	}
	if isTossCancelStatus(auth.Status) {
		if _, _, err := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, auth, "read-only confirm found an uncredited partial cancellation with provider balance remaining"); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss read-only confirm cancellation persist failed order_id=%s error=%q", orderId, err.Error()))
			tossRedirect(c, "/console/topup")
			return
		}
		if !tossTopUpCancellationNeedsRefundFence(auth) {
			_ = model.UpdatePendingTopUpStatusForMethod(orderId, model.PaymentProviderToss, model.PaymentMethodToss, common.TopUpStatusFailed)
		}
	}
	tossRedirect(c, "/console/topup")
}

func reconcileTossRecordedTopUp(ctx context.Context, topUp model.TopUp) (bool, error) {
	orderId := strings.TrimSpace(topUp.TradeNo)
	paymentKey := strings.TrimSpace(topUp.ProviderOrderId)
	if !strings.HasPrefix(orderId, "toss_") || paymentKey == "" || paymentKey == orderId {
		return false, nil
	}

	LockOrder(orderId)
	defer UnlockOrder(orderId)

	current, lookupErr := model.GetTopUpByTradeNoWithError(orderId)
	if errors.Is(lookupErr, model.ErrTopUpNotFound) {
		return true, nil
	}
	if lookupErr != nil {
		return false, lookupErr
	}
	paymentKey = strings.TrimSpace(current.ProviderOrderId)
	refundRequired, refundLookupErr := model.HasRequiredTossTopUpRefundWithContext(ctx, orderId, paymentKey)
	if refundLookupErr != nil {
		return false, refundLookupErr
	}
	if current.Status != common.TopUpStatusPending && !refundRequired {
		return true, nil
	}
	if current.Status != common.TopUpStatusPending && current.Status != model.TossTopUpStatusRefundPending &&
		current.Status != common.TopUpStatusFailed && current.Status != common.TopUpStatusExpired {
		return false, model.ErrTopUpStatusInvalid
	}
	// ReconcileStaleTossRecordedTopUps reserves a precise snapshot before this
	// callback. A browser callback or another master can advance the scheduling
	// marker before we reload; never act on the stale provider observation.
	if current.ProviderOrderTime != topUp.ProviderOrderTime || current.ProviderRetryTime != topUp.ProviderRetryTime {
		return false, nil
	}
	if current.PaymentProvider != model.PaymentProviderToss || current.PaymentMethod != model.PaymentMethodToss {
		return false, model.ErrPaymentMethodMismatch
	}
	if paymentKey == "" || paymentKey == current.TradeNo {
		return false, nil
	}

	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	auth, statusCode, err := getTossPaymentWithCredentialForMID(ctx, paymentKey, current.ProviderCredential, current.ProviderClientKeyHash, activeClientKey, activeSecretKey)
	if err != nil {
		if refundRequired {
			// A provider lookup miss is not evidence that a previously observed
			// payment has no balance. Preserve the terminal refund work item and
			// retry the exact paymentKey instead of invoking pending-confirm repair.
			return false, err
		}
		if statusCode == http.StatusNotFound {
			retriedAuth, resolved, handled, retryErr := retryTossGeneralTopUpConfirmAfterLookupMiss(ctx, current, paymentKey)
			if retryErr != nil || resolved {
				return resolved, retryErr
			}
			if handled {
				if retriedAuth == nil {
					return false, nil
				}
				auth = retriedAuth
			} else {
				return closeStaleTossPendingTopUp(ctx, current, common.TopUpStatusFailed, "payment_not_found")
			}
		} else {
			return false, err
		}
	}
	if auth == nil {
		return false, errors.New("Toss top-up reconciliation payment is empty")
	}
	if auth.Type != "NORMAL" {
		return false, errors.New("Toss top-up reconciliation returned an unexpected payment type")
	}
	if auth.PaymentKey != paymentKey {
		return recoverRecordedTossBindingAfterIdentityMismatch(ctx, current, paymentKey, "reconciliation_payment_key_mismatch")
	}
	if auth.OrderId != orderId {
		return recoverRecordedTossBindingAfterIdentityMismatch(ctx, current, paymentKey, "reconciliation_order_mismatch")
	}
	contractMismatch := auth.TotalAmount == current.Amount &&
		strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") &&
		!hasSupportedTossTopUpFundingSource(auth)
	if !refundRequired && contractMismatch &&
		(auth.Status == "DONE" || auth.Status == "WAITING_FOR_DEPOSIT") {
		if _, err := queueTossTopUpRefundRequirementWithContext(ctx, current, auth, "authenticated top-up contract mismatch"); err != nil {
			return false, err
		}
		refundRequired = true
	}
	if refundRequired {
		if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, current, auth); handled || finalizeErr != nil {
			return handled, finalizeErr
		}
		if err := cancelRequiredTossTopUp(ctx, current, auth); err != nil {
			if errors.Is(err, errTossTopUpVirtualAccountRefundPending) {
				logger.LogInfo(ctx, fmt.Sprintf("Toss virtual-account bank refund remains pending order_id=%s", orderId))
				return false, nil
			}
			if errors.Is(err, errTossTopUpRefundAccountRequired) || errors.Is(err, errTossTopUpVirtualAccountRefundUnproved) {
				retryAt := model.GetDBTimestamp() + int64(tossTopUpRefundManualRetryDelay/time.Second)
				if deferErr := model.DeferPendingTossTopUpProviderRetryWithContext(ctx, orderId, paymentKey, retryAt); deferErr != nil {
					return false, errors.Join(err, deferErr)
				}
				logger.LogError(ctx, fmt.Sprintf("TOSS MANUAL REFUND REQUIRED: virtual-account refund completion is not proven order_id=%s", orderId))
				return false, nil
			}
			return false, err
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss uncredited top-up automatically refunded order_id=%s", orderId))
		return true, nil
	}
	if auth.Status == "DONE" {
		if isValidTossTopUpPayment(auth, orderId, current.Amount) {
			if err := model.RechargeToss(orderId, auth.PaymentKey, "toss-pending-cleanup"); err != nil {
				if errors.Is(err, model.ErrTopUpQuotaCapacityExceeded) {
					if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, current, auth, "quota capacity prevented local fulfillment"); queueErr != nil {
						return false, errors.Join(err, queueErr)
					}
					if refundErr := cancelRequiredTossTopUp(ctx, current, auth); refundErr != nil {
						return false, refundErr
					}
					return true, nil
				}
				_, _ = persistTossFulfillmentEvent(auth)
				return false, err
			}
			if err := model.ResolveTossPaymentEvents(orderId, model.TossPaymentEventTypeFulfillment); err != nil {
				return false, err
			}
			logger.LogInfo(ctx, fmt.Sprintf("Toss stale pending top-up credited order_id=%s", orderId))
			return true, nil
		}
		if _, err := persistTossFinancialMismatchEvent(auth, "stale top-up DONE payload mismatch"); err != nil {
			return false, err
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss stale pending top-up DONE rejected order_id=%s total=%d currency=%s card_present=%t easy_pay_present=%t", orderId, auth.TotalAmount, auth.Currency, auth.Card != nil, auth.EasyPay != nil))
		// Amount/currency/provider-identity mismatches are not safe to cancel
		// automatically. Preserve the pending row and exact credential for manual
		// reconciliation instead of removing it from every recovery queue.
		return false, nil
	}
	if isTossTerminalFailStatus(auth.Status) || isTossCancelStatus(auth.Status) {
		target := common.TopUpStatusFailed
		if auth.Status == "EXPIRED" {
			target = common.TopUpStatusExpired
		}
		if isTossCancelStatus(auth.Status) {
			created, canceledAmount, persistErr := persistUncreditedTossTopUpCancellationWithContext(ctx, current, auth, "stale uncredited top-up was partially canceled with provider balance remaining")
			if persistErr != nil {
				return false, persistErr
			}
			paymentMethod := strings.TrimSpace(current.PaymentMethod)
			if paymentMethod == "" {
				paymentMethod = model.PaymentMethodToss
			}
			if created > 0 {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: %s on uncredited stale pending order order_id=%s user_id=%d canceled=%d KRW balance=%d KRW", auth.Status, orderId, current.UserId, canceledAmount, auth.BalanceAmount))
				model.RecordTopupLog(current.UserId, fmt.Sprintf("Toss payment %s before local credit (canceled: %d KRW, balance: %d KRW) — manual quota reconciliation required", auth.Status, canceledAmount, auth.BalanceAmount), "toss-pending-cleanup", paymentMethod, "toss-cancel")
			}
			if tossTopUpCancellationNeedsRefundFence(auth) {
				return false, nil
			}
		}
		return closeStaleTossPendingTopUp(ctx, current, target, strings.ToLower(auth.Status))
	}
	if !isTossRecordedTopUpPastApprovalWindow(current) {
		return false, nil
	}
	return closeStaleTossPendingTopUp(ctx, current, common.TopUpStatusExpired, "approval_window_elapsed_"+strings.ToLower(auth.Status))
}

// TossConfirm handles the successUrl redirect: ?paymentKey&orderId&amount.
func TossConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	paymentKey := c.Query("paymentKey")
	orderId := c.Query("orderId")
	amountStr := c.Query("amount")

	if !isValidTossPaymentKey(paymentKey) || !isValidTossOrderId(orderId) || amountStr == "" || len(amountStr) > 20 {
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

	topUp, lookupErr := model.GetTopUpByTradeNoWithError(orderId)
	if lookupErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm local order lookup failed order_id=%s error=%q", orderId, lookupErr.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if _, isWalletAutoRecharge, walletLookupErr := model.GetWalletAutoRechargeByChargeTradeNo(orderId); walletLookupErr != nil || isWalletAutoRecharge {
		// The browser success callback is only for NORMAL checkout confirmation.
		// A server-initiated BILLING order (including an opaque row whose tuple was
		// lost during restore) must stay on the wallet GET/settlement path and must
		// never accept a browser-supplied paymentKey.
		if walletLookupErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss confirm wallet order classification failed order_id=%s error=%q", orderId, walletLookupErr.Error()))
		} else {
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm rejected wallet auto recharge order_id=%s", orderId))
		}
		tossRedirect(c, "/console/topup")
		return
	}
	if err := validateTossConfirm(topUp, orderId, amount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed error=%q client_ip=%s", err.Error(), c.ClientIP()))
		tossRedirect(c, "/console/topup")
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		tossRedirect(c, tossTopUpSuccessRedirectPath(topUp))
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm abnormal status order_id=%s status=%q", orderId, topUp.Status))
		tossRedirect(c, "/console/topup")
		return
	}
	if !isTossTopUpOperationallyEnabled() {
		// The successUrl is browser-controlled. While the POST kill switch is
		// active, perform only authoritative read-only reconciliation and do not
		// let an unverified paymentKey replace the pristine order marker.
		reconcileTossConfirmWithoutProviderPOST(c, topUp, paymentKey, orderId, amount, "operational kill switch")
		return
	}
	if isTossConfirmProviderPostWindowElapsed(topUp) {
		// Cleanup is an operational convenience, not the authorization boundary.
		// Even if that worker did not run, an old browser callback is lookup-only:
		// an already-DONE payment can still settle, but no new /confirm operation
		// may be created after the local/provider approval window has elapsed.
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm provider POST blocked by elapsed local confirmation window order_id=%s", orderId))
		reconcileTossConfirmWithoutProviderPOST(c, topUp, paymentKey, orderId, amount, "local confirmation window elapsed")
		return
	}
	// Persist the paymentKey BEFORE approving the payment. This MUST succeed: the stale-pending
	// sweep treats provider_order_id == trade_no as "never approved" and expires such orders, so
	// approving without first recording the paymentKey could let a real paid order be wrongly
	// expired with no credit.
	if err := model.RecordTossPaymentKey(orderId, paymentKey); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss record paymentKey (pre-confirm) failed order_id=%s error=%q — aborting confirm", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	claimToken, claimedTopUp, claimed, claimErr := model.ClaimPendingTossTopUpConfirmRetry(orderId, paymentKey, amount)
	if claimErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm local authorization failed order_id=%s error=%q", orderId, claimErr.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if !claimed {
		if claimedTopUp != nil && claimedTopUp.Status == common.TopUpStatusSuccess {
			tossRedirect(c, tossTopUpSuccessRedirectPath(claimedTopUp))
			return
		}
		// A different node already owns the exact confirmation attempt. It uses
		// the same provider idempotency key and will settle the local order; this
		// callback must not create another independently authorized POST.
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm already claimed by another worker order_id=%s — left pending", orderId))
		tossRedirect(c, "/console/topup")
		return
	}
	defer func() {
		if releaseErr := model.ReleasePendingTossTopUpConfirmRetry(orderId, claimToken); releaseErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss confirm claim release failed order_id=%s error=%q", orderId, releaseErr.Error()))
		}
	}()
	topUp, claimErr = model.ValidatePendingTossTopUpConfirmClaim(orderId, paymentKey, amount, claimToken)
	if claimErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm authorization changed before provider POST order_id=%s error=%q", orderId, claimErr.Error()))
		if errors.Is(claimErr, model.ErrTossTopUpLifecycleInactive) {
			// A v1 checkout that has not crossed ProviderAttempted is provably
			// pre-POST even across restarts. Remove the browser-supplied key before
			// returning so a forged callback cannot poison an order merely because
			// its owner/organization became inactive. Legacy rows fail this reset
			// closed and retain read-only provider reconciliation below.
			if model.IsPristinePaymentKeyScopedTossTopUp(claimedTopUp) {
				reset, resetErr := model.ResetClaimedPendingTossTopUpPaymentBinding(orderId, paymentKey, amount, claimToken)
				if resetErr == nil && reset {
					logger.LogWarn(ctx, fmt.Sprintf("Toss inactive-lifecycle pre-POST paymentKey binding restored order_id=%s", orderId))
					tossRedirect(c, "/console/topup")
					return
				}
				if resetErr != nil && !errors.Is(resetErr, model.ErrTossTopUpBindingResetLegacy) && !errors.Is(resetErr, model.ErrTossTopUpConfirmClaimLost) {
					logger.LogError(ctx, fmt.Sprintf("Toss inactive-lifecycle binding restore failed order_id=%s error=%q", orderId, resetErr.Error()))
				}
			}
			// Lifecycle changes prohibit a new charge, but a prior/parallel attempt
			// may already be DONE. Read-only verification and settlement remain safe.
			reconcileTossConfirmWithoutProviderPOST(c, claimedTopUp, paymentKey, orderId, amount, "charge lifecycle inactive")
			return
		}
		tossRedirect(c, "/console/topup")
		return
	}

	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	result, statusCode, err := confirmTossPaymentWithCredentialForMID(ctx, paymentKey, orderId, amount, claimToken, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
	if err != nil {
		outcomeUncertain := isTossAmbiguousPaymentOutcome(statusCode, err)
		// Don't infer terminal-vs-transient from the HTTP status (e.g. 409 IDEMPOTENT_REQUEST_PROCESSING
		// is transient, not a rejection). Ask Toss authoritatively what actually happened.
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm API failed order_id=%s status=%d error=%q — re-verifying", orderId, statusCode, err.Error()))
		auth, verifyStatus, gerr := getTossPaymentWithCredentialForMID(ctx, paymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
		if gerr != nil {
			if verifyStatus == http.StatusNotFound {
				if outcomeUncertain {
					// A timeout or IDEMPOTENT_REQUEST_PROCESSING response means the original
					// POST can still finish after this immediate lookup. Keep the order
					// pending so the same idempotent request or the stale reconciler can
					// establish the final provider state.
					logger.LogWarn(ctx, fmt.Sprintf("Toss confirm verify: payment not visible yet after uncertain response order_id=%s — left pending", orderId))
					tossRedirect(c, "/console/topup")
					return
				}
				// A terminal request rejection plus a definitive lookup miss proves
				// this paymentKey cannot move money. Remove a scoped forged binding;
				// legacy attempts remain terminal because their orderId namespace may
				// already have been consumed.
				recoverRejectedTossConfirmBinding(ctx, topUp, paymentKey, amount, claimToken, "terminal_rejection_not_found")
				tossRedirect(c, "/console/topup")
				return
			}
			// Network / 5xx / other → transient; leave pending for the webhook/retry to resolve.
			logger.LogError(ctx, fmt.Sprintf("Toss confirm verify failed (transient) order_id=%s status=%d error=%q", orderId, verifyStatus, gerr.Error()))
			tossRedirect(c, "/console/topup")
			return
		}
		if auth == nil || auth.Type != "NORMAL" {
			logger.LogError(ctx, fmt.Sprintf("Toss confirm verification returned an unexpected payment type order_id=%s", orderId))
			tossRedirect(c, "/console/topup")
			return
		}
		if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, topUp, auth); handled || finalizeErr != nil {
			if finalizeErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm-verify refunded-order finalization failed order_id=%s error=%q", orderId, finalizeErr.Error()))
			}
			tossRedirect(c, "/console/topup")
			return
		}
		switch {
		case auth.OrderId != orderId:
			// Authenticated lookup proves this key belongs elsewhere. A scoped
			// attempt can safely restore the pristine checkout for the real key.
			recoverRejectedTossConfirmBinding(ctx, topUp, paymentKey, amount, claimToken, "authoritative_order_mismatch")
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm verify: order mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
			tossRedirect(c, "/console/topup")
			return
		case isValidTossTopUpPayment(auth, orderId, amount):
			// Actually approved (confirm response lost / concurrent idempotent confirm). Credit it.
			if rerr := model.RechargeToss(orderId, auth.PaymentKey, c.ClientIP()); rerr != nil {
				_ = recordTossTopUpSettlementFailure(ctx, topUp, auth, rerr)
				logger.LogError(ctx, fmt.Sprintf("Toss confirm-verify recharge failed order_id=%s error=%q", orderId, rerr.Error()))
				tossRedirect(c, "/console/topup")
				return
			}
			if resolveErr := model.ResolveTossPaymentEvents(orderId, model.TossPaymentEventTypeFulfillment); resolveErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm-verify reconciliation resolve failed order_id=%s error=%q", orderId, resolveErr.Error()))
			}
			logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded (confirm-verify) order_id=%s", orderId))
			tossRedirect(c, tossTopUpSuccessRedirectPath(topUp))
			return
		case strings.EqualFold(strings.TrimSpace(auth.Status), "DONE"):
			var persistErr error
			if auth.TotalAmount == amount && strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
				_, persistErr = queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "confirm verification DONE contract mismatch")
			} else {
				_, persistErr = persistTossFinancialMismatchEvent(auth, "confirm verification DONE payload mismatch")
			}
			if persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm-verify paid mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: payment DONE payload mismatch order_id=%s expected_amount=%d actual_amount=%d currency=%s card_present=%t easy_pay_present=%t", orderId, amount, auth.TotalAmount, auth.Currency, auth.Card != nil, auth.EasyPay != nil))
			tossRedirect(c, "/console/topup")
			return
		case isTossTerminalFailStatus(auth.Status):
			// EXPIRED / ABORTED → close so it isn't stranded pending.
			target := common.TopUpStatusFailed
			if auth.Status == "EXPIRED" {
				target = common.TopUpStatusExpired
			}
			_ = model.UpdatePendingTopUpStatusForMethod(orderId, model.PaymentProviderToss, model.PaymentMethodToss, target)
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm failed, authoritative %s order_id=%s — closed", auth.Status, orderId))
			tossRedirect(c, "/console/topup")
			return
		case isTossCancelStatus(auth.Status):
			if _, _, persistErr := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, auth, "confirm verification found an uncredited partial cancellation with provider balance remaining"); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				tossRedirect(c, "/console/topup")
				return
			}
			if !tossTopUpCancellationNeedsRefundFence(auth) {
				_ = model.UpdatePendingTopUpStatusForMethod(orderId, model.PaymentProviderToss, model.PaymentMethodToss, common.TopUpStatusFailed)
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm resolved as %s order_id=%s — reconciliation recorded", auth.Status, orderId))
			tossRedirect(c, "/console/topup")
			return
		default:
			// READY / IN_PROGRESS (incl. the HTTP 409 idempotent-processing case) — leave
			// pending; the PAYMENT_STATUS_CHANGED webhook resolves it.
			logger.LogWarn(ctx, fmt.Sprintf("Toss confirm failed, authoritative status=%s order_id=%s — left pending", auth.Status, orderId))
			tossRedirect(c, "/console/topup")
			return
		}
	}
	if result == nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm returned an empty successful response order_id=%s", orderId))
		tossRedirect(c, "/console/topup")
		return
	}
	if result.OrderId != orderId || result.PaymentKey != paymentKey {
		paidEvidenceForLocalOrder := result.Status == "DONE" && result.OrderId == orderId
		if paidEvidenceForLocalOrder {
			if _, persistErr := persistTossFinancialMismatchEvent(result, "confirm response identity mismatch"); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm paid identity mismatch persist failed order_id=%s response_order=%s error=%q", orderId, result.OrderId, persistErr.Error()))
			}
		}
		if !paidEvidenceForLocalOrder {
			// A 2xx response is authenticated provider evidence. If it belongs to
			// another order (or is a non-paid response for another key), this
			// browser-supplied binding cannot represent local money movement.
			recoverRejectedTossConfirmBinding(ctx, topUp, paymentKey, amount, claimToken, "successful_response_identity_mismatch")
		} else {
			// A different DONE paymentKey for this order is financially meaningful
			// evidence. Preserve both the binding and event for transaction/manual
			// reconciliation rather than erasing either side of the conflict.
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: confirm returned a different DONE paymentKey for local order order_id=%s", orderId))
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm identity mismatch order_id=%s response_order=%s payment_key_match=%t", orderId, result.OrderId, result.PaymentKey == paymentKey))
		tossRedirect(c, "/console/topup")
		return
	}
	if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, topUp, result); handled || finalizeErr != nil {
		if finalizeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss confirm refunded-order finalization failed order_id=%s error=%q", orderId, finalizeErr.Error()))
		}
		tossRedirect(c, "/console/topup")
		return
	}
	if !isValidTossTopUpPayment(result, orderId, amount) {
		exactRefundIdentity := result.TotalAmount == amount &&
			strings.EqualFold(strings.TrimSpace(result.Currency), "KRW")
		shouldQueueRefund := exactRefundIdentity &&
			(result.Status == "DONE" || result.Status == "WAITING_FOR_DEPOSIT") &&
			(result.Status == "DONE" || !hasSupportedTossTopUpFundingSource(result))
		if shouldQueueRefund {
			if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, topUp, result, "confirm response cannot be safely fulfilled"); queueErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm automatic-refund queue failed order_id=%s error=%q", orderId, queueErr.Error()))
			} else {
				logger.LogWarn(ctx, fmt.Sprintf("Toss confirm queued uncredited payment for automatic full refund order_id=%s method=%s status=%s", orderId, result.Method, result.Status))
			}
			tossRedirect(c, "/console/topup")
			return
		}
		switch {
		case result.Status == "DONE":
			if _, persistErr := persistTossFinancialMismatchEvent(result, "confirm response DONE payload mismatch"); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm paid mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
			}
		case isTossTerminalFailStatus(result.Status):
			target := common.TopUpStatusFailed
			if result.Status == "EXPIRED" {
				target = common.TopUpStatusExpired
			}
			if _, closeErr := closeStaleTossPendingTopUpWithClaim(ctx, topUp, target, "successful_response_"+strings.ToLower(result.Status), claimToken); closeErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm terminal response close failed order_id=%s error=%q", orderId, closeErr.Error()))
			}
		case isTossCancelStatus(result.Status):
			if _, _, persistErr := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, result, "confirm response found an uncredited partial cancellation with provider balance remaining"); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss confirm successful cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
			} else if !tossTopUpCancellationNeedsRefundFence(result) {
				if _, closeErr := closeStaleTossPendingTopUpWithClaim(ctx, topUp, common.TopUpStatusFailed, "successful_response_"+strings.ToLower(result.Status), claimToken); closeErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss confirm successful cancellation close failed order_id=%s error=%q", orderId, closeErr.Error()))
				}
			}
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm validation failed order_id=%s status=%s total=%d currency=%s card_present=%t easy_pay_present=%t", orderId, result.Status, result.TotalAmount, result.Currency, result.Card != nil, result.EasyPay != nil))
		tossRedirect(c, "/console/topup")
		return
	}

	if err := model.RechargeToss(orderId, result.PaymentKey, c.ClientIP()); err != nil {
		_ = recordTossTopUpSettlementFailure(ctx, topUp, result, err)
		logger.LogError(ctx, fmt.Sprintf("Toss recharge failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if resolveErr := model.ResolveTossPaymentEvents(orderId, model.TossPaymentEventTypeFulfillment); resolveErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss reconciliation resolve failed order_id=%s error=%q", orderId, resolveErr.Error()))
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded order_id=%s", orderId))
	tossRedirect(c, tossTopUpSuccessRedirectPath(topUp))
}

// TossFail handles the failUrl redirect.
func TossFail(c *gin.Context) {
	ctx := c.Request.Context()
	orderId := c.Query("orderId")
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(ctx, fmt.Sprintf("Toss payment failed order_id=%q code=%s client_ip=%s", trimTossFailureValue(orderId), sanitizeTossAPIErrorCode(code), c.ClientIP()))
	// failUrl is a browser redirect and is not an authoritative provider result.
	// Do not close the order here: a forged/duplicated fail redirect can race a
	// real success callback and otherwise turn a paid order into failed/no-credit.
	// Never-started card orders are expired by the stale-pending sweep.
	tossRedirect(c, tossTopUpFailureRedirectPath(ctx, orderId, code, message))
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

// shouldRetryTossCancellationWebhook distinguishes an untrusted unrelated
// webhook from a legitimate cancellation notification whose authoritative
// Payment view has not caught up yet. Once the authenticated GET matches both
// the local order and webhook paymentKey, acknowledging a less-final status
// would suppress Toss's retries and could permanently lose the refund event.
func shouldRetryTossCancellationWebhook(claimedStatus string, auth *tossConfirmResponse, orderId, paymentKey string) bool {
	claimedStatus = strings.ToUpper(strings.TrimSpace(claimedStatus))
	if !isTossCancelStatus(claimedStatus) || auth == nil {
		return false
	}
	orderId = strings.TrimSpace(orderId)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderId == "" || paymentKey == "" || strings.TrimSpace(auth.OrderId) != orderId || strings.TrimSpace(auth.PaymentKey) != paymentKey {
		return false
	}
	authoritativeStatus := strings.ToUpper(strings.TrimSpace(auth.Status))
	if !isTossCancelStatus(authoritativeStatus) {
		return true
	}
	// A final CANCELED webhook can precede the GET replica's transition from
	// PARTIAL_CANCELED. Keep delivery alive until that more-final state is visible.
	return claimedStatus == "CANCELED" && authoritativeStatus == "PARTIAL_CANCELED"
}

// shouldRetryTossDoneWebhook preserves a final payment notification while the
// authoritative Payment read is still on a pre-approval replica. This is
// intentionally limited to READY/IN_PROGRESS. A virtual-account payment can
// legitimately move from DONE back to WAITING_FOR_DEPOSIT after a deposit
// error, so that documented non-monotonic state must not be treated as mere
// read lag.
func shouldRetryTossDoneWebhook(claimedStatus string, auth *tossConfirmResponse, orderId, paymentKey string) bool {
	if strings.ToUpper(strings.TrimSpace(claimedStatus)) != "DONE" || auth == nil {
		return false
	}
	orderId = strings.TrimSpace(orderId)
	paymentKey = strings.TrimSpace(paymentKey)
	if orderId == "" || paymentKey == "" ||
		strings.TrimSpace(auth.OrderId) != orderId || strings.TrimSpace(auth.PaymentKey) != paymentKey {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(auth.Status)) {
	case "READY", "IN_PROGRESS":
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

func handleAuthoritativeTossSubscriptionCancellation(c *gin.Context, order *model.SubscriptionOrder, auth *tossConfirmResponse) bool {
	if order == nil || auth == nil || !isTossCancelStatus(auth.Status) {
		return false
	}
	ctx := c.Request.Context()
	expectedAmount := order.ProviderAmount
	if expectedAmount <= 0 {
		plan, planErr := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order)
		if planErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription cancellation plan lookup failed order_id=%s error=%q", order.TradeNo, planErr.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return true
		}
		expectedAmount = tossSubscriptionOrderChargeKRW(order, plan)
	}
	if !model.IsTossCardAmountPayableKRW(expectedAmount) || !isValidTossCardCancellation(auth, order.TradeNo, expectedAmount) {
		logger.LogError(ctx, fmt.Sprintf("Toss subscription cancellation mismatch order_id=%s expected=%d actual=%d currency=%s card_present=%t", order.TradeNo, expectedAmount, auth.TotalAmount, auth.Currency, auth.Card != nil))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return true
	}
	created, canceledAmount, persistErr := persistExpectedTossCancellationEvents(ctx, auth, order.TradeNo, expectedAmount)
	if persistErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss subscription cancellation persist failed order_id=%s error=%q", order.TradeNo, persistErr.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return true
	}
	payload, marshalErr := common.Marshal(auth)
	if marshalErr != nil {
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if stopErr := model.StopTossSubscriptionBillingAfterCancellationWithContext(ctx, order.TradeNo, string(payload)); stopErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss subscription cancellation billing stop failed order_id=%s error=%q", order.TradeNo, stopErr.Error()))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	paymentMethod := strings.TrimSpace(order.PaymentMethod)
	if paymentMethod == "" {
		paymentMethod = "toss"
	}
	if created > 0 {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment %s order_id=%s user_id=%d newly_canceled=%d KRW balance=%d KRW", auth.Status, order.TradeNo, order.UserId, canceledAmount, auth.BalanceAmount))
		model.RecordTopupLogWithContext(ctx, order.UserId, fmt.Sprintf("Toss subscription payment %s (canceled: %d KRW, balance: %d KRW) - manual subscription/quota reconciliation required", auth.Status, canceledAmount, auth.BalanceAmount), c.ClientIP(), paymentMethod, "toss-subscription-cancel")
	}
	c.Status(http.StatusOK)
	return true
}

func handleTossSubscriptionPaymentWebhook(c *gin.Context, orderId, paymentKey, status string, isCancel bool) bool {
	ctx := c.Request.Context()
	order, lookupErr := model.GetSubscriptionOrderByTradeNoWithErrorContext(ctx, orderId)
	if errors.Is(lookupErr, model.ErrSubscriptionOrderNotFound) {
		return false
	}
	if lookupErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss subscription webhook order lookup failed order_id=%s error=%q", orderId, lookupErr.Error()))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	if order.PaymentProvider != model.PaymentProviderToss {
		return false
	}
	if !isCancel {
		if status != "DONE" {
			c.Status(http.StatusOK)
			return true
		}
		if order.Status == common.TopUpStatusSuccess {
			if paymentKey == "" {
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			auth, _, verifyErr := getTossSubscriptionPaymentForWebhook(ctx, paymentKey, order)
			if verifyErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss successful subscription stale-DONE verification failed order_id=%s error=%q", orderId, verifyErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			if auth == nil || auth.OrderId != orderId || auth.PaymentKey != paymentKey {
				c.Status(http.StatusOK)
				return true
			}
			if handleAuthoritativeTossSubscriptionCancellation(c, order, auth) {
				return true
			}
			if auth.Status != "DONE" {
				c.Status(http.StatusOK)
				return true
			}
			expectedAmount := order.ProviderAmount
			if expectedAmount <= 0 {
				if plan, planErr := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order); planErr == nil {
					expectedAmount = tossSubscriptionOrderChargeKRW(order, plan)
				}
			}
			if expectedAmount <= 0 || !isValidTossBillingCharge(auth, orderId, expectedAmount) {
				if _, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "successful subscription webhook DONE payload mismatch"); persistErr != nil {
					c.Status(http.StatusServiceUnavailable)
					return true
				}
				c.Status(http.StatusOK)
				return true
			}
			if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss subscription webhook reconciliation resolve failed order_id=%s error=%q", orderId, err.Error()))
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			c.Status(http.StatusOK)
			return true
		}
		if order.Status != common.TopUpStatusPending {
			// A terminal local redirect/cleanup can race a provider approval. The
			// webhook status alone is not authoritative, so re-fetch with the exact
			// order MID and durably record any paid-but-unfulfilled charge. Never
			// reactivate a locally closed order automatically.
			if paymentKey == "" {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: closed subscription DONE webhook missing paymentKey order_id=%s local_status=%s", orderId, order.Status))
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			auth, _, verifyErr := getTossSubscriptionPaymentForWebhook(ctx, paymentKey, order)
			if verifyErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss closed subscription DONE webhook verification failed order_id=%s local_status=%s error=%q", orderId, order.Status, verifyErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			if auth != nil && auth.OrderId == orderId && auth.PaymentKey == paymentKey && handleAuthoritativeTossSubscriptionCancellation(c, order, auth) {
				return true
			}
			if auth != nil && auth.OrderId == orderId && auth.PaymentKey == paymentKey && strings.EqualFold(strings.TrimSpace(auth.Status), "DONE") {
				expectedAmount := order.ProviderAmount
				if expectedAmount <= 0 {
					if plan, planErr := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order); planErr == nil {
						expectedAmount = tossSubscriptionOrderChargeKRW(order, plan)
					}
				}
				var persistErr error
				if expectedAmount > 0 && isValidTossBillingCharge(auth, orderId, expectedAmount) {
					_, persistErr = persistTossFulfillmentEventWithContext(ctx, auth)
				} else {
					_, persistErr = persistTossFinancialMismatchEventWithContext(ctx, auth, "closed subscription DONE payload mismatch")
				}
				if persistErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss closed subscription paid event persist failed order_id=%s error=%q", orderId, persistErr.Error()))
					c.Status(http.StatusServiceUnavailable)
					return true
				}
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: subscription payment approved after local order closed order_id=%s user_id=%d local_status=%s amount=%d", orderId, order.UserId, order.Status, auth.TotalAmount))
				model.RecordLogWithContext(ctx, order.UserId, model.LogTypeTopup, fmt.Sprintf("Toss 구독 결제 승인 후 로컬 주문이 이미 종료됨 — 수동 구독 정산 필요 (trade_no=%s, status=%s, amount=%d)", orderId, order.Status, auth.TotalAmount))
			}
			c.Status(http.StatusOK)
			return true
		}
		if paymentKey == "" {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook missing paymentKey order_id=%s status=%s", orderId, status))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		plan, err := model.ResolveTossSubscriptionOrderPlanWithContext(ctx, order)
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
		auth, _, err := getTossSubscriptionPaymentForWebhook(ctx, paymentKey, order)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription DONE webhook get payment failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		if auth == nil || auth.OrderId != orderId || auth.PaymentKey != paymentKey {
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook identity mismatch order_id=%s", orderId))
			c.Status(http.StatusOK)
			return true
		}
		if auth != nil && auth.OrderId == orderId && auth.PaymentKey == paymentKey && handleAuthoritativeTossSubscriptionCancellation(c, order, auth) {
			return true
		}
		if !isValidTossBillingCharge(auth, orderId, chargeKRW) {
			if auth != nil && auth.Status == "DONE" {
				if _, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "subscription webhook DONE payload mismatch"); persistErr != nil {
					c.Status(http.StatusServiceUnavailable)
					return true
				}
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss subscription DONE webhook verification failed order_id=%s auth_order=%s status=%s amount=%d", orderId, auth.OrderId, auth.Status, auth.TotalAmount))
			c.Status(http.StatusOK)
			return true
		}
		payload, marshalErr := common.Marshal(auth)
		if marshalErr != nil {
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		if order.BillingKeyId <= 0 {
			_, _ = persistTossFulfillmentEventWithContext(ctx, auth)
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION: paid subscription webhook has no billing key order_id=%s user_id=%d", orderId, order.UserId))
			c.Status(http.StatusOK)
			return true
		}
		renewalSubID, _, _, isRenewalOrder, identityErr := model.ResolveTossRenewalOrderIdentity(order)
		if identityErr != nil {
			if _, persistErr := persistTossFulfillmentEventWithContext(ctx, auth); persistErr != nil {
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: paid renewal association is invalid order_id=%s user_id=%d error=%q", orderId, order.UserId, identityErr.Error()))
			c.Status(http.StatusOK)
			return true
		}
		if (strings.HasPrefix(orderId, model.TossRenewalTradeNoPrefix) || model.HasTossRenewalOpaqueOrderIDPrefix(orderId)) && !isRenewalOrder {
			if _, persistErr := persistTossFulfillmentEventWithContext(ctx, auth); persistErr != nil {
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: paid renewal webhook has invalid order id order_id=%s user_id=%d", orderId, order.UserId))
			c.Status(http.StatusOK)
			return true
		}
		if isRenewalOrder {
			if err := model.RenewTossSubscriptionWithContext(ctx, renewalSubID, orderId, order.Money, chargeKRW, string(payload)); err != nil {
				if _, persistErr := persistTossFulfillmentEventWithContext(ctx, auth); persistErr != nil {
					c.Status(http.StatusServiceUnavailable)
					return true
				}
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: Toss renewal DONE webhook settlement failed order_id=%s sub_id=%d error=%q", orderId, renewalSubID, err.Error()))
				if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
					errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
					errors.Is(err, model.ErrPaymentMethodMismatch) {
					c.Status(http.StatusOK)
				} else {
					c.Status(http.StatusServiceUnavailable)
				}
				return true
			}
			if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
				c.Status(http.StatusServiceUnavailable)
				return true
			}
			c.Status(http.StatusOK)
			return true
		}
		if err := model.CompleteTossBillingOrderWithContext(ctx, orderId, order.BillingKeyId, string(payload)); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss subscription DONE webhook complete failed order_id=%s error=%q", orderId, err.Error()))
			if errors.Is(err, model.ErrSubscriptionOrderNotFound) ||
				errors.Is(err, model.ErrSubscriptionOrderStatusInvalid) ||
				errors.Is(err, model.ErrPaymentMethodMismatch) {
				c.Status(http.StatusOK)
			} else {
				handleTossBillingActivationFailure(ctx, order.UserId, orderId, order.BillingKeyId, chargeKRW, auth, string(payload), err, false)
				c.Status(http.StatusServiceUnavailable)
			}
			return true
		}
		if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
			c.Status(http.StatusServiceUnavailable)
			return true
		}
		c.Status(http.StatusOK)
		return true
	}
	if paymentKey == "" {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION: subscription cancel webhook missing paymentKey order_id=%s status=%s", orderId, status))
		c.Status(http.StatusServiceUnavailable)
		return true
	}
	auth, _, err := getTossSubscriptionPaymentForWebhook(ctx, paymentKey, order)
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
	if strings.TrimSpace(auth.PaymentKey) != paymentKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription webhook paymentKey mismatch order_id=%s", orderId))
		c.Status(http.StatusOK)
		return true
	}
	if shouldRetryTossCancellationWebhook(status, auth, orderId, paymentKey) {
		if isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
			if _, _, persistErr := persistTossCancellationEventsWithContext(ctx, auth); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss lagging subscription cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return true
			}
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss subscription cancellation not yet visible in authoritative payment order_id=%s auth_status=%s", orderId, auth.Status))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return true
	}
	if handleAuthoritativeTossSubscriptionCancellation(c, order, auth) {
		return true
	}
	c.Status(http.StatusOK)
	return true
}

func handleTossWalletAutoRechargePaymentWebhook(c *gin.Context, policy *model.WalletAutoRecharge, topUp *model.TopUp, paymentKey, claimedStatus string) {
	ctx := c.Request.Context()
	orderId := strings.TrimSpace(topUp.TradeNo)
	paymentKey = strings.TrimSpace(paymentKey)
	if paymentKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss wallet auto recharge webhook missing paymentKey order_id=%s", orderId))
		c.Status(http.StatusServiceUnavailable)
		return
	}

	// Wallet recurring charges are created in the billing MID namespace. Use
	// the per-attempt encrypted billing secret first and only fall back according
	// to the common credential-rejection policy.
	activeBillingClient, activeBillingSecret := setting.TossActiveBillingKeyPair()
	auth, _, err := getTossPaymentWithCredentialForMID(ctx, paymentKey, topUp.ProviderCredential, policy.ProviderClientKeyHash, activeBillingClient, activeBillingSecret)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge webhook payment lookup failed order_id=%s error=%q", orderId, err.Error()))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if auth == nil || auth.Type != "BILLING" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss wallet auto recharge webhook payment type mismatch order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	if auth.OrderId != orderId {
		logger.LogWarn(ctx, fmt.Sprintf("Toss wallet auto recharge webhook order mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
		c.Status(http.StatusOK)
		return
	}
	if strings.TrimSpace(auth.PaymentKey) != paymentKey {
		logger.LogWarn(ctx, fmt.Sprintf("Toss wallet auto recharge webhook paymentKey mismatch order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	if shouldRetryTossCancellationWebhook(claimedStatus, auth, orderId, paymentKey) {
		if isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
			if _, _, persistErr := persistTossCancellationEventsWithContext(ctx, auth); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss lagging wallet cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return
			}
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss wallet cancellation not yet visible in authoritative payment order_id=%s auth_status=%s", orderId, auth.Status))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	payload, marshalErr := common.Marshal(auth)
	if marshalErr != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}

	switch strings.ToUpper(strings.TrimSpace(auth.Status)) {
	case "DONE":
		if !isValidTossBillingCharge(auth, orderId, topUp.Amount) {
			mismatchErr := model.MarkWalletAutoRechargeDoneMismatchWithContext(ctx, policy.Id, orderId, &model.TossBillingChargeResult{
				ProviderStatus:  auth.Status,
				Total:           auth.TotalAmount,
				BalanceAmount:   auth.BalanceAmount,
				PaymentKey:      auth.PaymentKey,
				ProviderPayload: string(payload),
			})
			if mismatchErr != nil && !errors.Is(mismatchErr, model.ErrWalletAutoRechargeReconciliationRequired) {
				logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge DONE mismatch persist failed order_id=%s error=%q", orderId, mismatchErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge DONE payload mismatch order_id=%s expected=%d actual=%d", orderId, topUp.Amount, auth.TotalAmount))
			c.Status(http.StatusOK)
			return
		}
		if err := model.RecordWalletAutoRechargeProviderDONEEvidenceWithContext(ctx, policy.Id, orderId, &model.TossBillingChargeResult{
			Done:            true,
			ProviderStatus:  auth.Status,
			Total:           auth.TotalAmount,
			BalanceAmount:   auth.BalanceAmount,
			PaymentKey:      auth.PaymentKey,
			ProviderPayload: string(payload),
		}); err != nil {
			if errors.Is(err, model.ErrWalletAutoRechargeReconciliationRequired) {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge DONE conflicts with cancellation order_id=%s", orderId))
				c.Status(http.StatusOK)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge DONE evidence persist failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if err := model.SettleWalletAutoRechargeWebhookDoneWithContext(ctx, policy.Id, orderId, auth.PaymentKey, string(payload), time.Time{}); err != nil {
			if errors.Is(err, model.ErrWalletAutoRechargeReconciliationRequired) {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge DONE raced local terminal state order_id=%s", orderId))
				c.Status(http.StatusOK)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge webhook settlement failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss wallet auto recharge settled (webhook) order_id=%s", orderId))
		c.Status(http.StatusOK)
		return

	case "CANCELED", "PARTIAL_CANCELED":
		// Toss cancellation transactions are the durable financial audit record;
		// persist them before changing local retry policy or acknowledging.
		if _, _, err := persistExpectedTossCancellationEvents(ctx, auth, orderId, topUp.Amount); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge cancellation persist failed order_id=%s error=%q", orderId, err.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		requiresReconciliation := strings.EqualFold(auth.Status, "PARTIAL_CANCELED") || auth.BalanceAmount != 0
		if err := model.ApplyWalletAutoRechargeWebhookTerminalWithContext(ctx, policy.Id, orderId, auth.Status, string(payload), requiresReconciliation, time.Time{}); err != nil {
			if !errors.Is(err, model.ErrWalletAutoRechargeReconciliationRequired) {
				logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge cancellation apply failed order_id=%s error=%q", orderId, err.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge %s order_id=%s balance=%d", auth.Status, orderId, auth.BalanceAmount))
		}
		c.Status(http.StatusOK)
		return

	case "ABORTED", "EXPIRED":
		if err := model.ApplyWalletAutoRechargeWebhookTerminalWithContext(ctx, policy.Id, orderId, auth.Status, string(payload), false, time.Time{}); err != nil {
			if !errors.Is(err, model.ErrWalletAutoRechargeReconciliationRequired) {
				logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge terminal apply failed order_id=%s status=%s error=%q", orderId, auth.Status, err.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge terminal raced charge order_id=%s status=%s", orderId, auth.Status))
		}
		c.Status(http.StatusOK)
		return
	default:
		c.Status(http.StatusOK)
	}
}

// TossWebhookReadDeadlineMiddleware must run before any response-writer wrapper
// (notably gzip). It applies one ingress processing deadline to the request and
// socket read, then commits the empty response under a separate write deadline
// before clearing both deadlines for keep-alive reuse. If the server cannot
// reach the underlying connection, fail before entering a blocking body read.
func TossWebhookReadDeadlineMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL == nil || c.Request.URL.Path != "/api/toss/webhook" {
			c.Next()
			return
		}
		responseController := http.NewResponseController(c.Writer)
		started := time.Now()
		processingDeadline := started.Add(tossWebhookProcessingTimeout)
		responseDeadline := started.Add(tossWebhookResponseTimeout)
		if parentDeadline, ok := c.Request.Context().Deadline(); ok {
			if parentDeadline.Before(processingDeadline) {
				processingDeadline = parentDeadline
			}
			if parentDeadline.Before(responseDeadline) {
				responseDeadline = parentDeadline
			}
		}
		if err := responseController.SetReadDeadline(processingDeadline); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Toss webhook connection read deadline unavailable error=%q", err.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if err := responseController.SetWriteDeadline(responseDeadline); err != nil {
			_ = responseController.SetReadDeadline(time.Time{})
			logger.LogError(c.Request.Context(), fmt.Sprintf("Toss webhook connection write deadline unavailable error=%q", err.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		requestCtx, cancel := context.WithDeadline(c.Request.Context(), processingDeadline)
		c.Request = c.Request.WithContext(requestCtx)
		defer func() {
			cancel()
			if err := responseController.SetWriteDeadline(time.Time{}); err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Toss webhook connection write deadline clear failed error=%q", err.Error()))
			}
			if err := responseController.SetReadDeadline(time.Time{}); err != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Toss webhook connection read deadline clear failed error=%q", err.Error()))
			}
		}()
		c.Next()
		// Toss responses have no body. Commit and flush the final status while the
		// write deadline is still active; Gin otherwise delays a Status-only header
		// until after middleware unwinds.
		if !c.Writer.Written() {
			c.Header("Content-Length", "0")
			c.Writer.WriteHeaderNow()
		}
		if err := responseController.Flush(); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Toss webhook response flush failed error=%q", err.Error()))
		}
	}
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
	// Toss requires an HTTP 200 response within ten seconds. Credential rotation
	// can make verification try more than one secret, so the deadline must cover
	// the whole handler rather than restart for every provider lookup candidate.
	ctx, cancel := context.WithTimeout(c.Request.Context(), tossWebhookProcessingTimeout)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tossWebhookMaxBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			return
		}
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
		if !isTrustedTossWebhookSource(c.Request) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss BILLING_DELETED webhook rejected from untrusted direct peer=%q", c.Request.RemoteAddr))
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		// BILLING_DELETED documents billingKey only at the top level. Treat a
		// nested-only variant as malformed instead of accepting an unofficial
		// identity location for a destructive local-key revocation.
		billingKey := strings.TrimSpace(event.BillingKey)
		if !isValidTossBillingKey(billingKey) {
			logger.LogWarn(ctx, "Toss billing deleted webhook missing billingKey")
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		boundedCtx, releaseDB, bindErr := model.BindDeadlineDBContext(ctx)
		if bindErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss webhook deadline DB context setup failed error=%q", bindErr.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		defer releaseDB()
		ctx = boundedCtx
		c.Request = c.Request.WithContext(ctx)
		// BILLING_DELETED documents only the provider billingKey identity. Do not
		// let an unexpected data.customerKey narrow the lookup and hide a corrupt
		// legacy candidate that may be the deleted key.
		revoked, err := model.RevokeTossBillingKeyByPlainWithContext(ctx, "", billingKey)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss billing deleted webhook revoke failed error=%q", err.Error()))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if revoked {
			logger.LogInfo(ctx, "Toss billing key revoked from BILLING_DELETED webhook")
		}
		c.Status(http.StatusOK)
		return
	}
	if !isTossPaymentWebhookEventType(event.EventType) {
		c.Status(http.StatusOK)
		return
	}

	orderId := event.Data.OrderId
	if !isValidTossOrderId(orderId) ||
		(event.Data.PaymentKey != "" && !isValidTossPaymentKey(event.Data.PaymentKey)) {
		c.Status(http.StatusOK)
		return
	}

	status := event.Data.Status
	isDone := status == "DONE"
	isWaitingForDeposit := status == "WAITING_FOR_DEPOSIT"
	isCancel := isTossCancelStatus(status)
	isFailTerminal := isTossTerminalFailStatus(status)
	if !isDone && !isWaitingForDeposit && !isCancel && !isFailTerminal {
		// READY / IN_PROGRESS and other non-financial intermediate states do not
		// require local action. WAITING_FOR_DEPOSIT is handled below so a
		// browser-mutated virtual-account order can be canceled before deposit.
		c.Status(http.StatusOK)
		return
	}

	if !LockOrderWithContext(ctx, orderId) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook order lock deadline exceeded order_id=%s", orderId))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	defer UnlockOrder(orderId)
	boundedCtx, releaseDB, bindErr := model.BindDeadlineDBContext(ctx)
	if bindErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss webhook deadline DB context setup failed order_id=%s error=%q", orderId, bindErr.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	defer releaseDB()
	ctx = boundedCtx
	c.Request = c.Request.WithContext(ctx)

	topUp, lookupErr := model.GetTopUpByTradeNoWithErrorContext(ctx, orderId)
	if lookupErr != nil && !errors.Is(lookupErr, model.ErrTopUpNotFound) {
		logger.LogError(ctx, fmt.Sprintf("Toss webhook top-up lookup failed order_id=%s error=%q", orderId, lookupErr.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if lookupErr == nil && model.HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo) &&
		(topUp.PaymentProvider != model.PaymentProviderToss || topUp.PaymentMethod != model.PaymentMethodToss) {
		// twa_ is a private Toss wallet namespace. A restored row can lose its
		// discriminator columns independently of trade_no; routing it through the
		// subscription fallback would acknowledge the webhook while hiding an
		// unscoped provider charge. Retry until an operator repairs the evidence.
		logger.LogError(ctx, fmt.Sprintf("Toss webhook rejected corrupt opaque wallet order order_id=%s", orderId))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if errors.Is(lookupErr, model.ErrTopUpNotFound) || topUp.PaymentProvider != model.PaymentProviderToss ||
		(topUp.PaymentMethod != model.PaymentMethodToss && !model.HasTossWalletOpaqueOrderIDPrefix(topUp.TradeNo)) {
		if handleTossSubscriptionPaymentWebhook(c, orderId, event.Data.PaymentKey, status, isCancel) {
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if walletPolicy, isWalletAutoRecharge, walletLookupErr := model.GetWalletAutoRechargeByChargeTradeNoWithContext(ctx, orderId); walletLookupErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss wallet auto recharge webhook policy lookup failed order_id=%s error=%q", orderId, walletLookupErr.Error()))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	} else if isWalletAutoRecharge {
		handleTossWalletAutoRechargePaymentWebhook(c, walletPolicy, topUp, event.Data.PaymentKey, status)
		return
	}
	if handleRefundFencedTossTopUpWebhook(c, topUp, event.Data.PaymentKey, status) {
		return
	}

	// Always re-read a terminal webhook for an already-credited order. Toss can
	// redeliver an older DONE event after the current Payment has become CANCELED;
	// trusting only the stale event.status would hide a real refund indefinitely.
	if topUp.Status == common.TopUpStatusSuccess {
		if event.Data.PaymentKey == "" {
			logger.LogError(ctx, fmt.Sprintf("Toss terminal webhook on credited order missing paymentKey order_id=%s status=%s", orderId, status))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
		auth, _, err := getTossPaymentWithCredentialForMID(ctx, event.Data.PaymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss credited-order terminal webhook verification failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if auth == nil || auth.Type != "NORMAL" {
			logger.LogWarn(ctx, fmt.Sprintf("Toss credited-order terminal payment type mismatch order_id=%s", orderId))
			c.Status(http.StatusOK)
			return
		}
		if auth.OrderId != orderId || strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(event.Data.PaymentKey) ||
			strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(topUp.ProviderOrderId) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss credited-order terminal identity mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
			c.Status(http.StatusOK)
			return
		}
		if shouldRetryTossDoneWebhook(status, auth, orderId, event.Data.PaymentKey) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss credited-order DONE is not yet visible in authoritative payment order_id=%s auth_status=%s", orderId, auth.Status))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		laggingFinalCancellation := shouldRetryTossCancellationWebhook(status, auth, orderId, event.Data.PaymentKey)
		if laggingFinalCancellation && !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss credited-order cancellation not yet visible order_id=%s auth_status=%s", orderId, auth.Status))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		switch {
		case auth.Status == "DONE":
			if !isValidTossTopUpPayment(auth, orderId, topUp.Amount) {
				if _, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "credited top-up webhook DONE payload mismatch"); persistErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss credited-order paid mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
					c.Status(http.StatusServiceUnavailable)
					return
				}
				c.Status(http.StatusOK)
				return
			}
			if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss webhook reconciliation resolve failed order_id=%s error=%q", orderId, err.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
		case isTossCancelStatus(auth.Status):
			if auth.TotalAmount != topUp.Amount || !strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") {
				if _, persistErr := persistTossFinancialMismatchEventWithContext(ctx, auth, "credited top-up cancellation amount or currency mismatch"); persistErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss credited cancellation mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
					c.Status(http.StatusServiceUnavailable)
					return
				}
				break
			}
			// The purchase contract may have been tightened after this order was
			// legitimately credited. Exact local identity and amount are sufficient
			// to persist later authoritative refund evidence; method/tax/escrow fields
			// must not erase a cancellation from the audit trail.
			created, canceledAmount, persistErr := persistTossCancellationEventsWithContext(ctx, auth)
			if persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			if created > 0 {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: payment %s after credit order_id=%s user_id=%d newly_canceled=%d KRW balance=%d KRW", auth.Status, orderId, topUp.UserId, canceledAmount, auth.BalanceAmount))
				model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss payment %s after credit (canceled: %d KRW, balance: %d KRW) — manual quota reconciliation required", auth.Status, canceledAmount, auth.BalanceAmount), c.ClientIP(), topUp.PaymentMethod, "toss-cancel")
			}
		}
		if laggingFinalCancellation {
			logger.LogWarn(ctx, fmt.Sprintf("Toss credited-order final cancellation remains partially visible order_id=%s", orderId))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status != common.TopUpStatusPending && isDone {
		// A provider completion can arrive after a local timeout/cleanup closed
		// the order. Verify with the exact stored MID, then use the same guarded
		// recovery path as transaction reconciliation. It may reopen only this
		// NORMAL Toss order with the same paymentKey, and settlement still checks
		// authoritative cancellation precedence under the order lock.
		if event.Data.PaymentKey == "" {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
		auth, _, err := getTossPaymentWithCredentialForMID(ctx, event.Data.PaymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss late-DONE webhook verification failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if auth == nil || auth.Type != "NORMAL" || auth.PaymentKey != event.Data.PaymentKey || auth.OrderId != orderId {
			authOrder := ""
			authStatus := ""
			if auth != nil {
				authOrder = auth.OrderId
				authStatus = auth.Status
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss late-DONE webhook authoritative classification/identity mismatch order_id=%s auth_order=%s auth_status=%s", orderId, authOrder, authStatus))
			c.Status(http.StatusOK)
			return
		}
		if shouldRetryTossDoneWebhook(status, auth, orderId, event.Data.PaymentKey) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss late-DONE is not yet visible in authoritative payment order_id=%s auth_status=%s", orderId, auth.Status))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if isTossCancelStatus(auth.Status) {
			if _, _, persistErr := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, auth, "late cancellation left an uncredited provider balance"); persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss stale-DONE/current-cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: stale DONE webhook resolved to current %s order_id=%s local_status=%s", auth.Status, orderId, topUp.Status))
			c.Status(http.StatusOK)
			return
		}
		if auth.Status != "DONE" {
			logger.LogWarn(ctx, fmt.Sprintf("Toss late-DONE webhook authoritative status changed order_id=%s auth_status=%s", orderId, auth.Status))
			c.Status(http.StatusOK)
			return
		}
		persistFallback := func(reason string, financialMismatch bool) bool {
			var created bool
			var persistErr error
			if financialMismatch {
				created, persistErr = persistTossFinancialMismatchEventWithContext(ctx, auth, reason)
			} else {
				created, persistErr = persistTossFulfillmentEventWithContext(ctx, auth)
			}
			if persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss late-DONE fulfillment persist failed order_id=%s reason=%s error=%q", orderId, reason, persistErr.Error()))
				return false
			}
			if created {
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: payment DONE after local terminal state order_id=%s user_id=%d local_status=%s amount=%d KRW reason=%s", orderId, topUp.UserId, topUp.Status, auth.TotalAmount, reason))
				model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss payment completed after local order became %s — manual quota fulfillment required", topUp.Status), c.ClientIP(), topUp.PaymentMethod, "toss-late-done")
			}
			return true
		}
		if !isValidTossTopUpPayment(auth, orderId, topUp.Amount) {
			exactRefundIdentity := auth.TotalAmount == topUp.Amount &&
				strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW")
			if exactRefundIdentity {
				if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "late DONE top-up contract mismatch"); queueErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss late-DONE refund queue failed order_id=%s error=%q", orderId, queueErr.Error()))
					c.Status(http.StatusServiceUnavailable)
					return
				}
			} else if !persistFallback("authoritative_payload_mismatch", true) {
				c.Status(http.StatusServiceUnavailable)
				return
			}
			c.Status(http.StatusOK)
			return
		}
		if err := model.RecoverTossTopUpPaymentKeyWithContext(ctx, orderId, auth.PaymentKey); err != nil {
			_ = persistFallback("payment_key_recovery_failed", false)
			logger.LogError(ctx, fmt.Sprintf("Toss late-DONE paymentKey recovery failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if err := model.RechargeTossWithContext(ctx, orderId, auth.PaymentKey, c.ClientIP()); err != nil {
			if errors.Is(err, model.ErrTossCancellationPrecedesFulfillment) {
				logger.LogWarn(ctx, fmt.Sprintf("Toss late-DONE settlement blocked by authoritative cancellation order_id=%s", orderId))
				c.Status(http.StatusOK)
				return
			}
			if errors.Is(err, model.ErrTopUpQuotaCapacityExceeded) {
				if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "quota capacity prevented late local fulfillment"); queueErr != nil {
					logger.LogError(ctx, fmt.Sprintf("Toss late-DONE refund queue failed order_id=%s error=%q", orderId, queueErr.Error()))
					c.Status(http.StatusServiceUnavailable)
					return
				}
			} else {
				_ = persistFallback("quota_settlement_failed", false)
			}
			logger.LogError(ctx, fmt.Sprintf("Toss late-DONE settlement failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss late-DONE reconciliation resolve failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		logger.LogInfo(ctx, fmt.Sprintf("Toss late-DONE top-up recovered and credited order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status != common.TopUpStatusPending && isCancel {
		// Even a locally closed order can later be canceled in the Toss console.
		// Persist that financial event instead of silently acknowledging it.
		if event.Data.PaymentKey == "" {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
		auth, _, err := getTossPaymentWithCredentialForMID(ctx, event.Data.PaymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
		if err != nil {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if auth == nil || auth.Type != "NORMAL" {
			logger.LogWarn(ctx, fmt.Sprintf("Toss terminal-order cancellation payment type mismatch order_id=%s", orderId))
			c.Status(http.StatusOK)
			return
		}
		if auth.OrderId != orderId || strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(event.Data.PaymentKey) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss terminal-order cancellation identity mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
			c.Status(http.StatusOK)
			return
		}
		laggingFinalCancellation := shouldRetryTossCancellationWebhook(status, auth, orderId, event.Data.PaymentKey)
		if laggingFinalCancellation && !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
			logger.LogWarn(ctx, fmt.Sprintf("Toss terminal-order cancellation not yet visible order_id=%s auth_status=%s", orderId, auth.Status))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if isTossCancelStatus(auth.Status) {
			if _, _, err := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, auth, "terminal uncredited top-up cancellation left a provider balance"); err != nil {
				c.Status(http.StatusServiceUnavailable)
				return
			}
		}
		if laggingFinalCancellation {
			logger.LogWarn(ctx, fmt.Sprintf("Toss terminal-order final cancellation remains partially visible order_id=%s", orderId))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
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
		c.Status(http.StatusServiceUnavailable)
		return
	}
	activeClientKey, activeSecretKey := setting.TossActiveKeyPair()
	auth, _, err := getTossPaymentWithCredentialForMID(ctx, event.Data.PaymentKey, topUp.ProviderCredential, topUp.ProviderClientKeyHash, activeClientKey, activeSecretKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss webhook get payment failed order_id=%s error=%q", orderId, err.Error()))
		c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
		return
	}
	if auth == nil || auth.Type != "NORMAL" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss pending-order webhook payment type mismatch order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	if auth.OrderId != orderId {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook order mismatch order_id=%s auth_order=%s", orderId, auth.OrderId))
		c.Status(http.StatusOK)
		return
	}
	if strings.TrimSpace(auth.PaymentKey) != strings.TrimSpace(event.Data.PaymentKey) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook paymentKey mismatch order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	if shouldRetryTossDoneWebhook(status, auth, orderId, event.Data.PaymentKey) {
		// Make the authenticated payment identity durable before asking Toss to
		// redeliver. If this process exits before the next webhook, the ordinary
		// recorded-payment recovery lane can repeat the exact idempotent confirm.
		if err := model.RecordTossPaymentKeyWithContext(ctx, orderId, auth.PaymentKey); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss lagging-DONE paymentKey persist failed order_id=%s error=%q", orderId, err.Error()))
		}
		logger.LogWarn(ctx, fmt.Sprintf("Toss pending-order DONE is not yet visible in authoritative payment order_id=%s auth_status=%s", orderId, auth.Status))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	laggingFinalCancellation := shouldRetryTossCancellationWebhook(status, auth, orderId, event.Data.PaymentKey)
	if laggingFinalCancellation && !isTossCancelStatus(strings.ToUpper(strings.TrimSpace(auth.Status))) {
		logger.LogWarn(ctx, fmt.Sprintf("Toss pending-order cancellation not yet visible order_id=%s auth_status=%s", orderId, auth.Status))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	if handled, finalizeErr := finalizeQueuedTossTopUpRefundIfTerminal(ctx, topUp, auth); handled || finalizeErr != nil {
		if finalizeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss webhook refunded-order finalization failed order_id=%s error=%q", orderId, finalizeErr.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.Status(http.StatusOK)
		return
	}
	if auth.Status == "WAITING_FOR_DEPOSIT" {
		if auth.TotalAmount == topUp.Amount && strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW") &&
			!hasSupportedTossTopUpFundingSource(auth) {
			if _, queueErr := queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "virtual-account top-up must be canceled before deposit"); queueErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss WAITING_FOR_DEPOSIT refund queue failed order_id=%s error=%q", orderId, queueErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
		}
		c.Status(http.StatusOK)
		return
	}

	if auth.Status == "DONE" {
		if !isValidTossTopUpPayment(auth, orderId, topUp.Amount) {
			exactRefundIdentity := auth.TotalAmount == topUp.Amount &&
				strings.EqualFold(strings.TrimSpace(auth.Currency), "KRW")
			var persistErr error
			if exactRefundIdentity {
				_, persistErr = queueTossTopUpRefundRequirementWithContext(ctx, topUp, auth, "top-up webhook DONE contract mismatch")
			} else {
				_, persistErr = persistTossFinancialMismatchEventWithContext(ctx, auth, "top-up webhook DONE payload mismatch")
			}
			if persistErr != nil {
				logger.LogError(ctx, fmt.Sprintf("Toss webhook paid mismatch persist failed order_id=%s error=%q", orderId, persistErr.Error()))
				c.Status(http.StatusServiceUnavailable)
				return
			}
			logger.LogWarn(ctx, fmt.Sprintf("Toss webhook DONE authoritative mismatch order_id=%s total=%d currency=%s card_present=%t easy_pay_present=%t", orderId, auth.TotalAmount, auth.Currency, auth.Card != nil, auth.EasyPay != nil))
			c.Status(http.StatusOK)
			return
		}
		// Persist the paymentKey before crediting (committed independently of the credit txn) so
		// the stale-pending sweep can't expire this approved order if RechargeToss rolls back or a
		// webhook retry is delayed past the sweep window. provider_order_id must != trade_no first.
		if err := model.RecordTossPaymentKeyWithContext(ctx, orderId, auth.PaymentKey); err != nil {
			_, _ = persistTossFulfillmentEventWithContext(ctx, auth)
			logger.LogError(ctx, fmt.Sprintf("Toss webhook record paymentKey failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
			return
		}
		if err := model.RechargeTossWithContext(ctx, orderId, auth.PaymentKey, c.ClientIP()); err != nil {
			_ = recordTossTopUpSettlementFailure(ctx, topUp, auth, err)
			c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
			return
		}
		if err := model.ResolveTossPaymentEventsWithContext(ctx, orderId, model.TossPaymentEventTypeFulfillment); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss webhook reconciliation resolve failed order_id=%s error=%q", orderId, err.Error()))
			c.Status(http.StatusServiceUnavailable)
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
		if err := model.UpdatePendingTopUpStatusForMethodContext(ctx, orderId, model.PaymentProviderToss, model.PaymentMethodToss, target); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("Toss webhook close pending order_id=%s status=%s error=%q", orderId, auth.Status, err.Error()))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("Toss webhook closed pending order order_id=%s status=%s", orderId, auth.Status))
		}
		c.Status(http.StatusOK)
		return
	}

	// CANCELED / PARTIAL_CANCELED on a still-pending order: the payment was completed at Toss
	// (PARTIAL_CANCELED may retain a balance) but we never credited it. Persist every cancellation.
	// If money remains, install the durable refund fence before acknowledging the webhook so the
	// normal cancellation worker owns the remainder. Only a zero-balance cancellation may close.
	if isTossCancelStatus(auth.Status) {
		created, canceledAmount, persistErr := persistUncreditedTossTopUpCancellationWithContext(ctx, topUp, auth, "uncredited top-up was partially canceled with provider balance remaining")
		if persistErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Toss pending cancellation persist failed order_id=%s error=%q", orderId, persistErr.Error()))
			c.Status(http.StatusServiceUnavailable)
			return
		}
		if created > 0 {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: %s on uncredited pending order order_id=%s user_id=%d canceled=%d KRW balance=%d KRW", auth.Status, orderId, topUp.UserId, canceledAmount, auth.BalanceAmount))
			model.RecordTopupLogWithContext(ctx, topUp.UserId, fmt.Sprintf("Toss %s on uncredited order (canceled: %d KRW, balance: %d KRW) — manual reconciliation required", auth.Status, canceledAmount, auth.BalanceAmount), c.ClientIP(), topUp.PaymentMethod, "toss-cancel")
		}
		if laggingFinalCancellation {
			logger.LogWarn(ctx, fmt.Sprintf("Toss pending-order final cancellation remains partially visible order_id=%s", orderId))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if tossTopUpCancellationNeedsRefundFence(auth) {
			// A partial refund still leaves customer funds at Toss. Keep this row in
			// the recovery lane so a balance-scoped full-cancel operation can refund
			// the remainder. A virtual-account bank refund also stays here until
			// refundStatus proves completion; closing either would strand funds.
			logger.LogWarn(ctx, fmt.Sprintf("Toss partial refund remains queued for automatic completion order_id=%s balance=%d", orderId, auth.BalanceAmount))
			c.Status(http.StatusOK)
			return
		}
		if err := model.UpdatePendingTopUpStatusForMethodContext(ctx, orderId, model.PaymentProviderToss, model.PaymentMethodToss, common.TopUpStatusFailed); err != nil &&
			!errors.Is(err, model.ErrTopUpStatusInvalid) {
			c.Status(http.StatusServiceUnavailable)
			return
		}
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
