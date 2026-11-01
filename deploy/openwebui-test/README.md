# Open WebUI ↔ new-api 결합 테스트 (Docker)

이 폴더는 develop 위의 Open WebUI 연동(fork overlay O13: new-api 내장 OIDC provider + relay JWT 토큰 치환)을
컨테이너로 올려 Open WebUI 와 결합 테스트하기 위한 것이다. 원본은 legacy `OpenWebUI` 브랜치의 같은 폴더이며,
develop 의 로그인 세션·step-up 증명·단일 `web/` 프런트에 맞춰 고쳤다. 이식 기록은 `docs/2026-10-21-openwebui-on-develop.md`,
운영 확인 절차는 `docs/manuals/operator-verification-checklist.ko.md` 의 "Open WebUI 연동" 절이다.

구성: `new-api`(이 저장소 루트 `Dockerfile` 로 빌드) · `postgres:15` · `redis:7` · `open-webui`(GHCR) · 선택 `mock-upstream`

## 1. new-api 이미지 만들기

저장소 루트의 `Dockerfile` 이 프런트(`web/`, Bun 1.4.0) 빌드와 Go 빌드(`GOWORK=off`)를 모두 수행한다. develop 에는 프런트가
`web/` 하나뿐이다(classic 테마 없음). `.dockerignore` 가 `/web/dist`·`/web/node_modules` 를 제외하므로 로컬에 오래된 빌드가 있어도
이미지에는 들어가지 않는다. `relaykit/`·`tokenkit/` 의 `go.mod` 가 있어야 하므로 저장소를 통째로 체크아웃한 상태에서 빌드한다.

가장 간단한 방법은 compose 가 빌드하게 하는 것이다(`docker-compose.yml` 의 `build.context` 가 저장소 루트).

```bash
# 저장소 루트에서 (develop-openwebui 또는 이 기능이 합쳐진 develop)
echo "owui-$(git rev-parse --short HEAD)" > VERSION    # 비어 있어도 되지만 화면에 버전이 안 보인다 (빌드 뒤 git checkout VERSION)
cd deploy/openwebui-test
cp .env.example .env                                  # compose 는 .env 의 필수 값이 없으면 빌드도 거부한다 (3절)
docker compose build new-api                          # 이미지 이름은 .env 의 NEW_API_IMAGE (기본 new-api-owui:latest)
```

직접 빌드해 다른 환경으로 옮길 때:

```bash
docker build -t new-api-owui:$(git rev-parse --short HEAD) -t new-api-owui:latest .       # 저장소 루트
docker tag new-api-owui:latest <registry>/new-api-owui:<tag> && docker push <registry>/new-api-owui:<tag>
# 레지스트리가 없으면: docker save new-api-owui:latest | gzip > new-api-owui.tar.gz   →   docker load < new-api-owui.tar.gz
```

- 빌드 머신에 인터넷이 필요하다(bun install, go mod download). 첫 빌드는 10분 안팎.
- 테스트 환경의 CPU 가 다르면 `--platform linux/amd64` 또는 `linux/arm64` 를 붙인다.
  두 아키텍처 모두 필요하면 `docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/new-api-owui:<tag> --push .`
- 옮긴 이미지를 쓸 때는 `.env` 의 `NEW_API_IMAGE` 를 그 태그로 맞추고 `--build` 없이 띄운다.

## 2. 호스트 이름 정하기 (가장 중요)

OIDC 는 **브라우저**와 **Open WebUI 서버**가 new-api 를 같은 주소로 봐야 한다. discovery 의 `issuer` 와 id_token 의 `iss`
가 `http://PUBLIC_HOST:3000`(옵션 `ServerAddress`)으로 박히고, Open WebUI 서버는 discovery 에 적힌 `token_endpoint`·`jwks_uri` 를 그대로 부르기 때문이다.

