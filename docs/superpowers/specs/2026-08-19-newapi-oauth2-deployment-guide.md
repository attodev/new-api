# new-api 배포 가이드 — OpenWebUI OAuth2 연동

이 문서는 **new-api 쪽**에서 OAuth2 프로바이더 기능을 실제로 켜기 위한
설정 방법을 정리한다. 구현 배경/설계는 다음을 참고:

- `docs/superpowers/specs/2026-08-16-oauth2-provider-design.md` (설계)
- `docs/superpowers/specs/2026-08-19-newapi-oauth2-changes-summary.md` (변경 요약)

> **선행 조건**: 이 기능은 `playground` 브랜치에 있다. `team`/`main`에
> 아직 머지되지 않았다면, 아래 설치 방법 중 "브랜치에서 직접 빌드"
> 방식을 써야 한다.

---

## 1. 처음부터 새로 설치하는 경우

### 1-1. 소스 준비

```bash
git clone git@github.com-atto:attodev/new-api.git
cd new-api
git checkout playground   # team/main에 머지되기 전까지는 이 브랜치 사용
```

### 1-2. Docker Compose로 띄우기 (권장)

new-api는 기본 `docker-compose.yml`에 이미 Redis + PostgreSQL이 포함되어
있다. **이 기능은 Redis가 필수**이므로 (Redis 없이는 OAuth2 토큰 자체가
저장될 곳이 없음) 별도 설정 없이 기본 compose 파일을 그대로 쓰면 된다.

```yaml
# docker-compose.yml — 기존 파일 그대로, 이미지만 자체 빌드로 교체
services:
  new-api:
    build:
      context: .
      dockerfile: Dockerfile   # 공식 이미지(calciumion/new-api) 대신 이 브랜치를 직접 빌드
    container_name: new-api
    restart: always
    ports:
      - "3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    environment:
      - SQL_DSN=postgresql://root:123456@postgres:5432/new-api
      - REDIS_CONN_STRING=redis://:123456@redis:6379   # 필수! 비워두면 OAuth2 기능이 통째로 동작 안 함
      - TZ=Asia/Seoul
    depends_on:
      - redis
      - postgres
    networks:
      - new-api-network
  redis:
    image: redis:latest
    command: ["redis-server", "--requirepass", "123456"]
    networks:
      - new-api-network
  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: root
      POSTGRES_PASSWORD: 123456
      POSTGRES_DB: new-api
    volumes:
      - pg_data:/var/lib/postgresql/data
    networks:
      - new-api-network
volumes:
  pg_data:
networks:
  new-api-network:
    driver: bridge
```

```bash
docker compose build
docker compose up -d
```

`http://localhost:3000`으로 접속해 초기 설정 마법사(관리자 계정 생성
등)를 진행한다.

### 1-3. Docker 없이 직접 실행하는 경우

```bash
# 백엔드
go build -o new-api .
SQL_DSN=postgresql://user:pass@localhost:5432/new-api \
REDIS_CONN_STRING=redis://localhost:6379 \
./new-api

# 프론트엔드 (별도 빌드 후 백엔드에 임베드됨 — 반드시 백엔드 빌드 전에 먼저 빌드)
cd web/default
bun install
bun run build
cd ../..
go build -o new-api .   # 프론트 빌드 후 다시 빌드해야 embed에 반영됨
```

---

## 2. 이미 운영 중인 alrouter(new-api)에 반영하는 경우

기존 배포가 `main`/`team` 브랜치 기반이라면, 이 기능이 담긴 `playground`
브랜치를 머지하거나 cherry-pick 해야 한다.

```bash
cd <기존 new-api 소스 디렉토리>
git fetch origin
git checkout team          # 또는 현재 운영 중인 브랜치
git merge origin/playground
```

### 반영 후 체크리스트

1. **Redis 확인** — 기존에 Redis 없이 운영 중이었다면 반드시 추가해야
   한다 (`REDIS_CONN_STRING` 환경변수). 이 기능뿐 아니라 토큰/사용자
   캐시 전반이 Redis 유무에 따라 동작이 달라지므로, 기존 운영에 Redis가
   없었다면 이번 기회에 추가하는 걸 권장.
2. **프론트엔드 재빌드** — `web/default` 디렉토리를 다시 빌드해야
   새 관리자 설정 화면과 Playground 버튼이 반영된다. 기존 배포 스크립트가
   `bun run build` → `go build`(임베드) 순서를 지키는지 확인.
3. **DB 마이그레이션** — 이 기능은 **새 DB 스키마/테이블이 없다** (OAuth2
   토큰은 Redis에만 저장되므로). 별도 마이그레이션 불필요.
4. **재시작 후 관리 화면에서 설정** — 아래 3번 항목 참고.

---

## 3. 관리 화면 설정 (Docker 여부와 무관하게 공통)

관리자 계정으로 로그인 후:

**사이드바 → 시스템 관리 → 사이트 및 브랜딩 → OpenWebUI 연동**

