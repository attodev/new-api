package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// The wallet-ceiling errors reach the web console, so they carry message keys.
var (
	ErrCheckinWalletLimit     error = common.NewMessage("Check-in failed: wallet quota limit reached")
	errCheckinQuotaUpdate     error = common.NewMessage("Check-in failed: quota update error")
	ErrAffTransferWalletLimit error = common.NewMessage("Invitation quota transfer failed: wallet quota limit reached")
)

// creditCheckinReward credits a check-in reward within the wallet ceiling in
// the check-in transaction, so a rejected reward rolls the check-in row back
// (team 2ba5cc513; upstream checkin.go credits without a ceiling).
func creditCheckinReward(tx *gorm.DB, userId, quota int) error {
	if quota <= 0 {
		return nil
	}
	if err := creditTopUpQuota(tx, userId, quota, nil); err != nil {
		if errors.Is(err, ErrTopUpQuotaLimitExceeded) {
			return ErrCheckinWalletLimit
		}
		return errCheckinQuotaUpdate
	}
	return nil
}

// checkAffTransferCeiling rejects an invitation quota transfer that would
// push the wallet past the ceiling.
func checkAffTransferCeiling(currentQuota, quota int) error {
	if quota > common.MaxWalletQuota-currentQuota {
		return ErrAffTransferWalletLimit
	}
	return nil
}
