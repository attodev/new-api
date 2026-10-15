# 로컬 Redis Sentinel 재현 환경

Redis Sentinel 직접 연결(설계 §4.14, `common/redis_sentinel_fork.go`)을 확인하기 위한 구성이다. master 1, replica 1, sentinel 3개이며 공식 `redis:7` 이미지를 쓴다. quorum은 2, `down-after-milliseconds`는 2000, `failover-timeout`은 5000이다.

> 이 개발용 Mac에는 Docker가 없어 이 절차는 운영자 작업이다. 이 저장소에서는 실행해 보지 못했다.

## 준비

Sentinel은 master와 replica를 호스트 이름(`redis-master`, `redis-replica`)으로 알려 준다. 호스트에서 테스트를 돌리려면 이 이름이 로컬로 풀려야 하므로 한 번만 `/etc/hosts`에 추가한다.

```
127.0.0.1 redis-master redis-replica
```

master는 호스트 6379, replica는 6380(컨테이너 안에서도 6380이라 Sentinel이 알려 주는 주소가 호스트에서 그대로 통한다), Sentinel은 26379, 26380, 26381로 열린다. 장애 조치 뒤에는 replica가 master가 되므로 두 이름이 모두 로컬로 풀려야 한다.

## 기동

```bash
cd scripts/redis-sentinel
docker compose up -d
docker compose logs sentinel1 | grep -E "monitor|sentinel"   # 세 Sentinel이 서로 발견할 때까지 몇 초 기다린다
```

## 연결 테스트

저장소 루트에서 실행한다. `REDIS_SENTINEL_TEST_ADDRS`가 없으면 이 테스트는 건너뛴다.

```bash
REDIS_SENTINEL_TEST_ADDRS=localhost:26379,localhost:26380,localhost:26381 \
REDIS_SENTINEL_TEST_MASTER=mymaster \
GOWORK=off go test ./common/ -run TestNewRedisClientConnectsThroughSentinel -v
```

`PASS`가 나와야 한다(`SKIP`이면 환경 변수가 전달되지 않은 것이다).

## 장애 조치 확인

```bash
cd scripts/redis-sentinel
docker compose stop redis-master
sleep 10                                    # down-after-milliseconds 2초 + 선출 시간
docker compose exec sentinel1 redis-cli -p 26379 sentinel get-master-addr-by-name mymaster   # redis-replica 6380
```

그 뒤 위 연결 테스트를 같은 명령으로 다시 실행한다. 새 master(원래 replica)로 붙어 `PASS`가 나와야 한다. master를 되돌리려면 `docker compose start redis-master`(replica로 합류한다).

## 앱에서 쓰기

```
REDIS_CONN_STRING=redis://localhost:6379/0
REDIS_SENTINEL_ADDRS=localhost:26379,localhost:26380,localhost:26381
REDIS_SENTINEL_MASTER_NAME=mymaster
```

`REDIS_CONN_STRING`의 호스트는 무시하고 사용자 이름·비밀번호·DB 번호만 쓴다. 시작 로그에 `Redis Sentinel enabled: master mymaster via …`가 남는다.

## 정리

```bash
docker compose down -v
```

## 참고

- Sentinel이 `sentinel.conf`를 직접 고쳐 쓰기 때문에 compose는 설정을 `/tmp`로 복사해 실행한다. 저장소의 파일은 바뀌지 않는다.
