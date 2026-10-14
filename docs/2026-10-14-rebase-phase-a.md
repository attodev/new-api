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
`git log --format='%h %s' 7de10f2c6..HEAD` 기준(오래된 순). Task 번호는 제목으로 추정했다.

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | b991d352e | chore(rebase): start rebase/upstream-2026-10 on qn/main with excelize and baseline record |
| 2 | 5b16acb25 | fix(model): keep users quota columns bigint under upstream AutoMigrate |
| 3 | 5f00d997e | chore(rebase): add the schema startup rehearsal script and record the local run |
| 3 | ff6fa9c62 | fix(rehearsal): assert the schema checks, support production copies, clean up servers |
| 4 | aa501593b | feat(tools): port the unode diagnostics workbench |
| 5 | f9466fbe6 | feat(contact): port the landing inquiry mail and admin SMTP test endpoints |
| 5 | 2f590b177 | refactor(contact): keep the access-token rule in a fork file and validate the test-mail recipient |
| 6 | 99ebf4337 | feat(web): add the contact recipient and SMTP test mail controls to email settings |
| 7 | 486938fe1 | feat(users): port Excel export/import onto the upstream wallet API |
| 7 | ad6baa2bd | fix(users): never import privileged accounts and validate imported rows |
| 8 | dfd2db8f8 | feat(web): add user Excel export and import to the users page |
| 8 | 29000a468 | fix(web): surface user import/export failures |
| 9 | 327940db2 | feat(paypal): port PayPal settings, order column and settlement onto upstream top-ups |
| 10 | 849703c7d | feat(paypal): port the PayPal order, capture and webhook handlers |
| 11 | fb194eca1 | feat(web): add PayPal to the wallet top-up flow |
| 12 | 44916e5c0 | feat(web): add the PayPal tab to payment settings |
| 13 | b8f3d9f2f | feat(landing): serve the landing and guide pages from web/landing on the upstream router |
| 13 | 6706f1bdf | fix(landing): serve landing pages from the NoRoute chain behind the plugin dispatcher |
| 14 | 3d0bcfd96 | feat(settings): enforce the email domain whitelist on OIDC login and hide model routing from user logs |
| 14 | 0e436151d | fix(oauth): check the OIDC email domain before creating the user |
| 15 | 731ef34c7 | feat(web): add B/T number tiers and hide the pricing model detail views |
| 16 | (이 커밋) | docs: record phase A of the upstream rebase and align the spec with the landing and PayPal ports |

## 4. upstream 병합 리허설 (Task 16)
`git fetch qn` 후 `git merge --no-commit --no-ff qn/main`.
- `qn/main` = 7de10f2c6(베이스와 동일). 결과: `Already up to date.` 충돌 파일 없음, 병합이 시작되지 않아 abort 불필요, `git status` clean.
- upstream에 새 커밋이 들어오면 이 리허설을 다시 하고, 충돌 파일이 `docs/fork-overlays.md`의 upstream 파일 목록 안에 있는지 확인한다(spec §8).

## 5. 최종 검증 (Task 16, 2026-10-14)
브랜치 HEAD(문서 커밋 직전)에서 실행.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `make test`: 종료 코드 0, `ok` 54 패키지, FAIL 0 (Task 1 기준선과 같은 패키지 수에 fork 테스트가 추가됨).
- `bun run typecheck`: 0 errors. `bun run test`: 189 파일 / 2262 테스트 pass (기준선 187 / 2256에서 +2 파일, +6 테스트). `bun run build`: 성공.
- `node --test test/*.test.mjs` (tools/unode-diagnostics): 41 pass, 0 fail.
- upstream 기준선 실패(우리가 건드리지 않은 파일, 종료 코드 1로 동일): `bun run lint` error 164 / warning 65 (Task 1 기준선과 숫자 동일), `bun run copyright:check` 23개 파일, `bun run format:check` 25개 파일, `bun run knip`(미사용 파일 70, 미사용 export 372, 미사용 타입 export 152 등). `copyright:check`·`format:check`가 지목한 파일 중 이 브랜치에서 수정한 파일은 없다. 게이트는 "수정한 파일에 새 문제 없음"으로 판단했다.
- 스키마 리허설과 `TestUserQuotaColumnsSurviveAutoMigrate`의 MySQL/PostgreSQL 실행은 이 장비에 컨테이너가 없어 미실행(§2).

