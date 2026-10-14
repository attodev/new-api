package service

import (
	"errors"

	"github.com/QuantumNous/new-api/model"
)

// fork: O3 — organization funding sources.
//
// An organization member's wallet request charges two balances together: the
// member limit (users.quota) and the shared organization wallet
// (organizations.quota). An organization plan request charges the plan and
// the organization wallet. User-side amounts are upstream ints; organization
// amounts are int64 and converted at the boundary.

func resolveOrganizationForWallet(userId int) (int, error) {
	user, err := model.GetUserById(userId, false)
	if err != nil {
		return 0, err
	}
	if user.OrganizationId <= 0 {
		return 0, nil
	}
	return user.OrganizationId, nil
}

// AdjustWalletQuotaForUser charges (delta > 0) or refunds (delta < 0) the
// user's wallet and, for organization members, the organization wallet of the
// user's current organization.
func AdjustWalletQuotaForUser(userId int, delta int) error {
	if delta == 0 {
		return nil
	}
	organizationId, err := resolveOrganizationForWallet(userId)
	if err != nil {
		return err
	}
	return adjustWalletQuotaForOrganization(userId, organizationId, delta)
}

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
				_ = model.IncreaseUserQuota(userId, delta, false)
				return err
			}
		}
		return nil
	}

	refund := -delta
	if err := model.IncreaseUserQuota(userId, refund, false); err != nil {
		return err
	}
	if organizationId > 0 {
		if err := model.IncreaseOrganizationQuota(organizationId, int64(refund)); err != nil {
			_ = model.DecreaseUserQuota(userId, refund, false)
			return err
		}
	}
	return nil
}

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
		return ErrInsufficientWalletQuota
	}
	return nil
}

// UpdateOrganizationUsedQuotaForUser adds quota (either sign) to the used
// quota of the user's current organization; no-op for non-members.
func UpdateOrganizationUsedQuotaForUser(userId int, quota int) {
	if quota == 0 {
		return
	}
	organizationId, err := resolveOrganizationForWallet(userId)
	if err != nil || organizationId <= 0 {
		return
	}
	_ = model.UpdateOrganizationUsedQuota(organizationId, int64(quota))
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
	if err := model.DecreaseOrganizationQuota(o.organizationId, o.preConsumed); err != nil {
		_ = model.RefundOrganizationSubscriptionPreConsume(o.requestId)
		o.preConsumed = 0
		return err
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
			_ = model.PostConsumeOrganizationUserSubscriptionDelta(o.organizationUserSubscriptionId, -d)
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
		_ = model.DecreaseOrganizationQuota(o.organizationId, refund)
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
	if err := refundWithRetry(func() error {
		return model.RefundOrganizationSubscriptionPreConsume(o.requestId)
	}); err != nil {
		return err
	}
	if o.organizationWalletConsumed > 0 {
		return model.IncreaseOrganizationQuota(o.organizationId, o.organizationWalletConsumed)
	}
	return nil
}
