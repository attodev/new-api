package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTossOptionUpdateValidatesTypesAndMinimum(t *testing.T) {
	value, err := normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossEnabled", Value: true})
	require.NoError(t, err)
	require.Equal(t, "true", value)

	value, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossUnitPrice", Value: float64(1300)})
	require.NoError(t, err)
	require.Equal(t, "1300", value)
	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossUnitPrice", Value: float64(1 << 31)})
	require.Error(t, err)

	value, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossMinTopUp", Value: float64(100)})
	require.NoError(t, err)
	require.Equal(t, "100", value)

	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossMinTopUp", Value: float64(99)})
	require.Error(t, err)
	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossMinTopUp", Value: float64(1 << 31)})
	require.Error(t, err)
	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossMinTopUp", Value: float64(1 << 63)})
	require.Error(t, err)
	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossEnabled", Value: "true"})
	require.Error(t, err)
	_, err = normalizeTossOptionUpdate(OptionUpdateRequest{Key: "TossSecretKey", Value: nil})
	require.Error(t, err)
}

func TestValidateTossOptionUpdateSetAllowsSecretRotationButNotMIDMixing(t *testing.T) {
	current := map[string]string{
		"TossEnabled":                   "true",
		"TossBillingEnabled":            "true",
		"TossWalletAutoRechargeEnabled": "false",
		"TossTestMode":                  "false",
		"TossClientKey":                 "live_ck_normal_mid",
		"TossSecretKey":                 "live_sk_normal_old",
		"TossTestClientKey":             "test_ck_normal_mid",
		"TossTestSecretKey":             "test_sk_normal",
		"TossBillingClientKey":          "live_ck_billing_mid",
		"TossBillingSecretKey":          "live_sk_billing_old",
		"TossBillingTestClientKey":      "test_ck_billing_mid",
		"TossBillingTestSecretKey":      "test_sk_billing",
	}

	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossSecretKey": "live_sk_normal_rotated",
	}))
	require.NoError(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_ck_normal_mid",
		"TossSecretKey": "live_sk_normal_rotated",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_ck_other_mid",
	}))
	require.NoError(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_ck_other_mid",
		"TossSecretKey": "live_sk_other_mid",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "test_ck_wrong_environment",
		"TossSecretKey": "test_sk_wrong_environment",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_sk_swapped",
		"TossSecretKey": "live_ck_swapped",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_gsk_swapped",
		"TossSecretKey": "live_gck_swapped",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_gck_widget_mid",
		"TossSecretKey": "live_gsk_widget_mid",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "custom_client_key",
		"TossSecretKey": "custom_secret_key",
	}))
}

func TestValidateTossOptionUpdateSetRequiresCompleteSelectedPair(t *testing.T) {
	current := map[string]string{
		"TossEnabled":                   "false",
		"TossBillingEnabled":            "false",
		"TossWalletAutoRechargeEnabled": "false",
		"TossTestMode":                  "false",
		"TossClientKey":                 "",
		"TossSecretKey":                 "",
		"TossTestClientKey":             "",
		"TossTestSecretKey":             "",
		"TossBillingClientKey":          "",
		"TossBillingSecretKey":          "",
		"TossBillingTestClientKey":      "",
		"TossBillingTestSecretKey":      "",
	}

	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossEnabled": "true",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossBillingEnabled": "true",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossWalletAutoRechargeEnabled": "true",
	}))
	require.NoError(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossClientKey": "live_ck_normal_mid",
		"TossSecretKey": "live_sk_normal_mid",
		"TossEnabled":   "true",
	}))
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossSecretKey": "live_sk_without_client",
	}))

	current["TossClientKey"] = "test_ck_stored_in_live_slot"
	current["TossSecretKey"] = "test_sk_stored_in_live_slot"
	require.Error(t, validateTossOptionUpdateSet(current, map[string]string{
		"TossEnabled": "true",
	}))
}

func TestTossRuntimeCredentialPairRejectsUnsafeLegacyValues(t *testing.T) {
	require.True(t, isSafeTossRuntimeCredentialPair("live_ck_public", "live_sk_private", false))
	require.True(t, isSafeTossRuntimeCredentialPair("test_ck_public", "test_sk_private", true))
	require.False(t, isSafeTossRuntimeCredentialPair("custom_client", "custom_secret", false))
	require.False(t, isSafeTossRuntimeCredentialPair("live_sk_swapped", "live_ck_swapped", false))
	require.False(t, isSafeTossRuntimeCredentialPair("live_gck_widget", "live_gsk_widget", false))
	require.False(t, isSafeTossRuntimeCredentialPair("test_ck_wrong_env", "test_sk_wrong_env", false))
}

func restoreTossConfigForOptionTest(t *testing.T, snapshot setting.TossConfigSnapshot) {
	t.Helper()
	require.NoError(t, setting.ApplyTossOptionValuesWithRevision(map[string]string{
		"TossEnabled":                   strconv.FormatBool(snapshot.Enabled),
		"TossBillingEnabled":            strconv.FormatBool(snapshot.BillingEnabled),
		"TossWalletAutoRechargeEnabled": strconv.FormatBool(snapshot.WalletAutoRechargeEnabled),
		"TossTestMode":                  strconv.FormatBool(snapshot.TestMode),
		"TossClientKey":                 snapshot.ClientKey,
		"TossSecretKey":                 snapshot.SecretKey,
		"TossTestClientKey":             snapshot.TestClientKey,
		"TossTestSecretKey":             snapshot.TestSecretKey,
		"TossBillingClientKey":          snapshot.BillingClientKey,
		"TossBillingSecretKey":          snapshot.BillingSecretKey,
		"TossBillingTestClientKey":      snapshot.BillingTestClientKey,
		"TossBillingTestSecretKey":      snapshot.BillingTestSecretKey,
		"TossUnitPrice":                 strconv.FormatFloat(snapshot.UnitPrice, 'f', -1, 64),
		"TossMinTopUp":                  strconv.Itoa(snapshot.MinTopUp),
	}, snapshot.Revision))
}

