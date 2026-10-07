# Open WebUI relay 연동 — 서비스 키 + 서명 JWT를 사용자 토큰으로 변환

> 상태: 설계 승인 (2026-10-07)
> 요구사항 출처: "Open WebUI 연동 요구사항 — NewAPI JWT 변환 · 내장 OIDC 서버" (Open WebUI main `8bd8b4f`, v0.11.4 기준)
> 후속 스펙: 내장 OIDC 서버 (기존 `oauth2` 제공자를 표준 OIDC로 확장, 별도 문서)

## 1. 배경과 목표

Open WebUI는 모든 사용자의 모델 호출을 **같은 연결·같은 서비스 키**로 보내고, 사람은 요청마다 새로 서명한 HS256 JWT 헤더(`X-OpenWebUI-User-Jwt`) 하나로만 구분한다. NewAPI는 이 요청을 받아 JWT를 검증하고, **그 사람 명의의 토큰으로 바꿔치기한 뒤** 기존 청구·로그·모델 제한 흐름에 태운다. Open WebUI 쪽은 코드 수정 없이 설정만 바꾼다.

이 포크에는 Open WebUI 연동용 OAuth2 제공자(`controller/oauth2_provider.go`, 브로커 방식)가 이미 있지만, 그 설계는 Open WebUI가 사용자마다 다른 `sk-` 토큰을 API 키로 쓰도록 Open WebUI를 고치는 전제였다. 이번 요구사항은 Open WebUI 무수정이 전제이므로 relay 쪽은 완전히 새 기능이다. 기존 OAuth2 제공자는 이 스펙에서 건드리지 않고, 다음 스펙(내장 OIDC)에서 확장·정리한다.

### 목표

- Open WebUI 사용자 A의 채팅·모델 목록·임베딩·이미지·배경 태스크가 NewAPI 사용 로그에 **A의 토큰**으로 청구된다.
- 모델 목록이 A의 그룹·모델 제한 기준으로 사용자별로 나온다.
- 기존 `sk-` 키로 직접 부르는 호출은 한 글자도 바뀌지 않는다.
- JWT 비밀을 무중단으로 교체할 수 있다.
- 거부 사유가 서버 로그에 남아, Open WebUI 화면에 "모델 없음"으로만 보이는 상황을 추적할 수 있다.

### 결정 사항 (브레인스토밍에서 확정)

| 질문 | 결정 |
| --- | --- |
| 기존 브로커식 OAuth2 제공자 | 유지. 다음 스펙에서 표준 OIDC로 확장하며 정리 |
| 착수 순서 | relay 먼저, OIDC 다음 |
| 사용자의 "지정 토큰" | 사용자별 **실제 토큰 행 자동 생성** (`openwebui`, `openwebui-task`). 사용자 키 목록에 보이며, 삭제하면 다음 요청에서 재생성 |
| 배경 태스크 청구 | 본인 명의 별도 토큰 `openwebui-task` |
| JWT 없는 요청 | 설정 토글 `allow_unsigned_requests`, 기본 true = 서비스 토큰 자체로 청구 |
| 최초 연결 | JWT `sub`를 키로, 처음 보는 `sub`는 `email`(소문자 비교)로 자동 연결 |
| 연결 실패 | 모두 403 거부 + 관리자 수동 연결 UI (1차 범위) |
| 구현 위치 | `TokenAuth()` 안에 치환 지점 하나, 로직은 `service/openwebui` 패키지에 격리 |

## 2. 아키텍처 · 구성요소

새 코드는 네 묶음이고, 기존 코드 수정은 `middleware/auth.go`의 `TokenAuth()` 한 곳과 `service/log_info_generate.go`의 `other` 조립 한 곳이다.

### 2.1 설정 — `setting/system_setting/openwebui.go`

`config.GlobalConfig.Register("openwebui", ...)`로 등록한다. 기존 `oauth2` 네임스페이스는 OIDC 단계에서 쓰므로 건드리지 않는다.

