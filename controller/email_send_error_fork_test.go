package controller

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// pointSMTPAtClosedPort makes common.SendEmail fail with a raw dial error that
// names the SMTP host, then restores the SMTP globals.
func pointSMTPAtClosedPort(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	server, smtpPort, ssl := common.SMTPServer, common.SMTPPort, common.SMTPSSLEnabled
	account, from, token := common.SMTPAccount, common.SMTPFrom, common.SMTPToken
	t.Cleanup(func() {
		common.SMTPServer, common.SMTPPort, common.SMTPSSLEnabled = server, smtpPort, ssl
		common.SMTPAccount, common.SMTPFrom, common.SMTPToken = account, from, token
	})
	common.SMTPServer, common.SMTPPort, common.SMTPSSLEnabled = "127.0.0.1", port, false
	common.SMTPAccount, common.SMTPFrom, common.SMTPToken = "mailer@example.com", "mailer@example.com", ""
}

func TestSendEmailVerificationHidesSMTPErrorFork(t *testing.T) {
	setupManageUserTestDB(t)
	pointSMTPAtClosedPort(t)
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/verification?email=new-user@example.com", nil)
	c.Request.Header.Set("Accept-Language", "ko")
	SendEmailVerification(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		MessageKey string `json:"message_key"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, "Failed to send email", response.Message)
	require.Equal(t, "Failed to send email", response.MessageKey)
	require.NotContains(t, recorder.Body.String(), "127.0.0.1")
	require.NotContains(t, recorder.Body.String(), "dial tcp")
}

// The web console translates the key; a Korean reader sees the fixed text.
func TestEmailSendFailedKeyHasKoreanTranslationFork(t *testing.T) {
	data, err := os.ReadFile("../web/src/i18n/locales/ko.json")
	require.NoError(t, err)
	var locale struct {
		Translation map[string]string `json:"translation"`
	}
	require.NoError(t, common.Unmarshal(data, &locale))
	require.Equal(t, "이메일을 보내지 못했습니다", locale.Translation["Failed to send email"])
}
