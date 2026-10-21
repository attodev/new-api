# Open WebUI 연동을 develop 위로 옮긴 기록 (fork overlay O13)

브랜치: develop-openwebui. 베이스: 022def514(로컬 develop). 계획: docs/superpowers/plans/2026-10-21-openwebui-on-develop.md(커밋 5fd988ea0). spec: docs/superpowers/specs/2026-10-14-upstream-rebase-design.md §4.10(O13). 기능 설계: docs/superpowers/specs/2026-10-07-openwebui-{relay-jwt,oidc-provider}-design.md(원본 브랜치에서 복사, 본문의 `web/default`·`kr`·쿠키 세션 서술은 team 기준).

원본은 legacy `OpenWebUI` 브랜치(팁 3d77fe508, 범위 `5167b251d..origin/OpenWebUI`)다. 커밋을 재생하지 않고 끝 상태를 develop 구조에 맞춰 옮겼다. 새 파일은 그대로 복사하고 upstream 파일에는 `// fork: O13` 훅만 두었다. 훅 위치는 모두 `docs/fork-overlays.md`의 `## O13 OpenWebUI` 표에 있다. ledger는 `.superpowers/sdd/2026-10-21-openwebui-on-develop/progress.md`.

## 1. 원본 sha ↔ develop 커밋

| Task | develop 커밋 | 내용 | 원본 sha (`Ported-From`) |
|---|---|---|---|
| 계획 | 5fd988ea0 | 계획, relay-JWT·OIDC 설계 문서 복사 | 1e6183cb0, 65652850e (설계 문서는 원본 팁과 같은 내용이라 070ffb438, 026125a5a의 수정도 이미 들어 있다) |
| 1 | 3ad87ae84 | 설정(`oauth2`, `openwebui`), 컨텍스트 키, Redis `GETDEL`, 링크 모델·마이그레이션, relay i18n 키 | b33275d8a, 2864564a6, 461b2574e, 7c4e9d65b, 3dfb18edd |
| 2 | 72a696f8f, 6e8eb8e87 | `service/openwebui` + `TokenAuth` 훅(서비스 키 → 사용자 토큰), JWT 헤더 상류 차단, 사용 로그 `owui_*` | b707ed279, 7128329c2, 40499c8e9, c4f206767, 4858e7fbb, afa263a55, 7ec663b2a, 8b0607d7b, 775c24930 |
| 3 | a47ae4adf, f2bdc667d, e7a80cfbc | `service/oidc`(서명 키·코드·클레임·id_token·discovery), 옵션 숨김·쓰기 거부·검증, `/api/status` 필드 | 3dfb18edd, f2ab4eafa, 6e9f7b067, 0e2e9c1d8, 69360b199, 0c43b4fd1, 5ac420052, 8b0607d7b, c58da62b5, 8e4525a4b |
| 4 | 2ac47990e, 7690bf032 | OIDC 엔드포인트, develop 로그인 세션 위의 end-session, Open WebUI 링크 관리 API | 92d61cef0, bb7058942, 968b1f29d, 731cacd76, 91741014b, 372fefb20, 518f587ee, 8fbe565fa, b03764a81, f1fe32c1e, fad31309f |
| 5 | aaa5fc430, ca513e0f4 | root의 조직 멤버 관리, 가격 폼 재렌더 초기화 방지 | c71c45b48, ae16512dc (59a9aa195는 옮기지 않음, R3) |
| 6 | fde5a14f6, 81ff450dd | OIDC authorize·logout 페이지, 로그인 뒤 복귀, 플레이그라운드 "채팅 앱에서 열기"·대체 | f6a4f3a6c, 766f7615c, 9c47b31b9, 6c30cc039, ab4bbd027, d6833320a, f9fd339b4, 954286a65, 8fbe565fa |
| 7 | ff3f296ce | 사이트 설정의 OpenWebUI 연동·릴레이 섹션, 사용자 서랍의 링크 관리 | 9a7323dfa, e8605506d, b89ca0f61, acb9c8812, 2cfa8dbbb, 74c440f13, f9fd339b4 |
| 8 | (이 문서 커밋) | 통합 테스트 스택 `deploy/openwebui-test/`, 이 기록, 운영 확인 절 | 56019efaf, a89ee30a3, a436c7b0b, 0575dce56, 1e6183cb0, 65652850e, 070ffb438, 026125a5a |

