# 스키마 기동 리허설

spec §5: 기존 운영 DB(bigint quota, Toss 테이블 포함)에서 재기반 바이너리가 데이터 손실 없이 기동하는지 확인한다.

로컬 컨테이너로:
    ./scripts/schema-rehearsal/run.sh all
운영 사본(이미 team 스키마인 DB)으로:
    LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
    LEGACY_SKIP=1 SQL_DSN='postgres://...' ./scripts/schema-rehearsal/run.sh postgres

통과 기준: "column types after"가 bigint, "wallet after"가 5000000000/3000000000, 로그에 schema 오류 없음.
정리: `docker compose -f scripts/schema-rehearsal/docker-compose.yml down -v`, `git worktree remove /tmp/new-api-schema-rehearsal/legacy-src`.
