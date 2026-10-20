# shellcheck shell=bash
# 공용 함수. 각 스크립트가 source 한다.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$HERE/env.sh" ] || { echo "ERROR: $HERE/env.sh 가 없다. env.example.sh를 복사해 채운다." >&2; exit 2; }
# shellcheck source=/dev/null
. "$HERE/env.sh"
for v in BASE_URL ADMIN_PAT USER_ID USER_KEY MODEL COMPOSE_DIR; do
  [ -n "${!v:-}" ] || { echo "ERROR: env.sh의 $v 가 비어 있다" >&2; exit 2; }
done
: "${COMPOSE:=docker compose}" "${APP_SERVICE:=newapi}" "${PG_SERVICE:=postgres}" "${REDIS_SERVICE:=redis}"
: "${DB_USER:=appuser}" "${DB_NAME:=appdb}" "${BATCH_INTERVAL:=5}"
command -v curl >/dev/null || { echo "ERROR: curl 필요" >&2; exit 2; }
command -v jq >/dev/null || { echo "ERROR: jq 필요" >&2; exit 2; }

FAILS=0
log()  { printf '\n== %s\n' "$*"; }
pass() { printf 'PASS: %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; FAILS=$((FAILS+1)); }
finish() { if [ "$FAILS" -eq 0 ]; then printf '\n모두 통과\n'; else printf '\n실패 %d건\n' "$FAILS" >&2; exit 1; fi; }

# 관리자 API: api METHOD PATH [JSON]
api() {
  local m=$1 p=$2 b=${3:-}
  curl -sS -X "$m" "$BASE_URL$p" -H "Authorization: Bearer $ADMIN_PAT" -H "Content-Type: application/json" ${b:+-d "$b"}
}
api_ok() { # api 응답의 success 필드가 true가 아니면 메시지와 함께 실패
  local out; out=$(api "$@")
  if [ "$(printf '%s' "$out" | jq -r '.success')" != "true" ]; then
    printf '%s\n' "$out" >&2; return 1
  fi
  printf '%s' "$out"
}

# compose 안의 psql / redis-cli
sql() { (cd "$COMPOSE_DIR" && $COMPOSE exec -T "$PG_SERVICE" psql -U "$DB_USER" -d "$DB_NAME" -v ON_ERROR_STOP=1 -At -c "$1"); }
rds() { (cd "$COMPOSE_DIR" && $COMPOSE exec -T "$REDIS_SERVICE" sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" exec redis-cli "$@"' sh "$@"); }
app_logs() { (cd "$COMPOSE_DIR" && $COMPOSE logs --no-color --tail="${1:-200}" "$APP_SERVICE"); }

db_quota()    { sql "SELECT quota FROM users WHERE id=$USER_ID"; }
redis_quota() { rds HGET "user:$USER_ID" Quota; }
api_quota()   { api GET "/api/user/$USER_ID" | jq -r '.data.quota'; }
# 사용자 소비 로그: logs_sum <since_unix> -> "합계 건수"
logs_sum()    { sql "SELECT COALESCE(SUM(quota),0) || ' ' || COUNT(*) FROM logs WHERE user_id=$USER_ID AND type=2 AND created_at>=$1"; }
now()         { date +%s; }

# 잔액을 정확히 value로 맞춘다(관리자 API, 캐시와 DB 모두 반영)
set_user_quota() {
  api_ok POST /api/user/manage "{\"id\":$USER_ID,\"action\":\"add_quota\",\"mode\":\"override\",\"value\":$1}" >/dev/null
}

# 채팅 요청 1건: chat <stream true|false> [model] -> HTTP 코드 한 줄 출력
chat() {
  local stream=$1 model=${2:-$MODEL}
  curl -sS -o /dev/null -w '%{http_code}\n' "$BASE_URL/v1/chat/completions" \
    -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$model\",\"stream\":$stream,\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with the single word OK.\"}]}"
}

# 예약(pre-consume) 검증용 요청: 긴 프롬프트(약 PROMPT_REPEAT×9 토큰) + 출력 1토큰.
# 예약액은 "프롬프트 토큰 × pre_consume_multiplier × 모델 ratio × 그룹 ratio"라 프롬프트가 짧고
# 모델이 싸면 0으로 반올림되어 아무것도 예약되지 않는다. 출력을 1토큰으로 묶으면 실제 비용 ≈ 예약액이
# 되어 한도 검증이 정확해진다.
PROMPT_REPEAT=${PROMPT_REPEAT:-400}
long_prompt() { printf 'The quick brown fox jumps over the lazy dog near the river bank. %.0s' $(seq 1 "$PROMPT_REPEAT"); }
chat_reserve() { # [model] -> HTTP 코드 한 줄
  local model=${1:-$MODEL}
  curl -sS -o /dev/null -w '%{http_code}\n' "$BASE_URL/v1/chat/completions" \
    -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$model\",\"stream\":false,\"max_tokens\":1,\"messages\":[{\"role\":\"user\",\"content\":\"$(long_prompt) Reply OK.\"}]}"
}
count_200() { cat "$1"/* | grep -c '^200$' || true; }
chat_body() { # 본문까지 받는다
  local stream=$1 model=${2:-$MODEL}
  curl -sS "$BASE_URL/v1/chat/completions" \
    -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$model\",\"stream\":$stream,\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with the single word OK.\"}]}"
}

wait_batch() { # 배치 두 번 지나기
  local s=$((BATCH_INTERVAL*2+2)); echo "배치 반영 대기 ${s}s"; sleep "$s"
}
