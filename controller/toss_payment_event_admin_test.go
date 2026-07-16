package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func performTossPaymentEventAdminRequest(t *testing.T, handler gin.HandlerFunc, method, target, body string, eventId, adminId int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if eventId > 0 {
		c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(eventId)}}
	}
	if adminId > 0 {
		c.Set("id", adminId)
	}
	handler(c)
	return recorder
}

func TestTossPaymentReconciliationAdminHandlers(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}))

	event := &model.TossPaymentEvent{
		EventKey:             "admin-handler-cancel-1",
		EventType:            model.TossPaymentEventTypeCancellation,
		OrderId:              "admin_handler_order_1",
		PaymentKey:           "pay_admin_handler_1",
		Status:               "PARTIAL_CANCELED",
		CancelAmount:         250,
		BalanceAmount:        750,
		OriginalAmount:       1000,
		ProviderPayload:      `{"paymentKey":"pay_admin_handler_1","status":"PARTIAL_CANCELED"}`,
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	}
	created, err := model.RecordTossPaymentEvent(event)
	require.NoError(t, err)
	require.True(t, created)

	listRecorder := performTossPaymentEventAdminRequest(
		t,
		ListTossPaymentReconciliationEvents,
		http.MethodGet,
		"/api/admin/toss/reconciliation-events?p=1&page_size=20",
		"",
		0,
		81,
	)
	require.Equal(t, http.StatusOK, listRecorder.Code)
	var listResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Total int                      `json:"total"`
			Items []model.TossPaymentEvent `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Equal(t, 1, listResponse.Data.Total)
	require.Len(t, listResponse.Data.Items, 1)
	require.Equal(t, event.Id, listResponse.Data.Items[0].Id)
	require.NotContains(t, listRecorder.Body.String(), "provider_payload")

	detailRecorder := performTossPaymentEventAdminRequest(
		t,
		GetTossPaymentReconciliationEvent,
		http.MethodGet,
		"/api/admin/toss/reconciliation-events/"+strconv.Itoa(event.Id),
		"",
		event.Id,
		81,
	)
	var detailResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Id              int    `json:"id"`
			ProviderPayload string `json:"provider_payload"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(detailRecorder.Body.Bytes(), &detailResponse))
	require.True(t, detailResponse.Success)
	require.Equal(t, event.Id, detailResponse.Data.Id)
	require.Equal(t, event.ProviderPayload, detailResponse.Data.ProviderPayload)

	resolveRecorder := performTossPaymentEventAdminRequest(
		t,
		ResolveTossPaymentReconciliationEvent,
		http.MethodPost,
		"/api/admin/toss/reconciliation-events/"+strconv.Itoa(event.Id)+"/resolve",
		`{"resolution_note":"Refund confirmed; quota adjusted manually."}`,
		event.Id,
		81,
	)
	var resolveResponse struct {
		Success bool `json:"success"`
		Data    struct {
			ReconciliationStatus string `json:"reconciliation_status"`
			ResolutionNote       string `json:"resolution_note"`
			ResolvedBy           int    `json:"resolved_by"`
			ResolvedTime         int64  `json:"resolved_time"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(resolveRecorder.Body.Bytes(), &resolveResponse))
	require.True(t, resolveResponse.Success)
	require.Equal(t, model.TossReconciliationStatusResolved, resolveResponse.Data.ReconciliationStatus)
	require.Equal(t, "Refund confirmed; quota adjusted manually.", resolveResponse.Data.ResolutionNote)
	require.Equal(t, 81, resolveResponse.Data.ResolvedBy)
	require.Positive(t, resolveResponse.Data.ResolvedTime)

	resolved, err := model.GetTossPaymentEventById(event.Id)
	require.NoError(t, err)
	require.Equal(t, 81, resolved.ResolvedBy)
	require.Equal(t, "Refund confirmed; quota adjusted manually.", resolved.ResolutionNote)

	requiredRecorder := performTossPaymentEventAdminRequest(
		t,
		ListTossPaymentReconciliationEvents,
		http.MethodGet,
		"/api/admin/toss/reconciliation-events",
		"",
		0,
		81,
	)
	require.NoError(t, common.Unmarshal(requiredRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Zero(t, listResponse.Data.Total)
}

func TestResolveTossPaymentReconciliationEventRejectsEmptyNote(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}))
	event := &model.TossPaymentEvent{
		EventKey:             "admin-handler-empty-note",
		EventType:            model.TossPaymentEventTypeFulfillment,
		OrderId:              "admin_handler_empty_note",
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	}
	_, err := model.RecordTossPaymentEvent(event)
	require.NoError(t, err)

	recorder := performTossPaymentEventAdminRequest(
		t,
		ResolveTossPaymentReconciliationEvent,
		http.MethodPost,
		"/api/admin/toss/reconciliation-events/"+strconv.Itoa(event.Id)+"/resolve",
		`{"resolution_note":"   "}`,
		event.Id,
		81,
	)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	reloaded, err := model.GetTossPaymentEventById(event.Id)
	require.NoError(t, err)
	require.Equal(t, model.TossReconciliationStatusRequired, reloaded.ReconciliationStatus)
}

func TestTossPaymentReconciliationAdminAcceptsFinancialMismatchFilter(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TossPaymentEvent{}))
	_, err := model.RecordTossPaymentEvent(&model.TossPaymentEvent{
		EventKey: "admin-financial-mismatch-filter", EventType: model.TossPaymentEventTypeFinancialMismatch,
		OrderId: "admin_financial_mismatch_order", Status: "PARTIAL_CANCELED",
		ReconciliationStatus: model.TossReconciliationStatusRequired,
	})
	require.NoError(t, err)

	recorder := performTossPaymentEventAdminRequest(
		t,
		ListTossPaymentReconciliationEvents,
		http.MethodGet,
		"/api/admin/toss/reconciliation-events?event_type="+model.TossPaymentEventTypeFinancialMismatch,
		"",
		0,
		81,
	)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, 1, response.Data.Total)
}
