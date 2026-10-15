package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// fork: O12 — TransferAffQuotaToQuota writes the wallet with Save and never
// touched the cache; with a resident balance that difference would persist.
func commitAffTransfer(tx *gorm.DB, userId int, quota int) error {
	if err := tx.Commit().Error; err != nil {
		return err
	}
	if err := cacheIncrUserQuota(userId, int64(quota)); err != nil {
		common.SysLog(common.LogText("failed to sync aff transfer to user quota cache: %s", err.Error()))
	}
	return nil
}
