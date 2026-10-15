package common

// fork: O12 — 사용자 잔액 캐시 상주 TTL(초). 0 이하면 상주를 끄고 upstream 동작
// (SYNC_FREQUENCY TTL, 차감 때 연장 없음)으로 돌아간다. 설계:
// docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md §4.8
var QuotaCacheTTL int