| 상황 | `PUBLIC_HOST` | 추가 설정 |
| --- | --- | --- |
| 같은 LAN, 공개 DNS 사용 가능 | `192-168-0-10.nip.io` (호스트 IP 를 `-` 로 이어 씀) | 없음. 브라우저도 컨테이너도 공개 DNS 로 호스트 IP 를 얻는다 |
| 사내 DNS 이름이 있음 | 그 이름 | 없음 |
| 오프라인 | `newapi.local` 같은 이름 | 브라우저 머신 `/etc/hosts` 에 추가 + `docker-compose.yml` 의 `extra_hosts` 주석 해제 |

`localhost` 는 쓸 수 없다. 컨테이너 안의 `localhost` 는 컨테이너 자신이다.

이 스택은 http 로 뜬다. develop 의 refresh 쿠키는 `SESSION_COOKIE_SECURE` 가 꺼져 있을 때(기본) http 에서도 동작한다.
https 앞단을 두고 `SESSION_COOKIE_SECURE=true` 를 켜면 `SESSION_COOKIE_TRUSTED_URL` 도 함께 넣어야 하고, `/api/oauth2/logout`·
`/api/user/auth/logout` 은 그 출처(Origin/Referer)에서 온 요청만 받는다.

## 3. 실행 순서

```bash
cd deploy/openwebui-test
cp -n .env.example .env                 # 1절에서 이미 만들었으면 그대로 둔다
# .env 편집: PUBLIC_HOST, 비밀값(PG/REDIS/SESSION/CRYPTO/OIDC_CLIENT_SECRET/OWUI_JWT_SECRET/OWUI_SECRET_KEY), 계정 비밀번호
docker compose up -d --build new-api    # 이미지 빌드 + postgres, redis, new-api
./bootstrap.sh                          # 루트·서비스 토큰·옵션·테스트 사용자. OWUI_SERVICE_KEY 를 .env 에 기록
docker compose up -d                    # open-webui 기동
```

계정 규칙(develop): 비밀번호 8~128자, 루트 이름 최대 12자(초기 설정 API), 그 밖의 사용자 이름 최대 20자, 표시 이름 최대 20자.

실제 모델 공급자 키가 없으면 mock 업스트림으로 과금 경로까지 볼 수 있다.

```bash
# .env 에 MOCK_UPSTREAM=1 을 두고
docker compose --profile mock up -d --build new-api mock-upstream && ./bootstrap.sh && docker compose --profile mock up -d
```

`bootstrap.sh` 는 멱등이라 설정을 바꾼 뒤 다시 실행해도 된다. 실제 채널은 new-api 콘솔(`http://PUBLIC_HOST:3000`, 루트 계정)에서 추가한다.

### bootstrap.sh 가 develop API 를 부르는 방식

| 단계 | 호출 | develop 에서 달라진 점 |
| --- | --- | --- |
| 루트 생성 | `GET/POST /api/setup` | 루트 이름 최대 12자, 비밀번호 8~128자 |
| 로그인 | `POST /api/user/login` → `data.access_token` | 쿠키 세션이 아니라 로그인 세션의 액세스 토큰을 `Authorization: Bearer` 로 보낸다. `New-Api-User` 헤더는 쓰지 않는다. 2단계 인증·패스키가 켜진 계정은 로그인 확인을 요구하므로 bootstrap 이 멈춘다. 끝나면 `POST /api/user/auth/logout` 으로 그 세션을 닫는다 |
| 사용자 생성 | `GET /api/user/search`, `POST /api/user/` (`role: 1`) | 일반 사용자 생성은 증명이 필요 없다. 관리자 이상을 만들 때만 `admin.user.create` 증명이 필요하다(bootstrap 은 만들지 않는다) |
| 서비스 토큰 | `GET/POST /api/token/`, `POST /api/token/:id/key` | 서비스 계정 본인 세션으로 만든다 |
| 옵션 | `PUT /api/option/` (RootAuth) | 증명 불필요. 서명 키 옵션(`OIDCSigningCurrentKey`/`PreviousKey`)은 이 API 로 읽거나 쓸 수 없다 |
| 이메일 | `docker compose exec postgres psql` | 관리자 사용자 생성·수정 API 가 이메일을 저장하지 않는다(원본과 같음). OIDC 클레임과 relay 자동 연결은 DB 에서 이메일을 읽는다 |
| 수동 연결(선택) | `POST /api/verify` (`method: password`, `scope: admin.user.update`, `context: {user_id}`) → `proof_token` → `POST /api/openwebui/links` 에 `X-Security-Proof` | develop 의 step-up 증명. 증명은 한 번 쓰면 소모된다. 해제(`DELETE /api/openwebui/links/:id`)는 `admin.user.binding.clear` (`{user_id, binding_type: "openwebui"}`) 증명이 필요하고 bootstrap 은 하지 않는다 |
| mock 잔액 | `POST /api/user/manage` (`action: add_quota`, `mode: override`) | develop 은 잔액을 Redis 에 상주시키므로(O12) psql 로 `users.quota` 를 바꾸면 캐시와 어긋난다. 관리 API 로 넣는다 |

