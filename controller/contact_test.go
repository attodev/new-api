package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func stubContactMail(t *testing.T, recipient string) (*string, *string, *string) {
	t.Helper()
	oldEmail, oldSend := common.ContactEmail, sendContactMail
	common.ContactEmail = recipient
	var subject, to, body string
	sendContactMail = func(s, r, b string) error { subject, to, body = s, r, b; return nil }
	t.Cleanup(func() { common.ContactEmail = oldEmail; sendContactMail = oldSend })
	return &subject, &to, &body
}

func postContact(t *testing.T, payload string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/contact", strings.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	Contact(ctx)
	return rec
}

func TestContactSendsLandingFormFields(t *testing.T) {
	subject, to, body := stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","phone":"010-0000","features":["chat","embedding"],
		"scale_type":"token","input_tokens":"1000000","output_tokens":"200000","notes":"<b>hi</b>"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":true`)
	require.Equal(t, "ops@example.com", *to)
	require.Contains(t, *subject, "ACME")
	for _, want := range []string{"ACME", "a@b.co", "010-0000", "chat, embedding", "1000000", "200000", "&lt;b&gt;hi&lt;/b&gt;"} {
		require.Contains(t, *body, want)
	}
}

func TestContactAcceptsLegacyMessageField(t *testing.T) {
	_, _, body := stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","message":"legacy text"}`)
	require.Contains(t, rec.Body.String(), `"success":true`)
	require.Contains(t, *body, "legacy text")
}

func TestContactRejectsMissingOrgOrEmail(t *testing.T) {
	stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"","email":"a@b.co"}`)
	require.Contains(t, rec.Body.String(), `"success":false`)
	rec = postContact(t, `{"org":"ACME","email":"not-an-email"}`)
	require.Contains(t, rec.Body.String(), `"success":false`)
}

func TestContactReturns503WhenUnconfigured(t *testing.T) {
	stubContactMail(t, "")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","notes":"x"}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTestEmailRequiresRecipient(t *testing.T) {
	oldSend := sendTestMail
	sendTestMail = func(string, string, string) error { return nil }
	t.Cleanup(func() { sendTestMail = oldSend })
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/test-email", strings.NewReader(`{"to":""}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	TestEmail(ctx)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTestEmailRejectsInvalidRecipient(t *testing.T) {
	oldSend := sendTestMail
	sendTestMail = func(string, string, string) error { return nil }
	t.Cleanup(func() { sendTestMail = oldSend })
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/test-email", strings.NewReader(`{"to":"not-an-email"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	TestEmail(ctx)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
