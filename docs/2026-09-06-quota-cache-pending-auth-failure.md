# Quota 캐시 pending으로 인한 인증 실패 분석 및 수정

작성일: 2026-09-06

이 문서는 동시 스트리밍 요청 중 간헐적으로 발생하던 HTTP 500
`"데이터베이스 오류입니다. 관리자에게 문의하세요"` 응답의 원인 분석과 수정
내용을 기록한다. 외부 사용자로부터 접수된 게이트웨이 오류 보고 4건 중
"간헐적 HTTP 500 / DB 오류" 항목에 해당한다.

## 증상

동시 스트리밍 요청 6건을 실행했을 때 일부 요청이 다음 응답으로 실패했다.

```json
{
  "error": {
    "message": "데이터베이스 오류입니다. 관리자에게 문의하세요",
    "type": "new_api_error"
  }
}
```

단발 요청에서는 재현되지 않고 동시 부하에서만 간헐적으로 발생했다.
`new_api_error` 타입과 해당 메시지는 `i18n/locales/ko.yaml`의
`common.database_error` 키이며, relay 계층이 아니라 인증 미들웨어에서
생성된다.

## 전제 조건

이 문제는 `BATCH_UPDATE_ENABLED=true`이고 Redis가 활성화된 배포에서만
발생한다. 배치 업데이트가 꺼져 있으면 아래에서 설명하는 pending 상태
자체가 존재하지 않는다.

관련 기본값은 다음과 같다.

| 환경변수 | 기본값 | 용도 |
| --- | --- | --- |
| `BATCH_UPDATE_ENABLED` | `false` | 배치 quota 반영 활성화 여부 |
| `BATCH_UPDATE_INTERVAL` | `5` | 배치 flush 주기(초) |
| `SYNC_FREQUENCY` | `60` | 사용자/토큰 quota 캐시 TTL(초) |

## 배경: 배치 모드의 quota 흐름

배치 업데이트가 켜지면 Redis가 잔액의 authoritative source가 되고
데이터베이스는 뒤늦게 따라온다.

1. 요청이 Lua 스크립트로 Redis 잔액을 원자적으로 차감한다.
2. 같은 증감분을 프로세스 로컬 큐(`batchUpdateStores`)에 적재한다.
3. `batchUpdate()`가 `BATCH_UPDATE_INTERVAL`마다 큐를 비우며 데이터베이스에
   반영한다.

따라서 정상 동작 중에도 다음 상태가 존재한다.

```
DB.remain_quota   = 1000   아직 증감분 미반영
Redis.RemainQuota =  700   이미 차감된 권위 있는 값
pending delta     = -300
```

이 상태에서 데이터베이스 스냅샷(1000)을 Redis에 다시 게시하면 이미 사용한
300이 되살아나 이중 사용이 발생한다. 그래서 `cacheInitToken`과
`populateUserCache`에는 "pending 중에는 캐시 게시 금지"라는 가드가 있다.
이 가드의 판단 자체는 옳다.

## 근본 원인

가드가 캐시 게시 실패가 아니라 요청 실패로 전파된 것이 원인이다.

수정 전 `GetTokenByKey`는 데이터베이스 조회에 성공했음에도 캐시 게시가
막히면 조회 결과를 버리고 오류를 반환했다.

```go
// 1) Redis 조회 실패는 무시하고 통과
// 2) 데이터베이스 조회 성공 - 토큰 정보는 모두 확보한 상태
if common.RedisEnabled {
    if _, cacheErr := cacheInitToken(*token); cacheErr != nil {
        if errors.Is(cacheErr, ErrQuotaCachePending) {
            return nil, cacheErr // 확보한 토큰을 버리고 실패
        }
    }
}
```

`GetUserCache`도 동일한 구조였다. 이 오류는 인증 미들웨어까지 그대로
전파된다.

```
GetTokenByKey -> ErrQuotaCachePending
  ValidateUserToken: gorm.ErrRecordNotFound 가 아니므로 ErrDatabase 로 래핑
    middleware/auth.go: abortWithOpenAiMessage(500, MsgDatabaseError)
```

부가로 `cacheInitToken`은 pending 검사를 fence 및 존재 검사보다 먼저
수행한다. 그래서 다른 고루틴이 이미 캐시를 정상적으로 채워 게시가 no-op일
상황에서도 요청이 실패했다.

## 고장 조건

오류가 발생하려면 캐시 미스와 pending 증감분이 동시에 성립해야 한다.

캐시 미스가 발생하는 경우는 다음과 같다.

- TTL 만료(`quotaCacheTTLSeconds()` = `SYNC_FREQUENCY`, 기본 60초)
- 토큰 변경 시 `invalidateTokenCacheForMutation`이 키를 삭제하고 10초 fence를
  설정하는 구간
- Redis eviction, 재시작, failover
- 캐시 스키마 버전 변경 후 배포(`CacheSchema` 불일치)

pending 증감분은 사실상 상시 존재한다. 과금 대상 요청은 예약 시점과 정산
시점에 각각 token id 및 user id로 레코드를 남기며, `batchUpdate()`는 큐를
`batchUpdateInFlight`로 옮긴 뒤 lock을 놓고 id별 데이터베이스 UPDATE를
수행하므로 그 쓰기 시간 내내 `hasPendingBatchRecordLocked`가 참을 유지한다.

동시 스트리밍에서 재현율이 높은 이유는 다음과 같다.

