# Open WebUI ↔ new-api 결합 테스트 (Docker)

이 폴더는 **현재 수정본(new-api 내장 OIDC + relay JWT 변환)** 을 다른 환경에서 컨테이너로 올려
Open WebUI 와 결합 테스트하기 위한 것이다. 요구사항 문서 11절 체크리스트를 통과시키는 것이 목표다.

구성: `new-api`(이 저장소에서 빌드) · `postgres:15` · `redis:7` · `open-webui`(GHCR) · 선택 `mock-upstream`

## 1. new-api 이미지 만들기

저장소 루트의 `Dockerfile` 이 프런트(default·classic) 빌드와 Go 빌드를 모두 수행한다.
`.dockerignore` 가 `web/*/dist` 를 제외하므로 로컬에 오래된 빌드가 있어도 이미지에는 들어가지 않는다.

```bash
# 저장소 루트, 브랜치 OpenWebUI 에서
git switch OpenWebUI
echo "owui-$(git rev-parse --short HEAD)" > VERSION          # VERSION 이 비어 있으면 화면에 버전이 안 보인다
docker build -t new-api-owui:$(git rev-parse --short HEAD) -t new-api-owui:latest .
```

- 빌드 머신에 인터넷이 필요하다(bun install, go mod download). 첫 빌드는 10분 안팎.
- 테스트 환경의 CPU 가 다르면 `--platform linux/amd64` 또는 `linux/arm64` 를 붙인다. 두 아키텍처 모두 필요하면
  `docker buildx build --platform linux/amd64,linux/arm64 -t <registry>/new-api-owui:<tag> --push .`
- 빌드 후 확인: `docker run --rm new-api-owui:latest --help` 가 출력되고, 이미지 안의 `/new-api` 가 실행 파일이다.

다른 환경으로 옮기기:

```bash
# 레지스트리가 있을 때
docker tag new-api-owui:latest <registry>/new-api-owui:<tag> && docker push <registry>/new-api-owui:<tag>
```

```bash
# 레지스트리가 없을 때 (파일로 전달)
docker save new-api-owui:latest | gzip > new-api-owui.tar.gz      # 테스트 환경에서: docker load < new-api-owui.tar.gz
```

`.env` 의 `NEW_API_IMAGE` 를 그 태그로 맞춘다.

## 2. 호스트 이름 정하기 (가장 중요)

OIDC 는 **브라우저**와 **Open WebUI 서버**가 new-api 를 같은 주소로 봐야 한다. discovery 의 `issuer` 와 id_token 의 `iss`
가 `http://PUBLIC_HOST:3000` 으로 박히고, Open WebUI 서버는 discovery 에 적힌 `token_endpoint`·`jwks_uri` 를 그대로 부르기 때문이다.

| 상황 | `PUBLIC_HOST` | 추가 설정 |
| --- | --- | --- |
| 같은 LAN, 공개 DNS 사용 가능 | `192-168-0-10.nip.io` (호스트 IP 를 `-` 로 이어 씀) | 없음. 브라우저도 컨테이너도 공개 DNS 로 호스트 IP 를 얻는다 |
| 사내 DNS 이름이 있음 | 그 이름 | 없음 |
| 오프라인 | `newapi.local` 같은 이름 | 브라우저 머신 `/etc/hosts` 에 추가 + `docker-compose.yml` 의 `extra_hosts` 주석 해제 |

`localhost` 는 쓸 수 없다. 컨테이너 안의 `localhost` 는 컨테이너 자신이다.

## 3. 실행 순서

```bash
cd deploy/openwebui-test
cp .env.example .env
# .env 편집: PUBLIC_HOST, NEW_API_IMAGE, 비밀값(PG/REDIS/SESSION/CRYPTO/OIDC_CLIENT_SECRET/OWUI_JWT_SECRET/OWUI_SECRET_KEY), 계정 비밀번호(8~20자)
docker compose up -d new-api            # postgres, redis, new-api
./bootstrap.sh                          # 루트·서비스 토큰·옵션·테스트 사용자. OWUI_SERVICE_KEY 를 .env 에 기록
docker compose up -d                    # open-webui 기동
```

실제 모델 공급자 키가 없으면 mock 업스트림으로 과금 경로까지 볼 수 있다.

```bash
# .env 에 MOCK_UPSTREAM=1 을 두고
docker compose --profile mock up -d new-api mock-upstream && ./bootstrap.sh && docker compose --profile mock up -d
```

`bootstrap.sh` 는 멱등이라 설정을 바꾼 뒤 다시 실행해도 된다. 실제 채널은 new-api 콘솔(`http://PUBLIC_HOST:3000`, 루트 계정)에서 추가한다.

## 4. Open WebUI 쪽 수동 설정 두 가지

1. **첫 로그인 사용자가 Open WebUI 관리자**가 된다(Open WebUI 규칙). 테스트 사용자 중 관리자로 쓸 사람으로 먼저 로그인한다.
2. **배경 태스크 구분 헤더**: Open WebUI 관리자 → Settings → Connections → OpenAI 연결의 설정(톱니) → Headers 에
   `{"X-Task": "{{TASK}}"}` 를 넣고 저장한다. 이것이 있어야 제목·태그·후속질문 생성이 사용자의 `openwebui-task` 토큰으로 청구된다.
   없으면 `openwebui` 토큰으로 청구된다(동작은 함). 환경 변수로는 설정할 수 없다.

## 5. 검수 (요구사항 문서 11절)

