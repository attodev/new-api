package common

import (
	"os"
	"testing"
	"time"

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
	fo := buildFailoverOptions(opt, []string{"s1:26379"}, "", "suser", "spw")
	require.Equal(t, "mymaster", fo.MasterName, "default master name")
	require.Equal(t, []string{"s1:26379"}, fo.SentinelAddrs)
	require.Equal(t, "suser", fo.SentinelUsername)
	require.Equal(t, "spw", fo.SentinelPassword)
	require.Equal(t, "app", fo.Username)
	require.Equal(t, "secret", fo.Password)
	require.Equal(t, 2, fo.DB)
	require.Equal(t, 7, fo.PoolSize)
	require.Equal(t, "prod-master", buildFailoverOptions(opt, nil, "prod-master", "", "").MasterName)
}

func TestBuildFailoverOptionsCarriesTLSAndConnectionOptions(t *testing.T) {
	opt, err := redis.ParseURL("rediss://app:secret@ignored:6380/1?dial_timeout=2s&read_timeout=3s&write_timeout=4s&max_retries=4&pool_timeout=5s&min_idle_conns=2")
	require.NoError(t, err)
	opt.PoolFIFO = true
	opt.MaxConnAge = 6 * time.Minute
	opt.IdleTimeout = 7 * time.Minute
	opt.IdleCheckFrequency = 8 * time.Minute
	opt.MinRetryBackoff = 9 * time.Millisecond
	opt.MaxRetryBackoff = 10 * time.Millisecond
	fo := buildFailoverOptions(opt, []string{"s1:26379"}, "", "", "")
	require.NotNil(t, fo.TLSConfig)
	require.Equal(t, "ignored", fo.TLSConfig.ServerName, "ServerName stays the conn-string host")
	require.Equal(t, 1, fo.DB)
	require.Equal(t, 4, fo.MaxRetries)
	require.Equal(t, 2*time.Second, fo.DialTimeout)
	require.Equal(t, 3*time.Second, fo.ReadTimeout)
	require.Equal(t, 4*time.Second, fo.WriteTimeout)
	require.Equal(t, 5*time.Second, fo.PoolTimeout)
	require.Equal(t, 2, fo.MinIdleConns)
	require.True(t, fo.PoolFIFO)
	require.Equal(t, 6*time.Minute, fo.MaxConnAge)
	require.Equal(t, 7*time.Minute, fo.IdleTimeout)
	require.Equal(t, 8*time.Minute, fo.IdleCheckFrequency)
	require.Equal(t, 9*time.Millisecond, fo.MinRetryBackoff)
	require.Equal(t, 10*time.Millisecond, fo.MaxRetryBackoff)
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
	require.Equal(t, "PONG", client.Ping(t.Context()).Val())
}
