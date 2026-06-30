package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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

func init() {
	model.SetTossBillingCharger(func(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (bool, int64, error) {
		res, _, err := chargeTossBilling(ctx, billingKey, customerKey, orderId, orderName, amount)
		if err != nil {
			return false, 0, err
		}
		return res.Status == "DONE", res.TotalAmount, nil
	})
}

// tossBillingIssueResponse is the response from /v1/billing/authorizations/issue.
type tossBillingIssueResponse struct {
	BillingKey  string `json:"billingKey"`
	CustomerKey string `json:"customerKey"`
	Card        struct {
		Company string `json:"company"`
		Number  string `json:"number"`
	} `json:"card"`
}

// tossSubscriptionChargeKRW converts a plan's USD-equivalent price to KRW for Toss.
// Delegates to model.TossPlanKRW as the single conversion source.
func tossSubscriptionChargeKRW(plan *model.SubscriptionPlan) int64 {
	return model.TossPlanKRW(plan.PriceAmount)
}

// issueTossBillingKey exchanges an authKey for a billing key.
func issueTossBillingKey(ctx context.Context, authKey, customerKey string) (*tossBillingIssueResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload := map[string]interface{}{"authKey": authKey, "customerKey": customerKey}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/billing/authorizations/issue", strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	cred := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+cred)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("toss billing issue failed: status=%d body=%s", resp.StatusCode, string(rb))
	}
	var out tossBillingIssueResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
}

// chargeTossBilling charges a billing key. Reuses tossConfirmResponse (status/totalAmount/orderId/currency).
func chargeTossBilling(ctx context.Context, billingKey, customerKey, orderId, orderName string, amount int64) (*tossConfirmResponse, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tossAPIBase+"/v1/billing/"+billingKey, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	cred := base64.StdEncoding.EncodeToString([]byte(setting.TossActiveSecretKey() + ":"))
	req.Header.Set("Authorization", "Basic "+cred)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", orderId)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("toss billing charge failed: status=%d body=%s", resp.StatusCode, string(rb))
	}
	var out tossConfirmResponse
	if err := common.Unmarshal(rb, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
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
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         tradeNo,
		PaymentMethod:   model.PaymentMethodToss,
		PaymentProvider: model.PaymentProviderToss,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		common.ApiErrorI18n(c, i18n.MsgPaymentCreateFailed)
		return
	}
	base := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key":   setting.TossActiveClientKey(),
			"customer_key": customerKey,
			"trade_no":     tradeNo,
			"success_url":  base + "/api/subscription/toss/confirm?trade_no=" + tradeNo,
			"fail_url":     base + "/api/subscription/toss/fail?trade_no=" + tradeNo,
		},
	})
}

// SubscriptionTossBillingConfirm is the billingAuth successUrl: issues + stores the
// billing key, charges the first period, and activates the subscription.
func SubscriptionTossBillingConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	authKey := c.Query("authKey")
	customerKey := c.Query("customerKey")
	tradeNo := c.Query("trade_no")
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
	plan, err := model.GetSubscriptionPlanById(order.PlanId)
	if err != nil {
		tossRedirect(c, "/console/topup")
		return
	}

	issued, _, err := issueTossBillingKey(ctx, authKey, customerKey)
	if err != nil || issued.BillingKey == "" {
		logger.LogError(ctx, fmt.Sprintf("Toss billing issue failed trade_no=%s err=%v", tradeNo, err))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}
	billingKeyId, err := model.StoreTossBillingKey(order.UserId, customerKey, issued.BillingKey, issued.Card.Company, issued.Card.Number)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing store failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}

	chargeKRW := tossSubscriptionChargeKRW(plan)
	orderName := fmt.Sprintf("%s 구독", plan.Title)
	result, _, err := chargeTossBilling(ctx, issued.BillingKey, customerKey, tradeNo, orderName, chargeKRW)
	if err != nil || result.Status != "DONE" || result.TotalAmount != chargeKRW {
		logger.LogWarn(ctx, fmt.Sprintf("Toss billing first charge not done trade_no=%s err=%v", tradeNo, err))
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
		tossRedirect(c, "/console/topup")
		return
	}

	payload, _ := common.Marshal(result)
	if err := model.CompleteTossBillingOrder(tradeNo, billingKeyId, string(payload)); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss billing complete order failed trade_no=%s err=%v", tradeNo, err))
		tossRedirect(c, "/console/topup")
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss subscription activated trade_no=%s plan=%d user=%d", tradeNo, plan.Id, order.UserId))
	tossRedirect(c, "/console/topup")
}

// SubscriptionTossBillingFail is the billingAuth failUrl.
func SubscriptionTossBillingFail(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	code := c.Query("code")
	logger.LogWarn(c.Request.Context(), fmt.Sprintf("Toss billing auth failed trade_no=%s code=%s", tradeNo, code))
	if tradeNo != "" {
		_ = model.ExpireSubscriptionOrder(tradeNo, model.PaymentProviderToss)
	}
	tossRedirect(c, "/console/topup")
}

// CancelTossAutoRenew disables auto-renew for the user's active Toss subscriptions and revokes the key.
func CancelTossAutoRenew(c *gin.Context) {
	userId := c.GetInt("id")
	if err := model.CancelTossAutoRenewForUser(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
