package model

// fork: O3 — organizations. Hooks: model/user.go (OrganizationId/OrganizationRole), model/main.go (MigrateForkOrganizationTables).

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	OrganizationStatusEnabled  = 1
	OrganizationStatusDisabled = 2
)

const (
	OrganizationRoleMember = "member"
	OrganizationRoleAdmin  = "admin"
	OrganizationRoleOwner  = "owner"
)

type Organization struct {
	Id          int    `json:"id"`
	Name        string `json:"name" gorm:"type:varchar(64);not null;uniqueIndex" validate:"max=64"`
	Description string `json:"description,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	OwnerUserId int    `json:"owner_user_id" gorm:"column:owner_user_id;index"`
	Quota       int64  `json:"quota" gorm:"type:bigint;default:0"`
	UsedQuota   int64  `json:"used_quota" gorm:"type:bigint;default:0;column:used_quota"`
	Status      int    `json:"status" gorm:"type:int;default:1"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

func IsValidOrganizationRole(role string) bool {
	return role == OrganizationRoleMember || role == OrganizationRoleAdmin || role == OrganizationRoleOwner
}

func HasOrganizationAdminRole(role string) bool {
	return role == OrganizationRoleAdmin || role == OrganizationRoleOwner
}

func HasOrganizationOwnerRole(role string) bool {
	return role == OrganizationRoleOwner
}

func GetOrganizationOwnerUserId(organizationId int) (int, error) {
	if organizationId <= 0 {
		return 0, nil
	}

	var ownerUserId int
	err := DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Select("owner_user_id").
		First(&ownerUserId).Error
	if err != nil {
		return 0, err
	}
	return ownerUserId, nil
}

func GetOrganizationQuota(organizationId int) (int64, error) {
	if organizationId <= 0 {
		return 0, nil
	}

	var quota int64
	err := DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Select("quota").
		First(&quota).Error
	return quota, err
}

func IncreaseOrganizationQuota(organizationId int, quota int64) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if organizationId <= 0 || quota == 0 {
		return nil
	}
	// Like increaseUserQuota, this deliberately does NOT enforce common.MaxWalletQuota:
	// its callers are subscription and wallet refund paths, and a refund that
	// cannot be applied destroys quota the organization already paid for. The
	// ceiling is enforced where new quota enters instead -- CreditTopUpTarget for
	// payments, request validation for admin writes.
	return DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Update("quota", gorm.Expr("quota + ?", quota)).Error
}

func DecreaseOrganizationQuota(organizationId int, quota int64) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if organizationId <= 0 || quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Update("quota", gorm.Expr("quota - ?", quota)).Error
}

