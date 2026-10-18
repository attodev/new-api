# 스키마 기동 리허설

spec §5: 기존 운영 DB(bigint quota, Toss 테이블 포함)에서 재기반 바이너리가 데이터 손실 없이 기동하는지 확인한다.

로컬 컨테이너로 (Docker 필요):
    ./scripts/schema-rehearsal/run.sh all
운영 사본(이미 team 스키마인 DB)으로 (Docker/team 바이너리 불필요, `mysql` 또는 `psql` 클라이언트가 PATH에 있어야 함):
    LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
    LEGACY_SKIP=1 SQL_DSN='postgres://user:pass@host:5432/db?sslmode=disable' ./scripts/schema-rehearsal/run.sh postgres

`LEGACY_SKIP=1`에서는 `SQL_DSN` 하나만 받으므로 `mysql`/`postgres` 중 하나를 골라야 한다(`all` 불가). 로컬 모드에서는 `SQL_DSN`을 무시하고 compose DB만 쓴다(운영 DB에 시드 UPDATE가 가는 사고 방지).

역방향(롤백) 리허설: `ROLLBACK=1`을 더하면 재기반 바이너리가 마이그레이션한 같은 DB에 team 바이너리를 다시 띄워 "team으로 되돌아가기"가 되는지 확인한다. 두 모드 모두에서 쓸 수 있다(`LEGACY_SKIP=1`이면 이 단계를 위해서만 `LEGACY_REF`(기본 `team`)에서 team 바이너리를 빌드한다. 복제본에 team 브랜치가 없으면 `LEGACY_REF=origin/team`).
    ROLLBACK=1 ./scripts/schema-rehearsal/run.sh all
    ROLLBACK=1 LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
이 단계가 단언하는 것: team 바이너리가 `/api/status`에 응답할 때까지 뜬다, 지갑 행이 그대로다, 네 컬럼이 여전히 bigint다, team 로그에 `failed to migrate`가 없다. team이 모르는 옵션 키 수(`failed to update option map`)는 참고용으로만 센다(재기반 바이너리가 새 키를 쓰므로 0보다 크며 무해). 로그는 `$WORK/rollback-<dialect>.log`. Redis 없이 도는 리허설이므로 Redis 쪽 롤백 절차는 운영 체크리스트 §9를 따른다.

스크립트는 검증을 직접 단언하고, 하나라도 어긋나면 `FAIL: ...`을 출력하며 0이 아닌 코드로 종료한다 (인자 오류/도구 누락은 2, 검증 실패는 1):
- 기동 후 `quota`, `used_quota`, `aff_quota`, `aff_history` 네 컬럼이 모두 bigint
- 지갑 행(가장 작은 user id)이 기동 전후 동일. 로컬 모드는 team 바이너리가 만든 `id=1` 행에 5000000000/3000000000을 시드한 뒤 그 값과 비교하고, `LEGACY_SKIP=1`에서는 실행 전에 읽은 값과 비교한다
- 새 바이너리 로그에 `users.quota uses`, `failed to migrate`가 없음 (넓은 키워드 스캔은 참고용으로만 출력)

새 바이너리는 포트가 비어 있어야(`PORT_NEW`, 기본 3301; team은 `PORT_LEGACY`, 기본 3302) 기동하며, 빈 작업 디렉터리(`$WORK/run-<dialect>/`)에서 실행되어 저장소의 `.env`를 읽지 않는다. 중단(Ctrl-C)해도 띄운 서버는 정리된다.

로컬 모드를 다시 돌릴 때는 먼저 컨테이너를 내려 깨끗한 DB에서 시작한다(남은 DB에서는 "구 스키마에서 시작"이 성립하지 않는다):
    docker compose -f scripts/schema-rehearsal/docker-compose.yml down -v
정리: 위 `down -v`, `git worktree remove --force /tmp/new-api-schema-rehearsal/legacy-src`.