| 필드 | 타입 | 기본값 | 의미 |
| --- | --- | --- | --- |
| `relay_enabled` | bool | false | JWT 변환 분기 on/off |
| `service_key` | string | "" | 서비스 토큰의 키(`sk-` 접두사 제거 후 저장). Open WebUI `OPENAI_API_KEYS`와 같은 값 |
| `jwt_secret` | string | "" | 현재 비밀. Open WebUI `FORWARD_USER_INFO_HEADER_JWT_SECRET` |
| `jwt_secret_previous` | string | "" | 교체용 구 비밀. 비어 있으면 무시 |
| `jwt_issuer` | string | `open-webui` | `iss` 기대값 |
| `jwt_leeway_seconds` | int | 60 | `exp`·`iat` 시계 오차 허용 |
| `allow_unsigned_requests` | bool | true | JWT 없는 요청을 서비스 토큰으로 청구할지 |

**저장 시 검증** (`controller/option.go`의 기존 옵션 검증 지점): `relay_enabled`를 true로 저장하려면 `service_key`가 존재하는 활성 토큰이어야 하고 `jwt_secret`이 32자 이상이어야 한다. 아니면 저장을 거부한다. `service_key`는 저장 시 `sk-` 접두사와 공백을 제거한다.

서비스 키를 토큰 행 플래그가 아니라 설정에 두는 이유: 토큰 스키마와 토큰 편집 UI를 건드리지 않고, 키 회전이 "새 토큰 발급 → 설정에 붙여넣기"로 끝난다.

### 2.2 매핑 모델 — `model/openwebui_link.go`

```go
type OpenWebUILink struct {
    Id        int    `json:"id"`
    Sub       string `json:"sub" gorm:"type:varchar(191);uniqueIndex"`
    UserId    int    `json:"user_id" gorm:"uniqueIndex"`
    Email     string `json:"email" gorm:"type:varchar(191)"` // 연결 당시 JWT email, 감사용
    LinkedBy  string `json:"linked_by" gorm:"type:varchar(16)"` // "auto" | "admin"
    CreatedAt int64  `json:"created_at"`
}
```

테이블 `openwebui_user_links`. `sub`·`user_id` 각각 unique라 한 Open WebUI 계정은 한 NewAPI 사용자에, 한 NewAPI 사용자는 한 Open WebUI 계정에만 묶인다. GORM AutoMigrate로 생성하며 SQLite·MySQL·PostgreSQL 모두에서 동작하는 타입만 쓴다.

함수: `GetOpenWebUILinkBySub`, `GetOpenWebUILinkByUserId`, `CreateOpenWebUILink`(unique 충돌을 `ErrOpenWebUILinkConflict`로 변환), `DeleteOpenWebUILink`, `FindUsersByEmailFold(email)`(삭제되지 않은 사용자, 소문자 비교. DB별 `LOWER()`는 세 DB 모두 지원).

**캐시**: `pkg/cachex.HybridCache[int]`로 `sub → user_id`를 메모리+Redis에 TTL 10분으로 둔다. 해제·수동 연결 시 해당 `sub` 키를 무효화한다. 미연결(negative) 결과는 캐시하지 않는다(관리자가 연결한 직후 바로 반영되어야 함).

### 2.3 서비스 패키지 — `service/openwebui/`

