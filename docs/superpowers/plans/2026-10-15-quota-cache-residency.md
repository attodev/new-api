# 사용자 잔액 캐시 상주 + Sentinel 연결 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 배치 업데이트를 켠 `develop`에서 사용자 지갑 잔액이 실제보다 많이 쓰여 음수가 되는 구멍을, 잔액 해시를 Redis에 상주시켜(TTL 24시간, 사용 중 연장) 닫는다. 요청을 막지 않고 DB로 우회하지 않으며 upstream 인증 장치는 그대로 둔다. 같이 앱이 Redis Sentinel에 직접 붙게 한다.

**Architecture:** 사용자 해시 `user:<id>`는 "잔액 TTL"(Redis EXPIRE, 잔액이 바뀔 때만 연장)과 "정보 신선 기한"(해시 필드 `FreshUntil`, 요청이 읽을 때만 확인)이라는 두 시계를 가진다. 신선 기한이 지나면 사용자 정보만 DB에서 다시 읽어 덮어쓰고 잔액은 두며 TTL은 연장하지 않는다. 배치 반영 중 표시, 반영 실패 재시도, 종료 시 큐 비우기, 캐시 반영 실패 복구 목록으로 DB 누락을 줄이고, 60초 재읽기 때 캐시·DB 잔액 어긋남을 (대기분 없음 + 두 관찰 동일) 규칙으로 감지해 로그만 남긴다. 모든 로직은 fork 파일에 두고 upstream 파일에는 `// fork: O12` 훅 한 줄만 둔다. 스위치 `QUOTA_CACHE_TTL<=0`이면 upstream 동작으로 돌아간다.

**Tech Stack:** Go 1.26(`GOWORK=off`), go-redis v8(`redis.NewFailoverClient`), Redis Lua, GORM(SQLite/MySQL/PostgreSQL), miniredis v2(테스트), go/ast(허용 목록 테스트).

**Spec:** `docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md` (§3 결정 1–8, §4.1–4.14, §5 쓰기 경로 표, §6 한계, §8 테스트 1–13). 재현 테스트 원본은 스크래치에 있던 `zz_quota_rehydrate_repro_test.go`/`zz_quota_negative_repro_test.go`이며 Task 2에서 회귀 테스트로 다시 쓴다.

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`(시작 HEAD `eb54e2a08` 이후). `/Users/molla/work/ai/new-api`(team 체크아웃)와 team 브랜치는 읽기 전용이며 이 계획은 team을 참고하지 않는다(스펙 결정 8).
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal/DecodeJson*`만 쓴다. 새 SQL은 없다(Redis와 기존 GORM 호출만).
- upstream 파일 수정은 훅 한 줄(또는 필드·변수 한 줄)로 제한하고 `// fork: O12` 주석을 단다. 로직은 fork 파일(`*_fork.go`)에 둔다. 모든 훅은 `docs/fork-overlays.md`의 새 절 "O12 잔액 캐시 상주·Sentinel"에 파일:줄로 기록한다. 훅이 upstream Lua 스크립트를 복사해 고친 경우 "upstream 스크립트 복사본, 병합 때 원본 변경을 반영" 메모를 단다.
- API 키 캐시(`token:*`), `userCacheTTLSeconds()`, 인증 fence(`userAuthFenceTTLSeconds`), 세션 캐시(`userSessionCacheDeadline`)는 바꾸지 않는다(스펙 결정 4, §4.5).
- 요청을 거부하는 새 오류 경로를 만들지 않는다(스펙 결정 3). 어긋난 잔액을 자동으로 고치지 않는다(결정 6).
- 테스트는 각 패키지의 기존 `TestMain`(SQLite in-memory)과 miniredis(`useUserCacheMiniRedis`, `model/user_cache_auth_version_test.go:17`)로 한다. 실제 Sentinel 테스트는 `REDIS_SENTINEL_TEST_ADDRS`가 없으면 skip한다.
- 커밋 트레일러: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- 한국어 문서는 `docs/translation-glossary.ko.md`의 용어를 따른다. 프런트 변경은 없다.

## Review Focus

1. **스위치를 끈 운영자(`QUOTA_CACHE_TTL=0`):** 차감·정산·캐시 쓰기가 upstream과 바이트 단위로 같은 Redis 동작(TTL = `SYNC_FREQUENCY`, 차감 때 연장 없음, `FreshUntil` 없음)이어야 한다. → Task 1·2·3의 "residency off" 테스트.
2. **인증 정보가 바뀌는 순간의 사용자:** 해시가 24시간 남아 있어도 신선 기한이 지난 인증 정보는 쓰이지 않고, fence가 걸려 있으면 지금처럼 거절돼야 한다. → Task 3 `TestStaleUserInfoRejectedWhileAuthFencePending`.
3. **재읽기 직후의 relay 요청:** 호출자가 받는 잔액이 DB 값이 아니라 캐시 값이어야 한다(아니면 배치 대기분만큼 초과 허용). → Task 3 `TestGetUserCacheReturnsCachedQuotaAfterInfoRefresh`.
4. **다른 노드가 반영 중인 사용자:** 어긋남 로그가 나면 안 된다. 같은 금액 요청을 반복하는 사용자도 마찬가지다. → Task 8 `TestUserQuotaDriftIgnoresOtherNodePendingWindow`.
5. **Redis가 잠깐 죽은 동안 요청한 사용자:** DB로 우회 차감된 뒤 되살아난 해시가 더 큰 잔액을 들고 있으면 안 된다. 복구 목록이 대기분이 없을 때 해시를 지워야 한다. → Task 7 `TestUserCacheRepairDeletesStaleHashAfterRedisError`.

---

### Task 1: 설정값 `QUOTA_CACHE_TTL`과 상주 스위치

**Files:**
- Create: `common/quota_cache_ttl_fork.go`, `model/user_quota_residency_fork.go`, `model/user_quota_residency_fork_test.go`
- Modify: `common/init.go:112`(한 줄 추가), `.env.example:48-49` 아래, `docker-compose.yml:51` 아래

**Interfaces:**
- Produces: `common.QuotaCacheTTL int`(환경 변수 원값, 기본 86400). `model.quotaCacheResidencyEnabled() bool`(= `common.QuotaCacheTTL > 0`). `model.quotaCacheTTLSeconds() int`(꺼져 있으면 `userCacheTTLSeconds()`, 켜져 있으면 `max(common.QuotaCacheTTL, userCacheTTLSeconds())`).

- [ ] **Step 1: 실패하는 테스트**

`model/user_quota_residency_fork_test.go`:

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func withQuotaCacheTTL(t *testing.T, ttl int) {
	t.Helper()
	old := common.QuotaCacheTTL
	common.QuotaCacheTTL = ttl
	t.Cleanup(func() { common.QuotaCacheTTL = old })
}

func TestQuotaCacheTTLSecondsFollowsSwitch(t *testing.T) {
	oldSync := common.SyncFrequency
	common.SyncFrequency = 60
	t.Cleanup(func() { common.SyncFrequency = oldSync })

	withQuotaCacheTTL(t, 0)
	require.False(t, quotaCacheResidencyEnabled())
	require.Equal(t, 60, quotaCacheTTLSeconds(), "off: same as user cache TTL")

	withQuotaCacheTTL(t, 86400)
	require.True(t, quotaCacheResidencyEnabled())
	require.Equal(t, 86400, quotaCacheTTLSeconds())

	withQuotaCacheTTL(t, 10)
	require.Equal(t, 60, quotaCacheTTLSeconds(), "never shorter than SYNC_FREQUENCY")
}
```

실행: `GOWORK=off go test ./model/ -run TestQuotaCacheTTLSecondsFollowsSwitch -count=1` → 컴파일 실패(`common.QuotaCacheTTL` 없음).

- [ ] **Step 2: 구현**

`common/quota_cache_ttl_fork.go`:

```go
package common

// fork: O12 — 사용자 잔액 캐시 상주 TTL(초). 0 이하면 상주를 끄고 upstream 동작
// (SYNC_FREQUENCY TTL, 차감 때 연장 없음)으로 돌아간다. 설계:
// docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md §4.8
var QuotaCacheTTL int
```

`common/init.go:112`의 `SyncFrequency = GetEnvOrDefault("SYNC_FREQUENCY", 60)` 바로 다음 줄에:

```go
	QuotaCacheTTL = GetEnvOrDefault("QUOTA_CACHE_TTL", 86400) // fork: O12
