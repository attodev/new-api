# upstream 재기반 단계 A 기록

베이스: qn/main 7de10f2c6. 브랜치: rebase/upstream-2026-10. 계획: docs/superpowers/plans/2026-10-14-rebase-phase-a.md

## 1. 기준선 (Task 1)
- Go: `GOWORK=off go build ./...` 통과. `make test` 종료 코드 0, 패키지 100개(루트 + relaykit + tokenkit 합산) — `ok` 54, `no test files` 46, FAIL 0. upstream 기준선 실패 없음.
- 프런트 (`web/`, bun 1.3.14 로컬 / CI는 1.4.0, `bun install --frozen-lockfile` 성공, `bun run build` 성공):
  - typecheck (`tsgo -b`): 0 errors.
  - vitest: 187 파일 / 2256 테스트 전부 pass.
  - lint (`oxlint`): upstream 기준선 실패 — error 164, warning 65, 종료 코드 1. 주요 규칙: `typescript(no-import-type-side-effects)` 57, `react(no-array-index-key)` 26, `unicorn(prefer-spread)` 16, `unicorn(prefer-string-replace-all)` 14, `eslint(no-nested-ternary)` 14, `eslint(curly)` 11 (예: `src/features/auth/lib/validation.ts`, `src/assets/logo.tsx`, `scripts/sync-i18n.mjs`). 우리가 고칠 대상이 아니므로 그대로 둔다.
- 의존성: `GOWORK=off go get github.com/xuri/excelize/v2@v2.10.1`. 아직 import하는 코드가 없어 `go mod tidy`가 줄을 지우므로 tidy는 돌리지 않았고, go.mod에는 `// indirect`로 들어갔다. excelize를 처음 import하는 Task에서 `go mod tidy`로 direct 승격한다. `relaykit`/`tokenkit` replace 줄은 그대로.

## 2. 스키마 리허설 (Task 3)
로컬 실행 안 함 — 이 개발 장비에 컨테이너 런타임이 없다. 실행 명령: `./scripts/schema-rehearsal/run.sh all` (Docker 있는 장비) 또는 운영 사본에 `LEGACY_SKIP=1 SQL_DSN=... ./scripts/schema-rehearsal/run.sh <dialect>`. 통과 기준은 scripts/schema-rehearsal/README.md.
- 이 장비에서 확인한 것: `bash -n run.sh` 통과, Docker 없이 `run.sh mysql`은 `docker: command not found`로 즉시 실패(예상 동작), team 트리(placeholder dist 포함)와 리베이스 트리 모두 `GOWORK=off go build`로 바이너리 빌드 성공 (go1.27.1 darwin/arm64).
- Task 2의 DSN 테스트(`TestUserQuotaColumnsSurviveAutoMigrate`)도 컨테이너가 있는 장비에서 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN`을 주고 돌려야 한다 (기대: PASS x2).
- 운영 사본 리허설: 미실행(운영자 작업).

## 3. 커밋 표
| Task | 커밋 | 내용 |
|---|---|---|

## 4. upstream 병합 리허설 (Task 16)
