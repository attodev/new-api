# Redis 사용자 잔액 캐시 운영 문서

## 1. 개요

이 문서는 develop(upstream 재기반 브랜치)의 사용자 잔액 Redis 캐시를 운영하는 방법을 설명한다. 대상은 서버를 배포하고 Redis와 DB를 관리하는 운영자다.

설계는 `docs/superpowers/specs/2026-10-15-quota-cache-residency-design.md`, 구현 기록은 `docs/2026-10-15-quota-cache-residency.md`에 있다. 이 문서의 절 번호 "설계 §n"은 그 설계 문서를 가리킨다.

요약:

- 사용자 캐시는 Redis 해시 `user:<사용자 ID>` 하나다. 인증 정보(그룹, 상태, 역할, 인증 버전 등)와 지갑 잔액(`Quota`)이 함께 들어 있다.
- 배치 업데이트(`BATCH_UPDATE_ENABLED=true`)를 켜면 차감은 Redis에 먼저 반영되고 DB에는 `BATCH_UPDATE_INTERVAL`(기본 5초)마다 반영된다. 그 사이에 해시가 사라지면 DB에 아직 없는 차감분이 잊혀 초과 사용이 생긴다(설계 §2).
- 그래서 잔액 해시를 Redis에 상주시킨다. 잔액 TTL은 기본 24시간이고 잔액이 바뀔 때마다 연장된다. 사용자 정보는 지금처럼 `SYNC_FREQUENCY`(기본 60초)마다 DB에서 다시 읽는다(해시 필드 `FreshUntil`).
- 잔액이 최대 24시간 Redis에만 있으므로 Redis의 영속성·메모리·장애 조치 구성이 손실 폭을 정한다(4절).

## 2. 설정과 끄는 법 (설계 §4.8)

| 환경 변수 | 기본값 | 의미 |
|---|---|---|
| `QUOTA_CACHE_TTL` | `86400` | 잔액 해시의 Redis TTL(초). 사전 차감·정산·충전처럼 잔액이 바뀔 때마다 이 값으로 다시 설정된다. |
| `SYNC_FREQUENCY` | `60` | 사용자 정보를 DB에서 다시 읽는 주기(초). 해시 필드 `FreshUntil` = 쓴 시각 + 이 값. |
| `BATCH_UPDATE_ENABLED` | 꺼짐 | `true`면 배치 모드. 이 문제는 배치 모드에서만 생긴다. |
| `BATCH_UPDATE_INTERVAL` | `5` | 배치 반영 주기(초). 캐시 반영 실패 복구 목록도 `max(이 값, 1)`초마다 돈다(배치 모드와 무관). |

- `QUOTA_CACHE_TTL`이 `SYNC_FREQUENCY`보다 작으면 `SYNC_FREQUENCY`를 쓴다. 인증 fence 수명 계산이 "fence가 신선한 정보보다 오래 산다"는 성질을 지키기 위해서다.
- `QUOTA_CACHE_TTL`을 `0` 이하로 두면 상주를 끈다. 잔액 TTL은 `SYNC_FREQUENCY`가 되고 차감·정산에서 TTL을 연장하지 않는다. 장애 때 되돌리는 스위치다.
- 이 스위치가 정하는 것은 잔액 TTL, `FreshUntil`(정보 신선 기한과 어긋남 감지 로그), TTL을 연장하는 캐시 스크립트뿐이다. 배치 반영 실패 재시도와 배치 실행 직렬화, 종료 시 배치 비우기, 반영 중 표시, 캐시 반영 실패 복구 루프(6절)는 끈 뒤에도 그대로 돈다. 잔액 캐시의 수명은 upstream과 같아지지만 바이너리 전체가 upstream 동작이 되는 것은 아니다.
- 환경 변수라서 바꾸려면 서버를 재시작해야 한다. HA 구성이면 모든 앱 서버를 같은 값으로 맞춘다.

### 2.1 끈 뒤에 할 일