```

`model/user_quota_residency_fork.go`:

```go
package model

import "github.com/QuantumNous/new-api/common"

// fork: O12 — 사용자 잔액 캐시 상주. 잔액 TTL(Redis EXPIRE)과 정보 신선 기한
// (해시 필드 FreshUntil)은 서로 다른 시계다. 설계 §4.1.

func quotaCacheResidencyEnabled() bool {
	return common.QuotaCacheTTL > 0
}

// quotaCacheTTLSeconds is the Redis TTL of the user hash. It is never shorter
// than the user-info TTL so the auth fence math in userAuthFenceTTLSeconds
// keeps its "fence outlives any fresh info" property.
func quotaCacheTTLSeconds() int {
	if !quotaCacheResidencyEnabled() {
		return userCacheTTLSeconds()
	}
	return max(common.QuotaCacheTTL, userCacheTTLSeconds())
}
```

`.env.example` 48행 `# SYNC_FREQUENCY=60` 뒤에:

```
# 사용자 잔액 캐시 상주 TTL(초). 0 이하면 끔(SYNC_FREQUENCY 동작). 기본 86400
# QUOTA_CACHE_TTL=86400
```

`docker-compose.yml` 51행 뒤에 `#      - QUOTA_CACHE_TTL=86400  # user quota cache residency TTL (seconds); <=0 disables`.

실행: Step 1 PASS, `GOWORK=off go vet ./common/ ./model/`.

- [ ] **Step 3: 커밋**

```bash
git add common/quota_cache_ttl_fork.go common/init.go model/user_quota_residency_fork.go model/user_quota_residency_fork_test.go .env.example docker-compose.yml
git commit -m "feat(quota): add QUOTA_CACHE_TTL switch for resident user quota cache

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: 차감·정산 스크립트의 잔액 TTL 연장과 재현 회귀 테스트

**Files:**
- Create: `model/user_quota_scripts_fork.go`, `model/user_quota_scripts_fork_test.go`, `model/user_quota_residency_regression_test.go`
- Modify: `model/quota_reserve.go:82-83`(`cacheTryReserveUserQuota`의 Eval 두 줄 → fork 호출 한 줄), `:88-89`(`cacheApplyUserQuotaDelta` 같은 방식)

**Interfaces:**
- Consumes: Task 1 `quotaCacheResidencyEnabled`, `quotaCacheTTLSeconds`; upstream 상수 `userQuotaReserveScript`, `userQuotaDeltaScript`(`model/quota_reserve.go:19-38`), `quotaResultFromLua`.
- Produces: `evalUserQuotaReserve(userID int, amount int64) (int, error)`, `evalUserQuotaDelta(userID int, delta int64) (int, error)`. 켜져 있으면 성공(`HINCRBY` 실행) 경로에서만 `EXPIRE KEYS[1] ARGV[4]`를 실행하는 복사본 스크립트를, 꺼져 있으면 upstream 스크립트를 그대로 쓴다.

- [ ] **Step 1: 실패하는 테스트**

`model/user_quota_scripts_fork_test.go`:

```go
package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func userHashTTL(t *testing.T, userID int) time.Duration {
	t.Helper()
	ttl, err := common.RDB.TTL(t.Context(), getUserCacheKey(userID)).Result()
	require.NoError(t, err)
	return ttl
}

func TestUserQuotaReserveExtendsResidentTTLOnlyOnSuccess(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9201, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())

	result, err := cacheTryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id), "success extends to quota TTL")

	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())
	result, err = cacheTryReserveUserQuota(user.Id, 5000)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaInsufficient, result)
	require.Equal(t, 5*time.Second, userHashTTL(t, user.Id), "insufficient leaves TTL alone")

	result, err = cacheApplyUserQuotaDelta(user.Id, 50)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id), "delta extends too")
}

func TestUserQuotaReserveKeepsUpstreamTTLWhenResidencyOff(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 0)
	user := insertUserForPaymentGuardTest(t, 9202, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 5*time.Second).Err())

	result, err := cacheTryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, result)
	require.Equal(t, 5*time.Second, userHashTTL(t, user.Id), "off: no extension")
}
```

`model/user_quota_residency_regression_test.go`(스펙 §2.2 재현의 회귀화, §8-1):

```go
package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func enableBatchForResidencyTest(t *testing.T) {
	t.Helper()
	old := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() {
		batchUpdate()
		common.BatchUpdateEnabled = old
	})
}

// Before residency: the hash expired inside the batch window, the next reserve
// rehydrated the stale DB balance, and after the flush DB=800 while cache=900.
func TestResidentQuotaCacheSurvivesBatchWindow(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)

	user := insertUserForPaymentGuardTest(t, 9203, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	ok, err := TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, ok)

	server.FastForward(10 * time.Second) // longer than SYNC_FREQUENCY, shorter than quota TTL
	require.True(t, server.Exists(getUserCacheKey(user.Id)), "hash must survive")

	ok, err = TryReserveUserQuota(user.Id, 100)
	require.NoError(t, err)
	require.True(t, ok)

	batchUpdate()
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	cachedQuota, err := common.RDB.HGet(t.Context(), getUserCacheKey(user.Id), "Quota").Int()
	require.NoError(t, err)
	require.Equal(t, 800, dbUser.Quota)
	require.Equal(t, dbUser.Quota, cachedQuota)

	ok, err = TryReserveUserQuota(user.Id, 900)
	require.NoError(t, err)
	require.False(t, ok, "cannot spend more than the real balance")
}

func TestIdleUserHashExpiresAfterQuotaTTL(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 30)
	user := insertUserForPaymentGuardTest(t, 9204, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	server.FastForward(31 * time.Second)
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
	cached, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, cached.Quota, "rehydrated from DB after idle expiry")
}
```

주의: `FreshUntil`(Task 3)은 실제 시계(`common.GetTimestamp()`)를 쓰고 miniredis의 `FastForward`는 Redis 쪽 TTL만 움직인다. 그래서 TTL 만료는 `FastForward`로, 신선 기한 경과는 해시 필드 `FreshUntil`을 과거로 덮어써서 만든다.

실행: `GOWORK=off go test ./model/ -run 'TestUserQuotaReserve|TestResidentQuotaCache|TestIdleUserHash' -count=1 -v` → 첫 테스트와 회귀 테스트 FAIL(TTL이 연장되지 않음, 해시가 사라짐). `TestIdleUserHashExpiresAfterQuotaTTL`은 Task 3 전까지 TTL이 2초라 통과할 수 있다(회귀 고정용).

- [ ] **Step 2: 구현**

`model/user_quota_scripts_fork.go`:

```go
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
```

`model/quota_reserve.go:82-83`(`cacheTryReserveUserQuota` 본문의 `result, err := common.RDB.Eval(...).Int()` 두 줄)을 `result, err := evalUserQuotaReserve(userID, amount) // fork: O12` 한 줄로, `:88-89`를 `result, err := evalUserQuotaDelta(userID, delta) // fork: O12`로 바꾼다. 훅 뒤 `return quotaResultFromLua(result, err)`는 그대로다. gofmt 후 두 함수의 줄 번호를 overlay 표에 적는다.

실행: Step 1 PASS. `GOWORK=off go test ./model/ -count=1`(기존 `user_cache_auth_version_test.go`, `user_session_test.go` 통과 확인).

- [ ] **Step 3: 커밋**

