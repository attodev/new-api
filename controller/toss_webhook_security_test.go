package controller

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tossDeadlineRecorder struct {
	*httptest.ResponseRecorder
	readDeadlines  []time.Time
	writeDeadlines []time.Time
	writeSetupErr  error
}

type tossTrackingBody struct {
	read bool
}

func (b *tossTrackingBody) Read([]byte) (int, error) {
	b.read = true
	return 0, nil
}

func (b *tossTrackingBody) Close() error { return nil }

func (r *tossDeadlineRecorder) SetReadDeadline(deadline time.Time) error {
	r.readDeadlines = append(r.readDeadlines, deadline)
	return nil
}

func (r *tossDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	r.writeDeadlines = append(r.writeDeadlines, deadline)
	if !deadline.IsZero() {
		return r.writeSetupErr
	}
	return nil
}

func TestTrustedTossWebhookSourceAcceptsPublishedDirectPeer(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/toss/webhook", nil)
	request.RemoteAddr = "13.124.18.147:443"
	require.True(t, isTrustedTossWebhookSource(request))
}

func TestTrustedTossWebhookSourceRejectsUntrustedPeerAndSpoofedForwardedFor(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/toss/webhook", nil)
	request.RemoteAddr = "203.0.113.10:443"
	request.Header.Set("X-Forwarded-For", "13.124.18.147")
	require.False(t, isTrustedTossWebhookSource(request))
}

func TestTrustedTossWebhookSourceUsesForwardedForOnlyBehindExplicitProxy(t *testing.T) {
	t.Setenv("TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS", "127.0.0.1/32, 10.0.0.0/8")

	request := httptest.NewRequest("POST", "/api/toss/webhook", nil)
	request.RemoteAddr = "127.0.0.1:8443"
	request.Header.Set("X-Forwarded-For", "13.124.18.147, 10.0.0.4")
	require.True(t, isTrustedTossWebhookSource(request))

	request.Header.Set("X-Forwarded-For", "203.0.113.15, 10.0.0.4")
	require.False(t, isTrustedTossWebhookSource(request))
}

func TestTrustedTossWebhookSourceAllowsConfiguredNewSource(t *testing.T) {
	t.Setenv("TOSS_WEBHOOK_SOURCE_CIDRS", "198.51.100.0/24")
	request := httptest.NewRequest("POST", "/api/toss/webhook", nil)
	request.RemoteAddr = "198.51.100.42:443"
	require.True(t, isTrustedTossWebhookSource(request))
}

func TestTossWebhookRejectsUntrustedBillingDeletedWithoutMutation(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	keyID, err := model.StoreTossBillingKey(
		7, "cust_untrusted_delete", "billing_untrusted_delete", "card", "****1234",
	)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(
		`{"eventType":"BILLING_DELETED","billingKey":"billing_untrusted_delete","reason":"FORGED"}`,
	))
	c.Request.RemoteAddr = "203.0.113.10:443"

	TossWebhook(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	var key model.UserBillingKey
	require.NoError(t, model.DB.First(&key, keyID).Error)
	require.Equal(t, model.BillingKeyStatusActive, key.Status)
}

func TestTossWebhookRetriesBillingDeletedWhenLegacyKeyIdentityIsUnresolved(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	key := &model.UserBillingKey{
		UserId:       7,
		CustomerKey:  "cust_unresolved_webhook",
		EncryptedKey: "not-a-valid-encrypted-key",
		Status:       model.BillingKeyStatusActive,
		CreateTime:   time.Now().Unix(),
	}
	require.NoError(t, model.DB.Create(key).Error)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(
		`{"eventType":"BILLING_DELETED","billingKey":"billing_unresolved_webhook","reason":"USER_REMOVED","data":{"customerKey":"cust_forged_scope"}}`,
	))
	c.Request.RemoteAddr = "13.124.18.147:443"

	TossWebhook(c)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var stored model.UserBillingKey
	require.NoError(t, model.DB.First(&stored, key.Id).Error)
	require.Equal(t, model.BillingKeyStatusActive, stored.Status)
}

func TestTossWebhookAppliesAndClearsUnderlyingReadAndWriteDeadlines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &tossDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	engine := gin.New()
	engine.Use(TossWebhookReadDeadlineMiddleware())
	engine.POST("/api/toss/webhook", TossWebhook)
	request := httptest.NewRequest(http.MethodPost, "/api/toss/webhook", bytes.NewBufferString(`{"eventType":"IGNORED"}`))

	started := time.Now()
	engine.ServeHTTP(writer, request)

	require.Equal(t, http.StatusOK, writer.Code)
	require.Len(t, writer.readDeadlines, 2)
	require.True(t, writer.readDeadlines[0].After(started))
	require.True(t, writer.readDeadlines[0].Before(started.Add(tossWebhookProcessingTimeout+time.Second)))
	require.True(t, writer.readDeadlines[1].IsZero(), "the keep-alive read deadline must be cleared")
	require.Len(t, writer.writeDeadlines, 2)
	require.True(t, writer.writeDeadlines[0].After(writer.readDeadlines[0]))
	require.True(t, writer.writeDeadlines[0].Before(started.Add(10*time.Second)))
	require.True(t, writer.writeDeadlines[1].IsZero(), "the keep-alive write deadline must be cleared")
}

func TestTossWebhookDeadlineMiddlewareFailsBeforeReadingUnsupportedWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(TossWebhookReadDeadlineMiddleware())
	engine.POST("/api/toss/webhook", TossWebhook)
	body := &tossTrackingBody{}
	request := httptest.NewRequest(http.MethodPost, "/api/toss/webhook", body)
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.False(t, body.read, "handler must not enter a blocking body read without a connection deadline")
}

func TestTossWebhookDeadlineMiddlewareFailsBeforeReadingWhenWriteDeadlineCannotBeSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := &tossDeadlineRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		writeSetupErr:    errors.New("write deadlines unavailable"),
	}
	engine := gin.New()
	engine.Use(TossWebhookReadDeadlineMiddleware())
	engine.POST("/api/toss/webhook", TossWebhook)
	body := &tossTrackingBody{}
	request := httptest.NewRequest(http.MethodPost, "/api/toss/webhook", body)

	engine.ServeHTTP(writer, request)

	require.Equal(t, http.StatusServiceUnavailable, writer.Code)
	require.False(t, body.read, "handler must not read the body without a response write deadline")
	require.Len(t, writer.readDeadlines, 2)
	require.False(t, writer.readDeadlines[0].IsZero())
	require.True(t, writer.readDeadlines[1].IsZero(), "the read deadline must be cleared after write deadline setup fails")
	require.Len(t, writer.writeDeadlines, 1)
}
