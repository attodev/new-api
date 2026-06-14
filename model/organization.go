package model

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
	Quota       int    `json:"quota" gorm:"type:int;default:0"`
	UsedQuota   int    `json:"used_quota" gorm:"type:int;default:0;column:used_quota"`
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

func GetOrganizationQuota(organizationId int) (int, error) {
	if organizationId <= 0 {
		return 0, nil
	}

	var quota int
	err := DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Select("quota").
		First(&quota).Error
	return quota, err
}

func IncreaseOrganizationQuota(organizationId int, quota int) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if organizationId <= 0 || quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Update("quota", gorm.Expr("quota + ?", quota)).Error
}

func DecreaseOrganizationQuota(organizationId int, quota int) error {
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

func UpdateOrganizationUsedQuota(organizationId int, quota int) error {
	if organizationId <= 0 || quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).
		Where("id = ?", organizationId).
		Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
}

func CreateOrganization(name string, description string, ownerUserId int, quota ...int) (*Organization, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || ownerUserId <= 0 {
		return nil, errors.New("invalid organization parameters")
	}
	initialQuota := 0
	if len(quota) > 0 {
		initialQuota = quota[0]
	}
	if initialQuota < 0 {
		return nil, errors.New("quota cannot be negative")
	}

	var owner User
	if err := DB.First(&owner, ownerUserId).Error; err != nil {
		return nil, err
	}
	if owner.OrganizationId > 0 {
		return nil, errors.New("user already belongs to an organization")
	}

	org := &Organization{
		Name:        name,
		Description: description,
		OwnerUserId: ownerUserId,
		Quota:       initialQuota,
		Status:      OrganizationStatusEnabled,
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
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

func DeleteOrganization(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		// 1. Find org to get owner
		var org Organization
		if err := tx.Where("id = ?", id).First(&org).Error; err != nil {
			return err
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

		// 4. Delete the organization
		result := tx.Where("id = ?", id).Delete(&Organization{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return nil
	})
}