```bash
git add model/user_quota_scripts_fork.go model/user_quota_scripts_fork_test.go model/user_quota_residency_regression_test.go model/quota_reserve.go
git commit -m "fix(quota): keep the user quota hash alive while it is being spent

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: 정보 신선 기한(`FreshUntil`)과 캐시 쓰기·읽기 규칙

**Files:**
- Create: `model/user_cache_fresh_fork.go`, `model/user_cache_fresh_fork_test.go`
- Modify: `model/user_cache.go:25`(`UserBase`에 필드 한 줄), `:133-136`(`cacheGetUserBase`의 floor 검사 뒤 한 줄), `:112`(`GetUserCache`의 `return user.ToBaseUser(), nil` 한 줄), `model/user_auth_cache.go:45-46`(`writeUserCache` 첫 줄에 위임 한 줄)

**Interfaces:**
- Consumes: Task 1·2. upstream `writeUserCache` 스크립트(`model/user_auth_cache.go:57-82`), `cacheGetUserBase`, `getUserAuthVersionFloor`, `common.RedisHGetObj`(필드 이름으로 해시 필드를 읽는다 → `FreshUntil int64` 필드가 해시의 `FreshUntil`을 받는다).
- Produces: `UserBase.FreshUntil int64 \`json:"-"\``. `userInfoStale(u *UserBase) bool`(켜져 있고 `FreshUntil <= now`). `errUserInfoStale`. `writeUserCacheResident(user *UserBase, includeQuota bool) error`(복사본 스크립트: `FreshUntil = now + SYNC_FREQUENCY`를 HSET에 추가, `EXPIRE`는 `TTL < 0`일 때만 잔액 TTL로). `userBaseAfterPopulate(user *User) (*UserBase, error)`(켜져 있으면 해시를 다시 읽어 캐시 잔액을 돌려주고, 실패하면 DB 값).

- [ ] **Step 1: 실패하는 테스트**

`model/user_cache_fresh_fork_test.go`:

```go
package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func setHashField(t *testing.T, userID int, field string, value any) {
	t.Helper()
	require.NoError(t, common.RDB.HSet(t.Context(), getUserCacheKey(userID), field, value).Err())
}

func TestFreshUntilRefreshKeepsQuotaAndTTL(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t) // SyncFrequency = 2
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9301, 1000)
	first, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 3600*time.Second, userHashTTL(t, user.Id))
	require.Greater(t, first.FreshUntil, common.GetTimestamp())

	// cache spends 100 (DB still 1000), profile changes in DB
	_, err = cacheApplyUserQuotaDelta(user.Id, -100)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("email", "new@example.com").Error)
	require.NoError(t, common.RDB.Expire(t.Context(), getUserCacheKey(user.Id), 1000*time.Second).Err())

	setHashField(t, user.Id, "FreshUntil", common.GetTimestamp()-1) // fresh-until passes, TTL does not
	second, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", second.Email, "profile re-read from DB")
	require.Equal(t, 900, second.Quota, "cached balance kept and returned")
	require.Equal(t, 1000*time.Second, userHashTTL(t, user.Id), "refresh does not extend TTL")
	require.Greater(t, second.FreshUntil, common.GetTimestamp(), "fresh again")
}

func TestGetUserCacheReturnsCachedQuotaAfterInfoRefresh(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9302, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = cacheApplyUserQuotaDelta(user.Id, -250)
	require.NoError(t, err)
	setHashField(t, user.Id, "FreshUntil", 1) // force stale
	got, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 750, got.Quota)
}

func TestStaleUserInfoRejectedWhileAuthFencePending(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9303, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	setHashField(t, user.Id, "FreshUntil", 1)
	var fresh User
	require.NoError(t, DB.First(&fresh, user.Id).Error)
	require.NoError(t, SetUserAuthVersionFence(user.Id, fresh.AuthVersion+1))
	_, err = GetUserCache(user.Id)
	require.ErrorIs(t, err, ErrUserAuthCachePending, "stale info is not served while a newer version is pending")
}

func TestWriteUserCacheMatchesUpstreamWhenResidencyOff(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 0)
	user := insertUserForPaymentGuardTest(t, 9304, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 2*time.Second, userHashTTL(t, user.Id))
	exists, err := common.RDB.HExists(t.Context(), getUserCacheKey(user.Id), "FreshUntil").Result()
	require.NoError(t, err)
	require.False(t, exists)
}
```

실행: `GOWORK=off go test ./model/ -run 'TestFreshUntil|TestGetUserCacheReturnsCachedQuota|TestStaleUserInfo|TestWriteUserCacheMatchesUpstream' -count=1 -v` → 컴파일 실패(`FreshUntil` 없음).

- [ ] **Step 2: 구현**

`model/user_cache.go:25` `CacheSchema int \`json:"-"\`` 다음 줄에 `FreshUntil  int64  \`json:"-"\` // fork: O12`를 추가한다(gofmt 정렬).

`model/user_cache_fresh_fork.go`:

```go
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
```

훅:
- `model/user_auth_cache.go:45-46` `writeUserCache` 첫 줄(`if user == nil || user.Id <= 0 || !common.RedisEnabled { return nil }` 다음)에 `if quotaCacheResidencyEnabled() { return writeUserCacheResident(user, includeQuota) } // fork: O12`.
- `model/user_cache.go:133-136` `cacheGetUserBase`에서 `if floor > userCache.AuthVersion { return nil, ErrUserAuthCachePending }` 다음에 `if userInfoStale(&userCache) { return nil, errUserInfoStale } // fork: O12`.
- `model/user_cache.go:112` `GetUserCache` 끝의 `return user.ToBaseUser(), nil`을 `return userBaseAfterPopulate(user) // fork: O12`로.

`GetUserCache`는 `cacheGetUserBase` 오류를 모두 DB 경로로 보내므로(`:90-93`) stale은 miss처럼 처리되고, DB를 읽은 뒤 `populateUserCache` → `writeUserCache` → 상주 스크립트가 정보만 덮어쓴다. fence가 걸려 있으면 `populateUserCache`가 `ErrUserAuthCachePending`을 돌려주고 `GetUserCache`가 그대로 반환한다(`:104-107`).

실행: Step 1 PASS, `GOWORK=off go test ./model/ ./service/ ./middleware/ ./controller/ -count=1`(fence·세션 테스트 포함).

- [ ] **Step 3: 커밋**

```bash
git add model/user_cache_fresh_fork.go model/user_cache_fresh_fork_test.go model/user_cache.go model/user_auth_cache.go
git commit -m "feat(quota): refresh user info on a fresh-until clock while the balance stays cached

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: 추천 적립 이전의 캐시 동기화

**Files:**
- Create: `model/aff_transfer_cache_fork.go`, `model/aff_transfer_cache_fork_test.go`
- Modify: `model/user.go:632`(`TransferAffQuotaToQuota`의 `return tx.Commit().Error` 한 줄)

**Interfaces:**
- Consumes: `cacheIncrUserQuota(userId int, delta int64) error`(`model/user_cache.go:143`).
- Produces: `commitAffTransfer(tx *gorm.DB, userId int, quota int) error`(커밋 성공 뒤 `cacheIncrUserQuota(userId, int64(quota))`; 캐시 오류는 로그만, Task 7이 복구 목록에 넣는다).

- [ ] **Step 1: 실패하는 테스트**

`model/aff_transfer_cache_fork_test.go`:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransferAffQuotaToQuotaSyncsCache(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	user := insertUserForPaymentGuardTest(t, 9401, 1000)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("aff_quota", 500000).Error)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	var fresh User
	require.NoError(t, DB.First(&fresh, user.Id).Error)
	require.NoError(t, fresh.TransferAffQuotaToQuota(500000))

	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.Equal(t, 501000, dbUser.Quota)
	require.Equal(t, dbUser.Quota, cached.Quota, "cache follows the transfer")
}
```

실행: `GOWORK=off go test ./model/ -run TestTransferAffQuotaToQuotaSyncsCache -count=1` → FAIL(캐시 1000).

- [ ] **Step 2: 구현**

`model/aff_transfer_cache_fork.go`:

```go
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
```

`model/user.go:632` `return tx.Commit().Error` → `return commitAffTransfer(tx, user.Id, quota) // fork: O12`.

