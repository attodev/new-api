#!/usr/bin/env bash
# 7. 인증: API 키 생성 즉시 사용·삭제 즉시 거부, 사용자 비활성화 즉시 거부
. "$(dirname "$0")/lib.sh"
name="ft-$(date +%s)"

log "API 키 생성 (사용자 $USER_ID 명의로 만들려면 그 사용자의 PAT가 필요하므로 관리자 자신의 키로 검사)"
api_ok POST /api/token/ "{\"name\":\"$name\",\"remain_quota\":1000000,\"unlimited_quota\":false,\"expired_time\":-1,\"model_limits_enabled\":false,\"model_limits\":\"\",\"allow_ips\":\"\",\"group\":\"\"}" >/dev/null
tid=$(api GET "/api/token/?p=1&page_size=50" | jq -r --arg n "$name" '.data.items[] | select(.name==$n) | .id')
[ -n "$tid" ] || { fail "만든 토큰을 목록에서 찾지 못함"; finish; }
key=$(api_ok POST "/api/token/$tid/key" | jq -r '.data.key')
code=$(USER_KEY="$key" chat false); [ "$code" = 200 ] && pass "새 키 즉시 사용 200" || fail "새 키 HTTP $code"
api_ok DELETE "/api/token/$tid" >/dev/null
code=$(USER_KEY="$key" chat false); [ "$code" = 401 ] && pass "삭제 즉시 401" || fail "삭제 뒤 HTTP $code (기대 401)"

if [ -n "${ADMIN_PASSWORD:-}" ]; then
  log "사용자 $USER_ID 비활성화(관리자 API + step-up 증명) → 즉시 거부 → 재활성화"
  out=$(api_proof POST /api/user/manage "{\"id\":$USER_ID,\"action\":\"disable\"}" admin.user.manage "{\"user_id\":$USER_ID,\"action\":\"disable\"}")
  [ "$(printf '%s' "$out" | jq -r '.success')" = "true" ] || { echo "$out"; fail "비활성화 호출 실패"; }
  code=$(chat false); [ "$code" != 200 ] && pass "비활성화 즉시 HTTP $code" || fail "비활성화 뒤에도 200"
  out=$(api_proof POST /api/user/manage "{\"id\":$USER_ID,\"action\":\"enable\"}" admin.user.manage "{\"user_id\":$USER_ID,\"action\":\"enable\"}")
  [ "$(printf '%s' "$out" | jq -r '.success')" = "true" ] || { echo "$out"; fail "재활성화 호출 실패"; }
  code=$(chat false); [ "$code" = 200 ] && pass "재활성화 뒤 200" || fail "재활성화 뒤 HTTP $code"
else
  log "사용자 $USER_ID 비활성화(DB 직접 변경; ADMIN_PASSWORD 없음) → SYNC_FREQUENCY 안에 거부 → 재활성화"
  SYNC=${SYNC_FREQUENCY:-60}
  sql "UPDATE users SET status=2 WHERE id=$USER_ID" >/dev/null; t0=$(now)
  for _ in $(seq 1 $((SYNC+30))); do code=$(chat false); [ "$code" != 200 ] && break; sleep 1; done
  el=$(( $(now) - t0 ))
  [ "$code" != 200 ] && [ "$el" -le $((SYNC+5)) ] && pass "비활성화 ${el}s 뒤 HTTP $code (≤ $SYNC)" || fail "비활성화가 ${el}s 안에 반영되지 않음(HTTP $code)"
  sql "UPDATE users SET status=1 WHERE id=$USER_ID" >/dev/null
  for _ in $(seq 1 $((SYNC+30))); do code=$(chat false); [ "$code" = 200 ] && break; sleep 1; done
  [ "$code" = 200 ] && pass "재활성화 반영" || fail "재활성화 뒤 HTTP $code"
  echo "note: 관리자 화면에서 비활성화하면 즉시 거부돼야 한다(auth version). 수동으로 한 번 확인"
fi

log "비밀번호 해시 알고리즘 (시험 기간 bcrypt)"
h=$(sql "SELECT substr(password,1,4) FROM users ORDER BY id DESC LIMIT 1")
case "$h" in '$2a$'|'$2b$'|'$2y$') pass "최근 사용자 해시 bcrypt ($h)";; '$arg') fail "argon2id 해시: ACCOUNT_PASSWORD_HASH_ALGORITHM=bcrypt 누락, team 롤백 시 로그인 불가";; *) echo "note: 최근 사용자 해시 접두사 '$h'";; esac
finish
