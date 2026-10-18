# 운영 환경 확인 절차 — develop(재기반 라인) 전환 전 점검

작성: 2026-10-15. 대상: `develop` `5d65184d6` 이후. 이 문서는 개발용 Mac(Docker 없음, SQLite만)에서 돌리지 못한 확인을 Docker와 운영 DB 사본이 있는 환경에서 수행하기 위한 절차다. 각 항목은 "무엇을, 어떻게, 통과 기준"으로 적었다. 결과는 이 문서 끝의 결과 표에 날짜와 함께 기록한다.

## 0. 준비

- Docker(compose v2), `mysql`·`psql` 클라이언트, Go 1.26(`GOWORK=off`), Bun.
- 운영 DB 사본(MySQL과 PostgreSQL 중 운영에서 쓰는 것). 사본은 team 스키마 그대로여야 한다(Toss 테이블, `users.quota` bigint 포함).
- 결제 provider의 sandbox 계정(Stripe, PayPal, Epay, Creem, Waffo 중 운영에서 켜는 것).
- 저장소: `git clone` 뒤 `develop` 체크아웃. `web/`에서 `bun install`.

## 1. 스키마 기동 리허설 (MySQL·PostgreSQL)

목적: 재기반 바이너리가 기존 운영 DB에서 데이터 손실 없이 기동하고, `users.quota`·`used_quota`·`aff_quota`·`aff_history`가 bigint로 유지되는지 확인한다(스펙 §5).

```bash
# 로컬 컨테이너로 (team 바이너리 → 새 바이너리 순서로 기동, Docker 필요)
./scripts/schema-rehearsal/run.sh all

# 운영 사본으로 (이미 team 스키마인 DB, mysql/postgres 중 하나)
LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
LEGACY_SKIP=1 SQL_DSN='postgres://user:pass@host:5432/db?sslmode=disable' ./scripts/schema-rehearsal/run.sh postgres
```

통과 기준: 스크립트가 `FAIL:` 없이 0으로 끝난다. 로그에 `users.quota uses`나 `failed to migrate`가 없다. 지갑 행이 기동 전후 동일하다. 자세한 설명은 `scripts/schema-rehearsal/README.md`.

이어서 DSN으로 켜는 Go 테스트를 같은 DB에 돌린다.

```bash
TEST_MYSQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' GOWORK=off go test ./model/ -run 'Migration|QuotaColumns|TokenMigration|PrefillGroup' -count=1 -v
TEST_POSTGRES_DSN='postgres://user:pass@host:5432/db?sslmode=disable' GOWORK=off go test ./model/ -run 'Migration|QuotaColumns|TokenMigration|PrefillGroup' -count=1 -v
```

통과 기준: 모두 PASS(DSN이 없으면 SKIP으로 나오므로 SKIP이 아니라 PASS인지 본다).

역방향(롤백) 리허설도 같은 사본으로 한 번 돌린다. 시험 중 문제가 생겨 team으로 되돌아갈 때 team 바이너리가 재기반 바이너리가 마이그레이션한 DB에서 기동하는지를 본다.

```bash
ROLLBACK=1 LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
```

통과 기준: `ROLLBACK:` 줄이 모두 `PASS`이고 `FAIL:` 없이 0으로 끝난다. 복제본에 team 브랜치가 없으면 `LEGACY_REF=origin/team`.

## 2. Redis — Sentinel 장애 조치와 운영 설정

목적: 앱이 Sentinel을 통해 master에 붙고, master가 죽어도 재연결하는지 확인한다(설계 §4.14). 잔액 상주 캐시가 재시작과 장애 조치에서 어떻게 되는지도 본다.

```bash
cd scripts/redis-sentinel
docker compose up -d
docker compose logs sentinel1 | grep -E "monitor|sentinel"     # 세 Sentinel이 서로 발견할 때까지 몇 초
cd ../..
# /etc/hosts에 "127.0.0.1 redis-master redis-replica" 추가 (README 참고)
REDIS_SENTINEL_TEST_ADDRS=localhost:26379,localhost:26380,localhost:26381 REDIS_SENTINEL_TEST_MASTER=mymaster \
  GOWORK=off go test ./common/ -run TestNewRedisClientConnectsThroughSentinel -count=1 -v
docker compose -f scripts/redis-sentinel/docker-compose.yml stop redis-master
sleep 10   # down-after 2초 + 선출. get-master-addr-by-name이 바뀔 때까지 다시 시도
REDIS_SENTINEL_TEST_ADDRS=localhost:26379,localhost:26380,localhost:26381 REDIS_SENTINEL_TEST_MASTER=mymaster \
  GOWORK=off go test ./common/ -run TestNewRedisClientConnectsThroughSentinel -count=1 -v
```

