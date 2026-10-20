#!/usr/bin/env bash
# 2. 동시 요청으로 잔액을 넘겨 쓸 수 없는지. 잔액을 요청 3건분으로 맞추고 10건을 동시에 보낸다.
. "$(dirname "$0")/lib.sh"
PAR=${PAR:-10}

log "예약 검증용 요청(긴 프롬프트, 출력 1토큰) 1건의 비용 측정"
set_user_quota 100000000
t0=$(now); code=$(chat_reserve); [ "$code" = 200 ] || { fail "측정 요청 HTTP $code"; finish; }
wait_batch
read -r cost _ <<<"$(logs_sum "$t0")"
echo "1건 비용 = $cost quota"
if [ "$cost" -lt 50 ]; then
  echo "모델이 너무 싸서($cost quota) 예약액이 0으로 반올림된다. PROMPT_REPEAT를 늘리거나(현재 $PROMPT_REPEAT) 더 비싼 모델을 MODEL로 지정한다."
  fail "1건 비용 $cost < 50: 한도 검증에 의미 있는 비용이 아님"; finish
fi

# 예약액 = 로컬 토큰 추정 × pre_consume_multiplier × ratio, 정산 = 상류 usage. 토크나이저가 달라 두 값이
# 어긋나며 방향은 모델마다 다르다(Qwen: 예약 1.35배 커서 2건 통과; 상류가 더 세는 모델: 4건 예약, 잔액 음수).
# 그래서 성공 1~4건, 음수 ≤ 성공 수 × 비용의 25%로 판정한다. 음수 방향이면 pre_consume_multiplier를 올린다.
budget=$((cost*3 + cost/2))
log "잔액을 ${budget}(3.5건분)으로 맞추고 ${PAR}건 동시 요청"
set_user_quota "$budget"
t1=$(now)
tmp=$(mktemp -d); run_parallel "$tmp" "$PAR" chat_reserve_to
succ=$(count_200 "$tmp"); report_parallel "$tmp"; rm -rf "$tmp"
echo "성공 $succ / $PAR"
echo "앱 로그의 예약 금액:"; show_preconsume_log 2

wait_batch
q_db=$(db_quota); q_rd=$(redis_quota)
read -r sum cnt <<<"$(logs_sum "$t1")"
overrun=$(( succ*cost/4 + 1 ))
echo "logs 합계=$sum (${cnt}건) DB 잔액=$q_db Redis 잔액=$q_rd (허용 초과 $overrun = 성공 ${succ}건 × 비용 25%)"
[ "$q_db" -ge "$((-overrun))" ] && pass "DB 잔액 ≥ -$overrun (초과는 예약액과 실제 비용의 토크나이저 차이뿐)" || fail "DB 잔액 $q_db < -$overrun: 예약이 동작하지 않음"
[ "$succ" -le 4 ] && [ "$succ" -ge 1 ] && pass "성공 건수 $succ (예산 3.5건 → 3~4건 허용)" || fail "성공 $succ건: 예산을 넘었거나 0건"
[ "$cnt" = "$succ" ] && pass "과금 로그 ${cnt}건 = 성공 건수" || fail "과금 로그 $cnt건 ≠ 성공 $succ건 (거부된 요청이 과금됨)"
[ "$sum" -le "$((budget+overrun))" ] && pass "과금 합계 ≤ 예산+초과 허용" || fail "과금 합계 $sum > 예산 $budget+$overrun"
[ "$q_rd" = "$q_db" ] && pass "Redis = DB" || fail "Redis $q_rd ≠ DB $q_db"

log "잔액 복구"
set_user_quota 100000000; echo "잔액 100000000으로 되돌림"
finish
