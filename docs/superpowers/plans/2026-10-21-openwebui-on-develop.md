# OpenWebUI 연동을 develop 위로 옮기는 계획 (단계 F, overlay O13)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** team에서 갈라진 `OpenWebUI` 브랜치(playground 33커밋 + OpenWebUI 49커밋)의 최종 기능을 `develop` 위의 새 브랜치 `develop-openwebui`에 fork overlay O13으로 옮긴다. 기능: 내장 OIDC 제공자(Open WebUI 로그인), relay JWT 치환(Open WebUI 서비스 키 + 사용자 JWT → 사용자 토큰), 사용자 링크 관리, 플레이그라운드의 "채팅 앱 열기", 관리 화면, 통합 테스트 스택.

**Architecture:** 커밋을 재생하지 않고 `origin/OpenWebUI` 끝 상태를 develop 구조에 맞춰 옮긴다(스펙 §4.10). 새 패키지·파일(`service/oidc`, `service/openwebui`, 컨트롤러, 설정, 모델, 프런트 기능 폴더)은 그대로 복사하고, 기존(upstream) 파일에는 한 줄 훅 `// fork: O13`만 두며 로직은 fork 파일에 둔다. develop과 다른 기반(브라우저 세션 → upstream 로그인 세션, `web/default` → `web/`, `kr` → `ko`, classic 테마 없음, i18n 방식)은 각 Task에 적은 대로 고친다.

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, GORM, Redis(miniredis 테스트), React 19 + TanStack Router + i18next, vitest, Bun.

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §4.10(O13)과 위험 항목("토큰 치환은 relay 경로에만"). 기능 설계는 `docs/superpowers/specs/2026-10-07-openwebui-relay-jwt-design.md`, `docs/superpowers/specs/2026-10-07-openwebui-oidc-provider-design.md`(OpenWebUI 브랜치에서 복사; 본문의 `web/default`·`kr`·세션 서술은 team 기준이다). 원본 코드는 `git show origin/OpenWebUI:<경로>`로 읽는다. 원본 범위: `git diff 5167b251d origin/OpenWebUI` (5167b251d = OpenWebUI 줄기가 team에서 마지막으로 받은 지점).

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-openwebui`, 브랜치 `develop-openwebui`(시작 `022def514` = 로컬 develop). `OpenWebUI`·`playground`·`team`·`develop` 브랜치와 다른 작업 트리는 건드리지 않는다. 푸시하지 않는다.
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal/UnmarshalJsonStr/DecodeJson`만(`json.RawMessage` 타입 참조는 허용). `relaykit/`, `tokenkit/` 수정 금지. upstream `_test.go` 수정 금지(새 테스트는 새 파일).
- 기존 upstream Go·TS 파일은 한 줄 훅만: Go는 줄 끝 `// fork: O13`, TSX는 `{/* fork: O13 */}` 또는 줄 끝 `// fork: O13`. 10줄 넘는 로직은 fork 파일로. 새로 만드는 파일(원본에도 새 파일인 것)은 파일 첫 주석에 `fork: O13 — <한 줄 설명>`을 둔다. 모든 훅은 `docs/fork-overlays.md`의 새 절 `## O13 OpenWebUI` 표(4열: overlay | upstream 파일 | 훅 | fork 전용 파일)에 행으로 남긴다(Task 1이 절을 만들고 이후 Task가 행을 추가).
- 보안 경계(스펙 위험 항목): 서비스 키 → 사용자 토큰 치환은 relay 경로의 `middleware.TokenAuth()`에만 둔다. `TokenAuthReadOnly`, `UserAuth`/`AdminAuth`/대시보드 경로, 접근 토큰(PAT) 경로에는 절대 두지 않는다. `X-OpenWebUI-User-Jwt` 헤더는 상류로 전달되지 않아야 한다. OIDC 서명 키 옵션(`OIDCSigningCurrentKey`, `OIDCSigningPreviousKey`)은 옵션 조회 API에 보이지 않고 옵션 저장 API로 쓸 수 없어야 한다.
- i18n: 백엔드에서 AI 클라이언트(relay)로 가는 오류는 go-i18n 키(`i18n/keys_fork.go` + `i18n/locales/{en,ko}.yaml`, O3과 같은 방식). 관리 화면 API 오류는 `common.ApiErrorT(c, "English sentence")`(develop 방식: 영문 문장이 키이고 프런트가 번역) — 그 영문 문장을 `web/src/i18n/locales/en.json`(값=키)·`ko.json`에 추가한다. **`bun run i18n:sync` 금지**(보호 키를 덮어씀). `cd web && node scripts/ko-locale.mjs check`와 `bun run test -- --run src/i18n/__tests__/ko-coverage.test.ts` 통과.
- 프런트: team의 `web/default/src/...` → develop의 `web/src/...`. 새 TS/TSX 파일은 기존 파일과 같은 AGPL 헤더(예: `web/src/features/usage-logs/components/dialogs/timing-breakdown-section.tsx` 첫 18줄). 게이트: `bun run typecheck`, 손댄 파일 `node_modules/.bin/oxlint -c .oxlintrc.json <files>` 오류 0, `bun run format:check` 출력에 손댄 파일 없음(고칠 때는 `node scripts/format-with-protected-headers.mjs --write`), 관련 vitest 통과. `web/src/routeTree.gen.ts`는 추적 파일이므로 라우트 추가 뒤 `bun run build`(또는 TanStack 라우터 생성)로 다시 만들어 커밋한다.
- DB: 새 테이블은 GORM AutoMigrate로만, SQLite·MySQL·PostgreSQL 모두에서 동작(raw SQL 금지).
- 커밋: 각 Task 끝에 한 번. 트레일러는 출처마다 `Ported-From: OpenWebUI <sha>` 한 줄(각 Task에 적은 sha) + 마지막 줄 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- 제외(옮기지 않음, 근거는 이 계획의 판정): classic 테마용 변경(`common/embed-file-system.go`, `router/web-router.go`의 `spaIndexPage`와 두 테스트) — develop에는 classic이 없다. `model/quota_reserve.go`의 `refreshTTLScript` — 원본에서 Redis 전용 합성 OAuth2 토큰을 위해 넣었고 끝 상태에는 그 토큰이 없다(토큰은 DB 행이 있는 사용자별 토큰), 사용자 잔액 스크립트는 O12가 따로 가진다. `service/billing_session.go` 변경 — 주석·변수명만. `.superpowers/sdd/task-6-report.md`, `web/default/src/i18n/locales/_reports/*`, `docs/superpowers/plans/2026-08-17-*`·`2026-10-07-*`(원본 계획, 합계 8천 줄)과 broker 시절 스펙 5개(`2026-08-1*`) — OpenWebUI 브랜치에 남겨 둔다.