| 확인 | 어디서 |
| --- | --- |
| Open WebUI 를 열면 new-api 로그인 → 돌아와서 계정 생성(`user`, 첫 사용자는 `admin`) | 브라우저 |
| 채팅이 그 사람의 `openwebui` 토큰으로 청구, `other` 에 `owui_sub` | new-api 콘솔 → 사용 로그 → 상세 |
| 제목·태그 생성이 `openwebui-task` 로 청구, `owui_task` 에 태스크 이름 | 같은 곳 (4절 2번 설정 후) |
| 최초 연결이 이메일로 자동 생성됨 | 사용자 관리 → 사용자 편집 → "Open WebUI 연결" (`linked_by: auto`) |
| 모델 목록이 사용자별 그룹·제한대로 | Open WebUI 모델 선택기 |
| Open WebUI 로그아웃 → new-api 세션 종료 | 로그아웃 뒤 `http://PUBLIC_HOST:3000` 열면 로그인 화면 |
| 잘못된 비밀·만료 JWT 거부 | new-api 로그 `openwebui: rejected (401) jwt_invalid` |
| 서명 키 교체 후 로그인 | 시스템 설정 → OpenWebUI 연동 → "서명 키 교체" → 다시 로그인 |

new-api 로그: `docker compose logs -f new-api | grep openwebui`. Open WebUI 로그: `docker compose logs -f open-webui`.

## 6. 알아둘 점

- **이메일이 매핑의 열쇠다.** relay 는 처음 보는 Open WebUI 계정을 이메일로 new-api 사용자에 연결한다. new-api 의 관리자 "사용자 추가" API 는
  이메일을 저장하지 않아서 `bootstrap.sh` 는 테스트 사용자의 이메일을 psql 로 직접 넣는다. 운영에서는 이메일 인증 가입(SMTP) 또는 내장 OIDC 로
  만들어진 계정만 쓰고, Open WebUI 의 자체 가입(`ENABLE_SIGNUP`)은 꺼 둔다. 그렇지 않으면 남의 이메일로 Open WebUI 에 가입한 계정이 그 사람의 지갑에 먼저 연결될 수 있다.
- 루트/관리자로 Open WebUI 에 로그인하려면 이메일이 있어야 한다. 이메일 서버 없이는 UI 로 넣을 수 없으니 `.env` 의 `NEW_API_ROOT_EMAIL` 로 bootstrap 이 psql 로 넣게 하거나 직접 `docker compose exec -T postgres psql -U newapi -d newapi -c "update users set email='admin@example.com' where username='root';"`.
- 관리자·루트 계정은 자동 연결되지 않는다(`privileged_user`). 필요하면 사용자 편집 → "Open WebUI 연결" 에서 수동 연결한다. Open WebUI 사용자 id(`sub`) 는 거부 로그에 찍힌다.
- 이메일이 없는 new-api 계정은 Open WebUI 로그인이 "이메일 주소가 필요합니다" 로 멈춘다. 프로필에서 이메일을 등록한 뒤 다시 연다.
- `OWUI_SERVICE_KEY` 는 `bootstrap.sh` 가 채운다. 서비스 토큰을 재발급했다면 다시 `bootstrap.sh` → `docker compose up -d open-webui`.
- 서비스 토큰은 모든 Open WebUI 사용자의 관문이다. 쿼터 무제한·만료 없음으로 두고, 필요하면 IP 제한을 Open WebUI 서버 주소로 건다.
- 서명 키 교체 직후 1~2분은 로그인이 잠깐 실패할 수 있다(JWKS 캐시 300초, 노드 간 옵션 동기화 60초).
- new-api 테마가 classic 이어도 `/oauth2/authorize`·`/oauth2/logout` 은 기본 테마로 서빙된다. 그 밖의 classic 화면 이동 경로는 검증하지 않았다.
- 비밀번호 로그인 대신 2FA·외부 OAuth 로 new-api 에 로그인한 사용자는 로그인 후 Open WebUI 로 자동 복귀하지 않는다. Open WebUI 를 다시 열면 세션이 있어 바로 로그인된다.

## 7. 문제 해결

| 증상 | 확인 |
| --- | --- |
| bootstrap: `Field validation for 'Password' failed on the 'min' tag` | new-api 계정 비밀번호는 8~20자, 사용자 이름은 최대 20자. `.env` 의 `NEW_API_SERVICE_PASSWORD` / `NEW_API_TEST_USERS` 를 고치고 다시 실행 |
| Open WebUI 에 모델이 하나도 없음 | `docker compose logs new-api \| grep "openwebui: rejected"`. `link_failed: no_user` 면 이메일 불일치, `jwt_invalid` 면 `OWUI_JWT_SECRET` 불일치 |
| "Continue with NewAPI" 를 눌러도 반응 없음 / discovery 오류 | Open WebUI 컨테이너에서 `wget -qO- http://PUBLIC_HOST:3000/.well-known/openid-configuration` 이 되는지. 안 되면 2절의 호스트 이름 문제 |
| 로그인 뒤 Open WebUI 가 `invalid_client` / 콜백 오류 | `.env` 의 `OIDC_CLIENT_SECRET` 과 new-api `oauth2.client_secret` 이 다름 → `bootstrap.sh` 다시 실행 |
| id_token 검증 실패 (`iss` 불일치) | new-api `ServerAddress` 가 `http://PUBLIC_HOST:3000` 인지 (bootstrap 이 설정). 바꿨으면 Open WebUI 재시작 |
| 로그아웃 뒤 바로 다시 로그인됨 | Open WebUI 가 `end_session_endpoint` 로 보내는지 Open WebUI 로그 확인. 브라우저가 new-api 쿠키를 가진 호스트 이름이 `PUBLIC_HOST` 와 같은지 |
| 채팅은 되는데 과금 로그에 `owui_task` 가 없음 | 4절 2번의 `X-Task` 헤더 미설정 |