통과 기준: 두 번 모두 PASS(두 번째는 replica가 master로 승격된 뒤).

앱 환경 변수(운영):
- `REDIS_CONN_STRING`은 그대로 필수(사용자·비밀번호·DB 번호를 여기서 읽고 호스트는 무시).
- `REDIS_SENTINEL_ADDRS=host1:26379,host2:26379,host3:26379`, `REDIS_SENTINEL_MASTER_NAME`, 필요하면 `REDIS_SENTINEL_USERNAME`/`REDIS_SENTINEL_PASSWORD`.
- TLS(`rediss://`)를 쓰면 Sentinel 연결에도 같은 TLS가 적용되고 인증서 이름은 `REDIS_CONN_STRING`의 호스트다.

Redis 서버 설정(직접 운영):
- `appendonly yes`, `appendfsync everysec`(재시작 때 잔액 해시 보존, 손실은 마지막 1초).
- `maxmemory`를 넉넉히(사용자 10만 명이어도 수십 MB), 다른 용도와 같이 쓰면 DB 번호 분리.
- 감시: `evicted_keys = 0`, `rdb_last_bgsave_status`·`aof_last_write_status = ok`, 장애 조치 횟수, 앱 로그의 `falling back to database` 빈도.

잔액 상주 확인(운영 앱 기동 뒤): `redis-cli TTL user:<활성 사용자 id>`가 수만 초(기본 86400)인지, `HGET user:<id> FreshUntil`이 있는지. 기본값을 바꾸려면 `QUOTA_CACHE_TTL`(0 이하면 upstream 동작). 자세한 운영 절차는 `docs/manuals/redis-quota-cache.ko.md`.

## 3. 스테이징 기동과 브라우저 스모크

```bash
# 백엔드: 운영 사본 DB + 위 Redis로 기동 (환경 변수는 §6)
GOWORK=off go build -o new-api . && ./new-api --port 3000
# 프런트 e2e (깨끗한 SQLite 서버에 root/12345678로도 가능)
cd web && bun run e2e:install && E2E_ADMIN_USERNAME=root E2E_ADMIN_PASSWORD=12345678 bun run e2e
```

통과 기준: e2e 21개 통과(조직 20 + 한국어 1). 수동 확인: 로그인 → 대시보드 → 조직 대시보드·지갑·로그 화면이 한국어로 나오고 키 문자열이 보이지 않는다. 조직 시나리오는 `docs/manuals/organization-test-guide.md`.

## 4. 결제 provider sandbox

목적: 결제 전 수용량 검증과 콜백 정산이 실제 provider와 맞물리는지 확인한다. 코드 경로는 단위 테스트로만 확인됐다(단계 B 기록 §6 21번).

각 provider에 대해:
1. 관리자 결제 설정에서 sandbox 자격 증명을 넣고 저장. "지우기" 버튼으로 비우면 그 provider가 꺼지는지도 한 번 확인.
2. 개인 지갑 충전 1건과 조직 지갑 충전 1건을 sandbox 결제로 완료.
3. 통과 기준: 주문이 `success`가 되고 잔액이 정확히 한 번 올라간다. 같은 콜백을 다시 보내도 두 번 올라가지 않는다(멱등). 조직 충전은 조직 지갑에만 반영된다.
4. Epay를 켜지 않은 상태에서 충전 화면에 Alipay/WeChat 버튼이 보이지 않아야 한다.

## 5. Claude adaptive thinking과 Gemini

- Anthropic 채널, AWS Bedrock 채널, Vertex 채널 각각에 `thinking: {"type":"adaptive"}`만 보낸 요청(Opus 4.7 이상)을 보내 응답에 thinking 요약이 있는지 확인한다. `display`를 `omitted`로 명시한 요청은 비어 있어야 한다.
- 통과 기준: 세 채널 모두 400 없이 응답하고 요약이 온다. Bedrock·Vertex가 `display` 필드를 거부하면 그 채널은 보고한다(upstream도 같은 필드를 보내므로 새 위험은 아니지만 확인은 필요).
- Gemini 채널에 캐시가 큰 이미지 요청을 보내 로그의 과금 금액이 상한 규칙(이미지 토큰 ≤ prompt − cached − audio)에 맞는지 한 건 확인한다.

