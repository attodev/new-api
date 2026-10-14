package model

import "github.com/QuantumNous/new-api/common"

// fork: O3 — inside an organization users.quota is the member spending cap that
// organization admins set freely; no money backs it. It must not buy platform
// subscriptions, and personal money must not flow into it (it is zeroed when the
// member leaves). Hooks: PurchaseSubscriptionWithBalance (model/subscription.go),
// TopUp redemption (controller/user.go), and ValidateTopUpTargetCapacity, which
// every personal pay/amount flow reaches through validateTopUpTargetCapacity.

var (
	ErrOrganizationMemberBalancePay    = common.NewMessage("organization members cannot pay with personal balance")
	ErrOrganizationMemberRedeem        = common.NewMessage("organization members cannot redeem codes into their personal balance")
	ErrOrganizationMemberPersonalTopUp = common.NewMessage("organization members cannot top up their personal balance")
)

// rejectOrganizationMemberBalancePay is called with the user row already locked
// by PurchaseSubscriptionWithBalance.
func rejectOrganizationMemberBalancePay(user *User) error {
	if user != nil && user.OrganizationId > 0 {
		return ErrOrganizationMemberBalancePay
	}
	return nil
}

// GetUserOrganizationId reads only organization_id (uncached: organization
// fields are kept out of the user cache).
func GetUserOrganizationId(userId int) (int, error) {
	var user User
	if err := DB.Select("organization_id").Where("id = ?", userId).First(&user).Error; err != nil {
		return 0, err
	}
	return user.OrganizationId, nil
}

// RejectOrganizationMemberRedeem refuses redemption codes for organization members.
func RejectOrganizationMemberRedeem(userId int) error {
	organizationId, err := GetUserOrganizationId(userId)
	if err != nil {
		return err
	}
	if organizationId > 0 {
		return ErrOrganizationMemberRedeem
	}
	return nil
}

func rejectOrganizationMemberPersonalTopUp(userId int) error {
	organizationId, err := GetUserOrganizationId(userId)
	if err != nil {
		return err
	}
	if organizationId > 0 {
		return ErrOrganizationMemberPersonalTopUp
	}
	return nil
}
