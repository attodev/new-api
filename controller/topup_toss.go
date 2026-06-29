package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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
	Method      string `json:"method"`
	ApprovedAt  string `json:"approvedAt"`
}

// getTossPayMoney returns the KRW amount to charge for the given entered amount.
func getTossPayMoney(amountKRW int64, group string) int64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	return decimal.NewFromInt(amountKRW).Mul(decimal.NewFromFloat(ratio)).Round(0).IntPart()
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

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"client_key": setting.TossActiveClientKey(),
			"order_id":   orderId,
			"order_name": fmt.Sprintf("크레딧 충전 %d원", chargedKRW),
			"amount":     chargedKRW,
			"success_url": system_setting.ServerAddress + "/api/toss/confirm",
			"fail_url":    system_setting.ServerAddress + "/api/toss/fail",
		},
	})
}

// confirmTossPayment calls Toss POST /v1/payments/confirm.
func confirmTossPayment(ctx context.Context, paymentKey, orderId string, amount int64) (*tossConfirmResponse, error) {
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

	result, err := confirmTossPayment(ctx, paymentKey, orderId, amount)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss confirm API failed order_id=%s error=%q", orderId, err.Error()))
		tossRedirect(c, "/console/topup")
		return
	}
	if result.Status != "DONE" || result.TotalAmount != amount {
		logger.LogWarn(ctx, fmt.Sprintf("Toss confirm not done order_id=%s status=%s total=%d", orderId, result.Status, result.TotalAmount))
		tossRedirect(c, "/console/topup")
		return
	}

	if err := model.RechargeToss(orderId, c.ClientIP()); err != nil {
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
		_ = model.UpdatePendingTopUpStatus(orderId, model.PaymentProviderToss, common.TopUpStatusFailed)
	}
	tossRedirect(c, "/console/topup")
}

// TossWebhook handles async settlement notifications (e.g. virtual account DONE).
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

	if event.Data.Status != "DONE" || event.Data.OrderId == "" {
		c.Status(http.StatusOK)
		return
	}

	orderId := event.Data.OrderId
	LockOrder(orderId)
	defer UnlockOrder(orderId)

	topUp := model.GetTopUpByTradeNo(orderId)
	if err := validateTossConfirm(topUp, orderId, event.Data.TotalAmount); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook validation failed error=%q", err.Error()))
		c.Status(http.StatusOK)
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		c.Status(http.StatusOK)
		return
	}

	// The webhook payload is unauthenticated, so never credit based on it.
	// Re-fetch the authoritative payment object from Toss and credit only if
	// Toss itself confirms a DONE payment for this exact order and amount.
	if event.Data.PaymentKey == "" {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook missing paymentKey order_id=%s", orderId))
		c.Status(http.StatusOK)
		return
	}
	auth, err := getTossPayment(ctx, event.Data.PaymentKey)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Toss webhook get payment failed order_id=%s error=%q", orderId, err.Error()))
		c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
		return
	}
	if auth.Status != "DONE" || auth.TotalAmount != topUp.Amount || auth.OrderId != orderId {
		logger.LogWarn(ctx, fmt.Sprintf("Toss webhook authoritative mismatch order_id=%s status=%s total=%d auth_order=%s", orderId, auth.Status, auth.TotalAmount, auth.OrderId))
		c.Status(http.StatusOK)
		return
	}

	if err := model.RechargeToss(orderId, c.ClientIP()); err != nil {
		c.Status(http.StatusServiceUnavailable) // Toss 재시도 유도
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("Toss recharge succeeded order_id=%s", orderId))
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