스위치를 끄면 **새로 쓰는 해시부터** upstream 동작이다. 이미 24시간 TTL을 받은 해시는 남은 TTL 동안 그대로 남는다(계획 결정 4). 상주를 끈 서버는 `FreshUntil`을 확인하지 않으므로, 남은 해시의 사용자 정보는 다른 경로(관리자 수정 등)가 해시를 고치거나 지우지 않는 한 만료될 때까지 다시 읽히지 않는다. upstream 동작으로 바로 돌아가려면 모든 서버를 재시작한 뒤 사용자 해시를 지운다.

```bash
# REDIS_CONN_STRING의 주소·비밀번호·DB 번호를 쓴다(예: -h 10.0.0.5 -a '<비밀번호>' -n 0).
# Sentinel 구성이면 먼저 master 주소를 확인한다(5절).
redis-cli -h <master> -p 6379 -n <DB> --scan --pattern 'user:[0-9]*' \
  | xargs -r -n 500 redis-cli -h <master> -p 6379 -n <DB> DEL
```

- `KEYS user:*`는 쓰지 않는다(큰 인스턴스를 멈춘다). `--scan`을 쓴다.
- 패턴은 `user:` 뒤에 숫자가 오는 키만 고른다. 인증 키(`auth:user:fence:<id>`, `auth:user:version:<id>`)와 API 키 캐시(`token:*`)는 지우지 않는다.
- 지우는 순간 각 서버에 있던 배치 대기분(몇 초 분량)만큼 초과 사용이 가능하다(설계 §6 첫 항목). 사용량이 적은 시간에 한다.
- 상주 이전 바이너리에서 롤링 업그레이드하는 동안에도 같은 섞인 구간이 생긴다. 옛 서버는 `FreshUntil`을 확인하지 않으므로, 새 서버가 24시간 TTL을 준 해시를 읽을 때 인증 fence 밖의 필드(그룹, 이메일, 설정 등)를 60초마다 다시 읽지 않고 해시가 만료될 때까지 그대로 쓴다. 인증 버전·fence 검사는 옛 서버에도 있다. 섞인 구간을 짧게 두고, 그동안 바꾼 사용자 정보가 옛 서버에 늦게 보이면 그 사용자의 해시를 지운다(3절 명령).
- 롤링 배포로 켠 서버와 끈 서버가 섞여 있는 동안에는 끈 서버의 캐시 쓰기가 해시 TTL을 `SYNC_FREQUENCY`로 되돌릴 수 있어(upstream 스크립트) 그 사용자에게는 원래 문제가 다시 생길 수 있다. 섞인 구간을 짧게 둔다.

## 3. DB 잔액을 직접 고친 경우 (설계 §4.9)

관리자 화면·API의 잔액 조정(`AdjustUserQuota`), 충전, 리딤 같은 앱 경로는 캐시를 함께 맞춘다. **가능하면 앱 경로로 고친다.**

SQL 콘솔이나 일회성 스크립트로 `users.quota`를 직접 고쳤다면 캐시는 바뀌지 않는다. 상주 전에는 60초 안에 해시가 사라지며 저절로 맞춰졌지만, 지금은 최대 24시간 동안 옛 잔액이 쓰인다. 그 사용자의 해시를 지운다. 다음 요청이 DB에서 새로 읽는다.

```bash
redis-cli -h <master> -p 6379 -n <DB> DEL user:<사용자 ID>
```

순서:

1. DB를 고친다.
2. 배치 모드라면 그 사용자의 최근 사용분이 DB에 반영될 때까지 기다린다. 기준은 그 사용자의 마지막 요청 뒤 `BATCH_UPDATE_INTERVAL` + 배치의 DB 쓰기 시간(운영 기준 3~5초)이다. 기본 설정이면 15초 정도 기다리면 충분하다. HA 구성이면 모든 서버의 대기분이 대상이다.
3. `DEL user:<사용자 ID>`를 실행한다.