이 호출들은 모두 세션 전용 경로다. 개인 액세스 토큰(PAT)으로는 `/api/openwebui/*`·`/api/oauth2/*` 를 부를 수 없다.

## 4. Open WebUI 쪽 수동 설정 세 가지

1. **첫 로그인 사용자가 Open WebUI 관리자**가 된다(Open WebUI 규칙). 테스트 사용자 중 관리자로 쓸 사람으로 먼저 로그인한다.
2. **배경 태스크 구분 헤더**: Open WebUI 관리자 → Settings → Connections → OpenAI 연결의 설정(톱니) → Headers 에
   `{"X-Task": "{{TASK}}"}` 를 넣고 저장한다. 이것이 있어야 제목·태그·후속질문 생성이 사용자의 `openwebui-task` 토큰으로 청구된다.
   없으면 `openwebui` 토큰으로 청구된다(동작은 함). 환경 변수로는 설정할 수 없다.
3. **모델 공개 범위**: Open WebUI 는 연결에서 받은 모델을 관리자에게만 모두 보여주고, 일반 사용자에게는 접근 범위가 **Public** 이거나
   그 사용자의 그룹에 허용된 모델만 보여준다. 관리자 → Admin Panel → Settings → Models 에서 각 모델을 열어 접근 범위를 Public 으로 저장한다
   (눈 아이콘도 켜져 있어야 한다). 사용자별 모델 제한은 Open WebUI 가 아니라 new-api 쪽(사용자 그룹, `openwebui` 토큰의 모델 제한)으로 거는 편이
   한 곳에서 관리되어 깔끔하다. new-api 에 채널을 새로 추가할 때마다 이 설정을 다시 확인한다.

## 5. new-api 쪽 설정 (옵션 키와 화면)

bootstrap 이 아래 옵션을 넣는다. 화면에서는 시스템 설정 → 사이트(`/system-settings/site`)의 "OpenWebUI 연동"(내장 OIDC provider·서명 키)과
"OpenWebUI 릴레이" 섹션이다. 시크릿 4개(`oauth2.client_secret`, `openwebui.service_api_key`·`jwt_secret`·`jwt_previous_secret`)는 화면에 다시
표시되지 않는다(빈 칸으로 저장 = 유지).