## 2. 옮기지 않은 것

| 대상 | 근거 |
|---|---|
| classic 테마용 변경(`common/embed-file-system.go`, `router/web-router.go`의 `spaIndexPage`와 테스트 2개) | develop에는 classic 테마가 없다. `/oauth2/authorize`·`/oauth2/logout`은 NoRoute → SPA로 간다(`TestSetOAuth2Router_LeavesSPAPagesToNoRoute`) |
| `model/quota_reserve.go`의 `refreshTTLScript` | 원본에서 Redis 전용 합성 OAuth2 토큰 때문에 넣었고 끝 상태에는 그 토큰이 없다(토큰은 DB 행이 있는 사용자별 `openwebui`·`openwebui-task`). 사용자 잔액 TTL은 O12가 따로 가진다 |
| `service/billing_session.go` 변경 | 주석·변수명만 |
| 원본 계획(`docs/superpowers/plans/2026-08-17-*`·`2026-10-07-*`, 합계 8천 줄), broker 시절 스펙 5개(`2026-08-1*`), `.superpowers/sdd/task-6-report.md`, `web/default/src/i18n/locales/_reports/*` | 작업 기록이다. OpenWebUI 브랜치에 남겨 둔다 |
| 59a9aa195(토큰당·요청당 가격 저장 시 가격 필수) — R3 | upstream 테스트 `editor-currency.test.tsx:957-969`가 빈 요청당 가격 저장을 기대한다. upstream 의도라 따른다. 대가: 관리자가 가격 없이 요청당 모델을 저장할 수 있다(upstream과 같음) |
| 조직 owner 지정의 "owner 또는 root" 예외 — R4 | develop O3은 owner 역할을 멤버십으로 주지 않고(조직 생성 때만) owner 멤버는 owner 본인만 다룬다. 더 엄격한 develop 규칙을 유지한다. 대가: owner를 바꾸려면 root가 조직을 다시 만든다 |
| OIDC 오류용 go-i18n 키 `MsgOIDCEmailRequired`·`MsgOIDCInvalidRequest` — R1 | authorize·logout 오류는 AI 클라이언트가 아니라 같은 출처 SPA로 가므로 develop 방식(`ApiErrorT` 영문 문장 + web en/ko json)으로 바꿨다. relay 오류 키는 `MsgOpenWebUIAuthFailed`·`NotLinked` 2개만 남겼다(링크 API용 3개는 7690bf032에서 지움) |

R2: 계획은 별도 사용자 검토 없이 실행했다(사용자가 새 브랜치에서 작업을 시작하라고 함, 근거는 승인된 spec §4.10).

## 3. develop에 맞춰 다시 쓴 부분

