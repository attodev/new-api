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