| 옵션 | bootstrap 값 | 규칙 |
| --- | --- | --- |
| `ServerAddress` | `http://PUBLIC_HOST:NEW_API_PORT` | OIDC issuer. 바꾸면 Open WebUI 재시작 |
| `openwebui.service_api_key` | 서비스 토큰 키 | 활성 토큰이어야 저장된다 |
| `openwebui.jwt_secret` | `OWUI_JWT_SECRET` | 앞뒤 공백 제외 32자 이상. Open WebUI 의 `FORWARD_USER_INFO_HEADER_JWT_SECRET` 과 같아야 한다 |
| `openwebui.jwt_previous_secret` | (넣지 않음) | 시크릿 교체 중에만. 32자 이상 |
| `openwebui.jwt_issuer` | `open-webui` | 비우면 `open-webui` |
| `openwebui.jwt_leeway_seconds` | (기본값) | `exp`·`iat` 허용 오차 |
| `openwebui.allow_unsigned_requests` | `OWUI_ALLOW_UNSIGNED` (기본 true) | JWT 없는 서비스 키 요청을 서비스 토큰으로 청구(로그 `owui_unsigned`). false 면 401 |
| `openwebui.relay_enabled` | true | 서비스 키와 32자 이상 시크릿이 있어야 켜진다. 켜진 동안 둘을 비울 수 없다 |
| `oauth2.client_id` / `oauth2.client_secret` | `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` | 시크릿 16자 이상 |
| `oauth2.redirect_uri` | `http://PUBLIC_HOST:OPEN_WEBUI_PORT/oauth/oidc/login/callback` | 절대 http(s) URL, Open WebUI 의 `OPENID_REDIRECT_URI` 와 정확히 같아야 한다 |
| `oauth2.app_url` | `http://PUBLIC_HOST:OPEN_WEBUI_PORT` | 플레이그라운드 "채팅 앱에서 열기" 대상 |
| `oauth2.post_logout_redirect_uri` | `.../auth` | 로그아웃 뒤 정확히 일치할 때만 이 주소로 돌려보낸다 |
| `oauth2.open_in_new_window` / `oauth2.replace_playground` | (기본 false) | 채팅 앱을 새 창으로 / 플레이그라운드를 채팅 앱으로 대체 |
| `oauth2.enabled` | true | client id·16자 이상 시크릿·절대 redirect URI 가 있어야 켜진다 |
| `oauth2.token_from_key_client_id` | `OIDC_MCP_CLIENT_ID` (기본 `openwebui-mcp`) | 비어 있으면 안 되고 `oauth2.client_id` 와 달라야 한다. Open WebUI 의 `OAUTH_TOKEN_EXCHANGE_TRUSTED_CLIENT_IDS` 에 넣는다 |
| `oauth2.token_from_key_ttl_seconds` / `oauth2.token_from_key_rate_limit` | (기본 600 / 30) | 발급 토큰 수명 60~900초 / API 키당 분당 1~600회 (IP당 분당 60회는 고정) |
| `oauth2.token_from_key_enabled` | true | `POST /oauth2/token-from-key` (API 키 → userinfo·introspection 전용 짧은 수명 토큰) |

**서명 키 교체**: 시스템 설정 → 사이트 → "OpenWebUI 연동" → "서명 키 교체"(root 전용, 확인 대화상자), 또는 root 로그인 세션으로
`POST /api/oauth2/rotate-signing-key`(상태는 `GET /api/oauth2/signing-key-status`). 새 키로 서명을 시작하고 이전 키는 24시간 JWKS 에 남는다.
키 자체는 옵션 API 로 보이지도 쓰이지도 않는다.

## 6. 검수

| 확인 | 어디서 |
| --- | --- |
| Open WebUI 를 열면 new-api 로그인 → 돌아와서 계정 생성(`user`, 첫 사용자는 `admin`) | 브라우저 |
| 채팅이 그 사람의 `openwebui` 토큰으로 청구, `other` 에 `owui_sub` | new-api 콘솔 → 사용 로그 → 상세 |
| 조직 멤버가 쓰면 개인 지갑이 아니라 조직 지갑에서 빠짐 | 조직 대시보드 지갑·로그 (운영 확인 절차 참고) |
| 제목·태그 생성이 `openwebui-task` 로 청구, `owui_task` 에 태스크 이름 | 같은 곳 (4절 2번 설정 후) |
| 최초 연결이 이메일로 자동 생성됨 | 사용자 관리 → 사용자 편집 → "Open WebUI 연결" (`linked_by: auto`) |
| 모델 목록이 사용자별 그룹·제한대로 | Open WebUI 모델 선택기 |
| Open WebUI 로그아웃 → new-api 세션 종료 | 로그아웃 뒤 `http://PUBLIC_HOST:3000` 열면 로그인 화면. 다른 계정의 id_token 이거나 힌트가 없으면 "<사이트 이름>에서 로그아웃할까요?" 확인 화면이 먼저 나온다 |
| 잘못된 비밀·만료 JWT 거부 | new-api 로그 `openwebui: rejected (401) jwt_invalid` |
| 서명 키 교체 후 로그인 | 5절의 "서명 키 교체" → 다시 로그인 |
| API 키 → OIDC 토큰 → Open WebUI JWT | 아래 curl (`sk-…` 는 웹 OIDC 로 한 번 이상 로그인한 사용자의 키). 발급은 감사 로그 `oidc.token_from_key.issue` 에 키·토큰 값 없이 남는다 |

