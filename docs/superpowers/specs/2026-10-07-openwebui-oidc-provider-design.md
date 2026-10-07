# Open WebUI 로그인 연동 — 내장 OIDC 제공자 (기존 oauth2 제공자의 표준 OIDC 확장)

> 상태: 설계 승인 (2026-10-07) · 구현 반영 수정 (2026-10-07)
> 요구사항 출처: "Open WebUI 연동 요구사항 — NewAPI JWT 변환 · 내장 OIDC 서버" 5~10절 (Open WebUI main `8bd8b4f`, v0.11.4 기준)
> 선행 스펙: `2026-10-07-openwebui-relay-jwt-design.md` (relay, 구현 완료)
> 대체하는 스펙: `2026-08-16-oauth2-provider-design.md` (브로커 방식, 방법 A)

## 1. 배경과 목표

Open WebUI는 Authlib 기반 표준 OIDC 클라이언트로 Authorization Code 흐름만 쓰고, discovery 문서 하나에서 나머지 주소를 찾는다. NewAPI를 그 발급 서버로 만들어 Open WebUI 코드 수정 없이 로그인·계정 생성·역할·그룹 반영을 하게 한다.

이 포크의 기존 OAuth2 제공자(`controller/oauth2_provider.go`, `model/oauth2_session.go`)는 NewAPI 화면에서 시작하는 **브로커 방식**이었다. 세션 쿠키가 `SameSite=Strict`라 Open WebUI에서 온 `authorize` 리다이렉트에 쿠키가 실리지 않는다는 이유였다. 이 스펙은 `authorize`를 SPA 페이지로 두어 같은 출처 API 호출로 그 제약을 우회하고, 기존 제공자를 표준 OIDC로 **확장**하며 브로커 전용 코드를 제거한다.

### 목표

- Open WebUI가 `OPENID_PROVIDER_URL`만 설정하면 로그인된다. NewAPI 세션이 있으면 화면 없이, 없으면 NewAPI 로그인 뒤 돌아간다.
- `sub`·`email`·`name`·`roles`·`groups` 클레임을 Open WebUI 기본 클레임 이름으로 제공한다.
- Open WebUI 로그아웃이 NewAPI 세션도 끝낸다(end-session).
- 서명 키를 무중단으로 교체할 수 있다.
- relay 스펙의 이메일 자동 연결이 신뢰할 수 있는 이메일(NewAPI가 발급한 값)을 받게 된다.

### 결정 사항 (브레인스토밍에서 확정)

| 질문 | 결정 |
| --- | --- |
| 기존 브로커 제공자 | 확장. `session-init`·센티널 relay 토큰·로그아웃 폐기 훅은 삭제 |
| authorize 성립 방식 | **SPA 페이지** + 같은 출처 `POST /api/oauth2/authorize`. 쿠키 SameSite는 Strict 유지 |
| `groups` 클레임 | NewAPI 그룹 이름 + 조직 소속이면 조직 이름 |
| 이메일 없는 계정 | authorize에서 거부하고 프로필 이동 안내. 가입 정책은 변경하지 않음 |
| Playground "Open in Chat App" | 유지하되 `app_url` 단순 링크로 변경 |
| 로그아웃 연동 | end-session만 1차. 백채널은 다음 단계 |
| 클라이언트 | Open WebUI 하나 고정(기존 `oauth2` 설정 확장) |
| refresh_token · 동의 화면 | 없음 |

## 2. 엔드포인트와 토큰 형식

issuer = `strings.TrimRight(system_setting.ServerAddress, "/")`.

