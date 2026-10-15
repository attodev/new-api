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
