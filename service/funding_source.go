package service

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// ---------------------------------------------------------------------------
// FundingSource — or
// ---------------------------------------------------------------------------

// FundingSource
type FundingSource interface {
	// Source "wallet" "subscription"
	Source() string
	// PreConsume amount
	PreConsume(amount int) error
	// Settle
	Settle(delta int) error
	// Refund
	Refund() error
}

// ---------------------------------------------------------------------------
// WalletFunding —
// ---------------------------------------------------------------------------

var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

type WalletFunding struct {
	userId   int
	consumed int
}

func (w *WalletFunding) Source() string { return BillingSourceWallet }

func (w *WalletFunding) PreConsume(amount int) error {
	if amount <= 0 {
		return nil
	}
	reserved, err := model.TryReserveUserQuota(w.userId, int64(amount))
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	w.consumed = amount
	return nil
}

func (w *WalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return model.DecreaseUserQuota(w.userId, int64(delta), false)
	}
	return model.IncreaseUserQuota(w.userId, -int64(delta), false)
}

func (w *WalletFunding) Refund() error {
	if w.consumed <= 0 {
		return nil
	}
	// IncreaseUserQuota quota += N
	// RefundSubscriptionPreConsume requestId
	return model.IncreaseUserQuota(w.userId, int64(w.consumed), false)
}

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

func AdjustWalletQuotaForUser(userId int, delta int64) error {
	if delta == 0 {
		return nil
	}
	organizationId, err := resolveOrganizationForWallet(userId)
	if err != nil {
		return err
	}
	return adjustWalletQuotaForOrganization(userId, organizationId, delta)
}

func adjustWalletQuotaForOrganization(userId int, organizationId int, delta int64) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		if err := model.DecreaseUserQuota(userId, delta, false); err != nil {
			return err
		}
		if organizationId > 0 {
			if err := model.DecreaseOrganizationQuota(organizationId, delta); err != nil {
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
		if err := model.IncreaseOrganizationQuota(organizationId, refund); err != nil {
			_ = model.DecreaseUserQuota(userId, refund, false)
			return err
		}
	}
	return nil
}

func reserveWalletQuotaForOrganization(userId int, organizationId int, amount int64) error {
	reserved, err := model.TryReserveUserQuota(userId, amount)
	if err != nil {
		return err
	}
	if !reserved {
		return ErrInsufficientWalletQuota
	}
	reserved, err = model.TryReserveOrganizationQuota(organizationId, amount)
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

func UpdateOrganizationUsedQuotaForUser(userId int, quota int64) {
	if quota == 0 {
		return
	}
	organizationId, err := resolveOrganizationForWallet(userId)
	if err != nil || organizationId <= 0 {
		return
	}
	if err := model.UpdateOrganizationUsedQuota(organizationId, quota); err != nil {
		return
	}
}

// ---------------------------------------------------------------------------
// OrganizationWalletFunding — organization member limit + owner wallet
// ---------------------------------------------------------------------------

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
	if err := reserveWalletQuotaForOrganization(o.memberId, o.organizationId, int64(amount)); err != nil {
		return err
	}
	o.consumed = amount
	return nil
}

func (o *OrganizationWalletFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	return adjustWalletQuotaForOrganization(o.memberId, o.organizationId, int64(delta))
}

func (o *OrganizationWalletFunding) Refund() error {
	if o.consumed <= 0 {
		return nil
	}
	return adjustWalletQuotaForOrganization(o.memberId, o.organizationId, -int64(o.consumed))
}

// ---------------------------------------------------------------------------
// SubscriptionFunding —
// ---------------------------------------------------------------------------

type SubscriptionFunding struct {
	requestId      string
	userId         int
	modelName      string
	amount         int64 // subConsume
	subscriptionId int
	preConsumed    int64
	// PreConsume RelayInfo
	AmountTotal     int64
	AmountUsedAfter int64
	PlanId          int
	PlanTitle       string
}

func (s *SubscriptionFunding) Source() string { return BillingSourceSubscription }

func (s *SubscriptionFunding) PreConsume(_ int) error {
	// amount s.amount preConsumedQuota
	res, err := model.PreConsumeUserSubscription(s.requestId, s.userId, s.modelName, 0, s.amount)
	if err != nil {
		return err
	}
	s.subscriptionId = res.UserSubscriptionId
	s.preConsumed = res.PreConsumed
	s.AmountTotal = res.AmountTotal
	s.AmountUsedAfter = res.AmountUsedAfter
	if planInfo, err := model.GetSubscriptionPlanInfoByUserSubscriptionId(res.UserSubscriptionId); err == nil && planInfo != nil {
		s.PlanId = planInfo.PlanId
		s.PlanTitle = planInfo.PlanTitle
	}
	return nil
}

func (s *SubscriptionFunding) Settle(delta int) error {
	if delta == 0 {
		return nil
	}
	return model.PostConsumeUserSubscriptionDelta(s.subscriptionId, int64(delta))
}

func (s *SubscriptionFunding) Refund() error {
	if s.preConsumed <= 0 {
		return nil
	}
	return refundWithRetry(func() error {
		return model.RefundSubscriptionPreConsume(s.requestId)
	})
}

// ---------------------------------------------------------------------------
// OrganizationSubscriptionFunding — organization subscription + organization wallet
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

// refundWithRetry
// try to refund with retries, only for refund functions based on transactions!!!
func refundWithRetry(fn func() error) error {
	if fn == nil {
		return nil
	}
	const maxAttempts = 3
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < maxAttempts-1 {
			time.Sleep(time.Duration(200*(i+1)) * time.Millisecond)
		}
	}
	return lastErr
}
