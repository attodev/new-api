# 사용자 잔액 캐시 상주·Redis Sentinel 연결 기록

브랜치: develop. 베이스: 41684baa2(계획 커밋). 계획: docs/superpowers/plans/2026-10-15-quota-cache-residency.md. 설계: docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md(이 작업으로 "구현 완료"). 운영 문서: docs/manuals/redis-quota-cache.ko.md. 재기반 spec: §2.1 O12, §9 결정 20–21.

단계 E 기록(`docs/2026-10-15-rebase-phase-e.md` §6)이 "단계 F 전에 따로 다룬다"고 넘긴 Redis 쿼터 캐시 TTL 문제를 닫는다. 커밋은 `git log --reverse --format='%h %s' 41684baa2..HEAD` 기준이고, Task 번호는 ledger(`.superpowers/sdd/2026-10-15-quota-cache-residency/progress.md`)와 맞췄다. 설계 커밋 4개(489b94e1a, c8175f3ff, f1e277611, eb54e2a08)는 계획 앞에 있다. upstream 파일의 훅 위치는 모두 `docs/fork-overlays.md` "O12 잔액 캐시 상주·Sentinel" 절에 있다.

| Task | 커밋 | 내용 |
|---|---|---|
| 계획 | 41684baa2 | docs: plan the resident user quota cache and Sentinel connection |
| 1 | 10349e64f | feat(quota): add QUOTA_CACHE_TTL switch for resident user quota cache |
| 2 | fe738e1f7 | fix(quota): keep the user quota hash alive while it is being spent |
| 3 | 4d96c61fc | feat(quota): refresh user info on a fresh-until clock while the balance stays cached |
| 3 | ef1999120 | test(quota): cover auth fence scenarios with a resident user hash |
| 4 | 90d68a9d3 | fix(quota): sync the user quota cache after an invitation quota transfer |
| 5 | dc7fd8d1b | feat(quota): track user quota batches while their rows are being written |
| 5 | b7419e841 | docs: align the plan's Task 6 test with the swap-time in-flight ruling |
| 5 | 52a2afcf3 | test(quota): prove swap-time in-flight marks and per-row clearing; refcount overlapping batches |
| 6 | 67e22ff31 | fix(quota): retry failed batch rows and flush the queue on shutdown |
| 6 | 1aec75403 | fix(quota): serialize batch runs and drain the queue on shutdown |
| 6 | 9ae47afc3 | test(quota): release the blocked batch on test cleanup; fix drifted overlay lines |
| 7 | 3b4f64255 | fix(quota): repair user hashes whose cache sync failed once no batch is pending |
| 7 | 71ff9c17f | fix(quota): take the repair list atomically so notes during a pass are kept |
| 7 | c79d89aab | fix(quota): close the repair-list delete window and drain it on shutdown |
| 8 | 750f3dc0e | feat(quota): log user quota cache drift that no pending batch explains |
| 9 | 6f0ca5e4f | test(quota): allowlist every users.quota write site so merges cannot add unsynced ones |
| 9 | 92c28697c | test(quota): widen the users.quota write detector and document its gaps |
| 9 | 36b61b4a5 | test(quota): drop a duplicated comment fragment in the write-site detector |
| 10 | 986906dc5 | feat(redis): connect through Sentinel when REDIS_SENTINEL_ADDRS is set |
| 10 | df1335380 | fix(redis): carry TLS and connection options into the Sentinel failover client |
| 11 | (이 문서 커밋) | docs: record the resident user quota cache and Sentinel connection work |

규모: fork 파일 10개 556줄(비테스트), fork 테스트 11파일 1,483줄, upstream 파일 훅 17개(Go 8파일)와 설정 문서 주석(`.env.example`, `docker-compose.yml`), Sentinel 재현 구성 `scripts/redis-sentinel/` 3파일.

## 1. 문제와 재현

배치 업데이트(`BATCH_UPDATE_ENABLED=true`)를 켠 develop에서 사용자 지갑 잔액이 실제보다 많이 쓰여 최종 잔액이 음수가 될 수 있었다.

- 사용자 캐시는 Redis 해시 `user:<id>` 하나이고, 인증 정보와 잔액(`Quota`)이 같이 들어 있다. 해시 TTL은 `SYNC_FREQUENCY`(기본 60초)이며 처음 쓸 때만 설정되고 차감·정산 스크립트는 TTL을 건드리지 않았다. 계속 쓰이는 사용자도 60초마다 해시가 사라졌다.
- 배치 모드에서 차감은 Redis에 먼저, DB에는 `BATCH_UPDATE_INTERVAL`(기본 5초)마다 반영된다(운영 AWS에서 배치 DB 쓰기 3~5초). 해시가 사라진 순간 DB에 아직 없는 차감분이 캐시에서 잊힌다.

miniredis 재현(잔액 1000, 100 차감, 배치 반영 전 해시 만료, 다시 100 차감):