| 항목 | 값 | 설명 |
|---|---|---|
| 외부 앱 연동 사용 | 켬 | 기능 자체 활성화 |
| 클라이언트 ID | 임의의 문자열 (예: `openwebui-prod`) | 외부 앱 쪽 설정과 **정확히 일치**해야 함 |
| 클라이언트 시크릿 | 충분히 긴 임의의 랜덤 문자열 | 예: `openssl rand -hex 24`로 생성. 외부 앱 쪽에 안전하게 전달 |
| 리다이렉트 URI | 외부 앱의 콜백 URL (예: `https://chat.example.com/auth/newapi/callback`) | 외부 앱이 실제로 리스닝하는 경로와 **완전히 일치**해야 함 (다르면 요청 자체가 거부됨) |
| 새 창으로 열기 | 필요에 따라 | 켜면 외부 앱이 새 탭에서 열리고 new-api 창은 유지됨 |
| Playground 대체 | 필요에 따라 | 켜면 Playground 진입 자체가 외부 앱으로 바로 이동 (기존 Playground 화면 안 씀) |

저장 후, 발급한 **클라이언트 ID / 시크릿 / new-api의 실제 접속 주소**를
외부 앱(OpenWebUI) 운영 담당자에게 전달한다. 이 값들은 외부 앱 쪽
환경변수 설정에 그대로 들어간다 (OpenWebUI 쪽 가이드 참고).

> Client Secret은 저장 후 화면에 다시 표시되지 않는다(보안상 GET 응답에서
> 제외됨). 처음 발급할 때 반드시 별도로 기록해둘 것.

---

## 4. 독립된 컨테이너 환경에서 운영할 때 참고 사항

new-api와 OpenWebUI를 서로 다른 컨테이너(또는 서로 다른 서버)로 운영하는
것이 일반적인 구성이다. 이 경우 아래를 신경써야 한다.

### 4-1. 컨테이너 간 네트워크 도달성

- **new-api → 없음** (new-api는 OpenWebUI에 직접 요청을 보내지 않는다.
  전부 브라우저를 경유하거나, OpenWebUI가 new-api를 호출하는 방향뿐)
- **OpenWebUI → new-api**: 서버-서버 호출(`/oauth2/token`,
  `/oauth2/userinfo`, `/v1/chat/completions` 등)이 있으므로, OpenWebUI
  컨테이너에서 new-api 컨테이너로의 네트워크 경로가 반드시 열려 있어야
  한다.
  - 같은 Docker Compose 프로젝트 안이라면 서비스명으로 통신
    (`http://new-api:3000`).
  - 별도 Compose 프로젝트/호스트라면 공인 IP, 리버스 프록시 도메인, 또는
    Docker의 `host.docker.internal`/브릿지 게이트웨이 IP를 사용해야 함
    (이 세션에서 실제 테스트할 때는 `http://172.17.0.1:3000`처럼 Docker
    브릿지 게이트웨이를 사용했다 — 컨테이너 밖 호스트에 떠 있는 new-api에
    접근하는 방법 중 하나).
- **브라우저 → 둘 다**: 최종 사용자의 브라우저가 new-api와 OpenWebUI
  양쪽 도메인/포트에 직접 접근 가능해야 한다 (리다이렉트가 실제
  브라우저에서 일어나므로).

### 4-2. `redirect_uri`는 브라우저 기준 주소여야 함

new-api 관리 화면에 등록하는 `redirect_uri`는 **OpenWebUI 컨테이너
내부 주소가 아니라, 사용자의 브라우저가 실제로 접속할 수 있는 외부
주소**여야 한다 (예: `https://chat.example.com/auth/newapi/callback`,
컨테이너 내부 IP나 `localhost`가 아님).

### 4-3. 쿠키/CORS

- new-api 세션 쿠키는 `SameSite=Strict`로 고정되어 있다 — 이 때문에
  이 연동이 "new-api가 먼저 시작하는" 방식으로 설계되어 있다(설계
  문서 참고). new-api 쪽에서 추가로 건드릴 CORS 설정은 없다.
- OpenWebUI 쪽 CORS/CORS_ALLOW_ORIGIN 설정은 OpenWebUI 가이드 문서 참고
  (실제 도메인으로 접속 시 반드시 필요한 설정이 있음).

### 4-4. Redis/PostgreSQL 공유 여부

- new-api와 OpenWebUI는 **서로 다른 Redis/DB를 써야 한다** — 이 통합은
  두 서비스가 데이터 저장소를 공유하는 구조가 아니다. new-api의 Redis는
  OAuth2 토큰/캐시 전용, OpenWebUI는 자체 SQLite(또는 PostgreSQL) +
  선택적으로 자체 Redis(웹소켓/캐시용, 있으면 사용, 없어도 동작)를 쓴다.

---

## 5. 트러블슈팅 빠른 체크리스트

| 증상 | 확인할 것 |
|---|---|
| "채팅 앱에서 열기" 버튼이 안 보임 | 관리 화면에서 "외부 앱 연동 사용"이 꺼져 있음 |
| 버튼 눌러도 아무 반응 없음/에러 토스트 | new-api Redis 연결 확인 (`REDIS_CONN_STRING`) |
| 외부 앱에서 "invalid_client" | new-api와 외부 앱의 client_id/secret이 서로 다름 |
| 외부 앱에서 "invalid_grant" | 링크를 너무 늦게 클릭(120초 초과) 또는 이미 사용된 링크 재사용 |
| 외부 앱 진입은 되는데 401/재연결 요구 | new-api 세션이 24시간 지났거나 로그아웃했음 — 정상 동작(재진입하면 해결) |
