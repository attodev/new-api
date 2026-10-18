#!/usr/bin/env bash
# Rehearse starting the rebased binary against a database created by the
# legacy (team) binary. See README.md. Usage: run.sh [mysql|postgres|all]
#
# Local mode (default): docker compose databases, team binary creates the old
# schema, a wallet above 2^31 is seeded, then the rebased binary starts.
# LEGACY_SKIP=1: no docker, no team binary; SQL_DSN points at an existing
# (copy of a) team database and only the rebased binary is started. Needs the
# mysql / psql client on PATH.
#
# ROLLBACK=1: after the rebased binary has run, start the legacy (team) binary
# again on the same database and assert it still boots, keeps the wallet and
# logs no migration error - the "go back to team" path. Works in both modes
# (with LEGACY_SKIP=1 it builds the legacy binary from LEGACY_REF just for
# this step).
#
# Every check is asserted: any failure prints "FAIL: ..." and exits 1.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
TARGET="${1:-all}"
WORK="${REHEARSAL_WORKDIR:-/tmp/new-api-schema-rehearsal}"
LEGACY_REF="${LEGACY_REF:-team}"
PORT_NEW="${PORT_NEW:-3301}"
PORT_LEGACY="${PORT_LEGACY:-3302}"
LEGACY_SKIP="${LEGACY_SKIP:-0}"
ROLLBACK="${ROLLBACK:-0}"

log() { printf '\n== %s\n' "$*"; }
pass() { printf 'PASS: %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
die_usage() { printf 'ERROR: %s\n' "$*" >&2; exit 2; }
usage() { echo "usage: run.sh [mysql|postgres|all]"; exit 2; }

# ---- argument validation (before anything touches docker, builds or disk) ----
case "$TARGET" in
  mysql) DIALECTS=(mysql) ;;
  postgres) DIALECTS=(postgres) ;;
  all) DIALECTS=(mysql postgres) ;;
  *) usage ;;
esac
if [ "$LEGACY_SKIP" = "1" ]; then
  [ -n "${SQL_DSN:-}" ] || die_usage "LEGACY_SKIP=1 requires SQL_DSN"
  [ "$TARGET" != "all" ] || die_usage "LEGACY_SKIP=1 takes one SQL_DSN, so pick mysql or postgres (not all)"
else
  command -v docker >/dev/null 2>&1 || die_usage "docker not found: local mode needs Docker (or set LEGACY_SKIP=1 with SQL_DSN to rehearse an existing database)"
  if [ -n "${SQL_DSN:-}" ]; then
    echo "note: SQL_DSN is ignored in local mode (it is only used with LEGACY_SKIP=1)"
  fi
fi
command -v curl >/dev/null 2>&1 || die_usage "curl not found"

# ---- DSN parsing ----
# MySQL: user:pass@tcp(host[:port])/db?params -> MYSQL_USER/PASS/HOST/PORT/DB
parse_mysql_dsn() {
  local re='^([^:@]+)(:(.*))?@tcp\(([^:)]+)(:([0-9]+))?\)/([^?]+)'
  [[ $1 =~ $re ]] || return 1
  MYSQL_USER="${BASH_REMATCH[1]}"
  MYSQL_PASS="${BASH_REMATCH[3]}"
  MYSQL_HOST="${BASH_REMATCH[4]}"
  MYSQL_PORT="${BASH_REMATCH[6]:-3306}"
  MYSQL_DB="${BASH_REMATCH[7]}"
}

# PostgreSQL: postgres://user[:pass]@host[:port]/db?params -> PG_USER/PASS/HOST/PORT/DB
parse_pg_dsn() {
  local re='^postgres(ql)?://([^:@/]+)(:([^@]*))?@([^:/?]+)(:([0-9]+))?/([^?]+)'
  [[ $1 =~ $re ]] || return 1
  PG_USER="${BASH_REMATCH[2]}"
  PG_PASS="${BASH_REMATCH[4]}"
  PG_HOST="${BASH_REMATCH[5]}"
  PG_PORT="${BASH_REMATCH[7]:-5432}"
  PG_DB="${BASH_REMATCH[8]}"
}