func TestUpdateTossOptionsRejectsPartialMIDChangeAndAllowsSecretRotation(t *testing.T) {
	setupTossBillingControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	original := setting.GetTossConfigSnapshot()
	t.Cleanup(func() { restoreTossConfigForOptionTest(t, original) })
	require.NoError(t, setting.ApplyTossOptionValues(map[string]string{
		"TossEnabled":        "false",
		"TossBillingEnabled": "false",
		"TossTestMode":       "false",
		"TossClientKey":      "live_ck_existing_mid",
		"TossSecretKey":      "live_sk_existing_secret",
		"TossUnitPrice":      "1300",
		"TossMinTopUp":       "100",
	}))
	common.OptionMapRWMutex.Lock()
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	state, err := model.GetTossConfigState()
	require.NoError(t, err)
	require.True(t, state.RepairRequired)
	require.NoError(t, model.RepairTossOptionsBulk(
		currentTossOptionValues(),
		state.RepairToken,
		validateTossOptionUpdateSet,
	))
	require.NoError(t, model.CompleteTossConfigurationMaintenance())

	perform := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/option/toss", bytes.NewBufferString(body))
		UpdateTossOptions(c)
		return recorder
	}

	rejected := perform(`{"updates":[{"key":"TossClientKey","value":"live_ck_other_mid"}]}`)
	require.Contains(t, rejected.Body.String(), `"success":false`)
	var optionCount int64
	require.NoError(t, model.DB.Model(&model.Option{}).Count(&optionCount).Error)
	require.GreaterOrEqual(t, optionCount, int64(len(tossAtomicOptionKeys)+1))
	require.Equal(t, "live_ck_existing_mid", setting.GetTossConfigSnapshot().ClientKey)

	rotated := perform(`{"updates":[{"key":"TossClientKey","value":"live_ck_existing_mid"},{"key":"TossSecretKey","value":"live_sk_rotated"}]}`)
	require.Contains(t, rotated.Body.String(), `"success":true`)
	var stored model.Option
	require.NoError(t, model.DB.Where("key = ?", "TossSecretKey").First(&stored).Error)
	require.True(t, strings.HasPrefix(stored.Value, "enc:v1:"))
	require.NotContains(t, stored.Value, "live_sk_rotated")
	require.Equal(t, "live_sk_rotated", setting.GetTossConfigSnapshot().SecretKey)
}

func TestUpdateTossOptionsRejectsOversizedTrailingBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/toss",
		strings.NewReader(oversizedTossPaymentJSON(`{"updates":[{"key":"TossEnabled","value":false}]}`)),
	)
	// Exercise the streaming path: a JSON decoder that stops after the first
	// value would otherwise never observe the oversized trailing bytes.
	c.Request.ContentLength = -1

	UpdateTossOptions(c)

	requireTossBodyTooLargeResponse(t, recorder)
}

func TestUpdateTossOptionsRejectsSecondJSONDocument(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/toss",
		strings.NewReader(`{"updates":[{"key":"TossEnabled","value":false}]}{"updates":[{"key":"TossEnabled","value":true}]}`),
	)

	UpdateTossOptions(c)

	require.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestUpdateOptionRejectsTossAtomicEndpointBypass(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewBufferString(
		`{"key":"TossTestMode","value":true}`,
	))

	UpdateOption(c)

	require.Contains(t, recorder.Body.String(), `"success":false`)
}

func TestGetOptionsExposesOnlyPublicTossClientKeys(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = map[string]string{
		"TossClientKey":        "live_ck_public",
		"TossSecretKey":        "live_sk_private",
		"TossBillingClientKey": "live_ck_billing_public",
		"TossBillingSecretKey": "live_sk_billing_private",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetOptions(c)

	require.Contains(t, recorder.Body.String(), "live_ck_public")
	require.Contains(t, recorder.Body.String(), "live_ck_billing_public")
	require.NotContains(t, recorder.Body.String(), "live_sk_private")
	require.NotContains(t, recorder.Body.String(), "live_sk_billing_private")
}

func TestGetOptionsRejectsLegacySecretsStoredInTossClientSlots(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = map[string]string{
		"TossClientKey":            "live_sk_legacy_secret",
		"TossTestClientKey":        "test_gsk_legacy_widget_secret",
		"TossBillingClientKey":     "unknown_legacy_value",
		"TossBillingTestClientKey": "test_ck_valid_public",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	GetOptions(c)

	require.NotContains(t, recorder.Body.String(), "live_sk_legacy_secret")
	require.NotContains(t, recorder.Body.String(), "test_gsk_legacy_widget_secret")
	require.NotContains(t, recorder.Body.String(), "unknown_legacy_value")
	require.Contains(t, recorder.Body.String(), "test_ck_valid_public")
}
