package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fork: O3 — organization billing errors returned to AI clients (relay and
// legacy Midjourney) are rendered in the language the request stated, while
// errors.Is still finds the model and service sentinels.

func newKoreanOrganizationBillingContext() *gin.Context {
	c := newOrganizationBillingContext()
	c.Request.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en;q=0.8")
	return c
}

// requireKorean checks that got is the ko.yaml text of key and that ko.yaml
// really translates it (the English text differs).
func requireKorean(t *testing.T, got string, key string, args ...map[string]any) {
	t.Helper()
	korean := i18n.Translate(i18n.LangKo, key, args...)
	require.NotEqual(t, key, korean, "ko.yaml lacks %s", key)
	require.NotEqual(t, i18n.Translate(i18n.LangEn, key, args...), korean, "%s is not translated", key)
	require.Equal(t, korean, got)
}

func newOrganizationRelayInfo(userId int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{UserId: userId, OriginModelName: "test-model", IsPlayground: true, ForcePreConsume: true}
}

func TestOrganizationRelayErrorsDisabledAndSuspended(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 1, "owner", 1000, 1, model.OrganizationRoleOwner)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 1)
	suspendOrganizationMemberForTest(t, 2)

	_, apiErr := NewBillingSession(newKoreanOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	requireKorean(t, apiErr.Error(), i18n.MsgOrganizationMemberSuspended)
	require.ErrorIs(t, apiErr, model.ErrOrganizationMemberSuspended)

	_, apiErr = NewBillingSession(newOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, "organization membership is suspended", apiErr.Error())
	require.ErrorIs(t, apiErr, model.ErrOrganizationMemberSuspended)

	disableOrganizationForTest(t, 1)
	_, apiErr = NewBillingSession(newKoreanOrganizationBillingContext(), newOrganizationRelayInfo(1), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	requireKorean(t, apiErr.Error(), i18n.MsgOrganizationDisabled)
	require.ErrorIs(t, apiErr, model.ErrOrganizationDisabled)

	_, apiErr = NewBillingSession(newOrganizationBillingContext(), newOrganizationRelayInfo(1), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, "organization is disabled", apiErr.Error())
	require.ErrorIs(t, apiErr, model.ErrOrganizationDisabled)
}

func TestOrganizationRelayErrorsOrganizationWalletShortfall(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 100, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)

	// Empty organization wallet: requireOrganizationQuota names the remaining quota.
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("quota", 0).Error)
	remaining := map[string]any{"Remaining": logger.FormatQuota(0)}
	_, apiErr := NewBillingSession(newKoreanOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	requireKorean(t, apiErr.Error(), i18n.MsgOrganizationQuotaInsufficientRemaining, remaining)
	_, apiErr = NewBillingSession(newOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, "organization quota insufficient, remaining: "+logger.FormatQuota(0), apiErr.Error())

	// Wallet smaller than the pre-charge.
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("quota", 10).Error)
	preCharge := map[string]any{"Remaining": logger.FormatQuota(10), "Required": logger.FormatQuota(25)}
	_, apiErr = NewBillingSession(newKoreanOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	requireKorean(t, apiErr.Error(), i18n.MsgOrganizationPreChargeInsufficient, preCharge)
	_, apiErr = NewBillingSession(newOrganizationBillingContext(), newOrganizationRelayInfo(2), 25)
	require.NotNil(t, apiErr)
	require.Equal(t, "organization pre-charge failed, remaining quota: "+logger.FormatQuota(10)+
		", required pre-charge quota: "+logger.FormatQuota(25), apiErr.Error())

	// The atomic pre-consume loses the race (organizationPreConsumeError).
	remaining = map[string]any{"Remaining": logger.FormatQuota(10)}
	session := &BillingSession{relayInfo: &relaycommon.RelayInfo{UserId: 2, IsPlayground: true}, funding: &OrganizationWalletFunding{memberId: 2, organizationId: 1}}
	apiErr = session.preConsume(newKoreanOrganizationBillingContext(), 50)
	require.NotNil(t, apiErr)
	requireKorean(t, apiErr.Error(), i18n.MsgOrganizationQuotaInsufficientRemaining, remaining)
	require.ErrorIs(t, apiErr, ErrInsufficientOrganizationWalletQuota)
	apiErr = session.preConsume(newOrganizationBillingContext(), 50)
	require.NotNil(t, apiErr)
	require.Equal(t, "organization quota insufficient, remaining: "+logger.FormatQuota(10), apiErr.Error())
}