## 6. 배포 설정 값

| 변수 | 값 | 없으면 |
|---|---|---|
| `GOOGLE_ANALYTICS_ID` | `G-Z73Y7213LF`(운영 GA4) | 분석이 꺼진다(team은 코드에 고정했음) |
| `CHATBOT_WIDGET_URL` | `https://chatbot.alrouter.ai` | 대시보드 SPA에 챗봇이 없다(랜딩은 템플릿에 고정) |
| `QUOTA_CACHE_TTL` | 기본 86400 | 기본값 사용. 0 이하면 상주 끔 |
| `REDIS_SENTINEL_*` | §2 | Sentinel 없이 단일 주소 |
| `BATCH_UPDATE_ENABLED` / `BATCH_UPDATE_INTERVAL` | 운영값 유지 | — |
| `INTERFACE_LANGUAGES` 등 언어 옵션 | en,ko | 다른 언어 코드는 영어로 열림 |
| `ACCOUNT_PASSWORD_HASH_ALGORITHM` | 시험 기간 `bcrypt` (§9) | argon2id로 저장되어 team으로 롤백하면 그 비밀번호는 검증 불가 |

`.env.example`에 운영값이 주석으로 적혀 있다. GA·GTM 변수 이름은 upstream과 같다.

## 7. 콘텐츠 확인

- 빠른 시작 가이드 PDF 두 개(`web/public/AlRouter Quick Start Guide_v1.0.pdf`, `…_EN_v1.0.pdf`)의 화면 설명이 upstream 새 UI와 맞는지 본다. 맞지 않으면 PDF만 교체한다(파일 이름 유지).
- 가격 페이지의 프로모션 안내 문구가 현재 정책과 맞는지 본다. 맞지 않으면 `web/src/features/pricing/components/pricing-promo-notice-fork.tsx`의 키와 en/ko 번역을 바꾼다.
- 가입 화면의 "계약된 도메인만 가입할 수 있습니다"는 이메일 도메인 제한 옵션이 켜졌을 때만 보인다. 운영에서 그 옵션이 켜져 있는지 확인한다.

## 8. 운영 중 감시할 로그

| 로그 | 뜻 | 정상 |
|---|---|---|
| `user quota cache drift: user=… cache=… db=… diff=…` | 캐시와 DB 잔액이 두 관찰 연속 어긋남 | 0건. 생기면 `redis-quota-cache.ko.md` 6절 절차 |
| `failed to batch update user <id> quota, requeued quota=… used=… requests=…` | 배치 DB 쓰기 실패 → 재큐 | DB 장애 때만 |
| `user cache repair deferred for <n> user(s)` | 복구 목록 처리 중 Redis 오류 | Redis 장애 때만 |
| `flushing batch updates before shutdown` / `queue empty: false` | 종료 시 큐 비움 결과 | `queue empty: true` |
| `invalid rate limit configuration normalized to safe bounds` | rate-limit 환경 변수가 잘못됨 | 0건 |
| `token quota cache reserve unavailable, falling back to database` 등 | Redis 오류 우회 | Redis 장애 때만 |

## 9. 롤백 절차 — develop 시험 중 team으로 되돌아가기

전환 전에 해 둘 것(이 셋이 있어야 롤백이 단순해진다):
1. **비밀번호 해시를 bcrypt로 고정**: develop은 기본으로 argon2id를 쓰고 team은 bcrypt만 검증한다. 시험 기간 환경 변수에 `ACCOUNT_PASSWORD_HASH_ALGORITHM=bcrypt`를 넣는다. 로그인 시 기존 해시를 바꾸지는 않으므로, 이걸 빠뜨리면 시험 기간에 가입하거나 비밀번호를 바꾼 사용자만 롤백 뒤 로그인이 안 된다(그 사용자들은 비밀번호 재설정).
2. **`options` 테이블 덤프**: develop은 upstream의 새 설정 키를 쓰고 일부 키의 값 형식이 다를 수 있다. 전환 직전 덤프를 받아 두고 롤백 때 복원한다(시험 기간에 관리자 화면에서 바꾼 설정은 사라진다).
   ```bash
   mysqldump --single-transaction db options > options-before-develop.sql
   pg_dump -t options db > options-before-develop.sql
   ```