| 경로 | 인증 | 동작 |
| --- | --- | --- |
| `GET /.well-known/openid-configuration` | 없음 | discovery 문서(2.1) |
| `GET /oauth2/jwks` | 없음 | 현재 키 + 교체 중인 이전 키의 공개키(JWK, `kid`, `use:"sig"`, `alg:"RS256"`) |
| `GET /oauth2/authorize` | SPA | 4절. 백엔드에 등록하지 않음(`NoRoute` → SPA). 이 경로와 `/oauth2/logout`은 테마가 classic이어도 기본 테마 `index.html`을 서빙한다(classic 프런트에는 해당 라우트가 없음) |
| `POST /api/oauth2/authorize` | 세션(`UserAuth`) | 파라미터 검증 → code 발급 → `{success, data:{redirect_url}}` 또는 `{success:false, error, message}` |
| `POST /oauth2/token` | `client_secret_basic` 또는 `client_secret_post` | code 교환(2.3) |
| `GET /oauth2/userinfo` | `Bearer <access_token>` | 5절 클레임 |
| `GET /oauth2/logout` | SPA | end-session 페이지(6절). discovery의 `end_session_endpoint` |
| `POST /api/oauth2/logout` | 세션(있으면) | SPA가 부름. 검증된 `id_token_hint` 또는 사용자 확인(`confirmed`)이 있을 때만 세션 삭제, `{redirect_url}` 반환. 아니면 `{redirect_url: null, needs_confirmation: true}` |
| `POST /api/oauth2/rotate-signing-key` | `RootAuth` | 서명 키 교체(3절) |

### 2.1 discovery 문서

```json
{
  "issuer": "https://newapi.example.com",
  "authorization_endpoint": "https://newapi.example.com/oauth2/authorize",
  "token_endpoint": "https://newapi.example.com/oauth2/token",
  "userinfo_endpoint": "https://newapi.example.com/oauth2/userinfo",
  "jwks_uri": "https://newapi.example.com/oauth2/jwks",
  "end_session_endpoint": "https://newapi.example.com/oauth2/logout",
  "response_types_supported": ["code"],
  "subject_types_supported": ["public"],
  "id_token_signing_alg_values_supported": ["RS256"],
  "code_challenge_methods_supported": ["S256"],
  "token_endpoint_auth_methods_supported": ["client_secret_basic", "client_secret_post"],
  "grant_types_supported": ["authorization_code"],
  "scopes_supported": ["openid", "email", "profile"],
  "claims_supported": ["sub", "email", "email_verified", "name", "preferred_username", "roles", "groups"]
}
```

`oauth2.enabled`가 꺼져 있으면 discovery·jwks·token·userinfo는 404 `{"error":"oauth2 is not enabled"}`.

### 2.2 authorization code

- 랜덤 48자(`common.GenerateKey`), 수명 120초, 1회용(읽고 즉시 삭제).
- 저장 값: `{user_id, client_id, redirect_uri, nonce, scope, code_challenge, code_challenge_method, auth_time}`.
- 저장소: `pkg/cachex.HybridCache[string]`(네임스페이스 `oauth2:code:v2`, 값은 `common.Marshal` JSON). Redis가 있으면 노드 간 공유, 없으면 메모리(단일 노드 전제).
- 교환 시 `client_id`·`redirect_uri`가 발급 시 값과 문자 단위로 같아야 한다. `code_challenge`가 저장돼 있으면 `code_verifier` 필수이고 `base64url(SHA256(code_verifier)) == code_challenge`여야 한다. 저장돼 있지 않은데 `code_verifier`가 오면 무시한다.
- 검증 실패는 code를 소모하지 않는다(공격자가 남의 code를 태울 수 없음). 성공 시 삭제는 `DeleteMany`의 결과로 승자를 가린다. Redis는 UNLINK 반환값, 메모리는 존재 여부를 돌려주므로 동시 교환 중 정확히 한 요청만 성공한다.

### 2.3 token 응답

```json
{
  "access_token": "<불투명 랜덤 48자>",
  "token_type": "Bearer",
  "expires_in": 3600,
  "id_token": "<RS256 JWT>",
  "scope": "openid email profile"
}
```

- **access_token**: userinfo 전용 불투명 토큰. `oauth2:access:v1` HybridCache에 `{user_id, client_id, scope}`를 TTL 3600초로 저장. relay `sk-` 토큰과 무관하며 TokenAuth를 통과하지 않는다. 기존 Redis 전용 센티널 relay 토큰은 사라진다.
- **id_token** 클레임: `iss`, `sub`(= `strconv.Itoa(user.Id)`), `aud`(= client_id), `exp`(iat+3600), `iat`, `auth_time`, `nonce`(authorize에서 받은 그대로, 없으면 생략), `email`, `email_verified`, `name`. 헤더 `alg: RS256`, `kid`. Open WebUI는 id_token에 `email`·`name`이 둘 다 있으면 userinfo를 생략하므로 이 둘은 반드시 넣는다.
- refresh_token은 발급하지 않는다(문서 9.1절).