- **end-session(로그아웃)**: 원본의 gin 쿠키 세션(`sessions.Default`) 대신 develop 로그인 세션을 쓴다. 세션 사용자는 Bearer 액세스 토큰(`service.ParseAccessToken`), 없으면 refresh 쿠키의 SID다. 종료는 `model.RevokeUserSession(..., "oidc_end_session")`과, 같은 세션의 refresh 쿠키면 `RevokeByRefreshToken`·`ClearRefreshCookie`다. 판정은 원본과 같다: 서명이 맞는 `id_token_hint`의 `sub`가 세션 사용자일 때나 `confirmed: true`일 때만 로그아웃하고, 그 밖은 `needs_confirmation`(아무것도 바꾸지 않음). refresh 쿠키가 `Path=/api/user/auth`라 브라우저가 이 엔드포인트에 보내지 않으므로, 프런트 logout 페이지가 세션을 복원해 Bearer로 부르고 끝나면 `/api/user/auth/logout`도 부른다.
- **오류 문구(i18n)**: 관리 화면·OIDC 페이지 오류는 `common.ApiErrorT(c, "English sentence")` + `web/src/i18n/locales/{en,ko}.json`. relay 오류만 go-i18n 키. 원본 `kr.json`·ko.yaml 번역은 용어집(릴레이, 시크릿, 플레이그라운드, 리디렉션)과 격식체로 맞췄다. `bun run i18n:sync`는 쓰지 않았다.
- **라우트 선언**: 새 `/api` 라우트 8개(`/api/openwebui/*` 4개, `/api/oauth2/*` 4개)를 접근 토큰 레지스트리(`middleware/fork_access_token_routes.go`)에 `accessTokenSessionRule`로 선언했다. 개인 액세스 토큰으로 링크·코드 발급·세션 종료·키 교체를 할 수 없다.
- **링크·해제 step-up**: 링크 생성은 `admin.user.update` 증명(`{user_id}`), 해제는 `admin.user.binding.clear` 증명(`{user_id, binding_type:"openwebui"}`)을 `X-Security-Proof`로 요구한다(develop의 관리자 비밀번호 재설정·바인딩 해제와 같은 증명). 성공하면 감사 로그 `openwebui.link.create`/`delete`. 화면은 `useUsers().requestVerification`으로 증명을 받는다.
- **서명 키 보호**: 옵션 조회에서 숨기고(develop의 `...Key` 접미사 필터와 별개의 이중 방어), 옵션 저장은 키 이름의 대소문자·앞뒤 공백과 무관하게 거부한다(MySQL `*_ci` 정렬에서 `oidcsigningcurrentkey`로 덮어쓰는 우회를 막음).
- **시크릿 길이**: `openwebui.jwt_secret`·`jwt_previous_secret`은 앞뒤 공백을 뺀 길이로 32자를 센다. 공백만 있는 값은 "비우기"가 아니라 너무 짧은 값으로 거부한다.
- **OIDC 엄격화**: `oauth2.redirect_uri`가 비면 provider를 미설정으로 본다. PKCE는 없거나 `S256`만 받는다(원본은 `plain`을 발급 시점에 거르지 않았다).
- **프런트**: `web/default/src` → `web/src`, `kr` → `ko`. 로그인 뒤 문서 로드 복귀를 같은 출처 `/oauth2/...` 대상으로 좁혔다(develop은 `navigate({ href })`가 쿼리를 보존하므로). authorize가 401이면 토큰 refresh 뒤 한 번 재시도한다. logout 확인 제목은 사이트 이름을 쓴다.
- **조직 멤버 관리**: root는 `?organization_id=`로 다른 조직의 멤버를 추가·수정·제거한다(`resolveOrganizationAdminTarget`).
- **통합 테스트 스택**: 이미지는 저장소 루트 `Dockerfile`(단일 `web/`, Bun 1.4.0, `GOWORK=off`)로 compose가 빌드한다. `bootstrap.sh`는 로그인 응답의 `access_token`을 Bearer로 쓰고(쿠키 세션 아님), 끝나면 그 세션을 닫는다. 일반 사용자 생성은 증명이 필요 없고(`role: 1`), 선택 기능 `OWUI_MANUAL_LINKS`는 `POST /api/verify`(password, `admin.user.update`)로 받은 증명으로 링크한다. 이메일은 관리 API가 없어 psql로 넣고(원본과 같음), mock 잔액은 O12 Redis 상주 때문에 psql 대신 `POST /api/user/manage`(`add_quota`, `override`)로 넣는다. 비밀번호 규칙을 8~128자로, 루트 이름을 최대 12자로 고쳤다.

## 4. 원본과 같게 둔 것 (검토에서 받아들임)

- 관리자가 수동으로 관리자 계정에 링크하면, 그 관리자의 JWT를 실은 서비스 키 요청이 `sk-<key>-<channelId>`로 채널을 고정할 수 있다. 자동 연결은 관리자·root를 연결하지 않고, 키 접미사는 Open WebUI 운영자가 정한다.
- `owui_sub`는 사용 로그 `other`의 공개 부분에 있다(그 사용자 자신의 Open WebUI id). 미서명 요청은 `owui_unsigned`만 남는다.
- `/v1/dashboard/billing`은 `TokenAuth`를 지나므로 서비스 키 + JWT로 부르면 치환된 사용자의 청구 정보가 나온다.
- 관리자가 채널의 헤더 재정의(header override)를 명시적으로 설정하면 `X-OpenWebUI-User-Jwt`가 상류로 갈 수 있다(기본 passthrough 목록에서는 빠짐).
- Responses WebSocket은 프레임마다 인증을 다시 거치므로 JWT `exp`도 다시 검사한다. JWT 수명(Open WebUI 기본 300초)이 지난 뒤의 프레임은 거부된다.
- TanStack Router가 로드 때 검색 문자열을 정규화하므로 숫자처럼 보이는 `state`·`nonce`(16자리 넘는 숫자, `1e5` 등)는 바뀔 수 있다. Open WebUI가 보내는 값(영숫자, base64url)은 영향이 없다.
- 플레이그라운드 대체 + 새 창 모드에서 주소를 직접 열어 팝업이 막히면 대시보드로 간다.

