package openwebui

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/samber/hot"
)

// cacheTTL bounds how long a sub→user or (user,name)→token-key entry is
// trusted without touching the database. Admin link/unlink invalidates the
// sub entry explicitly, so the TTL only matters for out-of-band DB edits.
const cacheTTL = 10 * time.Minute

const (
	subCacheNamespace      = "openwebui:sub:v1"
	tokenKeyCacheNamespace = "openwebui:token_key:v1"
	cacheCapacity          = 10000
)

var (
	cacheMu           sync.Mutex
	subCacheInst      *cachex.HybridCache[int]
	tokenKeyCacheInst *cachex.HybridCache[string]
)

func redisOn() bool {
	return common.RedisEnabled && common.RDB != nil
}

// subCache maps Open WebUI sub → new-api user id.
func subCache() *cachex.HybridCache[int] {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if subCacheInst == nil {
		subCacheInst = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace:    cachex.Namespace(subCacheNamespace),
			Redis:        common.RDB,
			RedisEnabled: redisOn,
			RedisCodec:   cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, cacheCapacity).WithTTL(cacheTTL).WithJanitor().Build()
			},
		})
	}
	return subCacheInst
}

// tokenKeyCache maps "<userId>:<tokenName>" → token key.
func tokenKeyCache() *cachex.HybridCache[string] {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if tokenKeyCacheInst == nil {
		tokenKeyCacheInst = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace:    cachex.Namespace(tokenKeyCacheNamespace),
			Redis:        common.RDB,
			RedisEnabled: redisOn,
			RedisCodec:   cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, cacheCapacity).WithTTL(cacheTTL).WithJanitor().Build()
			},
		})
	}
	return tokenKeyCacheInst
}

// InvalidateSub drops the cached mapping for sub. Called by the admin API
// after linking or unlinking so the next relay request re-reads the table.
func InvalidateSub(sub string) {
	if sub == "" {
		return
	}
	_, _ = subCache().DeleteMany([]string{sub})
}

// ResetCachesForTest discards both caches so tests start clean.
func ResetCachesForTest() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	subCacheInst = nil
	tokenKeyCacheInst = nil
}
