package service

import (
	"github.com/QuantumNous/new-api/model"
)

// fork: O3 — PreWssConsumeQuota (service/quota.go) checks the personal balance
// and the token before every realtime (WSS) charge. For an organization member
// users.quota is only the member spending cap, so the organization wallet the
// charge goes to must also cover it. Hook: one line after the user-quota check
// in PreWssConsumeQuota.

// requireOrganizationWalletFor returns ErrInsufficientOrganizationWalletQuota
// when the organization pinned at session start (relayInfo.BillingOrganizationId,
// the same organization postConsumeOrganizationQuota charges) holds less than
// quota. organizationId 0 (no organization funding) passes without a read.
func requireOrganizationWalletFor(organizationId int, quota int) error {
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