실행: Step 1 PASS, `GOWORK=off go test ./model/ -run 'Aff|Checkin' -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add model/aff_transfer_cache_fork.go model/aff_transfer_cache_fork_test.go model/user.go
git commit -m "fix(quota): sync the user quota cache after an invitation quota transfer

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: 배치 반영 중 표시와 대기분 조회

**Files:**
- Create: `model/batch_inflight_fork.go`, `model/batch_inflight_fork_test.go`
- Modify: `model/utils.go:121-123`(`batchUpdate`의 `for key := range userIDs` 루프 앞에 한 줄, 루프 본문의 `updateUserQuotaUsedQuotaAndRequestCount(...)` 호출 한 줄 교체)

**Interfaces:**
- Consumes: `batchUpdateStores`, `batchUpdateLocks`(`model/utils.go:24-25`), `BatchUpdateTypeUserQuota`.
- Produces: `markUserQuotaInFlight(ids map[int]struct{})`, `clearUserQuotaInFlight(id int)`, `userQuotaBatchPending(id int) bool`(큐 대기 또는 반영 중), `flushUserQuotaRow(id, quota, usedQuota, requestCount int)`(이 Task에서는 upstream `updateUserQuotaUsedQuotaAndRequestCount`를 부른 뒤 in-flight를 지운다; Task 6이 재시도를 넣는다).

- [ ] **Step 1: 실패하는 테스트**

`model/batch_inflight_fork_test.go`:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserQuotaBatchPendingCoversQueueAndInFlight(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9501, 1000)

	require.False(t, userQuotaBatchPending(user.Id))
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	require.True(t, userQuotaBatchPending(user.Id), "queued")

	// Simulate the flush window: stores are swapped out, rows not yet written.
	markUserQuotaInFlight(map[int]struct{}{user.Id: {}})
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	batchUpdateStores[BatchUpdateTypeUserQuota] = make(map[int]int)
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	require.True(t, userQuotaBatchPending(user.Id), "in flight")

	clearUserQuotaInFlight(user.Id)
	require.False(t, userQuotaBatchPending(user.Id))
}

func TestBatchUpdateClearsInFlightPerRow(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	a := insertUserForPaymentGuardTest(t, 9502, 1000)
	b := insertUserForPaymentGuardTest(t, 9503, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, a.Id, -100)
	addNewRecord(BatchUpdateTypeUserQuota, b.Id, -200)
	batchUpdate()
	require.False(t, userQuotaBatchPending(a.Id))
	require.False(t, userQuotaBatchPending(b.Id))
	var dbA, dbB User
	require.NoError(t, DB.First(&dbA, a.Id).Error)
	require.NoError(t, DB.First(&dbB, b.Id).Error)
	require.Equal(t, 900, dbA.Quota)
	require.Equal(t, 800, dbB.Quota)
}
```

실행: `GOWORK=off go test ./model/ -run 'TestUserQuotaBatchPending|TestBatchUpdateClearsInFlight' -count=1` → 컴파일 실패.

- [ ] **Step 2: 구현**

`model/batch_inflight_fork.go`:

```go
package model

// fork: O12 — 배치가 큐를 꺼내 DB에 쓰는 동안(운영 기준 3~5초) 그 사용자는
// 여전히 "DB에 반영되지 않은 차감"을 가진다. 행을 쓴 직후 한 사용자씩 푼다.
// 설계 §4.11. userQuotaInFlight는 batchUpdateLocks[BatchUpdateTypeUserQuota]로 보호한다.

var userQuotaInFlight = map[int]struct{}{}

func markUserQuotaInFlight(ids map[int]struct{}) {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	for id := range ids {
		userQuotaInFlight[id] = struct{}{}
	}
}

func clearUserQuotaInFlight(id int) {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	delete(userQuotaInFlight, id)
}

// userQuotaBatchPending reports whether this process still owes the database a
// quota delta for the user: queued for the next batch, or being written now.
func userQuotaBatchPending(id int) bool {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	_, queued := batchUpdateStores[BatchUpdateTypeUserQuota][id]
	_, inFlight := userQuotaInFlight[id]
	return queued || inFlight
}

func flushUserQuotaRow(id int, quota int, usedQuota int, requestCount int) {
	defer clearUserQuotaInFlight(id)
	updateUserQuotaUsedQuotaAndRequestCount(id, quota, usedQuota, requestCount)
}
```

`model/utils.go` `batchUpdate`에서 `for key := range userIDs {` 바로 앞에 `markUserQuotaInFlight(userIDs) // fork: O12`를, 루프 본문 `updateUserQuotaUsedQuotaAndRequestCount(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key])`를 `flushUserQuotaRow(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key]) // fork: O12`로 바꾼다. `userIDs`는 `map[int]struct{}`다(`:112`).

실행: Step 1 PASS, `GOWORK=off go test ./model/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add model/batch_inflight_fork.go model/batch_inflight_fork_test.go model/utils.go
git commit -m "feat(quota): track user quota batches while their rows are being written

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: 배치 반영 실패 재시도와 종료 시 큐 비우기

**Files:**
- Modify: `model/batch_inflight_fork.go`(`flushUserQuotaRow` 본문), `model/batch_inflight_fork_test.go`, `main.go:246-248`(`srv.Shutdown` 오류 처리 블록 뒤 한 줄)
- Create: `model/batch_shutdown_fork.go`

**Interfaces:**
- Consumes: Task 5. `common.BatchUpdateEnabled`, `batchUpdate()`.
- Produces: `flushUserQuotaRow`가 직접 `UPDATE users SET quota=quota+?, used_quota=used_quota+?, request_count=request_count+?`를 실행하고(upstream `updateUserQuotaUsedQuotaAndRequestCount`의 복사본, 오류를 돌려받기 위해), 실패하면 세 값을 `addNewRecord`로 다시 큐에 넣는다. `model.FlushBatchUpdates()`(배치가 켜져 있으면 `batchUpdate()` 한 번).

- [ ] **Step 1: 실패하는 테스트**

`model/batch_inflight_fork_test.go`에 추가:

```go
func TestFlushUserQuotaRowRequeuesOnDBError(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9601, 1000)
	markUserQuotaInFlight(map[int]struct{}{user.Id: {}})

	// Make the write fail: drop the table behind GORM for this call only.
	require.NoError(t, DB.Exec("ALTER TABLE users RENAME TO users_hidden").Error)
	flushUserQuotaRow(user.Id, -100, 100, 1)
	require.NoError(t, DB.Exec("ALTER TABLE users_hidden RENAME TO users").Error)

	require.True(t, userQuotaBatchPending(user.Id), "requeued after failure")
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	require.Equal(t, -100, batchUpdateStores[BatchUpdateTypeUserQuota][user.Id])
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	require.Equal(t, 100, batchUpdateStores[BatchUpdateTypeUsedQuota][user.Id])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()

	batchUpdate()
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	require.Equal(t, 900, dbUser.Quota, "retry landed")
}

func TestFlushBatchUpdatesWritesQueuedDeltas(t *testing.T) {
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9602, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -300)
	FlushBatchUpdates()
	var dbUser User
	require.NoError(t, DB.First(&dbUser, user.Id).Error)
	require.Equal(t, 700, dbUser.Quota)
}
```

SQLite의 `ALTER TABLE ... RENAME`이 TestMain의 in-memory DB에서 동작하는지 확인하고, 안 되면 `DB.Callback().Update().Before("gorm:update").Register("fork_test_fail", ...)`로 한 번 실패시키는 방식으로 바꾼다(테스트 종료 때 `Remove`).

실행: → FAIL(`FlushBatchUpdates` 없음, 재큐 없음).

- [ ] **Step 2: 구현**

`flushUserQuotaRow`를 다음으로 바꾼다:

```go
// flushUserQuotaRow is upstream updateUserQuotaUsedQuotaAndRequestCount with
// the error returned: a failed write is re-queued instead of dropped (§4.12-2).
func flushUserQuotaRow(id int, quota int, usedQuota int, requestCount int) {
	defer clearUserQuotaInFlight(id)
	if quota == 0 && usedQuota == 0 && requestCount == 0 {
		return
	}
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]any{
			"quota":         gorm.Expr("quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", usedQuota),
			"request_count": gorm.Expr("request_count + ?", requestCount),
		},
	).Error
	if err == nil {
		return
	}
	common.SysError(common.LogText("failed to batch update user %d quota, requeued: %s", id, err.Error()))
	if quota != 0 {
		addNewRecord(BatchUpdateTypeUserQuota, id, quota)
	}
	if usedQuota != 0 {
		addNewRecord(BatchUpdateTypeUsedQuota, id, usedQuota)
	}
	if requestCount != 0 {
		addNewRecord(BatchUpdateTypeRequestCount, id, requestCount)
	}
}
```

`model/batch_shutdown_fork.go`:

```go
package model