# Sets DSN, COL_SQL and BACKEND (client|docker) for a dialect.
prepare_dialect() { # $1=dialect
  local default client
  case "$1" in
    mysql)
      default='root:123456@tcp(127.0.0.1:33061)/new_api?parseTime=true'
      client=mysql ;;
    postgres)
      default='postgres://root:123456@127.0.0.1:54321/new_api?sslmode=disable'
      client=psql ;;
  esac
  if [ "$LEGACY_SKIP" = "1" ]; then DSN="$SQL_DSN"; else DSN="$default"; fi

  case "$1" in
    mysql)
      parse_mysql_dsn "$DSN" || die_usage "cannot parse the MySQL DSN (expected user:pass@tcp(host:port)/db?params)"
      COL_SQL="SELECT COLUMN_NAME, COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='$MYSQL_DB' AND TABLE_NAME='users' AND COLUMN_NAME IN ('quota','used_quota','aff_quota','aff_history') ORDER BY COLUMN_NAME" ;;
    postgres)
      parse_pg_dsn "$DSN" || die_usage "cannot parse the PostgreSQL DSN (expected postgres://user:pass@host:port/db?params)"
      COL_SQL="SELECT column_name, data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='users' AND column_name IN ('quota','used_quota','aff_quota','aff_history') ORDER BY column_name" ;;
  esac

  if command -v "$client" >/dev/null 2>&1; then
    BACKEND=client
  elif [ "$LEGACY_SKIP" = "1" ]; then
    die_usage "$client client required: with LEGACY_SKIP=1 the database is not a local compose container, so the schema checks need a local $client binary on PATH"
  else
    BACKEND=docker
  fi
}

query() { # $1=dialect $2=sql ; uses the globals set by prepare_dialect
  case "$1" in
    mysql)
      if [ "$BACKEND" = client ]; then
        MYSQL_PWD="$MYSQL_PASS" mysql --protocol=TCP -h "$MYSQL_HOST" -P "$MYSQL_PORT" -u "$MYSQL_USER" -N -e "$2" "$MYSQL_DB"
      else
        docker exec -e MYSQL_PWD="$MYSQL_PASS" new-api-rehearsal-mysql mysql -u"$MYSQL_USER" -N -e "$2" "$MYSQL_DB"
      fi ;;
    postgres)
      if [ "$BACKEND" = client ]; then
        psql "$DSN" -v ON_ERROR_STOP=1 -At -c "$2"
      else
        docker exec -e PGPASSWORD="$PG_PASS" new-api-rehearsal-pg psql -U "$PG_USER" -d "$PG_DB" -v ON_ERROR_STOP=1 -At -c "$2"
      fi ;;
  esac
}

# Normalize client output (tab or | separated) to single-space separated fields.
norm() { tr '|\t' '  ' | awk '{$1=$1}1'; }

wallet_row() { # $1=dialect -> "id quota used_quota" of the lowest user id
  local out
  out=$(query "$1" "SELECT id, quota, used_quota FROM users ORDER BY id LIMIT 1") || fail "wallet query failed"
  printf '%s\n' "$out" | norm
}

# ---- process control ----
SERVER_PID=""

stop_binary() {
  local pid="$SERVER_PID"
  [ -n "$pid" ] || return 0
  kill "$pid" 2>/dev/null || true
  for _ in $(seq 1 30); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  if kill -0 "$pid" 2>/dev/null; then
    echo "server $pid ignored TERM for 30s, killing"
    kill -KILL "$pid" 2>/dev/null || true
  fi
  wait "$pid" 2>/dev/null || true
  SERVER_PID=""
}

