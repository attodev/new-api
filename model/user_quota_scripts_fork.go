package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — upstream userQuotaReserveScript/userQuotaDeltaScript 복사본에
// 성공 경로의 EXPIRE(ARGV[4] = 잔액 TTL)만 더했다. 병합 때 upstream 스크립트가
// 바뀌면 여기에도 반영한다(model/quota_reserve.go:19-38).
const userQuotaReserveResidentScript = `
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
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1`

const userQuotaDeltaResidentScript = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[4])
return 1`

func evalUserQuotaReserve(userID int, amount int64) (int, error) {
	keys := []string{getUserCacheKey(userID)}
	if !quotaCacheResidencyEnabled() {
		return common.RDB.Eval(context.Background(), userQuotaReserveScript, keys, amount, userID, userCacheSchemaVersion).Int()
	}
	return common.RDB.Eval(context.Background(), userQuotaReserveResidentScript, keys, amount, userID, userCacheSchemaVersion, quotaCacheTTLSeconds()).Int()
}

func evalUserQuotaDelta(userID int, delta int64) (int, error) {
	keys := []string{getUserCacheKey(userID)}
	if !quotaCacheResidencyEnabled() {
		return common.RDB.Eval(context.Background(), userQuotaDeltaScript, keys, delta, userID, userCacheSchemaVersion).Int()
	}
	return common.RDB.Eval(context.Background(), userQuotaDeltaResidentScript, keys, delta, userID, userCacheSchemaVersion, quotaCacheTTLSeconds()).Int()
}