### 2.4 클라이언트 등록 (설정)

기존 `oauth2` 네임스페이스(`setting/system_setting/oauth2_client.go`)를 확장한다.

| 필드 | 기존/신규 | 의미 |
| --- | --- | --- |
| `enabled` | 기존 | OIDC 제공자 on/off |
| `client_id`, `client_secret` | 기존 | Open WebUI `OAUTH_CLIENT_ID`·`OAUTH_CLIENT_SECRET` |
| `redirect_uri` | 기존 | `{WEBUI_URL}/oauth/oidc/login/callback`. 정확히 일치해야 함 |
| `app_url` | 신규 | Open WebUI `WEBUI_URL`. Playground 링크 대상 |
| `post_logout_redirect_uri` | 신규, 선택 | Open WebUI `WEBUI_AUTH_SIGNOUT_REDIRECT_URL` |
| `open_in_new_window`, `replace_playground` | 기존 | Playground 링크 동작 |
| (서명 키) | 신규, 내부 | 옵션 `OIDCSigningCurrentKey` / `OIDCSigningPreviousKey`. `oauth2.*` 네임스페이스 밖(점 없음)이라 `GlobalConfig`로 라우팅되지 않고, `Key` 접미사 규칙과 명시적 제외로 `GetOptions`에서 숨겨지며 `UpdateOption`은 쓰기를 거부한다. 3절 |

클라이언트는 이 하나로 고정이다. 동적 등록·다중 클라이언트는 범위 밖.

## 3. 서명 키 보관과 교체

- **생성**: `oauth2.enabled`가 켜진 상태에서 키가 처음 필요할 때 2048비트 RSA 키를 만들어 옵션 `OIDCSigningCurrentKey`에 JSON `{kid, private_pem, created_at, rotated_at}`으로 저장한다. 옵션 테이블은 세 DB 모두에서 동작하고 이미 비밀을 담고 있으므로 새 테이블을 두지 않는다. 동시 생성 경쟁은 프로세스 내 `sync.Mutex` + 저장 전 재조회로 막는다(다중 노드에서 두 노드가 동시에 만들면 나중 저장이 이기고, 먼저 저장한 노드는 옵션 갱신 이벤트로 다시 읽는다).
- **kid**: 공개키 DER(PKIX)의 SHA-256 앞 16바이트를 base64url(패딩 없음)로 인코딩. 결정적이라 재시작·노드 간에 같다.
- **메모리 캐시**: 파싱된 `*rsa.PrivateKey`와 kid를 프로세스에 들고 있고, 옵션 문자열이 바뀌면(주기적 옵션 동기화로 다른 노드의 쓰기가 들어오면) 다시 파싱한다. 다중 노드 첫 기동에서 두 노드가 동시에 키를 만들면 마지막 DB 쓰기가 이기고, 진 노드가 그 사이 발급한 토큰은 검증되지 않는다(수용).
- **교체**: `POST /api/oauth2/rotate-signing-key`(RootAuth). 새 키를 먼저 생성한 뒤 현재 키를 `OIDCSigningPreviousKey`로 내리고(`rotated_at` 기록) 새 키를 `OIDCSigningCurrentKey`로 올린다. 전체가 하나의 잠금 안에서 일어나 JWKS가 중간 상태를 노출하지 않는다. 이전 키는 교체 후 **24시간**이 지나면 JWKS에서 빠지고 옵션 값이 비워진다(id_token 수명 1시간이므로 충분). 이미 이전 키가 있는 상태에서 다시 교체하면 가장 오래된 키가 버려진다.
- **검증 대상**: id_token 발급은 항상 현재 키. end-session의 `id_token_hint` 검증은 현재·이전 키 모두로 시도하고 만료는 허용한다.
- **응답 노출 금지**: `GetOptions`는 두 키 옵션을 이름으로 걸러 돌려주지 않고, `UpdateOption`은 두 이름에 대한 쓰기를 거부한다. 관리자 화면에는 현재·이전 `kid`와 생성 시각만 `GET /api/oauth2/signing-key-status`(RootAuth)로 보여준다.

**패키지 배치** — `service/oidc/`:

