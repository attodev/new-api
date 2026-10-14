package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O3 — a top-up settles into the payer's wallet or the organization
// wallet the order was created for. Hooks: the creditTopUpQuota /
// syncCreditUserQuotaCache pairs in model/topup.go and model/topup_paypal.go
// call creditTopUpTarget / syncCreditTopUpCache instead.

const (
	TopUpTargetTypeUser         = "user"
	TopUpTargetTypeOrganization = "organization"
)

func (topUp *TopUp) EffectiveTargetType() string {
	if topUp != nil && topUp.TargetType == TopUpTargetTypeOrganization {
		return TopUpTargetTypeOrganization
	}
	return TopUpTargetTypeUser
}

// EffectiveTargetId returns the wallet owner id. An organization order with no
// target id yields 0 (never the payer's user id, which would address an
// unrelated organization).
func (topUp *TopUp) EffectiveTargetId() int {
	if topUp.EffectiveTargetType() == TopUpTargetTypeOrganization {
		if topUp.TargetId > 0 {
			return topUp.TargetId
		}
		return 0
	}
	return topUp.UserId
}

var errOrganizationTopUpWithoutTarget = errors.New("organization top-up order has no target organization")

// ValidateOrganizationWalletCapacity is the pre-payment check for an
// organization wallet, mirroring ValidateTopUpQuotaCapacity. A disabled
// organization cannot be topped up.
func ValidateOrganizationWalletCapacity(orgId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	var org Organization
	if err := DB.Select("quota", "status").Where("id = ?", orgId).First(&org).Error; err != nil {
		return err
	}
	if org.Status != OrganizationStatusEnabled {
		return ErrOrganizationDisabled
	}
	if org.Quota > int64(maxCurrentQuota) {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

func ValidateTopUpTargetCapacity(targetType string, targetId int, creditedQuota int) error {
	if targetType == TopUpTargetTypeOrganization {
		if targetId <= 0 {
			return errOrganizationTopUpWithoutTarget
		}
		return ValidateOrganizationWalletCapacity(targetId, creditedQuota)
	}
	if err := RejectOrganizationMemberPersonalTopUp(targetId); err != nil {
		return err
	}
	return ValidateTopUpQuotaCapacity(targetId, creditedQuota)
}

// creditOrganizationQuota adds a top-up to an organization wallet with the
// same atomic ceiling predicate creditTopUpQuota uses for users. A negative
// balance (debt) can still be reduced; only the upper bound matters.
func creditOrganizationQuota(tx *gorm.DB, orgId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}
	result := tx.Model(&Organization{}).
		Where("id = ? AND quota <= ?", orgId, maxCurrentQuota).
		Update("quota", gorm.Expr("quota + ?", creditedQuota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var count int64
	if err := tx.Model(&Organization{}).Where("id = ?", orgId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return common.NewMessage("organization not found")
	}
	return ErrTopUpQuotaLimitExceeded
}

// creditTopUpTarget routes a settled order to its wallet. User-row updates
// (Stripe customer id, Creem email) only apply to user targets.
func creditTopUpTarget(tx *gorm.DB, topUp *TopUp, creditedQuota int, updates map[string]any) error {
	if topUp.EffectiveTargetType() == TopUpTargetTypeOrganization {
		orgId := topUp.EffectiveTargetId()
		if orgId <= 0 {
			return errOrganizationTopUpWithoutTarget
		}
		return creditOrganizationQuota(tx, orgId, creditedQuota)
	}
	return creditTopUpQuota(tx, topUp.UserId, creditedQuota, updates)
}

// syncCreditTopUpCache refreshes the payer's cached balance only when the
// payer's own wallet was credited; organization wallets are not cached.
func syncCreditTopUpCache(topUp *TopUp, creditedQuota int, operation string) {
	if topUp.EffectiveTargetType() == TopUpTargetTypeUser {
		syncCreditUserQuotaCache(topUp.UserId, creditedQuota, operation)
	}
}

func GetOrganizationTopUps(organizationId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	query := DB.Model(&TopUp{}).Where("target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationId)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	return topups, total, err
}

func SearchOrganizationTopUps(organizationId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	query := DB.Model(&TopUp{}).
		Where("target_type = ? AND target_id = ?", TopUpTargetTypeOrganization, organizationId)
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}
	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	return topups, total, err
}

// organizationTopUpExpireAge is the safety age below which a pending order of
// an enabled organization is kept: the owner may still be in the provider
// checkout.
const organizationTopUpExpireAge = time.Hour

// ExpirePendingOrganizationTopUps marks an organization's pending top-up
// orders expired (root action for abandoned checkouts, which otherwise block
// DeleteOrganizationClearingOwner). For an enabled organization only orders
// older than organizationTopUpExpireAge are expired and the younger ones are
// counted in skippedRecent; a disabled organization cannot be paid into, so
// all of its pending orders are expired. Settlement only credits pending
// orders, so a late provider callback for an expired order credits nothing.
func ExpirePendingOrganizationTopUps(organizationId int) (expired int64, skippedRecent int64, err error) {
	if organizationId <= 0 {
		return 0, 0, errOrganizationTopUpWithoutTarget
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", organizationId).First(&org).Error; err != nil {
			return err
		}
		pending := func() *gorm.DB {
			return tx.Model(&TopUp{}).Where("target_type = ? AND target_id = ? AND status = ?",
				TopUpTargetTypeOrganization, organizationId, common.TopUpStatusPending)
		}
		update := pending()
		if org.Status == OrganizationStatusEnabled {
			update = update.Where("create_time < ?", time.Now().Add(-organizationTopUpExpireAge).Unix())
		}
		result := update.Update("status", common.TopUpStatusExpired)
		if result.Error != nil {
			return result.Error
		}
		expired = result.RowsAffected
		return pending().Count(&skippedRecent).Error
	})
	if err != nil {
		return 0, 0, err
	}
	return expired, skippedRecent, nil
}
