package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPayPalCapacityTest(t *testing.T) {
	t.Helper()
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	oldUnit, oldMin := setting.PayPalUnitPrice, setting.PayPalMinTopUp
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	setting.PayPalUnitPrice, setting.PayPalMinTopUp = 1, 1
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		setting.PayPalUnitPrice, setting.PayPalMinTopUp = oldUnit, oldMin
		model.DB = oldDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, model.DB.Create(&model.User{
		Id: 42, Username: "paypal_capacity_user", Quota: common.MaxWalletQuota - 100_000, Status: common.UserStatusEnabled,
	}).Error)
}

func postPayPal(t *testing.T, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func TestRequestPayPalAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	setupPayPalCapacityTest(t)
	rec := postPayPal(t, "/api/user/paypal/amount", `{"amount":1}`, RequestPayPalAmount)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, rec.Body.String())
}

func TestRequestPayPalPayRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	setupPayPalCapacityTest(t)
	rec := postPayPal(t, "/api/user/paypal/pay", `{"amount":1,"payment_method":"paypal"}`, RequestPayPalPay)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Top-up quota limit exceeded")
}

func TestPayPalWebhookRejectedWhenDisabled(t *testing.T) {
	oldID := setting.PayPalWebhookID
	setting.PayPalWebhookID = ""
	t.Cleanup(func() { setting.PayPalWebhookID = oldID })
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/paypal/webhook", strings.NewReader(`{}`))
	PayPalWebhook(ctx)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
