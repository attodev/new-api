package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	// Like increaseUserQuota, this deliberately does NOT enforce common.MaxQuota:
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
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", ownerUserId).First(&owner).Error; err != nil {
			return err
		}
		if owner.OrganizationId > 0 {
			return errors.New("user already belongs to an organization")
		}
		if err := DeactivatePersonalTossBillingForOrganizationJoin(tx, ownerUserId); err != nil {
			return err
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

// AssignUserToOrganizationWithBillingLifecycle serializes membership changes
// on the user row and ends personal Toss recurring billing before a personal
// wallet disappears from the UI/funding path. Cross-organization transfers are
// deliberately rejected; they require a separate owner/organization lifecycle.
func AssignUserToOrganizationWithBillingLifecycle(userID, organizationID int, organizationRole string) error {
	if userID <= 0 || organizationID <= 0 || !IsValidOrganizationRole(organizationRole) {
		return errors.New("invalid organization assignment")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if user.OrganizationId != 0 && user.OrganizationId != organizationID {
			return errors.New("user belongs to another organization")
		}
		var organization Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status").Where("id = ?", organizationID).First(&organization).Error; err != nil {
			return err
		}
		if organization.Status != OrganizationStatusEnabled {
			return errors.New("organization is not active")
		}
		if user.OrganizationId == 0 {
			if err := DeactivatePersonalTossBillingForOrganizationJoin(tx, userID); err != nil {
				return err
			}
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

// UpdateOrganizationFieldsWithBillingLifecycle applies organization updates
// and atomically disables organization-targeted wallet billing when the
// organization is disabled or its billing owner changes.
func UpdateOrganizationFieldsWithBillingLifecycle(id int, updates map[string]interface{}) error {
	if id <= 0 {
		return errors.New("invalid organization id")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&org).Error; err != nil {
			return err
		}
		stopBilling := false
		if status, ok := updates["status"].(int); ok && status == OrganizationStatusDisabled {
			stopBilling = true
		}
		if ownerUserID, ok := updates["owner_user_id"].(int); ok && ownerUserID > 0 && ownerUserID != org.OwnerUserId {
			stopBilling = true
		}
		if stopBilling {
			if err := DeactivateTossBillingForOrganization(tx, id); err != nil {
				return err
			}
		}
		return tx.Model(&org).Updates(updates).Error
	})
}

var errOrganizationDeleteOwnerChanged = errors.New("organization owner changed during deletion")

func DeleteOrganization(id int) error {
	if id <= 0 {
		return errors.New("invalid organization id")
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
				err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).
					Select("id").Where("id = ?", reference.OwnerUserId).First(&owner).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
			}

			var org Organization
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&org).Error; err != nil {
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

			// Stop scheduled/threshold charges before deleting the target. Remote
			// billing-key deletion is retried asynchronously from pending_revocation.
			if err := DeactivateTossBillingForOrganization(tx, id); err != nil {
				return err
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
			return err
		}
	}
	return errors.New("organization owner changed concurrently, retry deletion")
}
