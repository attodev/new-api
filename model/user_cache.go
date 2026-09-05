package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
)

const userCacheSchemaVersion = 1

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id               int    `json:"id"`
	Group            string `json:"group"`
	OrganizationId   int    `json:"organization_id"`
	OrganizationRole string `json:"organization_role"`
	Email            string `json:"email"`
	Quota            int64  `json:"quota"`
	Status           int    `json:"status"`
	Username         string `json:"username"`
	Setting          string `json:"setting"`
	CacheSchema      int    `json:"-"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// controller
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

const writeUserCacheScript = `
local preserveQuota = tonumber(ARGV[11]) == 1
local complete = tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') == tonumber(ARGV[1])
  and redis.call('HEXISTS', KEYS[1], 'Quota') == 1
local quota = ARGV[6]
if preserveQuota and complete then
  quota = redis.call('HGET', KEYS[1], 'Quota')
end
redis.call('HSET', KEYS[1],
  'Id', ARGV[1], 'Group', ARGV[2], 'OrganizationId', ARGV[3],
  'OrganizationRole', ARGV[4], 'Email', ARGV[5], 'Quota', quota,
  'Status', ARGV[7], 'Username', ARGV[8], 'Setting', ARGV[9],
  'CacheSchema', ARGV[10])
redis.call('EXPIRE', KEYS[1], ARGV[12])
return 1`

func writeUserCache(user User, preserveQuota bool) error {
	if !common.RedisEnabled {
		return nil
	}
	ttl := quotaCacheTTLSeconds()
	preserve := 0
	if preserveQuota {
		preserve = 1
	}
	return common.RDB.Eval(context.Background(), writeUserCacheScript,
		[]string{getUserCacheKey(user.Id)}, user.Id, user.Group,
		user.OrganizationId, user.OrganizationRole, user.Email, user.Quota,
		user.Status, user.Username, user.Setting, userCacheSchemaVersion,
		preserve, ttl).Err()
}

// updateUserCache refreshes metadata without replacing quota maintained by
// atomic delta operations.
func updateUserCache(user User) error {
	return writeUserCache(user, true)
}

func populateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	if !common.BatchUpdateEnabled {
		return writeUserCache(user, true)
	}
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	if hasPendingBatchRecordLocked(BatchUpdateTypeUserQuota, user.Id) {
		return fmt.Errorf("%w: user %d", ErrQuotaCachePending, user.Id)
	}
	return writeUserCache(user, true)
}

// hydrateUserCache resolves the user profile and, on a cache miss, publishes
// the database snapshot to Redis. It reports ErrQuotaCachePending when batched
// quota deltas have not reached the database yet, because republishing that
// snapshot would resurrect quota the cache has already spent. The profile is
// returned alongside that error so callers that do not need an authoritative
// balance can still proceed. Use this only where the balance must be exact.
func hydrateUserCache(userId int) (userCache *UserBase, err error) {
	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// If Redis fails, get from DB
	user, err := GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}

	if common.RedisEnabled {
		// A read miss may be a transient Redis error rather than an absent hash.
		// Preserve an already-complete cache balance so a stale DB snapshot cannot
		// overwrite batched quota deltas that have not reached the database yet.
		if cacheErr := populateUserCache(*user); cacheErr != nil {
			if errors.Is(cacheErr, ErrQuotaCachePending) {
				return user.ToBaseUser(), cacheErr
			}
			common.SysLog("failed to synchronously populate user cache: " + cacheErr.Error())
		}
	}
	return user.ToBaseUser(), nil
}

// GetUserCache gets complete user cache from hash.
// A pending quota batch blocks cache publication, not the read itself: callers
// such as request authentication need identity, status and group, and failing
// them would turn routine quota bookkeeping into 500s under concurrency. The
// returned quota may lag the cache by the pending deltas, so spending paths
// must reserve through TryReserveUserQuota instead of trusting this value.
func GetUserCache(userId int) (*UserBase, error) {
	userCache, err := hydrateUserCache(userId)
	if errors.Is(err, ErrQuotaCachePending) {
		return userCache, nil
	}
	return userCache, err
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id != userId || userCache.CacheSchema != userCacheSchemaVersion {
		return nil, fmt.Errorf("user cache schema is stale")
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := cacheApplyUserQuotaDelta(userId, delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int64, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	if !common.RedisEnabled {
		return nil
	}
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Status", fmt.Sprintf("%d", statusInt))
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Setting", setting)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
