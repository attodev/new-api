# upstream 재기반 단계 B(O3 조직 관리) 기록

브랜치: develop. 계획: docs/superpowers/plans/2026-10-14-rebase-phase-b.md

## 1. 백엔드 (Task 1–7)

`git log --format='%h %s' 149c567a1..HEAD` 기준(오래된 순). Task 번호는 제목으로 추정했다.

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | 37d4659f7 | feat(organization): port the organization, plan and subscription models onto upstream |
| 2 | aad37212b | feat(organization): settle top-ups into the organization wallet by target |
| 2 | 0b4bfee8e | fix(organization): refuse to settle organization orders without a target |
| 3 | 5c2bb1301 | feat(organization): charge organization members against the member limit, organization wallet and organization plans |
| 3 | a16f417b7 | fix(organization): reserve organization funds atomically and bill legacy Midjourney against the organization |
| 3 | d1886beea | fix(organization): reject legacy Midjourney requests from plan members and empty organization wallets before the upstream call |
| 4 | 9a664cd4a | feat(organization): add member-scoped log queries and the organization member Excel service |
| 4 | 6f1d15b89 | fix(organization): keep the member group read-only on import and protect platform admins from remove-absent |
| 5 | b09a802ab | feat(organization): port the organization handlers, routes, casbin resource and audit entries |
| 5 | 4b8456c8a | fix(organization): close self-service quota, owner-targeting, hash-disclosure and order-by holes in the organization API |
| 5 | dd71f5dce | fix(organization): let root manage the owner's plan and scope membership clears to the organization |
| 6 | 5dfb77f71 | test(organization): port the organization controller and payment capacity tests |

- 검증: `GOWORK=off go vet ./...` 종료 코드 0(출력 없음). `make test` 종료 코드 0, 패키지 100개(루트 + relaykit + tokenkit 합산) — `ok` 54, `no test files` 46, FAIL 0. 단계 A 기준선(54/46/0)과 같은 패키지 구성이며, 조직 테스트는 기존 패키지(model, service, controller, router, service/authz)에 추가됐다.
- upstream 훅 수: `docs/fork-overlays.md`의 O3 행 54개(`grep -c "O3" docs/fork-overlays.md`). 모든 O3 행의 줄 번호를 트리와 대조했고 어긋난 행은 없었다.
- MySQL/PostgreSQL 실행: 미실행(컨테이너 없음). 실행 명령: `TEST_MYSQL_DSN=… TEST_POSTGRES_DSN=… GOWORK=off go test ./service/ ./model/ ./controller/ -run Organization`

## 2. 프런트엔드 (Task 8–12)
(단계 8–13에서 채운다)

## 3. 결정 사항
- 조직 target이 없는 조직형 충전 주문은 정산을 거부한다. `EffectiveTargetId`가 결제자 id로 되돌아가지 않는다.
- 이미지 수량 예약과 조직 구독 선차감은 조직 지갑을 원자적으로 예약하고, 부족하면 403을 돌려준다(team은 음수 잔액을 허용했다).
- 레거시 Midjourney는 upstream 호출 전에 조직 플랜 사용자와 빈 조직 지갑을 거부한다. 멤버 한도 + 조직 지갑에 과금하며, 환급은 현재 소속 조직 기준으로 한다.
- 조직 Excel 가져오기는 `group`을 쓰지 않고(내보내기 전용), `owner` 행은 오류로 처리하며, 없는 멤버 제거(remove-absent)는 플랫폼 관리자를 건드리지 않는다.
- 조직 API 보안 수정: 자기 자신 수정·제거 금지, 소유자 대상 변경은 소유자만 가능(구독 할당·취소는 root도 가능), `owner` 역할은 membership PUT으로 부여할 수 없음, 멤버 제거·조직 삭제 시 떠나는 사용자의 `users.quota`를 0으로 만들고(감사 로그에 previous_quota 기록), 목록 API는 제한된 컬럼만 조회하며, `ORDER BY`는 dialect별 quoting을 쓴다.
- `/api/organization/*` 접근 토큰 규칙: 기본은 세션 전용이고, 지갑 조회·충전만 wallet:read/write, subscription/self는 profile:read다. `/api/organizations/*`는 casbin `organization` 리소스를 쓴다.
- 조직 변경 핸들러 12개에 `organization.*` 감사 항목을 남긴다.
- 라우트 수는 38이다(계획의 42는 계산 오류).
- 조직 필드는 사용자 캐시에 넣지 않는다.
- MySQL/PostgreSQL 실행은 미실행이다(컨테이너 없음).

## 4. 단계 C로 넘기는 것
(단계 8–13에서 채운다)
