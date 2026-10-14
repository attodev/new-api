package service

import (
	"github.com/QuantumNous/new-api/model"
)

// fork: O3 — PreWssConsumeQuota (service/quota.go) checks the personal balance
// and the token before every realtime (WSS) charge. For an organization member
// users.quota is only the member spending cap, so the shared organization
// wallet must also cover the charge. Hook: one line after the user-quota check
// in PreWssConsumeQuota.

// requireOrganizationWalletFor returns ErrInsufficientOrganizationWalletQuota
// when userId is an organization member and the organization wallet holds less
// than quota. Users outside an organization pass without a wallet read.
func requireOrganizationWalletFor(userId int, quota int) error {
	organizationId, _, err := model.GetUserOrganizationMembership(userId)
	if err != nil {
		return err
	}
	if organizationId <= 0 {
		return nil
	}
	organizationQuota, err := model.GetOrganizationQuota(organizationId)
	if err != nil {
		return err
	}
	if organizationQuota < int64(quota) {
		return ErrInsufficientOrganizationWalletQuota
	}
	return nil
}
