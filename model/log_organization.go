package model

import "github.com/QuantumNous/new-api/common"

// fork: O3 — organization admins list their members' logs. Mirrors GetUserLogs
// (same filter, count limit, ClickHouse order and user-visibility formatting).
func GetLogsByUserIDs(userIDs []int, filter LogFilter, startIdx int, num int) (logs []*Log, total int64, err error) {
	if len(userIDs) == 0 {
		return []*Log{}, 0, nil
	}
	tx, err := filter.apply(LOG_DB.Where("logs.user_id IN ?", userIDs))
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error; err != nil {
		common.SysError(common.LogText("failed to count organization logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	if err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error; err != nil {
		common.SysError(common.LogText("failed to search organization logs: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to query logs")
	}
	formatUserLogs(logs, startIdx)
	return logs, total, nil
}
