# Upstream quota 통합 검토 및 후속 작업 기록

작성일: 2026-08-24

이 문서는 `codex/upstream-token-quota-reserve` 브랜치를 `team`에 병합하기 전
진행한 upstream 동기화, 호환성 검토, 후속 수정, 검증 결과 및 보류 작업을
기록한다.

## 검토 범위

최근 upstream에서 가져온 변경 사항을 로컬의 조직 과금, tiered 재시도,
비동기 작업 정산 및 Claude relay 변경 사항과 함께 검토했다. 주요 목표는
동시 요청으로 인한 quota 초과 사용을 방지하고, Redis와 배치 데이터베이스
정산의 일관성을 유지하며, 조직 정산 시 요청 시작 시점에 선택된 조직을
계속 사용하도록 보장하는 것이었다.

## 병합된 커밋

| 커밋 | 요약 | Upstream 또는 관련 커밋 |
| --- | --- | --- |
| `15834520f` | 토큰 quota 원자적 예약 | [upstream `ccd535ef8`](https://github.com/QuantumNous/new-api/commit/ccd535ef8) |
| `f9b1d1f50` | Tiered 재시도 예약 중 조직 지갑 조정 | 로컬 tiered 재시도 과금 관련 |
| `8e242b25f` | 비동기 작업 정산 및 환불을 위한 조직 스냅샷 | 조직 지갑 과금 관련 |
| `30559b2b8` | Claude relay의 인라인 PDF/텍스트 파일 처리 복원 | [upstream `263b9bc`](https://github.com/QuantumNous/new-api/commit/263b9bc695af1e557ed5a20ba910d682025cb0b1) |
| `ba6afab57` | 사용자 지갑 원자적 예약 | [upstream `ccd535ef8`](https://github.com/QuantumNous/new-api/commit/ccd535ef8), `15834520f` 관련 |
| `3cfc88575` | 토큰 quota 캐시 증감분 동기 반영 | `15834520f` 관련 |
| `5e6c6a716` | 사용자 quota 캐시 증감분 동기 반영 | `ba6afab57` 관련 |
| `9d0fa91fd` | 조직 지갑 원자적 예약 및 정산 대상 조직 고정 | `0ac72dcf7`, `f9b1d1f50`, `8e242b25f` 관련 |
| `f1dd2da1e` | 대기·처리 중 quota 배치 추적 및 쓰기 실패 재시도 | 사용자/토큰 원자적 예약 변경 관련 |
| `117fec391` | 사용자 캐시 메타데이터 로딩 중 현재 quota 보존 | `ba6afab57`, `5e6c6a716` 관련 |
| `10ade7045` | 캐시 로딩과 로컬 배치 예약 상태 직렬화 | `15834520f`, `ba6afab57`, `5e6c6a716`, `f1dd2da1e`, `117fec391` 관련 |
| `552ec4d14` | Tiered 재시도 시 그룹 전환 안정성 강화 | [upstream `e5aa3f5fd`](https://github.com/QuantumNous/new-api/commit/e5aa3f5fd), `f9b1d1f50` 관련 |

각 포팅 또는 후속 수정 커밋의 커밋 메시지에는 나중에 변경 근거를 추적할 수
있도록 upstream 링크 및/또는 `Related-Commit` trailer가 포함되어 있다.

## 반영 후 동작

- 사용자 및 quota가 제한된 토큰의 선차감은 원자적 확인·차감 경로를 사용한다.
- 명시적인 토큰 정산 증감분은 잔여 quota와 사용 quota를 모두 갱신한다.
- Redis quota 갱신은 조정 함수가 반환되기 전에 다른 요청에서 확인할 수 있다.
- 단일 노드에서는 대기 및 처리 중인 로컬 배치 증감분을 고려하여 오래된
  데이터베이스 값으로 fallback하는 것을 방지한다.
- 사용자/토큰 quota 배치 쓰기가 실패하면 해당 변경분을 큐로 되돌린다.
- 캐시 메타데이터를 불러올 때 이전 데이터베이스 스냅샷이 현재 quota 잔액을
  덮어쓰지 않는다.
- 조직 구성원 quota와 공유 조직 지갑은 각각의 잔액 경계에서 원자적으로
  예약된다.
- 조직 정산 및 비동기 작업 환불은 과금 시작 시점에 저장한 조직을 계속
  사용한다.
- Tiered 재시도는 추가 quota를 예약하기 전에 선택 그룹을 갱신하고, 이미
  사용한 채널 이력을 보존하며, 무료 그룹에서 유료 그룹으로 이동할 때
  오래된 무료 모델 상태를 제거한다.
- Claude 인라인 파일 변환은 PDF 문서와 텍스트 파일을 지원하며, 지원하지
  않는 파일 형식은 잘못된 content를 만들지 않고 건너뛴다.

## 수행한 검증

- 전체 `model`, `service`, `controller` 및 Claude relay 패키지 테스트를
  통과했다.
- 사용자/토큰/조직 quota 동시성 집중 테스트를 Go race detector와 함께
  통과했다.
- Tiered 그룹 전환 회귀 테스트를 통과했다.
- 변경된 model, service, controller 및 Claude relay 패키지에 대해
  `go vet`을 통과했다.
- `git diff --check`를 통과했으며, 이 문서를 추가하기 전 기능 워크트리는
  깨끗한 상태였다.
- `go test ./...`는 발견된 모든 패키지를 실행했으나, root 패키지는
  `web/classic/dist`가 없어 setup 단계에서 컴파일할 수 없었다. 이는 이번
  변경으로 발생한 실패가 아니라 기존 워크스페이스 산출물 문제다.

## 이번 병합의 운영 전제

현재 배포 환경은 단일 노드이며 Redis와 배치 업데이트를 사용한다. 이 전제
아래에서는 구현된 로컬 pending/in-flight 보호가 위 회귀 테스트로 기능상
검증되었다.

## 보류된 TODO

다음 항목은 현재 배포 환경이 단일 노드이므로 의도적으로 보류했다. 다중
노드로 확장하거나 다른 캐시 구성을 활성화하기 전에 완료해야 한다.

1. quota 종류별 전역 배치 mutex를 ID별 lock 또는 striped locking으로
   교체한다. 현재 사용자 및 토큰 lock은 Redis Lua 호출 중에도 유지되므로
   서로 관계없는 사용자나 토큰까지 직렬화된다. Redis 호출이 느려지면 lock
   convoy가 발생할 수 있다.
2. 여러 애플리케이션 노드를 실행하기 전에 pending/in-flight quota 버전
   정보를 공유 Redis 상태로 옮긴다. 현재 map과 mutex는 프로세스 로컬
   상태이므로, 다른 노드는 이 노드가 flush하려고 대기 중인 증감분을 볼 수
   없다. 그 결과 삭제된 캐시 항목을 오래된 데이터베이스 스냅샷으로 다시
   채울 수 있다.
3. Redis 없이 `BATCH_UPDATE_ENABLED=true`를 사용하는 경우의 지원 동작을
   정의하고 강제한다. 안전한 방법은 시작 시 해당 구성을 거부하거나,
   Redis를 사용할 수 없을 때 사용자/토큰 quota 변경을 데이터베이스에
   직접 기록하는 것이다.
4. 향후 검토에서 그래프 기반 영향 범위 분석이 필요하면 `/understand`로
   `.understand-anything/knowledge-graph.json`을 생성한다. 이번 검토 당시
   그래프가 없어 의존 관계는 수동으로 추적했다.
5. root 패키지를 포함한 `go test ./...` 전체 성공 결과가 필요하면
   `web/classic/dist`를 복원하거나 빌드한다.

## 저장소 상태 참고

기능 브랜치의 변경은 로컬에 커밋한 뒤 `team` 브랜치에 fast-forward로
병합했다. 최초 문서 작성 당시에는 외부 `origin` 목적지의 소유권 확인이
필요해 push를 수행하지 않았으며, 원격 반영은 별도의 명시적 작업으로
취급했다.
