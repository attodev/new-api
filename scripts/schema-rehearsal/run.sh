#!/usr/bin/env bash
# Rehearse starting the rebased binary against a database created by the
# legacy (team) binary. See README.md. Usage: run.sh [mysql|postgres|all]
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
TARGET="${1:-all}"
WORK="${REHEARSAL_WORKDIR:-/tmp/new-api-schema-rehearsal}"
LEGACY_REF="${LEGACY_REF:-team}"
PORT_NEW="${PORT_NEW:-3301}"
PORT_LEGACY="${PORT_LEGACY:-3302}"
mkdir -p "$WORK"

log() { printf '\n== %s\n' "$*"; }

build_new() {
  log "building rebased binary"
  (cd "$ROOT" && test -f web/dist/index.html || { echo "web/dist/index.html missing: run 'cd web && bun run build'"; exit 1; })
  (cd "$ROOT" && GOWORK=off go build -o "$WORK/new-api-new" .)
}

build_legacy() {
  log "building legacy binary from $LEGACY_REF"
  local wt="$WORK/legacy-src"
  if [ ! -d "$wt" ]; then
    (cd "$ROOT" && git worktree add --detach "$wt" "$LEGACY_REF")
  fi
  mkdir -p "$wt/web/default/dist" "$wt/web/classic/dist"
  touch "$wt/web/default/dist/index.html" "$wt/web/classic/dist/index.html"
  (cd "$wt" && GOWORK=off go build -o "$WORK/new-api-legacy" .)
}

wait_http() { # $1=port
  for _ in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:$1/api/status" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "server on :$1 did not come up"; return 1
}

run_binary() { # $1=binary $2=port $3=dsn $4=logfile
  SQL_DSN="$3" PORT="$2" SESSION_SECRET=rehearsal CRYPTO_SECRET=rehearsalrehearsalrehearsal12 \
    "$1" >"$4" 2>&1 &
  echo $!
}

query() { # $1=dialect $2=sql
  case "$1" in
    mysql) docker exec new-api-rehearsal-mysql mysql -uroot -p123456 -N -e "$2" new_api ;;
    postgres) docker exec new-api-rehearsal-pg psql -U root -d new_api -At -c "$2" ;;
  esac
}

rehearse() { # $1=dialect
  local dialect="$1" dsn col_sql
  case "$dialect" in
    mysql)
      dsn="${SQL_DSN:-root:123456@tcp(127.0.0.1:33061)/new_api?parseTime=true}"
      col_sql="SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='new_api' AND TABLE_NAME='users' AND COLUMN_NAME IN ('quota','used_quota','aff_quota','aff_history') ORDER BY COLUMN_NAME" ;;
    postgres)
      dsn="${SQL_DSN:-postgres://root:123456@127.0.0.1:54321/new_api?sslmode=disable}"
      col_sql="SELECT column_name, data_type FROM information_schema.columns WHERE table_name='users' AND column_name IN ('quota','used_quota','aff_quota','aff_history') ORDER BY column_name" ;;
  esac

  if [ "${LEGACY_SKIP:-0}" != "1" ]; then
    log "[$dialect] legacy binary creates the old schema"
    local pid; pid=$(run_binary "$WORK/new-api-legacy" "$PORT_LEGACY" "$dsn" "$WORK/legacy-$dialect.log")
    wait_http "$PORT_LEGACY"; kill "$pid"; wait "$pid" 2>/dev/null || true
    log "[$dialect] seeding a wallet above 2^31"
    query "$dialect" "UPDATE users SET quota=5000000000, used_quota=3000000000 WHERE id=1"
  fi

  log "[$dialect] column types before"; query "$dialect" "$col_sql"
  log "[$dialect] starting rebased binary"
  local pid; pid=$(run_binary "$WORK/new-api-new" "$PORT_NEW" "$dsn" "$WORK/new-$dialect.log")
  if ! wait_http "$PORT_NEW"; then echo "--- log:"; tail -50 "$WORK/new-$dialect.log"; exit 1; fi
  kill "$pid"; wait "$pid" 2>/dev/null || true
  log "[$dialect] column types after"; query "$dialect" "$col_sql"
  log "[$dialect] wallet after"; query "$dialect" "SELECT id, quota, used_quota FROM users WHERE id=1"
  log "[$dialect] schema errors in log (expect none)"
  grep -i -E "error|failed" "$WORK/new-$dialect.log" | grep -i -E "migrat|column|alter|schema" || echo "(none)"
}

if [ "${LEGACY_SKIP:-0}" != "1" ]; then
  (cd "$HERE" && docker compose up -d --wait)
  build_legacy
fi
build_new
case "$TARGET" in
  mysql|postgres) rehearse "$TARGET" ;;
  all) rehearse mysql; rehearse postgres ;;
  *) echo "usage: run.sh [mysql|postgres|all]"; exit 2 ;;
esac
log "done; logs in $WORK"
