# new-api 측 변경 사항 정리 — OpenWebUI OAuth2 연동

전체 설계/구현 과정은 다음 문서에 상세히 남아있다. 이 문서는 실제로 반영된
변경 사항을 커밋 단위로 정리한 요약본이다.

- 설계: `docs/superpowers/specs/2026-08-16-oauth2-provider-design.md`
- 구현 계획: `docs/superpowers/plans/2026-08-17-oauth2-provider-implementation.md`
- OpenWebUI 팀 전달용 계약서: `docs/superpowers/specs/2026-08-16-oauth2-openwebui-integration-handoff.md`
- 해당 계약서에 대한 응답: `docs/superpowers/specs/2026-08-17-openwebui-response-to-newapi-oauth2-handoff.md`

## 1. OAuth2 프로바이더 백엔드 (핵심 기능)

new-api가 외부 채팅 앱(OpenWebUI)에게 OAuth2 IdP(신원 제공자) 역할을 하도록
추가한 기능. **new-api가 OAuth 클라이언트로서 GitHub/OIDC 등에 로그인하는
기존 기능과는 정반대 방향**이다 — 여기서는 new-api가 발급자 역할을 한다.

### 흐름 (IdP-initiated)

당초 계획은 표준적인 "클라이언트가 시작하는" OAuth 흐름(`GET
/oauth2/authorize`)이었으나, new-api 세션 쿠키가 `SameSite=Strict`라서
외부 사이트가 시작한 최상위 리다이렉트로는 쿠키가 전달되지 않는 문제가
있었다. 그래서 **new-api가 먼저 시작하는 방식**으로 바뀌었다:

1. 사용자가 new-api에 로그인된 상태에서 Playground의 "채팅 앱에서 열기"
   버튼(또는 Playground 자체, 아래 4번 참고)을 누른다.
2. 프론트가 같은 출처(same-origin)에서 `POST /oauth2/session-init` 호출 →
   1회용 인증 코드(120초 유효) 발급.
3. 브라우저가 `<외부 앱 콜백 URL>?code=...&state=...`로 이동.
4. 외부 앱 백엔드가 서버 대 서버로 `POST /oauth2/token` → `access_token`
   획득 (24시간 유효), `GET /oauth2/userinfo` → 사용자 신원 확인.
5. 이후 이 `access_token`은 new-api의 일반 API 토큰(`sk-...`)과 완전히
   동일하게 동작 — 사용자별 과금/로그 분리, 토큰 목록에는 안 보임.

### 신규 라우트

| 라우트 | 용도 |
|---|---|
| `POST /oauth2/session-init` | same-origin 전용, 로그인 세션 필요. 인증 코드 발급 |
| `POST /oauth2/token` | 서버-서버. code → access_token 교환 |
| `GET /oauth2/userinfo` | 서버-서버. `{sub, email, name, is_admin}` 반환 |

### 신규/변경 파일 (백엔드)

- `common/redis.go` — `RedisGetDel`(원자적 조회+삭제), `IsRedisNotFound` 헬퍼 추가
- `setting/system_setting/oauth2_client.go` — 설정 구조체 (`Enabled`,
  `ClientId`, `ClientSecret`, `RedirectURI`, `OpenInNewWindow`,
  `ReplacePlayground`)
- `model/oauth2_session.go` — 토큰 발급/철회, 인증 코드 저장/소비 (전부
  Redis만 사용, DB row 없음 — 아래 3번 참고)
- `controller/oauth2_provider.go` — 3개 핸들러
- `router/oauth2-router.go` — 라우트 등록
- `controller/user.go`의 `Logout()` — 로그아웃 시 OAuth2 토큰 즉시 철회
- `controller/misc.go`의 `GetStatus()` — `oauth2_enabled`,
  `oauth2_open_in_new_window`, `oauth2_replace_playground`를 공개
  `/api/status`에 노출 (일반 사용자도 이 값을 봐야 하므로 관리자 전용
  `/api/option/`이 아닌 공개 엔드포인트 사용)

## 2. 왜 DB에 저장하지 않고 Redis에만 저장하는가

발급된 토큰은 **일반 DB row가 없는, Redis에만 존재하는 토큰**이다.

- 기존 Playground의 임시 토큰 트릭(요청 하나 안에서만 존재)은 재사용
  불가 — 외부 앱은 발급받은 토큰을 여러 개의 독립된 HTTP 요청에 걸쳐
  재사용해야 하기 때문.
