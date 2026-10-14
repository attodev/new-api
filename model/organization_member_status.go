package model

// fork: O3 — organization-scoped member status and note (plan decision 7).
// Organization admins suspend a member or keep a note on them through
// users.organization_member_status / users.organization_remark; the platform
// users.status / users.remark stay root-only. Hooks: model/user.go (the two
// struct fields), service/billing_session_organization.go and
// service/midjourney_organization.go (relay refusal).

import (
	"github.com/QuantumNous/new-api/common"
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