| 파일 | 책임 |
| --- | --- |
| `jwt.go` | `VerifyUserJWT(raw, cfg) (*Claims, error)`. `golang-jwt/jwt/v5`로 HS256만 허용(`WithValidMethods`), `exp` 필수(`WithExpirationRequired`), `iss` 일치(`WithIssuer`), leeway(`WithLeeway`), `aud` 미요구. `jwt_secret` → `jwt_secret_previous` 순으로 시도. 반환 Claims: `Sub, Email, Name, Role`. 오류는 사유 enum(`ErrJWTExpired`, `ErrJWTNoExp`, `ErrJWTBadSignature`, `ErrJWTBadIssuer`, `ErrJWTBadAlg`, `ErrJWTMalformed`)으로 구분 |
| `link.go` | `ResolveUserId(ctx, claims) (userId int, reason string, err error)`. 캐시 → DB → email 자동 연결 순. 실패 사유: `no_user`, `ambiguous_email`, `user_already_linked`, `user_missing` |
| `provision.go` | `GetOrCreateUserToken(userId int, name string) (*model.Token, error)`. 사용자 단위 `singleflight`로 동시 생성을 막고, 생성 뒤 재조회해 가장 오래된 행을 쓴다. `(user_id, name) → key`를 `HybridCache[string]` TTL 10분에 둔다. 캐시된 키의 토큰이 사라졌으면(삭제됨) 캐시를 비우고 다시 만든다 |
| `resolve.go` | `Resolve(c *gin.Context, serviceToken *model.Token) (*model.Token, error)`. 위 셋을 묶는 유일한 진입점. 미들웨어는 이것만 부른다. 오류 타입 `*RejectError{Status int, MessageKey i18n key, LogReason string}` |
| `context.go` | 컨텍스트 키 상수와 setter/getter: `openwebui_sub`, `openwebui_task`, `openwebui_attributed`, `openwebui_unsigned` |

자동 생성 토큰의 속성: `Name` = `openwebui` 또는 `openwebui-task`, `UnlimitedQuota=true`(사용자 지갑에서 차감), `Group=""`(사용자 그룹 따름), `ExpiredTime=-1`, `Status=Enabled`, `ModelLimitsEnabled=false`.

### 2.4 `TokenAuth()` 변경 — `middleware/auth.go`

```go
token, err := model.ValidateUserToken(key)
// ... 기존 오류 처리 ...
// ... 기존 IP 제한 검사 (들어온 서비스 토큰 기준) ...

if openwebui.IsServiceKey(key) {
    substituted, rejectErr := openwebui.Resolve(c, token)
    if rejectErr != nil {
        abortWithOpenAiMessage(c, rejectErr.Status, common.TranslateMessage(c, rejectErr.MessageKey))
        return
    }
    token = substituted
}

userCache, err := model.GetUserCache(token.UserId)
// ... 이하 기존 코드 그대로 (사용자 상태, 그룹, SetupContextForToken) ...
```

`IsServiceKey`는 `relay_enabled && service_key != "" && subtle.ConstantTimeCompare`. IP 제한은 **들어온 서비스 토큰**에 적용되므로 관리자가 서비스 토큰에 Open WebUI 서버 IP만 허용해 둘 수 있다. 치환 토큰의 IP 제한은 적용하지 않는다.

### 2.5 관리자 API · UI

`controller/openwebui_link.go`, 라우트 `/api/openwebui/*`(AdminAuth). 프런트는 `web/default`에 시스템 설정 섹션 확장과 사용자 편집 다이얼로그 영역 추가. 4절 참조.

## 3. 요청 흐름 (정상 경로)

1. `Authorization: Bearer <서비스 키>`를 기존 규칙(`sk-` 제거, `-`로 분리한 첫 조각)대로 파싱해 `ValidateUserToken()`으로 서비스 토큰 행을 얻는다.
2. 서비스 토큰의 IP 제한을 검사한다.
3. `IsServiceKey(key)`가 true면 `Resolve()`로 들어간다. 아니면 기존 흐름 그대로.
4. `X-OpenWebUI-User-Jwt`를 읽는다. 비어 있으면 미귀속 경로(5.2절).
5. JWT를 검증한다(2.3절 `jwt.go`). 검증은 요청을 받는 이 시점 한 번뿐이고, 스트리밍 중 재검증은 없다. 300초를 넘는 응답도 정상이다.
6. `sub`로 매핑을 찾는다. 캐시 → DB. 있으면 `user_id`.
7. 없으면 `email` 클레임을 소문자로 바꿔 `FindUsersByEmailFold`. 정확히 한 명이고 그 사용자가 다른 `sub`에 묶여 있지 않을 때만 `linked_by=auto`로 연결 행을 만들고 캐시에 넣는다. `email` 클레임이 비어 있으면 `no_user`.
8. 토큰 이름을 정한다. `X-Task` 헤더가 비어 있지 않으면 `openwebui-task`, 비어 있으면 `openwebui`. `GetOrCreateUserToken(userId, name)`.
9. 치환 토큰을 `ValidateUserToken(key)`로 다시 통과시킨다. 사용자가 `openwebui` 토큰을 **비활성**해 두면 여기서 기존 "토큰 비활성" 오류가 난다. 이것이 사용자 스스로 Open WebUI 사용을 끄는 방법이다.
10. 컨텍스트에 `openwebui_sub`, `openwebui_task`(X-Task 값), `openwebui_attributed=true`를 넣고 치환 토큰을 돌려준다.
11. 이후 사용자 상태(403 banned), 그룹 검증, `SetupContextForToken`, `Distribute`, 청구, 로그는 치환 토큰 기준으로 기존 코드가 돈다.
12. `service/log_info_generate.go`가 `other`를 조립할 때 컨텍스트에 `openwebui_sub`가 있으면 `owui_sub`, `owui_task`, `owui_unsigned`를 넣는다. 로그 상세의 `other` 표시로 바로 보인다.