| 파일 | 책임 |
| --- | --- |
| `keys.go` | 키 생성·로드·교체·정리, `CurrentKey()`, `VerificationKeys()`, `JWKS()` |
| `codes.go` | authorization code·access_token 저장·소비(HybridCache), PKCE 검증 |
| `idtoken.go` | id_token 서명(`kid` 포함), `id_token_hint` 검증(만료 허용 옵션) |
| `claims.go` | `*model.User` → 클레임 맵(5절). userinfo·id_token이 같은 함수를 씀 |
| `discovery.go` | discovery 문서 생성 |

`controller/oauth2_provider.go`는 HTTP 처리만 남기고 위 패키지를 호출한다. `model/oauth2_session.go`는 삭제한다.

## 4. 로그인 흐름 (SPA authorize 페이지)

프런트 라우트 `web/default/src/routes/oauth2/authorize.tsx`(인증 가드 밖, 로그인 레이아웃).

1. 페이지가 열리면 쿼리 문자열의 `response_type, client_id, redirect_uri, scope, state, nonce, code_challenge, code_challenge_method`를 **원문 그대로**(`URLSearchParams`로 읽음. 라우터의 기본 검색 파서는 값을 JSON으로 해석해 숫자 `state`를 깨뜨리므로 쓰지 않는다) 담아 `POST /api/oauth2/authorize`를 호출한다.
2. 응답 처리
   - **401**(`UserAuth`의 미로그인 응답) → `/sign-in?redirect=<현재 경로+쿼리 전체를 인코딩>`으로 이동. 로그인 화면은 `redirect`가 쿼리를 포함한 같은 출처 경로일 때 `new URL(target, origin)`으로 해석해 origin이 같은 경우에만 전체 내비게이션으로 돌아온다(문자열 휴리스틱 금지 — 탭·개행 우회로 오픈 리다이렉트가 된다). 돌아오면 1부터 다시.
   - **`success:false`** → 아래 표의 `error` 코드별 동작.
   - **`success:true`** → `window.location.replace(data.redirect_url)`.
3. 서버 검증 순서(`controller.OAuth2Authorize`):
   1. `oauth2.enabled`이고 `client_id` 일치 — 아니면 `error:"invalid_client"`
   2. `redirect_uri`가 설정값과 문자 단위 일치 — 아니면 `error:"invalid_redirect_uri"`
   3. `response_type == "code"` — 아니면 `error:"unsupported_response_type"`
   4. `scope`에 `openid` 포함 — 아니면 `error:"invalid_scope"`
   5. `code_challenge`가 있으면 `code_challenge_method == "S256"` — 아니면 `error:"invalid_request"`
   6. 사용자 상태 활성 — 아니면 `error:"access_denied"`
   7. 사용자 이메일 존재 — 아니면 `error:"email_required"`
   8. 통과: code 발급, `redirect_url = redirect_uri + "?code=…&state=…"`(`state`는 받은 그대로, 없으면 생략). 동의 화면 없음.

| `error` | 페이지 동작 |
| --- | --- |
| `invalid_client`, `invalid_redirect_uri` | Open WebUI로 돌려보내지 않는다(주소를 믿을 수 없음). NewAPI 페이지에 "요청이 올바르지 않습니다. 관리자에게 문의하세요" 표시 |
| `unsupported_response_type`, `invalid_scope`, `invalid_request` | `redirect_uri?error=<코드>&state=…`로 이동(표준 오류 응답). 서버가 `redirect_url`에 오류 쿼리를 담아 준다 |
| `access_denied` | 위와 같이 `redirect_uri?error=access_denied&state=…` |
| `email_required` | 페이지에 "Open WebUI를 쓰려면 이메일을 등록하세요" + "프로필로 이동" 버튼(`/profile`). 이메일을 등록한 뒤 Open WebUI를 다시 열면 자동 리다이렉트가 흐름을 다시 시작한다 |

`OAUTH_AUTO_REDIRECT` 시나리오: Open WebUI를 열자마자 authorize가 호출된다. NewAPI 로그인 상태면 페이지가 잠깐 보이고 Open WebUI로 돌아간다. 아니면 NewAPI 로그인 화면 → 로그인 → authorize → Open WebUI.

**Playground**: "Open in Chat App"은 `app_url`을 연다(새 창 여부는 `open_in_new_window`). `replace_playground`면 Playground 진입 시 바로 `app_url`로 이동. `session-init` 호출과 code 처리 코드는 삭제한다. `/api/status`에 `oauth2_app_url`을 추가한다. `app_url`이 비어 있으면 버튼을 숨긴다.

