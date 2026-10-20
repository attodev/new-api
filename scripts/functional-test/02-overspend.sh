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

budget=$((cost*3 + cost/2))   # 3건은 되고 4건째는 안 되는 잔액
slack=$((cost/10 + 1))        # 예약액(프롬프트분)과 실제 비용(＋출력 1토큰)의 차이 허용치
log "잔액을 ${budget}(3건분)으로 맞추고 ${PAR}건 동시 요청"
set_user_quota "$budget"
t1=$(now)
tmp=$(mktemp -d)
for i in $(seq 1 "$PAR"); do ( chat_reserve > "$tmp/$i" ) & done; wait
succ=$(count_200 "$tmp"); echo "HTTP 코드 분포: $(sort "$tmp"/* | uniq -c | tr -s ' ' | tr '\n' ';')"; rm -rf "$tmp"
echo "성공 $succ / $PAR"

wait_batch
q_db=$(db_quota); q_rd=$(redis_quota)
read -r sum cnt <<<"$(logs_sum "$t1")"
echo "logs 합계=$sum (${cnt}건) DB 잔액=$q_db Redis 잔액=$q_rd (허용 오차 $slack)"
[ "$q_db" -ge "$((-slack))" ] && pass "DB 잔액 ≥ -$slack (예약액 밖 초과는 출력 1토큰분뿐)" || fail "DB 잔액 $q_db < -$slack: 예약이 동작하지 않음"
[ "$succ" -le 3 ] && [ "$succ" -ge 1 ] && pass "성공 건수 $succ (예산 3건 안)" || fail "성공 $succ건: 예산(3건)을 넘었거나 0건"
[ "$cnt" = "$succ" ] && pass "과금 로그 ${cnt}건 = 성공 건수" || fail "과금 로그 $cnt건 ≠ 성공 $succ건 (거부된 요청이 과금됨)"
[ "$sum" -le "$((budget+slack))" ] && pass "과금 합계 ≤ 예산+오차" || fail "과금 합계 $sum > 예산 $budget+$slack"
[ "$q_rd" = "$q_db" ] && pass "Redis = DB" || fail "Redis $q_rd ≠ DB $q_db"

log "잔액 복구"
set_user_quota 100000000; echo "잔액 100000000으로 되돌림"
finish