import "github.com/QuantumNous/new-api/common"

// fork: O12 — 정상 종료 때 배치 큐를 DB에 쓴다. upstream은 큐를 버려 재배포마다
// 최대 BATCH_UPDATE_INTERVAL 분량의 차감이 DB에서 사라졌다(설계 §4.12-1).
func FlushBatchUpdates() {
	if !common.BatchUpdateEnabled {
		return
	}
	common.SysLog(common.LogText("flushing batch updates before shutdown"))
	batchUpdate()
}
```

`main.go:246-248`의 `if err := srv.Shutdown(ctx); err != nil { ... }` 블록 바로 뒤에 `model.FlushBatchUpdates() // fork: O12`.

실행: Step 1 PASS, `GOWORK=off go build ./... && GOWORK=off go test ./model/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add model/batch_inflight_fork.go model/batch_inflight_fork_test.go model/batch_shutdown_fork.go main.go
git commit -m "fix(quota): retry failed batch rows and flush the queue on shutdown

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: 캐시 반영 실패 복구 목록

**Files:**
- Create: `model/user_cache_repair_fork.go`, `model/user_cache_repair_fork_test.go`
- Modify: `model/user_cache.go:148`(`cacheIncrUserQuota`의 `return err` 한 줄), `model/quota_reserve.go:181`(`TryReserveUserQuota`의 DB 우회 분기 첫 줄에 한 줄), `:190`(보상 실패 `SysError` 줄 뒤 한 줄), `main.go:171`(배치 블록 뒤 한 줄)

**Interfaces:**
- Consumes: Task 5 `userQuotaBatchPending`, `invalidateUserCache`(`model/user_cache.go:63`), `common.BatchUpdateInterval`.
- Produces: `noteUserCacheSyncFailure(userID int, err error) error`(err가 nil이 아니고 Redis가 켜져 있으면 목록에 넣고 err를 그대로 돌려준다), `repairUserCaches()`(목록의 사용자 중 대기분이 없는 사용자의 해시를 지우고 목록에서 뺀다), `model.StartUserCacheSyncRepair()`(`max(BatchUpdateInterval,1)`초마다 `repairUserCaches`; 배치 모드와 무관하게 돈다).

- [ ] **Step 1: 실패하는 테스트**

`model/user_cache_repair_fork_test.go`:

```go
package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserCacheRepairDeletesStaleHashAfterRedisError(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9701, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)

	// Redis "down": the reserve falls back to the database and the hash keeps 1000.
	server.SetError("simulated outage")
	ok, err := TryReserveUserQuota(user.Id, 100)
	server.SetError("")
	require.NoError(t, err)
	require.True(t, ok)
	cached, err := cacheGetUserBase(user.Id)
	require.NoError(t, err)
	require.Equal(t, 1000, cached.Quota, "stale: DB is 900")

	repairUserCaches()
	require.False(t, server.Exists(getUserCacheKey(user.Id)), "stale hash removed")
	again, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, 900, again.Quota)
}

func TestUserCacheRepairWaitsForPendingBatch(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	withQuotaCacheTTL(t, 3600)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9702, 1000)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_ = noteUserCacheSyncFailure(user.Id, errors.New("boom"))
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)

	repairUserCaches()
	require.True(t, server.Exists(getUserCacheKey(user.Id)), "kept while a delta is pending")
	batchUpdate()
	repairUserCaches()
	require.False(t, server.Exists(getUserCacheKey(user.Id)))
}

func TestNoteUserCacheSyncFailureIgnoresNil(t *testing.T) {
	useUserCacheMiniRedis(t)
	require.NoError(t, noteUserCacheSyncFailure(1, nil))
	userCacheRepairMu.Lock()
	_, listed := userCacheRepairIDs[1]
	userCacheRepairMu.Unlock()
	require.False(t, listed)
}
```

`TryReserveUserQuota`의 우회 분기에서 `noteUserCacheSyncFailure`가 불리는지는 첫 테스트가 검증한다(훅이 없으면 해시가 남아 FAIL). 실행: → 컴파일 실패.

- [ ] **Step 2: 구현**

`model/user_cache_repair_fork.go`:

```go
package model

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

// fork: O12 — 커밋 뒤 캐시 반영에 실패했거나 Redis 오류로 DB에서만 차감한 사용자는
// 해시가 실제보다 큰 잔액을 들고 있을 수 있다. 대기분이 없을 때 해시를 지워
// 다음 요청이 DB에서 새로 읽게 한다. 값을 고치지는 않는다(설계 §4.12-3).

var (
	userCacheRepairMu  sync.Mutex
	userCacheRepairIDs = map[int]struct{}{}
)

func noteUserCacheSyncFailure(userID int, err error) error {
	if err == nil || !common.RedisEnabled || userID <= 0 {
		return err
	}
	userCacheRepairMu.Lock()
	userCacheRepairIDs[userID] = struct{}{}
	userCacheRepairMu.Unlock()
	return err
}

func repairUserCaches() {
	userCacheRepairMu.Lock()
	ids := make([]int, 0, len(userCacheRepairIDs))
	for id := range userCacheRepairIDs {
		ids = append(ids, id)
	}
	userCacheRepairMu.Unlock()
	for _, id := range ids {
		if userQuotaBatchPending(id) {
			continue
		}
		if err := invalidateUserCache(id); err != nil {
			common.SysLog(common.LogText("user cache repair for %d deferred: %s", id, err.Error()))
			continue
		}
		userCacheRepairMu.Lock()
		delete(userCacheRepairIDs, id)
		userCacheRepairMu.Unlock()
	}
}

func StartUserCacheSyncRepair() {
	if !common.RedisEnabled {
		return
	}
	interval := time.Duration(max(common.BatchUpdateInterval, 1)) * time.Second
	gopool.Go(func() {
		for {
			time.Sleep(interval)
			repairUserCaches()
		}
	})
}
```

훅:
- `model/user_cache.go:148` `cacheIncrUserQuota`의 `return err` → `return noteUserCacheSyncFailure(userId, err) // fork: O12`.
- `model/quota_reserve.go:181` `if err != nil || result == cacheQuotaMiss {` 바로 다음 줄에 `noteUserCacheSyncFailure(id, err) // fork: O12`(err가 nil이면 no-op).
- `model/quota_reserve.go:190`(`failed to compensate reserved user quota` SysError 줄 뒤)에 `noteUserCacheSyncFailure(id, compensateErr) // fork: O12`.
- `main.go:171`(배치 블록 `}` 뒤)에 `model.StartUserCacheSyncRepair() // fork: O12`. Redis는 `InitResources()`(`main.go:299`, `:356`에서 `common.InitRedisClient()`)에서 초기화되고 `main()`은 그 뒤에 이 지점에 이르므로 `common.RedisEnabled`가 이미 정해져 있다. 확인한 뒤 보고서에 적는다.

