package service

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// fork: O3 — organization funding sources.
//
// An organization member's wallet request charges two balances together: the
// member limit (users.quota) and the shared organization wallet
// (organizations.quota). An organization plan request charges the plan and
// the organization wallet. User-side amounts are upstream ints; organization
// amounts are int64 and converted at the boundary.

// adjustWalletQuotaForOrganization charges (delta > 0) or refunds (delta < 0)
// the member limit and, when organizationId > 0, the organization wallet.
func adjustWalletQuotaForOrganization(userId int, organizationId int, delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		if err := model.DecreaseUserQuota(userId, delta, false); err != nil {
			return err
		}
		if organizationId > 0 {
			if err := model.DecreaseOrganizationQuota(organizationId, int64(delta)); err != nil {
				if rollbackErr := model.IncreaseUserQuota(userId, delta, false); rollbackErr != nil {
					common.SysLog(common.LogText("error rolling back member limit charge (userId=%d, organizationId=%d, amount=%d): %s", userId, organizationId, delta, rollbackErr.Error()))
				}
				return err
			}
		}
		return nil
	}

	refund := -delta
	memberCredited, err := refundMemberLimit(userId, organizationId, refund)
	if err != nil {
		return err
	}
	if organizationId > 0 {
		if err := model.IncreaseOrganizationQuota(organizationId, int64(refund)); err != nil {
			if memberCredited {
				if rollbackErr := model.DecreaseUserQuota(userId, refund, false); rollbackErr != nil {
					common.SysLog(common.LogText("error rolling back member limit refund (userId=%d, organizationId=%d, amount=%d): %s", userId, organizationId, refund, rollbackErr.Error()))
				}
			}
			return err
		}
	}
	return nil
}

// refundMemberLimit credits the member limit of the pinned organization only
// while the user is still in it: an ex-member's cap was zeroed on removal and
// must not come back as personal balance. The organization wallet is refunded
// by the caller either way.
func refundMemberLimit(userId int, organizationId int, refund int) (bool, error) {
	if organizationId <= 0 {
		if err := model.IncreaseUserQuota(userId, refund, false); err != nil {
			return false, err
		}
		return true, nil
	}
	credited, err := model.IncreaseOrganizationMemberQuota(userId, organizationId, refund)
	if err != nil {
		return false, err
	}
	if !credited {
		common.SysLog(common.LogText("skipped member limit refund: user %d is no longer in organization %d (amount=%d)", userId, organizationId, refund))
	}
	return credited, nil
}

// ErrInsufficientOrganizationWalletQuota reports that the shared organization
// wallet (not the member limit) cannot cover a reservation. It does not wrap
// ErrInsufficientWalletQuota, which upstream maps to the personal-balance
// message; organizationPreConsumeError names the organization wallet instead.
var ErrInsufficientOrganizationWalletQuota = errors.New("organization wallet quota insufficient")

