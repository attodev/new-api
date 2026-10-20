#!/usr/bin/env bash
# 9. 조직 지갑 과금: 조직 멤버의 요청이 조직 지갑(organizations.quota)과 멤버 한도(users.quota)에서
#    같은 금액만큼 빠지고, 조직 지갑·멤버 한도 어느 쪽도 넘겨 쓸 수 없으며, 비활성 조직은 거부되는지.
#    env.sh에 ORG_USER_ID / ORG_USER_KEY(조직 멤버가 될 시험 사용자와 그 API 키)가 필요하다.
#    ORG_ID를 비우면 그 사용자가 속한 조직을 쓰고, 없으면 사용자를 owner로 조직을 새로 만든다.
. "$(dirname "$0")/lib.sh"
: "${ORG_USER_ID:?env.sh에 ORG_USER_ID 필요}" "${ORG_USER_KEY:?env.sh에 ORG_USER_KEY 필요}"
N=${N:-10}; PAR=${PAR:-10}; BIG=100000000

org_row()    { sql "SELECT quota || ' ' || used_quota || ' ' || status FROM organizations WHERE id=$ORG_ID"; }
member_db()  { sql "SELECT quota FROM users WHERE id=$ORG_USER_ID"; }
member_rd()  { rds HGET "user:$ORG_USER_ID" Quota; }
member_logs(){ sql "SELECT COALESCE(SUM(quota),0) || ' ' || COUNT(*) FROM logs WHERE user_id=$ORG_USER_ID AND type=2 AND created_at>=$1"; }
set_org_quota()    { api_ok PATCH "/api/organizations/$ORG_ID" "{\"quota\":$1}" >/dev/null; }
set_org_status()   { api_ok PATCH "/api/organizations/$ORG_ID" "{\"status\":$1}" >/dev/null; }
set_member_limit() { api_ok PATCH "/api/organization/users/$ORG_USER_ID?organization_id=$ORG_ID" "{\"quota\":$1}" >/dev/null; }
mchat() { USER_KEY="$ORG_USER_KEY" chat false; }
mchat_reserve() { USER_KEY="$ORG_USER_KEY" chat_reserve; }

log "조직 준비"
if [ -z "${ORG_ID:-}" ]; then
  ORG_ID=$(api GET "/api/user/$ORG_USER_ID" | jq -r '.data.organization_id // 0')
  if [ "$ORG_ID" = 0 ] || [ -z "$ORG_ID" ]; then
    name="ft-org-$(date +%s)"
    out=$(api_ok POST /api/organizations/ "{\"name\":\"$name\",\"description\":\"functional test\",\"owner_user_id\":$ORG_USER_ID,\"quota\":$BIG}")
    ORG_ID=$(printf '%s' "$out" | jq -r '.data.id // .data.organization.id // empty')
    [ -n "$ORG_ID" ] || { echo "$out"; fail "조직 생성 응답에서 id를 찾지 못함"; finish; }
    echo "조직 $name (id=$ORG_ID) 생성, owner=$ORG_USER_ID"
  else
    echo "사용자 $ORG_USER_ID 가 속한 조직 id=$ORG_ID 사용"
  fi
fi
role=$(api GET "/api/user/$ORG_USER_ID" | jq -r '.data.organization_role')
echo "멤버 역할: $role"
set_org_quota "$BIG"; set_member_limit "$BIG"
echo "조직(quota used status)=$(org_row)  멤버 한도 DB=$(member_db)"

log "A. 요청 ${N}건: logs = 조직 지갑 감소 = 멤버 한도 감소 = used_quota 증가"
read -r oq0 ou0 _ <<<"$(org_row)"; m0=$(member_db); t0=$(now)
ok=0; for i in $(seq 1 "$N"); do c=$(mchat); [ "$c" = 200 ] && ok=$((ok+1)) || echo "요청 $i: HTTP $c"; done
[ "$ok" = "$N" ] && pass "요청 $N건 200" || fail "성공 $ok/$N"
wait_batch
read -r oq1 ou1 _ <<<"$(org_row)"; m1=$(member_db); mr=$(member_rd); read -r sum cnt <<<"$(member_logs "$t0")"
echo "logs 합계=$sum (${cnt}건) 조직 감소=$((oq0-oq1)) used 증가=$((ou1-ou0)) 멤버 한도 감소=$((m0-m1)) Redis 멤버=$mr"
[ "$sum" -gt 0 ] && [ "$cnt" = "$N" ] && pass "로그 ${N}건, 과금 > 0" || fail "로그 $cnt건 합계 $sum"
[ "$((oq0-oq1))" = "$sum" ] && pass "조직 지갑 감소 = logs" || fail "조직 감소 $((oq0-oq1)) ≠ logs $sum"
[ "$((ou1-ou0))" = "$sum" ] && pass "조직 used_quota 증가 = logs" || fail "used 증가 $((ou1-ou0)) ≠ logs $sum"
[ "$((m0-m1))" = "$sum" ] && pass "멤버 한도 감소 = logs" || fail "멤버 한도 감소 $((m0-m1)) ≠ logs $sum"
[ "$mr" = "$m1" ] && pass "Redis 멤버 한도 = DB" || fail "Redis $mr ≠ DB $m1"
log "예약 검증용 요청 비용 측정 (긴 프롬프트, 출력 1토큰)"
tc=$(now); code=$(mchat_reserve); [ "$code" = 200 ] || { fail "측정 요청 HTTP $code"; finish; }
wait_batch; read -r cost _ <<<"$(member_logs "$tc")"
echo "1건 비용 = $cost quota"
if [ "$cost" -lt 50 ]; then fail "1건 비용 $cost < 50: 모델이 너무 싸서 예약액이 0이 된다(PROMPT_REPEAT를 늘리거나 비싼 모델)"; finish; fi
slack=$((cost/10 + 1))

