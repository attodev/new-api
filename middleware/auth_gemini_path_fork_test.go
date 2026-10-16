package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from team 2d1169e84. Team tests an isGeminiQueryKeyPath helper that
// develop does not have; develop inlines the path check in TokenAuth, so this
// drives TokenAuth directly and asserts only the "?key=" -> Authorization
// promotion. The later token lookup is expected to fail (no token rows).
func TestTokenAuthPromotesGeminiQueryKeyOnModelsPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupDashboardAuthMiddlewareTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}))

	cases := []struct {
		target string
		want   string
	}{
		{"/v1/models?key=sk-x", "Bearer sk-x"},
		{"/v1/models/gemini-3-flash-preview?key=sk-x", "Bearer sk-x"},
		{"/v1beta/models?key=sk-x", "Bearer sk-x"},
		{"/v1beta/models/x?key=sk-x", "Bearer sk-x"},
		{"/v1beta/openai/models?key=sk-x", "Bearer sk-x"},
		{"/v1/chat/completions?key=sk-x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, tc.target, nil)

			TokenAuth()(c)

			assert.Equal(t, tc.want, c.Request.Header.Get("Authorization"))
			assert.True(t, c.IsAborted(), "unknown token must still be rejected")
		})
	}
}