## Review Focus

1. **대시보드 요청에 Open WebUI 서비스 키를 Bearer로 넣은 경우:** 치환이 일어나면 안 되고 일반 거부(대시보드는 접근 토큰/세션만)여야 한다. → Task 2 `TestOpenWebUISubstitutionOnlyOnRelayTokenAuth`(TokenAuthReadOnly 경로 포함).
2. **상류로 가는 요청 헤더:** `X-OpenWebUI-User-Jwt`가 passthrough 대상에서 빠져야 한다. → Task 2 `relay/channel` 테스트.
3. **조직 멤버가 Open WebUI로 쓰는 경우:** 치환된 사용자 토큰 기준으로 과금되므로 조직 지갑에서 빠져야 한다(개인 지갑 아님). → Task 2 `TestOpenWebUIResolvedTokenUsesUserBillingContext`(치환 뒤 `id`·사용자 캐시가 실제 사용자로 바뀌는지) + Task 8 런북의 수동 확인.
4. **OIDC end-session:** `id_token_hint`가 없거나 다른 사용자 것이면 로그아웃하지 않고 확인을 요구해야 한다(클릭 없는 강제 로그아웃 금지). develop의 로그인 세션을 실제로 폐기해야 한다(refresh 쿠키까지). → Task 4 로그아웃 테스트.
5. **서명 키:** 옵션 조회 응답에 서명 키가 없고, 옵션 저장으로 덮어쓸 수 없어야 한다. → Task 3 `TestGetOptionsHidesOIDCSigningKeys`, `TestUpdateOptionRejectsSigningKeyWrites`.

---

### Task 1: 설정·컨텍스트 키·Redis 도우미·링크 모델·마이그레이션 (기반)