## 5. 운영 설정

옵션 키(시스템 설정 → 사이트의 "OpenWebUI 연동"·"OpenWebUI 릴레이" 섹션, 또는 root의 `PUT /api/option/`):

- relay: `openwebui.relay_enabled`, `openwebui.service_api_key`(활성 토큰), `openwebui.jwt_secret`(32자 이상), `openwebui.jwt_previous_secret`(교체 중), `openwebui.jwt_issuer`(기본 `open-webui`), `openwebui.jwt_leeway_seconds`, `openwebui.allow_unsigned_requests`.
- 내장 OIDC provider: `oauth2.enabled`, `oauth2.client_id`, `oauth2.client_secret`(16자 이상), `oauth2.redirect_uri`, `oauth2.app_url`, `oauth2.post_logout_redirect_uri`, `oauth2.open_in_new_window`, `oauth2.replace_playground`. issuer는 `ServerAddress`.
- relay를 켜려면 서비스 키와 시크릿이 먼저, OIDC를 켜려면 client id·시크릿·절대 redirect URI와 서버 주소(issuer)가 먼저 있어야 한다. 켜진 동안 서비스 키·JWT 시크릿은 비울 수 없다.
- 서명 키(`OIDCSigningCurrentKey`·`OIDCSigningPreviousKey`)는 첫 사용 때 만들어지고 옵션 API로 보이지도 쓰이지도 않는다. 교체는 화면의 "서명 키 교체"(root) 또는 root 로그인 세션의 `POST /api/oauth2/rotate-signing-key`, 상태는 `GET /api/oauth2/signing-key-status`. 이전 키는 24시간 JWKS에 남는다. 교체 직후 1~2분은 노드 간 옵션 동기화(60초)와 Open WebUI JWKS 캐시 때문에 로그인이 잠깐 실패할 수 있다.
- JWT 시크릿 교체: 현재 시크릿을 `jwt_previous_secret`으로 옮기고 새 시크릿을 넣은 뒤 Open WebUI를 재시작하고, 모든 Open WebUI가 새 시크릿을 쓰면 이전 시크릿을 지운다.
- 운영 확인 절차: `docs/manuals/operator-verification-checklist.ko.md` 10절. 시험 스택: `deploy/openwebui-test/README.md`.

## 6. 남은 위험과 미뤄 둔 것

