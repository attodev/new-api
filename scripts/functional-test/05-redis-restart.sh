#!/usr/bin/env bash
# 5. Redis 재시작 뒤 요청이 막히지 않고 DB 잔액으로 이어지는지 (AOF면 해시도 남는다)
. "$(dirname "$0")/lib.sh"

chat false >/dev/null; wait_batch
q0=$(db_quota); rd0=$(redis_quota); echo "재시작 전 DB=$q0 Redis=$rd0"
log "Redis 재시작"
(cd "$COMPOSE_DIR" && $COMPOSE restart "$REDIS_SERVICE")
sleep 3
rd1=$(redis_quota || true); echo "재시작 직후 Redis 잔액=${rd1:-없음(영속성 없음)}"
t0=$(now)
code=$(chat false); [ "$code" = 200 ] && pass "재시작 직후 요청 200" || fail "재시작 직후 HTTP $code"
wait_batch
q1=$(db_quota); rd2=$(redis_quota); read -r sum _ <<<"$(logs_sum "$t0")"
echo "요청 뒤 DB=$q1 Redis=$rd2 과금=$sum"
[ "$((q0-q1))" = "$sum" ] && pass "DB 감소 = 과금" || fail "DB 감소 $((q0-q1)) ≠ 과금 $sum"
[ "$rd2" = "$q1" ] && pass "Redis = DB" || fail "Redis $rd2 ≠ DB $q1"
if [ -n "$rd1" ]; then pass "AOF로 해시가 재시작을 넘어 남음"; else echo "note: 영속성 없이 돌고 있다(체크리스트 §2)"; fi
echo "앱 로그의 Redis 우회 메시지:"; app_logs 300 | grep -i "falling back to database\|redis" | tail -5 || true
finish