```bash
AT=$(curl -s -X POST -H "Authorization: Bearer sk-…" http://PUBLIC_HOST:3000/oauth2/token-from-key | jq -r .access_token)
curl -s -H "Authorization: Bearer $AT" http://PUBLIC_HOST:3000/oauth2/userinfo
curl -s -u "openwebui:$OIDC_CLIENT_SECRET" -d "token=$AT" http://PUBLIC_HOST:3000/oauth2/introspect
curl -s -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$AT\"}" http://PUBLIC_HOST:8080/api/v1/auths/oauth/oidc/token/exchange
```

new-api 로그: `docker compose logs -f new-api | grep openwebui`. Open WebUI 로그: `docker compose logs -f open-webui`.

## 7. 알아둘 점

- **이메일이 매핑의 열쇠다.** relay 는 처음 보는 Open WebUI 계정을 이메일로 new-api 사용자에 연결한다. new-api 의 관리자 "사용자 추가" API 는
  이메일을 저장하지 않아서 `bootstrap.sh` 는 테스트 사용자의 이메일을 psql 로 직접 넣는다. 운영에서는 이메일 인증 가입(SMTP) 또는 내장 OIDC 로
  만들어진 계정만 쓰고, Open WebUI 의 자체 가입(`ENABLE_SIGNUP`)은 꺼 둔다. 그렇지 않으면 남의 이메일로 Open WebUI 에 가입한 계정이 그 사람의 지갑에 먼저 연결될 수 있다.
- 루트/관리자로 Open WebUI 에 로그인하려면 이메일이 있어야 한다. 이메일 서버 없이는 UI 로 넣을 수 없으니 `.env` 의 `NEW_API_ROOT_EMAIL` 로 bootstrap 이 psql 로 넣게 하거나 직접 `docker compose exec -T postgres psql -U newapi -d newapi -c "update users set email='admin@example.com' where username='root';"`.
- 관리자·루트 계정은 자동 연결되지 않는다(`privileged_user`). 사용자 편집 → "Open WebUI 연결" 에서 수동 연결하면 화면이 비밀번호(또는 2단계 인증·패스키) 확인을
  먼저 요구한다(step-up). bootstrap 으로 하려면 `.env` 의 `OWUI_MANUAL_LINKS=root:<sub>` 를 넣고 다시 실행한다. Open WebUI 사용자 id(`sub`)는
  거부 로그 `openwebui: rejected (403) link_failed: privileged_user | sub=...` 에 찍힌다.
- 이메일이 없는 new-api 계정은 Open WebUI 로그인이 "이메일 주소가 필요합니다" 로 멈춘다. 프로필에서 이메일을 등록한 뒤 다시 연다.
- `OWUI_SERVICE_KEY` 는 `bootstrap.sh` 가 채운다. 서비스 토큰을 재발급했다면 다시 `bootstrap.sh` → `docker compose up -d open-webui`.
- 서비스 토큰은 모든 Open WebUI 사용자의 관문이다. 쿼터 무제한·만료 없음으로 두고, 필요하면 IP 제한을 Open WebUI 서버 주소로 건다.
- 서명 키 교체 직후 1~2분은 로그인이 잠깐 실패할 수 있다(JWKS 캐시 300초, 노드 간 옵션 동기화 60초).
- 로그아웃은 new-api 의 `/oauth2/logout` 페이지가 로그인 세션을 복원한 뒤 `POST /api/oauth2/logout` 을 부른다. id_token 힌트가 지금 로그인한 사용자 것일 때만
  바로 로그아웃하고, 아니면 확인 버튼을 보여 준다(클릭 없는 강제 로그아웃 방지).
