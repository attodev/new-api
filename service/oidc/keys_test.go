package oidc

import (
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOIDCTestDB gives model.UpdateOption a real options table and an
// in-memory OptionMap, with Redis off so caches use memory.
func setupOIDCTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)
	ResetKeyCacheForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Organization{}))
	model.DB = db

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		ResetKeyCacheForTest()
	})
}

func TestCurrentKey_CreatesOnceAndPersists(t *testing.T) {
	setupOIDCTestDB(t)

	k1, err := CurrentKey()
	require.NoError(t, err)
	require.NotNil(t, k1.Private)
	require.Len(t, k1.Kid, 22, "16 bytes base64url without padding")
	require.NotZero(t, k1.CreatedAt)

	k2, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k1.Kid, k2.Kid)

	// A fresh process (cache reset) reads the same key back from the options table.
	ResetKeyCacheForTest()
	k3, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k1.Kid, k3.Kid)
	require.Equal(t, k1.Private.D, k3.Private.D)

	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("`key` = ?", OptionSigningKeyCurrent).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestComputeKid_IsDeterministic(t *testing.T) {
	setupOIDCTestDB(t)
	k, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k.Kid, ComputeKid(&k.Private.PublicKey))
	require.Equal(t, ComputeKid(&k.Private.PublicKey), ComputeKid(&k.Private.PublicKey))
}

func TestRotateKey_PublishesBothThenRetiresPrevious(t *testing.T) {
	setupOIDCTestDB(t)
	old, err := CurrentKey()
	require.NoError(t, err)

	rotated, err := RotateKey()
	require.NoError(t, err)
	require.NotEqual(t, old.Kid, rotated.Kid)

	cur, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, rotated.Kid, cur.Kid)

	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, rotated.Kid, keys[0].Kid, "current first")
	require.Equal(t, old.Kid, keys[1].Kid)

	jwks, err := JWKS()
	require.NoError(t, err)
	list := jwks["keys"].([]map[string]any)
	require.Len(t, list, 2)
	require.Equal(t, "RSA", list[0]["kty"])
	require.Equal(t, "sig", list[0]["use"])
	require.Equal(t, "RS256", list[0]["alg"])
	require.Equal(t, rotated.Kid, list[0]["kid"])
	require.NotEmpty(t, list[0]["n"])
	require.Equal(t, "AQAB", list[0]["e"])

	// Age the previous key past retention: rewrite its stored created_at.
	stored := readStoredKey(t, OptionSigningKeyPrevious)
	stored.CreatedAt = common.GetTimestamp() - int64(PreviousKeyRetention.Seconds()) - 1
	stored.RotatedAt = common.GetTimestamp() - int64(PreviousKeyRetention.Seconds()) - 1
	writeStoredKey(t, OptionSigningKeyPrevious, stored)
	ResetKeyCacheForTest()

	keys, err = VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, rotated.Kid, keys[0].Kid)

	common.OptionMapRWMutex.RLock()
	_, stillThere := common.OptionMap[OptionSigningKeyPrevious]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, stillThere, "expired previous key is deleted from options")
}

func TestRotateKey_TwiceDropsOldest(t *testing.T) {
	setupOIDCTestDB(t)
	first, err := CurrentKey()
	require.NoError(t, err)
	second, err := RotateKey()
	require.NoError(t, err)
	third, err := RotateKey()
	require.NoError(t, err)

	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, third.Kid, keys[0].Kid)
	require.Equal(t, second.Kid, keys[1].Kid)
	for _, k := range keys {
		require.NotEqual(t, first.Kid, k.Kid)
	}
}

func readStoredKey(t *testing.T, option string) storedSigningKey {
	t.Helper()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[option]
	common.OptionMapRWMutex.RUnlock()
	require.NotEmpty(t, raw)
	var s storedSigningKey
	require.NoError(t, common.UnmarshalJsonStr(raw, &s))
	return s
}

func writeStoredKey(t *testing.T, option string, s storedSigningKey) {
	t.Helper()
	raw, err := common.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(option, string(raw)))
}

func TestVerificationKeys_NoPreviousKey(t *testing.T) {
	setupOIDCTestDB(t)
	cur, err := CurrentKey()
	require.NoError(t, err)
	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, cur.Kid, keys[0].Kid)
}

func TestVerificationKeys_RecentRotationKeepsPreviousDespiteOldCreatedAt(t *testing.T) {
	setupOIDCTestDB(t)
	old, err := CurrentKey()
	require.NoError(t, err)
	rotated, err := RotateKey()
	require.NoError(t, err)

	// The previous key was created long ago but rotated just now: retention
	// counts from RotatedAt, so it must still be published.
	stored := readStoredKey(t, OptionSigningKeyPrevious)
	stored.CreatedAt = common.GetTimestamp() - int64(PreviousKeyRetention.Seconds()) - 3600
	stored.RotatedAt = common.GetTimestamp()
	writeStoredKey(t, OptionSigningKeyPrevious, stored)
	ResetKeyCacheForTest()

	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, rotated.Kid, keys[0].Kid)
	require.Equal(t, old.Kid, keys[1].Kid)
}

func TestCurrentKey_ConcurrentFirstUseCreatesOneKey(t *testing.T) {
	setupOIDCTestDB(t)

	const n = 10
	kids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k, err := CurrentKey()
			errs[i] = err
			if err == nil {
				kids[i] = k.Kid
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		require.Equal(t, kids[0], kids[i])
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("`key` = ?", OptionSigningKeyCurrent).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