func reserveWalletQuotaForOrganization(userId int, organizationId int, amount int) error {
	reserved, err := model.TryReserveUserQuota(userId, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	reserved, err = model.TryReserveOrganizationQuota(organizationId, int64(amount))
	if err != nil || !reserved {
		if rollbackErr := model.IncreaseUserQuota(userId, amount, false); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		if err != nil {
			return err
		}
		return ErrInsufficientOrganizationWalletQuota
	}
	return nil
}

// UpdateBillingOrganizationUsedQuota adds quota (either sign) to the used
// quota of the organization the request was billed to
// (relayInfo.BillingOrganizationId, pinned at session start). organizationId 0
// (a personal request) is a no-op with no database read, so non-members pay
// nothing for it on the hot path.
func UpdateBillingOrganizationUsedQuota(organizationId int, quota int) {
	if quota == 0 || organizationId <= 0 {
		return
	}
	if err := model.UpdateOrganizationUsedQuota(organizationId, int64(quota)); err != nil {
		common.SysLog(common.LogText("error updating organization used quota (organizationId=%d, quota=%d): %s", organizationId, quota, err.Error()))
	}
}

// ---------------------------------------------------------------------------
// OrganizationWalletFunding — member limit + organization wallet
// ---------------------------------------------------------------------------

// OrganizationWalletFunding pins the organization at session start, so a
// membership change mid-request settles and refunds the original organization.
type OrganizationWalletFunding struct {
	memberId       int
	organizationId int
	consumed       int
}

func (o *OrganizationWalletFunding) Source() string { return BillingSourceWallet }

func (o *OrganizationWalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	if o.memberId <= 0 || o.organizationId <= 0 {
		return errors.New("invalid organization wallet funding")
	}
	if err := reserveWalletQuotaForOrganization(o.memberId, o.organizationId, amount); err != nil {
		return err
	}
	o.consumed = amount
	return nil
}

func (o *OrganizationWalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	return adjustWalletQuotaForOrganization(o.memberId, o.organizationId, delta)
}

func (o *OrganizationWalletFunding) Refund() error {
	if o.consumed <= 0 {
		return nil
	}
	// Not idempotent (quota += N), so no retry — same as upstream WalletFunding.
	return adjustWalletQuotaForOrganization(o.memberId, o.organizationId, -o.consumed)
}

// ---------------------------------------------------------------------------
// OrganizationSubscriptionFunding — organization plan + organization wallet
// ---------------------------------------------------------------------------

type OrganizationSubscriptionFunding struct {
	requestId                      string
	organizationId                 int
	userId                         int
	modelName                      string
	amount                         int64
	organizationUserSubscriptionId int
	preConsumed                    int64
	organizationWalletConsumed     int64
	AmountTotal                    int64
	AmountUsedAfter                int64
	PlanId                         int
	PlanTitle                      string
}

func (o *OrganizationSubscriptionFunding) Source() string {
	return BillingSourceOrganizationSubscription
}

func (o *OrganizationSubscriptionFunding) PreConsume(_ int) error {
	res, err := model.PreConsumeOrganizationUserSubscription(o.requestId, o.organizationId, o.userId, o.modelName, o.amount)
	if err != nil {
		return err
	}
	o.organizationUserSubscriptionId = res.OrganizationUserSubscriptionId
	o.preConsumed = res.PreConsumed
	o.AmountTotal = res.AmountTotal
	o.AmountUsedAfter = res.AmountUsedAfter
	o.PlanId = res.PlanId
	o.PlanTitle = res.PlanTitle

	if o.preConsumed <= 0 {
		return nil
	}
	// Atomic: the balance check in newOrganizationBillingSession is advisory only.
	reserved, err := model.TryReserveOrganizationQuota(o.organizationId, o.preConsumed)
	if err != nil || !reserved {
		if refundErr := model.RefundOrganizationSubscriptionPreConsume(o.requestId); refundErr != nil {
			common.SysLog(common.LogText("error refunding organization subscription pre-consume after organization wallet reserve failed (requestId=%s): %s", o.requestId, refundErr.Error()))
		}
		o.preConsumed = 0
		if err != nil {
			return err
		}
		return ErrInsufficientOrganizationWalletQuota
	}
	o.organizationWalletConsumed = o.preConsumed
	return nil
}

func (o *OrganizationSubscriptionFunding) Settle(delta int) error {
	d := int64(delta)
	if d == 0 {
		return nil
	}
	if d > 0 {
		if err := model.PostConsumeOrganizationUserSubscriptionDelta(o.organizationUserSubscriptionId, d); err != nil {
			return err
		}
		if err := model.DecreaseOrganizationQuota(o.organizationId, d); err != nil {
			if rollbackErr := model.PostConsumeOrganizationUserSubscriptionDelta(o.organizationUserSubscriptionId, -d); rollbackErr != nil {
				common.SysLog(common.LogText("error rolling back organization subscription charge (subscriptionId=%d, delta=%d): %s", o.organizationUserSubscriptionId, d, rollbackErr.Error()))
			}
			return err
		}
		o.organizationWalletConsumed += d
		return nil
	}

	refund := -d
	if err := model.IncreaseOrganizationQuota(o.organizationId, refund); err != nil {
		return err
	}
	if err := model.PostConsumeOrganizationUserSubscriptionDelta(o.organizationUserSubscriptionId, d); err != nil {
		if rollbackErr := model.DecreaseOrganizationQuota(o.organizationId, refund); rollbackErr != nil {
			common.SysLog(common.LogText("error rolling back organization wallet refund (organizationId=%d, amount=%d): %s", o.organizationId, refund, rollbackErr.Error()))
		}
		return err
	}
	o.organizationWalletConsumed -= refund
	if o.organizationWalletConsumed < 0 {
		o.organizationWalletConsumed = 0
	}
	return nil
}

func (o *OrganizationSubscriptionFunding) Refund() error {
	if o.preConsumed <= 0 {
		return nil
	}
	// The organization wallet is refunded even when the plan refund fails, so a
	// stuck plan record never also strands organization funds.
	planErr := refundWithRetry(func() error {
		return model.RefundOrganizationSubscriptionPreConsume(o.requestId)
	})
	if planErr != nil {
		common.SysLog(common.LogText("error refunding organization subscription pre-consume (requestId=%s): %s", o.requestId, planErr.Error()))
	}
	if o.organizationWalletConsumed > 0 {
		if err := model.IncreaseOrganizationQuota(o.organizationId, o.organizationWalletConsumed); err != nil {
			return errors.Join(planErr, err)
		}
	}
	return planErr
}