| 단계 | DB | 캐시 |
|---|---|---|
| 해시 재생성과 배치 반영 직후 | 800 | 900 |
| 캐시 잔액 900을 모두 쓴 뒤 배치 반영 | **-100** | 0 |

API 키 캐시(`token:<hmac>`)도 같은 결과(DB 800, 캐시 900)였다. 해시가 사라지는 가장 흔한 원인은 메모리가 아니라 60초 TTL이다. 이 재현은 `model/user_quota_residency_regression_test.go`의 `TestResidentQuotaCacheSurvivesBatchWindow`로 회귀 테스트가 되었다(해시가 `SYNC_FREQUENCY`를 넘겨도 남고, 배치 반영 뒤 캐시와 DB가 같다).

## 2. 설계 요약

### 2.1 두 시계 (설계 §4.1–4.5)

| 시계 | 위치 | 기본값 | 연장되는 때 |
|---|---|---|---|
| 잔액 TTL | Redis EXPIRE | `QUOTA_CACHE_TTL` 86400초 | 잔액이 바뀔 때만(사전 차감·정산 성공 경로의 `EXPIRE`, 새로 채운 해시) |
| 정보 신선 기한 | 해시 필드 `FreshUntil` | 쓴 시각 + `SYNC_FREQUENCY` | 사용자 정보를 DB에서 다시 읽어 쓸 때 |

- 읽기(`cacheGetUserBase`)에서 `FreshUntil`이 없거나 지났으면 miss와 같은 DB 경로로 사용자 정보를 다시 쓰고, 잔액은 덮어쓰지 않으며(`Quota`가 있으면 쓰지 않는 upstream 규칙), 잔액 TTL도 연장하지 않는다. 호출자에게는 해시를 다시 읽어 캐시 잔액을 돌려준다(`userBaseAfterPopulate`).
- 인증 fence·세션 캐시·`userCacheTTLSeconds()`는 바꾸지 않았다. 인증 정보가 쓰일 수 있는 기간이 여전히 `SYNC_FREQUENCY` 이내라 fence 수명 조건이 그대로 성립한다. 잔액 TTL은 `SYNC_FREQUENCY`보다 짧아질 수 없다.
- `QUOTA_CACHE_TTL <= 0`이면 상주를 끄고 upstream 동작(스크립트·`writeUserCache` 그대로, `FreshUntil` 확인 안 함)으로 돌아간다.
- 추천 적립 이전(`TransferAffQuotaToQuota`)은 커밋 뒤 캐시에 이전 금액을 더한다(상주 전에는 60초 뒤 저절로 맞춰지던 경로).

### 2.2 어긋남 감지 규칙 (설계 §4.7)

정보를 다시 읽을 때(`userBaseAfterPopulate`에서만) 캐시 잔액과 DB 잔액을 비교한다. 이 서버에 그 사용자의 배치 대기분이 있거나 반영 중이면 건너뛴다. 사용자별로 (캐시 잔액, 차이)를 한 쌍만 기억하고, 다음 관찰에서 둘 다 그대로이고 차이가 0이 아니면 `user quota cache drift: user=… cache=… db=… diff=…`를 `SysError`로 남긴다. 다른 서버의 대기분은 두 관찰 사이(최소 `SYNC_FREQUENCY`)에 DB에 반영되어 차이가 바뀌므로 걸리지 않는다(HA 가정). 값은 고치지 않는다.

### 2.3 DB 반영 누락 보강 세 가지 (설계 §4.11–4.12)

0. **반영 중 표시(전제):** `batchUpdate`가 큐를 꺼내는 순간(기존 스토어 락 안) 사용자 잔액 대기분을 반영 중 목록(refcount)에 옮기고, 각 사용자 행을 쓴 직후 그 사용자만 푼다. 감지·복구가 "대기분 없음"을 잘못 보지 않게 한다.
1. **종료 시 배치 비우기:** `main.go`의 `srv.Shutdown` 뒤 `FlushBatchUpdates()`가 큐가 빌 때까지 최대 3번 `batchUpdate()`를 돈다. 배치 실행은 `batchRunMu`로 직렬화되어 쓰는 중인 티커 배치를 기다린다. 이어서 복구 목록 패스를 한 번 돈다. upstream은 재배포마다 최대 배치 간격만큼의 차감을 버렸다.
2. **배치 반영 실패 재시도:** 사용자 행 UPDATE가 실패하면 세 값(quota, used_quota, request_count)을 큐에 다시 넣고 그 뒤에 반영 중 표시를 푼다. 재시도 횟수 제한은 없다("차감을 버리지 않는다").
3. **캐시 반영 실패 복구 목록:** 커밋 뒤 캐시 반영이 실패했거나(`cacheIncrUserQuota` 오류, 보상 실패) Redis 오류로 DB에서만 사전 차감한 사용자를 프로세스 안 목록에 적는다. `max(BATCH_UPDATE_INTERVAL,1)`초마다(배치 모드와 무관) 목록을 통째로 꺼내, 대기분이 없는 사용자의 해시를 지우고 대기 중·삭제 실패면 다시 넣는다. 삭제 직후 대기분이 생겼으면 다시 넣고, Redis가 아직 죽어 있으면 남은 id를 되돌리고 그 패스를 끝낸다.

