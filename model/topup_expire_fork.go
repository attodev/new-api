package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O3 — admin expiry of abandoned personal orders.

// personalTopUpExpireAge is the safety age below which a pending personal
// order is kept: the user may still be in the provider checkout.
const personalTopUpExpireAge = time.Hour

var errPersonalTopUpExpireUser = errors.New("invalid user id")

// PersonalOrderExpiry counts what ExpirePendingPersonalTopUps changed: expired
// orders and pending orders kept because they are younger than
// personalTopUpExpireAge, for top-ups and subscription orders separately.
type PersonalOrderExpiry struct {
	ExpiredTopUps              int64 `json:"expired"`
	SkippedRecentTopUps        int64 `json:"skipped_recent"`
	ExpiredSubscriptions       int64 `json:"expired_subscriptions"`
	SkippedRecentSubscriptions int64 `json:"skipped_recent_subscriptions"`
}

// ExpirePendingPersonalTopUps marks the abandoned personal orders that
// hasPendingPersonalOrderTx counts — pending top-ups for any target other than
// an organization wallet, and pending subscription orders (always personal) —
// expired when they are older than personalTopUpExpireAge, for one user or
// for every user when userId is 0, in one transaction. Organization top-ups
// are never touched (see ExpirePendingOrganizationTopUps). Such an order
// otherwise refuses the user's organization join until it settles; settlement
// only credits pending orders, so a late provider callback for an expired
// order credits nothing. Subscription orders follow ExpireSubscriptionOrder:
// status expired and complete_time set.
func ExpirePendingPersonalTopUps(userId int) (result PersonalOrderExpiry, err error) {
	if userId < 0 {
		return PersonalOrderExpiry{}, errPersonalTopUpExpireUser
	}
	cutoff := time.Now().Add(-personalTopUpExpireAge).Unix()
	err = DB.Transaction(func(tx *gorm.DB) error {
		forUser := func(query *gorm.DB) *gorm.DB {
			if userId > 0 {
				return query.Where("user_id = ?", userId)
			}
			return query
		}
		pendingTopUps := func() *gorm.DB {
			return forUser(tx.Model(&TopUp{}).Where("status = ? AND (target_type IS NULL OR target_type <> ?)",
				common.TopUpStatusPending, TopUpTargetTypeOrganization))
		}
		pendingSubscriptions := func() *gorm.DB {
			return forUser(tx.Model(&SubscriptionOrder{}).Where("status = ?", common.TopUpStatusPending))
		}

		topUps := pendingTopUps().Where("create_time < ?", cutoff).Update("status", common.TopUpStatusExpired)
		if topUps.Error != nil {
			return topUps.Error
		}
		result.ExpiredTopUps = topUps.RowsAffected
		if err := pendingTopUps().Count(&result.SkippedRecentTopUps).Error; err != nil {
			return err
		}

		subscriptions := pendingSubscriptions().Where("create_time < ?", cutoff).Updates(map[string]any{
			"status":        common.TopUpStatusExpired,
			"complete_time": common.GetTimestamp(),
		})
		if subscriptions.Error != nil {
			return subscriptions.Error
		}
		result.ExpiredSubscriptions = subscriptions.RowsAffected
		return pendingSubscriptions().Count(&result.SkippedRecentSubscriptions).Error
	})
	if err != nil {
		return PersonalOrderExpiry{}, err
	}
	return result, nil
}