## 6. 실행 중 내린 결정
- 베이스는 7de10f2c6이다. 설계 작성 뒤 `qn/main`이 3커밋 움직여서 spec §1·§4.1은 갱신했고, 계획 사본은 63cecab82 그대로 두었다.
- 작업은 메인 체크아웃이 아니라 git worktree(`new-api-rebase`)에서 했다.
- 컨테이너 런타임이 없어 스키마 리허설(Task 3)을 로컬에서 돌리지 못했다. `scripts/schema-rehearsal/run.sh`는 assert, CLI/docker 쿼리 백엔드 선택, trap·PID 정리를 보강했고 Docker 장비나 운영 DB 사본에서 실행해야 한다. Task 2의 DSN 테스트도 마찬가지다.
- upstream의 scoped access token 라우트 레지스트리: fork 라우트는 `middleware/fork_access_token_routes.go`에 선언(init 병합)하고, 인증 없는 fork 라우트는 `router/access_token_scope_test.go`의 exempt 목록에 둔다.
- 문의 백엔드는 랜딩 폼의 실제 필드(`notes`, `features`, `scale_type` 등)를 받고, `landing.js`는 `success`를 확인한다.
- Excel 가져오기는 어떤 호출자에게도 admin/root 계정을 만들지 않고, 행을 `common.Validate.Struct`로 검증한다. 가져오기·내보내기 오류는 UI에 표시한다.
- 랜딩 페이지는 명시 라우트가 아니라 NoRoute 체인(`landingPages`는 웹 rate limiter 뒤, `landingStatic`은 앞)으로 서빙해 task 플러그인의 우선권을 유지한다. 로그인 표시는 세션 힌트 쿠키만 쓴다. `web/.oxlintrc.json`·`web/.oxfmtrc.json`은 `landing/**`를 무시한다.
- OIDC 이메일 도메인 검사는 `findOrCreateOAuthUser` 앞에서 돌아 거부된 사용자의 행이 생기지 않는다.
- O8 범위: 인터페이스 언어 제한은 단계 D로 미룸, "기본 첫 화면 콘솔"은 team에서 되돌려져 제외, 64-bit 옵션 파싱은 upstream에서 no-op.
- 기준선 실패(`lint`, `format:check`, `copyright:check`, `knip`)는 upstream 파일 문제라 고치지 않았다.
- PayPal 충전은 fork 동작을 유지한다. 주문 금액(`Money`)이 `amount × PayPalUnitPrice × 충전 그룹 배율 × 금액 할인`이고 적립은 `Money × QuotaPerUnit`이라서, 단가와 할인이 결제 금액과 적립 잔액을 함께 바꾼다. upstream Stripe는 `Money = amount × 그룹 배율`이라 단가·할인이 결제 금액에만 영향을 준다. `model/topup_paypal.go` 주석과 결제 설정 안내 문구를 이 동작에 맞췄고, Stripe 방식으로 맞출지는 단계 E(O11)에서 정한다.
- Excel 가져오기는 요청마다 `user.import` 감사 로그 한 건(생성·건너뜀·오류 수, 생성 사용자명 최대 50개와 전체 수, 지급 잔액 합계)을 남기고 AdminAuth 기본 감사 항목은 생략한다. `quota` 칸이 있으면 `0`도 그대로 적용하고, 잔액 설정에 실패한 계정은 생성 수에 포함하되 `quota_errors`로 따로 알린다.

## 7. 단계 B로 넘기는 것
- 조직 내보내기(`service/org_user_export.go`): 사용자 내보내기와 같은 excelize 경로를 쓰며 O3와 함께 이식한다.
- PayPal 조직 target: 조직 지갑의 PayPal 충전(O2의 조직 분기).
- `TopUp.TargetType`/`TargetId`: O3에서 `TopUp`에 필드를 추가하고 PayPal 정산이 이를 읽게 한다.
- 인터페이스 언어 en/ko 제한: 단계 D(O9 한글)에서 `ko` locale과 함께.
- 운영 DB 사본 리허설(운영자)과 `TEST_MYSQL_DSN`/`TEST_POSTGRES_DSN` 테스트 실행: 컨테이너 장비 또는 운영 사본에서.
- 프런트 `getPaymentIcon`에 PayPal case 추가(현재 `PAYMENT_TYPES.PAYPAL` 색상만 있음).
- `knip`을 게이트로 삼게 되면 모델 상세 컴포넌트(`ModelDetails`, `ModelDetailsDrawer`, `model-details-*.tsx`)가 미사용으로 잡히므로 `knip.config.ts`에 ignore를 추가한다.