// TryReserveOrganizationQuota atomically deducts a shared organization wallet
// only when the full amount is available.
func TryReserveOrganizationQuota(organizationId int, quota int64) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if organizationId <= 0 || quota == 0 {
		return true, nil
	}
	result := DB.Model(&Organization{}).
		Where("id = ? AND quota >= ?", organizationId, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func UpdateOrganizationUsedQuota(organizationId int, quota int64) error {
	if organizationId <= 0 || quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
}

func CreateOrganization(name string, description string, ownerUserId int, quota ...int64) (*Organization, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || ownerUserId <= 0 {
		return nil, errors.New("invalid organization parameters")
	}
	var initialQuota int64
	if len(quota) > 0 {
		initialQuota = quota[0]
	}
	if initialQuota < 0 {
		return nil, errors.New("quota cannot be negative")
	}

	org := &Organization{
		Name:        name,
		Description: description,
		OwnerUserId: ownerUserId,
		Quota:       initialQuota,
		Status:      OrganizationStatusEnabled,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var owner User
		if err := lockForUpdate(tx).Where("id = ?", ownerUserId).First(&owner).Error; err != nil {
			return err
		}
		if owner.OrganizationId > 0 {
			return errors.New("user already belongs to an organization")
		}
		if err := tx.Create(org).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).
			Where("id = ?", ownerUserId).
			Updates(map[string]interface{}{
				"organization_id":   org.Id,
				"organization_role": OrganizationRoleOwner,
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return org, nil
}

// AssignUserToOrganization serializes membership changes on the user row.
// Cross-organization transfers are deliberately rejected; they require a
// separate owner/organization lifecycle.
func AssignUserToOrganization(userID, organizationID int, organizationRole string) error {
	if userID <= 0 || organizationID <= 0 || !IsValidOrganizationRole(organizationRole) {
		return errors.New("invalid organization assignment")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if user.OrganizationId != 0 && user.OrganizationId != organizationID {
			return errors.New("user belongs to another organization")
		}
		var organization Organization
		if err := lockForUpdate(tx).Select("id", "status").Where("id = ?", organizationID).First(&organization).Error; err != nil {
			return err
		}
		if organization.Status != OrganizationStatusEnabled {
			return errors.New("organization is not active")
		}
		result := tx.Model(&User{}).
			Where("id = ? AND organization_id = ?", userID, user.OrganizationId).
			Updates(map[string]interface{}{
				"organization_id":   organizationID,
				"organization_role": organizationRole,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("organization membership changed concurrently")
		}
		return nil
	})
}

func CanManageOrganizationTarget(actor User, target User) bool {
	if actor.OrganizationId == 0 || target.OrganizationId == 0 {
		return false
	}
	if actor.OrganizationId != target.OrganizationId {
		return false
	}
	if !HasOrganizationAdminRole(actor.OrganizationRole) {
		return false
	}
	if target.Role >= common.RoleAdminUser {
		return false
	}
	return true
}

// UpdateOrganizationFields applies organization updates under a row lock.
func UpdateOrganizationFields(id int, updates map[string]interface{}) error {
	if id <= 0 {
		return errors.New("invalid organization id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := lockForUpdate(tx).Where("id = ?", id).First(&org).Error; err != nil {
			return err
		}
		return tx.Model(&org).Updates(updates).Error
	})
}

var errOrganizationDeleteOwnerChanged = errors.New("organization owner changed during deletion")

func DeleteOrganization(id int) error {
	_, _, err := DeleteOrganizationClearingOwner(id)
	return err
}

// DeleteOrganizationClearingOwner deletes an organization without members and
// returns the owner whose membership (and member quota) was cleared, with that
// owner's quota before it was zeroed. ownerUserId is 0 when no owner row was
// cleared.
func DeleteOrganizationClearingOwner(id int) (ownerUserId int, ownerPreviousQuota int, err error) {
	if id <= 0 {
		return 0, 0, errors.New("invalid organization id")
	}
	// Reading the owner before taking locks is safe only when it is verified
	// again under the organization lock. If an owner transfer wins that race,
	// roll back and retry from the new owner instead of ever taking a user lock
	// after the organization lock. This keeps organization deletion compatible
	// with checkout/settlement's owner -> organization -> order sequence.
	for attempt := 0; attempt < 3; attempt++ {
		err := DB.Transaction(func(tx *gorm.DB) error {
			var reference Organization
			if err := tx.Select("owner_user_id").Where("id = ?", id).First(&reference).Error; err != nil {
				return err
			}
			if reference.OwnerUserId > 0 {
				var owner User
				err := lockForUpdate(tx.Unscoped()).
					Select("id").Where("id = ?", reference.OwnerUserId).First(&owner).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}

			var org Organization
			if err := lockForUpdate(tx).Where("id = ?", id).First(&org).Error; err != nil {
				return err
			}
			if org.OwnerUserId != reference.OwnerUserId {
				return errOrganizationDeleteOwnerChanged
			}

			// 2. Check for non-owner active members
			var memberCount int64
			if err := tx.Model(&User{}).
				Where("organization_id = ? AND deleted_at IS NULL AND id != ?", id, org.OwnerUserId).
				Count(&memberCount).Error; err != nil {
				return err
			}
			if memberCount > 0 {
				return fmt.Errorf("organization has %d non-owner members, remove them first", memberCount)
			}

			// 2. Delete user subscriptions
			if err := tx.Where("organization_id = ?", id).
				Delete(&OrganizationUserSubscription{}).Error; err != nil {
				return err
			}

			// 3. Delete subscription plans
			if err := tx.Where("organization_id = ?", id).
				Delete(&OrganizationSubscriptionPlan{}).Error; err != nil {
				return err
			}

			// 4. Clear org membership from remaining users (owner). The owner row
			// was locked before the organization, so this write cannot invert a
			// concurrent checkout, recovery, or user-deletion lifecycle.
			// The owner leaves through clearOrganizationMembershipTx so the member
			// quota cap does not survive as a personal balance.
			ownerUserId, ownerPreviousQuota = 0, 0
			if org.OwnerUserId > 0 {
				previousQuota, cleared, err := clearOrganizationMembershipTx(tx, org.OwnerUserId, id)
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				if cleared {
					ownerUserId, ownerPreviousQuota = org.OwnerUserId, previousQuota
				}
			}
			// Use Select to force-update zero-value fields that GORM might otherwise skip.
			if err := tx.Model(&User{}).
				Select("organization_id", "organization_role").
				Where("organization_id = ?", id).
				Updates(User{OrganizationId: 0, OrganizationRole: ""}).Error; err != nil {
				return err
			}

			// 5. Delete the organization
			result := tx.Where("id = ?", id).Delete(&Organization{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}

			return nil
		})
		if !errors.Is(err, errOrganizationDeleteOwnerChanged) {
			if err != nil {
				return 0, 0, err
			}
			return ownerUserId, ownerPreviousQuota, nil
		}
	}
	return 0, 0, errors.New("organization owner changed concurrently, retry deletion")
}

// ClearOrganizationMembership removes a user from their organization and zeroes
// users.quota in the same transaction: inside an organization the user quota is
// the member spending cap set by organization admins, so it must not survive as
// a personal balance once the user leaves. Returns the quota before clearing.
func ClearOrganizationMembership(userId int) (previousQuota int, err error) {
	if userId <= 0 {
		return 0, errors.New("invalid organization member")
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var cleared bool
		var clearErr error
		previousQuota, cleared, clearErr = clearOrganizationMembershipTx(tx, userId, 0)
		if clearErr != nil {
			return clearErr
		}
		if !cleared {
			return errors.New("user does not belong to an organization")
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return previousQuota, nil
}

// clearOrganizationMembershipTx locks the user and clears organization_id,
// organization_role and quota. organizationId > 0 restricts the clear to members
// of that organization; cleared is false when the user was not such a member.
func clearOrganizationMembershipTx(tx *gorm.DB, userId int, organizationId int) (previousQuota int, cleared bool, err error) {
	var user User
	if err := lockForUpdate(tx).Select("id", "quota", "organization_id").Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, false, err
	}
	if user.OrganizationId == 0 || (organizationId > 0 && user.OrganizationId != organizationId) {
		return 0, false, nil
	}
	if err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"organization_id":   0,
		"organization_role": "",
		"quota":             0,
	}).Error; err != nil {
		return 0, false, err
	}
	return user.Quota, true, nil
}