모델 목록(`GET /v1/models`)도 같은 미들웨어를 타므로 치환 토큰의 그룹 기준으로 사용자별 목록이 된다. 추가 코드 없음.

**성능**: 정상 경로에서 추가 비용은 HMAC 검증 한 번과 캐시 조회 두 번(`sub`, 토큰 키), 치환 토큰 `ValidateUserToken` 한 번(토큰 캐시 히트). DB는 최초 연결과 토큰 생성 때만 친다.

## 4. 관리자 API · UI

### 4.1 백엔드 API (AdminAuth, `/api/openwebui/`)

| 메서드 · 경로 | 동작 |
| --- | --- |
| `GET /links?user_id=N` 또는 `?sub=S` | 연결 행 조회. 둘 중 하나 필수. 응답에 그 사용자의 `openwebui`·`openwebui-task` 토큰 존재 여부·상태(`{name, exists, status}`)를 붙인다. 없으면 `data: null` |
| `POST /links` `{user_id, sub}` | 수동 연결, `linked_by=admin`. `sub`가 이미 다른 사용자에 묶였거나 사용자가 이미 다른 `sub`에 묶였으면 409. 사용자 없음 404. 성공 시 캐시 갱신 |
| `DELETE /links/:id` | 연결 해제 + 캐시 무효화. 해당 사용자의 두 토큰은 **삭제하지 않는다**(사용 로그 연속성). 다음 요청에서 email 자동 연결이 다시 시도된다 |

응답 형식은 기존 `{"success", "message", "data"}` 관례를 따른다.

### 4.2 설정 API

기존 `PUT /api/option/`을 그대로 쓴다. `openwebui.*` 키 저장 시 2.1절 검증. 조회 응답에서 `service_key`·`jwt_secret`·`jwt_secret_previous`는 기존 `oauth2.client_secret`과 같은 방식으로 다룬다(관리자에게 반환).

### 4.3 프런트 (`web/default`)

- **시스템 설정 → Site → "OpenWebUI Integration"** 섹션(`features/system-settings/site/openwebui-integration-section.tsx`)에 "Relay (JWT 변환)" 블록 추가. 토글 `relay_enabled`, 입력 `service_key`, `jwt_secret`, `jwt_secret_previous`, `jwt_issuer`, `jwt_leeway_seconds`, 토글 `allow_unsigned_requests`. 각 필드 설명에 Open WebUI 대응 변수명을 적는다. 기존 섹션의 폼 패턴(zod + 변경 키만 저장)을 따른다.
- **사용자 관리 → 사용자 편집 다이얼로그**에 "Open WebUI 연결" 영역 추가. 연결됨: `sub`, 연결 방식, 일시, 두 토큰 상태, "연결 해제" 버튼(확인 다이얼로그). 미연결: `sub` 입력 + "연결" 버튼. `relay_enabled`가 꺼져 있으면 영역을 숨긴다.
- i18n: 새 문자열은 en 키로 추가하고 `bun run i18n:sync`. 한국어는 포크의 기존 관례를 플랜 단계에서 확인해 따른다.
- 사용 로그 화면: 전용 컬럼·필터는 1차 범위 밖. `other` 표시로 충분하다.