## 5. 클레임 · 역할 · 그룹

`service/oidc/claims.go`의 함수 하나가 만들고, userinfo와 id_token이 공유한다.

| 클레임 | 값 | userinfo | id_token |
| --- | --- | --- | --- |
| `sub` | `strconv.Itoa(user.Id)` | ✓ | ✓ |
| `email` | `strings.ToLower(strings.TrimSpace(user.Email))` | ✓ | ✓ |
| `email_verified` | `true` | ✓ | ✓ |
| `name` | `DisplayName`, 비면 `Username` | ✓ | ✓ |
| `preferred_username` | `Username` | ✓ | — |
| `roles` | `["admin"]`(role ≥ 10) 또는 `["user"]` | ✓ | — |
| `groups` | `[user.Group]`, 조직 소속이면 `[user.Group, org.Name]` | ✓ | — |

- `roles`는 항상 넣는다. Open WebUI가 역할 관리를 끄면 무시되고, 켜면 관리자가 자동 반영된다(`OAUTH_ADMIN_ROLES=admin`, `OAUTH_ALLOWED_ROLES=user,admin` 기본값과 일치). 특정 사용자 차단 용도의 역할 값은 범위 밖.
- `groups`의 조직 이름은 `Organization.Name`(unique). `OrganizationId == 0`이면 그룹 하나만. 빈 그룹 문자열은 넣지 않는다.
- `email_verified: true`는 이메일이 인증 코드·관리자·OAuth 경로로만 들어온다는 뜻이지 유일성 보장이 아니다(`users.email`에 unique 제약 없음). Open WebUI의 `OAUTH_MERGE_ACCOUNTS_BY_EMAIL`은 전환 기간 외에는 켜지 않는다.
- `scope`: `openid` 필수. `email`·`profile`은 받아들이되 항상 전부 돌려준다(클라이언트 하나뿐).
- 클레임 이름은 모두 Open WebUI 기본값이라 `OAUTH_*_CLAIM` 설정이 필요 없다.
- userinfo의 `sub`는 id_token과 같다(둘 다 위 함수).

## 6. 로그아웃 · 오류 처리

### 6.1 end-session

`GET /oauth2/logout?id_token_hint=…&post_logout_redirect_uri=…&state=…`는 SPA 페이지다. Open WebUI에서 온 최상위 GET에는 Strict 쿠키가 실리지 않으므로, 페이지가 같은 출처 `POST /api/oauth2/logout`을 호출해 세션을 확실히 지운다.

서버(`POST /api/oauth2/logout`, 본문에 세 파라미터 + 페이지가 붙이는 `confirmed`):

1. 본문은 64KiB로 제한한다. 공급자가 켜져 있고 `id_token_hint`가 있으면 현재·이전 키로 서명을 확인하고 `iss`·`aud`를 검사한다. 만료는 허용. 공급자가 꺼져 있으면 키를 건드리지 않는다(검증도, 키 생성도 하지 않음).
2. 검증을 통과했거나 `confirmed: true`면 세션을 지운다(`session.Clear()` + `Save()`). 센티널 토큰 폐기 훅은 삭제되므로 남는 것은 세션 정리뿐이다.
3. 그 밖(hint 없음, 서명 틀림, 공급자 꺼짐)에는 세션을 건드리지 않고 `{redirect_url: null, needs_confirmation: true}`를 돌려준다(OIDC RP-Initiated Logout §2: 유효한 hint가 없으면 사용자에게 묻는다. 아무나 이 주소로 남의 세션을 끊지 못하게). 서명이 틀린 hint만 Warn 로그.
4. 로그아웃했으면 `post_logout_redirect_uri`가 설정의 `post_logout_redirect_uri`와 정확히 같을 때만 그곳(`state`가 있으면 `?state=`를 붙임)으로. 다르거나 비어 있으면 `/sign-in`.
5. 페이지는 `redirect_url`이 오면 `auth.reset()` 후 `window.location.replace(redirect_url)`. `needs_confirmation`이면 확인 화면을 보여 주고, "로그아웃"은 `confirmed: true`로 다시 요청, "로그인 유지"는 `/`로 보낸다.