### 2.4 Redis Sentinel (설계 §4.13–4.14)

`common/redis.go`의 `redis.NewClient(opt)` 한 줄을 `newRedisClient(opt)`로 바꿨다. `REDIS_SENTINEL_ADDRS`가 있으면 `redis.NewFailoverClient`, 없으면 기존 단일 노드 클라이언트다. 둘 다 `*redis.Client`라 나머지 코드는 그대로다. `REDIS_CONN_STRING`은 여전히 필수이며 Sentinel 모드에서는 호스트만 무시하고 `redis.ParseURL`이 정하는 모든 연결 옵션(인증, DB, TLS, 재시도·백오프, 타임아웃, 풀 설정)을 넘긴다. Sentinel 자체 인증은 `REDIS_SENTINEL_USERNAME`·`REDIS_SENTINEL_PASSWORD`, master 이름은 `REDIS_SENTINEL_MASTER_NAME`(기본 `mymaster`). 클러스터 모드는 지원하지 않는다. 운영 전제(AOF `everysec`, 넉넉한 `maxmemory`, 감시 지표)는 운영 문서 4절에 있다.

## 3. 훅 목록

`grep -rn "fork: O12" --include='*.go' .`의 표식 29개 중 upstream 파일의 훅 17개(나머지 12개는 fork 파일 안). 모두 `docs/fork-overlays.md` O12 표의 같은 줄에 있다(5절 검증).

| 파일:줄 | 훅 | 로직 |
|---|---|---|
| common/init.go:113 | `QuotaCacheTTL = GetEnvOrDefault("QUOTA_CACHE_TTL", 86400)` | common/quota_cache_ttl_fork.go, model/user_quota_residency_fork.go |
| common/redis.go:40 | `RDB = newRedisClient(opt)` | common/redis_sentinel_fork.go |
| main.go:172 | `model.StartUserCacheSyncRepair()` | model/user_cache_repair_fork.go |
| main.go:250 | `model.FlushBatchUpdates()` | model/batch_shutdown_fork.go |
| model/quota_reserve.go:82 | `result, err := evalUserQuotaReserve(userID, amount)` | model/user_quota_scripts_fork.go |
| model/quota_reserve.go:87 | `result, err := evalUserQuotaDelta(userID, delta)` | model/user_quota_scripts_fork.go |
| model/quota_reserve.go:180 | `noteUserCacheSyncFailure(id, err)` (DB 우회 분기 첫 줄) | model/user_cache_repair_fork.go |
| model/quota_reserve.go:193 | `noteUserCacheSyncFailure(id, compensateErr)` (보상 실패 블록) | model/user_cache_repair_fork.go |
| model/user_cache.go:27 | `UserBase.FreshUntil` 필드 | model/user_cache_fresh_fork.go |
| model/user_cache.go:115 | `return userBaseAfterPopulate(user)` | model/user_cache_fresh_fork.go, model/user_quota_drift_fork.go |
| model/user_cache.go:138 | `if userInfoStale(&userCache) {` (gofmt로 3줄) | model/user_cache_fresh_fork.go |
| model/user_cache.go:152 | `return noteUserCacheSyncFailure(userId, err)` | model/user_cache_repair_fork.go |
| model/user_auth_cache.go:49 | `if quotaCacheResidencyEnabled() {` → `writeUserCacheResident` (3줄) | model/user_cache_fresh_fork.go |
| model/user.go:632 | `return commitAffTransfer(tx, user.Id, quota)` | model/aff_transfer_cache_fork.go |
| model/utils.go:65 | `defer lockBatchRun()()` | model/batch_shutdown_fork.go |
| model/utils.go:86 | `stores[i] = takeBatchStoreLocked(i)` | model/batch_inflight_fork.go |
| model/utils.go:123 | `flushUserQuotaRow(key, userQuotaStore, usedQuotaStore[key], requestCountStore[key])` | model/batch_inflight_fork.go |

설정 문서: `.env.example:47-52, 55-56`, `docker-compose.yml:35-38, 56`(주석 줄만, overlay 표에 이번에 행을 더함).

upstream 복사본(병합 때 원본 변경을 옮겨야 함, overlay 메모와 코드 주석 "upstream 복사본, 병합 때 원본 변경 반영"):
- `userQuotaReserveResidentScript`·`userQuotaDeltaResidentScript`(model/user_quota_scripts_fork.go) ← `model/quota_reserve.go:19-39` + 성공 경로 `EXPIRE`.
- `writeUserCacheResident`(model/user_cache_fresh_fork.go) ← `writeUserCache`(`model/user_auth_cache.go:45-99`, 스크립트 :61-86) + `FreshUntil`, `TTL < 0`일 때만 `EXPIRE`, 잔액 TTL.
- `flushUserQuotaRow`(model/batch_inflight_fork.go) ← `updateUserQuotaUsedQuotaAndRequestCount`(`model/user.go:1488-1503`) + 오류 시 재큐잉.

