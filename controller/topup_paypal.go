package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

const (
	paypalProductionBase = "https://api-m.paypal.com"
	paypalSandboxBase    = "https://api-m.sandbox.paypal.com"
)

type paypalTokenCache struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

var ppTokenCache = &paypalTokenCache{}

type PayPalPayRequest struct {
	Amount        int64  `json:"amount"`
	PaymentMethod string `json:"payment_method"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
}

type paypalLink struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

type paypalOrderResponse struct {
	ID            string       `json:"id"`
	Status        string       `json:"status"`
	Links         []paypalLink `json:"links"`
	PurchaseUnits []struct {
		ReferenceID string `json:"reference_id"`
	} `json:"purchase_units"`
}

type paypalTokenResponse struct {
	AccessToken string  `json:"access_token"`
	ExpiresIn   float64 `json:"expires_in"`
}

func getPayPalBaseURL() string {
	if setting.PayPalSandbox {
		return paypalSandboxBase
	}
	return paypalProductionBase
}

func getPayPalAccessToken(ctx context.Context) (string, error) {
	ppTokenCache.mu.Lock()
	defer ppTokenCache.mu.Unlock()

	if ppTokenCache.token != "" && time.Now().Before(ppTokenCache.expiresAt) {
		return ppTokenCache.token, nil
	}

	baseURL := getPayPalBaseURL()
	credentials := base64.StdEncoding.EncodeToString([]byte(setting.PayPalClientId + ":" + setting.PayPalClientSecret))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+credentials)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("PayPal token 获取失败: status=%d body=%s", resp.StatusCode, string(body))
	}

	var tokenResp paypalTokenResponse
	if err := common.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("PayPal token 响应缺少 access_token")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 32400
	}
	ppTokenCache.token = tokenResp.AccessToken
	ppTokenCache.expiresAt = time.Now().Add(time.Duration(expiresIn-60) * time.Second)

	return ppTokenCache.token, nil
}

// createPayPalOrder creates a PayPal order and returns (paypalOrderID, approvalURL, error).
func createPayPalOrder(ctx context.Context, referenceId string, amountUSD float64, returnURL string, cancelURL string) (string, string, error) {
	token, err := getPayPalAccessToken(ctx)
	if err != nil {
		return "", "", fmt.Errorf("获取PayPal token失败: %w", err)
	}

	// Append ref to return URL so we can identify the order on capture
	if strings.Contains(returnURL, "?") {
		returnURL += "&ref=" + referenceId
	} else {
		returnURL += "?ref=" + referenceId
	}

	payload := map[string]interface{}{
		"intent": "CAPTURE",
		"purchase_units": []map[string]interface{}{
			{
				"reference_id": referenceId,
				"amount": map[string]interface{}{
					"currency_code": "USD",
					"value":         fmt.Sprintf("%.2f", amountUSD),
				},
			},
		},
		"payment_source": map[string]interface{}{
			"paypal": map[string]interface{}{
				"experience_context": map[string]interface{}{
					"payment_method_preference": "IMMEDIATE_PAYMENT_REQUIRED",
					"user_action":               "PAY_NOW",
					"return_url":                returnURL,
					"cancel_url":                cancelURL,
				},
			},
		},
	}

	bodyBytes, err := common.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	baseURL := getPayPalBaseURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/checkout/orders", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("PayPal 创建订单失败: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var order paypalOrderResponse
	if err := common.Unmarshal(respBody, &order); err != nil {
		return "", "", err
	}

	for _, link := range order.Links {
		if link.Rel == "payer-action" || link.Rel == "approve" {
			return order.ID, link.Href, nil
		}
	}

	return "", "", fmt.Errorf("PayPal 订单响应中未找到支付链接")
}

// capturePayPalOrder captures a PayPal order by its PayPal order ID.
func capturePayPalOrder(ctx context.Context, paypalOrderId string) (*paypalOrderResponse, error) {
	token, err := getPayPalAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取PayPal token失败: %w", err)
	}

	baseURL := getPayPalBaseURL()
	url := fmt.Sprintf("%s/v2/checkout/orders/%s/capture", baseURL, paypalOrderId)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PayPal 捕获订单失败: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var order paypalOrderResponse
	if err := common.Unmarshal(respBody, &order); err != nil {
		return nil, err
	}

	return &order, nil
}

func RequestPayPalPay(c *gin.Context) {
	var req PayPalPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	if req.PaymentMethod != model.PaymentMethodPayPal {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}

	minTopup := getPayPalMinTopup()
	if req.Amount < minTopup {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": fmt.Sprintf("充值数量不能小于 %d", minTopup)})
		return
	}

	if req.Amount > 10000 {
		c.JSON(http.StatusOK, gin.H{"message": "充值数量不能大于 10000", "data": 10})
		return
	}

	if req.SuccessURL != "" && common.ValidateRedirectURL(req.SuccessURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付成功重定向URL不在可信任域名列表中", "data": ""})
		return
	}

	if req.CancelURL != "" && common.ValidateRedirectURL(req.CancelURL) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "支付取消重定向URL不在可信任域名列表中", "data": ""})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	reference := fmt.Sprintf("new-api-paypal-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ppl_" + common.Sha1([]byte(reference))

	chargedMoney := getPayPalPayMoney(float64(req.Amount), user.Group)
	if chargedMoney <= 0.01 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "充值金额过低"})
		return
	}

	captureURL := system_setting.ServerAddress + "/api/paypal/capture"
	cancelURL := system_setting.ServerAddress + "/console/topup"
	if req.SuccessURL != "" {
		captureURL = req.SuccessURL
	}
	if req.CancelURL != "" {
		cancelURL = req.CancelURL
	}

	paypalOrderId, approvalURL, err := createPayPalOrder(c.Request.Context(), referenceId, chargedMoney, captureURL, cancelURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("PayPal 创建订单失败 user_id=%d trade_no=%s amount=%d error=%q", id, referenceId, req.Amount, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          req.Amount,
		Money:           chargedMoney,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodPayPal,
		PaymentProvider: model.PaymentProviderPayPal,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err = topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("PayPal 创建充值订单失败 user_id=%d trade_no=%s paypal_order=%s error=%q", id, referenceId, paypalOrderId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("PayPal 充值订单创建成功 user_id=%d trade_no=%s paypal_order=%s amount=%d money=%.2f", id, referenceId, paypalOrderId, req.Amount, chargedMoney))
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"pay_link": approvalURL,
		},
	})
}

// PayPalCapture handles the redirect back from PayPal after user approves.
// PayPal appends ?token={paypalOrderId}&PayerID={payerId} to the return URL.
// Our return URL also includes ?ref={referenceId}.
func PayPalCapture(c *gin.Context) {
	ctx := c.Request.Context()
	referenceId := c.Query("ref")
	paypalOrderId := c.Query("token")

	if referenceId == "" || paypalOrderId == "" {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal capture 缺少参数 ref=%q token=%q client_ip=%s", referenceId, paypalOrderId, c.ClientIP()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)

	topUp := model.GetTopUpByTradeNo(referenceId)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal capture 订单不存在 ref=%q paypal_order=%q client_ip=%s", referenceId, paypalOrderId, c.ClientIP()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	if topUp.Status == common.TopUpStatusSuccess {
		// Already processed (idempotent)
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/log")
		return
	}

	if topUp.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal capture 订单状态异常 ref=%q status=%q client_ip=%s", referenceId, topUp.Status, c.ClientIP()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	captureResult, err := capturePayPalOrder(ctx, paypalOrderId)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("PayPal capture 失败 ref=%q paypal_order=%q client_ip=%s error=%q", referenceId, paypalOrderId, c.ClientIP(), err.Error()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	if captureResult.Status != "COMPLETED" {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal capture 状态异常 ref=%q paypal_order=%q status=%q client_ip=%s", referenceId, paypalOrderId, captureResult.Status, c.ClientIP()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	if err := model.RechargePayPal(referenceId, c.ClientIP()); err != nil {
		logger.LogError(ctx, fmt.Sprintf("PayPal 充值处理失败 ref=%q paypal_order=%q client_ip=%s error=%q", referenceId, paypalOrderId, c.ClientIP(), err.Error()))
		c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/topup")
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("PayPal 充值成功 ref=%q paypal_order=%q client_ip=%s", referenceId, paypalOrderId, c.ClientIP()))
	c.Redirect(http.StatusFound, system_setting.ServerAddress+"/console/log")
}

func getPayPalPayMoney(amount float64, group string) float64 {
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}
	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok {
		if ds > 0 {
			discount = ds
		}
	}
	return amount * setting.PayPalUnitPrice * topupGroupRatio * discount
}

func RequestPayPalAmount(c *gin.Context) {
	var req PayPalPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)
	money := getPayPalPayMoney(float64(req.Amount), user.Group)
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": fmt.Sprintf("%.2f", money)})
}

func getPayPalMinTopup() int64 {
	minTopup := setting.PayPalMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}