사용자가 계속 요청을 보내는 중이면 대기분이 늘 있으므로, 지우는 순간의 대기분만큼 오차가 날 수 있다(몇 초 분량). 절차를 빼먹으면 6절의 어긋남 로그가 그 사용자를 가리킨다.

## 4. Redis 운영 전제 (설계 §4.13)

운영 Redis는 직접 관리한다(ElastiCache 아님). 클러스터 모드는 지원하지 않는다(upstream 인증 fence가 여러 키를 한 Lua 스크립트와 `MGET`으로 다룸).

### 4.1 영속성 (AOF)

```
appendonly yes
appendfsync everysec
```

- 재시작 때 해시가 남아 손실이 마지막 1초 분량으로 줄어든다.
- 영속성이 없으면 재시작 때 모든 활성 사용자가 DB에서 다시 읽히고, 각 앱 서버의 배치 대기분(10초 안팎)만큼 초과 사용이 가능하다.
- Sentinel 구성이면 master와 모든 replica에 같은 설정을 둔다(장애 조치 뒤 replica가 master가 된다). 실행 중에 바꿀 때는 `CONFIG SET appendonly yes` 뒤 `CONFIG REWRITE`로 설정 파일에 남긴다.

### 4.2 메모리

- 사용자 해시 하나는 약 0.6 KB 이하다(추정). 24시간 안에 잔액을 쓴 사용자만 남는다: 1만 명 약 6 MB, 10만 명 약 60 MB.
- `maxmemory`를 이 추정의 수십 배로 넉넉히 잡는다. 한도에 닿으면 정책에 따라 결과가 다르다.
  - `noeviction`: 쓰기가 실패한다. 앱은 DB로 우회 차감하고 캐시는 그대로 남아, 초과 사용 방향으로 어긋난다(복구 목록이 나중에 해시를 지운다, 6.2절).
  - eviction 정책(`volatile-lru` 등): 해시가 밀려난다. 밀려난 순간의 배치 대기분만큼 초과 사용이 가능하다.
  - 어느 쪽이든 한도에 닿지 않게 하는 것이 목적이다.
- 다른 용도와 Redis를 같이 쓰면 new-api 전용 인스턴스나 DB 번호를 분리한다.

### 4.3 감시 지표

| 지표 | 확인 방법 | 정상 |
|---|---|---|
| 밀려난 키 | `redis-cli INFO stats` → `evicted_keys` | 0 |
| 영속성 | `redis-cli INFO persistence` → `rdb_last_bgsave_status`, `aof_last_write_status` | `ok` |
| 메모리 | `redis-cli INFO memory` → `used_memory` / `maxmemory` | 한도에서 멀리 |
| 장애 조치 횟수 | Sentinel 로그의 `+switch-master`, `redis-cli -p 26379 SENTINEL MASTER mymaster` | 예상한 작업 때만 |
| DB 우회 차감 빈도 | 앱 로그 `user quota cache reserve unavailable, falling back to database` | 평소 0, Redis 장애 때만 |
| 잔액 어긋남 | 앱 로그 `user quota cache drift:` (6.1절) | 0 |

`DEFAULT_LANGUAGE`가 `zh`로 시작하면 upstream 로그 줄은 중국어로 찍힌다(`common/log_text.zh-CN.json`). 위의 DB 우회 차감 줄은 `无法在缓存中预扣用户额度，改为直接扣数据库`이다. 이 작업이 더한 fork 로그 줄(6절)은 언어 설정과 상관없이 영어다.

## 5. Redis Sentinel 연결 (설계 §4.14)

