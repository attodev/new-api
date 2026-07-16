package controller

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

const organizationOwnerPermissionRequired = "organization owner permission required"

type walletAutoRechargeRequest struct {
	PresetId          int    `json:"preset_id"`
	PresetFingerprint string `json:"preset_fingerprint"`
}

type walletAutoRechargeTossResponse struct {
	ClientKey         string                              `json:"client_key"`
	CustomerKey       string                              `json:"customer_key"`
	TradeNo           string                              `json:"trade_no"`
	SuccessURL        string                              `json:"success_url"`
	FailURL           string                              `json:"fail_url"`
	PresetFingerprint string                              `json:"preset_fingerprint"`
	Policy            model.WalletAutoRechargePresetTerms `json:"policy"`
}

type walletRechargeTarget struct {
	TargetType  string
	TargetId    int
	OwnerUserId int
}

var walletAutoRechargeBillingKeyIssuer = issueTossBillingKeyWithSecret

var walletAutoRechargeTossCharger model.TossBillingCharger = tossBillingChargeForModel

var walletAutoRechargeStoredBillingKeyRevoker = model.RevokeStoredTossBillingKey

type walletAutoRechargeIssuedBillingKeyCleanup func(
	ctx context.Context,
	userID int,
	tradeNo, fallbackCustomerKey string,
	issued *tossBillingIssueResponse,
	secretKey, reason string,
) error

func durablyRevokeWalletAutoRechargeIssuedBillingKey(
	ctx context.Context,
	userID int,
	tradeNo, fallbackCustomerKey string,
	issued *tossBillingIssueResponse,
	secretKey, reason string,
) error {
	if issued == nil || strings.TrimSpace(issued.BillingKey) == "" {
		return errors.New("wallet auto recharge issued billing key is unavailable")
	}
	billingKeyID := queueIssuedTossBillingKeyForRevocation(
		ctx,
		userID,
		tradeNo,
		fallbackCustomerKey,
		issued,
		secretKey,
		reason,
	)
	if billingKeyID <= 0 {
		return errors.New("wallet auto recharge billing key cleanup was not durably queued")
	}
	// Once the pending-revocation row is durable, the background cleanup task
	// can retry a failed DELETE. The immediate remote call only reduces latency.
	revokeTossBillingKey(ctx, billingKeyID, reason)
	return nil
}

var walletAutoRechargeIssuedBillingKeyRevoker walletAutoRechargeIssuedBillingKeyCleanup = durablyRevokeWalletAutoRechargeIssuedBillingKey

func init() {
	model.SetWalletAutoRechargeTossPaymentLookup(lookupWalletAutoRechargeTossPayment)
	model.SetWalletAutoRechargeIssueReconciler(reconcileWalletAutoRechargeBillingIssue)
}

func lookupWalletAutoRechargeTossPayment(ctx context.Context, secretKeys []string, orderId string, amount int64) (*model.TossBillingChargeResult, error) {
	if len(secretKeys) == 0 {
		secretKeys = []string{""}
	}
	var (
		res        *tossConfirmResponse
		statusCode int
		err        error
	)
	for i, secretKey := range secretKeys {
		res, statusCode, err = getTossPaymentByOrderIdWithSecret(ctx, orderId, secretKey)
		if err == nil {
			break
		}
		if statusCode == http.StatusNotFound {
			if i > 0 {
				// The exact attempt credential was rejected and this 404 came
				// from a rotated fallback key. Toss scopes idempotency to the API
				// key, so treating this as a safe-to-POST absence could charge the
				// card again in a different namespace while the original payment
				// remains inaccessible. Keep the durable attempt pending instead.
				return nil, fmt.Errorf(
					"%w: rotated credential fallback returned 404 order_id=%s",
					model.ErrTossBillingChargePending,
					orderId,
				)
			}
			return nil, fmt.Errorf("%w: order_id=%s", model.ErrTossBillingPaymentNotFound, orderId)
		}
		if i == len(secretKeys)-1 || !isTossCredentialRejection(statusCode, err) {
			return nil, fmt.Errorf("%w: lookup status=%d err=%v", model.ErrTossBillingChargePending, statusCode, err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: lookup status=%d err=%v", model.ErrTossBillingChargePending, statusCode, err)
	}
	result := &model.TossBillingChargeResult{}
	if res == nil {
		return nil, fmt.Errorf("%w: empty lookup response", model.ErrTossBillingChargePending)
	}
	result.Total = res.TotalAmount
	result.BalanceAmount = res.BalanceAmount
	result.PaymentKey = res.PaymentKey
	result.ProviderStatus = res.Status
	if payload, marshalErr := common.Marshal(res); marshalErr == nil {
		result.ProviderPayload = string(payload)
	}
	switch strings.ToUpper(strings.TrimSpace(res.Status)) {
	case "DONE":
		result.Done = isValidTossBillingCharge(res, orderId, amount)
		if !result.Done {
			return result, errors.New("Toss wallet auto recharge lookup payload mismatch")
		}
		return result, nil
	case "CANCELED", "PARTIAL_CANCELED":
		if !isValidTossCardCancellation(res, orderId, amount) {
			return result, errors.New("Toss wallet auto recharge cancellation amount or payment method mismatch")
		}
		if _, _, persistErr := persistExpectedTossCancellationEvents(ctx, res, orderId, amount); persistErr != nil {
			// No retry/new charge is safe until the provider-side financial event
			// is durably recorded.
			return result, fmt.Errorf("%w: persist wallet cancellation: %v", model.ErrTossBillingChargePending, persistErr)
		}
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeCanceled, res.Status)
	case "ABORTED", "EXPIRED":
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargeTerminal, res.Status)
	default:
		return result, fmt.Errorf("%w: payment status=%s", model.ErrTossBillingChargePending, res.Status)
	}
}

