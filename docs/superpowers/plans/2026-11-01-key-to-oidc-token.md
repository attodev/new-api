> **develop-openwebui 이식**: 이 계획(원본 `OpenWebUI` bf236c829)은 이 커밋에서 `develop-openwebui`로 이식되었다(O13). 아래 본문은 원본 그대로이며, 이식에서 바뀐 점(감사 로그 `oidc.token_from_key.issue`, 서버 오류 문장, 웹 경로 등)은 `docs/fork-overlays.md`의 `## O13 OpenWebUI`에 있다.

# API 키 → OIDC 액세스 토큰 발급 — 구현 계획

> 요구서: `open-webui/docs/superpowers/specs/2026-10-31-newapi-key-to-oidc-token-requirements.md`
> 선행: `docs/superpowers/specs/2026-10-07-openwebui-oidc-provider-design.md` (구현 완료, 브랜치 `OpenWebUI`)

## 0. 현재 상태 (선행 작업에서 이미 있는 것)

| 요구 | 현재 | 이번에 할 일 |
| --- | --- | --- |
| userinfo가 받아들이는 토큰 | `service/oidc/codes.go`의 불투명 access token(`oauth2:access:v1` HybridCache, `{user_id, client_id, scope}`) | 같은 저장소에 **짧은 TTL**로 발급만 하면 된다 |
| 웹 로그인과 같은 클레임 | `oidc.BuildClaims(...).UserInfo()`가 `sub`·`email`·`name`·`preferred_username`·`roles`·`groups`를 userinfo에 담음 | 그대로 재사용 → R3-1·R3-2 자동 충족 |
| 다른 API 사용 불가 | access token은 `TokenAuth`/`UserAuth`와 무관한 저장소 | 테스트로 확인만 (R3-5) |
| introspection | **없음** | 신규 (R4) |
| 키 검증 | `model.ValidateUserToken`은 quota 소진도 거부 | quota를 보지 않는 별도 검증 필요 (R2) |

## 1. 결정 사항 (MCP 쪽에 회신할 값)

| 항목 | 제안 |
| --- | --- |
| 엔드포인트 | `POST /oauth2/token-from-key` (기존 `/oauth2/*` 공개 엔드포인트 묶음에 맞춤) |
| 토큰 수명 | 600초 고정 (설정값 `token_from_key_ttl_seconds`, 60~900 범위로 제한) |
| client_id | `openwebui-mcp` (설정 가능, 시크릿 없음 — 발급 경로가 키 인증이므로 클라이언트 인증 불필요) |
| introspection | 지원. `POST /oauth2/introspect`, Open WebUI 클라이언트 자격증명으로 모든 client_id 토큰 확인 가능 |
| quota | 판단에 쓰지 않음 (요구서 R2 그대로) |

## 2. 작업 단위

### Task 1. 설정 (`setting/system_setting/oauth2_client.go`)
- `OAuth2Settings`에 추가: `TokenFromKeyEnabled bool`(기본 false), `TokenFromKeyClientId string`(기본 `openwebui-mcp`), `TokenFromKeyTTLSeconds int`(기본 600, 저장 시 60~900 검증), `TokenFromKeyRateLimit int`(키당 분당, 기본 30).
- `controller/option.go` 검증: client_id 비어 있지 않음, 메인 `client_id`와 달라야 함, TTL 범위.
- 프런트 `openwebui-integration-section.tsx`(default) — 스위치·client_id·TTL 입력, i18n 키 추가(`bun run i18n:sync`). classic 테마는 필요 시 동일.

### Task 2. 토큰 저장소 확장 (`service/oidc/codes.go`)
- `IssueAccessTokenTTL(at AccessToken, ttl time.Duration)` 추가(기존 `IssueAccessToken`은 3600으로 이를 호출). `HybridCache.SetWithTTL`이 Redis·메모리 모두 항목별 TTL을 지원함을 확인했음 — 메모리 hot cache의 기본 TTL(3600)보다 짧은 값이 실제로 먼저 만료되는지 테스트로 고정.
- `AccessToken`에 `IssuedAt`, `ExpiresAt int64`, `TokenId int`(발급 근거 키 id, 감사·폐기용, 선택) 추가 → introspection의 `exp`/`iat`에 사용. 기존 항목은 0이면 생략.

### Task 3. 키 검증 (`model/token.go` 또는 `service/oidc`)
- `model.ValidateTokenForIdentity(key) (*Token, error)`: `GetTokenByKey` → 상태 Enabled, 만료 아님. **quota 무시**. 에러는 `ErrTokenInvalid`로 통일(존재 여부를 구분해 노출하지 않음).
- 키 파싱은 `middleware/auth.go`와 같은 규칙(`Bearer ` 제거, `sk-` 접두어 제거, `-` 뒤 채널 지정 부분 제거) — 공용 헬퍼로 뽑아 재사용.
- 사용자: `model.GetUserById` → `Status == UserStatusEnabled` 아니면 `403 user_disabled`. 이메일이 비어 있으면 Open WebUI가 거부하므로 `403 email_required`로 미리 막음(요구서에 없는 코드 → 회신 시 알림).

