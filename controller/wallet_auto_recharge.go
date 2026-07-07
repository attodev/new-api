package controller

import (
	"context"
	"fmt"
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
	PresetId int `json:"preset_id"`
}

type walletAutoRechargeTossResponse struct {
	ClientKey   string `json:"client_key"`
	CustomerKey string `json:"customer_key"`
	TradeNo     string `json:"trade_no"`
	SuccessURL  string `json:"success_url"`
	FailURL     string `json:"fail_url"`
}

type walletRechargeTarget struct {
	TargetType  string
	TargetId    int
	OwnerUserId int
}

var walletAutoRechargeBillingKeyIssuer = issueTossBillingKey

var walletAutoRechargeTossCharger model.TossBillingCharger = tossBillingChargeForModel

func resolveUserWalletTarget(c *gin.Context) (walletRechargeTarget, bool) {
	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		common.ApiError(c, err)
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
	if !isTossBillingEnabled() {
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}
	if !isValidServerAddress(system_setting.ServerAddress) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("wallet auto recharge blocked: invalid ServerAddress=%q", system_setting.ServerAddress))
		common.ApiErrorI18n(c, i18n.MsgPaymentNotConfigured)
		return
	}

	var req walletAutoRechargeRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if req.PresetId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	preset, err := model.GetWalletAutoRechargePresetForTarget(req.PresetId, policyType, target.TargetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	customerKey, err := model.GetOrCreateTossCustomerKey(target.OwnerUserId)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}

	reference := fmt.Sprintf("wallet_auto_auth_%d_%d_%s", target.OwnerUserId, time.Now().UnixMilli(), randstr.String(4))
	policy, err := model.CreatePendingWalletAutoRecharge(model.CreateWalletAutoRechargeRequest{
		PresetId:          preset.Id,
		Type:              policyType,
		TargetType:        target.TargetType,
		TargetId:          target.TargetId,
		OwnerUserId:       target.OwnerUserId,
		CustomerKey:       customerKey,
		AuthTradeNo:       reference,
		Amount:            preset.Amount,
		ThresholdAmount:   preset.ThresholdAmount,
		ThresholdQuota:    preset.ThresholdQuota,
		IntervalUnit:      preset.IntervalUnit,
		IntervalValue:     preset.IntervalValue,
		CustomSeconds:     preset.CustomSeconds,
		ChargeImmediately: preset.ChargeImmediately,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	common.ApiSuccess(c, walletAutoRechargeTossResponse{
		ClientKey:   setting.TossActiveClientKey(),
		CustomerKey: customerKey,
		TradeNo:     policy.AuthTradeNo,
		SuccessURL:  base + "/api/wallet/auto-recharge/toss/confirm?trade_no=" + policy.AuthTradeNo,
		FailURL:     base + "/api/wallet/auto-recharge/toss/fail?trade_no=" + policy.AuthTradeNo,
	})
}

func loadWalletAutoRechargeByTradeNo(tradeNo string) (*model.WalletAutoRecharge, error) {
	var policy model.WalletAutoRecharge
	if err := model.DB.Where("auth_trade_no = ?", tradeNo).First(&policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func walletAutoRechargeRedirectBase(policy *model.WalletAutoRecharge) string {
	if policy != nil && policy.TargetType == model.TopUpTargetTypeOrganization {
		return "/organization/wallet"
	}
	return "/wallet"
}

func redirectWalletAutoRechargeResult(c *gin.Context, policy *model.WalletAutoRecharge, result string) {
	tossRedirect(c, walletAutoRechargeRedirectBase(policy)+"?wallet_auto_recharge="+result)
}

func cancelPendingWalletAutoRecharge(policy *model.WalletAutoRecharge) {
	if policy == nil || policy.Status != model.WalletAutoRechargeStatusPending {
		return
	}
	if err := model.CancelWalletAutoRecharge(policy.Id, policy.TargetType, policy.TargetId); err != nil {
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
	if err := model.CancelWalletAutoRecharge(id, target.TargetType, target.TargetId); err != nil {
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
	if err := model.CancelPendingWalletAutoRechargeByTradeNo(tradeNo, target.TargetType, target.TargetId); err != nil {
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
	if err := model.CancelWalletAutoRecharge(id, target.TargetType, target.TargetId); err != nil {
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
	if err := model.CancelPendingWalletAutoRechargeByTradeNo(tradeNo, target.TargetType, target.TargetId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func WalletAutoRechargeTossConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	tradeNo := c.Query("trade_no")
	authKey := c.Query("authKey")
	customerKey := c.Query("customerKey")
	if tradeNo == "" || authKey == "" || customerKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("wallet auto recharge confirm missing params trade_no=%q", tradeNo))
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
		cancelPendingWalletAutoRecharge(policy)
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}

	issued, _, err := walletAutoRechargeBillingKeyIssuer(ctx, authKey, customerKey, tradeNo)
	if err != nil || issued == nil || issued.BillingKey == "" {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge billing key issue failed trade_no=%s err=%v", tradeNo, err))
		cancelPendingWalletAutoRecharge(policy)
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}
	secretKey := setting.TossActiveBillingSecretKey()

	billingKeyID, err := model.StoreTossBillingKeyWithSecret(policy.OwnerUserId, customerKey, issued.BillingKey, issued.cardCompanyForStorage(), issued.cardNumberForStorage(), secretKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge store billing key failed trade_no=%s err=%v", tradeNo, err))
		cancelPendingWalletAutoRecharge(policy)
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}

	if _, err := model.ActivateWalletAutoRechargeFromToss(tradeNo, billingKeyID, issued.cardCompanyForStorage(), issued.cardNumberForStorage(), false, walletAutoRechargeTossCharger); err != nil {
		logger.LogError(ctx, fmt.Sprintf("wallet auto recharge activation failed trade_no=%s err=%v", tradeNo, err))
		redirectWalletAutoRechargeResult(c, policy, "failed")
		return
	}

	redirectWalletAutoRechargeResult(c, policy, "success")
}

func WalletAutoRechargeTossFail(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	code := c.Query("code")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("wallet auto recharge billing auth failed trade_no=%s code=%s", tradeNo, code))
	if tradeNo == "" {
		redirectWalletAutoRechargeResult(c, nil, "failed")
		return
	}
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)

	policy, err := loadWalletAutoRechargeByTradeNo(tradeNo)
	if err != nil {
		redirectWalletAutoRechargeResult(c, nil, "failed")
		return
	}
	cancelPendingWalletAutoRecharge(policy)
	redirectWalletAutoRechargeResult(c, policy, "failed")
}
