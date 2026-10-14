# upstream 재기반 단계 A 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `qn/main`(63cecab82) 위에 새 브랜치 `rebase/upstream-2026-10`를 만들고, 독립 overlay(O1 랜딩, O2 PayPal, O4 내보내기, O5 문의 메일, O8 소형 설정, O10 진단 도구)와 quota bigint 보호, 스키마 리허설을 올려 "독립 기능이 동작하는 빌드"를 만든다.

**Architecture:** upstream 파일은 "훅 한 줄 + `// fork:` 주석"으로만 건드리고 로직은 fork 전용 파일(`router/fork-router.go`, `router/landing-router.go`, `controller/contact.go`, `model/topup_paypal.go` 등)에 둔다. team의 코드는 `git show team:<path>`로 가져온 뒤 upstream API(64-bit `int` 지갑, `paymentError`/`common.NewMessage`, 세션 쿠키 없음)에 맞춰 고친다. 모든 훅은 `docs/fork-overlays.md`에 기록한다.

**Tech Stack:** Go 1.26 (`GOWORK=off`), Gin, GORM 1.25.12, excelize v2.10.1, Bun 1.4 + rsbuild + vitest, Docker(MySQL 8 / PostgreSQL 15, 리허설 전용).

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` (§2.1 O1·O2·O4·O5·O8·O10, §3 규칙, §4.1–4.3, §4.5, §5, §7, §8 단계 A)

## Global Constraints

- 베이스 커밋: `qn/main` = `63cecab82` (2026-10-13). 브랜치 이름 `rebase/upstream-2026-10`. `team`은 건드리지 않는다.
- 모듈 경로 `github.com/QuantumNous/new-api` 유지, 브랜딩·저작권 문구 수정 금지 (CLAUDE.md Rule 5). 새 Go/TS 파일에는 upstream과 같은 AGPL 헤더 주석을 붙인다(`web/`는 `bun run copyright:check`가 검사한다).
- JSON은 `common.Marshal/Unmarshal/DecodeJson/UnmarshalJsonStr`만 사용 (Rule 1). `encoding/json`은 `json.RawMessage` 타입 참조만 허용.
- DB 코드는 SQLite·MySQL ≥ 5.7.8·PostgreSQL ≥ 9.6 모두 동작 (Rule 2).
- upstream 파일 수정은 훅 한 줄 또는 구조체 필드 추가 + `// fork: O<n>` 주석으로 제한하고, 수정한 파일·줄을 `docs/fork-overlays.md`에 적는다 (spec §3).
- 지갑 수치는 upstream 도메인을 쓴다: Go `int`, `common.MaxWalletQuota = 1<<53-1`, `common.ValidateWalletQuota(int)`, `common.WalletQuotaFromDecimalStrict`. `common.MaxQuota`(=MaxInt32)는 단일 요청 과금 상한이므로 지갑에 쓰지 않는다 (spec §2.2).
- Go 테스트 명령: 루트에서 `GOWORK=off go test ./<pkg>/ -run <Name>`; 전체는 `make test`. `web/dist/index.html`이 없으면 루트 패키지 빌드가 실패하므로 Task 1에서 만든다.
- 프런트 명령: `cd web && bun run typecheck && bun run test && bun run lint && bun run copyright:check`. 새 영문 키는 `web/src/i18n/locales/en.json`에 넣고 `bun run i18n:sync`로 다른 로케일에 전파한다.
- 커밋 메시지 끝에 `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. team에서 옮긴 코드는 본문에 `Ported-From: team <path>`를 적는다.
- Toss 관련 코드·테이블·프런트 조각은 절대 가져오지 않는다 (spec §2.2).

## Review Focus

spec이 말하지 않지만 사용자가 부딪힐 입력. 각 줄의 테스트는 해당 Task에 들어 있다.

1. **잔액이 2^31을 넘는 운영 DB로 기동** — 기동 후 `users.quota`가 bigint로 남고 값이 보존되어야 한다. 좁히면 MySQL strict 모드에서 ALTER가 실패하거나 값이 잘린다. → Task 2 `TestUserQuotaColumnsSurviveAutoMigrate`, Task 3 리허설.
2. **랜딩 문의 폼 제출** — 폼은 `notes`·`features[]`·`scale_type` 등을 보내는데 team 백엔드는 `message`를 요구해 모든 문의가 조용히 버려졌다. 새 백엔드는 폼의 실제 필드를 받고, 프런트는 `success` 필드를 확인해야 한다. → Task 5 `TestContactSendsLandingFormFields`, Task 13 landing.js 수정.
3. **관리자(비-root)가 Excel로 root/admin 계정을 가져오기** — 권한 상승이 되면 안 된다. → Task 7 `TestImportUsersRefusesRolesTheCallerCannotManage`.
4. **PayPal webhook이 설정 미완료 상태로 호출됨** — 결제 준수 확인 전이거나 Webhook ID가 비어 있으면 403으로 거절해야 한다(다른 제공자와 동일). → Task 10 `TestPayPalWebhookRejectedWhenDisabled`.
5. **로그인한 사용자가 랜딩에 접근** — upstream에는 서버 세션이 없으므로 `new_api_has_session` 힌트 쿠키만으로 "로그인됨"을 판단한다. 사용자명·역할 배지는 더 이상 렌더하지 않는다. → Task 13 `TestLandingPagesReflectSessionHint`.

---

## 파일 구조

새로 만드는 fork 전용 파일(모두 team에서 복사하거나 새로 작성):

| 파일 | 책임 |
|---|---|
| `docs/fork-overlays.md` | upstream 파일에 넣은 훅·필드 목록 (규칙 3) |
| `docs/2026-10-14-rebase-phase-a.md` | 기준선·리허설 결과·커밋 표 기록 |
| `model/user_quota_columns_fork_test.go` | users quota 컬럼 bigint 태그 보호 테스트 |
| `scripts/schema-rehearsal/{docker-compose.yml,run.sh,README.md}` | 운영 DB 사본 기동 리허설 |
| `tools/unode-diagnostics/**` | O10, 그대로 복사 |
| `controller/contact.go`, `controller/email.go`, `controller/contact_test.go` | O5 |
| `router/fork-router.go` | O2·O4·O5 라우트 등록 함수 3개 |
| `model/user_export.go`, `service/user_export.go`, `service/user_export_test.go`, `controller/user_export.go` | O4 |
| `setting/payment_paypal.go`, `model/topup_paypal.go`, `model/topup_paypal_test.go`, `controller/payment_availability_paypal.go`, `controller/topup_paypal.go`, `controller/topup_paypal_test.go` | O2 |
| `web/landing/templates/*.tmpl`, `web/landing/public/**`, `router/landing-router.go`, `router/landing_router_test.go` | O1 |
| `controller/oauth_email_domain.go`(+test), `model/log_other_fork.go`(+test) | O8 백엔드 |
| `web/src/features/users/components/users-import-dialog.tsx`(+test) | O4 프런트 |
| `web/src/lib/__tests__/number-suffix.test.ts` | O8 프런트 |

upstream 파일에 넣는 훅(각 Task에 정확한 줄이 있다): `go.mod`, `main.go`, `model/user.go`, `model/topup.go`, `model/option.go`, `model/log_other.go`, `common/constants.go`, `controller/topup.go`, `controller/oauth.go`, `service/log_info_generate.go`, `service/task_billing.go`, `router/api-router.go`, `router/web-router.go`, `.gitignore`, `web/src/{features/wallet/*, features/system-settings/*, features/users/*, features/pricing/*, features/rankings/*, routes/pricing/$modelId/index.tsx, lib/currency.ts, lib/format.ts, i18n/locales/*.json}`.

---

### Task 1: 브랜치·의존성·기준선

**Files:**
- Create: `docs/fork-overlays.md`, `docs/2026-10-14-rebase-phase-a.md`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces: 브랜치 `rebase/upstream-2026-10`, `web/dist/`(로컬 빌드 산출물, 커밋 안 함), 두 기록 문서의 골격.

- [ ] **Step 1: 브랜치 만들기**

```bash
git fetch qn
git switch -c rebase/upstream-2026-10 qn/main
git log --oneline -1   # 63cecab82 이어야 한다
```

- [ ] **Step 2: 프런트 빌드로 `web/dist` 만들기 (루트 패키지 embed용)**

```bash
cd web && bun install --frozen-lockfile && bun run build && cd ..
test -f web/dist/index.html
```

- [ ] **Step 3: Go 기준선 테스트 실행 후 결과 기록**

```bash
GOWORK=off go build ./... && make test 2>&1 | tee /tmp/phase-a-go-baseline.txt | tail -40
```

Expected: 전부 `ok` 또는 `no test files`. 실패가 있으면 그 패키지 이름과 메시지를 Step 6의 문서에 "upstream 기준선 실패"로 적고 계속한다(우리가 고칠 대상이 아니다).

- [ ] **Step 4: 프런트 기준선**

```bash
cd web && bun run typecheck && bun run test 2>&1 | tail -15 && bun run lint && cd ..
```

Expected: typecheck 0 errors, vitest 전부 pass.

- [ ] **Step 5: excelize 추가**

```bash
GOWORK=off go get github.com/xuri/excelize/v2@v2.10.1
GOWORK=off go mod tidy
git diff --stat go.mod go.sum
```

Expected: `go.mod`에 `github.com/xuri/excelize/v2 v2.10.1` 한 줄(+ 간접 의존성). `relaykit`/`tokenkit` replace 줄은 그대로.

- [ ] **Step 6: 기록 문서 두 개 작성**

`docs/fork-overlays.md`:

```markdown
# fork overlays — upstream 파일에 넣은 훅 목록

upstream(QuantumNous/new-api)과 동기화할 때 충돌이 나는 파일은 이 목록과 일치해야 한다.
규칙: upstream 파일에는 "훅 한 줄" 또는 "구조체 필드 + `// fork: O<n>` 주석"만 둔다. 로직은 fork 전용 파일에 둔다.

| overlay | upstream 파일 | 훅 | fork 전용 파일 |
|---|---|---|---|
| (Task별로 채운다) | | | |

## 빌드·테스트 전제
- `web/dist/index.html`이 있어야 루트 패키지가 빌드된다 (`cd web && bun run build`).
- `make test`는 `GOWORK=off`로 루트·relaykit·tokenkit을 돈다.
```

`docs/2026-10-14-rebase-phase-a.md`:

```markdown
# upstream 재기반 단계 A 기록

베이스: qn/main 63cecab82. 브랜치: rebase/upstream-2026-10. 계획: docs/superpowers/plans/2026-10-14-rebase-phase-a.md

## 1. 기준선 (Task 1)
- Go: (make test 결과 요약 — 패키지 수, 실패 목록)
- 프런트: (typecheck / vitest 결과)

## 2. 스키마 리허설 (Task 3)
(Task 3에서 채운다)

## 3. 커밋 표
| Task | 커밋 | 내용 |
|---|---|---|

## 4. upstream 병합 리허설 (Task 16)
```

Step 3·4의 결과를 §1에 채운다.

- [ ] **Step 7: 커밋**

```bash
git add go.mod go.sum docs/fork-overlays.md docs/2026-10-14-rebase-phase-a.md
git commit -m "chore(rebase): start rebase/upstream-2026-10 on qn/main with excelize and baseline record

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: users quota 컬럼 bigint 보호

upstream `model/user.go:97-104`는 `Quota`·`UsedQuota`·`AffQuota`·`AffHistoryQuota`에 `gorm:"type:int"` 태그를 두면서도 기동 시 `ensureUserQuotaColumns`가 bigint를 요구한다. GORM 1.25.12 `MigrateColumn`은 모델 타입 `int default 0`이 DB 타입 `bigint`(MySQL) / `int8`→`bigint`(PostgreSQL)와 prefix가 맞지 않으면 ALTER를 내므로, 우리 bigint 컬럼을 int로 **좁힌다**. 태그를 bigint로 바꿔 막는다(spec §5의 "좁히면 패치" 조건이 확인됨).

**Files:**
- Modify: `model/user.go:97-104`
- Create: `model/user_quota_columns_fork_test.go`

**Interfaces:**
- Consumes: `model.chooseDB(envKey string, isLog bool) (*gorm.DB, ?, error)`(upstream, `model/migration_dialector_test.go:79`에서 쓰는 형태 그대로), `model.userQuotaColumns`, `model.is64BitIntegerType(dbType, dataType)`.

- [ ] **Step 1: 태그 테스트 작성**

`model/user_quota_columns_fork_test.go`:

```go
package model

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// fork: wallet columns must stay bigint (docs/fork-overlays.md, spec §5).
func TestUserQuotaColumnsDeclareBigint(t *testing.T) {
	userType := reflect.TypeOf(User{})
	for _, name := range []string{"Quota", "UsedQuota", "AffQuota", "AffHistoryQuota"} {
		field, ok := userType.FieldByName(name)
		require.True(t, ok, name)
		require.Contains(t, field.Tag.Get("gorm"), "type:bigint",
			"User.%s must declare type:bigint so AutoMigrate never narrows the wallet column", name)
	}
}

func TestUserQuotaColumnsSurviveAutoMigrate(t *testing.T) {
	cases := []struct {
		dialect string
		env     string
		dbType  common.DatabaseType
	}{
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	}
	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			t.Setenv("QUOTA_WIDTH_TEST_DSN", dsn)
			db, _, err := chooseDB("QUOTA_WIDTH_TEST_DSN", false)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			const table = "users_quota_width_test"
			t.Cleanup(func() { _ = db.Migrator().DropTable(table) })
			require.NoError(t, db.Table(table).AutoMigrate(&User{}))
			// A second pass is what production startup does; it must not narrow.
			require.NoError(t, db.Table(table).AutoMigrate(&User{}))

			columns, err := db.Table(table).Migrator().ColumnTypes(&User{})
			require.NoError(t, err)
			seen := 0
			for _, column := range columns {
				for _, expected := range userQuotaColumns {
					if strings.EqualFold(column.Name(), expected) {
						seen++
						require.True(t, is64BitIntegerType(tc.dbType, column.DatabaseTypeName()),
							"%s is %s", expected, column.DatabaseTypeName())
					}
				}
			}
			require.Equal(t, len(userQuotaColumns), seen)

			big := &User{Username: "quota_width_" + tc.dialect, Quota: 5_000_000_000, Status: common.UserStatusEnabled}
			require.NoError(t, db.Table(table).Create(big).Error)
			var got User
			require.NoError(t, db.Table(table).Where("id = ?", big.Id).First(&got).Error)
			require.Equal(t, 5_000_000_000, got.Quota)
		})
	}
}
```

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./model/ -run TestUserQuotaColumnsDeclareBigint -v
```

Expected: FAIL, `User.Quota must declare type:bigint`.

- [ ] **Step 3: 태그 수정**

`model/user.go` 97-104줄의 네 필드를 다음으로 바꾼다(Go 타입 `int`는 유지, `RequestCount`·`AffCount`·`InviterId`는 건드리지 않는다):

```go
	Quota                int                        `json:"quota" gorm:"type:bigint;default:0"`                              // fork: wallet columns stay bigint (docs/fork-overlays.md)
	UsedQuota            int                        `json:"used_quota" gorm:"type:bigint;default:0;column:used_quota"`       // fork: bigint
	RequestCount         int                        `json:"request_count" gorm:"type:int;default:0;"`               // request number
	Group                string                     `json:"group" gorm:"type:varchar(64);default:'default'"`
	AffCode              string                     `json:"aff_code" gorm:"type:varchar(32);column:aff_code;uniqueIndex"`
	AffCount             int                        `json:"aff_count" gorm:"type:int;default:0;column:aff_count"`
	AffQuota             int                        `json:"aff_quota" gorm:"type:bigint;default:0;column:aff_quota"`         // fork: bigint
	AffHistoryQuota      int                        `json:"aff_history_quota" gorm:"type:bigint;default:0;column:aff_history"` // fork: bigint
```

- [ ] **Step 4: 통과 확인 + SQLite 회귀**

```bash
GOWORK=off go test ./model/ -run 'TestUserQuotaColumns' -v
GOWORK=off go test ./model/ ./controller/ 2>&1 | tail -5
```

Expected: PASS(DSN 미설정이면 두 번째 테스트는 SKIP). 기존 model/controller 테스트 모두 ok.

- [ ] **Step 5: (가능하면) MySQL/PostgreSQL로 실행**

Task 3의 컨테이너를 먼저 띄웠다면:

```bash
TEST_MYSQL_DSN='root:123456@tcp(127.0.0.1:33061)/new_api_test?parseTime=true' \
TEST_POSTGRES_DSN='postgres://root:123456@127.0.0.1:54321/new_api_test?sslmode=disable' \
GOWORK=off go test ./model/ -run TestUserQuotaColumnsSurviveAutoMigrate -v
```

Expected: 두 서브테스트 PASS. (Task 3 전에 실행했다면 Task 3 끝에서 다시 돌린다.)

- [ ] **Step 6: fork-overlays.md에 기록 후 커밋**

`docs/fork-overlays.md` 표에 추가: `| 공통(quota) | model/user.go:97-104 | Quota/UsedQuota/AffQuota/AffHistoryQuota 태그 type:bigint | model/user_quota_columns_fork_test.go |`

```bash
git add model/user.go model/user_quota_columns_fork_test.go docs/fork-overlays.md
git commit -m "fix(model): keep users quota columns bigint under upstream AutoMigrate

upstream tags the four wallet columns type:int while requiring bigint at
startup; GORM would narrow an existing bigint column. Pin the tags and
guard them with a reflection test plus a DSN-gated migration test.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: 스키마 기동 리허설 스크립트

운영 DB 사본에서 돌릴 수 있는 스크립트를 만들고, 로컬 컨테이너에서 team 바이너리로 "구 스키마(bigint, Toss 테이블 포함)"를 만든 뒤 새 바이너리를 기동해 §5 절차를 자동화한다.

**Files:**
- Create: `scripts/schema-rehearsal/docker-compose.yml`, `scripts/schema-rehearsal/run.sh`, `scripts/schema-rehearsal/README.md`
- Modify: `docs/2026-10-14-rebase-phase-a.md` §2

**Interfaces:**
- Produces: `scripts/schema-rehearsal/run.sh [mysql|postgres|all]` — 환경 변수 `LEGACY_SKIP=1`이면 team 바이너리 단계를 건너뛰고 `SQL_DSN`으로 주어진 기존 DB에 새 바이너리만 기동한다(운영 사본용).

- [ ] **Step 1: compose 파일**

`scripts/schema-rehearsal/docker-compose.yml`:

```yaml
services:
  mysql:
    image: mysql:8.0
    container_name: new-api-rehearsal-mysql
    environment:
      MYSQL_ROOT_PASSWORD: "123456"
      MYSQL_DATABASE: new_api
    command: --sql-mode=STRICT_TRANS_TABLES,NO_ENGINE_SUBSTITUTION
    ports: ["33061:3306"]
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1", "-p123456"]
      interval: 3s
      retries: 20
  postgres:
    image: postgres:15-alpine
    container_name: new-api-rehearsal-pg
    environment:
      POSTGRES_USER: root
      POSTGRES_PASSWORD: "123456"
      POSTGRES_DB: new_api
    ports: ["54321:5432"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U root -d new_api"]
      interval: 3s
      retries: 20
```

- [ ] **Step 2: run.sh**

`scripts/schema-rehearsal/run.sh`:

```bash
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
```

`chmod +x scripts/schema-rehearsal/run.sh`.

- [ ] **Step 3: README**

`scripts/schema-rehearsal/README.md`:

```markdown
# 스키마 기동 리허설

spec §5: 기존 운영 DB(bigint quota, Toss 테이블 포함)에서 재기반 바이너리가 데이터 손실 없이 기동하는지 확인한다.

로컬 컨테이너로:
    ./scripts/schema-rehearsal/run.sh all
운영 사본(이미 team 스키마인 DB)으로:
    LEGACY_SKIP=1 SQL_DSN='root:...@tcp(host:3306)/db?parseTime=true' ./scripts/schema-rehearsal/run.sh mysql
    LEGACY_SKIP=1 SQL_DSN='postgres://...' ./scripts/schema-rehearsal/run.sh postgres

통과 기준: "column types after"가 bigint, "wallet after"가 5000000000/3000000000, 로그에 schema 오류 없음.
정리: `docker compose -f scripts/schema-rehearsal/docker-compose.yml down -v`, `git worktree remove /tmp/new-api-schema-rehearsal/legacy-src`.
```

- [ ] **Step 4: 실행**

```bash
./scripts/schema-rehearsal/run.sh all 2>&1 | tee /tmp/rehearsal.txt
```

Expected: 두 dialect 모두 "column types after"가 `bigint`(MySQL `bigint`, PG `bigint`), wallet 5000000000/3000000000, schema 오류 없음. 새 바이너리가 `users.quota uses int` 오류로 죽으면 Task 2가 적용되지 않은 것이다.

- [ ] **Step 5: Task 2의 DSN 테스트 재실행**

```bash
TEST_MYSQL_DSN='root:123456@tcp(127.0.0.1:33061)/new_api?parseTime=true' \
TEST_POSTGRES_DSN='postgres://root:123456@127.0.0.1:54321/new_api?sslmode=disable' \
GOWORK=off go test ./model/ -run TestUserQuotaColumnsSurviveAutoMigrate -v
```

Expected: PASS ×2.

- [ ] **Step 6: 결과 기록 후 커밋**

`docs/2026-10-14-rebase-phase-a.md` §2에 Step 4 출력의 "before/after/wallet" 블록을 dialect별로 붙이고, 운영 사본 리허설은 "미실행(운영자 작업)"으로 표시한다.

```bash
git add scripts/schema-rehearsal docs/2026-10-14-rebase-phase-a.md
git commit -m "chore(rebase): add the schema startup rehearsal script and record the local run

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: O10 unode 진단 도구

**Files:**
- Create: `tools/unode-diagnostics/**` (team 17파일 그대로)
- Modify: `.gitignore`

- [ ] **Step 1: 복사**

```bash
git checkout team -- tools/unode-diagnostics
printf '\n# fork: O10 diagnostics history\ntools/unode-diagnostics/data/\n' >> .gitignore
ls tools/unode-diagnostics/lib tools/unode-diagnostics/test
```

- [ ] **Step 2: 테스트**

```bash
cd tools/unode-diagnostics && node --test test/*.test.mjs && cd ../..
```

Expected: 3개 파일의 테스트 모두 pass.

- [ ] **Step 3: 커밋**

```bash
git add tools/unode-diagnostics .gitignore
git commit -m "feat(tools): port the unode diagnostics workbench

Ported-From: team tools/unode-diagnostics
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: O5 문의 메일 백엔드

team의 `Contact`는 `message`를 요구하지만 랜딩 폼은 `notes`·`features[]`·`scale_type`·`headcount`·`input_tokens`·`output_tokens`를 보낸다. 새 핸들러는 폼 필드를 그대로 받는다. 메시지는 upstream 방식(`common.ApiErrorT`에 영문 원문)으로 낸다.

**Files:**
- Create: `controller/contact.go`, `controller/email.go`, `controller/contact_test.go`, `router/fork-router.go`
- Modify: `common/constants.go`(SMTP 변수 블록 뒤), `model/option.go`(OptionMap 초기화 `SMTPForceAuthLogin` 다음 줄, `updateOptionMap` switch `SMTPToken` case 다음), `router/api-router.go:281`

**Interfaces:**
- Produces: `controller.Contact`, `controller.TestEmail`, `var sendContactMail = common.SendEmail`(테스트 교체용), `router.registerForkPublicRoutes(apiRouter *gin.RouterGroup)`, `common.ContactEmail string`.
- 응답: 성공 `{"success":true,"message":""}`; 검증 실패 `common.ApiErrorT` (HTTP 200, `success:false`); 미설정 HTTP 503.

- [ ] **Step 1: 테스트 작성**

`controller/contact_test.go`:

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func stubContactMail(t *testing.T, recipient string) (*string, *string, *string) {
	t.Helper()
	oldEmail, oldSend := common.ContactEmail, sendContactMail
	common.ContactEmail = recipient
	var subject, to, body string
	sendContactMail = func(s, r, b string) error { subject, to, body = s, r, b; return nil }
	t.Cleanup(func() { common.ContactEmail = oldEmail; sendContactMail = oldSend })
	return &subject, &to, &body
}

func postContact(t *testing.T, payload string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/contact", strings.NewReader(payload))
	ctx.Request.Header.Set("Content-Type", "application/json")
	Contact(ctx)
	return rec
}

func TestContactSendsLandingFormFields(t *testing.T) {
	subject, to, body := stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","phone":"010-0000","features":["chat","embedding"],
		"scale_type":"token","input_tokens":"1000000","output_tokens":"200000","notes":"<b>hi</b>"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":true`)
	require.Equal(t, "ops@example.com", *to)
	require.Contains(t, *subject, "ACME")
	for _, want := range []string{"ACME", "a@b.co", "010-0000", "chat, embedding", "1000000", "200000", "&lt;b&gt;hi&lt;/b&gt;"} {
		require.Contains(t, *body, want)
	}
}

func TestContactAcceptsLegacyMessageField(t *testing.T) {
	_, _, body := stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","message":"legacy text"}`)
	require.Contains(t, rec.Body.String(), `"success":true`)
	require.Contains(t, *body, "legacy text")
}

func TestContactRejectsMissingOrgOrEmail(t *testing.T) {
	stubContactMail(t, "ops@example.com")
	rec := postContact(t, `{"org":"","email":"a@b.co"}`)
	require.Contains(t, rec.Body.String(), `"success":false`)
	rec = postContact(t, `{"org":"ACME","email":"not-an-email"}`)
	require.Contains(t, rec.Body.String(), `"success":false`)
}

func TestContactReturns503WhenUnconfigured(t *testing.T) {
	stubContactMail(t, "")
	rec := postContact(t, `{"org":"ACME","email":"a@b.co","notes":"x"}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTestEmailRequiresRecipient(t *testing.T) {
	oldSend := sendTestMail
	sendTestMail = func(string, string, string) error { return nil }
	t.Cleanup(func() { sendTestMail = oldSend })
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/admin/test-email", strings.NewReader(`{"to":""}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	TestEmail(ctx)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
```

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./controller/ -run 'TestContact|TestTestEmail' 2>&1 | head -5
```

Expected: 컴파일 실패 (`undefined: Contact`).

- [ ] **Step 3: 옵션·상수 훅**

`common/constants.go`: SMTP 변수 블록(`SMTPToken` 등, 104-112줄 부근) 바로 뒤에

```go
var ContactEmail = "" // fork: O5 landing inquiry recipient
```

`model/option.go`: OptionMap 초기화에서 `common.OptionMap["SMTPForceAuthLogin"] = ...` 줄 다음에

```go
	common.OptionMap["ContactEmail"] = common.ContactEmail // fork: O5
```

`updateOptionMap`의 `case "SMTPToken":` 블록 다음에

```go
	case "ContactEmail": // fork: O5
		common.ContactEmail = value
```

- [ ] **Step 4: 컨트롤러 작성**

`controller/contact.go` (team `controller/contact.go`를 기반으로 재작성):

```go
package controller

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O5 landing inquiry form (web/landing/public/landing.js posts these fields).
type contactRequest struct {
	Org          string   `json:"org"`
	Email        string   `json:"email"`
	Phone        string   `json:"phone"`
	Features     []string `json:"features"`
	ScaleType    string   `json:"scale_type"`
	Headcount    string   `json:"headcount"`
	InputTokens  string   `json:"input_tokens"`
	OutputTokens string   `json:"output_tokens"`
	Notes        string   `json:"notes"`
	Message      string   `json:"message"` // legacy clients
}

// sendContactMail is replaced in tests.
var sendContactMail = common.SendEmail

func Contact(c *gin.Context) {
	var req contactRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid request")
		return
	}
	req.Org = strings.TrimSpace(req.Org)
	req.Email = strings.TrimSpace(req.Email)
	if req.Notes == "" {
		req.Notes = req.Message
	}
	if req.Org == "" || req.Email == "" {
		common.ApiErrorT(c, "Organization and email are required")
		return
	}
	if common.Validate.Var(req.Email, "required,email,max=254") != nil {
		common.ApiErrorT(c, "Please enter a valid email address")
		return
	}
	if common.ContactEmail == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Contact email is not configured"})
		return
	}
	subject := fmt.Sprintf("[%s] Enterprise Inquiry — %s", common.SystemName, req.Org)
	if err := sendContactMail(subject, common.ContactEmail, renderContactMail(req)); err != nil {
		common.SysError(fmt.Sprintf("contact email send failed: %v", err))
		common.ApiErrorT(c, "Failed to send the inquiry, please try again later")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func renderContactMail(req contactRequest) string {
	row := func(label, value string) string {
		return fmt.Sprintf(`<tr><td style="padding:8px 12px;font-weight:700;width:160px;background:#f3f4f6;">%s</td><td style="padding:8px 12px;border-bottom:1px solid #e5e7eb;white-space:pre-wrap;">%s</td></tr>`, label, html.EscapeString(value))
	}
	scale := req.Headcount
	scaleLabel := "Headcount"
	if req.ScaleType == "token" {
		scale = fmt.Sprintf("input %s / output %s tokens", req.InputTokens, req.OutputTokens)
		scaleLabel = "Monthly tokens"
	}
	return `<html><body style="font-family:sans-serif;color:#222;"><h2 style="color:#0EA5E9;">Enterprise Inquiry</h2>` +
		`<table style="border-collapse:collapse;width:100%;max-width:640px;">` +
		row("Company / Organization", req.Org) + row("Email", req.Email) + row("Phone", req.Phone) +
		row("Features", strings.Join(req.Features, ", ")) + row(scaleLabel, scale) + row("Notes", req.Notes) +
		`</table></body></html>`
}
```

`controller/email.go` (team 파일에서 i18n만 제거):

```go
package controller

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// fork: O5 admin SMTP test mail.
type testEmailRequest struct {
	To string `json:"to"`
}

var sendTestMail = common.SendEmail

func TestEmail(c *gin.Context) {
	var req testEmailRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid parameters"})
		return
	}
	if req.To == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Recipient address is required"})
		return
	}
	subject := fmt.Sprintf("[%s] SMTP test email", common.SystemName)
	body := fmt.Sprintf(`<html><body style="font-family:sans-serif;color:#222;padding:24px;"><h2 style="color:#0EA5E9;">SMTP configuration test</h2><p>This is a test email sent from <strong>%s</strong>.</p><p>If you received this message, your SMTP settings are working correctly.</p></body></html>`, common.SystemName)
	if err := sendTestMail(subject, req.To, body); err != nil {
		common.SysError(fmt.Sprintf("test email send failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Failed to send the test email"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
```

- [ ] **Step 5: 라우트 파일과 훅**

`router/fork-router.go`:

```go
package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// fork: routes added by the fork. Each function is called from one line in
// router/api-router.go; keep the call sites listed in docs/fork-overlays.md.

// registerForkPublicRoutes: anonymous endpoints (O5 contact, O2 PayPal browser return/webhook).
func registerForkPublicRoutes(apiRouter *gin.RouterGroup) {
	bodyLimit := middleware.AnonymousRequestBodyLimit()
	apiRouter.POST("/contact", middleware.CriticalRateLimit(), bodyLimit, controller.Contact)
	apiRouter.POST("/admin/test-email", middleware.AdminAuth(), middleware.CriticalRateLimit(), controller.TestEmail)
}

// registerForkSelfRoutes: user-authenticated endpoints (filled by O2).
func registerForkSelfRoutes(selfRoute *gin.RouterGroup) {}

// registerForkAdminUserRoutes: admin endpoints under /api/user (filled by O4).
func registerForkAdminUserRoutes(adminRoute *gin.RouterGroup) {}
```

`router/api-router.go`: `registerAuthzRoutes(apiRouter)`(281줄) 다음에

```go
		registerForkPublicRoutes(apiRouter) // fork: router/fork-router.go
```

`selfRoute.Use(middleware.DisableCache(), middleware.UserAuth())`(94줄) 바로 뒤 블록 안 첫 줄에

```go
			registerForkSelfRoutes(selfRoute) // fork: router/fork-router.go
```

`adminRoute.Use(middleware.AdminAuth())`(154줄) 뒤 블록 안 첫 줄에

```go
				registerForkAdminUserRoutes(adminRoute) // fork: router/fork-router.go
```

- [ ] **Step 6: 테스트 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./controller/ -run 'TestContact|TestTestEmail' -v 2>&1 | tail -15
```

Expected: 5개 PASS.

- [ ] **Step 7: 기록·커밋**

`docs/fork-overlays.md`에 O5 행 3개(constants.go, option.go ×2, api-router.go 훅 3줄)를 추가.

```bash
git add common/constants.go model/option.go controller/contact.go controller/email.go controller/contact_test.go router/fork-router.go router/api-router.go docs/fork-overlays.md
git commit -m "feat(contact): port the landing inquiry mail and admin SMTP test endpoints

The handler now accepts the fields the landing form actually sends
(notes, features, scale) instead of the unused message field.

Ported-From: team controller/contact.go controller/email.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: O5 문의 메일 프런트(설정 화면)

**Files:**
- Modify: `web/src/features/system-settings/integrations/email-settings-section.tsx`, `web/src/features/system-settings/api.ts`, `web/src/features/system-settings/types.ts:373`, `web/src/features/system-settings/operations/index.tsx:42`, `web/src/features/system-settings/operations/section-registry.tsx:76`, `web/src/i18n/locales/en.json`

**Interfaces:**
- Produces: `sendTestEmail(to: string): Promise<{success: boolean; message: string}>` in `features/system-settings/api.ts`; 옵션 키 `ContactEmail`.

- [ ] **Step 1: api.ts에 함수 추가 (파일 끝)**

```ts
export async function sendTestEmail(to: string) {
  const res = await api.post<{ success: boolean; message: string }>(
    '/api/admin/test-email',
    { to },
    { skipErrorHandler: true }
  )
  return res.data
}
```

- [ ] **Step 2: 타입·기본값·레지스트리**

- `types.ts` 373줄 `SMTPForceAuthLogin: boolean` 다음에 `ContactEmail: string`.
- `operations/index.tsx` 42줄 `SMTPForceAuthLogin: false,` 다음에 `ContactEmail: '',`.
- `operations/section-registry.tsx` 76줄 `SMTPForceAuthLogin: settings.SMTPForceAuthLogin,` 다음에 `ContactEmail: settings.ContactEmail ?? '',`.

- [ ] **Step 3: 이메일 섹션 수정**

`email-settings-section.tsx`:
- import에 `useState`(react), `useMutation`(`@tanstack/react-query`), `toast`(`sonner` — 파일이 이미 쓰는 것과 같은 모듈), `sendTestEmail`(`../api`)을 추가.
- `createEmailSchema`의 `SMTPForceAuthLogin: z.boolean(),` 다음에

```ts
    ContactEmail: z.string().refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)
    }, t('Enter a valid email or leave blank')),
```

- `sanitized`와 `initial` 객체에 각각 `ContactEmail: values.ContactEmail.trim(),` / `ContactEmail: defaultValues.ContactEmail.trim(),`.
- `SMTPForceAuthLogin` update push 다음에

```ts
    if (sanitized.ContactEmail !== initial.ContactEmail) {
      updates.push({ key: 'ContactEmail', value: sanitized.ContactEmail })
    }
```

- 컴포넌트 본문 상단(`const form = useForm` 앞)에

```ts
  const [testTo, setTestTo] = useState('')
  const testEmailMutation = useMutation({
    mutationFn: () => sendTestEmail(testTo.trim()),
    onSuccess: (data) => {
      if (data.success) toast.success(t('Test email sent successfully'))
      else toast.error(data.message || t('Failed to send test email'))
    },
    onError: (error: unknown) => {
      const msg =
        (error as { response?: { data?: { message?: string } } })?.response?.data?.message ||
        t('Failed to send test email')
      toast.error(msg)
    },
  })
```

- `SMTPToken` FormField 다음, `</SettingsForm>` 앞에 team `web/default/src/features/system-settings/integrations/email-settings-section.tsx` 342-389줄의 `ContactEmail` FormField와 "Send Test Email" FormItem을 그대로 붙여 넣는다(`git show team:web/default/src/features/system-settings/integrations/email-settings-section.tsx | sed -n 342,389p`).

- [ ] **Step 4: i18n 키**

`en.json`에 추가(값=키): `Contact Recipient Email`, `contact@yourcompany.com`, `Inquiry emails from the landing page will be sent to this address`, `Send Test Email`, `Enter recipient address`, `Send a test message to verify your SMTP configuration`, `Test email sent successfully`, `Failed to send test email`, `Send`. 이미 있는 키는 건너뛴다. 그다음 `cd web && bun run i18n:sync`.

- [ ] **Step 5: 검증**

```bash
cd web && bun run typecheck && bun run lint && bun run test 2>&1 | tail -5 && bun run copyright:check && cd ..
```

Expected: 모두 통과.

- [ ] **Step 6: 브라우저 확인(선택, DEBUG 서버)**

`make dev-api`로 띄운 뒤 시스템 설정 → 이메일 섹션에 "Contact Recipient Email" 필드와 "Send Test Email" 버튼이 보이는지 확인.

- [ ] **Step 7: 커밋**

```bash
git add web/src/features/system-settings web/src/i18n/locales
git commit -m "feat(web): add the contact recipient and SMTP test mail controls to email settings

Ported-From: team web/default/src/features/system-settings/integrations/email-settings-section.tsx
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: O4 사용자 Excel 내보내기·가져오기 백엔드

team 코드를 upstream 지갑 API에 맞춘다: `int64` → `int`, `GrantUserQuota`(team 전용) → `model.IncreaseUserQuota(id, delta, true)`(upstream, 상한 검사 포함), 가져오기 역할은 호출자가 관리할 수 있는 역할로 제한한다(upstream `canManageTargetRole` 규칙: root이거나 자기보다 낮은 역할만).

**Files:**
- Create: `model/user_export.go`, `service/user_export.go`, `service/user_export_test.go`, `controller/user_export.go`
- Modify: `router/fork-router.go` (`registerForkAdminUserRoutes`)

**Interfaces:**
- Consumes: `model.IncreaseUserQuota(id int, quota int, db bool) error`, `model.DecreaseUserQuota(id int, quota int, db bool) error`, `model.CheckUserExistOrDeleted(username, email string) (bool, error)`, `(*model.User).Insert(inviterId int) error`, `common.ValidateWalletQuota(int) error`, `common.RoleRootUser`.
- Produces: `model.GetAllUsersForExport() ([]*User, error)`; `service.ExcelHeaders`, `service.BuildExportFile() (*excelize.File, error)`, `service.ImportResult{Created, Skipped int; SkippedUsernames, Errors []string}`, `service.ImportUsersFromFile(f *excelize.File, callerRole int) (*ImportResult, error)`; `controller.ExportUsers`, `controller.ImportUsers`; 라우트 `GET /api/user/export`, `POST /api/user/import`(관리자).

- [ ] **Step 1: 테스트 작성**

`service/user_export_test.go` (`truncate(t)`는 upstream `service/task_billing_test.go:74`의 공용 헬퍼):

```go
package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func newImportWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	sheet := f.GetSheetName(0)
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		require.NoError(t, err)
		require.NoError(t, f.SetSheetRow(sheet, cell, &row))
	}
	return f
}

func TestImportUsersRejectsQuotaAboveWalletCeiling(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "quota", "aff_quota"},
		{"import_quota_over", "password123", fmt.Sprintf("%d", common.MaxWalletQuota+1), ""},
		{"import_aff_over", "password123", "", fmt.Sprintf("%d", common.MaxWalletQuota+1)},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "line 2 (import_quota_over): invalid quota value")
	require.Contains(t, result.Errors[0], fmt.Sprintf("wallet quota exceeds %d", common.MaxWalletQuota))
	require.Contains(t, result.Errors[1], "line 3 (import_aff_over): invalid aff_quota value")
	require.Zero(t, result.Created)
}

func TestImportUsersSetsImportedQuotaAbove32Bit(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "quota"},
		{"import_big_wallet", "password123", "5000000000"},
	})
	result, err := ImportUsersFromFile(f, common.RoleRootUser)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
	var user model.User
	require.NoError(t, model.DB.Where("username = ?", "import_big_wallet").First(&user).Error)
	require.Equal(t, 5_000_000_000, user.Quota)
}

func TestImportUsersRefusesRolesTheCallerCannotManage(t *testing.T) {
	truncate(t)
	f := newImportWorkbook(t, [][]any{
		{"username", "initial_password", "role"},
		{"import_as_root", "password123", "root"},
		{"import_as_admin", "password123", "admin"},
		{"import_as_user", "password123", "user"},
	})
	result, err := ImportUsersFromFile(f, common.RoleAdminUser)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Len(t, result.Errors, 2)
	require.Contains(t, result.Errors[0], "import_as_root")
	require.Contains(t, result.Errors[1], "import_as_admin")
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("username IN ?", []string{"import_as_root", "import_as_admin"}).Count(&count).Error)
	require.Zero(t, count)
}

func TestExportThenImportRoundTrip(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create(&model.User{Username: "export_me", DisplayName: "Export Me", Group: "vip", Quota: 123, Status: common.UserStatusEnabled, Role: common.RoleCommonUser}).Error)
	f, err := BuildExportFile()
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	rows, err := f.GetRows(f.GetSheetName(0))
	require.NoError(t, err)
	require.Equal(t, ExcelHeaders, rows[0])
	require.Len(t, rows, 2)
	require.Equal(t, "export_me", rows[1][1])

	// Re-import under a new name with a password filled in.
	rows[1][1] = "export_me_copy"
	rows[1][13] = "password123" // initial_password column
	imported := newImportWorkbook(t, [][]any{anyRow(rows[0]), anyRow(rows[1])})
	result, err := ImportUsersFromFile(imported, common.RoleRootUser)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, 1, result.Created)
	var copy model.User
	require.NoError(t, model.DB.Where("username = ?", "export_me_copy").First(&copy).Error)
	require.Equal(t, "vip", copy.Group)
	require.Equal(t, 123, copy.Quota)
}

func anyRow(cells []string) []any {
	out := make([]any, len(cells))
	for i, c := range cells {
		out[i] = c
	}
	return out
}
```

`ExcelHeaders`에서 `initial_password`의 인덱스가 13이 아니면 team 헤더 순서(`id, username, display_name, email, role, status, group, quota, used_quota, request_count, remark, aff_quota, inviter_id, initial_password, created_at`)를 확인해 맞춘다.

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./service/ -run 'TestImportUsers|TestExportThenImport' 2>&1 | head -5
```

Expected: 컴파일 실패.

- [ ] **Step 3: 모델·서비스·컨트롤러**

`model/user_export.go`:

```go
package model

// fork: O4 — rows for the Excel export; password and legacy access token are omitted.
func GetAllUsersForExport() ([]*User, error) {
	var users []*User
	err := DB.Omit("password", "access_token").Order("id asc").Find(&users).Error
	return users, err
}
```

`service/user_export.go`: `git show team:service/user_export.go > service/user_export.go` 후 다음을 고친다.
1. `common.ValidateWalletQuota(int64(quota))` → `common.ValidateWalletQuota(quota)`; `int64(affQuota)` 도 동일.
2. `AffQuota: int64(affQuota),` → `AffQuota: affQuota,`.
3. 시그니처 `func ImportUsersFromFile(f *excelize.File, callerRole int) (*ImportResult, error)`.
4. `Role: stringToRole(getCell(row, "role")),`를 쓰기 전에(user 구조체 생성 앞) 역할 검사 추가:

```go
		role := stringToRole(getCell(row, "role"))
		if !(callerRole == common.RoleRootUser || callerRole > role) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d (%s): role %q is not permitted for the importing admin", lineNum, username, getCell(row, "role")))
			continue
		}
```

그리고 구조체에는 `Role: role,`.
5. `setImportedUserQuota(user.Id, common.QuotaForNewUser, int64(quota))` → `setImportedUserQuota(user.Id, common.QuotaForNewUser, quota)`.
6. 파일 끝에 (team `service/org_user_export.go:52-61`을 옮겨 upstream API로):

```go
// setImportedUserQuota moves a user's balance to target. Increases go through
// IncreaseUserQuota, which enforces common.MaxWalletQuota; decreases through
// DecreaseUserQuota.
func setImportedUserQuota(userId int, current int, target int) error {
	delta := target - current
	if delta > 0 {
		return model.IncreaseUserQuota(userId, delta, true)
	}
	if delta < 0 {
		return model.DecreaseUserQuota(userId, -delta, true)
	}
	return nil
}
```

`controller/user_export.go`: team `controller/user.go:1286-1335`의 `ExportUsers`/`ImportUsers`를 그대로 옮기되 `service.ImportUsersFromFile(f, c.GetInt("role"))`로 호출한다. import 목록: `fmt`, `net/http`, `time`, `common`, `service`, `gin`, `excelize`.

`router/fork-router.go`의 `registerForkAdminUserRoutes`:

```go
func registerForkAdminUserRoutes(adminRoute *gin.RouterGroup) {
	adminRoute.GET("/export", controller.ExportUsers)
	adminRoute.POST("/import", controller.ImportUsers)
}
```

- [ ] **Step 4: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./service/ -run 'TestImportUsers|TestExportThenImport' -v 2>&1 | tail -12 && GOWORK=off go test ./service/ ./controller/ 2>&1 | tail -3
```

Expected: 4개 PASS, 패키지 전체 ok.

- [ ] **Step 5: 커밋**

`docs/fork-overlays.md`에 O4 행(fork-router 훅은 이미 기록됨)을 추가.

```bash
git add model/user_export.go service/user_export.go service/user_export_test.go controller/user_export.go router/fork-router.go docs/fork-overlays.md
git commit -m "feat(users): port Excel export/import onto the upstream wallet API

Imports use IncreaseUserQuota so the 2^53 wallet ceiling applies, and an
admin cannot import accounts at or above their own role.

Ported-From: team service/user_export.go controller/user.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: O4 프런트(사용자 화면 버튼·다이얼로그)

**Files:**
- Create: `web/src/features/users/components/users-import-dialog.tsx`, `web/src/features/users/components/__tests__/users-import-dialog.test.tsx`
- Modify: `web/src/features/users/api.ts`(끝), `web/src/features/users/types.ts`(끝), `web/src/features/users/components/users-primary-buttons.tsx`, `web/src/i18n/locales/en.json`

**Interfaces:**
- Produces: `exportUsers(): Promise<void>`, `importUsers(file: File): Promise<ImportResult>`, `interface ImportResult { created: number; skipped: number; skipped_usernames: string[]; errors: string[] }`, `<UsersImportDialog open onOpenChange onSuccess />`.

- [ ] **Step 1: 다이얼로그 테스트 작성**

`web/src/features/users/components/__tests__/users-import-dialog.test.tsx`:

```tsx
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { UsersImportDialog } from '../users-import-dialog'

vi.mock('../../api', () => ({
  importUsers: vi.fn(async () => ({
    created: 2,
    skipped: 1,
    skipped_usernames: ['dup'],
    errors: ['line 4 (bad): initial_password is empty'],
  })),
}))

describe('UsersImportDialog', () => {
  it('uploads the chosen workbook and shows the result summary', async () => {
    const onSuccess = vi.fn()
    render(<UsersImportDialog open onOpenChange={() => {}} onSuccess={onSuccess} />)
    const input = screen.getByLabelText('Select .xlsx file') as HTMLInputElement
    const file = new File(['x'], 'users.xlsx', {
      type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    })
    await userEvent.upload(input, file)
    await userEvent.click(screen.getByRole('button', { name: 'Upload' }))
    await waitFor(() => expect(onSuccess).toHaveBeenCalled())
    expect(screen.getByText(/2 users created/)).toBeInTheDocument()
    expect(screen.getByText(/1 users skipped/)).toBeInTheDocument()
    expect(screen.getByText(/initial_password is empty/)).toBeInTheDocument()
  })
})
```

team 다이얼로그의 파일 입력에 `aria-label`/`<label>`이 없으면 Step 3에서 `aria-label={t('Select .xlsx file')}`를 붙인다. `@testing-library/user-event`가 `web/package.json`에 없으면 `fireEvent.change(input, { target: { files: [file] } })`와 `fireEvent.click`으로 바꾼다.

- [ ] **Step 2: 실패 확인**

```bash
cd web && bun run test -- users-import-dialog 2>&1 | tail -5; cd ..
```

Expected: 모듈을 찾지 못해 실패.

- [ ] **Step 3: 구현**

```bash
git show team:web/default/src/features/users/components/users-import-dialog.tsx > web/src/features/users/components/users-import-dialog.tsx
git show team:web/default/src/features/users/components/users-primary-buttons.tsx > web/src/features/users/components/users-primary-buttons.tsx
```

- `api.ts` 끝에 team `web/default/src/features/users/api.ts:197-229`의 `exportUsers`/`importUsers`를 붙이고 상단 import에 `ImportResult`를 추가.
- `types.ts` 끝에 `ImportResult` 인터페이스.
- import 경로가 upstream 규칙(`@/components/ui/...`, 상대 `../api`)과 맞는지 확인; upstream은 import 그룹 사이에 빈 줄을 둔다(`bun run lint`가 알려준다).
- `useUsers()`가 `triggerRefresh`를 제공하는지 확인(`users-provider.tsx`에 있음).

- [ ] **Step 4: i18n 키**

`en.json`에 추가: `Import Users`, `Import`, `Exporting...`, `{{count}} users created`, `{{count}} users skipped (already exist)`, `Select .xlsx file`, `Import failed`, `Uploading...`, `Errors` (이미 있는 것은 건너뜀). `cd web && bun run i18n:sync`.

- [ ] **Step 5: 검증**

```bash
cd web && bun run test -- users-import-dialog 2>&1 | tail -8 && bun run typecheck && bun run lint && bun run copyright:check && cd ..
```

Expected: 테스트 PASS, 나머지 통과.

- [ ] **Step 6: 커밋**

```bash
git add web/src/features/users web/src/i18n/locales
git commit -m "feat(web): add user Excel export and import to the users page

Ported-From: team web/default/src/features/users
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: O2 PayPal 모델·설정·옵션

**Files:**
- Create: `setting/payment_paypal.go`, `model/topup_paypal.go`, `model/topup_paypal_test.go`, `controller/payment_availability_paypal.go`
- Modify: `model/topup.go`(TopUp 구조체 끝, 두 const 블록, `ManualCompleteTopUp` 485줄), `model/option.go`(Stripe OptionMap 100줄 뒤, `updateOptionMap` Stripe case 526줄 뒤), `controller/topup.go`(`GetTopUpInfo` Waffo Pancake 블록 뒤 76줄, data map 102·118줄)

**Interfaces:**
- Consumes: `lockForUpdate(tx)`, `creditTopUpQuota(tx, userId, quota int, updates map[string]any)`, `syncCreditUserQuotaCache(userId, quota int, op string)`, `RecordTopupLog(userId int, msg *common.Message, ip, method, callbackMethod string)`, `ErrPaymentMethodMismatch`, `ErrInvalidTopUpQuota`, `common.UsingMainDatabase(common.DatabaseTypePostgreSQL)`, `isPaymentComplianceConfirmed()`.
- Produces: `setting.PayPalClientId/PayPalClientSecret/PayPalWebhookID string`, `setting.PayPalSandbox bool`, `setting.PayPalUnitPrice float64`, `setting.PayPalMinTopUp int`; `model.PaymentMethodPayPal = "paypal"`, `model.PaymentProviderPayPal = "paypal"`, `model.TopUp.ProviderOrderId string`, `model.RechargePayPal(tradeNo, callerIp string) error`; `controller.isPayPalTopUpEnabled() bool`, `controller.isPayPalWebhookEnabled() bool`, `controller.appendPayPalPayMethod(payMethods []map[string]string) []map[string]string`; `GetTopUpInfo` 응답 키 `enable_paypal_topup`, `paypal_min_topup`.

- [ ] **Step 1: 모델 테스트 작성**

`model/topup_paypal_test.go` (model 패키지는 `TestMain`이 SQLite `DB`를 준비한다; `insertTopUpForPaymentGuardTest`는 `payment_method_guard_test.go`의 헬퍼):

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRechargePayPalCreditsOnceAndRejectsOtherProviders(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})
	oldUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500_000
	t.Cleanup(func() { common.QuotaPerUnit = oldUnit })

	user := insertUserForPaymentGuardTest(t, 901, 1_000)
	insertTopUpForPaymentGuardTest(t, "pp-ok", user.Id, PaymentProviderPayPal)
	insertTopUpForPaymentGuardTest(t, "pp-stripe", user.Id, PaymentProviderStripe)

	require.NoError(t, RechargePayPal("pp-ok", "127.0.0.1"))
	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 1_000+4_995_000, got.Quota) // Money 9.99 USD * QuotaPerUnit 500_000

	require.NoError(t, RechargePayPal("pp-ok", "127.0.0.1")) // idempotent
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 1_000+4_995_000, got.Quota)
	require.Equal(t, common.TopUpStatusSuccess, getTopUpStatusForPaymentGuardTest(t, "pp-ok"))

	err := RechargePayPal("pp-stripe", "127.0.0.1")
	require.Error(t, err)
	require.Equal(t, common.TopUpStatusPending, getTopUpStatusForPaymentGuardTest(t, "pp-stripe"))
}
```

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./model/ -run TestRechargePayPal 2>&1 | head -5
```

Expected: 컴파일 실패 (`undefined: RechargePayPal`).

- [ ] **Step 3: 설정 파일·모델 수정**

```bash
git show team:setting/payment_paypal.go > setting/payment_paypal.go
```

`model/topup.go`:
- `TopUp` 구조체 마지막 필드 `Status` 뒤에

```go
	ProviderOrderId string  `json:"provider_order_id" gorm:"type:varchar(255);index"` // fork: O2 PayPal order id
```

- `PaymentMethodBalance = "balance"` 뒤에 `PaymentMethodPayPal = "paypal" // fork: O2`; `PaymentProviderBalance` 뒤에 `PaymentProviderPayPal = "paypal" // fork: O2`.
- `ManualCompleteTopUp`(485줄) `if topUp.PaymentProvider == PaymentProviderStripe {` → `if topUp.PaymentProvider == PaymentProviderStripe || topUp.PaymentProvider == PaymentProviderPayPal { // fork: O2 PayPal also stores Money in USD`.

`model/topup_paypal.go`:

```go
package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// fork: O2 — PayPal settlement, mirrors RechargeWaffo. Money is the USD amount
// after the group ratio, so quota = Money * QuotaPerUnit (same as Stripe).
func RechargePayPal(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}
	var quotaToAdd int
	topUp := &TopUp{}
	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("top-up order not found")
		}
		if topUp.PaymentProvider != PaymentProviderPayPal {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil // idempotent: webhook and capture may both arrive
		}
		if topUp.Status != common.TopUpStatusPending {
			return errors.New("invalid top-up order status")
		}
		quotaToAdd, err = common.WalletQuotaFromDecimalStrict(
			decimal.NewFromFloat(topUp.Money).Mul(decimal.NewFromFloat(common.QuotaPerUnit)),
		)
		if err != nil || quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}
		return creditTopUpQuota(tx, topUp.UserId, quotaToAdd, nil)
	})
	if err != nil {
		common.SysError(common.LogText("paypal topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	if quotaToAdd > 0 {
		syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("paypal topup"))
		RecordTopupLog(topUp.UserId, common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}",
			map[string]any{"provider": "PayPal", "quota": logger.FormatQuota(quotaToAdd), "amount": fmt.Sprintf("%.2f", topUp.Money)}),
			callerIp, topUp.PaymentMethod, PaymentMethodPayPal)
	}
	return nil
}
```

- [ ] **Step 4: 모델 테스트 통과**

```bash
GOWORK=off go test ./model/ -run TestRechargePayPal -v 2>&1 | tail -5
```

Expected: PASS.

- [ ] **Step 5: 옵션 등록**

`model/option.go` OptionMap 초기화의 Stripe 블록(100줄, `StripePromotionCodesEnabled` 등) 뒤에

```go
	// fork: O2 PayPal
	common.OptionMap["PayPalClientId"] = setting.PayPalClientId
	common.OptionMap["PayPalClientSecret"] = setting.PayPalClientSecret
	common.OptionMap["PayPalWebhookID"] = setting.PayPalWebhookID
	common.OptionMap["PayPalSandbox"] = strconv.FormatBool(setting.PayPalSandbox)
	common.OptionMap["PayPalUnitPrice"] = strconv.FormatFloat(setting.PayPalUnitPrice, 'f', -1, 64)
	common.OptionMap["PayPalMinTopUp"] = strconv.Itoa(setting.PayPalMinTopUp)
```

`updateOptionMap`의 마지막 Stripe case(526줄) 뒤에

```go
	// fork: O2 PayPal
	case "PayPalClientId":
		setting.PayPalClientId = value
	case "PayPalClientSecret":
		setting.PayPalClientSecret = value
	case "PayPalWebhookID":
		setting.PayPalWebhookID = value
	case "PayPalSandbox":
		setting.PayPalSandbox = value == "true"
	case "PayPalUnitPrice":
		setting.PayPalUnitPrice, _ = strconv.ParseFloat(value, 64)
	case "PayPalMinTopUp":
		setting.PayPalMinTopUp, _ = strconv.Atoi(value)
```

(`PayPalClientSecret`은 upstream `controller/option.go`의 `Secret` 접미사 마스킹을 자동으로 받는다.)

- [ ] **Step 6: 가용성 함수와 GetTopUpInfo 훅**

`controller/payment_availability_paypal.go`:

```go
package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

// fork: O2 — PayPal availability, same compliance gate as the other providers.
func isPayPalTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(setting.PayPalClientId) != "" &&
		strings.TrimSpace(setting.PayPalClientSecret) != "" &&
		strings.TrimSpace(setting.PayPalWebhookID) != ""
}

func isPayPalWebhookEnabled() bool { return isPayPalTopUpEnabled() }

// appendPayPalPayMethod adds the PayPal entry to GetTopUpInfo's pay method list.
func appendPayPalPayMethod(payMethods []map[string]string) []map[string]string {
	if !isPayPalTopUpEnabled() {
		return payMethods
	}
	for _, method := range payMethods {
		if method["type"] == model.PaymentMethodPayPal {
			return payMethods
		}
	}
	return append(payMethods, map[string]string{
		"name":      "PayPal",
		"type":      model.PaymentMethodPayPal,
		"color":     "#003087",
		"min_topup": strconv.Itoa(setting.PayPalMinTopUp),
	})
}
```

`controller/topup.go` `GetTopUpInfo`: Waffo Pancake 블록이 끝나는 76줄 뒤에 `payMethods = appendPayPalPayMethod(payMethods) // fork: O2`; data map의 `"enable_stripe_topup"` 줄 뒤에 `"enable_paypal_topup": isPayPalTopUpEnabled(), // fork: O2`, `"stripe_min_topup"` 줄 뒤에 `"paypal_min_topup": setting.PayPalMinTopUp, // fork: O2`.

- [ ] **Step 7: 빌드·테스트**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./model/ ./controller/ ./setting/... 2>&1 | tail -4
```

Expected: ok.

- [ ] **Step 8: 커밋**

`docs/fork-overlays.md`에 O2 행(topup.go 필드·상수·ManualComplete, option.go, controller/topup.go 훅 3줄) 추가.

```bash
git add setting/payment_paypal.go model/topup.go model/topup_paypal.go model/topup_paypal_test.go model/option.go controller/payment_availability_paypal.go controller/topup.go docs/fork-overlays.md
git commit -m "feat(paypal): port PayPal settings, order column and settlement onto upstream top-ups

Ported-From: team setting/payment_paypal.go model/topup.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: O2 PayPal 컨트롤러·라우트·테스트

**Files:**
- Create: `controller/topup_paypal.go`(team 복사 후 수정), `controller/topup_paypal_test.go`
- Modify: `router/fork-router.go`

**Interfaces:**
- Consumes: `paymentError(c, err)`, `common.NewMessage(text, params...)`, `rejectInvalidCreditedQuota(c, userId, decimal.Decimal) bool`, `LockOrder/UnlockOrder(tradeNo)`, `model.GetTopUpByTradeNo`, `model.RechargePayPal`, `isPayPalWebhookEnabled()`.
- Produces: `controller.RequestPayPalPay`, `RequestPayPalAmount`, `PayPalCapture`, `PayPalWebhook`; 라우트 `POST /api/user/paypal/pay`, `POST /api/user/paypal/amount`(로그인), `GET /api/paypal/capture`, `POST /api/paypal/webhook`(익명).

- [ ] **Step 1: 테스트 작성**

`controller/topup_paypal_test.go` (upstream `controller/topup_quota_limit_test.go:126-170`의 패턴):

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupPayPalCapacityTest(t *testing.T) {
	t.Helper()
	oldQuotaPerUnit := common.QuotaPerUnit
	oldDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	oldDB := model.DB
	oldUnit, oldMin := setting.PayPalUnitPrice, setting.PayPalMinTopUp
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	setting.PayPalUnitPrice, setting.PayPalMinTopUp = 1, 1
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	model.DB = db
	t.Cleanup(func() {
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = oldDisplayType
		setting.PayPalUnitPrice, setting.PayPalMinTopUp = oldUnit, oldMin
		model.DB = oldDB
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	require.NoError(t, model.DB.Create(&model.User{
		Id: 42, Username: "paypal_capacity_user", Quota: common.MaxWalletQuota - 100_000, Status: common.UserStatusEnabled,
	}).Error)
}

func postPayPal(t *testing.T, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 42)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler(ctx)
	return recorder
}

func TestRequestPayPalAmountRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	setupPayPalCapacityTest(t)
	rec := postPayPal(t, "/api/user/paypal/amount", `{"amount":1}`, RequestPayPalAmount)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"message":"error","data":"Top-up quota limit exceeded","message_key":"Top-up quota limit exceeded"}`, rec.Body.String())
}

func TestRequestPayPalPayRejectsTopUpThatWouldOverflowWallet(t *testing.T) {
	setupPayPalCapacityTest(t)
	rec := postPayPal(t, "/api/user/paypal/pay", `{"amount":1,"payment_method":"paypal"}`, RequestPayPalPay)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Top-up quota limit exceeded")
}

func TestPayPalWebhookRejectedWhenDisabled(t *testing.T) {
	oldID := setting.PayPalWebhookID
	setting.PayPalWebhookID = ""
	t.Cleanup(func() { setting.PayPalWebhookID = oldID })
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/paypal/webhook", strings.NewReader(`{}`))
	PayPalWebhook(ctx)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
```

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./controller/ -run 'PayPal' 2>&1 | head -5
```

Expected: 컴파일 실패.

- [ ] **Step 3: 컨트롤러 복사·수정**

```bash
git show team:controller/topup_paypal.go > controller/topup_paypal.go
```

수정 목록:
1. import에서 `github.com/QuantumNous/new-api/i18n` 제거.
2. `common.ApiErrorI18n(c, i18n.X[, params])` 호출을 모두 `paymentError(c, common.NewMessage("…"))`로 바꾼다:

| team | 대체 텍스트 |
|---|---|
| `MsgInvalidParams` | `"Invalid parameters"` |
| `MsgTopupUnsupportedProvider` | `"Unsupported payment channel"` |
| `MsgTopupAmountTooSmall` + `{"Min": minTopup}` | `common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": minTopup})` |
| `MsgTopupAmountTooLarge` | `common.NewMessage("Top-up amount cannot exceed {{max}}", map[string]any{"max": 10000})` |
| `MsgTopupRedirectNotTrusted` | `"Payment success redirect URL is not in the trusted domain list"` |
| `MsgTopupCancelRedirectBad` | `"Payment cancel redirect URL is not in the trusted domain list"` |
| `MsgUserNotExists` | `"User does not exist"` |
| `MsgTopupAmountTooLow2` | `"Payment amount is too low"` |
| `MsgPaymentStartFailed` | `"Failed to start payment"` |
| `MsgPaymentCreateFailed` | `"Failed to create order"` |

3. `TopUp` 생성(346-347줄)에서 `TargetType: getTopUpTargetType(c),`·`TargetId: getTopUpTargetId(c),` 두 줄 삭제(O3에서 복원).
4. `PayPalWebhook` 시작의 `if strings.TrimSpace(setting.PayPalWebhookID) == "" {` 게이트를 `if !isPayPalWebhookEnabled() {`로 바꾸고 `c.AbortWithStatus(http.StatusForbidden)`로 응답하도록 맞춘다(upstream `StripeWebhook` 164-170줄과 같은 형태).
5. `go build ./controller/`가 보고하는 나머지 미정의 심볼(있다면)을 upstream 이름으로 바꾼다. `rejectInvalidCreditedQuota`, `LockOrder`, `UnlockOrder`, `model.GetTopUpByTradeNo`, `model.GetUserById`, `common.ValidateRedirectURL`, `common.Sha1`, `common.GetTopupGroupRatio`는 upstream에 같은 시그니처로 있다.

- [ ] **Step 4: 라우트**

`router/fork-router.go`:
- `registerForkPublicRoutes`에 추가:

```go
	apiRouter.GET("/paypal/capture", controller.PayPalCapture)
	apiRouter.POST("/paypal/webhook", bodyLimit, controller.PayPalWebhook)
```

- `registerForkSelfRoutes`:

```go
func registerForkSelfRoutes(selfRoute *gin.RouterGroup) {
	selfRoute.POST("/paypal/pay", middleware.CriticalRateLimit(), controller.RequestPayPalPay)
	selfRoute.POST("/paypal/amount", controller.RequestPayPalAmount)
}
```

- [ ] **Step 5: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./controller/ -run 'PayPal' -v 2>&1 | tail -10 && GOWORK=off go test ./controller/ ./router/ 2>&1 | tail -3
```

Expected: 3개 PASS, 패키지 ok. `PayPalWebhook`가 403이 아닌 다른 코드를 돌려주면 Step 3-4를 다시 본다. `RequestPayPalPay`가 수용량 검사보다 앞에서 거절하면(예: 사용자 그룹 조회) 테스트의 사용자 행에 `Group: "default"`를 더한다; `success_url`/`cancel_url`은 비어 있으면 검증하지 않는다(team 302·307줄).

- [ ] **Step 6: 커밋**

```bash
git add controller/topup_paypal.go controller/topup_paypal_test.go router/fork-router.go
git commit -m "feat(paypal): port the PayPal order, capture and webhook handlers

Messages use upstream's paymentError/NewMessage shape; organization
targets return with the organization overlay.

Ported-From: team controller/topup_paypal.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: O2 PayPal 프런트(지갑)

**Files:**
- Modify: `web/src/features/wallet/constants.ts`, `web/src/features/wallet/types.ts`, `web/src/features/wallet/api.ts`, `web/src/features/wallet/lib/payment.ts`, `web/src/features/wallet/hooks/use-payment.ts`, `web/src/features/wallet/hooks/use-payment.test.ts`, `web/src/features/wallet/components/recharge-form-card.tsx:133-137`

**Interfaces:**
- Produces: `PAYMENT_TYPES.PAYPAL = 'paypal'`, `isPayPalPayment(type)`, `calculatePayPalAmount`, `requestPayPalPayment`, `PaymentAmountCalculators.paypal`, `TopupInfo.enable_paypal_topup?/paypal_min_topup?`.

- [ ] **Step 1: 테스트 추가**

`use-payment.test.ts`의 기존 `describe('payment amount routing')` 안에:

```ts
  test('uses the dedicated PayPal amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(50, PAYMENT_TYPES.PAYPAL, {
      regular: async () => { calls.push('regular'); return { success: true, data: '1' } },
      stripe: async () => { calls.push('stripe'); return { success: true, data: '2' } },
      paypal: async (request) => { calls.push(`paypal:${request.amount}`); return { success: true, data: '50.00' } },
      waffo: async () => { calls.push('waffo'); return { success: true, data: '3' } },
      waffoPancake: async () => { calls.push('pancake'); return { success: true, data: '4' } },
    })
    expect(calls).toEqual(['paypal:50'])
    expect(amount).toBe(50)
  })
```

기존 테스트의 calculators 리터럴에도 `paypal: async () => ({ success: true, data: '0' }),`를 넣는다(인터페이스가 필수 필드가 되므로).

- [ ] **Step 2: 실패 확인**

```bash
cd web && bun run test -- use-payment 2>&1 | tail -6; cd ..
```

Expected: `PAYMENT_TYPES.PAYPAL` undefined 또는 타입 오류로 실패.

- [ ] **Step 3: 구현**

- `constants.ts`: `PAYMENT_TYPES`에 `PAYPAL: 'paypal',`(STRIPE 뒤), `PAYMENT_ICON_COLORS`에 `[PAYMENT_TYPES.PAYPAL]: '#003087',`.
- `types.ts`: `StripePaymentResponse` 옆에 `export type PayPalPaymentResponse = ApiResponse<{ pay_link: string }>`; 요청 타입 옆에 `export interface PayPalPaymentRequest { amount: number; payment_method: 'paypal' }`; `TopupInfo`에 `enable_paypal_topup?: boolean` 과 `paypal_min_topup?: number`.
- `api.ts`: `calculateStripeAmount` 뒤에

```ts
export async function calculatePayPalAmount(request: AmountRequest): Promise<AmountResponse> {
  const res = await api.post<AmountResponse>('/api/user/paypal/amount', request)
  return res.data
}
```

`requestStripePayment` 뒤에

```ts
export async function requestPayPalPayment(request: PayPalPaymentRequest): Promise<PayPalPaymentResponse> {
  const res = await api.post<PayPalPaymentResponse>('/api/user/paypal/pay', request)
  return res.data
}
```

(기존 Stripe 함수의 타입 이름을 따른다. `AmountRequest`/`AmountResponse` 이름이 다르면 그 파일의 이름을 쓴다.)
- `lib/payment.ts`: `isStripePayment` 뒤에 `export function isPayPalPayment(paymentType: string): boolean { return paymentType === PAYMENT_TYPES.PAYPAL }`; `getMinTopupAmount`의 Stripe 분기 뒤에 `if (topupInfo.enable_paypal_topup) { return topupInfo.paypal_min_topup || DEFAULT_MIN_TOPUP }`.
- `hooks/use-payment.ts`:
  - import에 `calculatePayPalAmount`, `requestPayPalPayment`, `isPayPalPayment`.
  - `PaymentAmountCalculators`에 `paypal: AmountCalculator`, 기본값에 `paypal: calculatePayPalAmount`.
  - `requestPaymentAmount`의 분기: `if (isStripePayment(paymentType)) {...} else if (isPayPalPayment(paymentType)) { calculator = calculators.paypal } else if ...`.
  - `processPayment`: 

```ts
        const isStripe = isStripePayment(paymentType)
        const isPayPal = isPayPalPayment(paymentType)
        const amount = Math.floor(topupAmount)
        let response
        if (isStripe) {
          response = await requestStripePayment({ amount, payment_method: 'stripe' })
        } else if (isPayPal) {
          response = await requestPayPalPayment({ amount, payment_method: 'paypal' })
        } else {
          response = await requestPayment({ amount, payment_method: paymentType })
        }
        ...
        if ((isStripe || isPayPal) && response.data?.pay_link) {
          const payLink = response.data.pay_link as string
          if (isPayPal) {
            // Server-side return flow: PayPal sends the browser back to
            // /api/paypal/capture, so navigate the current window.
            window.location.href = payLink
          } else {
            window.open(payLink, '_blank')
          }
          toast.success(i18next.t('Redirecting to payment page...'))
          return true
        }
        if (!isStripe && !isPayPal && response.data) { ...기존 epay 분기... }
```

- `recharge-form-card.tsx` `hasConfigurableTopup`(133-137줄)에 `topupInfo?.enable_paypal_topup ||` 추가.

- [ ] **Step 4: 검증**

```bash
cd web && bun run test -- wallet 2>&1 | tail -6 && bun run typecheck && bun run lint && cd ..
```

Expected: wallet 테스트 PASS, typecheck/lint 통과.

- [ ] **Step 5: 커밋**

```bash
git add web/src/features/wallet
git commit -m "feat(web): add PayPal to the wallet top-up flow

Ported-From: team web/default/src/features/wallet
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 12: O2 PayPal 프런트(결제 설정 탭)

**Files:**
- Modify: `web/src/features/system-settings/types.ts:328`, `web/src/features/system-settings/billing/index.tsx:82`, `web/src/features/system-settings/billing/section-registry.tsx:151`, `web/src/features/system-settings/integrations/payment-settings-section.tsx`, `web/src/i18n/locales/en.json`

- [ ] **Step 1: 타입·기본값·레지스트리**

- `types.ts` Stripe 필드 블록(323-328) 뒤에

```ts
  PayPalClientId: string
  PayPalClientSecret: string
  PayPalWebhookID: string
  PayPalSandbox: boolean
  PayPalUnitPrice: number
  PayPalMinTopUp: number
```

- `billing/index.tsx` Stripe 기본값(77-82) 뒤에 `PayPalClientId: '', PayPalClientSecret: '', PayPalWebhookID: '', PayPalSandbox: false, PayPalUnitPrice: 1, PayPalMinTopUp: 1,`.
- `billing/section-registry.tsx` Stripe 매핑(146-151) 뒤에 `PayPalClientId: settings.PayPalClientId ?? '', PayPalClientSecret: settings.PayPalClientSecret ?? '', PayPalWebhookID: settings.PayPalWebhookID ?? '', PayPalSandbox: settings.PayPalSandbox ?? false, PayPalUnitPrice: settings.PayPalUnitPrice ?? 1, PayPalMinTopUp: settings.PayPalMinTopUp ?? 1,`.

- [ ] **Step 2: payment-settings-section.tsx**

- zod 스키마(Stripe 146-151 뒤): team `web/default/src/features/system-settings/integrations/payment-settings-section.tsx:139-144`의 PayPal 6줄.
- `sanitized`(432-437 뒤)와 `initial`(476-482 뒤)에 team 567-572 / 632-637의 PayPal 항목.
- update push: Stripe 블록(567-604) 뒤에 team 778-809의 PayPal 블록(`PayPalClientId`·`PayPalClientSecret`은 비어 있지 않고 바뀐 경우만, 나머지는 바뀌면 push).
- `TabsList`: `grid-cols-6` → `grid-cols-7`, `min-w-[44rem]` → `min-w-[52rem]`, `<TabsTrigger value='paypal'>PayPal</TabsTrigger>`를 Stripe 뒤에.
- Stripe `TabsContent`(1261-1451) 뒤, Creem 앞에 `<TabsContent value='paypal' className={paymentTabContentClassName}>…</TabsContent>`. 내용은 team 1791-1984의 섹션을 그대로 옮기되 바깥 `<Separator/>`와 "Clear" 버튼(`clearPayPalSettings`, 1964-1982)은 제외한다(upstream에 해당 패턴이 없음). 안내 상자의 Return URL은 `<ServerAddress>/api/paypal/capture`, Webhook URL은 `<ServerAddress>/api/paypal/webhook`, 이벤트 `CHECKOUT.ORDER.APPROVED`, `PAYMENT.CAPTURE.COMPLETED`.

- [ ] **Step 3: i18n**

`en.json`에 추가: `PayPal Gateway`, `Configuration for PayPal payment integration`, `Callback Configuration:`, `Return URL:`, `PayPal Developer Dashboard`, `Enter PayPal Client ID`, `PayPal Client ID (public identifier)`, `PayPal application Client Secret`, `Leave blank unless updating`, `Webhook ID`, `Webhook ID from PayPal Developer Dashboard`, `Used to verify PayPal webhook signatures`, `Use the PayPal sandbox environment`. `cd web && bun run i18n:sync`.

- [ ] **Step 4: 검증**

```bash
cd web && bun run typecheck && bun run lint && bun run test -- system-settings 2>&1 | tail -5 && bun run copyright:check && cd ..
```

Expected: 통과.

- [ ] **Step 5: 커밋**

```bash
git add web/src/features/system-settings web/src/i18n/locales
git commit -m "feat(web): add the PayPal tab to payment settings

Ported-From: team web/default/src/features/system-settings/integrations/payment-settings-section.tsx
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 13: O1 랜딩·가이드 페이지

upstream은 서버 세션(`gin-contrib/sessions`)이 없고 SPA를 전부 `NoRoute` 체인에서 낸다. 랜딩은 (1) 명시적 GET 라우트 8개(페이지 4 + 301 리다이렉트 4)와 (2) `NoRoute` 체인에 끼우는 정적 자산 핸들러 하나로 올린다. 로그인 여부는 upstream의 힌트 쿠키 `new_api_has_session=1`로만 판단하고, 사용자명·역할·그룹 배지와 로그아웃 버튼은 없앤다(사용자는 "대시보드" 링크로 콘솔에 간다). GA4 ID는 upstream SPA와 같은 `GOOGLE_ANALYTICS_ID` 환경 변수에서 읽는다.

위치는 spec §4.2의 `web/public/landing/…`가 아니라 **`web/landing/{templates,public}`**이다. `web/public`은 rsbuild가 `web/dist`로 복사해 209 MB를 두 번 embed하고 `ServeFrontendFiles`가 mp4를 메모리에 gzip 캐시하므로 피한다(spec §4.2는 Task 16에서 고친다).

**Files:**
- Create: `web/landing/templates/*.tmpl`(9), `web/landing/public/**`(team 61파일 중 미참조 로고 4개 제외), `router/landing-router.go`, `router/landing_router_test.go`
- Modify: `main.go:44-48, 205-208`, `router/web-router.go:16-19, 21-36`, `web/public/favicon.ico`, `web/public/logo.png`

**Interfaces:**
- Consumes: `service.SessionHintCookieName`, `service.SessionHintCookieValue`, `common.EmbedFolder(embed.FS, string) static.ServeFileSystem`, `static.LocalFile(root, indexes)`, `common.DebugEnabled`.
- Produces: `router.WebAssets.LandingPublicFS fs.FS`, `router.WebAssets.LandingTemplatesFS fs.FS`(embed.FS 할당 가능), `registerLandingRoutes(router *gin.Engine, assets WebAssets, handlers ...gin.HandlerFunc)`, `landingStatic(assets WebAssets) gin.HandlerFunc`, `landingPageData{Active string; IsLoggedIn bool; GoogleAnalyticsID string}`, `renderLandingTemplate(tmpl, name string, data landingPageData) []byte`, `buildAssetETags(fs.FS, root string) (map[string]string, error)`, `etagMatches(header, current string) bool`, `prefersEnglish(acceptLanguage string) bool`.

- [ ] **Step 1: 자산 복사**

```bash
mkdir -p web/landing
git archive team web/default/templates | tar -x -C /tmp && mv /tmp/web/default/templates web/landing/templates
git archive team web/default/public | tar -x -C /tmp && mv /tmp/web/default/public web/landing/public
rm -rf /tmp/web
# SPA 자산은 upstream web/public 것을 쓴다; 브랜드 파일 두 개만 덮어쓴다.
cp web/landing/public/favicon.ico web/public/favicon.ico
cp web/landing/public/logo.png web/public/logo.png
rm web/landing/public/pay-apple.png web/landing/public/pay-card.png web/landing/public/pay-google.png web/landing/public/waffo-logo-dark.svg web/landing/public/waffo-logo-light.svg
rm web/landing/public/logo_anthropic.svg web/landing/public/logo_claude.svg web/landing/public/logo_gemini.svg web/landing/public/logo_openai.svg
rm "web/landing/public/AlRouter Quick Start Guide_v1.0.pdf" "web/landing/public/AlRouter Quick Start Guide_EN_v1.0.pdf"   # 대시보드 전용, O1 범위 밖
ls web/landing/templates | wc -l   # 9
find web/landing/public -type f | wc -l   # 57
```

(`favicon.ico`/`logo.png`는 랜딩 템플릿이 `/logo.png`를 참조하므로 `web/landing/public`에도 남겨 둔다.)

- [ ] **Step 2: 템플릿·스크립트 수정**

1. `web/landing/templates/analytics.tmpl`를 다음으로 교체:

```
{{define "google_analytics"}}{{if .GoogleAnalyticsID}}
    <script async src="https://www.googletagmanager.com/gtag/js?id={{.GoogleAnalyticsID}}"></script>
    <script>
      window.dataLayer = window.dataLayer || [];
      function gtag(){dataLayer.push(arguments);}
      gtag('js', new Date());
      gtag('config', {{.GoogleAnalyticsID}});
    </script>
{{end}}{{end}}
```

2. `header_ko.tmpl` 51-69줄의 `{{if .IsLoggedIn}} … {{else}}` 블록에서 `<div class="profile-menu">…</div>` 전체를 `<a href="/dashboard" class="btn-login">대시보드</a>`로 바꾼다. `header_en.tmpl` 같은 줄은 `<a href="/dashboard" class="btn-login">Dashboard</a>`.
3. `web/landing/public/popup.js`의 `wireProfileMenu` IIFE(176-210줄 부근, `.pd-logout` fetch 포함)를 삭제한다. `grep -n "profile-menu\|pd-logout\|/api/user/logout" web/landing/public/*.js web/landing/templates/*.tmpl` 결과가 비어야 한다.
4. `web/landing/public/landing.js` 문의 폼 제출(84-92줄): `if (!res.ok) throw new Error();` 다음에 `const payload = await res.json().catch(() => null); if (!payload || payload.success !== true) throw new Error();`를 넣는다(백엔드는 검증 실패를 HTTP 200 + `success:false`로 낸다).
5. 템플릿에서 `.DisplayName`·`.UserInitial`·`.UserAvatarBg`·`.UserRole`·`.UserGroup`·`.Username` 참조가 남아 있지 않은지: `grep -n "DisplayName\|UserInitial\|UserAvatarBg\|UserRole\|UserGroup\|\.Username" web/landing/templates/*.tmpl` → 없음.

- [ ] **Step 3: 테스트 작성**

`router/landing_router_test.go`:

```go
package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildAssetETags(t *testing.T) {
	files := fstest.MapFS{
		"public/landing.css": {Data: []byte("body { color: white; }")},
		"public/logo.svg":    {Data: []byte("<svg></svg>")},
	}
	etags, err := buildAssetETags(files, "public")
	require.NoError(t, err)
	require.NotEmpty(t, etags["/landing.css"])
	require.NotEmpty(t, etags["/logo.svg"])
	require.NotEqual(t, etags["/landing.css"], etags["/logo.svg"])
	changed, err := buildAssetETags(fstest.MapFS{"public/landing.css": {Data: []byte("body { color: black; }")}}, "public")
	require.NoError(t, err)
	require.NotEqual(t, etags["/landing.css"], changed["/landing.css"])
}

func newLandingTestEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	assets := WebAssets{LandingTemplatesFS: os.DirFS(".."), LandingPublicFS: os.DirFS("..")}
	router := gin.New()
	registerLandingRoutes(router, assets)
	router.NoRoute(landingStatic(assets), func(c *gin.Context) { c.String(http.StatusNotFound, "spa") })
	return router
}

func get(t *testing.T, router *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestLandingPagesPickLanguageAndRedirectLegacyPaths(t *testing.T) {
	router := newLandingTestEngine(t)
	ko := get(t, router, "/", map[string]string{"Accept-Language": "ko-KR,ko;q=0.9,en;q=0.5"})
	require.Equal(t, http.StatusOK, ko.Code)
	require.Contains(t, ko.Body.String(), `lang="ko"`)
	require.Equal(t, "no-cache, must-revalidate", ko.Header().Get("Cache-Control"))
	en := get(t, router, "/", map[string]string{"Accept-Language": "en-US,en;q=0.9"})
	require.Contains(t, en.Body.String(), `lang="en"`)
	require.Contains(t, get(t, router, "/en/guide", nil).Body.String(), `lang="en"`)
	legacy := get(t, router, "/guide_en.html", nil)
	require.Equal(t, http.StatusMovedPermanently, legacy.Code)
	require.Equal(t, "/en/guide", legacy.Header().Get("Location"))
}

func TestLandingPagesReflectSessionHint(t *testing.T) {
	router := newLandingTestEngine(t)
	anon := get(t, router, "/en", nil)
	require.Contains(t, anon.Body.String(), `data-logged-in="false"`)
	require.Contains(t, anon.Body.String(), `href="/sign-in"`)
	signed := get(t, router, "/en", map[string]string{"Cookie": service.SessionHintCookieName + "=" + service.SessionHintCookieValue})
	require.Contains(t, signed.Body.String(), `data-logged-in="true"`)
	require.Contains(t, signed.Body.String(), `href="/dashboard"`)
	require.NotContains(t, signed.Body.String(), "profile-menu")
}

func TestLandingAnalyticsFollowsEnv(t *testing.T) {
	router := newLandingTestEngine(t)
	require.NotContains(t, get(t, router, "/en/guide", nil).Body.String(), "googletagmanager")
	t.Setenv("GOOGLE_ANALYTICS_ID", "G-TEST123")
	router = newLandingTestEngine(t)
	body := get(t, router, "/en/guide", nil).Body.String()
	require.Contains(t, body, "gtag/js?id=G-TEST123")
	require.Contains(t, body, `"G-TEST123"`)
}

func TestLandingStaticAssetsRevalidateWithETag(t *testing.T) {
	router := newLandingTestEngine(t)
	first := get(t, router, "/landing.css", nil)
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, "no-cache, must-revalidate", first.Header().Get("Cache-Control"))
	etag := first.Header().Get("ETag")
	require.True(t, strings.HasPrefix(etag, `"`), etag)
	second := get(t, router, "/landing.css", map[string]string{"If-None-Match": etag})
	require.Equal(t, http.StatusNotModified, second.Code)
	require.Equal(t, http.StatusNotFound, get(t, router, "/does-not-exist.css", nil).Code)
	require.Equal(t, "spa", get(t, router, "/does-not-exist.css", nil).Body.String())
}
```

- [ ] **Step 4: 실패 확인**

```bash
GOWORK=off go test ./router/ -run 'TestLanding|TestBuildAssetETags' 2>&1 | head -5
```

Expected: 컴파일 실패.

- [ ] **Step 5: landing-router.go**

```go
package router

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// fork: O1 — server-rendered landing/guide pages and their static assets.
// Hooks: router/web-router.go (registerLandingRoutes, landingStatic, WebAssets
// fields) and main.go (embeds). Assets live in web/landing/{templates,public}.

const (
	landingTemplatesDir   = "web/landing/templates"
	landingPublicDir      = "web/landing/public"
	landingRevalidate     = "no-cache, must-revalidate"
	landingHTMLContentType = "text/html; charset=utf-8"
)

type landingPageData struct {
	Active            string // "about" (index pages) or "guide"
	IsLoggedIn        bool   // from upstream's session hint cookie only
	GoogleAnalyticsID string // GOOGLE_ANALYTICS_ID, same env as the SPA
}

var landingPages = []struct{ name, active string }{
	{"index_ko", "about"}, {"index_en", "about"}, {"guide_ko", "guide"}, {"guide_en", "guide"},
}

func landingPageDataFor(c *gin.Context, active string) landingPageData {
	data := landingPageData{Active: active, GoogleAnalyticsID: os.Getenv("GOOGLE_ANALYTICS_ID")}
	if c != nil {
		if v, err := c.Cookie(service.SessionHintCookieName); err == nil && v == service.SessionHintCookieValue {
			data.IsLoggedIn = true
		}
	}
	return data
}

func renderLandingTemplate(tmpl *template.Template, name string, data landingPageData) []byte {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		common.SysLog("failed to render landing template " + name + ": " + err.Error())
		return nil
	}
	return buf.Bytes()
}

func prefersEnglish(acceptLanguage string) bool {
	lower := strings.ToLower(acceptLanguage)
	return strings.Contains(lower, "en") && !strings.HasPrefix(lower, "ko")
}

// buildAssetETags hashes every file under root so stable-name landing assets
// can be revalidated with If-None-Match after a deploy.
func buildAssetETags(fileSystem fs.FS, root string) (map[string]string, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(fileSystem, root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		file, err := fileSystem.Open(filePath)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		urlPath := "/" + strings.TrimPrefix(strings.TrimPrefix(filePath, root), "/")
		etags[urlPath] = `"` + hex.EncodeToString(hash.Sum(nil)) + `"`
		return nil
	})
	return etags, err
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == current {
			return true
		}
	}
	return false
}

// registerLandingRoutes registers the four pages and the legacy .html redirects.
// handlers (gzip, web rate limit) come from SetWebRouter so the pages share the
// SPA's protections.
func registerLandingRoutes(router *gin.Engine, assets WebAssets, handlers ...gin.HandlerFunc) {
	group := router.Group("", handlers...)
	var render func(c *gin.Context, name, active string)
	if common.DebugEnabled {
		common.SysLog("landing dev mode (DEBUG=true): templates/public served from disk")
		render = func(c *gin.Context, name, active string) {
			tmpl, err := template.ParseGlob(landingTemplatesDir + "/*.tmpl")
			if err != nil {
				c.String(http.StatusInternalServerError, "landing templates: %v", err)
				return
			}
			c.Data(http.StatusOK, landingHTMLContentType, renderLandingTemplate(tmpl, name, landingPageDataFor(c, active)))
		}
	} else {
		tmpl := template.Must(template.ParseFS(assets.LandingTemplatesFS, landingTemplatesDir+"/*.tmpl"))
		anonymous := make(map[string][]byte, len(landingPages))
		for _, page := range landingPages {
			anonymous[page.name] = renderLandingTemplate(tmpl, page.name, landingPageDataFor(nil, page.active))
		}
		render = func(c *gin.Context, name, active string) {
			data := landingPageDataFor(c, active)
			if !data.IsLoggedIn {
				c.Data(http.StatusOK, landingHTMLContentType, anonymous[name])
				return
			}
			c.Data(http.StatusOK, landingHTMLContentType, renderLandingTemplate(tmpl, name, data))
		}
	}
	page := func(name, active string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", landingRevalidate)
			render(c, name, active)
		}
	}
	group.GET("/", func(c *gin.Context) {
		if prefersEnglish(c.GetHeader("Accept-Language")) {
			page("index_en", "about")(c)
			return
		}
		page("index_ko", "about")(c)
	})
	group.GET("/en", page("index_en", "about"))
	group.GET("/guide", page("guide_ko", "guide"))
	group.GET("/en/guide", page("guide_en", "guide"))
	for from, to := range map[string]string{"/index.html": "/", "/index_en.html": "/en", "/guide.html": "/guide", "/guide_en.html": "/en/guide"} {
		group.GET(from, func(c *gin.Context) { c.Redirect(http.StatusMovedPermanently, to) })
	}
}

// landingStatic serves web/landing/public with ETag revalidation and aborts;
// any other path falls through to the SPA handlers.
func landingStatic(assets WebAssets) gin.HandlerFunc {
	var publicFS static.ServeFileSystem
	etags := map[string]string{}
	if common.DebugEnabled {
		publicFS = static.LocalFile(landingPublicDir, false)
	} else {
		publicFS = newLandingFileSystem(assets.LandingPublicFS)
		built, err := buildAssetETags(assets.LandingPublicFS, landingPublicDir)
		if err != nil {
			common.SysLog("failed to build landing asset ETags: " + err.Error())
		} else {
			etags = built
		}
	}
	fileServer := http.FileServer(publicFS)
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		if strings.HasSuffix(urlPath, "/") || !publicFS.Exists("/", urlPath) {
			return
		}
		c.Header("Cache-Control", landingRevalidate)
		if etag, ok := etags[urlPath]; ok {
			c.Header("ETag", etag)
			if (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && etagMatches(c.GetHeader("If-None-Match"), etag) {
				c.Status(http.StatusNotModified)
				c.Abort()
				return
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
		c.Abort()
	}
}

// landingFileSystem adapts any fs.FS rooted at the repo (embed.FS in production,
// os.DirFS in tests) to gin-contrib/static. common.EmbedFolder only takes embed.FS.
type landingFileSystem struct{ http.FileSystem }

func (l landingFileSystem) Exists(prefix string, path string) bool {
	f, err := l.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func newLandingFileSystem(root fs.FS) static.ServeFileSystem {
	sub, err := fs.Sub(root, landingPublicDir)
	if err != nil {
		panic(err)
	}
	return landingFileSystem{http.FS(sub)}
}
```

`static.ServeFileSystem`은 `http.FileSystem` + `Exists(prefix, path string) bool`이다. `common.EmbedFolder`는 쓰지 않는다.

- [ ] **Step 6: web-router.go·main.go 훅**

`router/web-router.go`:
- import에 `"io/fs"` 추가.
- `WebAssets`에

```go
	LandingPublicFS    fs.FS // fork: O1 web/landing/public
	LandingTemplatesFS fs.FS // fork: O1 web/landing/templates
```

- `SetWebRouter`: `compress := gzip.Gzip(gzip.DefaultCompression)` 다음 줄에

```go
	registerLandingRoutes(router, assets, compress, middleware.GlobalWebRateLimit(frontendFS)) // fork: O1
```

- `router.NoRoute(...)` 체인에서 `middleware.AccessTokenAudit(),` 다음, `middleware.GlobalWebRateLimit(frontendFS),` 앞에

```go
		landingStatic(assets), // fork: O1
```

`main.go`:
- 48줄 `var indexPage []byte` 뒤에

```go
//go:embed web/landing/public
var landingPublicFS embed.FS // fork: O1

//go:embed web/landing/templates
var landingTemplatesFS embed.FS // fork: O1
```

- `router.SetRouter(server, router.WebAssets{...})`에 `LandingPublicFS: landingPublicFS, LandingTemplatesFS: landingTemplatesFS, // fork: O1`.

- [ ] **Step 7: 통과 확인**

```bash
GOWORK=off go build ./... && GOWORK=off go test ./router/ -run 'TestLanding|TestBuildAssetETags' -v 2>&1 | tail -15 && GOWORK=off go test ./router/ ./middleware/ 2>&1 | tail -3
```

Expected: 5개 PASS. `go vet ./router/`도 깨끗해야 한다(redirect 클로저는 Go 1.22+ 반복 변수 의미로 안전).

- [ ] **Step 8: 수동 확인**

```bash
PORT=3000 GOWORK=off go run . &
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' -H 'Accept-Language: ko' http://127.0.0.1:3000/
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:3000/videos/main/alrouter_poster.png
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:3000/dashboard
```

Expected: `200 text/html`, `200`, `200`(SPA index). 서버를 종료한다.

- [ ] **Step 9: 커밋**

`docs/fork-overlays.md`에 O1 행(web-router.go 훅 2줄 + 필드 2, main.go embed 2 + 필드 2, web/public 브랜드 파일 2) 추가.

```bash
git add web/landing web/public/favicon.ico web/public/logo.png router/landing-router.go router/landing_router_test.go router/web-router.go main.go docs/fork-overlays.md
git commit -m "feat(landing): serve the landing and guide pages from web/landing on the upstream router

Login state comes from upstream's session hint cookie; the header shows a
dashboard link instead of the profile menu. GA4 reads GOOGLE_ANALYTICS_ID.

Ported-From: team router/web-router.go web/default/templates web/default/public
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 14: O8 백엔드 (OIDC 도메인 화이트리스트, 로그 라우팅 필드 숨김)

O8 중 백엔드 항목의 처리: OIDC 도메인 화이트리스트(이식), 사용자 로그에서 `is_model_mapped`/`upstream_model_name` 숨김(이식), 옵션 64-bit 파싱(upstream은 Go `int`=64-bit이므로 **no-op**, 기록만), 정적 자산 rate limit/캐시(Task 13의 `landingStatic`이 담당, SPA 자산은 upstream 정책 유지).

**Files:**
- Create: `controller/oauth_email_domain.go`, `controller/oauth_email_domain_test.go`, `model/log_other_fork.go`, `model/log_other_fork_test.go`
- Modify: `controller/oauth.go:333-335`(`handleOAuthLogin`), `model/log_other.go:253`(user 분기)

**Interfaces:**
- Consumes: `oauth.Provider`, `*oauth.OIDCProvider`, `oauth.OAuthUser.Email`, `common.EmailDomainRestrictionEnabled`, `common.EmailDomainWhitelist []string`, `formatLogOtherJSON(value string, visibility logOtherVisibility) string`, `logOtherVisibilityUser/Admin`.
- Produces: `enforceOIDCEmailDomain(provider oauth.Provider, user *oauth.OAuthUser) error`, `forkUserHiddenLogOtherKeys`, `deleteLogOtherKeys(values map[string]json.RawMessage, keys []string) bool`.

- [ ] **Step 1: 테스트 작성**

`controller/oauth_email_domain_test.go`:

```go
package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/stretchr/testify/require"
)

func TestEnforceOIDCEmailDomain(t *testing.T) {
	oldEnabled, oldList := common.EmailDomainRestrictionEnabled, common.EmailDomainWhitelist
	common.EmailDomainRestrictionEnabled = true
	common.EmailDomainWhitelist = []string{"example.com"}
	t.Cleanup(func() { common.EmailDomainRestrictionEnabled, common.EmailDomainWhitelist = oldEnabled, oldList })

	oidc := &oauth.OIDCProvider{}
	require.NoError(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "ok@Example.com"}))
	require.Error(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "no@other.com"}))
	require.Error(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "malformed"}))
	// Only OIDC is restricted; other providers keep upstream behaviour.
	require.NoError(t, enforceOIDCEmailDomain(&oauth.GitHubProvider{}, &oauth.OAuthUser{Email: "no@other.com"}))

	common.EmailDomainRestrictionEnabled = false
	require.NoError(t, enforceOIDCEmailDomain(oidc, &oauth.OAuthUser{Email: "no@other.com"}))
}
```

`model/log_other_fork_test.go`:

```go
package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserLogsHideModelRouting(t *testing.T) {
	const other = `{"is_model_mapped":true,"upstream_model_name":"openai/gpt-x","frt":12}`
	user := formatLogOtherJSON(other, logOtherVisibilityUser)
	require.NotContains(t, user, "upstream_model_name")
	require.NotContains(t, user, "is_model_mapped")
	require.Contains(t, user, `"frt":12`)
	admin := formatLogOtherJSON(other, logOtherVisibilityAdmin)
	require.Contains(t, admin, `"upstream_model_name":"openai/gpt-x"`)
}
```

- [ ] **Step 2: 실패 확인**

```bash
GOWORK=off go test ./controller/ -run TestEnforceOIDCEmailDomain 2>&1 | head -3
GOWORK=off go test ./model/ -run TestUserLogsHideModelRouting 2>&1 | tail -3
```

Expected: 첫 번째 컴파일 실패, 두 번째 FAIL(키가 남아 있음).

- [ ] **Step 3: 구현**

`controller/oauth_email_domain.go`:

```go
package controller

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
)

// fork: O8 — the email domain whitelist also applies to OIDC sign-in, for new
// and existing accounts alike (team a8d74c438). Other providers are unchanged.
func enforceOIDCEmailDomain(provider oauth.Provider, user *oauth.OAuthUser) error {
	if _, isOIDC := provider.(*oauth.OIDCProvider); !isOIDC {
		return nil
	}
	if !common.EmailDomainRestrictionEnabled {
		return nil
	}
	parts := strings.Split(strings.TrimSpace(user.Email), "@")
	if len(parts) != 2 || parts[1] == "" {
		return errors.New("Please enter a valid email address")
	}
	domain := strings.ToLower(parts[1])
	allowed := slices.ContainsFunc(common.EmailDomainWhitelist, func(entry string) bool {
		return strings.EqualFold(strings.TrimSpace(entry), domain)
	})
	if !allowed {
		return errors.New("This email address is not allowed by the administrator's email policy.")
	}
	return nil
}
```

`controller/oauth.go` `handleOAuthLogin`: `findOrCreateOAuthUser` 오류 처리 블록이 끝난 뒤(`// 8. Check user status` 앞)에

```go
	if err := enforceOIDCEmailDomain(provider, oauthUser); err != nil { // fork: O8
		common.ApiErrorT(c, err.Error())
		return
	}
```

`model/log_other_fork.go`:

```go
package model

import "encoding/json"

// fork: O8 — routing details hidden from end users (admins still see them).
var forkUserHiddenLogOtherKeys = []string{"is_model_mapped", "upstream_model_name"}

func deleteLogOtherKeys(values map[string]json.RawMessage, keys []string) bool {
	changed := false
	for _, key := range keys {
		if _, exists := values[key]; exists {
			delete(values, key)
			changed = true
		}
	}
	return changed
}
```

`model/log_other.go` `formatLogOtherJSON`의 `if visibility == logOtherVisibilityUser {` 블록 끝(두 번째 for 루프 뒤)에

```go
		changed = deleteLogOtherKeys(values, forkUserHiddenLogOtherKeys) || changed // fork: O8
```

- [ ] **Step 4: 통과 확인**

```bash
GOWORK=off go test ./controller/ -run TestEnforceOIDCEmailDomain -v 2>&1 | tail -3
GOWORK=off go test ./model/ -run 'TestUserLogsHideModelRouting|TestFormatLogOther' -v 2>&1 | tail -6
GOWORK=off go test ./controller/ ./model/ 2>&1 | tail -3
```

Expected: PASS; upstream `log_other_test.go`도 그대로 통과.

- [ ] **Step 5: 커밋**

`docs/fork-overlays.md`에 O8 행 2개(oauth.go 훅, log_other.go 훅)와 "옵션 64-bit 파싱: upstream Go int가 64-bit이므로 적용 안 함" 메모 추가.

```bash
git add controller/oauth_email_domain.go controller/oauth_email_domain_test.go controller/oauth.go model/log_other_fork.go model/log_other_fork_test.go model/log_other.go docs/fork-overlays.md
git commit -m "feat(settings): enforce the email domain whitelist on OIDC login and hide model routing from user logs

Ported-From: team controller/oidc.go model/log.go
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 15: O8 프런트 (큰 수 약어, 가격 페이지 모델 상세 숨김)

인터페이스 언어 제한(en/ko)은 ko 로케일이 생기는 단계 D로 미루고, "기본 첫 화면 콘솔"은 team에서 이미 되돌려져 있어(d79a1ddd8) 가져오지 않는다.

**Files:**
- Create: `web/src/lib/__tests__/number-suffix.test.ts`
- Modify: `web/src/lib/currency.ts:269-283`, `web/src/lib/format.ts:248-253`, `web/src/features/pricing/index.tsx`, `web/src/features/pricing/components/model-card.tsx:396-410`, `web/src/features/pricing/components/model-card-grid.tsx:35,107`, `web/src/features/pricing/components/pricing-table.tsx:42,55,83-87,106-107`, `web/src/features/pricing/__tests__/model-cards.test.tsx`, `web/src/features/rankings/components/entity-links.tsx:43-53`, `web/src/routes/pricing/$modelId/index.tsx:38-56`

- [ ] **Step 1: 숫자 테스트 작성**

`web/src/lib/__tests__/number-suffix.test.ts`:

```ts
import { describe, expect, test } from 'vitest'

import { formatCurrencyFromUSD } from '../currency'
import { formatTokens } from '../format'

describe('formatTokens', () => {
  test('keeps small and mid-range rendering unchanged', () => {
    expect(formatTokens(0)).toBe('-')
    expect(formatTokens(999)).toBe('999')
    expect(formatTokens(1500)).toBe('1.5K')
    expect(formatTokens(1e6)).toBe('1.00M')
  })
  test('uses B and T tiers for large token counts', () => {
    expect(formatTokens(1e9)).toBe('1.00B')
    expect(formatTokens(2e9)).toBe('2.00B')
    expect(formatTokens(1e12)).toBe('1.00T')
    expect(formatTokens(9e15)).toBe('9000.00T') // ~MaxWalletQuota
  })
})

describe('formatCurrencyFromUSD abbreviation tiers', () => {
  test('abbreviates thousands, millions and billions', () => {
    expect(formatCurrencyFromUSD(1500, { abbreviate: true, showSymbol: false })).toBe('1.5k')
    expect(formatCurrencyFromUSD(2_000_000, { abbreviate: true, showSymbol: false })).toBe('2M')
    expect(formatCurrencyFromUSD(5_000_000_000, { abbreviate: true, showSymbol: false })).toBe('5B')
  })
})
```

`formatCurrencyFromUSD`의 옵션 이름(`abbreviate`, `showSymbol`)은 `currency.ts:93-136`의 `CurrencyFormatOptions`를 따른다; 기본 통화가 USD가 아닌 테스트 환경이면 `getCurrencyDisplay()`의 기대 형식에 맞춰 첫 단언의 문자열을 조정한다(단위 접미사 `k/M/B`가 핵심).

- [ ] **Step 2: 실패 확인**

```bash
cd web && bun run test -- number-suffix 2>&1 | tail -8; cd ..
```

Expected: `1.00B`/`5B` 단언 실패(현재 `1000.00M`, `5000000k`).

- [ ] **Step 3: 구현**

`currency.ts` `formatNumberWithSuffix`(269-283) 앞에

```ts
const SUFFIX_TIERS = [
  { threshold: 1e12, suffix: 'T' },
  { threshold: 1e9, suffix: 'B' },
  { threshold: 1e6, suffix: 'M' },
  { threshold: 1e3, suffix: 'k' },
] as const
```

본문의 `if (abbreviate && abs >= 1000) { const result = value / 1000; return ... 'k' }`를

```ts
  if (abbreviate && abs >= 1000) {
    for (const tier of SUFFIX_TIERS) {
      if (abs >= tier.threshold) {
        return removeTrailingZeros((value / tier.threshold).toFixed(1)) + tier.suffix
      }
    }
  }
```

`format.ts` `formatTokens`를

```ts
export function formatTokens(tokens: number): string {
  if (tokens === 0) return '-'
  if (tokens < 1000) return tokens.toString()
  if (tokens < 1_000_000) return `${(tokens / 1000).toFixed(1)}K`
  if (tokens < 1_000_000_000) return `${(tokens / 1_000_000).toFixed(2)}M`
  if (tokens < 1_000_000_000_000) return `${(tokens / 1_000_000_000).toFixed(2)}B`
  return `${(tokens / 1_000_000_000_000).toFixed(2)}T`
}
```

- [ ] **Step 4: 숫자 테스트 통과 + 회귀**

```bash
cd web && bun run test -- number-suffix 2>&1 | tail -5 && bun run test 2>&1 | tail -5; cd ..
```

Expected: PASS. 기존 테스트 중 `1000k` 같은 표기를 단언하는 것이 있으면 새 표기(`1M`)로 고친다.

- [ ] **Step 5: 가격 페이지 모델 상세 숨김**

- `pricing/index.tsx`: `selectedModelName` state(41), `handleModelClick`(86-88), `selectedModel`(90-97), `onModelClick={handleModelClick}`(128, 146), `ModelDetailsDrawer` 블록(263-271)과 관련 import를 제거.
- `components/model-card.tsx`: `onClick` prop과 396-410의 `<Button …>{t('Details')}…</Button>`를 제거(`ModelPerfBadge`는 자식 없이 유지).
- `components/model-card-grid.tsx`: `onModelClick` prop(35)과 전달(107) 제거.
- `components/pricing-table.tsx`: `onModelClick`(42, 55), `handleRowClick`(83-87), 행의 `onClick`/`cursor-pointer`(106-107) 제거.
- `rankings/components/entity-links.tsx` `ModelLink`(43-53): `<Link to='/pricing/$modelId' …>` → `<span className={cn('text-foreground', props.className)}>{props.children ?? props.modelName}</span>`; 사용하지 않게 된 `Link` import 제거.
- `routes/pricing/$modelId/index.tsx`: `beforeLoad`와 `component`를

```ts
export const Route = createFileRoute('/pricing/$modelId/')({
  validateSearch: modelDetailsSearchSchema,
  beforeLoad: () => {
    throw redirect({ to: '/pricing' })
  },
})
```

로 바꾸고 `ModelDetails`, `getModuleAccessForGuard`, `useAuthStore` import를 제거.
- `__tests__/model-cards.test.tsx`: `onModelClick={vi.fn()}` prop(231, 599, 630) 제거; 154·174줄의 "Details" 클릭/활성 단언은 `expect(screen.queryByRole('button', { name: 'Details' })).toBeNull()`로 바꾸고, 560-586의 "calls onModelClick" 테스트는 삭제.

- [ ] **Step 6: 검증**

```bash
cd web && bun run test -- pricing 2>&1 | tail -6 && bun run typecheck && bun run lint && bun run knip 2>&1 | tail -5; cd ..
```

Expected: pricing 테스트 PASS, typecheck 0, lint 통과. `knip`이 `ModelDetailsDrawer`/`ModelDetails`를 미사용으로 보고하면 `components/model-details.tsx`에서 해당 export를 제거하지 말고 `web/knip.config.ts`의 ignore에 추가한다(단계 C에서 할인 UI가 같은 파일을 만진다).

- [ ] **Step 7: 커밋**

```bash
git add web/src/lib web/src/features/pricing web/src/features/rankings 'web/src/routes/pricing/$modelId/index.tsx' web/knip.config.ts
git commit -m "feat(web): add B/T number tiers and hide the pricing model detail views

Ported-From: team web/default/src/lib/currency.ts web/default/src/features/pricing
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

(`web/knip.config.ts`가 바뀌지 않았으면 add 목록에서 뺀다.)

---

### Task 16: 기록·spec 정정·upstream 병합 리허설

**Files:**
- Modify: `docs/fork-overlays.md`, `docs/2026-10-14-rebase-phase-a.md`, `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md`(§4.2, §4.3, §8 O8 메모)

- [ ] **Step 1: 전체 검증**

```bash
GOWORK=off go vet ./... && make test 2>&1 | tail -8
cd web && bun run typecheck && bun run test 2>&1 | tail -4 && bun run lint && bun run copyright:check && bun run build 2>&1 | tail -3 && cd ..
cd tools/unode-diagnostics && node --test test/*.test.mjs && cd ../..
```

Expected: 전부 통과. 실패하면 해당 Task로 돌아간다.

- [ ] **Step 2: upstream 병합 리허설**

```bash
git fetch qn
git merge --no-commit --no-ff qn/main 2>&1 | tail -5; git diff --name-only --diff-filter=U | tee /tmp/merge-conflicts.txt; git merge --abort 2>/dev/null || true
```

Expected: qn/main이 63cecab82 그대로면 "Already up to date". 새 upstream 커밋이 있으면 충돌 파일 목록이 `docs/fork-overlays.md`의 upstream 파일 목록 안에 있어야 한다(spec §8). 벗어난 파일이 있으면 기록하고 이유를 적는다.

- [ ] **Step 3: spec 정정**

`docs/superpowers/specs/2026-10-14-upstream-rebase-design.md`:
- §4.2 첫 문장을 "템플릿은 `web/landing/templates/`, 정적 자산은 `web/landing/public/`(upstream `web/public`은 rsbuild가 dist로 복사해 209 MB를 이중 embed하므로 분리). `main.go`에 embed 2개, `router/web-router.go`에 `registerLandingRoutes`·`landingStatic` 훅 2줄, 본문은 `router/landing-router.go`. 로그인 표시는 upstream 세션 힌트 쿠키만 쓰며 사용자명·역할 배지와 로그아웃은 없앤다."로 바꾼다.
- §4.3 "`getPayMoney`, `LockOrder`, `rejectInvalidTopUpQuota`" → "`rejectInvalidCreditedQuota`, `LockOrder`, `UnlockOrder`"; "`hooks/use-paypal-payment.ts`" → "`hooks/use-payment.ts`의 PayPal 분기"; `TopUp.ProviderOrderId` 컬럼 추가를 명시.
- §2.1 O8 행 끝에 "(단계 A 결과: 인터페이스 언어 제한은 단계 D로, 기본 첫 화면 콘솔은 team에서 되돌려져 제외, 64-bit 옵션 파싱은 no-op)"를 덧붙인다.

- [ ] **Step 4: 기록 완성**

`docs/2026-10-14-rebase-phase-a.md` §3 커밋 표(Task 1–16 커밋 해시·제목), §4 병합 리허설 결과, 그리고 "단계 B로 넘기는 것" 절: 조직 내보내기(`service/org_user_export.go`), PayPal 조직 target, `TopUp.TargetType/TargetId`, 인터페이스 언어 en/ko(단계 D), 운영 DB 사본 리허설(운영자).

- [ ] **Step 5: 커밋**

```bash
git add docs/fork-overlays.md docs/2026-10-14-rebase-phase-a.md docs/superpowers/specs/2026-10-14-upstream-rebase-design.md
git commit -m "docs: record phase A of the upstream rebase and align the spec with the landing and PayPal ports

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```
