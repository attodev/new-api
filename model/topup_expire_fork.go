package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O3 — admin expiry of abandoned personal top-up orders.

// personalTopUpExpireAge is the safety age below which a pending personal
// order is kept: the user may still be in the provider checkout.
const personalTopUpExpireAge = time.Hour

var errPersonalTopUpExpireUser = errors.New("invalid user id")

// ExpirePendingPersonalTopUps marks pending personal top-up orders (any
// target other than an organization wallet, as hasPendingPersonalOrderTx
// counts them) older than personalTopUpExpireAge expired, for one user or for
// every user when userId is 0. Younger pending personal orders are counted in
// skippedRecent. Organization orders are never touched (see
// ExpirePendingOrganizationTopUps). Such an order otherwise refuses the user's
// organization join until it settles; settlement only credits pending orders,
// so a late provider callback for an expired order credits nothing.
func ExpirePendingPersonalTopUps(userId int) (expired int64, skippedRecent int64, err error) {
	if userId < 0 {
		return 0, 0, errPersonalTopUpExpireUser
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		pending := func() *gorm.DB {
			query := tx.Model(&TopUp{}).Where("status = ? AND (target_type IS NULL OR target_type <> ?)",
				common.TopUpStatusPending, TopUpTargetTypeOrganization)
			if userId > 0 {
				query = query.Where("user_id = ?", userId)
			}
			return query
		}
		result := pending().Where("create_time < ?", time.Now().Add(-personalTopUpExpireAge).Unix()).
			Update("status", common.TopUpStatusExpired)
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