upstream 훅 없는 fork 장치: 어긋남 감지(`model/user_quota_drift_fork.go`, fork 파일에서만 호출), `users.quota` 쓰기 위치 허용 목록 테스트(`model/user_quota_write_sites_fork_test.go`, 22항목 = 설계 §5 표).

## 4. 결정 사항

### 4.1 설계 결정 (설계 §3)

1. 사용자 잔액 캐시를 Redis에 상주시킨다. 잔액 TTL 기본 24시간.
2. 잔액 외 사용자 정보는 지금 주기(`SYNC_FREQUENCY`)로 다시 읽되, 요청이 들어온 순간에만 확인한다(백그라운드 작업 없음).
3. 요청을 막는 방식(team `10ade7045`)과 캐시 miss 때 DB 우회 차감은 쓰지 않는다.
4. API 키 캐시는 바꾸지 않는다.
5. 조직 기능의 `InvalidateUserCache` 경로는 바꾸지 않는다.
6. 캐시·DB 잔액 어긋남은 자동 보정하지 않고 감지해 로그만 남긴다.
7. 앱 서버 HA(여러 대가 같은 Redis)를 가정하고, 어긋남 판정은 그 가정에서 성립하는 규칙을 쓴다.
8. `team` 브랜치는 범위 밖이다.

### 4.2 계획 결정 (계획 하단 1–10)

1. 상주는 사용자 해시에만. 2. upstream Lua 스크립트는 fork 파일에 복사해 고치고 훅은 한 줄(대가: 병합 때 복사본 갱신). 3. 잔액 TTL ≥ `SYNC_FREQUENCY`. 4. 스위치를 끄면 새 쓰기부터 upstream 동작, 남은 24시간 해시는 운영자가 `user:*` 삭제(운영 문서 2.1절). 5. 어긋남 감지는 `userBaseAfterPopulate`에서만. 6. 복구 목록은 배치 모드와 무관하게 `max(BATCH_UPDATE_INTERVAL,1)`초마다. 7. 재시도는 세 열을 함께. 8. Sentinel 모드에서도 `REDIS_CONN_STRING` 필수. 9. 실제 Sentinel 장애 조치 확인은 운영자 작업. 10. `team` 브랜치는 건드리지 않는다.

### 4.3 실행 중 ruling (ledger `Ruling:` 줄 14개, 오래된 순; 대가 = 틀렸을 때 비용)

