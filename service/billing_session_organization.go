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

// orgQuotaError is the 403 of an organization quota shortfall; err carries the
// client text (organizationError).
func orgQuotaError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func requireOrganizationQuota(c *gin.Context, organizationId int, need int64) *types.NewAPIError {
	organizationQuota, err := model.GetOrganizationQuota(organizationId)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if organizationQuota <= 0 {
		return orgQuotaError(organizationError(i18n.StatedLang(c), nil, i18n.MsgOrganizationQuotaInsufficientRemaining,
			map[string]any{"Remaining": logger.FormatQuota(int(organizationQuota))}))
	}
	if organizationQuota-need < 0 {
		return orgQuotaError(organizationError(i18n.StatedLang(c), nil, i18n.MsgOrganizationPreChargeInsufficient,
			map[string]any{"Remaining": logger.FormatQuota(int(organizationQuota)), "Required": logger.FormatQuota(int(need))}))
	}
	return nil
}

// requireActiveOrganization refuses relay requests of a disabled organization's
// members with 403 before anything is charged.
func requireActiveOrganization(c *gin.Context, organizationId int) *types.NewAPIError {
	if err := model.RequireActiveOrganization(organizationId); err != nil {
		if errors.Is(err, model.ErrOrganizationDisabled) {
			return types.NewErrorWithStatusCode(organizationError(i18n.StatedLang(c), err, i18n.MsgOrganizationDisabled), types.ErrorCodeAccessDenied, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	return nil
}

// requireActiveOrganizationMember refuses relay requests of a member an
// organization admin suspended (users.organization_member_status) with 403
// before anything is charged. memberStatus comes from the same read as the
// organization id (model.GetUserOrganizationMembership).
func requireActiveOrganizationMember(c *gin.Context, memberStatus int) *types.NewAPIError {
	if memberStatus == model.OrganizationMemberStatusSuspended {
		return types.NewErrorWithStatusCode(
			organizationError(i18n.StatedLang(c), model.ErrOrganizationMemberSuspended, i18n.MsgOrganizationMemberSuspended), types.ErrorCodeAccessDenied, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	return nil
}

// newOrganizationBillingSession returns handled=false for users without an
// organization so NewBillingSession continues with upstream's preference logic.
func newOrganizationBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError, bool) {
	// One primary-key read of a single column for every request (organization
	// fields stay outside the user cache, plan decision 3); non-members stop here.
	organizationId, memberStatus, err := model.GetUserOrganizationMembership(relayInfo.UserId)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if organizationId <= 0 {
		return nil, nil, false
	}
	if apiErr := requireActiveOrganization(c, organizationId); apiErr != nil {
		return nil, apiErr, true
	}
	if apiErr := requireActiveOrganizationMember(c, memberStatus); apiErr != nil {
		return nil, apiErr, true
	}
	hasOrgSub, err := model.HasActiveOrganizationUserSubscription(organizationId, relayInfo.UserId)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry()), true
	}
	if hasOrgSub {
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		if apiErr := requireOrganizationQuota(c, organizationId, subConsume); apiErr != nil {
			return nil, apiErr, true
		}
		session := &BillingSession{
			relayInfo: relayInfo,
			funding: &OrganizationSubscriptionFunding{
				requestId: relayInfo.RequestId, organizationId: organizationId, userId: relayInfo.UserId,
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
	if apiErr := requireOrganizationQuota(c, organizationId, int64(preConsumedQuota)); apiErr != nil {
		return nil, apiErr, true
	}
	session := &BillingSession{
		relayInfo: relayInfo,
		funding:   &OrganizationWalletFunding{memberId: relayInfo.UserId, organizationId: organizationId},
		lang:      i18n.StatedLang(c),
	}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		return nil, apiErr, true
	}
	return session, nil, true
}

// organizationPreConsumeError maps an organization wallet shortfall from
// FundingSource.PreConsume to a 403 that names the organization wallet; upstream
// preConsume would otherwise report the member's personal balance. Hook:
// BillingSession.preConsume, before its ErrInsufficientWalletQuota branch.
func organizationPreConsumeError(c *gin.Context, funding FundingSource, err error) *types.NewAPIError {
	if !errors.Is(err, ErrInsufficientOrganizationWalletQuota) {
		return nil
	}
	organizationId := 0
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		organizationId = f.organizationId
	case *OrganizationSubscriptionFunding:
		organizationId = f.organizationId
	}
	remaining, quotaErr := model.GetOrganizationQuota(organizationId)
	if quotaErr != nil {
		remaining = 0
	}
	return orgQuotaError(organizationError(i18n.StatedLang(c), err, i18n.MsgOrganizationQuotaInsufficientRemaining,
		map[string]any{"Remaining": logger.FormatQuota(int(remaining))}))
}

// reserveOrganizationFunding is the reserveFunding case for both organization fundings.
// requireAvailableQuota mirrors upstream's WalletFunding branch: when the
// quantity is known up front (image requests) both balances are reserved
// atomically and the reserve fails instead of admitting debt.
func reserveOrganizationFunding(funding FundingSource, delta int, requireAvailableQuota bool, lang string) error {
	switch f := funding.(type) {
	case *OrganizationWalletFunding:
		if requireAvailableQuota {
			if err := reserveWalletQuotaForOrganization(f.memberId, f.organizationId, delta); err != nil {
				if errors.Is(err, ErrInsufficientOrganizationWalletQuota) {
					return orgQuotaError(organizationError(lang, err, i18n.MsgOrganizationWalletReserveInsufficient))
				}
				if errors.Is(err, ErrInsufficientWalletQuota) {
					return orgQuotaError(organizationError(lang, err, i18n.MsgOrganizationMemberQuotaInsufficient, map[string]any{"Error": err.Error()}))
				}
				return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
			}
			f.consumed += delta
			return nil
		}
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(organizationWalletSettleError(lang, err),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		f.consumed += delta
		return nil
	case *OrganizationSubscriptionFunding:
		if requireAvailableQuota {
			return reserveOrganizationSubscriptionAtomically(f, delta, lang)
		}
		if err := f.Settle(delta); err != nil {
			return types.NewErrorWithStatusCode(
				errors.New(i18n.Translate(lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		return nil
	}
	return types.NewError(fmt.Errorf("unsupported funding source: %s", funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

// reserveOrganizationSubscriptionAtomically applies the plan delta, then
// reserves the organization wallet only if it can cover the delta; on failure
// the plan delta is undone.
func reserveOrganizationSubscriptionAtomically(f *OrganizationSubscriptionFunding, delta int, lang string) error {
	d := int64(delta)
	if err := model.PostConsumeOrganizationUserSubscriptionDelta(f.organizationUserSubscriptionId, d); err != nil {
		return types.NewErrorWithStatusCode(
			errors.New(i18n.Translate(lang, i18n.MsgQuotaSubscriptionInsufficient, map[string]any{"Error": err.Error()})),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	reserved, err := model.TryReserveOrganizationQuota(f.organizationId, d)
	if err != nil || !reserved {
		if rollbackErr := model.PostConsumeOrganizationUserSubscriptionDelta(f.organizationUserSubscriptionId, -d); rollbackErr != nil {
			common.SysLog(common.LogText("error undoing organization subscription reserve (subscriptionId=%d, delta=%d): %s", f.organizationUserSubscriptionId, d, rollbackErr.Error()))
		}
		if err != nil {
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		return orgQuotaError(organizationError(lang, ErrInsufficientOrganizationWalletQuota, i18n.MsgOrganizationWalletReserveInsufficient))
	}
	f.organizationWalletConsumed += d
	return nil
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
			var orgErr error
			if quota > 0 {
				orgErr = model.DecreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(quota))
			} else {
				orgErr = model.IncreaseOrganizationQuota(relayInfo.BillingOrganizationId, int64(-quota))
			}
			if orgErr != nil {
				if rollbackErr := model.PostConsumeOrganizationUserSubscriptionDelta(relayInfo.SubscriptionId, -int64(quota)); rollbackErr != nil {
					common.SysLog(common.LogText("error undoing organization subscription post-consume (subscriptionId=%d, delta=%d): %s", relayInfo.SubscriptionId, quota, rollbackErr.Error()))
				}
				return true, orgErr
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