func TestOrganizationRelayErrorsReserve(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 30, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)
	require.NoError(t, model.DB.Model(&model.Organization{}).Where("id = ?", 1).Update("quota", 10).Error)
	funding := &OrganizationWalletFunding{memberId: 2, organizationId: 1}

	// Member limit (30) cannot cover 50.
	err := reserveOrganizationFunding(funding, 50, true, i18n.LangKo)
	require.Error(t, err)
	requireKorean(t, err.Error(), i18n.MsgOrganizationMemberQuotaInsufficient, map[string]any{"Error": ErrInsufficientWalletQuota.Error()})
	require.ErrorIs(t, err, ErrInsufficientWalletQuota)
	err = reserveOrganizationFunding(funding, 50, true, "")
	require.EqualError(t, err, "member quota insufficient: wallet quota insufficient")
	require.ErrorIs(t, err, ErrInsufficientWalletQuota)

	// Member limit covers 20, the organization wallet (10) does not.
	err = reserveOrganizationFunding(funding, 20, true, i18n.LangKo)
	require.Error(t, err)
	requireKorean(t, err.Error(), i18n.MsgOrganizationWalletReserveInsufficient)
	require.ErrorIs(t, err, ErrInsufficientOrganizationWalletQuota)
	err = reserveOrganizationFunding(funding, 20, true, "")
	require.EqualError(t, err, "organization wallet quota insufficient")
	require.ErrorIs(t, err, ErrInsufficientOrganizationWalletQuota)
	require.Equal(t, 30, getUserQuotaForBillingTest(t, 2), "the member reserve is rolled back")

	// A failed (non-reserving) settle names the organization wallet.
	cause := errors.New("database is locked")
	err = organizationWalletSettleError(i18n.LangKo, cause)
	requireKorean(t, err.Error(), i18n.MsgOrganizationWalletInsufficient, map[string]any{"Error": cause.Error()})
	require.ErrorIs(t, err, cause)
	require.EqualError(t, organizationWalletSettleError("", cause), "organization wallet quota insufficient: database is locked")
}

func TestOrganizationRelayErrorsMidjourney(t *testing.T) {
	truncate(t)
	seedOrganizationUser(t, 2, "member", 500, 1, model.OrganizationRoleMember)
	seedOrganizationUser(t, 3, "plan-member", 500, 1, model.OrganizationRoleMember)
	seedOrganization(t, 1, 2)
	_, err := createOrganizationPlanAndAssign(t, 1, 3, 1000)
	require.NoError(t, err)
	seedOrganizationUser(t, 5, "empty-wallet-member", 500, 2, model.OrganizationRoleMember)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 2, Name: "org-empty", OwnerUserId: 5, Status: model.OrganizationStatusEnabled, Quota: 0}).Error)
	ko := newKoreanOrganizationBillingContext()
	en := newOrganizationBillingContext()

	err = CheckMidjourneyOrganizationBilling(ko, 3)
	requireKorean(t, err.Error(), i18n.MsgOrganizationMjPlanUnsupported)
	require.EqualError(t, CheckMidjourneyOrganizationBilling(en, 3), "legacy Midjourney billing does not support organization plans")

	err = CheckMidjourneyOrganizationBilling(ko, 5)
	requireKorean(t, err.Error(), i18n.MsgOrganizationQuotaInsufficient)
	require.EqualError(t, CheckMidjourneyOrganizationBilling(en, 5), "organization quota insufficient")

	suspendOrganizationMemberForTest(t, 2)
	err = CheckMidjourneyOrganizationBilling(ko, 2)
	requireKorean(t, err.Error(), i18n.MsgOrganizationMemberSuspended)
	require.ErrorIs(t, err, model.ErrOrganizationMemberSuspended)
	require.EqualError(t, CheckMidjourneyOrganizationBilling(en, 2), "organization membership is suspended")

	disableOrganizationForTest(t, 1)
	err = CheckMidjourneyOrganizationBilling(ko, 2)
	requireKorean(t, err.Error(), i18n.MsgOrganizationDisabled)
	require.ErrorIs(t, err, model.ErrOrganizationDisabled)
	err = CheckMidjourneyOrganizationBilling(en, 2)
	require.EqualError(t, err, "organization is disabled")
	require.ErrorIs(t, err, model.ErrOrganizationDisabled)

	// A nil context (no stated language) gets the default language.
	require.EqualError(t, CheckMidjourneyOrganizationBilling(nil, 2), "organization is disabled")
}