1. (사전) upstream Lua 스크립트 두 개·`writeUserCache` 스크립트와 `updateUserQuotaUsedQuotaAndRequestCount`의 fork 복사본을 계획이 정한 중복으로 받아들인다. overlay 규칙이 upstream 파일의 여러 줄 수정을 금하기 때문이고, 복사본마다 "병합 때 원본 반영" 메모를 단다. 대가: 병합된 upstream 스크립트 변경이 복사본에 조용히 빠질 수 있다(병합마다 overlay 검토).
2. (사전) 상주는 사용자 해시에만, 토큰 해시는 그대로(설계 결정 4). 대가: 키 한도만 있는 요청은 배치 한 주기만큼 키 한도를 넘을 수 있다.
3. (Task 3) upstream fence 테스트는 고치지 않는다(상주를 끈 기본 상태로 돌고, 그것도 지원하는 모드다). 같은 두 시나리오를 상주를 켠 fork 테스트로 더하고 `FreshUntil`은 HSET으로 늙힌다 — upstream 테스트는 §4.5가 일부러 대신한 해시 TTL 만료를 전제한다. 대가: 상주 상태의 fence 회귀는 새 fork 테스트로만 잡힌다.
4. (Task 3) `FreshUntil`은 앱 노드 시계를 쓴다. 노드 사이 시계 차이가 max(`SYNC_FREQUENCY`,60)초를 넘으면 §4.5 여유가 줄어든다는 점을 설계 §6에 적는다(NTP면 ms). Redis `TIME`은 읽을 때마다 왕복이 하나 더 든다. 대가: 시계가 크게 틀어진 노드는 그 차이만큼 오래된 인증 정보를 쓸 수 있다.
5. (Task 3) 커밋 트레일러는 Claude Fable 5.1(하네스 귀속)로 하고 ce1c6cf1a를 수정 라운드에서 고친다. 대가: 없음.
6. (Task 5) 반영 중 표시는 계획의 "교체 뒤 `markUserQuotaInFlight(userIDs)`" 대신 교체 순간 스토어 락 안에서 한다(`stores[i] = batchUpdateStores[i]` → `takeBatchStoreLocked(i)`). 계획 위치는 사용자가 큐에도 반영 중에도 없는 락 밖 틈을 남긴다. 대가: used/request 차이만 있는 사용자는 반영 중 표시가 없다(잔액 차이만 중요하므로 맞는 동작).
7. (Task 5) 반영 중 맵을 refcount(`map[int]int`)로 바꾼다. Task 6의 종료 flush가 티커 배치와 겹칠 수 있어, 집합이면 한 배치의 해제가 다른 배치의 표시를 지운다. 대가: 없음(0에서 삭제하므로 pending 의미는 같다).
8. (Task 5) Task 6 브리프·계획을 고친다: 반영 중 준비는 지운 `markUserQuotaInFlight` 대신 락 안에서 `addNewRecord` + `takeBatchStoreLocked`. 대가: 없음.
9. (Task 6) 해제 비대칭(used/request 스토어에만 있는 id가 받지 않은 감소를 받음, 종료 flush가 티커와 겹치면 문제)을 고친다: `flushUserQuotaRow`에 사용자 잔액 스토어를 넘겨 꺼낸 id만 해제한다. 대가: `utils.go` 사용자 루프 훅 문구가 한 번 더 바뀐다.
10. (Task 6) 배치 실행을 fork 뮤텍스로 직렬화(`batchUpdate` 첫 줄 훅 하나 더)하고, 종료 flush는 스토어가 빌 때까지 반복(최대 3번)한다. 쓰는 중인 배치 사이에 프로세스가 끝나는 창을 닫는다. 재시도는 "차감을 버리지 않는다"에 따라 무제한. 대가: 종료 때 DB 쓰기가 멈추면 배치 자체의 타임아웃까지 프로세스가 남는다(`SHUTDOWN_TIMEOUT_SECONDS`가 이미 스트림을 120초 기다리므로 수용).
11. (Task 7) 복구 목록은 원자적으로 꺼내고(스냅숏 + 비우기) 대기 중·오류면 다시 넣는다. 브리프 그대로의 루프에 있던 "패스 중 기록이 사라지는 창"을 닫는다. 대가: id가 다시 들어가 한 번 더 시도될 수 있다.
12. (Task 7) 리뷰 minor m1–m3(삭제 뒤 대기분 재확인, Redis 오류면 그 패스 중단, 종료 때 복구 패스)를 지금 수정 라운드에 넣는다. m1은 상주 상태의 정확성 구멍이다. 대가: 복구 대상 사용자마다 재추가·재시도 한 번 더.
13. (Task 9) 검출기를 싼 세 패턴(구조체 리터럴 `Updates(User{Quota:})`, 상수 열 이름, `Exec`/`Raw` SQL)과 재귀 탐색으로 넓히고, 남는 휴리스틱 공백(이름에 `user` 없는 변수의 `Save`, `Model(인자).Updates(맵)`, `Select("quota").Updates`)은 설계 §5에 적는다. 대가: 적어 둔 공백 패턴을 더하는 병합은 여전히 통과한다. 허용 목록 테스트는 그물이지 증명이 아니다.
14. (Task 10) 계획 결함(브리프는 네 필드만 복사): `ParseURL`이 정할 수 있는 모든 연결 옵션(TLSConfig, MaxRetries, 백오프, Dial/Read/Write/Pool/Idle 타임아웃, MinIdleConns, MaxConnAge, PoolFIFO, IdleCheckFrequency)을 복사하고 `REDIS_SENTINEL_USERNAME`을 더한다. TLS가 Sentinel 연결에도 적용되고 `ServerName`은 conn-string 호스트라는 점을 문서화한다. 대가: TLS를 쓰지 않는 배포에는 없음.

### 4.4 이 기록 단계(Task 11)에서 정한 것

- **overlay 번호:** 재기반 spec §2.1은 O12를 OpenWebUI에 배정해 두었지만, 이 작업의 코드 표식 29개와 overlay 절이 이미 `fork: O12`를 쓴다. 아직 코드가 없는 OpenWebUI를 O13으로 옮겼다(spec §2.1·§4.10·§8 단계 F, 결정 21). 대가: 옛 문서·대화에서 "O12 = OpenWebUI"로 읽은 사람이 혼동할 수 있다.
- **설계 문안 정리:** §4.4 마지막 항목을 실제 동작("해시는 있는데 `Quota`가 없으면 `Quota`만 쓰고 TTL은 그대로 둔다")으로, §4.13 연결 항목을 "upstream은 단일 노드, 이 작업이 Sentinel을 더함"으로 고쳤고 §6에 `SYNC_FREQUENCY=1` 항목을 더했다(Task 3 minor m3·m4).

## 5. 남는 한계

### 5.1 설계 §6

