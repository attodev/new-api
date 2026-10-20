# 기능 확인 절차 — develop 바이너리, 운영 DB 사본 시험 환경

작성: 2026-10-19. 전제: 운영 DB 사본을 복원한 compose 시험 환경에서 develop 이미지가 기동했고(`failed to migrate` 없음), 운영 체크리스트 §1(정·역방향 리허설)은 통과했다. 이 문서는 "team과 같은 돈이 같은 곳에서 빠지는가"를 중심으로 동작을 확인하는 순서다. 1~9절은 `scripts/functional-test/`가 자동화하고, 10절 이후는 수동이다. 결과는 끝의 표에 적는다.

## 0. 준비

1. **시험용 사용자**: 운영 사용자가 아닌 계정 하나(`USER_ID`). 잔액은 스크립트가 바꾼다. 그 사용자의 API 키 하나(`USER_KEY`, `sk-…`). 과금은 값싼 모델 하나(`MODEL`)로 한다.
2. **관리자 접근 토큰**(`ADMIN_PAT`): root 계정으로 로그인 → 설정 → 접근 토큰에서 만든다. 스크립트가 쓰는 권한은 `user:read`, `user:write`, `api_key:*`, `log:read`, `redemption:write`, `billing:write`. 시험 기간에만 쓰는 전체 권한 토큰이 가장 단순하다. 교환 적립까지 자동으로 보려면 시험 사용자의 토큰(`USER_PAT`)도 하나 만든다.
3. **도구**: 시험 compose가 있는 호스트에서 `curl`, `jq`. DB·Redis 조회는 `docker compose exec`로 한다.
4. 설정:
   ```bash
   cd scripts/functional-test && cp env.example.sh env.sh && $EDITOR env.sh
   ```
5. 모든 확인은 **시험 환경에만** 한다. 스크립트는 잔액을 덮어쓰고 앱과 Redis를 재시작한다.

바로 쓸 조회 명령(아래 절들이 반복해서 쓴다):
```bash
# DB 잔액·소비 로그
docker compose exec -T postgres psql -U appuser -d appdb -Atc "SELECT quota FROM users WHERE id=<USER_ID>"
docker compose exec -T postgres psql -U appuser -d appdb -Atc "SELECT id, model_name, quota, prompt_tokens, completion_tokens FROM logs WHERE user_id=<USER_ID> AND type=2 ORDER BY id DESC LIMIT 5"
# Redis 잔액 해시
docker compose exec -T redis sh -c 'REDISCLI_AUTH=$REDIS_PASSWORD redis-cli HGETALL user:<USER_ID>'
docker compose exec -T redis sh -c 'REDISCLI_AUTH=$REDIS_PASSWORD redis-cli TTL user:<USER_ID>'
# 앱 로그
docker compose logs --tail=200 newapi
```

## 1. 과금 정확성 — `01-billing-consistency.sh`

무엇을: 시험 사용자로 비스트리밍·스트리밍 요청 10건을 보내고 배치 간격 두 번(기본 12초)을 기다린 뒤 세 값을 비교한다.
통과 기준: `logs` 합계 = DB 잔액 감소 = Redis 잔액 감소, 로그 10건, 과금 금액 > 0.
실패하면: 과금이 0이면 모델 가격(관리자 → 모델 가격) 확인. Redis와 DB가 다르면 몇 초 뒤 `redis-cli HGET user:<id> Quota`와 DB를 다시 비교하고, 계속 다르면 로그의 `user quota cache drift` 줄을 본다.

추가로 수동 1건: 같은 모델·같은 프롬프트로 team 운영 로그의 1건과 금액을 비교한다. 그룹 ratio·모델 ratio·할인이 모두 반영된 값이어야 한다.

## 2. 동시 과소비 — `02-overspend.sh`