func resolveUserWalletTarget(c *gin.Context) (walletRechargeTarget, bool) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return walletRechargeTarget{}, false
	}
	if user.OrganizationId > 0 {
		common.ApiErrorI18n(c, i18n.MsgPaymentPersonalBillingOrgActive)
		return walletRechargeTarget{}, false
	}
	return walletRechargeTarget{
		TargetType:  model.TopUpTargetTypeUser,
		TargetId:    user.Id,
		OwnerUserId: user.Id,
	}, true
}

func resolveOrganizationWalletTarget(c *gin.Context) (walletRechargeTarget, bool) {
	actor, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
		return walletRechargeTarget{}, false
	}
	if actor.OrganizationId <= 0 {
		common.ApiErrorMsg(c, organizationOwnerPermissionRequired)
		return walletRechargeTarget{}, false
	}

	org := &model.Organization{}
	if err := model.DB.First(org, actor.OrganizationId).Error; err != nil {
		common.ApiError(c, err)
		return walletRechargeTarget{}, false
	}
	if actor.OrganizationRole != model.OrganizationRoleOwner || org.OwnerUserId != actor.Id {
		common.ApiErrorMsg(c, organizationOwnerPermissionRequired)
		return walletRechargeTarget{}, false
	}

	return walletRechargeTarget{
		TargetType:  model.TopUpTargetTypeOrganization,
		TargetId:    org.Id,
		OwnerUserId: actor.Id,
	}, true
}