```
t=0.0s  스트림 6건 시작, 예약 6건으로 pending 적재
t=5.0s  batchUpdate 시작, in-flight 이동 후 DB UPDATE 진행(여전히 pending)
t=5.1s  토큰 캐시 TTL 만료 또는 fence와 겹침
        신규 요청 -> 캐시 미스 -> DB 조회 성공 -> pending -> 500
t=5.3s  batchUpdate 종료
t=5.4s  다음 요청이 pending을 다시 채움
```

스트리밍 요청은 수명이 길어 겹침이 크고 pending 창이 거의 닫히지 않는다.
그 결과 단발 요청은 정상이지만 동시 부하에서만 간헐적으로 실패하는 패턴이
된다.

## 수정 내용

캐시에 게시하지 못하는 것과 요청을 실패시키는 것을 분리했다. 사용자 및
토큰 조회 함수를 각각 두 갈래로 나눴다.

```go
// 잔액이 정확해야 하는 경로 전용. 기존 fail-closed 동작을 유지한다.
func hydrateTokenCache(key string, fromDB bool) (*Token, error) {
    // ...
    if errors.Is(cacheErr, ErrQuotaCachePending) {
        return token, cacheErr // 값을 함께 반환하며 계약을 주석에 명시
    }
    // ...
}

// 인증 및 조회 경로. pending은 캐시 게시만 막고 읽기는 통과시킨다.
func GetTokenByKey(key string, fromDB bool) (*Token, error) {
    token, err := hydrateTokenCache(key, fromDB)
    if errors.Is(err, ErrQuotaCachePending) {
        return token, nil
    }
    return token, err
}
```

`GetUserCache`와 `hydrateUserCache`도 동일한 구조로 나눴다.

호출부는 다음과 같이 라우팅했다.

| 경로 | 사용 함수 | 동작 |
| --- | --- | --- |
| `TryReserveUserQuota`, `TryReserveTokenQuota`, `GetUserQuota` | `hydrateUserCache`, `hydrateTokenCache` | fail-closed 유지 |
| 인증 미들웨어, 컨트롤러, 일반 조회 | `GetUserCache`, `GetTokenByKey` | fail-open |

`populateUserCache`와 `cacheInitToken`의 가드는 그대로 두었다. stale
스냅샷은 여전히 Redis에 게시되지 않으며, 바뀐 것은 그 가드의 실패를
호출자에게 전달하는 방식뿐이다.

## 안전성 근거

인증 단계에서 반환하는 잔액은 pending 증감분만큼 높게 보일 수 있다. 그러나
그 값은 사전 확인에만 사용된다.

```go
// ValidateUserToken
if !token.UnlimitedQuota && token.RemainQuota <= 0 { /* ... */ }
```

실제 차감은 항상 Redis Lua 원자 연산이 담당하며 해당 경로는 fail-closed를
유지한다.

```
인증 통과(잔액이 다소 높게 보일 수 있음)
  TryReserveTokenQuota  hydrate 계열이므로 pending이면 여기서 차단
    Lua: remain 부족 시 0 반환 -> "token quota is not enough"
```

사전 확인은 fail-open, 실제 지출은 fail-closed라는 순서가 성립한다. 수정
전에는 지출 가드가 인증 단계까지 올라와 quota를 사용하지 않는 요청까지
실패시키고 있었다.

## 검증

재현 테스트를 먼저 작성해 수정 전 실패를 확인했다.

```
--- FAIL: TestGetTokenByKeyServesDatabaseSnapshotWhileBatchPending
    Received unexpected error: quota cache unavailable while database updates are pending: token 1
--- FAIL: TestGetUserCacheServesDatabaseSnapshotWhileBatchPending
    Received unexpected error: quota cache unavailable while database updates are pending: user 1
```

테스트는 실제 고장 조건을 그대로 재현한다. 예약으로 pending을 만든 뒤
`RDB.Del`로 캐시를 삭제하고 조회한다. 각 테스트는 다음 세 가지를 함께
검증한다.

1. 인증 조회가 성공한다.
2. stale 스냅샷이 여전히 게시되지 않는다.
3. 예약은 여전히 `ErrQuotaCachePending`으로 차단된다.

기존 fail-closed 테스트 2건은 `hydrate` 계열을 직접 호출하도록 변경해
유지했다. `go build ./...`, `go vet`, `go test ./...`를 통과했다.

## 운영 확인 방법

배포 후 다음 로그가 사라졌는지 확인한다.

```
grep -E "ValidateUserToken database error|GetUserCache error for user" <log>
```

전제 조건은 다음으로 확인한다. `BATCH_UPDATE_ENABLED`가 `true`가 아니면
증상의 원인은 다른 곳에 있다.

```
printenv BATCH_UPDATE_ENABLED SYNC_FREQUENCY BATCH_UPDATE_INTERVAL
```

## 남은 작업

`service/pre_consume_quota.go`의 `PreConsumeQuota`는 `GetUserQuota`를 통해
`hydrateUserCache`를 사용하므로 동일한 조건에서 여전히 실패한다. 다만 이는
인증 500이 아니라 relay 단계의 `ErrorCodeQueryDataError`로 표면화되며 보고된
증상과는 다른 응답이다.

이 경로까지 완화하려면 `GetUserQuota`가 pending 중 높게 보이는 잔액을
반환하도록 허용해야 한다. 현재 `TestUserHydrationFailsClosedWithPendingBatch`가
명시적으로 지키고 있는 quota 가드이므로 별도 판단 없이 변경하지 않았다.
