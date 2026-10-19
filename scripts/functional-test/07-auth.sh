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

log "사용자 $USER_ID 비활성화 → 즉시 거부 → 재활성화"
api_ok POST /api/user/manage "{\"id\":$USER_ID,\"action\":\"disable\"}" >/dev/null
code=$(chat false); [ "$code" != 200 ] && pass "비활성화 즉시 HTTP $code" || fail "비활성화 뒤에도 200"
api_ok POST /api/user/manage "{\"id\":$USER_ID,\"action\":\"enable\"}" >/dev/null
code=$(chat false); [ "$code" = 200 ] && pass "재활성화 뒤 200" || fail "재활성화 뒤 HTTP $code"

log "비밀번호 해시 알고리즘 (시험 기간 bcrypt)"
h=$(sql "SELECT substr(password,1,4) FROM users ORDER BY id DESC LIMIT 1")
case "$h" in '$2a$'|'$2b$'|'$2y$') pass "최근 사용자 해시 bcrypt ($h)";; '$arg') fail "argon2id 해시: ACCOUNT_PASSWORD_HASH_ALGORITHM=bcrypt 누락, team 롤백 시 로그인 불가";; *) echo "note: 최근 사용자 해시 접두사 '$h'";; esac
finish
