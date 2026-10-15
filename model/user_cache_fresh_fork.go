package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — 정보 신선 기한. 요청이 해시를 읽을 때만 확인하며, 지났으면
// 사용자 정보만 DB에서 다시 읽어 덮어쓴다. 잔액은 두고 TTL은 연장하지 않는다.
// 설계 §4.3, §4.4.

var errUserInfoStale = errors.New("user cache info is stale")

func userInfoFreshUntil() int64 {
	return common.GetTimestamp() + int64(userCacheTTLSeconds())
}

func userInfoStale(u *UserBase) bool {
	return quotaCacheResidencyEnabled() && u.FreshUntil <= common.GetTimestamp()
}

// writeUserCacheResident is upstream writeUserCache's script (model/user_auth_cache.go)
// with three changes: FreshUntil is written, EXPIRE is set only when the hash
// has no TTL yet (new hash), and that TTL is the quota TTL. Keep in sync on merges.
// upstream 복사본, 병합 때 원본 변경 반영.
func writeUserCacheResident(user *UserBase, includeQuota bool) error {
	user.CacheSchema = userCacheSchemaVersion
	if user.AuthVersion <= 0 {
		return errors.New("invalid user auth version")
	}
	includeQuotaArg := "0"
	if includeQuota {
		includeQuotaArg = "1"
	}
	const script = `
local incoming = tonumber(ARGV[1])
local pending = tonumber(redis.call('GET', KEYS[2]) or '0')
local committed = tonumber(redis.call('GET', KEYS[3]) or '0')
local current = tonumber(redis.call('HGET', KEYS[1], 'AuthVersion') or '0')
if pending > incoming or committed > incoming or current > incoming then
  return 0
end
if committed < incoming then
  redis.call('SET', KEYS[3], ARGV[1])
end
if pending > 0 and pending <= incoming then
  redis.call('DEL', KEYS[2])
end
if ARGV[10] == '0' and redis.call('EXISTS', KEYS[1]) == 0 then
  return 1
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[2], 'Group', ARGV[3], 'Email', ARGV[4],
  'Status', ARGV[5], 'Role', ARGV[6], 'Username', ARGV[7],
  'Setting', ARGV[8], 'AuthVersion', ARGV[1], 'CacheSchema', ARGV[9],
  'FreshUntil', ARGV[13])
if ARGV[10] == '1' and redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  redis.call('HSET', KEYS[1], 'Quota', ARGV[11])
end
if redis.call('TTL', KEYS[1]) < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[12])
end
return 1`
	result, err := common.RDB.Eval(context.Background(), script,
		[]string{getUserCacheKey(user.Id), getUserAuthFenceKey(user.Id), getUserAuthVersionKey(user.Id)},
		user.AuthVersion, user.Id, user.Group, user.Email, user.Status, user.Role,
		user.Username, user.Setting, user.CacheSchema, includeQuotaArg, user.Quota,
		quotaCacheTTLSeconds(), userInfoFreshUntil(),
	).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrUserAuthCachePending
	}
	return nil
}

// userBaseAfterPopulate returns what callers must act on after a DB read:
// the hash balance (which may already be ahead of the DB by pending batches),
// not the DB snapshot. Falls back to the snapshot when the hash is unreadable.
func userBaseAfterPopulate(user *User) (*UserBase, error) {
	if !quotaCacheResidencyEnabled() {
		return user.ToBaseUser(), nil
	}
	cached, err := cacheGetUserBase(user.Id)
	if err != nil {
		return user.ToBaseUser(), nil
	}
	return cached, nil
}
