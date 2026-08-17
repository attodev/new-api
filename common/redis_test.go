package common

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := RDB
	originalEnabled := RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	RedisEnabled = true
	t.Cleanup(func() {
		RDB = originalRDB
		RedisEnabled = originalEnabled
	})
}

func TestRedisGetDel_ReturnsValueAndDeletesKey(t *testing.T) {
	setupTestRedis(t)

	require.NoError(t, RedisSet("test:getdel:key", "hello", 0))

	val, err := RedisGetDel("test:getdel:key")
	require.NoError(t, err)
	require.Equal(t, "hello", val)

	_, err = RedisGet("test:getdel:key")
	require.Error(t, err, "key must be gone after GetDel")
}

func TestRedisGetDel_MissingKeyReturnsRedisNil(t *testing.T) {
	setupTestRedis(t)

	_, err := RedisGetDel("test:getdel:does-not-exist")
	require.Error(t, err)
	require.ErrorIs(t, err, redis.Nil)
}