func requestWalletAutoRecharge(c *gin.Context, policyType string, target walletRechargeTarget) {
	if !requirePaymentCompliance(c) {
		return
	}
	if !isTossWalletAutoRechargeEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	tossConfig, err := model.GetFreshTossConfigSnapshot()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isFreshTossWalletAutoRechargeCheckoutEnabled(tossConfig) {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		logger.LogError(c.Request.Context(), "wallet auto recharge blocked: invalid ServerAddress configuration")
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}

	var req walletAutoRechargeRequest
	if err := decodeTossPaymentRequestJSON(c, &req); err != nil {
		if common.IsRequestBodyTooLargeError(err) {
			respondTossPaymentRequestBodyTooLarge(c)
			return
		}
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.PresetFingerprint = strings.TrimSpace(req.PresetFingerprint)
	if req.PresetId <= 0 || req.PresetFingerprint == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.GetWalletAutoRechargePresetForTargetMode(req.PresetId, policyType, target.TargetType, tossConfig.TestMode)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if preset.TermsFingerprint != req.PresetFingerprint {
		common.ApiError(c, model.ErrWalletAutoRechargePresetChanged)
		return
	}

	customerKey, err := model.GetOrCreateTossCustomerKey(target.OwnerUserId)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	// A legacy key can still be valid for the server-side Billing API while
	// exceeding the JavaScript SDK v2 contract. Never expose such a value to the
	// browser or create a pending policy that the card-registration SDK cannot
	// complete.
	if !isValidTossSDKCustomerKey(customerKey) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wallet auto recharge blocked: stored customer key is incompatible with Toss SDK v2 owner=%d", target.OwnerUserId))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	// Capture the client/secret pair from one immutable setting snapshot so a
	// concurrent admin rotation cannot mix keys from different MIDs.
	providerClientKey, providerSecretKey := setting.TossActiveBillingKeyPairFromSnapshot(tossConfig)
	if strings.TrimSpace(providerClientKey) == "" || strings.TrimSpace(providerSecretKey) == "" {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	providerCredential, err := model.EncryptProviderCredential(providerSecretKey)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wallet auto recharge provider credential snapshot failed owner=%d err=%v", target.OwnerUserId, err))
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	referenceSeed := fmt.Sprintf("wallet-auto-auth-%d-%d-%s", target.OwnerUserId, time.Now().UnixMilli(), randstr.String(16))
	reference := "wallet_auth_" + common.Sha1([]byte(referenceSeed))
	policy, lockedPreset, err := model.CreatePendingWalletAutoRechargeFromPreset(model.CreateWalletAutoRechargeRequest{
		PresetId:              preset.Id,
		Type:                  policyType,
		TargetType:            target.TargetType,
		TargetId:              target.TargetId,
		OwnerUserId:           target.OwnerUserId,
		CustomerKey:           customerKey,
		AuthTradeNo:           reference,
		ProviderCredential:    providerCredential,
		ProviderClientKeyHash: model.TossBillingClientKeyFingerprint(providerClientKey),
	}, req.PresetFingerprint, tossConfig.TestMode)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	common.ApiSuccess(c, walletAutoRechargeTossResponse{
		ClientKey:         providerClientKey,
		CustomerKey:       customerKey,
		TradeNo:           policy.AuthTradeNo,
		SuccessURL:        base + "/api/wallet/auto-recharge/toss/confirm?trade_no=" + policy.AuthTradeNo,
		FailURL:           base + "/api/wallet/auto-recharge/toss/fail?trade_no=" + policy.AuthTradeNo,
		PresetFingerprint: lockedPreset.TermsFingerprint,
		Policy:            lockedPreset.Terms(),
	})
}

func loadWalletAutoRechargeByTradeNo(tradeNo string) (*model.WalletAutoRecharge, error) {
	var policy model.WalletAutoRecharge
	if err := model.DB.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func lockWalletAutoRechargeAuthTrade(policy *model.WalletAutoRecharge) func() {
	if policy == nil || strings.TrimSpace(policy.AuthTradeNo) == "" {
		return func() {}
	}
	LockOrder(policy.AuthTradeNo)
	return func() { UnlockOrder(policy.AuthTradeNo) }
}

func walletAutoRechargeRedirectBase(_ *model.WalletAutoRecharge) string {
	// `/wallet` is the cross-theme billing entry point. Default dispatches it
	// to the active personal or organization wallet, while Classic rewrites it
	// to `/console/topup` and preserves the callback query string.
	return "/wallet"
}

func redirectWalletAutoRechargeResult(c *gin.Context, policy *model.WalletAutoRecharge, result string) {
	tossRedirect(c, walletAutoRechargeRedirectBase(policy)+"?wallet_auto_recharge="+result)
}

func redirectWalletAutoRechargeFailure(c *gin.Context, policy *model.WalletAutoRecharge, code, message string) {
	values := url.Values{}
	values.Set("wallet_auto_recharge", "failed")
	if code = strings.TrimSpace(code); code != "" {
		values.Set("toss_error_code", trimTossFailureValue(code))
	}
	if message = strings.TrimSpace(message); message != "" {
		values.Set("toss_error_message", trimTossFailureValue(message))
	}
	tossRedirect(c, walletAutoRechargeRedirectBase(policy)+"?"+values.Encode())
}

func cancelWalletAutoRechargeAndRevoke(ctx context.Context, policy *model.WalletAutoRecharge) error {
	if policy == nil {
		return nil
	}
	billingKeyID, err := model.CancelWalletAutoRechargeAndGetBillingKey(policy.Id, policy.TargetType, policy.TargetId)
	if err != nil {
		return err
	}
	if billingKeyID > 0 {
		if err := walletAutoRechargeStoredBillingKeyRevoker(ctx, billingKeyID); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge billing key revoke queued policy=%d key=%d err=%v", policy.Id, billingKeyID, err))
		}
	}
	return nil
}

func cancelPendingWalletAutoRecharge(ctx context.Context, policy *model.WalletAutoRecharge) {
	if policy == nil || policy.Status != model.WalletAutoRechargeStatusPending {
		return
	}
	if err := cancelWalletAutoRechargeAndRevoke(ctx, policy); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("wallet auto recharge cancel failed trade_no=%s err=%v", policy.AuthTradeNo, err))
	}
}

func GetWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRecharges(target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func RequestWalletScheduledRecharge(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	requestWalletAutoRecharge(c, model.WalletAutoRechargeTypeScheduled, target)
}

func RequestWalletThresholdRecharge(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	requestWalletAutoRecharge(c, model.WalletAutoRechargeTypeThreshold, target)
}

func CancelWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	policy, err := model.GetWalletAutoRechargeForTarget(id, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer lockWalletAutoRechargeAuthTrade(policy)()
	policy, err = model.GetWalletAutoRechargeForTarget(id, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := cancelWalletAutoRechargeAndRevoke(c.Request.Context(), policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CancelPendingWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveUserWalletTarget(c)
	if !ok {
		return
	}
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	if tradeNo == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	policy, err := model.GetWalletAutoRechargeByTradeNoForTarget(tradeNo, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if policy.Status != model.WalletAutoRechargeStatusPending {
		common.ApiError(c, errors.New("wallet auto recharge is not pending"))
		return
	}
	if err := cancelWalletAutoRechargeAndRevoke(c.Request.Context(), policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func GetOrganizationWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	rows, err := model.ListWalletAutoRecharges(target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}

func RequestOrganizationWalletScheduledRecharge(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	requestWalletAutoRecharge(c, model.WalletAutoRechargeTypeScheduled, target)
}

func RequestOrganizationWalletThresholdRecharge(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	requestWalletAutoRecharge(c, model.WalletAutoRechargeTypeThreshold, target)
}

func CancelOrganizationWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	policy, err := model.GetWalletAutoRechargeForTarget(id, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer lockWalletAutoRechargeAuthTrade(policy)()
	policy, err = model.GetWalletAutoRechargeForTarget(id, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := cancelWalletAutoRechargeAndRevoke(c.Request.Context(), policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CancelPendingOrganizationWalletAutoRecharge(c *gin.Context) {
	target, ok := resolveOrganizationWalletTarget(c)
	if !ok {
		return
	}
	tradeNo := strings.TrimSpace(c.Param("trade_no"))
	if tradeNo == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	policy, err := model.GetWalletAutoRechargeByTradeNoForTarget(tradeNo, target.TargetType, target.TargetId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if policy.Status != model.WalletAutoRechargeStatusPending {
		common.ApiError(c, errors.New("wallet auto recharge is not pending"))
		return
	}
	if err := cancelWalletAutoRechargeAndRevoke(c.Request.Context(), policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func cancelClaimedWalletAutoRechargeBillingIssue(ctx context.Context, tradeNo, claimToken, reason string) error {
	if err := model.CancelClaimedWalletAutoRechargeBillingIssue(tradeNo, claimToken); err != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing issue cleanup failed trade_no=%s reason=%s err=%v", tradeNo, reason, err))
		return err
	}
	return nil
}

func processClaimedWalletAutoRechargeBillingIssue(
	ctx context.Context,
	policy *model.WalletAutoRecharge,
	claimToken, authKey, customerKey, secretKey string,
) (resolved bool, retainClaim bool, err error) {
	if policy == nil {
		return false, false, errors.New("wallet auto recharge policy is unavailable")
	}
	tradeNo := strings.TrimSpace(policy.AuthTradeNo)
	issueWasAttempted := policy.IssueAttempted
	cleanupOnly := policy.Status == model.WalletAutoRechargeStatusCancelPending
	cleanupReason := "cancel_pending_cleanup"
	transitionToCleanupOnly := func(reason string) error {
		if transitionErr := model.TransitionClaimedWalletAutoRechargeBillingIssueToCleanup(tradeNo, claimToken); transitionErr != nil {
			return transitionErr
		}
		policy.Status = model.WalletAutoRechargeStatusCancelPending
		cleanupOnly = true
		cleanupReason = reason
		return nil
	}
	cleanupIssuedKey := func(issued *tossBillingIssueResponse, reason string) error {
		// Reassert the claim-owned cleanup fence immediately before every local or
		// remote revocation. This covers workers that paused in the provider ISSUE
		// call while a newer lease owner attached the same idempotent key.
		if transitionErr := transitionToCleanupOnly(reason); transitionErr != nil {
			return transitionErr
		}
		if cleanupErr := walletAutoRechargeIssuedBillingKeyRevoker(
			ctx,
			policy.OwnerUserId,
			tradeNo,
			customerKey,
			issued,
			secretKey,
			reason,
		); cleanupErr != nil {
			return fmt.Errorf("durably queue wallet auto recharge billing key cleanup (%s): %w", reason, cleanupErr)
		}
		return nil
	}
	if policy.CustomerKey != customerKey {
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "customer_key_mismatch")
	}
	// A cancel_pending row is only expected after a provider ISSUE attempt. If
	// legacy/corrupt state says otherwise, finalizing it locally is safer than
	// creating a brand-new provider billing key merely so it can be deleted.
	if cleanupOnly && !issueWasAttempted {
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "unattempted_cleanup")
	}
	if tossBillingIssueAuthorizationExpired(policy.CreateTime) {
		if !policy.IssueAttempted {
			logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge billing authorization expired before provider request trade_no=%s policy=%d", tradeNo, policy.Id))
			return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "stale_authorization")
		}
		// The old request may already have created a key despite a lost response.
		// Move the policy out of usable state before repeating that exact request;
		// the result below is cleanup-only and can never activate or charge.
		if transitionErr := transitionToCleanupOnly("stale_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
	}
	if !cleanupOnly {
		owner, ownerErr := model.GetUserById(policy.OwnerUserId, false)
		if ownerErr != nil {
			return false, false, ownerErr
		}
		if owner == nil || owner.Status != common.UserStatusEnabled {
			if !policy.IssueAttempted {
				return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "owner_inactive")
			}
			if transitionErr := transitionToCleanupOnly("owner_inactive_cleanup"); transitionErr != nil {
				return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
			}
		}
	}
	if !cleanupOnly && !isTossWalletAutoRechargeOperationallyEnabled() {
		if !issueWasAttempted {
			return false, false, model.ErrTossBillingOperationallyDisabled
		}
		// The original idempotent ISSUE may have succeeded even though its
		// response was lost. Disabling the contract must not orphan that key:
		// repeat only the already-attempted request, then queue/delete the result
		// without ever activating the wallet policy.
		if transitionErr := transitionToCleanupOnly("billing_disabled_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
	}
	activeBillingClient := ""
	activeBillingSecret := ""
	if !cleanupOnly {
		tossConfig, err := model.GetFreshTossConfigSnapshot()
		if err != nil {
			return false, issueWasAttempted, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
		}
		activeBillingClient, activeBillingSecret = setting.TossActiveBillingKeyPairFromSnapshot(tossConfig)
	}
	if cleanupOnly && tossBillingIssueReplayMayBeOutsideIdempotencyWindow(policy.CreateTime) {
		logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge ISSUE cleanup exceeded provider idempotency window trade_no=%s policy=%d", tradeNo, policy.Id))
		return false, true, errTossBillingIssueIdempotencyWindowExpired
	}
	if err := model.MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, claimToken); err != nil {
		return false, false, err
	}
	policy.IssueAttempted = true
	if tossBillingIssueAuthorizationExpired(policy.CreateTime) && !cleanupOnly {
		if transitionErr := transitionToCleanupOnly("stale_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
	}
	if !cleanupOnly && !isTossWalletAutoRechargeOperationallyEnabled() {
		if !issueWasAttempted {
			return false, false, model.ErrTossBillingOperationallyDisabled
		}
		if transitionErr := transitionToCleanupOnly("billing_disabled_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
	}

	issueContext := model.WithTossWalletAutoRechargeIssueContext(ctx)
	if cleanupOnly {
		issueContext = model.WithTossBillingIssueCleanupContext(ctx)
	}
	issued, issueStatus, issueErr := walletAutoRechargeBillingKeyIssuer(
		issueContext, authKey, customerKey, tradeNo, secretKey,
	)
	// Only this process's first, explicitly rejected ISSUE request can advance to
	// a rotated secret. Persist the new same-MID namespace and reset its attempt
	// marker before retrying so a crash between the two provider calls remains
	// recoverable without treating an unmade request as ambiguous.
	if !issueWasAttempted && !cleanupOnly &&
		(issued == nil || strings.TrimSpace(issued.BillingKey) == "") &&
		isTossCredentialRejection(issueStatus, issueErr) {
		activeBillingClient = strings.TrimSpace(activeBillingClient)
		activeBillingSecret = strings.TrimSpace(activeBillingSecret)
		if activeBillingClient != "" && activeBillingSecret != "" && activeBillingSecret != strings.TrimSpace(secretKey) &&
			model.IsValidTossClientKeyFingerprint(policy.ProviderClientKeyHash) &&
			policy.ProviderClientKeyHash == model.TossBillingClientKeyFingerprint(activeBillingClient) {
			if promotionErr := model.PromoteClaimedWalletAutoRechargeBillingIssueCredentialAfterRejection(
				tradeNo,
				claimToken,
				secretKey,
				activeBillingClient,
				activeBillingSecret,
			); promotionErr != nil {
				if errors.Is(promotionErr, model.ErrWalletAutoRechargeClaimLost) {
					reloaded, reloadErr := loadWalletAutoRechargeByTradeNo(tradeNo)
					if reloadErr == nil && reloaded.Status == model.WalletAutoRechargeStatusCancelPending && reloaded.IssueClaimToken == claimToken {
						return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "cancelled_during_credential_rotation")
					}
				}
				return false, !errors.Is(promotionErr, model.ErrWalletAutoRechargeClaimLost), promotionErr
			}
			secretKey = activeBillingSecret
			policy.IssueAttempted = false
			if tossBillingIssueAuthorizationExpired(policy.CreateTime) {
				return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "stale_authorization_after_credential_rotation")
			}
			if !isTossWalletAutoRechargeOperationallyEnabled() {
				return false, false, model.ErrTossBillingOperationallyDisabled
			}
			if err := model.RequireFreshTossConfig(); err != nil {
				return false, false, fmt.Errorf("%w: %v", model.ErrTossBillingOperationallyDisabled, err)
			}
			if err := model.MarkWalletAutoRechargeBillingIssueAttempt(tradeNo, claimToken); err != nil {
				return false, false, err
			}
			policy.IssueAttempted = true
			if tossBillingIssueAuthorizationExpired(policy.CreateTime) {
				return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "stale_authorization_after_credential_rotation")
			}
			if !isTossWalletAutoRechargeOperationallyEnabled() {
				return false, false, model.ErrTossBillingOperationallyDisabled
			}
			issued, issueStatus, issueErr = walletAutoRechargeBillingKeyIssuer(
				model.WithTossWalletAutoRechargeIssueContext(ctx), authKey, customerKey, tradeNo, secretKey,
			)
		}
	}
	if issueErr != nil || issued == nil || strings.TrimSpace(issued.BillingKey) == "" || issued.CustomerKey != customerKey {
		if isTossBillingIssueRecoveryUncertain(issued, issueStatus, issueErr, issueWasAttempted) {
			logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: wallet auto recharge billing issue response uncertain trade_no=%s policy=%d status=%d err=%v", tradeNo, policy.Id, issueStatus, issueErr))
			if !issueWasAttempted {
				model.RecordLog(policy.OwnerUserId, model.LogTypeTopup, fmt.Sprintf("Toss wallet auto recharge billing-key issue uncertain; automatic same-order recovery scheduled (trade_no=%s)", tradeNo))
			}
			if issueErr == nil {
				issueErr = errors.New("wallet auto recharge billing issue returned an uncertain empty response")
			}
			return false, true, issueErr
		}
		if issued != nil && strings.TrimSpace(issued.BillingKey) != "" {
			if cleanupErr := cleanupIssuedKey(issued, "issue_response_invalid"); cleanupErr != nil {
				return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
			}
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "provider_rejected_issue")
	}
	if cleanupOnly {
		if cleanupErr := cleanupIssuedKey(issued, cleanupReason); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, cleanupReason)
	}
	if tossBillingIssueAuthorizationExpired(policy.CreateTime) {
		if transitionErr := transitionToCleanupOnly("stale_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
		if cleanupErr := cleanupIssuedKey(issued, cleanupReason); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, cleanupReason)
	}
	if !isTossWalletAutoRechargeOperationallyEnabled() {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge billing key issued after billing was disabled trade_no=%s policy=%d", tradeNo, policy.Id))
		if cleanupErr := cleanupIssuedKey(issued, "billing_disabled_after_issue"); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "billing_disabled_after_issue")
	}

	// Re-read eligibility after the remote operation. The owner or organization
	// may have been disabled while Toss was issuing the key. Transient DB errors
	// retain the encrypted issue snapshot; a definitive eligibility loss revokes
	// the just-issued provider key and releases the pending policy slot.
	reloaded, reloadErr := loadWalletAutoRechargeByTradeNo(tradeNo)
	if reloadErr != nil {
		return false, true, fmt.Errorf("reload wallet auto recharge after billing-key issue: %w", reloadErr)
	}
	policy = reloaded
	if policy.Status == model.WalletAutoRechargeStatusCancelPending {
		if cleanupErr := cleanupIssuedKey(issued, "cancel_pending_after_issue"); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "cancel_pending_after_issue")
	}
	if targetErr := model.ValidateWalletAutoRechargeTarget(policy.Id); targetErr != nil {
		if !errors.Is(targetErr, model.ErrWalletAutoRechargeTargetInvalid) {
			return false, true, fmt.Errorf("revalidate wallet auto recharge after billing-key issue: %w", targetErr)
		}
		if cleanupErr := cleanupIssuedKey(issued, "target_inactive_after_issue"); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "target_inactive_after_issue")
	}
	if !isTossWalletAutoRechargeOperationallyEnabled() {
		if cleanupErr := cleanupIssuedKey(issued, "billing_disabled_before_activation"); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "billing_disabled_before_activation")
	}
	if tossBillingIssueAuthorizationExpired(policy.CreateTime) {
		if transitionErr := transitionToCleanupOnly("stale_issue_cleanup"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), transitionErr
		}
		if cleanupErr := cleanupIssuedKey(issued, cleanupReason); cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), cleanupErr
		}
		return true, false, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, cleanupReason)
	}

	activated, billingKeyID, activationErr := model.StoreAndActivateClaimedWalletAutoRechargeFromToss(
		tradeNo,
		claimToken,
		customerKey,
		issued.BillingKey,
		issued.cardCompanyForStorage(),
		issued.cardNumberForStorage(),
		secretKey,
		policy.ProviderClientKeyHash,
		false,
		walletAutoRechargeTossCharger,
	)
	if activationErr != nil {
		if activated != nil && activated.Status == model.WalletAutoRechargeStatusActive {
			// Billing-key activation succeeded. Any immediate-charge error is now
			// handled by the durable provider/local settlement path.
			logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge activated with immediate settlement pending trade_no=%s policy=%d err=%v", tradeNo, activated.Id, activationErr))
			return true, false, nil
		}
		if errors.Is(activationErr, model.ErrWalletAutoRechargeClaimLost) {
			// A newer claim owner may be recovering the exact same idempotent
			// provider key. The atomic model transaction did not leave an unlinked
			// local row, so do not delete the provider object out from under it.
			return false, false, activationErr
		}
		if transitionErr := transitionToCleanupOnly("activation_failed"); transitionErr != nil {
			return false, !errors.Is(transitionErr, model.ErrWalletAutoRechargeClaimLost), errors.Join(activationErr, transitionErr)
		}
		cleanupErr := error(nil)
		if billingKeyID > 0 {
			cleanupErr = walletAutoRechargeStoredBillingKeyRevoker(ctx, billingKeyID)
			if cleanupErr != nil {
				logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge stored-key cleanup failed; queuing issued key trade_no=%s key=%d err=%v", tradeNo, billingKeyID, cleanupErr))
			}
		}
		if billingKeyID <= 0 || cleanupErr != nil {
			cleanupErr = cleanupIssuedKey(issued, "activation_failed")
		}
		if cleanupErr != nil {
			return false, !errors.Is(cleanupErr, model.ErrWalletAutoRechargeClaimLost), errors.Join(activationErr, cleanupErr)
		}
		_ = cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "activation_failed")
		return true, false, activationErr
	}
	return true, false, nil
}

