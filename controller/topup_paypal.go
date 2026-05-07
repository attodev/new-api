package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// ============================================================================
// PayPal Webhook
// ============================================================================

// paypalVerifyRequest is the body sent to PayPal's signature-verification endpoint.
type paypalVerifyRequest struct {
	AuthAlgo         string          `json:"auth_algo"`
	CertURL          string          `json:"cert_url"`
	TransmissionID   string          `json:"transmission_id"`
	TransmissionSig  string          `json:"transmission_sig"`
	TransmissionTime string          `json:"transmission_time"`
	WebhookID        string          `json:"webhook_id"`
	WebhookEvent     json.RawMessage `json:"webhook_event"`
}

// verifyPayPalWebhookSignature calls PayPal's verify API and returns true when
// the signature is valid.
func verifyPayPalWebhookSignature(ctx context.Context, req paypalVerifyRequest) (bool, error) {
	token, err := getPayPalAccessToken(ctx)
	if err != nil {
		return false, err
	}

	bodyBytes, err := common.Marshal(req)
	if err != nil {
		return false, err
	}

	baseURL := getPayPalBaseURL()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/v1/notifications/verify-webhook-signature",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		return false, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var result struct {
		VerificationStatus string `json:"verification_status"`
	}
	if err := common.Unmarshal(respBody, &result); err != nil {
		return false, err
	}
	return result.VerificationStatus == "SUCCESS", nil
}

// getPayPalOrderReferenceID calls GET /v2/checkout/orders/{orderID} and returns
// the reference_id stored in purchase_units[0], which is our internal trade_no.
func getPayPalOrderReferenceID(ctx context.Context, orderID string) (string, error) {
	token, err := getPayPalAccessToken(ctx)
	if err != nil {
		return "", err
	}

	baseURL := getPayPalBaseURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/v2/checkout/orders/"+orderID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var order paypalOrderResponse
	if err := common.Unmarshal(body, &order); err != nil {
		return "", err
	}
	if len(order.PurchaseUnits) == 0 {
		return "", fmt.Errorf("PayPal 주문에 purchase_units 없음 order_id=%s", orderID)
	}
	return order.PurchaseUnits[0].ReferenceID, nil
}

// fulfillPayPalOrder credits the user for the given trade_no idempotently.
// It must be called while holding the order lock.
func fulfillPayPalOrder(ctx context.Context, referenceID string, clientIP string, eventType string) {
	topUp := model.GetTopUpByTradeNo(referenceID)
	if topUp == nil {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal webhook 주문 없음 ref=%q event=%s", referenceID, eventType))
		return
	}
	if topUp.Status == common.TopUpStatusSuccess {
		logger.LogInfo(ctx, fmt.Sprintf("PayPal webhook 이미 처리됨 ref=%q event=%s", referenceID, eventType))
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal webhook 상태 이상 ref=%q status=%q event=%s", referenceID, topUp.Status, eventType))
		return
	}
	if err := model.RechargePayPal(referenceID, clientIP); err != nil {
		logger.LogError(ctx, fmt.Sprintf("PayPal webhook 충전 실패 ref=%q event=%s error=%q", referenceID, eventType, err.Error()))
		return
	}
	logger.LogInfo(ctx, fmt.Sprintf("PayPal webhook 충전 성공 ref=%q event=%s", referenceID, eventType))
}