3. **DB 전체 스냅샷**: 최후 수단. 복원하면 시험 기간의 사용량·충전·구독 기록을 잃으므로, 아래 절차가 실패할 때만 쓴다.
4. 시험 기간에는 upstream에만 있는 기능(새 채널 타입, 표현식 과금, 패스키·2단계 인증)을 설정하지 않는다. team이 그 데이터를 무시하거나 채널 목록에서 오류를 낼 수 있다.

롤백 순서:
1. 트래픽을 끊는다(LB에서 제외).
2. develop에 SIGTERM을 보낸다(SIGKILL 금지). `SHUTDOWN_TIMEOUT_SECONDS`(기본 120초) 안에 배치 큐를 DB에 비우고 끝난다. 로그에서 `flushing batch updates before shutdown` 다음 줄이 `queue empty: true`인지 확인한다. `false`면 `failed to batch update user` 줄의 사용자 id를 적어 두고 DB 값과 대조한다.
3. Redis에서 사용자·토큰 캐시를 지운다. team은 develop이 쓴 해시(스키마 버전 2)를 어차피 캐시 미스로 보지만, 지우면 첫 요청의 경로가 team의 평소 재적재 경로로 통일된다.
   ```bash
   redis-cli --scan --pattern 'user:*' | xargs -r redis-cli DEL
   redis-cli --scan --pattern 'token:*' | xargs -r redis-cli DEL
   ```
   (Sentinel이면 현재 master에, DB 번호를 나눠 썼으면 `-n`으로 그 DB에.)
4. `options` 덤프를 복원한다(2번에서 받은 것). 스키마는 되돌리지 않는다: develop의 AutoMigrate는 컬럼·테이블을 추가만 했고 team이 없는 유니크 인덱스를 만들지 않았으며, team은 모르는 컬럼·테이블을 무시한다(§1 역방향 리허설이 이를 확인한다).
5. team 환경 변수로 돌아간다. `SESSION_SECRET`/`CRYPTO_SECRET`은 양쪽이 같아야 세션과 토큰 캐시 키가 이어진다. `QUOTA_CACHE_TTL`은 team 값(600)으로, develop 전용 변수(`REDIS_SENTINEL_*`, `CHATBOT_WIDGET_URL`, `GOOGLE_ANALYTICS_ID`, `ACCOUNT_PASSWORD_HASH_ALGORITHM`)는 team이 무시하므로 두어도 된다.
6. team 바이너리를 띄우고 로그에 `failed to migrate`가 없는지, `/api/status`가 응답하는지 본다. `failed to update option map`은 develop이 쓴 새 키 때문이며 무해하다.
7. 트래픽을 되돌리고 로그인 1건, 과금 요청 1건, 충전 화면 1건을 확인한다.

롤백 뒤 남는 것: develop 기간의 로그·충전·구독·조직 데이터는 team에도 같은 컬럼이 있어 그대로 읽힌다. Toss는 develop 기간에 멈춰 있었으므로 활성 정기결제가 있다면 그 기간의 갱신이 빠진 채 재개된다. 재시도 여부는 team의 Toss 조정 로그로 확인한다.

## 10. 결과 기록

| 항목 | 날짜 | 수행자 | 결과 | 비고 |
|---|---|---|---|---|
| 1 스키마 리허설 MySQL | | | | |
| 1 스키마 리허설 PostgreSQL | | | | |
| 1 DSN 테스트 | | | | |
| 1 역방향(롤백) 리허설 | | | | |
| 2 Sentinel 연결·장애 조치 | | | | |
| 2 Redis AOF·maxmemory 설정 | | | | |
| 3 스테이징 e2e 21개 | | | | |
| 4 결제 sandbox (provider별) | | | | |
| 5 Claude display 3채널 | | | | |
| 5 Gemini 이미지 상한 1건 | | | | |
| 6 배포 설정 값 | | | | |
| 7 PDF·안내문·도메인 옵션 | | | | |
| 9 전환 전 준비(bcrypt·options 덤프·스냅샷) | | | | |