cleanup() { stop_binary || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

port_free() { # $1=port
  if command -v lsof >/dev/null 2>&1; then
    ! lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  elif command -v nc >/dev/null 2>&1; then
    ! nc -z 127.0.0.1 "$1" >/dev/null 2>&1
  else
    return 0
  fi
}

# Starts the server in this shell (SERVER_PID is its real pid) from an empty
# directory so a repo-root .env cannot leak settings into the rehearsal.
start_binary() { # $1=dialect $2=binary $3=port $4=dsn $5=logfile
  local rundir="$WORK/run-$1"
  port_free "$3" || fail "port $3 is already in use (stale server?); free it or set PORT_NEW/PORT_LEGACY"
  rm -rf "$rundir"; mkdir -p "$rundir"
  (cd "$rundir" && exec env -u REDIS_CONN_STRING SQL_DSN="$4" PORT="$3" SESSION_SECRET=rehearsal CRYPTO_SECRET=rehearsalrehearsalrehearsal12 "$2") >"$5" 2>&1 &
  SERVER_PID=$!
}

wait_http() { # $1=port $2=pid $3=logfile
  for _ in $(seq 1 60); do
    if ! kill -0 "$2" 2>/dev/null; then
      echo "server (pid $2) exited before becoming ready; log tail:"; tail -50 "$3"; return 1
    fi
    if curl -fsS "http://127.0.0.1:$1/api/status" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "server on :$1 did not come up within 60s; log tail:"; tail -50 "$3"; return 1
}

# ---- builds ----
build_new() {
  log "building rebased binary"
  (cd "$ROOT" && test -f web/dist/index.html || { echo "web/dist/index.html missing: run 'cd web && bun run build'"; exit 1; })
  (cd "$ROOT" && GOWORK=off go build -o "$WORK/new-api-new" .)
}

build_legacy() {
  log "building legacy binary from $LEGACY_REF"
  local wt="$WORK/legacy-src"
  (cd "$ROOT" && git rev-parse --verify --quiet "$LEGACY_REF^{commit}" >/dev/null) || die_usage "LEGACY_REF '$LEGACY_REF' is not a commit in this clone (fetch the team branch or set LEGACY_REF=origin/team)"
  if [ ! -d "$wt" ]; then
    (cd "$ROOT" && git worktree add --detach "$wt" "$LEGACY_REF")
  fi
  mkdir -p "$wt/web/default/dist" "$wt/web/classic/dist"
  touch "$wt/web/default/dist/index.html" "$wt/web/classic/dist/index.html"
  (cd "$wt" && GOWORK=off go build -o "$WORK/new-api-legacy" .)
}

# ---- the rehearsal ----
rehearse() { # $1=dialect
  local d="$1" want_wallet got cnt rows n bad logf
  prepare_dialect "$d"
  logf="$WORK/new-$d.log"

  if [ "$LEGACY_SKIP" != "1" ]; then
    log "[$d] legacy binary creates the old schema"
    start_binary "$d" "$WORK/new-api-legacy" "$PORT_LEGACY" "$DSN" "$WORK/legacy-$d.log"
    wait_http "$PORT_LEGACY" "$SERVER_PID" "$WORK/legacy-$d.log" || fail "[$d] legacy binary did not start"
    stop_binary

    log "[$d] seeding a wallet above 2^31"
    cnt=$(query "$d" "SELECT COUNT(*) FROM users WHERE id=1" | norm) || fail "[$d] user count query failed"
    [ "$cnt" = "1" ] || fail "[$d] expected user id=1 to exist after the legacy start (found $cnt rows); cannot seed"
    query "$d" "UPDATE users SET quota=5000000000, used_quota=3000000000 WHERE id=1" >/dev/null || fail "[$d] seed UPDATE failed"
    want_wallet="1 5000000000 3000000000"
    got=$(wallet_row "$d")
    [ "$got" = "$want_wallet" ] || fail "[$d] seed did not take: wallet is '$got', wanted '$want_wallet'"
    pass "[$d] seeded wallet '$want_wallet'"
  else
    want_wallet=$(wallet_row "$d")
    [ -n "$want_wallet" ] || fail "[$d] users table has no rows; nothing to compare the wallet against"
    echo "[$d] wallet before (must be unchanged after): $want_wallet"
  fi

  log "[$d] column types before"; query "$d" "$COL_SQL" || fail "[$d] column query failed"
  log "[$d] starting rebased binary"
  start_binary "$d" "$WORK/new-api-new" "$PORT_NEW" "$DSN" "$logf"
  wait_http "$PORT_NEW" "$SERVER_PID" "$logf" || fail "[$d] rebased binary did not start"
  stop_binary

  log "[$d] column types after"
  rows=$(query "$d" "$COL_SQL") || fail "[$d] column query failed"
  printf '%s\n' "$rows"
  rows=$(printf '%s\n' "$rows" | norm)
  n=$(printf '%s\n' "$rows" | awk 'NF>=2' | wc -l | tr -d ' ')
  bad=$(printf '%s\n' "$rows" | awk 'NF>=2 && $2 !~ /^bigint/ {print $1 "=" $2}' | tr '\n' ' ')
  [ "$n" = "4" ] || fail "[$d] expected 4 wallet columns (quota, used_quota, aff_quota, aff_history), found $n"
  [ -z "$bad" ] || fail "[$d] wallet columns are not bigint after startup: $bad"
  pass "[$d] all 4 wallet columns are bigint"

  log "[$d] wallet after"
  got=$(wallet_row "$d")
  echo "$got"
  [ "$got" = "$want_wallet" ] || fail "[$d] wallet changed: '$got' (wanted '$want_wallet')"
  pass "[$d] wallet preserved ('$got')"

  log "[$d] schema errors in log"
  if grep -q -F -e "users.quota uses" -e "failed to migrate" "$logf"; then
    grep -F -e "users.quota uses" -e "failed to migrate" "$logf" | head -5
    fail "[$d] schema/migration error in $logf"
  fi
  pass "[$d] no fatal schema error in the log"
  echo "informational (broader keyword scan):"
  grep -i -E "error|failed" "$logf" | grep -i -E "migrat|column|alter|schema" || echo "(none)"

  [ "$ROLLBACK" = "1" ] || return 0
  rollback_rehearse "$d" "$want_wallet"
}

# Reverse step: the legacy binary starts on the database the rebased binary
# has just migrated. Asserts: it becomes ready, the wallet row is unchanged,
# the four wallet columns are still bigint, and its log has no migration error.
rollback_rehearse() { # $1=dialect $2=expected wallet row
  local d="$1" want_wallet="$2" got rows n bad logf unknown
  logf="$WORK/rollback-$d.log"

  log "[$d] ROLLBACK: starting legacy binary on the migrated database"
  start_binary "$d" "$WORK/new-api-legacy" "$PORT_LEGACY" "$DSN" "$logf"
  wait_http "$PORT_LEGACY" "$SERVER_PID" "$logf" || fail "[$d] ROLLBACK: legacy binary did not start on the migrated database"
  stop_binary
  pass "[$d] ROLLBACK: legacy binary became ready"

  log "[$d] ROLLBACK: column types after legacy restart"
  rows=$(query "$d" "$COL_SQL") || fail "[$d] ROLLBACK: column query failed"
  printf '%s\n' "$rows"
  rows=$(printf '%s\n' "$rows" | norm)
  n=$(printf '%s\n' "$rows" | awk 'NF>=2' | wc -l | tr -d ' ')
  bad=$(printf '%s\n' "$rows" | awk 'NF>=2 && $2 !~ /^bigint/ {print $1 "=" $2}' | tr '\n' ' ')
  [ "$n" = "4" ] || fail "[$d] ROLLBACK: expected 4 wallet columns, found $n"
  [ -z "$bad" ] || fail "[$d] ROLLBACK: wallet columns are not bigint after the legacy restart: $bad"
  pass "[$d] ROLLBACK: all 4 wallet columns are still bigint"

  log "[$d] ROLLBACK: wallet after legacy restart"
  got=$(wallet_row "$d")
  echo "$got"
  [ "$got" = "$want_wallet" ] || fail "[$d] ROLLBACK: wallet changed: '$got' (wanted '$want_wallet')"
  pass "[$d] ROLLBACK: wallet preserved ('$got')"

  log "[$d] ROLLBACK: schema errors in legacy log"
  if grep -q -F -e "failed to migrate" "$logf"; then
    grep -F -e "failed to migrate" "$logf" | head -5
    fail "[$d] ROLLBACK: migration error in $logf"
  fi
  pass "[$d] ROLLBACK: no migration error in the legacy log"
  unknown=$(grep -c -F "failed to update option map" "$logf" || true)
  echo "informational: option keys the legacy binary did not recognise: $unknown (expected > 0 after a rebased run; harmless)"
  echo "informational (broader keyword scan):"
  grep -i -E "error|failed" "$logf" | grep -i -E "migrat|column|alter|schema" || echo "(none)"
}

mkdir -p "$WORK"
for d in "${DIALECTS[@]}"; do prepare_dialect "$d"; done  # fail early on bad DSNs / missing clients

if [ "$LEGACY_SKIP" != "1" ]; then
  log "starting database containers"
  docker compose -f "$HERE/docker-compose.yml" up -d --wait "${DIALECTS[@]}"
  build_legacy
elif [ "$ROLLBACK" = "1" ]; then
  build_legacy
fi
build_new
for d in "${DIALECTS[@]}"; do rehearse "$d"; done
if [ "$ROLLBACK" = "1" ]; then
  log "PASS: rehearsal (forward + rollback) finished; logs in $WORK"
else
  log "PASS: rehearsal finished; logs in $WORK"
fi
