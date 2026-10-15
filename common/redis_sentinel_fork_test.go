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