| 환경 변수 | 필수 | 의미 |
|---|---|---|
| `REDIS_CONN_STRING` | 예 | Redis 사용 여부의 스위치이자 master 인증 정보. Sentinel 모드에서는 **호스트 부분을 무시**하고 사용자 이름·비밀번호·DB 번호와 연결 옵션(TLS, 타임아웃, 재시도, 풀 설정)만 쓴다. |
| `REDIS_SENTINEL_ADDRS` | 아니요 | 쉼표로 구분한 Sentinel `host:port` 목록. 비어 있으면 지금처럼 단일 노드로 붙는다. |
| `REDIS_SENTINEL_MASTER_NAME` | 아니요 | Sentinel의 master 이름. 기본 `mymaster`. |
| `REDIS_SENTINEL_USERNAME` | 아니요 | Sentinel 자체의 ACL 사용자 이름(`REDIS_SENTINEL_PASSWORD`와 함께). |
| `REDIS_SENTINEL_PASSWORD` | 아니요 | Sentinel 자체의 비밀번호. master 비밀번호는 `REDIS_CONN_STRING`에서 온다. |
| `REDIS_POOL_SIZE` | 아니요 | 지금처럼 풀 크기. Sentinel 모드에도 그대로 쓴다. |

예:

```
REDIS_CONN_STRING=redis://:<master 비밀번호>@ignored-host:6379/0
REDIS_SENTINEL_ADDRS=10.0.0.1:26379,10.0.0.2:26379,10.0.0.3:26379
REDIS_SENTINEL_MASTER_NAME=mymaster
```

- **TLS:** `REDIS_CONN_STRING`이 `rediss://`면 TLS 설정이 master 연결과 Sentinel 연결에 같이 적용된다. 인증서 `ServerName`은 `REDIS_CONN_STRING`의 호스트 그대로이므로, master·replica·Sentinel 인증서가 모두 그 이름을 받아 주어야 한다.
- **시작 로그:** `Redis Sentinel enabled: master mymaster via 10.0.0.1:26379,… (host in REDIS_CONN_STRING is ignored; …)`. 디버그 로그 `Redis connected to …`는 Sentinel 모드에서도 무시된 `REDIS_CONN_STRING` 호스트를 찍는다(알려진 표시 문제, 연결에는 영향 없음).
- **장애 조치 중:** 연결 오류는 Redis 오류 처리로 흡수된다. 사전 차감은 DB로 우회하고(4.3절의 DB 우회 차감 로그), 그 사용자는 캐시 반영 실패 복구 목록에 올라 Redis가 돌아오면 해시가 지워진다(6.2절). 복제가 비동기라 장애 조치 순간 마지막 몇 ms의 쓰기가 빠질 수 있다.
- **`REDIS_CONN_STRING`을 비우면 Redis 없이 돈다:** `REDIS_SENTINEL_ADDRS`만 넣고 `REDIS_CONN_STRING`을 비우면 upstream 초기화가 먼저 끝나 버려(Sentinel 설정을 보기 전) 오류 없이 Redis를 끄고 시작한다. 시작 로그에 `REDIS_CONN_STRING not set, Redis is not enabled`가 찍히고 `Redis Sentinel enabled` 줄이 없다. 배치 모드라면 잔액이 DB 기준으로만 돌아 이 문서의 캐시 보호가 모두 꺼진다. Sentinel 모드에서도 `REDIS_CONN_STRING`을 꼭 넣는다(호스트는 아무 값이나 된다).
- **현재 master 확인:** `redis-cli -h <sentinel> -p 26379 SENTINEL get-master-addr-by-name mymaster`. 2·3절의 `redis-cli` 명령은 이 주소에 실행한다.
- **실제 연결 확인(운영자 작업):** 이 저장소에서는 Docker가 없어 실제 Sentinel 연결과 장애 조치를 확인하지 못했다. 로컬 재현 구성(`scripts/redis-sentinel/`)과 절차는 `scripts/redis-sentinel/README.md`에 있다. 연결 테스트는 `REDIS_SENTINEL_TEST_ADDRS`(와 선택 `REDIS_SENTINEL_TEST_MASTER`)가 있을 때만 돈다:

```bash
REDIS_SENTINEL_TEST_ADDRS=<sentinel1>:26379,<sentinel2>:26379,<sentinel3>:26379 \
REDIS_SENTINEL_TEST_MASTER=mymaster \
GOWORK=off go test ./common/ -run TestNewRedisClientConnectsThroughSentinel -v
```