**Files:**
- Create: `setting/system_setting/oauth2_client_fork.go`(원본 `setting/system_setting/oauth2_client.go`), `setting/system_setting/openwebui_fork.go`(원본 `openwebui.go`), 각 `_fork_test.go`(원본 테스트)
- Create: `constant/context_key_openwebui_fork.go` — 원본 `constant/context_key.go`에 추가된 네 상수(`ContextKeyOpenWebUISub`, `ContextKeyOpenWebUITask`, `ContextKeyOpenWebUIAttributed`, `ContextKeyOpenWebUIUnsigned`)를 같은 `const` 형태로
- Create: `common/redis_getdel_fork.go` — 원본 `common/redis.go`에 추가된 `IsRedisNotFound`, `RedisGetDel`(develop의 `common.SysLog(common.LogText(...))` 로깅 방식에 맞춤) + `common/redis_getdel_fork_test.go`(원본 `common/redis_test.go`의 해당 케이스)
- Create: `model/openwebui_link.go`, `model/openwebui_token.go`, `model/openwebui_link_test.go`(원본 그대로, develop 모델 API에 맞춤)
- Modify: `model/main.go` — `OpenWebUILink` 마이그레이션. develop의 fork 마이그레이션 함수(`MigrateForkOrganizationTables`, `model/main.go:383` 훅 참고) 옆에 `MigrateForkOpenWebUITables(db *gorm.DB) error`를 fork 파일(`model/openwebui_migrate_fork.go`)에 만들고 같은 자리에 한 줄 훅. `migrateDBFast` 쪽도 develop 구조에 fork 테이블을 넣는 방식이 있으면 그것을 따르고, 없으면 같은 함수 호출 한 줄.
- Create: `i18n/keys_openwebui_fork.go` — 원본 `i18n/keys.go`의 relay 오류 키 5개(`MsgOpenWebUIAuthFailed`, `MsgOpenWebUINotLinked`, `MsgOpenWebUILinkConflict`, `MsgOpenWebUILinkNotFound`, `MsgOpenWebUISubRequired`)와 OIDC 키 2개(`MsgOIDCEmailRequired`, `MsgOIDCInvalidRequest`). `i18n/locales/en.yaml`·`ko.yaml`에 원본 ko.yaml/en.yaml의 해당 7줄. 옵션 검증 메시지 키(`MsgOption*`)는 만들지 않는다(Task 3에서 `ApiErrorT` 영문 문장으로).
- Modify: `docs/fork-overlays.md` — `## O12 …` 절 뒤(또는 파일의 overlay 절 순서상 맞는 자리)에 `## O13 OpenWebUI` 절과 4열 표 머리를 만들고 이 Task의 훅 행을 넣는다.

**출처 sha:** b33275d8a, 2864564a6, 461b2574e, 7c4e9d65b, 3dfb18edd

- [ ] Step 1: 원본 파일을 읽고 develop의 대응 API를 확인한다(`config.GlobalConfig.Register`, `model.Token.Insert`, `common.GenerateKey`, 토큰 상태 상수). 설정 등록 이름은 원본과 같은 `oauth2`, `openwebui`(develop의 `oidc` 키는 OIDC *클라이언트* 로그인 설정이므로 건드리지 않는다).
- [ ] Step 2: 테스트를 먼저 옮기고(RED) 구현을 옮긴다(GREEN). 원본 테스트가 team 전용 헬퍼를 쓰면 같은 패키지의 develop 헬퍼로 바꾼다.
- [ ] Step 3: `GOWORK=off go test ./setting/... ./common/ ./model/ ./i18n/ -count=1`, `gofmt -l` 빈 출력, `go vet` 해당 패키지.
- [ ] Step 4: 커밋 `feat(openwebui): settings, context keys, link model and migration (O13)`.

### Task 2: relay JWT 치환 — `service/openwebui` 패키지와 TokenAuth 훅