## 5. 오류 처리 · 미귀속 경로 · 비밀 교체

### 5.1 거부 응답

모든 거부는 `abortWithOpenAiMessage()`로 OpenAI 오류 형식을 돌려준다. 사용자 메시지는 두 종류뿐이고, 세부 사유는 서버 로그(`logger.LogWarn`, 경로·사유·파싱 가능하면 `sub`·클라이언트 IP 포함)에만 남긴다. 응답 본문에 `sub`, email, 비밀, 어느 클레임이 틀렸는지를 넣지 않는다.

| 상황 | 상태 | 사용자 메시지 (i18n en/zh) | 서버 로그 사유 |
| --- | --- | --- | --- |
| JWT 서명 불일치, 만료, `exp` 없음, `iss` 불일치, HS256 외 알고리즘(`none` 포함), 파싱 불가 | 401 | "Open WebUI authentication failed, contact your administrator" | `jwt_invalid: <구체 사유>` |
| `sub` 매핑 없고 email 자동 연결 실패 | 403 | "Your Open WebUI account is not linked to a NewAPI account" | `link_failed: no_user / ambiguous_email / user_already_linked`, sub, email |
| 매핑된 사용자가 삭제됨 | 403 | 위와 같음 | `link_failed: user_missing` |
| `relay_enabled`인데 `jwt_secret`이 비어 있음(저장 검증을 우회한 경우) | 401 | 인증 실패 메시지 | `misconfigured: empty_secret` |
| 사용자 비활성 · 치환 토큰 비활성 · 쿼터 소진 · 속도 제한 | 기존 코드 | 기존 메시지 | 기존 |

### 5.2 미귀속 경로

JWT 헤더가 없거나(평문 `X-OpenWebUI-User-*`만 있는 경우 포함):

- `allow_unsigned_requests=true`: 치환 없이 서비스 토큰으로 진행. 컨텍스트 `openwebui_attributed=false`, `openwebui_unsigned=true`. 로그 `other`에 `owui_unsigned: true`. 운영 지표는 사용 로그에서 토큰 이름이 서비스 토큰인 건수로 센다.
- `allow_unsigned_requests=false`: 401 인증 실패.

평문 `X-OpenWebUI-User-Id/Email/Name/Role` 헤더는 어떤 경우에도 읽지 않는다.

**JWT가 있는데 잘못된 경우**는 `allow_unsigned_requests`와 무관하게 항상 401이다. 비밀 설정 오류가 미귀속 청구로 조용히 흡수되는 것을 막는다.

### 5.3 비밀 교체

1. NewAPI 설정에서 `jwt_secret_previous` ← 현재 `jwt_secret`, `jwt_secret` ← 새 비밀. 저장.
2. Open WebUI를 새 비밀로 재시작. 재시작 전후 요청이 모두 통과한다.
3. 안정되면 `jwt_secret_previous`를 비운다.

검증 순서는 신 → 구. 어느 비밀로 통과했는지는 디버그 로그에만 남긴다.

### 5.4 서비스 토큰 자체의 변화

- 서비스 토큰이 삭제·비활성·만료되면 1단계 `ValidateUserToken()`에서 기존 401이 난다.
- `service_key`를 바꾸면 즉시 반영된다(설정은 메모리 상주).

## 6. 테스트

### 6.1 단위 테스트 (`service/openwebui/*_test.go`, 외부 의존 없음)

- JWT 검증 표: 정상 / 만료 / `exp` 없음 / 다른 비밀 / 구 비밀로 통과 / `iss` 불일치 / `alg: none` 거부 / RS256 거부 / 시계 30초 앞선 `iat` 통과(leeway 60) / leeway 초과 거부 / 비밀 비어 있음 / `aud` 있어도 통과.
- 연결 규칙: `sub` 있음 / email 자동 연결 성공 / 대소문자 다른 email 성공 / 사용자 없음 / email 둘 이상 / 이미 다른 `sub`에 연결 / 삭제된 사용자 / email 클레임 없음.
- 프로비저닝: 없으면 생성 / 있으면 재사용 / 삭제 후 재생성 / `X-Task` 유무에 따른 이름 / 동시 생성 10건에 행 1개.