## 6. 로그와 대응

### 6.1 잔액 어긋남 감지 (설계 §4.7)

```
[SYS] 2026/10/15 - 14:00:00 | user quota cache drift: user=<id> cache=<캐시 잔액> db=<DB 잔액> diff=<캐시 − DB>
```

- 오류 출력(`SysError`)으로 남는다. 잔액은 고치지 않는다(설계 결정 6).
- 언제 남나: 요청이 그 사용자의 해시를 읽을 때 사용자 정보가 `SYNC_FREQUENCY`보다 오래됐으면 DB를 다시 읽고 캐시 잔액과 DB 잔액을 비교한다. 이 서버에 그 사용자의 배치 대기분이 있거나 반영 중이면 비교하지 않는다. 두 번 연속 관찰에서 캐시 잔액과 차이가 모두 그대로이고 0이 아니면 한 줄을 남긴다. 두 관찰은 `SYNC_FREQUENCY` 이상 떨어져야 하며, 그보다 가까운 관찰(같은 순간 여러 요청이 함께 다시 읽는 경우 등)은 앞 관찰을 그대로 두고 무시한다. 다른 서버의 대기분은 두 관찰 사이에 DB에 반영되어 차이가 바뀌므로 걸리지 않는다.
- 잔액을 계속 쓰는 사용자는 캐시 잔액이 바뀌므로 걸리지 않는다. 잔액을 바꾸지 않는 요청을 보내는 동안의 사용자가 대상이다.

원인과 맞는 쪽:

| 차이 | 흔한 원인 | 맞는 쪽 |
|---|---|---|
| `diff > 0` (캐시가 많음) | DB를 직접 줄이고 해시를 지우지 않음(3절), 복구 목록이 프로세스와 함께 사라짐(강제 종료) | 보통 DB |
| `diff > 0`, 직전에 그 사용자의 재큐잉 로그가 몰려 있음 | DB 쓰기 시간 초과·연결 끊김 뒤 재시도가 같은 차감을 한 번 더 반영(6.4절) | **캐시** (DB가 틀림) |
| `diff < 0` (캐시가 적음) | DB를 직접 늘리고 해시를 지우지 않음, 강제 종료(OOM, SIGKILL)로 배치 대기분이 DB에 반영되지 못함, DB 장애로 배치 재시도가 아직 실패 중 | 직접 수정이면 DB, 강제 종료·DB 장애면 캐시 |

대응 절차:

1. 같은 사용자의 로그를 찾는다: `failed to batch update user <id> quota, requeued`(DB 장애, 재시도 중. DB가 회복된 뒤에도 `diff > 0`이 그대로면 6.4절의 이중 반영), 종료 로그(6.3절)의 `queue empty: false`, 최근의 직접 DB 수정 기록, 서버의 강제 종료 기록.
2. DB 장애로 재시도 중이면 기다린다. DB가 회복되면 재시도가 반영하고 차이가 사라진다. 회복 뒤에도 `diff > 0`이 같은 값으로 남으면 6.4절의 대조 절차를 따른다(해시를 지우지 않는다).
3. DB가 맞으면(직접 수정 등): 대기분이 반영된 뒤(3절 2단계) `DEL user:<id>`. 다음 요청이 DB 잔액으로 다시 채운다.
4. 캐시가 맞으면(차감분이 DB에 반영되지 못함): 해시를 지우면 DB의 큰 잔액으로 돌아가 그만큼 더 쓸 수 있게 된다. 그 사용자가 요청을 보내지 않는 동안 DB를 캐시 값에 맞춘다(`users.quota`를 로그의 `cache` 값으로). 해시는 지우지 않는다. 판단이 서지 않으면 3단계(DB 기준)를 따른다 — 상주 전 develop과 같은 결과다.
5. 다음 `SYNC_FREQUENCY` 주기 뒤 같은 사용자의 로그가 더 나오지 않는지 확인한다(차이가 0이면 기억을 지운다).