**Files:**
- Create: `service/openwebui/{cache,context,jwt,link,provision,resolve}.go`와 각 `_test.go`(원본 그대로; develop API에 맞춤)
- Modify: `middleware/auth.go` — `TokenAuth()`에서 토큰 검증과 IP 허용 목록 검사 **뒤**, `model.GetUserCache(token.UserId)`로 사용자 상태·그룹을 읽기 **전** 자리에 훅 한 줄 `if !openWebUISubstitute(c, key, &token) { return } // fork: O13`(이름은 자유). 로직은 `middleware/auth_openwebui_fork.go`: `openwebui.IsServiceKey(key)`면 `openwebui.Resolve(c, token)`; 거부면 `abortWithOpenAiMessage(c, rejectErr.Status, i18n.T(c, rejectErr.MessageKey))`(develop의 relay 오류 번역 함수 — `TokenAuth` 안의 다른 거부와 같은 함수)로 끝내고 false; 성공이면 token 교체 + `c.Set("id", token.UserId)`. 원본 위치는 `git show origin/OpenWebUI:middleware/auth.go`의 `IsServiceKey` 블록(사용자 캐시 조회 직전)이다. **`TokenAuthReadOnly`와 대시보드 인증 함수에는 넣지 않는다.**
- Create: `middleware/auth_openwebui_fork_test.go`(원본 `middleware/auth_openwebui_test.go`) + `middleware/auth_openwebui_redis_fork_test.go`(원본 `auth_openwebui_redis_test.go`). 추가 테스트 `TestOpenWebUISubstitutionOnlyOnRelayTokenAuth`: 서비스 키를 `TokenAuthReadOnly`로 보낸 요청은 치환되지 않는다(사용자 JWT 헤더가 있어도 `id`가 서비스 토큰 소유자 그대로이거나 거부). 추가 테스트 `TestOpenWebUIResolvedTokenUsesUserBillingContext`: 치환 뒤 컨텍스트의 `id`, 토큰 id, 사용자 그룹이 링크된 사용자 것이다.
- Modify: `relay/channel/api_request.go` — `passthroughSkipHeaderNamesLower` 맵에 `"x-openwebui-user-jwt": {}, // fork: O13` 한 줄. 테스트 `relay/channel/api_request_openwebui_fork_test.go`(원본 `api_request_test.go`에 추가된 케이스).
- Modify: `service/log_info_generate.go` — `GenerateTextOtherInfo`의 원본과 같은 자리에 `appendOpenWebUIInfo(ctx, other) // fork: O13` 한 줄; 함수는 `service/log_info_openwebui_fork.go`. 테스트 `service/log_info_openwebui_fork_test.go`(원본 `service/log_info_openwebui_test.go`).
- Modify: `docs/fork-overlays.md` O13 표에 이 Task의 훅 행.

**출처 sha:** b707ed279, 7128329c2, 40499c8e9, c4f206767, 4858e7fbb, afa263a55, 7ec663b2a, 8b0607d7b, 775c24930

- [ ] Step 1: 원본 패키지와 develop의 사용자·토큰 API를 대조한다(`model.GetUserCache`, 이메일로 사용자 찾기, 토큰 캐시 갱신 함수, `common.GetContextKeyString` 등). 원본이 team에만 있는 함수를 부르면 develop의 대응 함수로 바꾸고 보고서에 표로 남긴다.
- [ ] Step 2: 테스트 먼저(RED) → 구현(GREEN). Redis 테스트는 miniredis.
- [ ] Step 3: `GOWORK=off go test ./service/openwebui/ ./middleware/ ./relay/channel/ ./service/ -count=1`, `-race -run OpenWebUI ./middleware/ ./service/openwebui/`, `gofmt -l`, `go vet`.
- [ ] Step 4: 커밋 `feat(openwebui): substitute the user's token for Open WebUI service-key relay requests (O13)`.

### Task 3: 내장 OIDC 제공자 서비스와 옵션·상태 훅

**Files:**
- Create: `service/oidc/{claims,codes,discovery,idtoken,keys}.go`와 각 테스트(`codes_redis_test.go` 포함). 원본 그대로; develop API에 맞춤.
- Modify: `controller/option.go`:
  - `GetOptions`의 옵션 순회에서 서명 키를 건너뛰는 훅 한 줄(예: `if oidcHiddenOption(k) { continue } // fork: O13`).
  - `UpdateOption` 앞쪽(원본과 같은 자리)에서 서명 키 직접 쓰기 거부 한 줄(`if rejectOIDCSigningKeyWrite(c, option.Key) { return } // fork: O13`).
  - 옵션 저장 검증: 원본 `switch option.Key`에 추가된 `openwebui.*`, `oauth2.*` case 묶음을 fork 함수 `validateOpenWebUIOption(c, &option) (ok bool)`(파일 `controller/option_openwebui_fork.go`)로 옮기고, develop `UpdateOption`의 검증 `switch` 바로 앞이나 안에 한 줄 훅. 오류는 `common.ApiErrorT(c, "<영문 문장>")`. 원본 i18n 키 → 영문 문장(이 문장을 웹 en/ko json에 추가: 아래 번역 참고):
    - option.openwebui_relay_config_missing → "Set the service key and a JWT secret of at least 32 characters before enabling the Open WebUI relay" / ko: 원본 ko.yaml 문장
    - option.openwebui_service_key_invalid → "The Open WebUI service key does not match an enabled token"
    - option.openwebui_secret_too_short → "The Open WebUI JWT secret must be at least 32 characters"
    - option.openwebui_cannot_clear_while_enabled → "Disable the Open WebUI relay before clearing the service key or JWT secret"
    - option.oauth2_config_missing → "Set a client ID, a client secret of at least 16 characters and an absolute redirect URI before enabling external app access"
    - option.oauth2_invalid_url → "Must be an absolute http(s) URL"
    - option.oauth2_secret_too_short → "The client secret must be at least 16 characters"
    - i18n.MsgInvalidParams(원본) → develop에서 같은 상황에 쓰는 문장(`"Invalid parameters"`, `common.ApiErrorT`).
  - ko 번역은 원본 `git show origin/OpenWebUI:i18n/locales/ko.yaml`의 해당 줄을 쓴다(용어집 `docs/translation-glossary.ko.md` 확인).