예약 규칙(둘 다 upstream 설계): 요청 전에 예약하는 금액은 **프롬프트 토큰 × `pre_consume_multiplier`(기본 1) × 모델 ratio × 그룹 ratio**이고 출력 토큰은 포함하지 않는다. 잔액이 `trust_quota_usd`(기본 $10)보다 많으면 예약 없이 통과한다(신뢰 우회; 조직 지갑은 예외). 그래서 프롬프트가 짧고 모델이 싸면 예약액이 0으로 반올림되어 동시 요청이 모두 통과하고 잔액이 요청 수만큼 음수가 된다. 이건 결함이 아니라 설계이며 team도 같은 식이었다. 스크립트는 이를 피하려고 약 3,600토큰 프롬프트(`PROMPT_REPEAT`, 기본 400)에 출력 1토큰으로 요청해 실제 비용 ≈ 예약액이 되게 하고, 1건 비용이 50 quota 미만이면 중단한다(더 비싼 모델을 `MODEL`로 지정).

무엇을: 예약 검증용 요청 1건 비용을 재고, 잔액을 3건분으로 맞춘 뒤 10건을 동시에 보낸다.
통과 기준: 성공 1~3건, 과금 로그 건수 = 성공 건수(거부된 요청은 과금되지 않음), DB 잔액 ≥ −(1건 비용의 10%)(출력 1토큰분만 예약 밖), 과금 합계 ≤ 예산+오차, Redis = DB. 끝나면 잔액을 100,000,000으로 되돌린다.

같은 방식으로 수동 확인할 것: 잔액 0 사용자(관리자 → 사용자 → 잔액 override 0)의 요청이 거부되는지; 한도 있는 키(`unlimited_quota=false`, `remain_quota` 작게)가 한도에서 멈추는지; 조직 멤버의 과금이 조직 지갑에서만 빠지는지(`organization_wallets` 또는 조직 대시보드).

## 3. Redis 상주 — `03-residency.sh`

무엇을: 요청 뒤 `TTL user:<id>`가 수만 초, `FreshUntil`이 미래, `CacheSchema=2`인지. 관리자 API로 그룹을 바꾸고 Redis 해시의 `Group`이 바뀌기까지 몇 초인지 잰다(`SYNC_FREQUENCY` 60초 안). 드리프트 로그 0건.
실패하면: TTL이 작으면 `QUOTA_CACHE_TTL`이 600 같은 값으로 들어온 것. 그룹 반영이 60초를 넘으면 `FreshUntil` 갱신 경로를 본다(`docs/manuals/redis-quota-cache.ko.md`).

## 4. 종료 경로 — `04-shutdown-flush.sh`

무엇을: 요청 5건 직후 `docker compose stop newapi`. 로그에 `flushed batch updates … queue empty: true`, 종료 뒤 DB = logs, 중지 직전 Redis 잔액 = 종료 뒤 DB 잔액. 다시 띄워 요청 200.
실패하면: `queue empty` 줄이 없으면 SIGKILL이 먼저 온 것 → compose의 `stop_grace_period`(45s)와 `SHUTDOWN_TIMEOUT_SECONDS`(30) 확인.

## 5. Redis 재시작 — `05-redis-restart.sh`

무엇을: `docker compose restart redis` 직후 요청이 200인지, 그 뒤 DB 감소 = 과금, Redis = DB. AOF를 켰으면 재시작 뒤에도 해시가 남아 있다.
실패하면: 요청이 막히면 develop이 아니라 team 동작이다(바이너리 확인). 해시가 사라졌는데 과금은 맞으면 영속성만 없는 것(체크리스트 §2).

## 6. 채널 스모크 — `06-channel-smoke.sh`

무엇을: `MODELS`의 모델마다 비스트리밍(usage 있음), 스트리밍(`stream_options.include_usage`로 usage 청크), tool call(`tool_calls` 반환). `CLAUDE_MODELS`는 `/v1/messages`에 `thinking: {type: adaptive}`(Opus 4.7+면 thinking 요약 블록이 와야 함)와 OpenAI 호환 경로의 `reasoning_effort`. `RESPONSES_MODELS`는 `/v1/responses`. 마지막에 없는 모델로 오류 형식(`{error:{message}}`) 확인. 각 요청 뒤 최근 로그 1건(model, quota, 토큰 수)을 찍는다.
```bash
MODELS="gpt-4o-mini gemini-2.5-flash" CLAUDE_MODELS="claude-opus-4-7 claude-sonnet-4-5" RESPONSES_MODELS="gpt-5" ./06-channel-smoke.sh
```
Bedrock·Vertex 채널을 쓰면 그 채널로 라우팅되는 모델 이름을 `CLAUDE_MODELS`에 같이 넣는다(세 채널 모두 400 없이 요약이 와야 한다).