### 6.2 캐시 반영 실패 복구 목록 (설계 §4.12-3)

커밋 뒤 캐시에 차이를 더하지 못했거나 사전 차감을 DB에서만 한 사용자(Redis 오류, 또는 해시가 없고 채우지도 못한 경우)는 프로세스 안 목록에 오른다. `max(BATCH_UPDATE_INTERVAL, 1)`초마다 Redis가 응답하면, 대기분이 없는 사용자의 해시를 지운다. 다음 요청이 DB에서 새로 읽는다. 값을 고치지는 않는다.

- "대기분이 없음"은 **이 서버**의 배치 큐와 반영 중 표시만 본다. HA 구성이면 다른 서버가 그 사용자의 차감분을 아직 DB에 쓰지 못했을 수 있고, 그 상태로 지우면 그 대기분(배치 간격과 DB 쓰기 시간 동안의 몇 초 분량)만큼 초과 사용이 가능하다. 해시가 지워지는 다른 경우(설계 §6 첫 항목)와 같은 크기의 한계다.

목록에 오르게 하는 로그(이 줄이 보이면 그 사용자가 목록에 있다. 예외는 아래):

```
user quota cache reserve unavailable, falling back to database: <오류>
failed to compensate reserved user quota: result=<n> error=<오류>
failed to sync <operation> credit to user quota cache: <오류>
failed to increase user quota: <오류>
failed to decrease user quota: <오류>
failed to sync aff transfer to user quota cache: <오류>
failed to decrease user quota cache after subscription balance purchase: <오류>
```

- `failed to compensate reserved user quota`는 `error`가 nil이 아닐 때만 목록에 오른다. `result=2 error=<nil>`은 보상할 해시가 없었다는 뜻(miss)이라 고칠 것이 없다.
- 해시가 없고 채우지도 못해(예: 인증 fence가 DB보다 새로움) 사전 차감을 DB에서 한 경우는 로그 줄 없이 목록에 오른다.

복구 패스 로그:

```
user cache repair deferred for <n> user(s): <오류>
```

- Redis가 아직 응답하지 않아 해시를 지우지 못한 것이다. 남은 사용자는 목록에 남고 다음 주기에 다시 시도한다. Redis 장애 동안 주기마다 한 줄씩 남는다.
- Redis가 회복된 뒤에도 계속 나오면 Redis 연결(Sentinel이면 5절)을 확인한다.
- 목록은 프로세스 메모리에 있다. 정상 종료 때는 한 번 더 패스를 돌리지만(6.3절), 강제 종료되면 사라진다. 그 사용자들은 6.1절 로그로 드러난다.

### 6.3 종료 시 배치 비우기 (설계 §4.12-1)

정상 종료(SIGTERM 등) 때 배치 모드면 남은 배치 큐를 DB에 쓴다.

```
flushing batch updates before shutdown
flushed batch updates in <n> pass(es), queue empty: true
```

- 이미 쓰는 중인 배치가 있으면 끝나기를 기다린 뒤 최대 3번 반복한다.
- `queue empty: false`면 3번 뒤에도 큐가 남았다(DB 쓰기 실패가 계속되었거나 종료 중에 요청이 계속 들어옴). 남은 차감분은 DB에 없고 캐시에만 있다. `failed to batch update user <id> quota, requeued`로 사용자를 찾아 6.1절 4단계처럼 다룬다.
- `SHUTDOWN_TIMEOUT_SECONDS`(기본 120초) 안에 HTTP 서버가 끝나지 않았다면 처리 중인 요청이 아직 큐에 넣을 수 있어 best-effort다.
- 이어서 Redis가 켜져 있으면(배치 모드가 아니어도) 복구 목록 패스를 한 번 돈다.
- `kill -9`, OOM 같은 강제 종료에서는 이 처리가 돌지 않는다. 배포 도구가 종료 신호 뒤 충분히 기다리도록 둔다.

### 6.4 배치 반영 실패 재시도 (설계 §4.12-2)