- **해시가 지워지는 경우:** Redis 재시작·장애 조치, upstream·조직 기능의 해시 삭제, 캐시 스키마 버전 변경. 그 순간의 배치 대기분만큼 초과 사용 가능(작고 드묾).
- **API 키 잔액:** 키 캐시는 그대로라 잔액 한도가 있는 키는 60초 만료 때 대기분만큼 초과될 수 있다. 지갑 차감 요청은 지갑이 지켜지지만, 구독 플랜처럼 지갑을 차감하지 않는 요청은 키 한도 초과가 남는다.
- **직접 DB 수정:** 운영 절차(`DEL user:<id>`)를 따르지 않으면 어긋남이 남는다. 감지 로그로 드러난다.
- **배치 반영 실패가 길어지는 경우:** 재시도가 계속 실패하면(DB 장애) DB가 뒤처진다. 캐시가 맞는 쪽이고 감지 로그가 가리킨다. DB가 회복되면 재시도가 반영한다.
- **배치 비활성 환경:** 원래 문제가 없다. 상주는 DB 읽기를 조금 줄일 뿐이다.
- **비정상 종료(OOM, SIGKILL):** 배치 큐와 복구 목록이 사라진다. 캐시가 맞고 DB가 대기분만큼 뒤처지며 24시간 뒤 해시가 사라질 때 DB 값으로 돌아간다(지금 develop도 60초 뒤 같은 손실). 사용 중인 동안은 감지 로그에 걸리지 않는다.
- **병합으로 들어오는 새 쓰기 경로:** 허용 목록 테스트가 병합 직후 드러낸다(검출 공백은 5.2).
- **노드 사이 시계 차이:** 4.3의 ruling 4.
- **`SYNC_FREQUENCY=1`:** 다시 쓴 뒤 다시 읽는 사이 초가 바뀌면 그 요청은 DB 스냅숏을 받는다(안전한 쪽의 저하, Task 3 m4).

### 5.2 Task 9 허용 목록 검출기에서 발견된 것 (설계 §5)

검출기는 `model/`·`service/`·`controller/`를 `go/ast`로 훑는 함수 단위 휴리스틱이다. 잡지 못하는 것: 이름에 `user`가 없는 `User` 변수의 `Save`, `Model(인자).Updates(맵)`(인자가 리터럴이 아님), 패키지 수준 `var f = func…` 본문, 허용된 함수 안에 나중에 더해지는 두 번째 쓰기, 세 패키지 밖의 쓰기. 열 이름이 변수인 `Update(col, v)`는 `col`이 `"quota"` 상수·변수일 때만 잡는다. 검출 결과 22곳 모두 캐시를 맞추거나(차이 더함·해시 삭제) `users.quota`와 무관한 것으로 확인했다(독립 재조사에서 누락 없음).

### 5.3 실행 중 미뤄 둔 minor (ledger `minor (deferred` 줄 5개)

- (Task 3) m1: `userBaseAfterPopulate`는 Redis 채우기 오류 뒤에도 해시를 다시 읽는다(장애 중 세 번째로 실패하는 왕복). upstream `GetUserCache`에서 채우기 오류가 블록 안 변수라 넘기려면 두 번째 훅이 필요하다 — 남김. m2: 스위치를 끄면 24시간 해시가 신선 기한 없이 남는다 → 운영 문서 2.1절에 삭제 절차를 적었다. m3: 설계 §4.4 마지막 항목 문구 → 이번에 고침. m4: §6 `SYNC_FREQUENCY=1` → 이번에 더함.
- (Task 6) `userQuotaInFlight`의 낡은 주석(배치가 이제 직렬화되어 refcount는 안전망) → Task 7에서 고침.
- (Task 7) `model/quota_reserve.go:193` 보상 실패 훅은 테스트가 없다(도달이 어려움). "삭제 전 사전 차감 / 재확인 뒤 큐 넣기" 경쟁은 수용했다(그 차이가 반영되기 전에 해시는 DB에서 다시 채워진다).
- (Task 9) `sqlMentionsQuotaUpdate`는 첫 문자열 리터럴만 읽는다(이어 붙이거나 상수인 SQL은 놓침). `m[quotaCol]=` 대입은 인식하지 않는다. 다른 파일의 열 이름 상수를 찾는 경로는 합성 테스트가 확인하지 않는다.
- (Task 10) Sentinel 모드에서 디버그 로그 `Redis connected to %s`가 무시된 호스트를 찍는다(두 번째 upstream 훅이 필요해 남김). Sentinel compose는 실행해 보지 못했다(Docker 없음).

리뷰에서 받아들인 그 밖의 minor: 재시도 속도 제한 없음, `srv.Shutdown`이 시간 초과로 끝난 뒤의 flush는 best-effort(Task 6). 감지 보고 함수를 뮤텍스 안에서 부름, 감지 기억은 시간으로는 무한하지만 사용자 수로 제한됨(Task 8).

### 5.4 이 단계 검증에서 발견된 것