- 비밀번호 로그인과 2단계 인증·패스키 확인을 거친 로그인은 같은 로그인 완료 경로(`handleLoginSuccess`)로 `/oauth2/authorize` 에 돌아온다.
  외부 OAuth(GitHub 등)로 new-api 에 로그인한 뒤의 복귀는 확인하지 않았다. 돌아오지 않으면 Open WebUI 를 다시 열면 세션이 있어 바로 로그인된다.

## 8. 문제 해결

| 증상 | 확인 |
| --- | --- |
| bootstrap: `setup: ...` 비밀번호·이름 오류 | 비밀번호 8~128자, 루트 이름 최대 12자. `.env` 를 고치고 다시 실행 |
| bootstrap: `사용자 ... 생성: Invalid input ...` | 사용자 이름·표시 이름 최대 20자, 비밀번호 8~128자. `NEW_API_SERVICE_PASSWORD` / `NEW_API_TEST_USERS` 를 고치고 다시 실행 |
| bootstrap: `로그인이 추가 확인(2단계 인증·패스키)을 요구합니다` | 그 계정의 2단계 인증·패스키를 끄거나 새 DB 로 시작 |
| bootstrap: 로그인·키 조회가 갑자기 실패(요청 제한) | 로그인·토큰 키 조회·로그아웃·`/api/verify` 는 IP 당 20분 20회 제한(`CRITICAL_RATE_LIMIT`)을 같이 쓴다. bootstrap 한 번에 5회 안팎. 20분 기다리거나 시험 스택에서만 new-api 환경 변수 `CRITICAL_RATE_LIMIT` 를 올린다 |
| bootstrap: `step-up 증명(...)` 실패 | 루트 비밀번호가 맞는지, 비밀번호 로그인 암호화(`PASSWORD_LOGIN_ENCRYPTION_ENABLED`)를 켜지 않았는지 |
| Open WebUI 에 모델이 하나도 없음 | `docker compose logs new-api \| grep "openwebui: rejected"`. `link_failed: no_user` 면 이메일 불일치, `jwt_invalid` 면 `OWUI_JWT_SECRET` 불일치, `privileged_user` 면 관리자·루트 → 7절의 수동 연결 |
| 관리자는 모델이 보이는데 일반 사용자만 안 보임 | new-api 는 정상(그 사용자의 `openwebui` 토큰으로 `curl /v1/models` 하면 나온다). Open WebUI 의 모델 접근 범위가 비공개 → 4절 3번대로 Public 으로. 그래도 안 되면 Admin Panel → Users 에서 역할이 `pending` 이 아닌지 |
| "Continue with NewAPI" 를 눌러도 반응 없음 / discovery 오류 | Open WebUI 컨테이너에서 `wget -qO- http://PUBLIC_HOST:3000/.well-known/openid-configuration` 이 되는지. 안 되면 2절의 호스트 이름 문제. `{"error":"oauth2 is not enabled"}` 면 `oauth2.*` 옵션이 덜 채워졌다 → `bootstrap.sh` 다시 실행 |
| 로그인 뒤 Open WebUI 가 `invalid_client` / 콜백 오류 | `.env` 의 `OIDC_CLIENT_SECRET` 과 new-api `oauth2.client_secret` 이 다름 → `bootstrap.sh` 다시 실행 |
| id_token 검증 실패 (`iss` 불일치) | new-api `ServerAddress` 가 `http://PUBLIC_HOST:3000` 인지 (bootstrap 이 설정). 바꿨으면 Open WebUI 재시작 |
| 로그아웃 뒤 바로 다시 로그인됨 | Open WebUI 가 `end_session_endpoint` 로 보내는지 Open WebUI 로그 확인. 브라우저가 new-api 쿠키를 가진 호스트 이름이 `PUBLIC_HOST` 와 같은지 |
| 로그아웃 때마다 확인 화면이 나옴 | Open WebUI 가 `id_token_hint` 를 보내지 않거나, 힌트의 사용자가 지금 new-api 에 로그인한 사용자와 다르다. 확인을 누르면 로그아웃된다 |
| 채팅은 되는데 과금 로그에 `owui_task` 가 없음 | 4절 2번의 `X-Task` 헤더 미설정 |
