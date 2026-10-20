#!/usr/bin/env bash
# 3. Redis 상주: TTL·FreshUntil, 사용자 정보 변경이 SYNC_FREQUENCY 안에 캐시에 반영되는지
. "$(dirname "$0")/lib.sh"
SYNC=${SYNC_FREQUENCY:-60}

log "요청 1건 뒤 해시 상태"
chat false >/dev/null
ttl=$(rds TTL "user:$USER_ID"); fresh=$(rds HGET "user:$USER_ID" FreshUntil); schema=$(rds HGET "user:$USER_ID" CacheSchema); t=$(now)
echo "TTL=$ttl FreshUntil=$fresh now=$t (FreshUntil-now=$((fresh-t))s) CacheSchema=$schema"
[ "${ttl:-0}" -gt 3600 ] && pass "TTL ${ttl}s (상주)" || fail "TTL $ttl: 상주가 꺼져 있다(QUOTA_CACHE_TTL 확인)"
# FreshUntil = 마지막으로 DB에서 사용자 정보를 읽은 시각 + SYNC_FREQUENCY. 요청 직후라면 now ~ now+SYNC 사이여야 한다.
if [ -n "$fresh" ] && [ "$fresh" -ge "$((t-SYNC))" ] && [ "$fresh" -le "$((t+SYNC+5))" ]; then pass "FreshUntil이 now±SYNC 안"; else fail "FreshUntil $fresh 가 now($t)±$SYNC 밖 (없음이면 상주 꺼짐, 멀면 시계 차이)"; fi
[ "$schema" = "2" ] && pass "CacheSchema 2" || fail "CacheSchema $schema"

log "FreshUntil이 지나면 다음 요청이 DB에서 다시 읽고 FreshUntil을 앞으로 미는지"
wait=$((fresh - $(now) + 1)); [ "$wait" -lt 0 ] && wait=0; [ "$wait" -gt $((SYNC+5)) ] && wait=$((SYNC+5))
echo "${wait}s 대기 뒤 요청"; sleep "$wait"
chat false >/dev/null; fresh2=$(rds HGET "user:$USER_ID" FreshUntil); t2=$(now)
echo "FreshUntil $fresh → $fresh2 (now=$t2)"
[ -n "$fresh2" ] && [ "$fresh2" -gt "$fresh" ] && [ "$fresh2" -gt "$t2" ] && pass "요청이 FreshUntil을 갱신함(+$((fresh2-fresh))s)" || fail "FreshUntil이 갱신되지 않음"

log "DB에서 바꾼 그룹이 SYNC_FREQUENCY 안에 캐시에 반영되는지 (관리자 API는 캐시를 즉시 고치므로 DB 직접 변경으로 재읽기 경로를 본다)"
cur=$(sql "SELECT \"group\" FROM users WHERE id=$USER_ID")
other=$(api GET /api/group/ | jq -r '.data[]' 2>/dev/null | grep -v "^$cur$" | head -1 || true)
if [ -z "$other" ]; then
  echo "다른 그룹이 없어 건너뜀 (현재 $cur)"
else
  sql "UPDATE users SET \"group\"='$other' WHERE id=$USER_ID" >/dev/null
  t0=$(now)
  for _ in $(seq 1 $((SYNC+30))); do
    chat false >/dev/null || true
    g=$(rds HGET "user:$USER_ID" Group)
    if [ "$g" = "$other" ]; then break; fi
    sleep 1
  done
  el=$(( $(now) - t0 ))
  if [ "$g" = "$other" ]; then
    [ "$el" -le $((SYNC+5)) ] && pass "그룹 $cur→$other 캐시 반영 ${el}s (≤ SYNC_FREQUENCY $SYNC)" || fail "반영 ${el}s > $SYNC"
  else
    fail "$((SYNC+30))s 안에 반영되지 않음 (Redis Group=$g)"
  fi
  sql "UPDATE users SET \"group\"='$cur' WHERE id=$USER_ID" >/dev/null
  wait_batch; rds DEL "user:$USER_ID" >/dev/null; chat false >/dev/null
  echo "그룹 $cur 으로 복구 (캐시 재적재: Group=$(rds HGET "user:$USER_ID" Group))"
fi

log "드리프트 로그"
n=$(app_logs 5000 | grep -c "user quota cache drift" || true)
[ "$n" = 0 ] && pass "drift 로그 0건" || fail "drift 로그 ${n}건 (redis-quota-cache.ko.md 6절)"
finish