- **상주를 켜면 실패하는 upstream 테스트 2개:** 테스트 기본값은 상주 꺼짐(`common.QuotaCacheTTL` 0, `TestMain`이 `InitEnv`를 부르지 않음)이다. `init()`에서 `common.QuotaCacheTTL = 86400`을 넣는 임시 테스트 파일(커밋하지 않고 지움)을 model·service·controller·middleware에 두고 돌린 결과 실패는 `model.TestUserAuthFenceRollbackExpiresAndRecovers`("1m2s is not greater than 24h0m0s" — 해시 TTL이 fence TTL보다 짧다고 가정)와 `service.TestIndependentRedisAuthVersionAdvanceConvergesAfterCacheTTL`(miniredis `FastForward(3s)`로 해시 만료를 기대, 상주에서는 `FreshUntil`이 실제 시계라 아직 신선 → `login session is revoked`) 두 개뿐이다. 두 번 돌려 같았다. 같은 시나리오는 상주를 켠 fork 테스트(`model/user_cache_fresh_fence_fork_test.go`의 `TestResidentHashRecoversAfterRolledBackFenceExpires`, `TestResidentHashConvergesAfterIndependentNodeAdvancesAuthVersion`, `TestResidentHashRejectedByCommittedFloorConvergesFromDB`)가 다룬다(ruling 3). controller·middleware·pkg/wsmanager·router는 상주를 켜도 통과했다.
- **기존 테스트 전용 data race:** `go test -race ./model/ -run 'Quota|Batch|Drift|Repair|Fresh|Aff|Flush|InFlight' -count=3`이 upstream 테스트 `TestTokenCacheInitPreservesLiveQuotaAndFenceBlocksStaleSnapshot`에서 race로 실패한다. upstream 테스트 `TestBatchUpdateAccumulatesTwoMaximumRequestCharges`의 `DecreaseUserQuota`가 띄운 fire-and-forget gopool 고루틴이 `common.RedisEnabled`를 읽고(`model/user_cache.go:148`, upstream 줄), 다음 테스트의 `useUserCacheMiniRedis`(`model/user_cache_auth_version_test.go:23`)가 같은 변수를 쓴다. 두 테스트 파일 모두 upstream과 같다. 이 계획 전 베이스 41684baa2에서도 2/2 재현되고(qn/main 718a4e7f9에서는 2/2 재현 안 됨, 그 사이 fork 테스트가 실행 순서를 바꾼 것으로 보임), model 패키지 전체를 `-race`로 돌리면 통과한다. 이 테스트 하나를 `-skip`하면 같은 패턴이 55개 테스트 × 3 = 165 PASS, race 0이다. 제품 코드가 아니라 테스트 사이 전역 변수 공유 문제라 이 작업에서는 고치지 않았다.

## 6. 검증과 운영자 작업

### 6.1 전체 검증 (Task 11, 2026-10-15)

HEAD df1335380(작업 트리 깨끗)에서 문서 수정 전에 실행했다.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `GOWORK=off go test ./...`: 종료 코드 0, `ok` 44 / `no test files` 39 / `grep -E "^(FAIL|---)"` 0줄. `-count=1`로 다시 돌려도 같았다(단계 E 끝과 같은 패키지 수).
- `GOWORK=off go test -race ./model/ -run 'Quota|Batch|Drift|Repair|Fresh|Aff|Flush|InFlight' -count=3`: 56개 테스트 × 3 중 167 PASS, 1 FAIL(위 5.4의 기존 race, 4번 돌려 4번 재현). 그 테스트를 `-skip`하면 `ok`(165 PASS, race 0). `go test -race ./model/` 전체는 `ok`.
- Sentinel: `go test ./common/ -run 'Sentinel|Failover' -v` → 단위 테스트 3개 PASS, 실제 연결 테스트 `TestNewRedisClientConnectsThroughSentinel`은 `REDIS_SENTINEL_TEST_ADDRS`가 없어 SKIP.
- 프런트: 바뀐 파일이 없어 `cd web && bun run typecheck`(`tsgo -b`)만 — 종료 코드 0.

### 6.2 upstream 병합 리허설 (2026-10-15)

`qn/main`은 **718a4e7f9**(단계 E 리허설과 같음), merge-base 7de10f2c6. HEAD df1335380, 작업 트리가 깨끗할 때 `git merge --no-commit --no-ff qn/main`(종료 코드 1).

충돌 **7건**, 단계 E와 같은 집합이고 모두 overlay 표에 있다:

| 파일 | 종류 | 분류 | 리허설 트리에서의 해소 |
|---|---|---|---|
| `go.sum` | content | O4 `go.mod, go.sum` 행(moejs checksum 인접 충돌) | `git checkout --theirs go.sum` + `GOWORK=off go mod tidy` → `go.sum` 2줄 차이, `GOWORK=off go build ./...` 성공 |
| `web/src/i18n/locales/{fr,ja,ru,vi,zh-TW,zh}.json` | modify/delete(6) | "삭제한 upstream 파일" | `git rm` 6개로 모두 풀림 |

- 병합이 만든 변경: 35파일, 1,752 insertions, 266 deletions(단계 E와 같음). upstream 쪽 변경은 O12 훅이 있는 파일(main.go, common/init.go, common/redis.go, model/{user,user_cache,user_auth_cache,utils,quota_reserve}.go, `.env.example`, `docker-compose.yml`)을 하나도 건드리지 않는다. 병합 트리의 `fork: O12` 표식은 29개 그대로.
- 해소한 병합 트리에서 `GOWORK=off go vet ./model/ ./common/ .` 통과, `GOWORK=off go test ./model/ ./common/ -count=1` 둘 다 `ok`.
- `git merge --abort` 종료 코드 0, `MERGE_HEAD` 없음, `git status --short` 0줄, HEAD df1335380 그대로, locale 디렉터리는 en·ko만.

