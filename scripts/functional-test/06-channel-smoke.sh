#!/usr/bin/env bash
# 6. 채널 스모크: 모델마다 비스트리밍·스트리밍·tool call 1건씩, Claude native, Responses.
#    운영에서 쓰는 모델 이름으로 MODELS / CLAUDE_MODELS / RESPONSES_MODELS 를 env.sh에 적는다.
. "$(dirname "$0")/lib.sh"
MODELS=${MODELS:-$MODEL}
CLAUDE_MODELS=${CLAUDE_MODELS:-}
RESPONSES_MODELS=${RESPONSES_MODELS:-}
TOOL_CHOICE=${TOOL_CHOICE:-auto}

last_log() { sql "SELECT model_name || ' quota=' || quota || ' p=' || prompt_tokens || ' c=' || completion_tokens FROM logs WHERE user_id=$USER_ID AND type=2 ORDER BY id DESC LIMIT 1"; }

for m in $MODELS; do
  log "[$m] 비스트리밍"
  out=$(chat_body false "$m"); usage=$(printf '%s' "$out" | jq -c '.usage // empty' 2>/dev/null || true)
  if [ -n "$usage" ]; then pass "[$m] usage $usage"; else fail "[$m] 응답에 usage 없음: $(printf '%s' "$out" | head -c 300)"; fi
  log "[$m] 스트리밍 (usage 청크 포함 여부)"
  out=$(curl -sS "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"stream\":true,\"stream_options\":{\"include_usage\":true},\"max_tokens\":16,\"messages\":[{\"role\":\"user\",\"content\":\"Reply OK\"}]}")
  printf '%s' "$out" | grep -q '"usage"' && pass "[$m] 스트림 usage 청크" || fail "[$m] 스트림에 usage 없음"
  log "[$m] tool call (tool_choice=$TOOL_CHOICE)"
  # Claude 5.5 계열은 강제 선택(required→any)을 거부하므로 기본은 auto. 강제로 보고 싶으면 TOOL_CHOICE=required.
  out=$(curl -sS "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"max_tokens\":256,\"tools\":[{\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"description\":\"Get the current weather for a city\",\"parameters\":{\"type\":\"object\",\"properties\":{\"city\":{\"type\":\"string\"}},\"required\":[\"city\"]}}}],\"tool_choice\":\"$TOOL_CHOICE\",\"messages\":[{\"role\":\"user\",\"content\":\"Use the get_weather tool to check the weather in Seoul.\"}]}")
  if printf '%s' "$out" | jq -e '.choices[0].message.tool_calls[0].function.name' >/dev/null 2>&1; then
    pass "[$m] tool_calls 반환 $(printf '%s' "$out" | jq -c '.choices[0].message.tool_calls[0].function')"
  elif printf '%s' "$out" | jq -e '.error' >/dev/null 2>&1; then
    fail "[$m] tool call 오류: $(printf '%s' "$out" | head -c 300)"
  else
    echo "note: [$m] 모델이 도구를 쓰지 않고 답함(finish_reason=$(printf '%s' "$out" | jq -r '.choices[0].finish_reason')): $(printf '%s' "$out" | jq -r '.choices[0].message.content' | head -c 120)"
  fi
  sleep 1; echo "최근 로그: $(last_log)"
done

for m in $CLAUDE_MODELS; do
  log "[$m] Claude native /v1/messages + adaptive thinking"
  out=$(curl -sS "$BASE_URL/v1/messages" -H "x-api-key: $USER_KEY" -H "anthropic-version: 2023-06-01" -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"max_tokens\":256,\"thinking\":{\"type\":\"adaptive\"},\"messages\":[{\"role\":\"user\",\"content\":\"In one sentence, why is the sky blue?\"}]}")
  if printf '%s' "$out" | jq -e '.usage.output_tokens' >/dev/null 2>&1; then
    pass "[$m] 응답 usage $(printf '%s' "$out" | jq -c '.usage')"
    printf '%s' "$out" | jq -e '[.content[] | select(.type=="thinking")] | length > 0' >/dev/null 2>&1 && pass "[$m] thinking 블록(요약) 있음" || echo "note: [$m] thinking 블록 없음 (adaptive라 모델이 생각하지 않기로 한 것이면 정상; thinking_tokens 확인)"
  else fail "[$m] 오류: $(printf '%s' "$out" | head -c 300)"; fi
  log "[$m] OpenAI 호환 경로 reasoning_effort"
  out=$(curl -sS "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"max_tokens\":128,\"reasoning_effort\":\"low\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply OK\"}]}")
  printf '%s' "$out" | jq -e '.usage' >/dev/null 2>&1 && pass "[$m] 호환 경로 usage $(printf '%s' "$out" | jq -c '.usage')" || fail "[$m] 호환 경로 오류: $(printf '%s' "$out" | head -c 300)"
  sleep 1; echo "최근 로그: $(last_log)"
done

for m in $RESPONSES_MODELS; do
  log "[$m] /v1/responses"
  out=$(curl -sS "$BASE_URL/v1/responses" -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
    -d "{\"model\":\"$m\",\"input\":\"Reply OK\",\"max_output_tokens\":16}")
  printf '%s' "$out" | jq -e '.usage.output_tokens' >/dev/null 2>&1 && pass "[$m] responses usage $(printf '%s' "$out" | jq -c '.usage')" || fail "[$m] responses 오류: $(printf '%s' "$out" | head -c 300)"
  sleep 1; echo "최근 로그: $(last_log)"
done

log "상류 오류 형식 (존재하지 않는 모델)"
out=$(chat_body false "no-such-model-xyz"); printf '%s' "$out" | jq -e '.error.message' >/dev/null 2>&1 && pass "오류가 OpenAI 형식 {error:{message}}" || fail "오류 형식 이상: $(printf '%s' "$out" | head -c 200)"
finish