### Task 4. 발급 엔드포인트 (`controller/oauth2_token_from_key.go`, 라우터 `router/oauth2-router.go`)
처리 순서:
1. `oauth2.enabled` 꺼짐 → 404(기존 관례). `TokenFromKeyEnabled` 꺼짐 → `403 feature_disabled`.
2. IP별 제한(미들웨어, `rateLimitFactory` 패턴, mark `OIDCK`, 예: 분당 60) → `429 rate_limited`.
3. 키 검증(Task 3) → `401 invalid_key`.
4. 키별 제한(token id 기준, Redis/메모리 둘 다) → `429 rate_limited`.
5. 사용자 검증 → `403 user_disabled`.
6. `IssueAccessTokenTTL({UserId, ClientId: TokenFromKeyClientId, Scope: "openid email profile"}, ttl)`.
7. 응답 `{access_token, token_type:"Bearer", expires_in}` + `Cache-Control: no-store`.
- 오류 형식은 요구서대로 `{error, message}` (기존 token 엔드포인트의 RFC 6749 형식과 다름 — 의도적).
- 감사 로그: `model.RecordLog(userId, LogTypeSystem, "oidc token-from-key issued token_id=%d client_id=%s ip=%s")`. 거부는 `logger.LogWarn`(token id/IP만). 키 원문·토큰 원문은 절대 기록하지 않음.

### Task 5. introspection (`POST /oauth2/introspect`, RFC 7662)
- 클라이언트 인증: 기존 `clientCredentials` + `subtle.ConstantTimeCompare` 재사용, Open WebUI 클라이언트만 허용. 실패 → 401 `invalid_client`.
- form `token`(`token_type_hint` 무시). 조회 실패·만료 → `{"active": false}`.
- 사용자 비활성 → `{"active": false}` + 토큰 폐기(userinfo와 동일 정책).
- 활성 → `{active:true, client_id, scope, sub, username, token_type:"Bearer", exp, iat, iss}`. client_id는 **발급 대상과 무관하게** 저장값 그대로 → MCP 토큰도 Open WebUI 자격증명으로 확인 가능.
- discovery(`service/oidc/discovery.go`)에 `introspection_endpoint`, `introspection_endpoint_auth_methods_supported` 추가.
- 웹 로그인 토큰도 대상이 됨(같은 저장소). `OAUTH_TOKEN_EXCHANGE_TRUSTED_CLIENT_IDS`에 두 client_id를 넣을지는 Open WebUI 운영 판단.

### Task 6. 테스트
- `service/oidc`: 짧은 TTL 만료(메모리 + miniredis), `exp/iat` 기록.
- 컨트롤러(SQLite 인메모리): 완료 확인 1~6 전부
  - 정상 발급 → userinfo 200, 웹 흐름(code→token→userinfo)과 응답 JSON 완전 동일
  - 비활성 키·만료 키·없는 키·헤더 없음 → 401 `invalid_key`; quota 0 키는 **성공**
  - 비활성 사용자 → 403; 기능 꺼짐 → 403 `feature_disabled`
  - TTL 경과 후 userinfo 401 (TTL을 테스트용으로 짧게)
  - 발급 토큰으로 `/v1/chat/completions`(TokenAuth), `/api/user/self`(UserAuth) → 거부
  - 키당 제한 초과 → 429
  - introspect: active+`openwebui-mcp`, 잘못된 자격증명 401, 만료 토큰 `active:false`
  - 로그에 키·토큰 원문이 없는지 확인
- `deploy/openwebui-test`: `ENABLE_OAUTH_TOKEN_EXCHANGE=true` 추가, README에 완료 확인 7(end-to-end exchange) 절차와 curl 예시.

### Task 7. 문서
- 이 브랜치 스펙에 9.x절(키→토큰) 추가 또는 별도 스펙 `2026-11-01-key-to-oidc-token-design.md`.
- MCP 쪽 회신 문서(1절 표 내용).

## 3. 확정 사항 (2026-11-01)
1. 경로: 기존 OIDC 경로 묶음에 맞춰 `POST /oauth2/token-from-key`, `POST /oauth2/introspect`.
2. 이메일 없는 사용자는 `403 email_required`.
3. 키의 `AllowIps`는 적용(`403 ip_not_allowed`), 모델 제한은 무관.
4. 키 비활성화·삭제 시 발급된 토큰은 즉시 폐기하지 않는다(최대 TTL 900초). 사용자 비활성은 userinfo·introspection에서 즉시 반영되고 토큰도 폐기된다.

## 4. 구현 결과 (MCP 쪽 회신용)

| 항목 | 값 |
| --- | --- |
| 엔드포인트 | `POST {issuer}/oauth2/token-from-key`, 헤더 `Authorization: Bearer sk-…`, 본문 없음 |
| 성공 | `200 {access_token, token_type:"Bearer", expires_in}` |
| 실패 | `{error, message}` — `401 invalid_key`, `403 user_disabled`, `403 email_required`, `403 ip_not_allowed`, `403 feature_disabled`, `429 rate_limited`. OIDC 제공자 자체가 꺼져 있으면 `404 {"error":"oauth2 is not enabled"}` |
| 토큰 기본 수명 | 600초 (관리자 설정 60~900) |
| client_id | `openwebui-mcp` (관리자 설정, Open WebUI client_id와 달라야 함) |
| introspection | 지원. discovery의 `introspection_endpoint` = `{issuer}/oauth2/introspect`, Open WebUI 클라이언트 자격증명(basic/post). 응답 `{active, client_id, scope, sub, username, token_type, iss, exp, iat}` |
| 호출 제한 | 키당 분당 30회(설정), IP당 분당 60회(고정) |
| 감사 로그 | 사용자 로그(시스템 유형)에 `token_id`, `client_id`, `ip` 기록. 키·토큰 원문은 기록하지 않음 |
| 기능 스위치 | 시스템 설정 → OpenWebUI 연동 → "API 키로 토큰 발급" (`oauth2.token_from_key_enabled`, 기본 꺼짐) |
