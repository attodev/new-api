package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOptionsAPI_HidesAndProtectsOIDCSigningKeys(t *testing.T) {
	setupOAuth2OptionTest(t)
	oidc.ResetKeyCacheForTest()
	t.Cleanup(oidc.ResetKeyCacheForTest)

	_, err := oidc.CurrentKey()
	require.NoError(t, err)
	_, err = oidc.RotateKey() // populates both current and previous

	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(c)
	body := rec.Body.String()
	require.Contains(t, body, `"success":true`)
	require.NotContains(t, body, oidc.OptionSigningKeyCurrent)
	require.NotContains(t, body, oidc.OptionSigningKeyPrevious)
	require.NotContains(t, body, "PRIVATE KEY")

	for _, name := range []string{oidc.OptionSigningKeyCurrent, oidc.OptionSigningKeyPrevious} {
		common.OptionMapRWMutex.RLock()
		before := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.NotEmpty(t, before)

		require.Contains(t, putOAuth2Option(t, name, "tampered"), `"success":false`)

		common.OptionMapRWMutex.RLock()
		after := common.OptionMap[name]
		common.OptionMapRWMutex.RUnlock()
		require.Equal(t, before, after)
	}
}