// PayPalWebhook handles incoming webhook events from PayPal.
//
// Relevant events:
//   - CHECKOUT.ORDER.APPROVED : user approved; we call capture here so that
//     orders are fulfilled even when the browser redirect (PayPalCapture) fails.
//   - PAYMENT.CAPTURE.COMPLETED : capture is confirmed; credit the user.
func PayPalWebhook(c *gin.Context) {
	ctx := c.Request.Context()

	if strings.TrimSpace(setting.PayPalWebhookID) == "" {
		logger.LogWarn(ctx, "PayPal webhook 비활성화됨 (WebhookID 미설정)")
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(ctx, "PayPal webhook body 읽기 실패: "+err.Error())
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	// Verify signature via PayPal API
	verified, err := verifyPayPalWebhookSignature(ctx, paypalVerifyRequest{
		AuthAlgo:         c.GetHeader("PAYPAL-AUTH-ALGO"),
		CertURL:          c.GetHeader("PAYPAL-CERT-URL"),
		TransmissionID:   c.GetHeader("PAYPAL-TRANSMISSION-ID"),
		TransmissionSig:  c.GetHeader("PAYPAL-TRANSMISSION-SIG"),
		TransmissionTime: c.GetHeader("PAYPAL-TRANSMISSION-TIME"),
		WebhookID:        setting.PayPalWebhookID,
		WebhookEvent:     json.RawMessage(body),
	})
	if err != nil || !verified {
		logger.LogWarn(ctx, fmt.Sprintf("PayPal webhook 서명 검증 실패 client_ip=%s error=%v", c.ClientIP(), err))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	// Parse event envelope
	var event struct {
		EventType string `json:"event_type"`
		Resource  struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			// CHECKOUT.ORDER.APPROVED — purchase_units contains reference_id
			PurchaseUnits []struct {
				ReferenceID string `json:"reference_id"`
			} `json:"purchase_units"`
			// PAYMENT.CAPTURE.COMPLETED — order_id lives here
			SupplementaryData struct {
				RelatedIDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if err := common.Unmarshal(body, &event); err != nil {
		logger.LogError(ctx, "PayPal webhook 파싱 실패: "+err.Error())
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("PayPal webhook 수신 event=%s client_ip=%s", event.EventType, c.ClientIP()))

	switch event.EventType {

	case "CHECKOUT.ORDER.APPROVED":
		// User approved the order on PayPal. Capture it immediately so that
		// credit is granted even when the browser redirect (PayPalCapture) fails.
		paypalOrderID := event.Resource.ID
		if paypalOrderID == "" {
			logger.LogWarn(ctx, "PayPal CHECKOUT.ORDER.APPROVED: resource.id 없음")
			c.Status(http.StatusOK)
			return
		}

		var referenceID string
		if len(event.Resource.PurchaseUnits) > 0 {
			referenceID = event.Resource.PurchaseUnits[0].ReferenceID
		}
		if referenceID == "" {
			// Fallback: fetch order details
			referenceID, err = getPayPalOrderReferenceID(ctx, paypalOrderID)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("PayPal APPROVED: 주문 조회 실패 order_id=%s error=%q", paypalOrderID, err.Error()))
				c.Status(http.StatusOK)
				return
			}
		}

		LockOrder(referenceID)
		defer UnlockOrder(referenceID)

		// Check if already fulfilled (e.g. PayPalCapture ran first)
		topUp := model.GetTopUpByTradeNo(referenceID)
		if topUp == nil || topUp.Status == common.TopUpStatusSuccess {
			c.Status(http.StatusOK)
			return
		}

		// Capture; PayPal will then fire PAYMENT.CAPTURE.COMPLETED
		captureResult, err := capturePayPalOrder(ctx, paypalOrderID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("PayPal APPROVED: capture 실패 ref=%q order_id=%s error=%q",
				referenceID, paypalOrderID, err.Error()))
			c.Status(http.StatusOK)
			return
		}
		if captureResult.Status != "COMPLETED" {
			logger.LogWarn(ctx, fmt.Sprintf("PayPal APPROVED: capture 상태 이상 ref=%q status=%q",
				referenceID, captureResult.Status))
			c.Status(http.StatusOK)
			return
		}
		fulfillPayPalOrder(ctx, referenceID, c.ClientIP(), event.EventType)

	case "PAYMENT.CAPTURE.COMPLETED":
		// Capture is confirmed (may have been triggered by PayPalCapture or the
		// CHECKOUT.ORDER.APPROVED handler above). Credit the user idempotently.
		orderID := event.Resource.SupplementaryData.RelatedIDs.OrderID
		if orderID == "" {
			logger.LogWarn(ctx, "PayPal PAYMENT.CAPTURE.COMPLETED: order_id 없음")
			c.Status(http.StatusOK)
			return
		}

		referenceID, err := getPayPalOrderReferenceID(ctx, orderID)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("PayPal CAPTURE.COMPLETED: 주문 조회 실패 order_id=%s error=%q", orderID, err.Error()))
			c.Status(http.StatusOK)
			return
		}

		LockOrder(referenceID)
		defer UnlockOrder(referenceID)

		fulfillPayPalOrder(ctx, referenceID, c.ClientIP(), event.EventType)

	default:
		// Unrelated event; acknowledge so PayPal stops retrying.
		logger.LogInfo(ctx, fmt.Sprintf("PayPal webhook 무시 event=%s", event.EventType))
	}

	c.Status(http.StatusOK)
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
