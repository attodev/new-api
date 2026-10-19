#!/usr/bin/env bash
# 8. 상한: 구독 플랜 기간·리셋 상한, 교환 코드 지갑 상한, 교환 적립
. "$(dirname "$0")/lib.sh"
MAX=9007199254740991

log "구독 플랜 상한 (생성 거부되어야 함)"
for body in \
  '{"plan":{"title":"ft-dur","duration_unit":"month","duration_value":1201,"price_amount":1}}' \
  '{"plan":{"title":"ft-custom","duration_unit":"custom","custom_seconds":31536001,"price_amount":1}}' \
  '{"plan":{"title":"ft-reset","duration_unit":"month","duration_value":1,"quota_reset_period":"custom","quota_reset_custom_seconds":31536001,"price_amount":1}}'; do
  out=$(api POST /api/subscription/admin/plans "$body")
  if [ "$(printf '%s' "$out" | jq -r '.success')" = "false" ]; then pass "거부: $(printf '%s' "$out" | jq -r '.message')"; else fail "상한 밖 플랜이 생성됨: $body"; fi
done

log "교환 코드 상한 (MaxWalletQuota+1 거부)"
out=$(api POST /api/redemption/ "{\"name\":\"ft-over\",\"quota\":$((MAX+1)),\"count\":1}")
[ "$(printf '%s' "$out" | jq -r '.success')" = "false" ] && pass "거부: $(printf '%s' "$out" | jq -r '.message')" || fail "상한 초과 교환 코드가 생성됨"

log "교환 코드 1000 적립 (사용자 $USER_ID 의 PAT가 없으면 수동: 지갑 화면에서 코드 입력)"
key=$(api_ok POST /api/redemption/ '{"name":"ft-redeem","quota":1000,"count":1}' | jq -r '(.data[0]? // .data.data[0]? // .data.keys[0]?) // empty')
echo "생성된 코드: $key"
if [ -n "${USER_PAT:-}" ]; then
  q0=$(db_quota)
  out=$(curl -sS -X POST "$BASE_URL/api/user/topup" -H "Authorization: Bearer $USER_PAT" -H "Content-Type: application/json" -d "{\"key\":\"$key\"}")
  [ "$(printf '%s' "$out" | jq -r '.success')" = "true" ] && pass "교환 성공" || fail "교환 실패: $out"
  sleep 2; q1=$(db_quota); [ "$((q1-q0))" = 1000 ] && pass "잔액 +1000" || fail "잔액 변화 $((q1-q0))"
  out=$(curl -sS -X POST "$BASE_URL/api/user/topup" -H "Authorization: Bearer $USER_PAT" -H "Content-Type: application/json" -d "{\"key\":\"$key\"}")
  [ "$(printf '%s' "$out" | jq -r '.success')" = "false" ] && pass "같은 코드 재사용 거부" || fail "같은 코드가 두 번 적립됨"
else
  echo "USER_PAT 미설정: 위 코드를 사용자 화면에서 한 번 쓰고, 두 번째는 거부되는지 본다"
fi
finish