실행: Step 1 PASS, `GOWORK=off go build ./... && GOWORK=off go test ./model/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add model/user_cache_repair_fork.go model/user_cache_repair_fork_test.go model/user_cache.go model/quota_reserve.go main.go
git commit -m "fix(quota): repair user hashes whose cache sync failed once no batch is pending

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: 잔액 어긋남 감지 로그

**Files:**
- Create: `model/user_quota_drift_fork.go`, `model/user_quota_drift_fork_test.go`
- Modify: `model/user_cache_fresh_fork.go`(`userBaseAfterPopulate`에서 관찰 호출 한 줄; fork 파일)

**Interfaces:**
- Consumes: Task 5 `userQuotaBatchPending`; Task 3 `userBaseAfterPopulate(user *User)`(DB 스냅샷 `user.Quota`와 해시 `cached.Quota`를 둘 다 가진 지점).
- Produces: `observeUserQuotaDrift(userID int, cacheQuota int, dbQuota int)`; `var userQuotaDriftReporter = func(userID, cacheQuota, dbQuota, diff int)`(기본: `common.SysError` 경고 로그; 테스트가 교체); 사용자별 기억 `(cacheQuota, diff)`는 `sync.Map` 또는 뮤텍스 map.

- [ ] **Step 1: 실패하는 테스트**

`model/user_quota_drift_fork_test.go`:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type driftCall struct{ userID, cacheQuota, dbQuota, diff int }

func captureDrift(t *testing.T) *[]driftCall {
	t.Helper()
	old := userQuotaDriftReporter
	var calls []driftCall
	userQuotaDriftReporter = func(userID, cacheQuota, dbQuota, diff int) {
		calls = append(calls, driftCall{userID, cacheQuota, dbQuota, diff})
	}
	t.Cleanup(func() { userQuotaDriftReporter = old; resetUserQuotaDriftMemory() })
	return &calls
}

func TestUserQuotaDriftLogsOnlyWhenStableAcrossTwoObservations(t *testing.T) {
	calls := captureDrift(t)
	enableBatchForResidencyTest(t)
	observeUserQuotaDrift(1, 900, 1000) // first sight: remember only
	require.Empty(t, *calls)
	observeUserQuotaDrift(1, 900, 1000) // same cache, same diff → drift
	require.Len(t, *calls, 1)
	require.Equal(t, driftCall{1, 900, 1000, -100}, (*calls)[0])

	observeUserQuotaDrift(2, 900, 1000)
	observeUserQuotaDrift(2, 800, 900) // cache moved → user is active, no log
	require.Len(t, *calls, 1)

	observeUserQuotaDrift(3, 900, 1000)
	observeUserQuotaDrift(3, 900, 900) // diff gone → cleared
	observeUserQuotaDrift(3, 900, 900)
	require.Len(t, *calls, 1)
}

func TestUserQuotaDriftIgnoresOtherNodePendingWindow(t *testing.T) {
	calls := captureDrift(t)
	enableBatchForResidencyTest(t)
	// Another node holds -100 at the first observation; it flushes before the second.
	observeUserQuotaDrift(4, 900, 1000)
	observeUserQuotaDrift(4, 900, 900)
	require.Empty(t, *calls)
}

func TestUserQuotaDriftSkipsLocalPendingUsers(t *testing.T) {
	calls := captureDrift(t)
	truncateTables(t)
	enableBatchForResidencyTest(t)
	user := insertUserForPaymentGuardTest(t, 9801, 1000)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	observeUserQuotaDrift(user.Id, 900, 1000)
	observeUserQuotaDrift(user.Id, 900, 1000)
	require.Empty(t, *calls, "local pending → never compared")
}
```

실행: → 컴파일 실패.

- [ ] **Step 2: 구현**

`model/user_quota_drift_fork.go`:

```go
package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// fork: O12 — 60초 재읽기 때 캐시·DB 잔액을 비교한다. 이 서버에 대기분이 있으면
// 건너뛰고, 캐시 잔액과 차이가 두 관찰에서 그대로이고 0이 아닐 때만 로그를 남긴다.
// 다른 노드의 대기분은 두 관찰 사이에 반영되어 차이를 바꾸므로 걸리지 않는다
// (설계 §4.7). 값을 고치지는 않는다.

type userQuotaDriftObservation struct {
	cacheQuota int
	diff       int
}

var (
	userQuotaDriftMu     sync.Mutex
	userQuotaDriftMemory = map[int]userQuotaDriftObservation{}
)

var userQuotaDriftReporter = func(userID, cacheQuota, dbQuota, diff int) {
	common.SysError(common.LogText("user quota cache drift: user=%d cache=%d db=%d diff=%d", userID, cacheQuota, dbQuota, diff))
}

func resetUserQuotaDriftMemory() {
	userQuotaDriftMu.Lock()
	userQuotaDriftMemory = map[int]userQuotaDriftObservation{}
	userQuotaDriftMu.Unlock()
}

func observeUserQuotaDrift(userID int, cacheQuota int, dbQuota int) {
	if userQuotaBatchPending(userID) {
		return
	}
	diff := cacheQuota - dbQuota
	userQuotaDriftMu.Lock()
	defer userQuotaDriftMu.Unlock()
	if diff == 0 {
		delete(userQuotaDriftMemory, userID)
		return
	}
	prev, seen := userQuotaDriftMemory[userID]
	userQuotaDriftMemory[userID] = userQuotaDriftObservation{cacheQuota: cacheQuota, diff: diff}
	if seen && prev.cacheQuota == cacheQuota && prev.diff == diff {
		userQuotaDriftReporter(userID, cacheQuota, dbQuota, diff)
	}
}
```

`model/user_cache_fresh_fork.go`의 `userBaseAfterPopulate`에서 `cached`를 얻은 뒤 `return cached, nil` 앞에 `observeUserQuotaDrift(user.Id, cached.Quota, user.Quota)`를 넣는다. 첫 채움(해시가 없던 경우)은 `cached.Quota == user.Quota`라 기억이 지워질 뿐이다.

실행: Step 1 PASS, `GOWORK=off go test ./model/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add model/user_quota_drift_fork.go model/user_quota_drift_fork_test.go model/user_cache_fresh_fork.go
git commit -m "feat(quota): log user quota cache drift that no pending batch explains

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: `users.quota` 쓰기 경로 허용 목록 테스트

**Files:**
- Create: `model/user_quota_write_sites_fork_test.go`

**Interfaces:**
- Consumes: Go 표준 `go/parser`, `go/ast`. 스펙 §5 표.
- Produces: 테스트 `TestUserQuotaWriteSitesAreAllowlisted`. 검출 규칙: `model/`, `service/`, `controller/`의 비테스트 `.go`에서 (a) `.Update("quota", …)`/`.UpdateColumn("quota", …)`, (b) `.Updates(map…)`/`.UpdateColumns(map…)`의 복합 리터럴 키에 `"quota"`가 있는 호출, (c) `.Save(x)`·`.Updates(x)`에서 x가 식별자이고 이름에 `user`가 들어가는 호출(구조체 저장; 대소문자 무시). 각 검출을 `파일:함수`로 만들어 허용 목록과 비교한다.

- [ ] **Step 1: 실패하는 테스트**

```go
package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fork: O12 — 상주 캐시에서는 캐시를 맞추지 않는 DB 잔액 쓰기가 60초 뒤에도
// 저절로 맞춰지지 않는다. upstream 병합이 새 쓰기 경로를 들여오면 여기서 드러난다.
// 목록에 넣을 때 그 경로가 캐시에 차이를 더하는지(또는 해시를 지우는지) 확인하고
// 설계 문서 §5 표를 갱신한다. 값은 "상대경로:함수명".
var userQuotaWriteAllowlist = map[string]string{
	// users.quota — cache is kept in step
	"model/quota_reserve.go:persistUserQuotaDelta":          "cache decremented first (TryReserveUserQuota)",
	"model/quota_reserve.go:reserveUserQuotaDB":             "Redis-unavailable fallback; repair list",
	"model/user.go:increaseUserQuota":                       "IncreaseUserQuota calls cacheIncrUserQuota",
	"model/user.go:decreaseUserQuota":                       "DecreaseUserQuota calls cacheDecrUserQuota",
	"model/user.go:updateUserQuotaUsedQuotaAndRequestCount": "batch flush (upstream; unused after Task 6 hook)",
	"model/user.go:TransferAffQuotaToQuota":                 "commitAffTransfer syncs (Task 4)",
	"model/user.go:UpdateWithTx":                            "Updates(newUser) with quota omitted",
	"model/batch_inflight_fork.go:flushUserQuotaRow":        "batch flush (fork)",
	"model/user_quota_adjustment.go:AdjustUserQuota":        "committed delta synced after commit",
	"model/topup.go:creditTopUpQuota":                       "callers run syncCreditUserQuotaCache",
	"model/subscription.go:PurchaseSubscriptionWithBalance": "cacheDecrUserQuota after commit",
	// other tables that happen to have a quota column
	"model/organization.go:IncreaseOrganizationQuota":       "organizations table",
	"model/organization.go:DecreaseOrganizationQuota":       "organizations table",
	"model/organization.go:TryReserveOrganizationQuota":     "organizations table",
	"model/organization.go:IncreaseOrganizationMemberQuota": "organization member limit",
	"model/organization.go:SetOrganizationMemberQuota":      "users.quota set by import; caller invalidates the hash",
	"model/topup_target.go:creditOrganizationQuota":         "organizations table",
	"model/task.go:UpdateQuota":                             "tasks table",
	"model/usedata.go:increaseQuotaData":                    "quota_data table",
}

