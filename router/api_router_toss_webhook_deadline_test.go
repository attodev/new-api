package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tossWebhookDeadlineWriter struct {
	*httptest.ResponseRecorder
	readDeadlines  []time.Time
	writeDeadlines []time.Time
}

func (w *tossWebhookDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	w.readDeadlines = append(w.readDeadlines, deadline)
	return nil
}

func (w *tossWebhookDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadlines = append(w.writeDeadlines, deadline)
	return nil
}

func TestApiRouterTossWebhookBypassesGzipAndReachesConnectionDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	writer := &tossWebhookDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"IGNORED"}`),
	)
	request.RemoteAddr = "13.124.18.147:443"
	request.Header.Set("Accept-Encoding", "gzip")
	started := time.Now()

	engine.ServeHTTP(writer, request)

	require.Equal(t, http.StatusOK, writer.Code)
	require.Empty(t, writer.Header().Get("Content-Encoding"), "gzip writer must not wrap the deadline-sensitive webhook")
	require.Len(t, writer.readDeadlines, 2)
	require.True(t, writer.readDeadlines[0].After(started))
	require.True(t, writer.readDeadlines[0].Before(started.Add(9*time.Second)))
	require.True(t, writer.readDeadlines[1].IsZero())
	require.Len(t, writer.writeDeadlines, 2)
	require.True(t, writer.writeDeadlines[0].After(writer.readDeadlines[0]))
	require.True(t, writer.writeDeadlines[0].Before(started.Add(10*time.Second)))
	require.True(t, writer.writeDeadlines[1].IsZero())
}

func TestApiRouterTossWebhookRejectsSpoofedForwardedSourceBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("TOSS_WEBHOOK_TRUSTED_PROXY_CIDRS", "")
	engine := gin.New()
	SetApiRouter(engine)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/toss/webhook",
		bytes.NewBufferString(`{"eventType":"PAYMENT_STATUS_CHANGED","data":{"orderId":"toss_spoofed","paymentKey":"pay_spoofed","status":"DONE"}}`),
	)
	request.RemoteAddr = "198.51.100.23:443"
	request.Header.Set("X-Forwarded-For", "13.124.18.147")
	response := &tossWebhookDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}

	engine.ServeHTTP(response, request)

	require.Equal(t, http.StatusForbidden, response.Code)
	require.Len(t, response.readDeadlines, 2)
	require.Len(t, response.writeDeadlines, 2)
}
