#!/usr/bin/env bash
# 2. 동시 요청으로 잔액을 넘겨 쓸 수 없는지. 잔액을 요청 3건분으로 맞추고 10건을 동시에 보낸다.
. "$(dirname "$0")/lib.sh"
PAR=${PAR:-10}

log "요청 1건의 비용 측정"
set_user_quota 100000000
t0=$(now); chat false >/dev/null; wait_batch
read -r cost _ <<<"$(logs_sum "$t0")"
[ "$cost" -gt 0 ] || { fail "1건 비용을 측정하지 못함(과금 0)"; finish; }
echo "1건 비용 = $cost"

budget=$((cost*3 + cost/2))   # 3건은 되고 4건째는 안 되는 잔액
log "잔액을 ${budget}(3건분)으로 맞추고 ${PAR}건 동시 요청"
set_user_quota "$budget"
t1=$(now)
tmp=$(mktemp -d)
for i in $(seq 1 "$PAR"); do ( chat false > "$tmp/$i" ) & done; wait
succ=$(cat "$tmp"/* | grep -c '^200$' || true); rm -rf "$tmp"
echo "성공 $succ / $PAR"

wait_batch
q_db=$(db_quota); q_rd=$(redis_quota)
read -r sum cnt <<<"$(logs_sum "$t1")"
echo "logs 합계=$sum (${cnt}건) DB 잔액=$q_db Redis 잔액=$q_rd"
[ "$q_db" -ge 0 ] && pass "DB 잔액이 음수가 아님" || fail "DB 잔액 음수 $q_db"
[ "$succ" -le 3 ] && [ "$succ" -ge 1 ] && pass "성공 건수 $succ (예산 안)" || fail "성공 $succ건: 예산(3건)을 넘었거나 0건"
[ "$sum" -le "$budget" ] && pass "과금 합계 ≤ 예산" || fail "과금 합계 $sum > 예산 $budget"
[ "$q_rd" = "$q_db" ] && pass "Redis = DB" || fail "Redis $q_rd ≠ DB $q_db"

log "잔액 복구"
set_user_quota 100000000; echo "잔액 100000000으로 되돌림"
finish
