package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// fork: O3 — organization members never use the personal billing preference:
// an active organization plan charges plan + organization wallet, otherwise
// the member limit (users.quota) + organization wallet are charged together.
// Hook: NewBillingSession calls newOrganizationBillingSession first.

func isOrganizationSubscriptionSource(source string) bool {
	return source == BillingSourceOrganizationSubscription
}

func orgQuotaError(c *gin.Context, msg string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(msg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func requireOrganizationQuota(c *gin.Context, organizationId int, need int64) *types.NewAPIError {
	organizationQuota, err := model.GetOrganizationQuota(organizationId)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if organizationQuota <= 0 {
		return orgQuotaError(c, fmt.Sprintf("organization quota insufficient, remaining: %s", logger.FormatQuota(int(organizationQuota))))
	}
	if organizationQuota-need < 0 {
		return orgQuotaError(c, fmt.Sprintf("organization pre-charge failed, remaining quota: %s, required pre-charge quota: %s",
			logger.FormatQuota(int(organizationQuota)), logger.FormatQuota(int(need))))
	}
	return nil
}

// newOrganizationBillingSession returns handled=false for users without an
// organization so NewBillingSession continues with upstream's preference logic.
func newOrganizationBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError, bool) {
	user, err := model.GetUserById(relayInfo.UserId, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if user.OrganizationId <= 0 {
		return nil, nil, false
	}
	hasOrgSub, err := model.HasActiveOrganizationUserSubscription(user.OrganizationId, relayInfo.UserId)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if hasOrgSub {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		if apiErr := requireOrganizationQuota(c, user.OrganizationId, subConsume); apiErr != nil {
			return nil, apiErr, true
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &OrganizationSubscriptionFunding{
				requestId: relayInfo.RequestId, organizationId: user.OrganizationId, userId: relayInfo.UserId,
				modelName: relayInfo.OriginModelName, amount: subConsume,
			},
			lang: i18n.StatedLang(c),
		}
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr, true
		}
		return session, nil, true
	}

	// Wallet mode: the member limit is checked exactly like upstream's tryWallet.
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if userQuota <= 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota)})),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog()), true
	}
	if userQuota-preConsumedQuota < 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(i18n.T(c, i18n.MsgQuotaPreConsumeInsufficient, map[string]any{"Remaining": logger.FormatQuota(userQuota), "Required": logger.FormatQuota(preConsumedQuota)})),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog()), true
	}
	relayInfo.UserQuota = userQuota
	if apiErr := requireOrganizationQuota(c, user.OrganizationId, int64(preConsumedQuota)); apiErr != nil {
		return nil, apiErr, true
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &OrganizationWalletFunding{memberId: relayInfo.UserId, organizationId: user.OrganizationId},
		lang:      i18n.StatedLang(c),
	}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		return nil, apiErr, true
	}
	return session, nil, true
}

// reserveOrganizationFunding is the reserveFunding case for both organization fundings.
func reserveOrganizationFunding(funding FundingSource, delta int, lang string) error {
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("organization wallet quota insufficient: %s", err.Error()),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		f.consumed += delta
		return nil
	case *OrganizationSubscriptionFunding:
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(
				errors.New(i18n.Translate(lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return nil
	}
	return types.NewError(fmt.Errorf("unsupported funding source: %s", funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

func rollbackOrganizationFundingReserve(funding FundingSource, delta int) {
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		if err := f.Settle(-delta); err != nil {
			common.SysLog(common.LogText("error rolling back organization wallet funding reserve: %s", err.Error()))
		} else {
			f.consumed -= delta
		}
	case *OrganizationSubscriptionFunding:
		if err := f.Settle(-delta); err != nil {
			common.SysLog(common.LogText("error rolling back organization subscription funding reserve: %s", err.Error()))
		}
	}
}

func refundOrganizationExtraReserved(funding FundingSource, subscriptionId int, extraReserved int) {
	if extraReserved <= 0 || subscriptionId <= 0 || !isOrganizationSubscriptionSource(funding.Source()) {
		return
	}
	if err := model.PostConsumeOrganizationUserSubscriptionDelta(subscriptionId, -int64(extraReserved)); err != nil {
		common.SysLog(common.LogText("error refunding organization subscription extra reserved quota: %s", err.Error()))
	}
}

func organizationFundingNeedsRefund(funding FundingSource) bool {
	if sub, ok := funding.(*OrganizationSubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	if wallet, ok := funding.(*OrganizationWalletFunding); ok && wallet.consumed > 0 {
		return true
	}
	return false
}

func syncOrganizationRelayInfo(funding FundingSource, info *relaycommon.RelayInfo, extraReserved int) {
	info.BillingOrganizationId = 0
	switch f := funding.(type) {
	case *OrganizationSubscriptionFunding:
		info.BillingOrganizationId = f.organizationId
		info.SubscriptionId = f.organizationUserSubscriptionId
		info.SubscriptionPreConsumed = f.preConsumed + int64(extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = f.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = f.AmountUsedAfter + int64(extraReserved)
		info.SubscriptionPlanId = f.PlanId
		info.SubscriptionPlanTitle = f.PlanTitle
	case *OrganizationWalletFunding:
		info.BillingOrganizationId = f.organizationId
	}
}

func isOrganizationSubscriptionPreConsumeError(errMsg string) bool {
	return strings.Contains(errMsg, "no active organization subscription")
}

// postConsumeOrganizationQuota is the legacy post-consume path (no BillingSession).
func postConsumeOrganizationQuota(relayInfo *relaycommon.RelayInfo, quota int) (bool, error) {
	if relayInfo == nil {
		return false, nil
	}
	if isOrganizationSubscriptionSource(relayInfo.BillingSource) {
		if relayInfo.SubscriptionId == 0 {
			return true, errors.New("organization subscription id is missing")
		}
		if quota != 0 {
			if err := model.PostConsumeOrganizationUserSubscriptionDelta(relayInfo.SubscriptionId, int64(quota)); err != nil {
				return true, err
			}
			if quota > 0 {
				if err := model.DecreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(quota)); err != nil {
					return true, err
				}
			} else if err := model.IncreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(-quota)); err != nil {
				return true, err
			}
			relayInfo.SubscriptionPostDelta += int64(quota)
		}
		return true, nil
	}
	if relayInfo.BillingOrganizationId > 0 {
		return true, adjustWalletQuotaForOrganization(relayInfo.UserId, relayInfo.BillingOrganizationId, quota)
	}
	return false, nil
}
