#!/usr/bin/env bash
# 3. Redis 상주: TTL·FreshUntil, 사용자 정보 변경이 SYNC_FREQUENCY 안에 캐시에 반영되는지
. "$(dirname "$0")/lib.sh"
SYNC=${SYNC_FREQUENCY:-60}

log "요청 1건 뒤 해시 상태"
chat false >/dev/null
ttl=$(rds TTL "user:$USER_ID"); fresh=$(rds HGET "user:$USER_ID" FreshUntil); schema=$(rds HGET "user:$USER_ID" CacheSchema)
echo "TTL=$ttl FreshUntil=$fresh CacheSchema=$schema"
[ "${ttl:-0}" -gt 3600 ] && pass "TTL ${ttl}s (상주)" || fail "TTL $ttl: 상주가 꺼져 있다(QUOTA_CACHE_TTL 확인)"
[ -n "$fresh" ] && [ "$fresh" -gt "$(now)" ] && pass "FreshUntil이 미래" || fail "FreshUntil 없음/과거"
[ "$schema" = "2" ] && pass "CacheSchema 2" || fail "CacheSchema $schema"

log "그룹 변경이 캐시에 반영되는 시간"
user=$(api GET "/api/user/$USER_ID"); cur=$(printf '%s' "$user" | jq -r '.data.group')
groups=$(api GET /api/group/ | jq -r '.data[]' 2>/dev/null | grep -v "^$cur$" | head -1 || true)
if [ -z "$groups" ]; then
  echo "다른 그룹이 없어 건너뜀 (현재 $cur)"
else
  body=$(printf '%s' "$user" | jq -c --arg g "$groups" '.data | .group=$g | del(.password)')
  api_ok PUT /api/user/ "$body" >/dev/null
  t0=$(now)
  for _ in $(seq 1 $((SYNC+30))); do
    chat false >/dev/null || true
    g=$(rds HGET "user:$USER_ID" Group)
    if [ "$g" = "$groups" ]; then break; fi
    sleep 1
  done
  el=$(( $(now) - t0 ))
  if [ "$g" = "$groups" ]; then
    [ "$el" -le $((SYNC+5)) ] && pass "그룹 $cur→$groups 반영 ${el}s (≤ SYNC_FREQUENCY $SYNC)" || fail "반영 ${el}s > $SYNC"
  else
    fail "$((SYNC+30))s 안에 반영되지 않음 (Redis Group=$g)"
  fi
  body=$(printf '%s' "$user" | jq -c '.data | del(.password)')
  api_ok PUT /api/user/ "$body" >/dev/null; echo "그룹 $cur 으로 복구"
fi

log "드리프트 로그"
n=$(app_logs 5000 | grep -c "user quota cache drift" || true)
[ "$n" = 0 ] && pass "drift 로그 0건" || fail "drift 로그 ${n}건 (redis-quota-cache.ko.md 6절)"
finish
