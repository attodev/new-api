#!/usr/bin/env bash
# 1. 과금 정확성: 요청 N건 뒤 logs 합계 = DB 잔액 감소 = Redis 잔액 감소
. "$(dirname "$0")/lib.sh"
N=${N:-10}

log "시작 상태"
q0_db=$(db_quota); q0_rd=$(redis_quota); t0=$(now)
echo "DB=$q0_db Redis=${q0_rd:-없음}"
[ -n "$q0_rd" ] || echo "note: Redis에 아직 해시가 없다(첫 요청 뒤 생긴다)"

log "요청 ${N}건 (비스트리밍·스트리밍 번갈아)"
ok=0
for i in $(seq 1 "$N"); do
  if [ $((i % 2)) -eq 0 ]; then s=true; else s=false; fi
  code=$(chat $s); [ "$code" = 200 ] && ok=$((ok+1)) || echo "요청 $i: HTTP $code"
done
[ "$ok" = "$N" ] && pass "요청 $N건 모두 200" || fail "성공 $ok/$N"

wait_batch
log "결과 비교"
q1_db=$(db_quota); q1_rd=$(redis_quota)
read -r sum cnt <<<"$(logs_sum "$t0")"
echo "logs 합계=$sum (${cnt}건)  DB 감소=$((q0_db-q1_db))  Redis 잔액=$q1_rd  DB 잔액=$q1_db"
[ "$cnt" = "$N" ] && pass "로그 ${N}건" || fail "로그 건수 $cnt (기대 $N)"
[ "$sum" = "$((q0_db-q1_db))" ] && pass "logs 합계 = DB 감소분" || fail "logs 합계 $sum ≠ DB 감소 $((q0_db-q1_db))"
[ "$q1_rd" = "$q1_db" ] && pass "Redis 잔액 = DB 잔액" || fail "Redis $q1_rd ≠ DB $q1_db (배치 미반영이면 몇 초 뒤 다시 확인)"
[ "$sum" -gt 0 ] && pass "과금 금액 > 0" || fail "과금이 0이다(모델 가격 설정 확인)"
finish