- Modify: `controller/misc.go` `GetStatus` — 원본이 추가한 네 키(`oauth2_enabled`, `oauth2_open_in_new_window`, `oauth2_replace_playground`, `oauth2_app_url`)를 fork 함수 `openWebUIStatusFields() map[string]any`로 만들고, 상태 맵에 병합하는 한 줄 훅(develop `misc.go:98`의 O11 훅과 같은 방식; 맵 리터럴 안이면 키별 한 줄씩도 허용 — 표에 행으로).
- Create: 테스트 `controller/option_oauth2_fork_test.go`, `option_openwebui_fork_test.go`, `option_signing_key_fork_test.go`(원본 세 파일), 추가 `TestGetOptionsHidesOIDCSigningKeys`, `TestUpdateOptionRejectsSigningKeyWrites`(원본에 같은 의미가 있으면 그것으로 충족).
- Modify: `web/src/i18n/locales/en.json`, `ko.json` — 위 영문 문장 7개 + 번역. `docs/fork-overlays.md` O13 표 행.

**출처 sha:** 3dfb18edd, f2ab4eafa, 6e9f7b067, 0e2e9c1d8, 69360b199, 0c43b4fd1, 5ac420052, 8b0607d7b, c58da62b5, 8e4525a4b

- [ ] Step 1: 원본과 develop `controller/option.go`를 비교해 훅 자리를 정한다(develop은 검증 `switch`가 두 개다: `controller/option.go:199`, `:217`).
- [ ] Step 2: 테스트 먼저 → 구현.
- [ ] Step 3: `GOWORK=off go test ./service/oidc/ ./controller/ -count=1`(controller는 약 35초), `gofmt -l`, `go vet`; 웹은 `node scripts/ko-locale.mjs check`.
- [ ] Step 4: 커밋 `feat(oidc): built-in OIDC provider service, option validation and status fields (O13)`.

### Task 4: OIDC·링크 컨트롤러와 라우트 (develop 로그인 세션에 맞춘 end-session)

