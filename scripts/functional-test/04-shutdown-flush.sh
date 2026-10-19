#!/usr/bin/env bash
# 4. 종료 경로: 요청 직후 SIGTERM → 배치 큐가 DB에 비워지고 'queue empty: true'
. "$(dirname "$0")/lib.sh"

log "요청 5건 보내고 바로 중지"
t0=$(now); q0=$(db_quota)
for _ in 1 2 3 4 5; do chat false >/dev/null; done
rd=$(redis_quota); echo "중지 직전 Redis 잔액=$rd DB 잔액=$(db_quota)"
(cd "$COMPOSE_DIR" && $COMPOSE stop "$APP_SERVICE")
line=$(app_logs 100 | grep -E "flushed batch updates|queue empty" | tail -1 || true)
echo "$line"
printf '%s' "$line" | grep -q "queue empty: true" && pass "배치 큐 비움" || fail "'queue empty: true' 없음 (stop_grace_period·SHUTDOWN_TIMEOUT_SECONDS 확인)"
q1=$(db_quota); read -r sum cnt <<<"$(logs_sum "$t0")"
echo "logs 합계=$sum (${cnt}건) DB 감소=$((q0-q1))"
[ "$sum" = "$((q0-q1))" ] && pass "종료 뒤 DB = logs" || fail "DB 감소 $((q0-q1)) ≠ logs $sum"
[ "$rd" = "$q1" ] && pass "중지 직전 Redis 잔액 = 종료 뒤 DB 잔액" || fail "Redis $rd ≠ DB $q1"

log "다시 기동"
(cd "$COMPOSE_DIR" && $COMPOSE start "$APP_SERVICE")
for _ in $(seq 1 60); do curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1 && break; sleep 1; done
code=$(chat false); [ "$code" = 200 ] && pass "재기동 뒤 요청 200" || fail "재기동 뒤 HTTP $code"
finish
