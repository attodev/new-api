package common

import (
	"context"
	"errors"

	"github.com/go-redis/redis/v8"
)

// fork: O13 — atomic one-time reads (OIDC authorization codes, refresh tokens).

// IsRedisNotFound reports whether err represents a "key not found" result
// from Redis (redis.Nil), as opposed to any other (e.g. connectivity) error.
func IsRedisNotFound(err error) bool {
	return errors.Is(err, redis.Nil)
}

// RedisGetDel returns the value at key and deletes it in one atomic step.
func RedisGetDel(key string) (string, error) {
	if DebugEnabled {
		SysLog(LogText("Redis GETDEL: key=%s", key))
	}
	ctx := context.Background()
	return RDB.GetDel(ctx, key).Result()
}