**Files:**
- Create: `controller/oauth2_provider.go`(원본), `controller/openwebui_link.go`(원본), 테스트 `controller/oauth2_provider_fork_test.go`, `oauth2_provider_redis_fork_test.go`, `oauth2_logout_fork_test.go`, `openwebui_link_fork_test.go`(원본 네 파일).
- Create: `router/oauth2_router_fork.go`(원본 `router/oauth2-router.go`: `/.well-known/openid-configuration`, `/oauth2/jwks`, `POST /oauth2/token`, `GET /oauth2/userinfo`) + 테스트(원본 `router/oauth2-router_test.go`).
- Modify: `router/main.go` — `SetOAuth2Router(router) // fork: O13` 한 줄(원본과 같은 자리).
- Modify: `router/api-router.go` — 원본이 추가한 두 그룹(`/api/openwebui` AdminAuth: `GET relay-status`, `GET/POST links`, `DELETE links/:id`; `/api/oauth2`: `POST authorize`(UserAuth), `POST logout`, `POST rotate-signing-key`(RootAuth), `GET signing-key-status`(RootAuth))을 fork 함수 `registerForkOpenWebUIRoutes(apiRouter)`(파일 `router/openwebui_routes_fork.go`)로 옮기고 한 줄 훅. develop의 다른 fork 라우트 등록 방식(`registerForkOrganizationRoutes`, `router/organization-router.go`)을 따른다.
- Modify: `middleware/access_token_routes.go` — 새 `/api` 라우트 7개의 접근 토큰 규칙. 관리용 4개(`/api/openwebui/*`, `rotate-signing-key`, `signing-key-status`)와 `POST /api/oauth2/authorize`, `POST /api/oauth2/logout`은 `accessTokenSessionRule`(브라우저 세션 전용). fork 규칙 맵이 있으면 그쪽에, 없으면 맵에 행을 추가하되 각 줄 `// fork: O13`. `router/access_token_scope_test.go` 같은 "모든 라우트가 선언됐는가" 테스트가 통과해야 한다.
- **end-session 다시 쓰기(핵심):** 원본 `OAuth2Logout`은 gin 쿠키 세션(`sessions.Default(c)`, `session.Get("id")`, `session.Clear()`)을 썼다. develop은 upstream 로그인 세션(메모리 Bearer 액세스 토큰 + refresh 쿠키 + `user_sessions` 행)이다. 다시 쓸 규칙:
  - 현재 세션 사용자: `Authorization: Bearer`(SPA가 보냄)를 `service.ParseAccessToken`으로 읽어 `identity.UserID`/`SessionID`를 얻는다. 없으면 refresh 쿠키(`service.RefreshCookieName`)의 SID로 판단한다. 둘 다 없으면 "세션 사용자 없음".
  - `id_token_hint`가 우리 키로 검증되고 그 `sub`가 세션 사용자면, 또는 사용자가 확인(`confirm: true`)했으면 로그아웃한다. 아니면 원본처럼 `needs_confirmation`.
  - 로그아웃 동작은 develop `controller/auth_session.go` `AuthLogout`과 같은 효과: `model.RevokeUserSession(identity.UserID, identity.SessionID, "oidc_end_session")`, refresh 쿠키가 같은 세션이면 `service.RevokeByRefreshToken` + `service.ClearRefreshCookie(c)`. 공통 부분은 `AuthLogout`을 고치지 말고 fork 쪽에서 같은 서비스 함수를 호출한다.
  - `/api/oauth2/logout`은 develop의 `SessionCookieOriginGuard`를 거쳐야 하는지 확인한다(refresh 쿠키를 지우는 POST이므로 `auth/logout`과 같은 가드가 맞다).
  - `OAuth2Authorize`는 UserAuth 뒤에 있으므로 `c.GetInt("id")`가 그대로 동작한다(develop UserAuth가 설정).
- Modify: `docs/fork-overlays.md` O13 표 행.

**출처 sha:** 92d61cef0, bb7058942, 968b1f29d, 731cacd76, 91741014b, 372fefb20, 518f587ee, 8fbe565fa, b03764a81, f1fe32c1e, fad31309f

- [ ] Step 1: 원본 컨트롤러가 쓰는 team 전용 함수(세션, 사용자 조회, 감사 로그)를 develop 대응으로 정리한다.
- [ ] Step 2: 로그아웃 테스트를 develop 세션 모델로 다시 쓴다: (a) 힌트=세션 사용자 → 세션 행 revoked, refresh 쿠키 지움; (b) 힌트 없음·다른 사용자·검증 실패 → `needs_confirmation`, 세션 유지; (c) `confirm: true` → 로그아웃; (d) 제공자 꺼짐 → 키를 건드리지 않음; (e) post_logout_redirect_uri 정확 일치만 허용.
- [ ] Step 3: `GOWORK=off go test ./controller/ ./router/ ./middleware/ -count=1`, `-race -run 'OAuth2|OpenWebUI' ./controller/`, `gofmt -l`, `go vet`.
- [ ] Step 4: 커밋 `feat(oidc): OIDC endpoints, end-session on develop login sessions, Open WebUI link admin API (O13)`.

### Task 5: 조직 멤버 관리에서 root 허용 (playground의 일반 수정)