- **실제 결합 미검증**: 이 Mac에는 Docker가 없어 스택과 실제 Open WebUI 로그인·로그아웃 왕복을 돌리지 못했다. `bootstrap.sh`는 로컬 SQLite 서버(같은 브랜치 바이너리, psql 호출은 sqlite3로 대신)에 세 번 돌려 확인했다: 루트 생성, 서비스 토큰, 옵션 12개, 테스트 사용자·이메일, mock 채널·가격·잔액, step-up 수동 연결, 재실행 멱등, 로그인 세션 종료. 서명한 JWT로 `/v1/models`를 불러 이메일 자동 연결도 확인했다. 실제 확인은 운영 확인 절차 10절.
- **조직 지갑 과금**: 치환 뒤 컨텍스트가 실제 사용자로 바뀌는 것은 테스트(`TestOpenWebUIResolvedTokenUsesUserBillingContext`)로 고정했고, 조직 멤버의 사용량이 조직 지갑에서 빠지는지는 운영 확인 10절의 수동 확인이다.
- Task 7 정리 후보: 마지막 수정 묶음에서 `/api/status`의 `oauth2_enabled`를 provider 엔드포인트와 같은 `oauth2ProviderConfigured` 기준(켜졌지만 client_id·secret·redirect_uri 중 하나가 비면 false)으로 보고하고, OIDC 섹션의 `oauth2.*` 저장이 성공하면 `['status']` 무효화·localStorage `status` 스냅샷 삭제, 서비스 키 입력 `autoComplete="new-password"`를 적용했다. 켜진 상태에서 client_id·secret·redirect_uri를 비우는 것 자체는 여전히 막지 않는다(비우면 authorize가 `invalid_client`로 닫히고 상태 응답도 꺼진 것으로 보인다). 남은 것: `client_secret` 설정 여부 배지, 링크 취소·relay 끈 상태 테스트 없음, issuer 입력 오류 문구가 일반 문구.
- 서버 주소(OIDC issuer): `oauth2.enabled` 켜기는 이제 `system_setting.ServerAddress`(`oidc.Issuer()`)가 비어 있으면 "Set the server address before enabling external app access"로 거부한다. 켠 뒤에 서버 주소를 바꾸거나 비우는 것은 막지 않는다 — 바꾸면 discovery의 issuer와 이미 발급한 id_token의 `iss`가 달라지므로 Open WebUI 재로그인이 필요하다.
- Redis를 켠 상태의 로그아웃 테스트가 없다(세션 조회 `GetUserSessionCached`의 Redis 경로는 develop 기존 테스트에만 기댄다).
- relay 사용자 확인이 O12 Redis 상태에서 `ErrUserAuthCachePending`을 받으면 403이 아니라 500(데이터베이스 오류)으로 거부한다.
- 호출 위치 AST 검사(`TestOpenWebUISubstitutionOnlyOnRelayTokenAuth`)는 `middleware` 패키지만 본다.
- root가 없는 `organization_id`로 조직 멤버 API를 부르면 정리되지 않은 record-not-found 오류가 나온다(develop 기존 동작).
- `oauth2.app_url`이 이 서버의 `/playground`를 가리키는 설정을 막지 않는다. 첫 상태 응답 전에 플레이그라운드가 잠깐 그려질 수 있다.
- 공유 `TestMain`을 쓰는 model 테스트가 링크 테이블이 필요하면 `MigrateForkOpenWebUITables`를 불러야 한다. fork 테스트 일부가 `model.InitLogDB()`로 컬럼 따옴표 전역값을 남긴다(다른 fork 테스트와 같음).
- 검토에서 미뤄 둔 사소한 것: Task 5(root의 조직 멤버 관리) 테스트가 감사 로그 매개변수를 단언하지 않는다. Task 4 접근 토큰 레지스트리(`middleware/fork_access_token_routes.go`)는 줄마다 표식 대신 그 파일 관례대로 O13 블록 주석 하나를 쓴다. Task 6 플레이그라운드 라우트(`web/src/routes/_authenticated/playground/index.tsx`)의 `beforeLoad` 표식은 줄 끝이 아니라 위 줄에 있다.

운영자가 알아야 할 남은 위험(브랜치 전체 검토):

- **Open WebUI `ENABLE_SIGNUP`은 운영에서 꺼 둔다.** relay의 이메일 자동 연결은 Open WebUI가 JWT에 넣은 이메일을 믿으므로, 가입이 열려 있으면 누구나 기존 new-api 사용자의 이메일로 Open WebUI 계정을 만들어 그 사용자에게 연결·과금될 수 있다.
- **`openwebui.allow_unsigned_requests`의 기본값은 true다**(원본 기본값, 의도적). 서명 JWT(`X-OpenWebUI-User-Jwt`) 없이 서비스 키만으로 온 요청은 401 대신 서비스 토큰 소유자에게 과금된다 — 서비스 키를 가진 쪽은 누구든 그 소유자 잔액을 쓸 수 있으므로 키를 사용자 토큰처럼 보관하고, 필요하면 끈다.
- **upstream 테스트 순서 의존**: `go test -shuffle=on ./controller/`에서 `TestAPITokenAuditDatabaseMatrix`와 task-plugin 레지스트리 테스트(`TestAddChannelTaskPluginRequiresBindPermission` 등)가 순서에 따라 실패한다. 베이스 022def514(develop 자체)에서도 재현되는 upstream 문제이고 O13과 무관하다. 기본 순서(`-count=1`)에서는 통과한다.