log "B. 조직 지갑이 3건분일 때 ${PAR}건 동시 → 조직 지갑 안에서만 성공"
set_member_limit "$BIG"; set_org_quota $((cost*3 + cost/2)); t1=$(now)
tmp=$(mktemp -d); for i in $(seq 1 "$PAR"); do ( mchat_reserve > "$tmp/$i" ) & done; wait
succ=$(count_200 "$tmp"); rm -rf "$tmp"
wait_batch; read -r oq2 _ _ <<<"$(org_row)"; read -r sumB cntB <<<"$(member_logs "$t1")"
echo "성공 $succ/$PAR 조직 잔액=$oq2 과금=$sumB (${cntB}건, 허용 오차 $slack)"
[ "$oq2" -ge "$((-slack))" ] && pass "조직 잔액 ≥ -$slack" || fail "조직 잔액 $oq2 < -$slack: 조직 예약이 동작하지 않음"
[ "$succ" -ge 1 ] && [ "$succ" -le 3 ] && pass "성공 $succ건 (예산 3건)" || fail "성공 $succ건: 조직 예산을 넘었거나 0건"
[ "$cntB" = "$succ" ] && pass "과금 로그 = 성공 건수" || fail "과금 로그 $cntB건 ≠ 성공 $succ건"
[ "$sumB" -le $((cost*3 + cost/2 + slack)) ] && pass "과금 ≤ 조직 예산+오차" || fail "과금 $sumB > 조직 예산"

log "C. 멤버 한도가 3건분일 때 ${PAR}건 동시 → 멤버 한도 안에서만 성공"
set_org_quota "$BIG"; set_member_limit $((cost*3 + cost/2)); t2=$(now)
tmp=$(mktemp -d); for i in $(seq 1 "$PAR"); do ( mchat_reserve > "$tmp/$i" ) & done; wait
succ=$(count_200 "$tmp"); rm -rf "$tmp"
wait_batch; m2=$(member_db); read -r oq3 _ _ <<<"$(org_row)"; read -r sumC _ <<<"$(member_logs "$t2")"
echo "성공 $succ/$PAR 멤버 한도=$m2 조직 감소=$((BIG-oq3)) 과금=$sumC (허용 오차 $slack)"
[ "$m2" -ge "$((-slack))" ] && pass "멤버 한도 ≥ -$slack" || fail "멤버 한도 $m2 < -$slack: 멤버 한도 예약이 동작하지 않음"
[ "$succ" -ge 1 ] && [ "$succ" -le 3 ] && pass "성공 $succ건 (한도 3건)" || fail "성공 $succ건: 멤버 한도를 넘었거나 0건"
[ "$((BIG-oq3))" = "$sumC" ] && pass "조직 지갑 감소 = 과금 (한도 거부분은 조직에서도 안 빠짐)" || fail "조직 감소 $((BIG-oq3)) ≠ 과금 $sumC"

log "D. 비활성 조직은 과금 거부"
set_org_quota "$BIG"; set_member_limit "$BIG"; set_org_status 2
c=$(mchat); [ "$c" != 200 ] && pass "비활성 조직 멤버 요청 HTTP $c" || fail "비활성 조직인데 200"
set_org_status 1; c=$(mchat); [ "$c" = 200 ] && pass "재활성 뒤 200" || fail "재활성 뒤 HTTP $c"

if [ -n "${ORG_USER_PAT:-}" ]; then
  log "E. 멤버의 개인 충전·교환 차단"
  key=$(api_ok POST /api/redemption/ '{"name":"ft-org-redeem","quota":1000,"count":1}' | jq -r '(.data[0]? // .data.data[0]? // .data.keys[0]?) // empty')
  out=$(curl -sS -X POST "$BASE_URL/api/user/topup" -H "Authorization: Bearer $ORG_USER_PAT" -H "Content-Type: application/json" -d "{\"key\":\"$key\"}")
  [ "$(printf '%s' "$out" | jq -r '.success')" = "false" ] && pass "멤버 교환 거부: $(printf '%s' "$out" | jq -r '.message')" || fail "조직 멤버가 개인 교환에 성공함"
else
  echo "E. ORG_USER_PAT 미설정: 멤버로 로그인해 지갑 화면에서 교환·충전이 막히는지 수동 확인"
fi

log "복구"; set_org_quota "$BIG"; set_member_limit "$BIG"; echo "조직 지갑·멤버 한도 $BIG 으로 되돌림 (조직은 남겨 둠, id=$ORG_ID)"
finish