**Files:**
- Modify: `controller/organization.go` — `UpdateOrganizationUser`, `AssignOrganizationUser`, `RemoveOrganizationUserMembership`이 `actor.OrganizationId`/관리자 역할 검사 대신 `resolveOrganizationAdminTarget(c)`(root는 `?organization_id=`, 그 외는 자기 조직의 owner/admin)를 쓰게 한다. owner 지정은 owner 또는 root만. 원본 diff: `git diff 5167b251d origin/OpenWebUI -- controller/organization.go`. 이 파일은 O3 fork 파일(`fork: O3` 헤더)이므로 직접 고친다.
- Create: `controller/organization_membership_fork_test.go`(원본 `controller/organization_membership_test.go`, 122줄).
- 함께 확인: playground의 가격 화면 수정 두 건(`ae16512dc` 가격 폼이 무관한 재렌더마다 초기화, `59a9aa195` 토큰당/요청당 가격 저장 시 가격 필수; 원본 `web/default/src/features/system-settings/models/{ratio-settings-card,model-pricing-sheet}.tsx`). develop의 같은 파일에 같은 결함이 있으면(원본 diff의 수정 전 패턴이 develop에도 있으면) 최소 훅으로 옮기고 vitest 한 개씩, 없으면 보고서에 "develop에 해당 없음"과 근거를 적는다.

**출처 sha:** c71c45b48, ae16512dc, 59a9aa195

- [ ] Step 1: 테스트 먼저 → 구현. root가 다른 조직의 멤버를 추가·수정·제거할 수 있고, 조직 admin은 자기 조직만, owner 지정은 owner/root만.
- [ ] Step 2: `GOWORK=off go test ./controller/ -run 'Organization' -count=1`; 웹 수정이 있으면 해당 vitest·typecheck·lint.
- [ ] Step 3: 커밋 `fix(organization): let root manage members of any organization` (+ 가격 수정이 있으면 같은 커밋 또는 별도 커밋).

### Task 6: 프런트 — 로그인 리다이렉트, OIDC 페이지, 플레이그라운드 "채팅 앱 열기"

**Files (team `web/default/src` → develop `web/src`):**
- Create: `features/auth/lib/redirect-target.ts`(+ `redirect-target.test.ts`), `features/oidc/api.ts`, `routes/oauth2/authorize.tsx`, `routes/oauth2/logout.tsx`.
- Modify(최소 훅): `features/auth/hooks/use-auth-redirect.ts`(원본 diff 7줄 — develop에는 O9 훅이 이미 있으므로 그 줄을 건드리지 않고 옆에), `features/auth/types.ts`, `routes/(auth)/sign-in.tsx`(redirect 파라미터), `lib/nav-modules.ts`, `features/playground/index.tsx`·`routes/_authenticated/playground/index.tsx`(상태 플래그 `oauth2_enabled`·`oauth2_replace_playground`·`oauth2_open_in_new_window`·`oauth2_app_url`에 따른 리다이렉트/버튼; 원본 diff 106줄은 fork 컴포넌트 `features/playground/components/open-chat-app-fork.tsx` 등으로 빼고 upstream 파일에는 훅만).
- `routeTree.gen.ts` 재생성.
- OIDC 페이지는 develop 로그인 세션을 쓴다: `/oauth2/authorize`는 로그인 안 됐으면 `/sign-in?redirect=<현재 URL>`로 보내고, 로그인 뒤 `POST /api/oauth2/authorize`(Bearer)로 코드를 받아 리다이렉트. `/oauth2/logout`은 `POST /api/oauth2/logout`에 `id_token_hint`·`state`·`post_logout_redirect_uri`를 보내고 `needs_confirmation`이면 확인 버튼. 같은 출처 리다이렉트 판정은 `redirect-target.ts`.
- 백엔드 SPA 제공 확인: develop `router/web-router.go` NoRoute에서 `/oauth2/authorize`·`/oauth2/logout`이 랜딩 훅(`landingPages`, O1)에 가로채이지 않고 SPA index로 가는지 확인하고, 가로채이면 랜딩 쪽 fork 함수에서 두 경로를 제외(한 줄). Go 테스트 하나.
- i18n: 원본 en.json/kr.json에 추가된 키(81줄 중 이 Task 화면 몫)를 develop en.json(값=키)/ko.json에 텍스트로 추가.

**출처 sha:** f6a4f3a6c, 766f7615c, 9c47b31b9, 6c30cc039, ab4bbd027, d6833320a, f9fd339b4, 954286a65, 8fbe565fa