### 6.2 token 엔드포인트 오류 (RFC 6749 `{"error","error_description"}`)

| 상황 | 상태 | `error` |
| --- | --- | --- |
| 클라이언트 인증 실패(basic → post 순으로 시도) | 401 + `WWW-Authenticate: Basic realm="oauth2"` | `invalid_client` |
| `grant_type != authorization_code` | 400 | `unsupported_grant_type` |
| code 없음·만료·재사용, `redirect_uri` 불일치, `code_verifier` 불일치·누락 | 400 | `invalid_grant` |
| 설정 꺼짐 | 404 | `oauth2 is not enabled` |

`error_description`에 code·secret·verifier 값은 넣지 않는다. 클라이언트 인증은 `client_secret_basic`을 먼저 보고(RFC 6749 §2.3.1에 따라 id·secret을 form-urldecode, 디코딩 실패 시 원문), 없으면 form의 `client_id`·`client_secret`을 본다. 비교는 `subtle.ConstantTimeCompare`.

### 6.3 userinfo 오류

토큰 없음·만료·모름 → 401 + `WWW-Authenticate: Bearer error="invalid_token"`. 토큰은 유효하지만 사용자가 비활성·삭제됨 → 401 `invalid_token`(캐시 항목도 삭제).

### 6.4 로그

authorize 거부(사유·client_id·IP), token 거부(사유·client_id·IP), end-session 서명 실패는 `logger.LogWarn`. code·client_secret·code_verifier·access_token 값은 로그에 쓰지 않는다.

### 6.5 설정 저장 검증 (`controller/option.go`)

- `oauth2.enabled=true`: `client_id` 비어 있지 않음, `client_secret` 16자 이상, `redirect_uri` 절대 URL(`http`/`https`). 아니면 기존 `MsgOptionOAuth2ConfigMissing` 계열 메시지로 거부(새 키 `option.oauth2_config_missing`).
- `oauth2.redirect_uri`, `oauth2.app_url`, `oauth2.post_logout_redirect_uri`: 비어 있거나 절대 URL(호스트 필수, `#fragment` 금지 — RFC 6749 §3.1.2). 문자열 값은 앞뒤 공백을 제거해 저장한다.
- `oauth2.client_secret`: 비어 있거나 16자 이상.
- 기존 `oauth2Configured()`의 "unified UI는 redirect_uri 없이도 됨" 분기는 브로커 흐름과 함께 삭제한다.

## 7. 정리 대상

| 대상 | 처리 |
| --- | --- |
| `controller/oauth2_provider.go` — `OAuth2SessionInit`, `oauth2Configured`(unified UI 분기) | 삭제. `OAuth2Token`·`OAuth2UserInfo`는 `service/oidc` 기반으로 재작성. `OAuth2Authorize`, `OAuth2Discovery`, `OAuth2JWKS`, `OAuth2Logout`, `OAuth2RotateSigningKey`, `OAuth2SigningKeyStatus` 추가 |
| `model/oauth2_session.go` 전체(`StoreAuthCode`, `ConsumeAuthCode`, `IssueOAuth2Token`, `RevokeOAuth2Token`, `IsOAuth2SentinelTokenId`) | 삭제. `IsOAuth2SentinelTokenId` 호출부(청구 정산의 특수 처리)도 제거 |
| `controller/user.go` `Logout`의 `RevokeOAuth2Token` 호출 | 삭제 |
| `controller/logout_oauth2_test.go`, `controller/oauth2_provider_test.go`, `model/oauth2_session_test.go` | 새 테스트로 대체 |
| `router/oauth2-router.go` | `/oauth2/session-init` 제거. `/oauth2/jwks`, `/oauth2/token`, `/oauth2/userinfo`, `/.well-known/openid-configuration` 등록. `/api/oauth2/authorize`, `/api/oauth2/logout`(api-router, 세션 미들웨어 적용), `/api/oauth2/rotate-signing-key`, `/api/oauth2/signing-key-status`(RootAuth) |
| 프런트 `features/playground/api.ts` session-init 호출, Playground 라우트 code 처리 | `app_url` 단순 링크로 교체 |
| 프런트 `openwebui-integration-section.tsx` | `app_url`, `post_logout_redirect_uri` 입력, "서명 키 교체" 버튼과 현재 kid·생성 시각 표시, `redirect_uri` 설명을 `{WEBUI_URL}/oauth/oidc/login/callback`으로 갱신 |
| 프런트 라우트 | `routes/oauth2/authorize.tsx`, `routes/oauth2/logout.tsx` 추가 |
| 스펙 `2026-08-16-oauth2-provider-design.md`, OpenWebUI 핸드오프 문서 | 상단에 "이 설계로 대체됨(`2026-10-07-openwebui-oidc-provider-design.md`)" 메모 추가 |