func TestUserQuotaWriteSitesAreAllowlisted(t *testing.T) {
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	var found []string
	for _, dir := range []string{"model", "service", "controller"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			found = append(found, quotaWriteSitesInFile(t, root, filepath.Join(dir, name))...)
		}
	}
	sort.Strings(found)
	var missing []string
	for _, site := range found {
		if _, ok := userQuotaWriteAllowlist[site]; !ok {
			missing = append(missing, site)
		}
	}
	require.Empty(t, missing, "new users.quota write sites: check cache sync and add to the allowlist (spec §5)")
}

func quotaWriteSitesInFile(t *testing.T, root, rel string) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
	require.NoError(t, err)
	var sites []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			if isQuotaWriteCall(sel.Sel.Name, call.Args) {
				sites = append(sites, rel+":"+fn.Name.Name)
				return false
			}
			return true
		})
	}
	return sites
}

func isQuotaWriteCall(method string, args []ast.Expr) bool {
	switch method {
	case "Update", "UpdateColumn":
		lit, ok := args[0].(*ast.BasicLit)
		return ok && lit.Value == `"quota"`
	case "Updates", "UpdateColumns":
		if comp, ok := args[0].(*ast.CompositeLit); ok {
			for _, elt := range comp.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Value == `"quota"` {
					return true
				}
			}
			return false
		}
		return isUserIdent(args[0])
	case "Save":
		return isUserIdent(args[0])
	}
	return false
}

func isUserIdent(expr ast.Expr) bool {
	if u, ok := expr.(*ast.UnaryExpr); ok {
		expr = u.X
	}
	id, ok := expr.(*ast.Ident)
	return ok && strings.Contains(strings.ToLower(id.Name), "user")
}
```

허용 목록은 2026-10-15 HEAD 기준 grep으로 만든 것이다(`EditWithTx`는 map 기반 `Updates`에 quota 키가 없어 검출되지 않는다). 실행: `GOWORK=off go test ./model/ -run TestUserQuotaWriteSitesAreAllowlisted -count=1 -v` → 검출기가 더 찾거나 덜 찾으면 FAIL하며 차이를 출력한다. `SetOrganizationMemberQuota`는 `users.quota`를 직접 쓰므로 호출자(`service/org_user_export.go`)가 `InvalidateUserCache`를 부르는지 이 단계에서 다시 확인한다.

- [ ] **Step 2: 허용 목록 맞추기**

FAIL이면 출력의 각 `파일:함수`를 열어 (1) 어느 테이블의 `quota`인지, (2) 사용자 지갑이면 캐시 동기화나 해시 삭제가 있는지 확인하고 사유를 적어 목록에 넣는다. 사용자 지갑인데 캐시를 맞추지 않는 경로가 새로 발견되면 이 계획 밖의 결함이므로 ledger에 기록하고 Task 11의 기록 §6에 적는다(고치지 않는다). 스펙 §5 표를 결과에 맞게 갱신한다. 실행: PASS, 그리고 목록에서 한 항목을 지워 FAIL하는지 확인한 뒤 복구한다.

- [ ] **Step 3: 커밋**

```bash
git add model/user_quota_write_sites_fork_test.go docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md
git commit -m "test(quota): allowlist every users.quota write site so merges cannot add unsynced ones

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: Redis Sentinel 직접 연결

**Files:**
- Create: `common/redis_sentinel_fork.go`, `common/redis_sentinel_fork_test.go`, `scripts/redis-sentinel/docker-compose.yml`, `scripts/redis-sentinel/README.md`
- Modify: `common/redis.go:40`(`RDB = redis.NewClient(opt)` 한 줄), `.env.example`(Redis 절), `docker-compose.yml:34` 아래 주석

**Interfaces:**
- Consumes: go-redis v8 `redis.FailoverOptions{MasterName, SentinelAddrs, SentinelPassword, Username, Password, DB, PoolSize}`, `redis.NewFailoverClient(*FailoverOptions) *redis.Client`.
- Produces: `sentinelAddrsFromEnv(raw string) []string`(쉼표 분리, 공백 제거, 빈 항목 제외), `buildFailoverOptions(opt *redis.Options, addrs []string, masterName, sentinelPassword string) *redis.FailoverOptions`, `newRedisClient(opt *redis.Options) *redis.Client`(Sentinel 주소가 있으면 failover 클라이언트, 없으면 `redis.NewClient(opt)`).

- [ ] **Step 1: 실패하는 테스트**

`common/redis_sentinel_fork_test.go`:

```go
package common

import (
	"os"
	"testing"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestSentinelAddrsFromEnv(t *testing.T) {
	require.Nil(t, sentinelAddrsFromEnv(""))
	require.Nil(t, sentinelAddrsFromEnv(" , "))
	require.Equal(t, []string{"10.0.0.1:26379", "10.0.0.2:26379"}, sentinelAddrsFromEnv(" 10.0.0.1:26379, 10.0.0.2:26379 ,"))
}

func TestBuildFailoverOptionsCarriesMasterCredentials(t *testing.T) {
	opt, err := redis.ParseURL("redis://app:secret@ignored-host:6379/2")
	require.NoError(t, err)
	opt.PoolSize = 7
	fo := buildFailoverOptions(opt, []string{"s1:26379"}, "", "spw")
	require.Equal(t, "mymaster", fo.MasterName, "default master name")
	require.Equal(t, []string{"s1:26379"}, fo.SentinelAddrs)
	require.Equal(t, "spw", fo.SentinelPassword)
	require.Equal(t, "app", fo.Username)
	require.Equal(t, "secret", fo.Password)
	require.Equal(t, 2, fo.DB)
	require.Equal(t, 7, fo.PoolSize)
	require.Equal(t, "prod-master", buildFailoverOptions(opt, nil, "prod-master", "").MasterName)
}

func TestNewRedisClientConnectsThroughSentinel(t *testing.T) {
	addrs := os.Getenv("REDIS_SENTINEL_TEST_ADDRS")
	if addrs == "" {
		t.Skip("set REDIS_SENTINEL_TEST_ADDRS=host:26379[,…] (see scripts/redis-sentinel) to run")
	}
	t.Setenv("REDIS_SENTINEL_ADDRS", addrs)
	t.Setenv("REDIS_SENTINEL_MASTER_NAME", os.Getenv("REDIS_SENTINEL_TEST_MASTER"))
	opt, err := redis.ParseURL("redis://ignored:6379/0")
	require.NoError(t, err)
	client := newRedisClient(opt)
	defer client.Close()
	require.NoError(t, client.Ping(t.Context()).Err())
	require.Equal(t, "PONG", client.Ping(t.Context()).Val())
}
```

실행: `GOWORK=off go test ./common/ -run 'Sentinel|FailoverOptions|NewRedisClient' -count=1` → 컴파일 실패.

- [ ] **Step 2: 구현**

`common/redis_sentinel_fork.go`:

```go
package common

import (
	"os"
	"strings"

	"github.com/go-redis/redis/v8"
)

// fork: O12 — Redis Sentinel 직접 연결. REDIS_SENTINEL_ADDRS가 있으면 failover
// 클라이언트를 만들고, master의 사용자 이름·비밀번호·DB·풀 크기는 REDIS_CONN_STRING
// 에서 가져온다(호스트는 무시). 설계 §4.14.

func sentinelAddrsFromEnv(raw string) []string {
	var addrs []string
	for _, part := range strings.Split(raw, ",") {
		if addr := strings.TrimSpace(part); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}

func buildFailoverOptions(opt *redis.Options, addrs []string, masterName, sentinelPassword string) *redis.FailoverOptions {
	if masterName == "" {
		masterName = "mymaster"
	}
	return &redis.FailoverOptions{
		MasterName:       masterName,
		SentinelAddrs:    addrs,
		SentinelPassword: sentinelPassword,
		Username:         opt.Username,
		Password:         opt.Password,
		DB:               opt.DB,
		PoolSize:         opt.PoolSize,
	}
}

func newRedisClient(opt *redis.Options) *redis.Client {
	addrs := sentinelAddrsFromEnv(os.Getenv("REDIS_SENTINEL_ADDRS"))
	if len(addrs) == 0 {
		return redis.NewClient(opt)
	}
	fo := buildFailoverOptions(opt, addrs, os.Getenv("REDIS_SENTINEL_MASTER_NAME"), os.Getenv("REDIS_SENTINEL_PASSWORD"))
	SysLog(LogText("Redis Sentinel enabled: master %s via %s (host in REDIS_CONN_STRING is ignored)", fo.MasterName, strings.Join(addrs, ",")))
	return redis.NewFailoverClient(fo)
}
```