- [ ] Step 1: develop 라우트 구조(`web/src/routes/`)와 로그인 흐름(`use-auth-redirect.ts`, 세션 복원)을 읽고 원본과 대조.
- [ ] Step 2: vitest 먼저(redirect-target, authorize 페이지의 미로그인 리다이렉트, logout 확인 흐름, 플레이그라운드 플래그별 동작) → 구현.
- [ ] Step 3: 웹 게이트 전부 + `GOWORK=off go test ./router/ -count=1`.
- [ ] Step 4: 커밋 `feat(web): OIDC authorize/logout pages and open-in-chat-app entry (O13)`.

### Task 7: 프런트 — 관리 화면(사이트 설정의 Open WebUI 두 섹션, 사용자 링크)

**Files:**
- Create: `features/system-settings/site/openwebui-integration-section.tsx`(OIDC 클라이언트·앱 URL·로그아웃 URL·새 창/플레이그라운드 대체·서명 키 회전), `features/system-settings/site/openwebui-relay-section.tsx`(relay 켜기·서비스 키·JWT 비밀·이전 비밀·issuer·leeway·미서명 허용·상태), `features/users/components/user-openwebui-link-section.tsx`(사용자 편집 서랍의 링크 보기·만들기·지우기).
- Modify(훅): `features/system-settings/site/section-registry.tsx`, `site/index.tsx`, `system-settings/api.ts`, `system-settings/types.ts`, `features/users/api.ts`, `features/users/types.ts`, `features/users/components/users-mutate-drawer.tsx`. 15줄 넘는 원본 추가분은 fork 파일로.
- i18n: 이 화면 몫의 키를 en/ko에.
- vitest: 두 섹션의 저장 검증(서비스 키·비밀 길이), 링크 섹션의 목록·생성·삭제 호출.

**출처 sha:** 9a7323dfa, e8605506d, b89ca0f61, acb9c8812, 2cfa8dbbb, 74c440f13, f9fd339b4

- [ ] Step 1: develop `site/section-registry.tsx`의 섹션 등록 방식 확인 후 이식.
- [ ] Step 2: 테스트 → 구현 → 웹 게이트 전부.
- [ ] Step 3: 커밋 `feat(web): Open WebUI relay and OIDC settings, user link management (O13)`.

### Task 8: 통합 테스트 스택·문서·전체 게이트

**Files:**
- Create: `deploy/openwebui-test/{docker-compose.yml,.env.example,bootstrap.sh,mock_upstream.py,README.md}`(원본). 이미지 빌드는 저장소 루트 `Dockerfile`(develop: `web/` 단일 프런트)로, README의 `web/default`·classic·`kr` 서술과 비밀번호 규칙을 develop 기준으로 고친다. `bootstrap.sh`가 쓰는 관리 API가 develop에서 세션 전용·step-up 증명을 요구하면(예: 사용자 관리, 옵션) 그에 맞춘다(접근 토큰 + `X-Security-Proof`, 또는 DB 시드).
- Create: `docs/2026-10-21-openwebui-on-develop.md` — 기록: 옮긴 것/뺀 것(제외 목록과 근거), develop에서 다시 쓴 부분(end-session, i18n, 라우트 선언), 원본 sha 대응표, 남은 위험, 운영 설정(옵션 키 목록: `openwebui.*`, `oauth2.*`, 서명 키 회전 절차).
- Modify: `docs/manuals/operator-verification-checklist.ko.md` — Open WebUI 연동 확인 절(스택 기동, OIDC 로그인, relay JWT 과금이 실제 사용자/조직 지갑으로 가는지, 서명 키 회전, 로그아웃 왕복). `docs/fork-overlays.md` O13 표 최종 점검(모든 `fork: O13` 표식이 행에 있는지: `grep -rn "fork: O13" --include='*.go' --include='*.ts' --include='*.tsx' .` 결과와 대조).
- 전체 게이트: `GOWORK=off go test ./... -count=1`(약 수 분), `gofmt -l $(git diff --name-only 022def514 -- '*.go')` 빈 출력, `cd web && bun run typecheck && bun run test -- --run`, 손댄 웹 파일 lint/format, `node scripts/ko-locale.mjs check`.

**출처 sha:** 56019efaf, a89ee30a3, a436c7b0b, 0575dce56, 1e6183cb0, 65652850e, 070ffb438, 026125a5a

- [ ] Step 1: 스택·문서 이식.
- [ ] Step 2: 전체 게이트 실행, 실패가 이 계획 밖의 기존 실패인지 구분해 보고.
- [ ] Step 3: 커밋 `docs(openwebui): integration test stack, record and operator checks (O13)`.
