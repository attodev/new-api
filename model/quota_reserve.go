package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

var ErrQuotaCachePending = errors.New("quota cache unavailable while database updates are pending")

const userQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', -tonumber(ARGV[1]))
return 1`

const userQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', tonumber(ARGV[1]))
return 1`

const tokenQuotaReserveScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remain = tonumber(redis.call('HGET', KEYS[1], 'RemainQuota'))
if remain == nil or remain < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', -tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

const tokenQuotaDeltaScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', tonumber(ARGV[1]))
redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func cacheTryReserveUserQuota(userID int, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{getUserCacheKey(userID)}, amount, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyUserQuotaDelta(userID int, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{getUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{getTokenCacheKey(key)}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaDeltaScript,
		[]string{getTokenCacheKey(key)}, delta, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func persistTokenQuotaDelta(id int, delta int) error {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, int64(delta))
		return nil
	}
	result := DB.Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", delta),
			"used_quota":    gorm.Expr("used_quota - ?", delta),
			"accessed_time": common.GetTimestamp(),
		},
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func applyTokenQuotaDelta(id int, key string, delta int) error {
	if common.BatchUpdateEnabled {
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
		defer batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
		addNewRecordLocked(BatchUpdateTypeTokenQuota, id, int64(delta))
		if common.RedisEnabled {
			if _, cacheErr := cacheApplyTokenQuotaDelta(id, key, int64(delta)); cacheErr != nil {
				common.SysLog("failed to synchronously update token quota cache: " + cacheErr.Error())
			}
		}
		return nil
	}
	cacheApplied := false
	if common.RedisEnabled {
		result, cacheErr := cacheApplyTokenQuotaDelta(id, key, int64(delta))
		if cacheErr != nil {
			common.SysLog("failed to synchronously update token quota cache: " + cacheErr.Error())
		} else {
			cacheApplied = result == cacheQuotaOK
		}
	}

	if err := persistTokenQuotaDelta(id, delta); err != nil {
		if cacheApplied {
			compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, -int64(delta))
			if compensateErr != nil || compensated != cacheQuotaOK {
				common.SysError(fmt.Sprintf("failed to compensate token quota delta: result=%d error=%v", compensated, compensateErr))
			}
		}
		return err
	}
	return nil
}

func persistUserQuotaDelta(id int, delta int64, forceDB bool) error {
	if !forceDB && common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, delta)
		return nil
	}
	result := DB.Model(&User{}).Where("id = ?", id).
		Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func applyUserQuotaDelta(id int, delta int64, forceDB bool) error {
	if !forceDB && common.BatchUpdateEnabled {
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
		addNewRecordLocked(BatchUpdateTypeUserQuota, id, delta)
		if common.RedisEnabled {
			if _, cacheErr := cacheApplyUserQuotaDelta(id, delta); cacheErr != nil {
				common.SysLog("failed to synchronously update user quota cache: " + cacheErr.Error())
			}
		}
		return nil
	}
	cacheApplied := false
	if common.RedisEnabled {
		result, cacheErr := cacheApplyUserQuotaDelta(id, delta)
		if cacheErr != nil {
			common.SysLog("failed to synchronously update user quota cache: " + cacheErr.Error())
		} else {
			cacheApplied = result == cacheQuotaOK
		}
	}

	if err := persistUserQuotaDelta(id, delta, forceDB); err != nil {
		if cacheApplied {
			compensated, compensateErr := cacheApplyUserQuotaDelta(id, -delta)
			if compensateErr != nil || compensated != cacheQuotaOK {
				common.SysError(fmt.Sprintf("failed to compensate user quota delta: result=%d error=%v", compensated, compensateErr))
			}
		}
		return err
	}
	return nil
}

func reserveUserQuotaDB(id int, quota int64) (bool, error) {
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func reserveTokenQuotaDB(id int, quota int) (bool, error) {
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// TryReserveUserQuota atomically checks and deducts user wallet quota. Redis
// remains authoritative while batched database deltas are pending.
func TryReserveUserQuota(id int, quota int64) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if !common.RedisEnabled {
		return reserveUserQuotaDB(id, quota)
	}
	if common.BatchUpdateEnabled {
		if _, err := GetUserCache(id); err != nil {
			return false, err
		}
		batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
		defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
		result, err := cacheTryReserveUserQuota(id, quota)
		if err != nil || result == cacheQuotaMiss {
			return false, fmt.Errorf("%w: user %d", ErrQuotaCachePending, id)
		}
		if result == cacheQuotaInsufficient {
			return false, nil
		}
		addNewRecordLocked(BatchUpdateTypeUserQuota, id, -quota)
		return true, nil
	}

	result, err := cacheTryReserveUserQuota(id, quota)
	if err == nil && result == cacheQuotaMiss && !hasPendingBatchRecord(BatchUpdateTypeUserQuota, id) {
		if _, hydrateErr := GetUserCache(id); hydrateErr == nil {
			result, err = cacheTryReserveUserQuota(id, quota)
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if common.BatchUpdateEnabled && hasPendingBatchRecord(BatchUpdateTypeUserQuota, id) {
			return false, fmt.Errorf("%w: user %d", ErrQuotaCachePending, id)
		}
		return reserveUserQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistUserQuotaDelta(id, -quota, false); err != nil {
		compensated, compensateErr := cacheApplyUserQuotaDelta(id, quota)
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(fmt.Sprintf("failed to compensate reserved user quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}

// TryReserveTokenQuota atomically checks and deducts token quota. Redis is
// authoritative while batched database updates are pending, preventing a
// stale database balance from accepting additional requests.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, DecreaseTokenQuota(id, key, quota)
	}
	if !common.RedisEnabled {
		return reserveTokenQuotaDB(id, quota)
	}
	if common.BatchUpdateEnabled {
		if _, err := GetTokenByKey(key, false); err != nil {
			return false, err
		}
		batchUpdateLocks[BatchUpdateTypeTokenQuota].Lock()
		defer batchUpdateLocks[BatchUpdateTypeTokenQuota].Unlock()
		result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
		if err != nil || result == cacheQuotaMiss {
			return false, fmt.Errorf("%w: token %d", ErrQuotaCachePending, id)
		}
		if result == cacheQuotaInsufficient {
			return false, nil
		}
		addNewRecordLocked(BatchUpdateTypeTokenQuota, id, -int64(quota))
		return true, nil
	}

	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cacheQuotaMiss && !hasPendingBatchRecord(BatchUpdateTypeTokenQuota, id) {
		if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
			result, err = cacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if common.BatchUpdateEnabled && hasPendingBatchRecord(BatchUpdateTypeTokenQuota, id) {
			return false, fmt.Errorf("%w: token %d", ErrQuotaCachePending, id)
		}
		if err != nil {
			common.SysLog("token quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveTokenQuotaDB(id, quota)
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistTokenQuotaDelta(id, -quota); err != nil {
		compensated, compensateErr := cacheApplyTokenQuotaDelta(id, key, int64(quota))
		if compensateErr != nil || compensated != cacheQuotaOK {
			common.SysError(fmt.Sprintf("failed to compensate reserved token quota: result=%d error=%v", compensated, compensateErr))
		}
		return false, err
	}
	return true, nil
}