`common/redis.go:40` `RDB = redis.NewClient(opt)` → `RDB = newRedisClient(opt) // fork: O12`.

`.env.example`의 `# REDIS_CONN_STRING=…` 아래:

```
# Redis Sentinel(선택). 설정하면 REDIS_CONN_STRING의 호스트는 무시하고 인증·DB 번호만 쓴다
# REDIS_SENTINEL_ADDRS=10.0.0.1:26379,10.0.0.2:26379,10.0.0.3:26379
# REDIS_SENTINEL_MASTER_NAME=mymaster
# REDIS_SENTINEL_PASSWORD=
```

`docker-compose.yml:34` 아래에 같은 세 변수를 주석으로 둔다.

`scripts/redis-sentinel/docker-compose.yml`: redis master 1, replica 1, sentinel 3(공식 `redis:7` 이미지, sentinel은 `redis-sentinel /etc/sentinel.conf`로 기동, 설정은 `sentinel monitor mymaster redis-master 6379 2`, `down-after-milliseconds 2000`, `failover-timeout 5000`). `README.md`: 기동 명령, `REDIS_SENTINEL_TEST_ADDRS=localhost:26379,localhost:26380,localhost:26381 REDIS_SENTINEL_TEST_MASTER=mymaster GOWORK=off go test ./common/ -run TestNewRedisClientConnectsThroughSentinel -v`, 그리고 `docker compose stop redis-master`로 장애 조치를 일으킨 뒤 같은 테스트가 다시 통과하는 확인 절차. 이 Mac에는 Docker가 없으므로 운영자 작업으로 기록에 남긴다.

실행: Step 1의 두 단위 테스트 PASS, 세 번째는 SKIP. `GOWORK=off go build ./...`.

- [ ] **Step 3: 커밋**

```bash
git add common/redis_sentinel_fork.go common/redis_sentinel_fork_test.go common/redis.go scripts/redis-sentinel .env.example docker-compose.yml
git commit -m "feat(redis): connect through Sentinel when REDIS_SENTINEL_ADDRS is set

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: 기록·overlay 표·운영 문서·전체 검증·병합 리허설

**Files:**
- Create: `docs/2026-10-15-quota-cache-residency.md`, `docs/manuals/redis-quota-cache.ko.md`(`docs/manuals/`가 없으면 `docs/ops/`를 만든다. 기존 운영 문서 위치를 `ls docs`로 먼저 확인한다)
- Modify: `docs/fork-overlays.md`(새 절 `## O12 잔액 캐시 상주·Sentinel`), spec `2026-10-14-upstream-rebase-design.md` §2.1 overlay 목록에 O12 한 줄과 §9 결정 한 줄, `docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md` 상태를 "구현 완료"로

- [ ] **Step 1: 전체 검증** — `GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"`; `GOWORK=off go test -race ./model/ -run 'Quota|Batch|Drift|Repair|Fresh|Aff' -count=3`; 프런트는 바뀌지 않았으므로 `cd web && bun run typecheck`만. 숫자를 기록한다.
- [ ] **Step 2: 병합 리허설** — `git fetch qn && git merge --no-commit --no-ff qn/main`; 충돌 목록을 기록하고 각 충돌이 overlay 표의 upstream 파일인지 확인한 뒤 `git merge --abort`. `git status`가 깨끗하고 HEAD가 그대로인지 확인한다.
- [ ] **Step 3: overlay 표** — `grep -rn "fork: O12" --include='*.go' .`로 모든 훅을 찾아 파일:줄을 표에 적는다. 복사본 스크립트 두 개(`writeUserCacheResident`, 차감·정산 스크립트)와 upstream `updateUserQuotaUsedQuotaAndRequestCount` 복사본(`flushUserQuotaRow`)에 "upstream 복사본, 병합 때 원본 변경 반영" 메모를 단다. `main.go` 훅 두 개(`FlushBatchUpdates`, `StartUserCacheSyncRepair`)도 포함한다.
- [ ] **Step 4: 운영 문서** — 설계 §4.8(스위치), §4.9(직접 DB 수정 뒤 `DEL user:<id>`, 배치 반영 뒤에), §4.13(AOF, maxmemory, 감시 지표), §4.14(Sentinel 변수)를 운영자용 한국어 문서로 정리한다. 어긋남 로그 문구(`user quota cache drift: user=… cache=… db=… diff=…`)와 대응 절차(대기분 반영 뒤 해시 삭제)를 넣는다.
- [ ] **Step 5: 기록** — `docs/2026-10-15-quota-cache-residency.md`: §1 문제와 재현 수치(DB 800/캐시 900, 최종 -100), §2 설계 요약(두 시계, 감지 규칙, 보강 세 가지, Sentinel), §3 훅 목록, §4 결정 사항(스펙 §3 + 실행 중 ruling), §5 남는 한계(스펙 §6 + Task 9에서 발견된 것), §6 검증 수치와 운영자 작업(Sentinel 실연결 테스트, AOF 설정, MySQL/PostgreSQL 미실행), §7 다음 단계(단계 F).
- [ ] **Step 6: 커밋** — `docs: record the resident user quota cache and Sentinel connection work` (트레일러 동일).

---

## 계획 결정 사항

1. 상주는 사용자 해시에만 적용한다. API 키 해시는 upstream 그대로다(스펙 결정 4). 키 한도만 있는 요청(구독 플랜)의 키 한도 초과는 남는다(스펙 §6).
2. upstream Lua 스크립트는 fork 파일에 복사해 고친다(차감·정산 스크립트, `writeUserCache` 스크립트). 훅은 함수 첫 줄 위임 또는 Eval 호출 한 줄 교체다. 스크립트 문자열 안을 부분 수정하지 않는다. 대가는 병합 때 upstream 스크립트 변경을 복사본에 옮겨야 한다는 것이고, overlay 메모로 남긴다.
3. 잔액 TTL은 `SYNC_FREQUENCY`보다 짧아질 수 없다. 인증 fence 수명 계산(`SYNC_FREQUENCY` 기준)이 "fence가 신선한 정보보다 오래 산다"는 성질을 유지하기 위해서다.
4. 스위치를 끄면 새 쓰기부터 upstream 동작이다. 이미 24시간 TTL을 받은 해시는 남은 TTL 동안 그대로 있다. 즉시 원하면 운영자가 `user:*` 키를 지운다(운영 문서에 적는다).
5. 어긋남 감지는 `userBaseAfterPopulate`(신선 기한 재읽기)에서만 한다. 첫 채움은 차이가 0이라 기억만 지운다.
6. 복구 목록은 배치 모드와 무관하게 `max(BATCH_UPDATE_INTERVAL,1)`초마다 돈다. Redis 오류 우회 차감은 배치를 끈 환경에서도 생기기 때문이다.
7. 배치 반영 실패 재시도는 사용자 행의 세 열(quota, used_quota, request_count)을 함께 다시 넣는다. 세 열은 한 UPDATE로 쓰이므로 함께 실패한다.
8. Sentinel 모드에서도 `REDIS_CONN_STRING`은 필수다(Redis 사용 여부 스위치이자 master 인증 정보). 호스트는 무시하고 로그에 남긴다.
9. 실제 Sentinel 장애 조치 확인은 Docker가 있는 환경의 운영자 작업이다. 코드 쪽은 옵션 생성 단위 테스트와 환경 변수로 켜는 실연결 테스트로 고정한다.
10. `team` 브랜치는 건드리지 않는다(스펙 결정 8).