수동: Gemini에 이미지 1장을 포함한 요청을 보내고 로그의 과금이 상한 규칙(이미지 토큰 ≤ prompt − cached − audio) 안인지; 이미지 생성·임베딩을 쓰면 1건씩; 스트리밍 중 클라이언트를 끊고(`curl … | head -c 100`) 로그에 부분 토큰이 과금되는지.

## 7. 인증 — `07-auth.sh`

무엇을: 관리자 명의 API 키 생성 → 즉시 200 → 삭제 → 즉시 401. 시험 사용자 비활성화 → 즉시 거부 → 재활성화 → 200. 최근 가입자 해시가 bcrypt(`$2a$`)인지.
수동: 운영 덤프의 기존 사용자로 로그인, 비밀번호 변경 후 재로그인, 운영에서 쓰는 OAuth 로그인 1건.

## 8. 상한·교환 — `08-bounds.sh`

무엇을: 구독 플랜 `duration_value=1201`, `custom_seconds=31,536,001`, `quota_reset_custom_seconds=31,536,001` 생성이 거부되는지; 교환 코드 `MaxWalletQuota+1` 거부; 1000짜리 교환 코드 적립 +1000, 재사용 거부(`USER_PAT` 없으면 지갑 화면에서 수동).

## 9. 조직 지갑 과금 — `09-org-billing.sh`

조직 멤버의 요청은 개인 지갑이 아니라 조직 지갑에서 빠진다. 구조: `organizations.quota`가 실제 잔액(DB에만 있음, Redis 캐시 없음)이고, 멤버의 `users.quota`는 "멤버 한도"로 같은 금액만큼 함께 줄어든다(Redis 상주 대상). 한 요청은 먼저 멤버 한도를 예약하고 그다음 조직 지갑을 예약하며, 둘 중 하나라도 모자라면 거부되고 먼저 예약한 쪽은 되돌린다. 조직 멤버는 개인 충전·교환·구독 구매를 할 수 없다.

준비: `env.sh`에 두 번째 시험 사용자 `ORG_USER_ID`와 그 API 키 `ORG_USER_KEY`. `ORG_ID`를 비우면 그 사용자가 속한 조직을 쓰고, 조직이 없으면 그 사용자를 owner로 `ft-org-<시각>` 조직을 만든다(관리자 접근 토큰에 조직 쓰기 권한 필요).

무엇을:
- A. 요청 10건 → 멤버 `logs` 합계 = 조직 지갑 감소 = `organizations.used_quota` 증가 = 멤버 한도 감소, Redis 멤버 한도 = DB.
- B. 조직 지갑을 3건분으로 줄이고(멤버 한도는 큼) 10건 동시(예약 검증용 긴 프롬프트) → 1~3건만 성공, 과금 로그 = 성공 건수, 조직 잔액 ≥ −오차, 과금 ≤ 예산+오차.
- C. 멤버 한도를 3건분으로 줄이고(조직 지갑은 큼) 10건 동시 → 1~3건만 성공, 멤버 한도 ≥ −오차, 조직 지갑 감소 = 과금(한도에서 거부된 요청은 조직에서도 빠지지 않음).
- D. 조직 상태 비활성(2) → 멤버 요청 거부, 재활성(1) → 200.
- E. `ORG_USER_PAT`가 있으면 멤버의 교환 코드 사용이 거부되는지. 없으면 멤버로 로그인해 지갑 화면에서 충전·교환이 막히는지 수동 확인.
끝나면 조직 지갑과 멤버 한도를 100,000,000으로 되돌리고 조직은 남겨 둔다.

수동으로 더 볼 것: 조직 대시보드(`/organization/dashboard`)의 사용량이 A의 합계와 맞는지; 조직 로그 화면(`/organization/usage-logs/common`)에 멤버 요청이 보이는지; 조직 구독 플랜을 멤버에게 배정하면 과금이 조직 지갑이 아니라 구독 사용량에서 빠지는지(배정 뒤 요청 1건 → `organizations.quota` 불변, `/api/organization/subscription/users` 의 사용량 증가).