### 6.3 overlay 표 검증

- upstream 파일의 O12 훅 17개(`grep -rn "fork: O12" --include='*.go' .` 29줄 중 fork 파일 12줄 제외)를 스크립트로 O12 표와 대조: 17행 모두 적힌 줄에 표식이 있고, 표에 없는 훅 0, 표식이 없는 줄을 가리키는 행 0.
- O1 `main.go:50-54`, `main.go:215-216`(Task 7이 main.go 훅 때문에 214-215에서 옮김)이 여전히 맞다. 이 계획이 바꾼 upstream 파일을 가리키는 다른 행(`model/user.go:97-98,105-106`, `:101-102`, `:118-119`, `:256`, `:541`, O11 `:618`, `model/main.go:383`, O9 `.env.example:12`)도 맞다.
- 고친 것: O12 스크립트 복사본 메모 `model/quota_reserve.go:19-38` → **19-39**(delta 스크립트 끝 줄), O9 `docker-compose.yml:39` → **43**(Task 10의 Sentinel 주석 4줄이 밀어냄). 표에 없던 설정 문서 변경(`.env.example:47-52, 55-56`, `docker-compose.yml:35-38, 56`)에 O12 행 두 개를 더했다.

### 6.4 운영자 작업 (이 저장소에서 하지 못한 것)

- **Sentinel 실연결·장애 조치:** Docker가 없어 `scripts/redis-sentinel/`의 compose와 `TestNewRedisClientConnectsThroughSentinel`을 돌리지 못했다. 절차는 `scripts/redis-sentinel/README.md`. 운영 Sentinel에서 `REDIS_SENTINEL_TEST_ADDRS`로 연결 테스트를 돌리고, master를 내려 장애 조치 뒤 앱 로그(DB 우회 차감 → 복구 목록 → 정상)를 확인한다. TLS를 쓰면 인증서가 `REDIS_CONN_STRING` 호스트 이름을 받는지 확인한다.
- **Redis 설정:** master와 replica 모두 `appendonly yes`, `appendfsync everysec`, 넉넉한 `maxmemory`, 감시 지표(`evicted_keys` 0, `rdb_last_bgsave_status`·`aof_last_write_status` ok, 장애 조치 횟수, "falling back to database" 빈도) — 운영 문서 4절.
- **배포 설정:** 모든 앱 서버에 NTP, 같은 `QUOTA_CACHE_TTL`, 종료 신호 뒤 충분한 대기(강제 종료 금지), 직접 DB 수정 뒤 `DEL user:<id>` 절차 공유.
- **MySQL/PostgreSQL 미실행:** 컨테이너 런타임이 없다. 새 SQL은 없다 — `flushUserQuotaRow`는 upstream과 같은 GORM `Updates(map + gorm.Expr)`이고 나머지는 Redis와 기존 GORM 호출이다. `TestUserQuotaColumnsSurviveAutoMigrate`의 mysql·postgres 하위 테스트는 SKIP.
- **운영 부하 확인:** 배치 DB 쓰기 3~5초 환경에서 반영 중 표시·재시도·종료 flush가 실제로 어떻게 보이는지(종료 로그 `queue empty: true`)는 스테이징에서 확인한다.

## 7. 다음 단계

- **단계 F: O13 OpenWebUI**(재기반 spec §4.10, 처음 문안의 O12). OAuth2/OIDC 제공자, relay `TokenAuth`의 서비스 키 → 사용자 토큰 치환(scoped access token 경로와 분리), 사용자 링크 관리, 프런트 28파일 이식. `OpenWebUI` 브랜치는 그때까지 그대로 둔다. 통합 테스트 스택 `deploy/openwebui-test` 통과가 산출물이다.
- 단계 F 계획 전에 이 기록 5.4의 테스트 race(upstream 테스트 사이 `common.RedisEnabled` 공유)를 고칠지 정한다(테스트만의 문제, fork 테스트 헬퍼에서 비동기 캐시 고루틴을 기다리는 방식 등).
- 다음 upstream 동기화 때: `fork: O12` 복사본 세 곳(3절)을 upstream 원본 변경과 비교하고, `TestUserQuotaWriteSitesAreAllowlisted`가 실패하면 새 쓰기 위치가 캐시를 맞추는지 확인한 뒤 허용 목록과 설계 §5 표를 같이 고친다.
- 단계 G(스테이징 → `team` 교체) 전에 6.4의 운영자 작업을 끝낸다. team 브랜치에는 이 작업을 옮기지 않는다(설계 결정 8).
- 단계 C·D·E에서 넘어온 미해결 항목은 단계 E 기록 §6 그대로다.
