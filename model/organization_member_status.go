package model

// fork: O3 — organization-scoped member status and note (plan decision 7).
// Organization admins suspend a member or keep a note on them through
// users.organization_member_status / users.organization_remark; the platform
// users.status / users.remark stay root-only. Hooks: model/user.go (the two
// struct fields), service/billing_session_organization.go and
// service/midjourney_organization.go (relay refusal).

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	OrganizationMemberStatusActive    = 1
	OrganizationMemberStatusSuspended = 2
)

// ErrOrganizationMemberSuspended refuses relay and legacy Midjourney requests
// of a member an organization admin suspended. The member's own reads (wallet
// summary, organization plan, top-up history) stay allowed.
var ErrOrganizationMemberSuspended = common.NewMessage("organization membership is suspended")

// IsValidOrganizationMemberStatus reports whether status is active or suspended.
func IsValidOrganizationMemberStatus(status int) bool {
	return status == OrganizationMemberStatusActive || status == OrganizationMemberStatusSuspended
}

// RequireActiveOrganizationMember returns ErrOrganizationMemberSuspended when
// the user belongs to an organization and is suspended there. A stray value on
// a user outside any organization has no effect.
func RequireActiveOrganizationMember(userId int) error {
	var user User
	if err := DB.Select("organization_id", "organization_member_status").
		Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.OrganizationId > 0 && user.OrganizationMemberStatus == OrganizationMemberStatusSuspended {
		return ErrOrganizationMemberSuspended
	}
	return nil
}

// GetUserOrganizationMembership reads organization_id and
// organization_member_status in one primary-key read, so the relay billing
// session checks membership and suspension without a second query.
func GetUserOrganizationMembership(userId int) (organizationId int, memberStatus int, err error) {
	if userId == 0 {
		return 0, 0, common.NewMessage("ID is empty!") // same as GetUserById
	}
	var user User
	if err := DB.Select("organization_id", "organization_member_status").
		Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, 0, err
	}
	return user.OrganizationId, user.OrganizationMemberStatus, nil
}

// UpdateOrganizationMemberFields writes member fields scoped to the
// organization: a user who left (or moved) after the caller's permission check
// is not written and ErrOrganizationMembershipChanged is returned. A write that
// changes nothing (MySQL then reports 0 affected rows) is not an error while
// the user is still a member.
func UpdateOrganizationMemberFields(userId int, organizationId int, updates map[string]interface{}) error {
	return updateOrganizationMemberFieldsTx(DB, userId, organizationId, updates)
}

func updateOrganizationMemberFieldsTx(tx *gorm.DB, userId int, organizationId int, updates map[string]interface{}) error {
	if userId <= 0 || organizationId <= 0 {
		return errors.New("invalid organization member")
	}
	if len(updates) == 0 {
		return nil
	}
	result := tx.Model(&User{}).
		Where("id = ? AND organization_id = ?", userId, organizationId).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var members int64
	if err := tx.Model(&User{}).Where("id = ? AND organization_id = ?", userId, organizationId).Count(&members).Error; err != nil {
		return err
	}
	if members == 0 {
		return ErrOrganizationMembershipChanged
	}
	return nil
}