```
failed to batch update user <id> quota, requeued quota=<q> used=<u> requests=<r>: <오류>
```

사용자 행의 DB 쓰기가 실패하면 그 차감분(quota, used_quota, request_count 세 값)을 다음 배치 큐에 다시 넣는다. 버리지 않으며 재시도 횟수 제한은 없다. 재시도 중인 사용자는 어긋남 감지(6.1절)와 복구 목록(6.2절)에서 대기 중으로 취급된다. DB가 회복되면 저절로 반영된다. 이 줄이 계속 나오면 DB 상태를 확인한다. `q`는 잔액 증감(차감이면 음수), `u`·`r`은 사용량·요청 수 증가분이다.

**이중 반영(남는 한계, 설계 §6):** 쓰기 시간 초과나 연결 끊김은 DB 서버가 UPDATE를 이미 적용한 뒤에도 오류로 돌아올 수 있다. 앱은 실패로 보고 다시 넣으므로 재시도가 같은 증감을 한 번 더 적용한다. 그러면 DB 잔액이 캐시보다 `|q|`만큼 적어지고(사용자에게 불리한 쪽), `used_quota`·`request_count`도 `u`·`r`만큼 더 커진다. 맞는 쪽은 캐시이고 DB가 틀렸다. 한 번만 적용되게 하려면 장부가 필요해 이 작업 범위 밖이다(설계 §9).

증상: 그 사용자의 재큐잉 줄이 몰려 나온 뒤(오류가 `i/o timeout`, `bad connection`, `connection reset` 같은 연결 계열), DB가 회복된 다음에도 6.1절의 어긋남 로그가 같은 `diff > 0`으로 남는다.

대조 절차:

1. 그 사용자의 재큐잉 줄을 모두 모은다. 같은 묶음이 재시도마다 다시 찍히므로(큐에서 새 차감과 합쳐질 수 있음) 줄마다 금액을 확인한다.
2. 어긋남 로그의 `diff`가 그중 연결 계열 오류 줄 하나(또는 몇 개의 합)의 `-q`와 정확히 같은지 확인한다. 같지 않으면 이중 반영이 아니다. 6.1절의 다른 원인을 본다.
3. 해시는 지우지 않는다(틀린 DB 값으로 돌아간다). DB를 상대값으로 고친다. 배치 반영도 상대값 UPDATE라 사용자가 요청을 보내는 중이어도 섞이지 않는다.

```sql
UPDATE users
SET quota = quota - (<q>), used_quota = used_quota - <u>, request_count = request_count - <r>
WHERE id = <id>;
```

4. 다음 `SYNC_FREQUENCY` 주기 두 번 뒤 그 사용자의 어긋남 로그가 더 나오지 않는지 확인한다.

## 7. 알려진 한계

- **해시가 지워지는 경우:** Redis 재시작(영속성 없음)이나 장애 조치, 수동 `DEL`, 조직 기능의 캐시 무효화, 캐시 스키마 버전이 바뀌는 업그레이드. 그 순간의 배치 대기분만큼 초과 사용이 가능하다.
- **API 키 잔액:** API 키 캐시(`token:*`)는 바꾸지 않았다. 지갑을 차감하지 않는 요청(구독 플랜)에서 키 한도만 상한이면 배치 대기분만큼 키 한도를 넘을 수 있다.
- **배치 쓰기의 이중 반영:** DB 쓰기 오류가 실제로는 적용된 뒤 돌아오면 재시도가 같은 차감을 한 번 더 반영한다. DB가 틀린 쪽이며 6.4절 절차로 대조한다.
- **노드 사이 시계 차이:** `FreshUntil`은 앱 서버 시계로 쓰고 비교한다. 서버 사이 시계 차이가 `max(SYNC_FREQUENCY, 60)`초를 넘으면 인증 fence 여유가 줄어든다. 모든 앱 서버에 NTP를 켠다.
- 전체 목록은 설계 §6과 기록 `docs/2026-10-15-quota-cache-residency.md` §5에 있다.