## 10. 결제·구독 (sandbox, 수동)

쓰는 provider별로: 개인 충전 1건, 조직 충전 1건 → 주문 `success`, 잔액 정확히 1회 증가, 같은 콜백 재전송 시 중복 없음. Epay 미설정이면 충전 화면에 Alipay/WeChat 버튼이 없어야 한다. 구독 플랜 구매 → `user_subscriptions.end_time`과 다음 리셋 시각. 관리자 잔액 조정(add/subtract/override)과 지갑 상한 거부. 절차 상세는 운영 체크리스트 §4.

## 11. 조직 (수동 + e2e)

`docs/manuals/organization-test-guide.md`에서 A(생성·멤버), 지갑, 구독, 권한(멤버가 관리 화면 접근 불가), 멤버 정지·재개. e2e 21개를 시험 서버에 돌릴 수 있다:
```bash
cd web && bun run e2e:install
E2E_BASE_URL=http://<시험서버>:3000 E2E_BACKEND_URL=http://<시험서버>:3000 E2E_REUSE_SERVER=1 E2E_ALLOW_EXTERNAL_DB=1 \
E2E_ADMIN_USERNAME=root E2E_ADMIN_PASSWORD=<pw> bun run e2e
```
(e2e는 조직·사용자를 만들고 지우므로 운영 사본에서만.)

## 12. UI·콘텐츠 (수동)

로그인 → 대시보드 → 로그 → 지갑 → 조직 화면이 한국어로 나오고 키 문자열이 보이지 않는다. 영어 전환. 랜딩 페이지 소스(`view-source:`)에 `gtag/js?id=G-Z73Y7213LF`, 대시보드 소스에 `<CHATBOT_WIDGET_URL>/widget.js`, 빠른 시작 PDF 두 개 열림, 가격 페이지 안내문, 가입 화면 도메인 안내(옵션 켠 경우).

## 13. 운영 기능 (수동)

로그 조회·Excel 내보내기, 사용자 내보내기(비밀번호·토큰 빈 값), 대시보드 통계, 채널 테스트 버튼(이미지 채널 포함), 모델 동기화, 메일 발송(SMTP 실패 시 고정 문구), 토큰 만료·구독 리셋 작업이 로그에 도는지(`subscription quota reset task started`).

## 14. 부하와 타임아웃 (수동)

동시 스트림 50개를 몇 분 유지:
```bash
for i in $(seq 1 50); do curl -sN "$BASE_URL/v1/chat/completions" -H "Authorization: Bearer $USER_KEY" -H "Content-Type: application/json" \
  -d '{"model":"'"$MODEL"'","stream":true,"max_tokens":512,"messages":[{"role":"user","content":"Write 300 words about the sea."}]}' >/dev/null & done; wait
```
`docker stats newapi-test-app`로 메모리 추이, 끝난 뒤 메모리가 내려오는지. 로그에 `[ERR]` 노이즈가 없는지. 헤더를 안 주는 상류 대비는 `RELAY_RESPONSE_HEADER_TIMEOUT`(기본값 유지).

## 15. 롤백 리허설 (수동)

위를 다 돌린 DB에서 team 이미지로 돌아간다(운영 체크리스트 §9 순서: SIGTERM → `queue empty: true` → Redis `user:*`/`token:*` 삭제 → `options` 복원 → team 기동). 로그인 1건, 요청 1건, 충전 화면 1건. `failed to update option map`은 무해.

## 16. 결과 기록

| 절 | 날짜 | 수행자 | 결과 | 비고 |
|---|---|---|---|---|
| 1 과금 정확성 | | | | |
| 2 동시 과소비 | | | | |
| 3 Redis 상주 | | | | |
| 4 종료 경로 | | | | |
| 5 Redis 재시작 | | | | |
| 6 채널 스모크 (모델별) | | | | |
| 7 인증 | | | | |
| 8 상한·교환 | | | | |
| 9 조직 지갑 과금 | | | | |
| 10 결제·구독 | | | | |
| 11 조직·e2e | | | | |
| 12 UI·콘텐츠 | | | | |
| 13 운영 기능 | | | | |
| 14 부하 | | | | |
| 15 롤백 리허설 | | | | |