func reconcileWalletAutoRechargeBillingIssue(ctx context.Context, candidate model.WalletAutoRecharge) (resolved bool, err error) {
	tradeNo := strings.TrimSpace(candidate.AuthTradeNo)
	if tradeNo == "" {
		return true, nil
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	policy, err := loadWalletAutoRechargeByTradeNo(tradeNo)
	if err != nil {
		return false, err
	}
	if policy.Status == model.WalletAutoRechargeStatusActive || policy.Status == model.WalletAutoRechargeStatusCancelled || policy.Status == model.WalletAutoRechargeStatusFailed {
		return true, nil
	}
	claimToken, claimed, err := model.ClaimStoredWalletAutoRechargeBillingIssue(tradeNo)
	if err != nil || !claimed {
		return false, err
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseWalletAutoRechargeBillingIssueClaim(tradeNo, claimToken); err == nil && releaseErr != nil {
				err = releaseErr
			}
		}
	}()
	authKey, customerKey, secretKey, snapshotErr := model.GetClaimedWalletAutoRechargeBillingIssue(tradeNo, claimToken)
	if snapshotErr != nil {
		if errors.Is(snapshotErr, model.ErrTossBillingIssueSnapshotInvalid) {
			claimedPolicy, stateErr := model.GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, claimToken)
			if stateErr != nil {
				retainClaim = true
				return false, errors.Join(snapshotErr, stateErr)
			}
			if claimedPolicy.Status == model.WalletAutoRechargeStatusCancelPending || claimedPolicy.IssueAttempted {
				// The cleanup authorization is the only durable way to discover an
				// issue response that may have been lost. Never clear a corrupt cleanup
				// or attempted snapshot silently; retain it for a node with the repaired
				// crypto key or explicit operator reconciliation.
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: attempted wallet billing issue snapshot is unreadable trade_no=%s policy=%d; preserving authorization and claim", tradeNo, claimedPolicy.Id))
				return false, snapshotErr
			}
			return true, cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "invalid_snapshot")
		}
		return false, snapshotErr
	}
	claimedPolicy, stateErr := model.GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, claimToken)
	if stateErr != nil {
		return false, stateErr
	}
	resolved, retainClaim, err = processClaimedWalletAutoRechargeBillingIssue(ctx, claimedPolicy, claimToken, authKey, customerKey, secretKey)
	return resolved, err
}

func WalletAutoRechargeTossConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	tradeNo := strings.TrimSpace(c.Query("trade_no"))
	authKey := c.Query("authKey")
	customerKey := strings.TrimSpace(c.Query("customerKey"))
	if !isValidTossOrderId(tradeNo) || authKey == "" || len(authKey) > model.TossBillingIssueAuthKeyMaxBytes ||
		customerKey == "" || len(customerKey) > model.TossBillingIssueCustomerKeyMaxBytes {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge confirm missing params trade_no=%q", tradeNo))
		redirectWalletAutoRechargeResult(c, nil, "failed")
		return
	}
	if err := model.ValidateTossBillingCryptoConfiguration(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge confirm blocked by unsafe credential encryption trade_no=%s err=%v", tradeNo, err))
		redirectWalletAutoRechargeResult(c, nil, "failed")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	policy, err := loadWalletAutoRechargeByTradeNo(tradeNo)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge policy missing trade_no=%s err=%v", tradeNo, err))
		redirectWalletAutoRechargeResult(c, nil, "failed")
		return
	}
	if policy.Status == model.WalletAutoRechargeStatusActive {
		redirectWalletAutoRechargeResult(c, policy, "success")
		return
	}
	if policy.Status == model.WalletAutoRechargeStatusCancelled || policy.Status == model.WalletAutoRechargeStatusFailed {
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	if policy.CustomerKey != customerKey {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge customerKey mismatch trade_no=%s", tradeNo))
		// Browser-controlled callback data must not destroy a valid durable issue
		// snapshot or free the policy slot for a second authorization.
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	claimToken, claimed, err := model.ClaimWalletAutoRechargeBillingIssue(tradeNo, authKey, customerKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing issue claim failed trade_no=%s err=%v", tradeNo, err))
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	if !claimed {
		policy, _ = loadWalletAutoRechargeByTradeNo(tradeNo)
		if policy != nil && policy.Status == model.WalletAutoRechargeStatusActive {
			redirectWalletAutoRechargeResult(c, policy, "success")
			return
		}
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	retainClaim := false
	defer func() {
		if !retainClaim {
			if releaseErr := model.ReleaseWalletAutoRechargeBillingIssueClaim(tradeNo, claimToken); releaseErr != nil {
				logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing issue claim release failed trade_no=%s err=%v", tradeNo, releaseErr))
			}
		}
	}()
	storedAuthKey, storedCustomerKey, storedSecretKey, snapshotErr := model.GetClaimedWalletAutoRechargeBillingIssue(tradeNo, claimToken)
	if snapshotErr != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing issue snapshot load failed trade_no=%s err=%v", tradeNo, snapshotErr))
		if errors.Is(snapshotErr, model.ErrTossBillingIssueSnapshotInvalid) {
			claimedPolicy, stateErr := model.GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, claimToken)
			if stateErr != nil {
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("wallet auto recharge claimed state reload failed trade_no=%s err=%v", tradeNo, stateErr))
				redirectWalletAutoRechargeResult(c, policy, "failed")
				return
			}
			if claimedPolicy.Status == model.WalletAutoRechargeStatusCancelPending || claimedPolicy.IssueAttempted {
				retainClaim = true
				logger.LogError(ctx, fmt.Sprintf("TOSS RECONCILIATION REQUIRED: attempted wallet billing issue snapshot is unreadable trade_no=%s policy=%d; preserving authorization and claim", tradeNo, claimedPolicy.Id))
			} else {
				_ = cancelClaimedWalletAutoRechargeBillingIssue(ctx, tradeNo, claimToken, "invalid_snapshot")
			}
		}
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	claimedPolicy, stateErr := model.GetClaimedWalletAutoRechargeBillingIssueState(tradeNo, claimToken)
	if stateErr != nil {
		retainClaim = true
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge claimed state reload failed trade_no=%s err=%v", tradeNo, stateErr))
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	policy = claimedPolicy
	_, retainClaim, err = processClaimedWalletAutoRechargeBillingIssue(ctx, claimedPolicy, claimToken, storedAuthKey, storedCustomerKey, storedSecretKey)
	if err != nil && !retainClaim {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing issue processing failed trade_no=%s err=%v", tradeNo, err))
	}
	reloaded, reloadErr := loadWalletAutoRechargeByTradeNo(tradeNo)
	if reloadErr == nil {
		policy = reloaded
	}
	if policy != nil && policy.Status == model.WalletAutoRechargeStatusActive {
		redirectWalletAutoRechargeResult(c, policy, "success")
		return
	}
	redirectWalletAutoRechargeResult(c, policy, "failed")
}

func WalletAutoRechargeTossFail(c *gin.Context) {
	tradeNo := strings.TrimSpace(c.Query("trade_no"))
	code := c.Query("code")
	message := c.Query("message")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("wallet auto recharge billing auth failed trade_no=%q code=%s", tradeNo, sanitizeTossAPIErrorCode(code)))
	if !isValidTossOrderId(tradeNo) {
		redirectWalletAutoRechargeFailure(c, nil, code, message)
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	policy, err := loadWalletAutoRechargeByTradeNo(tradeNo)
	if err != nil {
		redirectWalletAutoRechargeFailure(c, nil, code, message)
		return
	}
	providerActive, activityErr := model.HasWalletAutoRechargeProviderActivity(policy)
	if activityErr != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("wallet auto recharge fail callback activity check failed trade_no=%s err=%v", tradeNo, activityErr))
		redirectWalletAutoRechargeFailure(c, policy, code, message)
		return
	}
	if policy.Status != model.WalletAutoRechargeStatusPending || providerActive {
		redirectWalletAutoRechargeFailure(c, policy, code, message)
		return
	}
	cancelPendingWalletAutoRecharge(c.Request.Context(), policy)
	redirectWalletAutoRechargeFailure(c, policy, code, message)
}