## 8. 테스트

- **`service/oidc` 단위**: 키 생성·kid 결정성·JWKS 두 키 게시·교체 뒤 이전 키로 검증 가능·24시간 경과 정리; code 1회성·만료·`client_id`/`redirect_uri` 바인딩·PKCE S256 일치/불일치/누락; id_token 클레임·`kid`·`nonce` 전달; access_token 만료; 클레임 변환(roles admin/user, groups 조직 포함·미포함, name 대체, 이메일 소문자).
- **컨트롤러 통합**(SQLite 인메모리; 기본은 Redis 꺼진 메모리 모드, 전체 code 흐름·동시 교환 단일 승자·토큰 저장소는 miniredis로도 한 번): discovery 필드; authorize 성공·미로그인 401·`redirect_uri` 불일치·이메일 없음·비활성·PKCE 방식 오류; token 교환 성공(basic·post)·code 재사용 실패·잘못된 verifier·잘못된 secret·`redirect_uri` 불일치; userinfo 성공·만료 토큰·비활성 사용자; end-session 세션 삭제·등록되지 않은 `post_logout_redirect_uri` 무시·서명 틀린 hint 무시·만료된 hint 허용; 설정 꺼짐 404.
- **옵션 검증**: `enabled` 켜기 조건, URL 형식, secret 길이.
- **프런트**: authorize 페이지의 세 분기(로그인 이동·거부 표시·리다이렉트)와 logout 페이지, Playground 링크. 타입체크 + 브라우저 확인.
- **스테이징**: 요구사항 문서 11.2절 체크리스트 전체. 특히 "Open WebUI에서 로그아웃하면 NewAPI 세션도 끝난다"와 "서명 키 교체 뒤 기존 사용자 정상 로그인".

## 9. 범위 밖

- 백채널 로그아웃(NewAPI 로그아웃·비활성 → Open WebUI 세션 종료). relay 요청은 사용자 상태 검사에서 막히므로 모델 사용은 즉시 차단된다.
- refresh_token, 동의 화면, 동적 클라이언트 등록·다중 클라이언트, scope별 클레임 축소.
- `roles` 클레임으로 특정 사용자의 Open WebUI 사용 차단.
- 가입 시 이메일 필수화.
- 세션 쿠키 SameSite 변경.

## 10. Open WebUI 쪽 설정 (참고)

| 변수 | 값 |
| --- | --- |
| `WEBUI_URL` | `https://chat.example.com` (= NewAPI `oauth2.app_url`) |
| `OPENID_PROVIDER_URL` | `https://newapi.example.com/.well-known/openid-configuration` |
| `OAUTH_CLIENT_ID` · `OAUTH_CLIENT_SECRET` | NewAPI `oauth2.client_id` · `oauth2.client_secret` |
| `OPENID_REDIRECT_URI` | `https://chat.example.com/oauth/oidc/login/callback` (= NewAPI `oauth2.redirect_uri`) |
| `OAUTH_CODE_CHALLENGE_METHOD` | `S256` |
| `ENABLE_OAUTH_SIGNUP` | `true` |
| `OAUTH_AUTO_REDIRECT` · `ENABLE_LOGIN_FORM` | `true` · `false` |
| `DEFAULT_USER_ROLE` | `user` (역할 관리를 켜면 불필요) |
| `ENABLE_OAUTH_ROLE_MANAGEMENT` | `true` 권장 (기본 클레임 이름 사용) |
| `ENABLE_OAUTH_GROUP_MANAGEMENT` | 선택. 켜면 NewAPI 그룹·조직 이름과 같은 Open WebUI 그룹에 동기화 |
| `WEBUI_AUTH_SIGNOUT_REDIRECT_URL` | 선택 (= NewAPI `oauth2.post_logout_redirect_uri`) |