### 6.2 통합 테스트 (`middleware/auth_openwebui_test.go`, SQLite 인메모리 + miniredis — 기존 `controller/logout_oauth2_test.go` 패턴)

- 서비스 키 + 유효 JWT → 컨텍스트 `token_id`가 사용자의 `openwebui` 토큰, `id`가 그 사용자.
- `X-Task: title_generation` → `openwebui-task` 토큰.
- JWT 없음: `allow_unsigned_requests=true` → 서비스 토큰으로 통과, `openwebui_attributed=false`; false → 401.
- 평문 `X-OpenWebUI-User-Email`만 있음 → JWT 없음과 동일, 헤더 값 무시.
- 잘못된 JWT는 `allow_unsigned_requests=true`여도 401.
- 미연결 → 403, 응답 본문에 `sub`·email 미노출.
- 사용자 비활성 → 403 기존 메시지. `openwebui` 토큰 비활성 → 기존 토큰 오류.
- 서비스 토큰 IP 제한에 걸리는 IP → 403, JWT 검증까지 가지 않음.
- `relay_enabled=false` 또는 일반 `sk-` 키 → 기존 경로 그대로(회귀).
- `GET /v1/models`가 치환 토큰의 그룹 기준으로 응답.

### 6.3 관리자 API · 옵션 테스트

- 연결 / `sub` 중복 409 / 사용자 중복 409 / 해제 후 캐시 무효화 / `sub`·`user_id` 조회.
- 옵션 저장: 비밀 32자 미만 거부 / 서비스 키가 존재하지 않는 토큰 거부 / `sk-` 접두사 제거.

### 6.4 스테이징 검수

요구사항 문서 11.1절 체크리스트를 그대로 쓴다. 코드 테스트로 못 보는 항목은 실측한다: 5분 넘는 스트리밍, 생성 중지 시 업스트림 취소, Open WebUI API 키로 들어온 호출의 JWT 부착, `X-Task` 값이 실제 헤더에 찍히는지.

## 7. 범위 밖

- 내장 OIDC 서버와 기존 브로커 코드(`/oauth2/session-init`, 센티널 토큰, Playground "Open in Chat App") 정리 → 다음 스펙.
- Open WebUI 관리자 API(`GET /api/v1/users/{sub}`)를 이용한 `oauth.oidc.sub` 정밀 매핑. email 방식이 부족해지면 그때 검토.
- 미연결 사용자의 NewAPI 계정 자동 생성.
- Open WebUI 인스턴스 여러 개(서비스 키·비밀 세트 하나만 지원).
- 사용 로그 화면의 `sub`·태스크 전용 컬럼·필터.
- JWT의 `role`·`name` 클레임은 로그에만 남기고 권한·표시에 쓰지 않는다.
- `X-OpenWebUI-Chat-Id`, `X-OpenWebUI-Auth-Type`은 서명되지 않으므로 읽지 않는다.

## 8. Open WebUI 쪽 설정 (참고)

| 변수 | 값 |
| --- | --- |
| `ENABLE_FORWARD_USER_INFO_HEADERS` | `true` |
| `FORWARD_USER_INFO_HEADER_JWT_SECRET` | NewAPI `openwebui.jwt_secret`과 같은 값 (32바이트 이상) |
| `FORWARD_USER_INFO_HEADER_JWT_EXPIRES_SECONDS` | `300` |
| `ENABLE_DIRECT_CONNECTIONS` | `false` |
| `OPENAI_API_BASE_URLS` · `OPENAI_API_KEYS` | `http://newapi:3000/v1` · NewAPI `openwebui.service_key` (sk- 포함 가능, `-` 없음) |
| 연결 Headers(관리자 화면) | `{"X-Task": "{{TASK}}"}` |
| RAG · 이미지 · 음성 base URL/key | 위와 같은 NewAPI URL · 서비스 키 |