- `model.GetTokenByKey`가 이미 "Redis 먼저, 없으면 DB" 구조라서, 기존
  토큰 캐시 포맷(`token:<hmac>`)에 24시간 TTL로 직접 써넣으면 DB
  insert 없이도 기존 `TokenAuth()` 검증 경로를 그대로 통과한다.
- 장점: 정리할 DB row가 없고, 토큰 목록 UI에 자동으로 안 보이고, TTL
  지나면 자동 소멸.
- 트레이드오프: Redis가 초기화/재시작되면 세션이 끊김 — 이 배포 환경은
  Redis가 항상 켜져 있다는 전제 하에 설계됨.

## 3. 보안 관련 결정 및 최종 리뷰에서 수정된 사항

- `client_secret` 비교는 `crypto/subtle.ConstantTimeCompare` 사용 (타이밍
  공격 방지)
- 인증 코드는 1회용 + 120초 TTL, **원자적** Redis `GETDEL`로 소비 (조회 후
  삭제를 별도로 하면 동시 요청 시 이중 사용 가능)
- `redirect_uri`는 하드코딩이 아니라 관리자 설정값 (아래 5번)
- 최종 코드 리뷰에서 발견되어 수정된 것들:
  - `client_id`/`client_secret`/`redirect_uri` 중 하나라도 비어있으면
    "비활성화됨"과 동일하게 404 처리 (빈 문자열끼리 비교하면 통과되는
    허점 방지)
  - `RevokeOAuth2Token`이 실제 Redis 에러와 "토큰 없음"을 구분 못 하던
    문제 수정 (전자는 호출자에게 전파해야 함)
  - `/oauth2/userinfo`가 일반 `sk-` 토큰까지 받아버리던 문제 수정 — 이제
    OAuth2로 발급된 토큰인지 이름으로 확인 후에만 응답

## 4. Playground 연동 (프론트엔드)

- Playground 화면 우측 상단에 **"채팅 앱에서 열기"** 버튼 추가
  (`oauth2.enabled`가 꺼져 있으면 버튼 자체가 안 보임)
- **Playground 대체** 옵션을 켜면, Playground로 들어가는 것 자체가 이
  버튼을 누른 것처럼 동작 — 기본 제공 Playground 화면 자체를 더 이상
  쓰지 않게 됨
  - 사이드바에서 "플레이그라운드"를 **클릭하는 순간**(TanStack Router의
    `beforeLoad`)에 바로 처리 — 화면 전환이나 버튼 없이 즉시 이동
  - 단, 라우터가 마우스를 올리기만 해도 미리 로드(`preload: 'intent'`)를
    시도하는데, 이 미리보기 시점에는 아무 것도 하지 않도록 구분 처리
    (`preload` 플래그 확인) — 안 그러면 마우스만 올려도 새 창이 열리는
    버그가 생김
  - 링크를 직접 입력하거나 새로고침한 경우(클릭이 아니라서 팝업 차단
    대상)엔 화면에 버튼 하나만 보여주는 형태로 대체(브라우저가 사용자
    클릭 없는 `window.open`은 차단하기 때문)

## 5. 관리자 설정 화면

**사이드바 → 사이트 및 브랜딩 → OpenWebUI 연동**에 새 섹션 추가.

- 항목: 사용 여부, Client ID, Client Secret, Redirect URI, 새 창으로
  열기, Playground 대체
- 처음엔 "인증" 탭의 OAuth 연동 화면 안에 넣었다가, "다른 탭들은 전부
  new-api가 로그인하는 방향인데 이건 반대 방향이라 성격이 다르다"는
  피드백으로 별도 위치로 이동. 처음엔 최상위 메뉴로 뺐다가, 다시 "사이트
  및 브랜딩" 그룹 안의 하위 섹션으로 최종 정착.
- 기존 옵션 저장 방식(`PUT /api/option/`, 관리자 전용)을 그대로 재사용 —
  새 백엔드 엔드포인트를 만들지 않음.

## 6. 국제화(i18n)

이 기능과 관련된 모든 문자열을 영어(`en.json`)와 한국어(`kr.json`)에
반영. 관리 화면 라벨/설명, Playground 버튼/안내 문구, 상태 메시지 전부
한글 번역 완료.

## 정리되지 않고 남아있는 부분 / 운영 시 참고 사항

- `client_id`/`client_secret`/`redirect_uri`의 실제 값은 배포 시
  관리자가 직접 설정해야 함 (테스트 환경에서만 임의값 사용 중)
- Redis가 꺼져 있는 배포 환경에서는 이 기능 전체가 동작하지 않음
  (설계상 의도된 제약)
