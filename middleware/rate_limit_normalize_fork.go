package middleware

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

var invalidRateLimitConfigWarningOnce sync.Once

// fork: O11 — clamp malformed rate-limit configuration to safe bounds
// (team 2c619a31e). The warning is fixed and value-free: constructors run for
// many routes and must not flood logs or echo deployment configuration.
func normalizeRateLimitParameters(maxRequestNum int, duration int64) (int, int64) {
	maxDuration := int64(common.RateLimitKeyExpirationDuration / time.Second)
	if maxDuration <= 0 {
		maxDuration = 1
	}
	if maxRequestNum > 0 && duration > 0 && duration <= maxDuration {
		return maxRequestNum, duration
	}
	invalidRateLimitConfigWarningOnce.Do(func() {
		common.SysError(common.LogText("invalid rate limit configuration normalized to safe bounds"))
	})
	if maxRequestNum <= 0 {
		maxRequestNum = 1
	}
	if duration <= 0 {
		duration = 1
	} else if duration > maxDuration {
		duration = maxDuration
	}
	return maxRequestNum, duration
}
