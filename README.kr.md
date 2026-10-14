<div align="center">

![new-api](/web/public/logo.png)

# New API

**모델, 애플리케이션, 에이전트를 위한 AI 게이트웨이**

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a> |
  <strong>한국어</strong>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/Calcium-Ion/new-api/main/LICENSE">
    <img src="https://img.shields.io/github/license/Calcium-Ion/new-api?color=brightgreen" alt="license">
  </a><!--
  --><a href="https://github.com/Calcium-Ion/new-api/releases/latest">
    <img src="https://img.shields.io/github/v/release/Calcium-Ion/new-api?color=brightgreen&include_prereleases" alt="release">
  </a><!--
  --><a href="https://hub.docker.com/r/CalciumIon/new-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/badge.svg"/>
  </a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/20180" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/20180" alt="QuantumNous%2Fnew-api | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
  </a>
  <br>
  <a href="https://hellogithub.com/repository/QuantumNous/new-api" target="_blank">
    <img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=539ac4217e69431684ad4a0bab768811&claim_uid=tbFPfKIDHpc4TzR" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" />
  </a><!--
  -->
  <a href="https://atomgit.com/QuantumNous/new-api" target="_blank">
    <img alt="AtomGit G-Star" src="https://atomgit.com/QuantumNous/new-api/star/new_badge.svg" width="250" height="55" />
  </a>
</p>

<p align="center">
  <a href="#screenshots">스크린샷</a> •
  <a href="#capabilities">주요 기능</a> •
  <a href="#quick-start">빠른 시작</a> •
  <a href="#deployment">배포</a> •
  <a href="#development">개발</a> •
  <a href="#documentation">문서</a>
</p>

</div>

---

## 📝 프로젝트 설명

New API는 애플리케이션, 에이전트, 팀을 위한 자체 호스팅 AI 게이트웨이입니다. 업스트림 모델 서비스를 연결하고 클라이언트에는 일관된 API를 제공하며, 라우팅·접근·사용량·비용을 한곳에서 관리합니다.

팀 안에서 승인된 모델 접근을 공유하거나, 클라이언트마다 다시 설정하지 않고 공급자를 바꾸거나, 웹 콘솔로 비공개 멀티 모델 서비스를 운영하는 데 사용할 수 있습니다. 업스트림으로 OpenAI, Anthropic, Google Gemini, Azure OpenAI, AWS Bedrock, Vertex AI, DeepSeek, Qwen 및 기타 호환 서비스를 지원합니다.

> [!IMPORTANT]
> - 본 프로젝트는 합법적이고 승인된 AI API 게이트웨이, 조직 단위 인증, 멀티 모델 관리, 사용량 분석, 비용 정산, 비공개 배포 용도로만 사용해야 합니다.
> - 사용자는 업스트림 API 키, 계정, 모델 서비스, 인터페이스 권한을 합법적으로 취득해야 하며, 업스트림 서비스 약관과 관계 법령을 준수해야 합니다.
> - 사용자는 자신의 사용이 업스트림 서비스 약관과 관계 법령에 부합하는지 확인해야 합니다.
> - 생성형 AI 서비스를 대중에게 제공하는 경우, 사용자는 해당 규제 요건을 준수하고 관할 지역에서 요구하는 신고, 인허가, 콘텐츠 안전, 실명 인증, 로그 보관, 세무, 업스트림 승인 의무를 모두 이행해야 합니다.

<!-- -->

> [!WARNING]
> 본 프로젝트를 공개 생성형 AI 서비스나 API 재판매 서비스로 운영하는 경우, 사용자는 먼저 필요한 신고, 인허가, 콘텐츠 안전, 실명 인증, 로그 보관, 세무, 결제, 업스트림 승인 의무를 모두 이행해야 합니다.

---

<a id="screenshots"></a>

## 스크린샷

아래에 보이는 계정, 채널, 플러그인 설치 상태, 사용량, 가격, 비용은 모의 데이터입니다. 마켓플레이스에는 공식 플러그인 카탈로그가 표시됩니다. 이미지를 클릭하면 원본 크기로 볼 수 있습니다.

| 사용량 대시보드 | 플러그인 마켓플레이스 |
| --- | --- |
| [![모의 요청, 비용, 성능 지표가 표시된 사용량 대시보드](assets/screenshots/dashboard.en.jpg)](assets/screenshots/dashboard.en.jpg) | [![모의 설치 상태가 표시된 공식 플러그인 마켓플레이스](assets/screenshots/plugin-marketplace.en.jpg)](assets/screenshots/plugin-marketplace.en.jpg) |
| **모델 광장** | **사용 로그** |
| [![모의 모델 가격과 사용 가능 여부가 표시된 모델 광장](assets/screenshots/models.en.jpg)](assets/screenshots/models.en.jpg) | [![모의 토큰, 비용, 응답 시간이 표시된 사용 로그](assets/screenshots/usage-logs.en.jpg)](assets/screenshots/usage-logs.en.jpg) |

---

## 🤝 신뢰할 수 있는 파트너

<p align="center">
  <em>순서 없음</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="베이징 대학" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="Alibaba Cloud" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 특별 감사

<p align="center">
  <a href="https://www.jetbrains.com/?from=new-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong><a href="https://www.jetbrains.com/?from=new-api">JetBrains</a>가 본 프로젝트에 무료 오픈소스 개발 라이선스를 제공해 주셔서 감사합니다</strong>
</p>

---

<a id="capabilities"></a>

## 주요 기능

| 영역 | 할 수 있는 일 |
| --- | --- |
| 모델 접근 | OpenAI Chat Completions, Responses, Anthropic Messages, Gemini API를 사용합니다. 지원되는 경우 응답 스트리밍과 도구, 추론, 멀티모달 입력을 사용할 수 있습니다 |
| 라우팅 | 모델 매핑, 채널 우선순위와 가중치, 재시도, 채널 어피니티, 여러 업스트림 키를 설정합니다 |
| 사용량과 비용 | 쿼터, 구독, 사용 로그, 캐시 과금, 사용량 구간별 표현식 기반 가격을 관리합니다 |
| 접근 제어 | 사용자, 그룹, 세분화된 권한, API 키 제한을 관리하고 OAuth/OIDC, 패스키, 2단계 인증, 로그인 세션 관리를 사용합니다 |
| 비동기 작업 | JavaScript 플러그인으로 이미지, 동영상 등 작업 API를 확장하며, 작업 상태와 결과 조회를 포함합니다 |
| 웹 콘솔 | 채널과 모델을 설정하고 사용 로그와 감사 로그를 확인하며 플레이그라운드에서 모델을 사용해 봅니다. 영어, 중국어 간체, 중국어 번체, 프랑스어, 일본어, 러시아어, 베트남어를 지원합니다 |

### 프로토콜과 엔드포인트

| 인터페이스 | 주요 엔드포인트 |
| --- | --- |
| OpenAI Chat / Responses | `POST /v1/chat/completions`, `POST /v1/responses` |
| Anthropic Messages | `POST /v1/messages` |
| Gemini | `POST /v1beta/models/{model}:generateContent`, `POST /v1beta/models/{model}:streamGenerateContent` |
| Realtime / Responses WebSocket | `GET /v1/realtime`, `GET /v1/responses` (WebSocket 업그레이드) |
| 이미지 / 오디오 | `/v1/images/generations`, `/v1/images/edits`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/audio/translations` |
| 임베딩 / 재순위 | `POST /v1/embeddings`, `POST /v1/rerank` |
| 작업 플러그인 | `POST /v1/tasks/{pluginKey}`, `GET /v1/tasks/{taskId}`, 그리고 각 플러그인이 선언한 라우트 |

[RelayKit](./relaykit/README.md)은 네 가지 텍스트 프로토콜 사이의 요청, 응답, 스트리밍 변환을 제공합니다. 사용할 수 있는 기능은 채널, 업스트림 모델, 변환 경로에 따라 다르며, 프로토콜 고유의 도구와 필드는 정확히 대응되지 않을 수 있습니다. WebSocket 지원에도 호환되는 업스트림과 채널 설정이 필요합니다.

이 README는 현재 소스 트리를 설명합니다. 배포하는 버전의 릴리스 노트를 확인하세요.

<a id="quick-start"></a>

## 빠른 시작

### Docker로 로컬에서 사용해 보기

SQLite를 쓰는 단일 인스턴스를 시작하고 localhost에 바인딩합니다:

```bash
mkdir -p data
docker run --name new-api -d --restart unless-stopped \
  -p 127.0.0.1:3000:3000 \
  -e TZ=Asia/Shanghai \
  -v "$(pwd)/data:/data" \
  calciumion/new-api:latest
```

[http://localhost:3000](http://localhost:3000)을 열고 설정 마법사를 완료해 관리자 계정을 만드세요. `data` 디렉터리는 컨테이너를 교체해도 SQLite 데이터베이스를 유지합니다.

### 첫 요청 보내기

1. 업스트림 API 키, 사용 가능한 모델, 그룹 배정을 넣어 채널을 추가하고 채널 테스트를 실행합니다.
2. 모델 가격을 설정하고 사용자에게 쿼터나 유효한 구독이 있는지 확인합니다.
3. 콘솔에서 같은 그룹과 모델에 접근할 수 있는 API 키를 만듭니다.
4. OpenAI 호환 클라이언트의 base URL을 `http://localhost:3000/v1`로 설정하고 **New API에서 발급한 키**를 사용합니다.

셸의 `NEW_API_KEY`에 그 키를 설정한 뒤, 이 키로 접근할 수 있는 모델 목록을 조회합니다:

```bash
curl --fail-with-body http://localhost:3000/v1/models \
  -H "Authorization: Bearer ${NEW_API_KEY}"
```

그다음 `your-enabled-model`을 이 인터페이스를 지원하는 활성화된 모델로 바꿔 Responses를 호출합니다:

```bash
curl --fail-with-body http://localhost:3000/v1/responses \
  -H "Authorization: Bearer ${NEW_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"model":"your-enabled-model","input":"Hello!"}'
```

<a id="deployment"></a>

## 배포

### Docker Compose

저장소의 [Compose 설정](./docker-compose.yml)은 기본으로 **New API + PostgreSQL + Redis**를 시작합니다. MySQL과 별도 ClickHouse 로그 데이터베이스 예시도 들어 있습니다.

```bash
git clone https://github.com/QuantumNous/new-api.git
cd new-api
```

시작하기 전에 `docker-compose.yml`을 편집하세요. 서비스와 연결 문자열 양쪽에서 데이터베이스와 Redis 예시 비밀번호를 바꾸고, 영구적인 무작위 `SESSION_SECRET`을 설정합니다(`openssl rand -hex 32`로 생성). HTTPS 콘솔이라면 `SESSION_COOKIE_SECURE=true`와, 정확한 공개 HTTPS origin을 넣은 `SESSION_COOKIE_TRUSTED_URL`을 설정합니다.

```bash
docker compose up -d
docker compose logs -f new-api
```

### 저장소와 설정

| 구성 요소 | 선택지 |
| --- | --- |
| 메인 데이터베이스 | SQLite, MySQL ≥ 5.7.8, PostgreSQL ≥ 9.6 |
| 별도 로그 데이터베이스 | `LOG_SQL_DSN`으로 설정하며 ClickHouse도 지원합니다 |
| 캐시 | 선택 사항인 Redis와 메모리 캐시. 애플리케이션 노드가 속도 제한을 공유해야 하면 공유 Redis를 사용합니다 |
| 컨테이너 플랫폼 | Linux amd64 / arm64 |

| 변수 | 용도 |
| --- | --- |
| `SQL_DSN` | 메인 데이터베이스 연결. 설정하지 않으면 SQLite를 사용합니다 |
| `LOG_SQL_DSN` | 선택 사항인 별도 로그 데이터베이스 연결 |
| `REDIS_CONN_STRING` | Redis 연결 문자열 |
| `SESSION_SECRET` | 영구 인증 비밀값. 모든 노드가 같은 값을 써야 합니다 |
| `CRYPTO_SECRET` | 기본값은 `SESSION_SECRET`. Redis를 공유하는 노드는 실제 적용 값이 같아야 합니다 |
| `SESSION_COOKIE_SECURE` | HTTPS 콘솔이면 `true`로 설정합니다. Secure 갱신 쿠키와 엄격한 갱신/로그아웃 origin 검사를 켭니다 |
| `SESSION_COOKIE_TRUSTED_URL` | Secure 모드에서 필수: 쉼표로 구분한 정확한 HTTPS origin(경로·와일드카드 없음). 로컬 HTTP에서는 설정하지 않습니다 |
| `TRUSTED_PROXIES` | 신뢰하는 리버스 프록시 IP/CIDR 또는 `none`. 네트워크에 맞게 명시적으로 설정합니다 |

전체 설정은 [환경 변수 예시](./.env.example), [환경 변수 레퍼런스](https://docs.newapi.ai/en/docs/installation/config-maintenance/environment-variables), [인증과 세션 가이드](./docs/authentication.md)를 참고하세요. 컨테이너 변수는 Compose의 `environment`나 `env_file`로 설정합니다. `.env.example`을 복사하는 것만으로는 컨테이너에 변수가 들어가지 않습니다.

운영 환경에서는 콘솔을 HTTPS 뒤에 두고 리버스 프록시가 스트리밍과 WebSocket 업그레이드를 처리하도록 설정하세요. 데이터베이스와 마운트한 데이터를 영구 보관하고 백업합니다. 멀티 노드 배포는 메인 데이터베이스와 인증 비밀값을 공유해야 합니다. Redis 인스턴스를 따로 쓰거나 메모리 속도 제한을 쓰면 노드마다 한도를 따로 셉니다. 토폴로지별 전파 방식은 세션 가이드에 설명되어 있습니다.

[Releases](https://github.com/QuantumNous/new-api/releases)에서 이미지 버전을 고정하고, 업그레이드 노트를 확인한 뒤 업그레이드 전에 백업하세요. `latest` 태그는 게시된 빌드를 따라가므로 바뀔 수 있습니다. 기존 설치에 대한 마이그레이션과 호환성은 직접 검토해야 합니다.

<a id="development"></a>

## 개발과 확장

백엔드는 Go와 Gin을 사용합니다. 웹 콘솔은 React 19, TypeScript, Rsbuild, TanStack, Tailwind CSS 4를 사용합니다. 프런트엔드 의존성과 스크립트에는 Bun을 사용하세요. Go 언어 기준 버전은 [go.mod](./go.mod), 컨테이너 빌드 도구 체인은 [Dockerfile](./Dockerfile)을 참고하세요.

백엔드는 `web/dist`를 내장하므로, 백엔드를 시작하기 전에 프런트엔드를 빌드합니다:

```bash
# Repository root
cd web
bun install --frozen-lockfile
bun run build
cd ..
go run .
```

두 번째 터미널에서 프런트엔드 개발 서버를 시작합니다:

```bash
cd web
bun run dev -- --port 5173
```

[http://localhost:5173](http://localhost:5173)을 여세요. 개발 서버는 API 요청을 3000번 포트의 백엔드로 프록시합니다. 컨테이너 개발 백엔드는 [docker-compose.dev.yml](./docker-compose.dev.yml)과 [makefile](./makefile)의 `make dev` 타깃을 참고하세요.

| 위치 | 역할 |
| --- | --- |
| `router/`, `middleware/`, `controller/` | HTTP 라우트, 접근 검사, API 핸들러 |
| `relay/` | 업스트림 어댑터와 요청 라우팅 |
| `service/`, `model/` | 비즈니스 로직과 영속화 |
| [relaykit/](./relaykit/README.md) | 프로토콜 DTO와 변환을 위한, 독립적으로 빌드할 수 있는 Go 모듈 |
| [plugins/tasks/](./plugins/tasks/) | JavaScript 작업 플러그인. 작성 방법과 호스트 경계는 [Task Plugin API v1](./docs/plugin-api/v1.md)을 참고하세요 |
| `web/` | 웹 콘솔. [프런트엔드 규칙](./web/AGENTS.md)을 참고하세요 |
| [electron/](./electron/README.md) | 데스크톱 래퍼와 패키징 |

기여하기 전에 [AGENTS.md](./AGENTS.md)를 읽어 주세요. 변경에 맞는 검사를 실행합니다. Go 모듈은 `make test`, 프런트엔드 변경은 `web/`에서 `bun run typecheck`, `bun run lint`, `bun run test`, `bun run build`를 실행합니다. RelayKit 변경은 `relaykit/`에서 `GOWORK=off go build ./...`도 통과해야 합니다.

<a id="documentation"></a>

## 문서와 커뮤니티

| 리소스 | 링크 |
| --- | --- |
| 공식 문서 | [가이드](https://docs.newapi.ai/en/docs) · [설치](https://docs.newapi.ai/en/docs/installation) · [API 레퍼런스](https://docs.newapi.ai/en/docs/api) |
| 프로젝트 탐색 | [DeepWiki](https://deepwiki.com/QuantumNous/new-api) |
| 질문과 토론 | [FAQ](https://docs.newapi.ai/en/docs/support/faq) · [커뮤니티](https://docs.newapi.ai/en/docs/support/community-interaction) |
| 버그와 기능 요청 | [GitHub Issues](https://github.com/QuantumNous/new-api/issues) |
| 보안 신고 | [보안 정책](./.github/SECURITY.md)에 따라 비공개로 신고하세요 |

버그를 신고할 때는 버전, 배포 방식, 재현 단계, 민감 정보를 가린 로그를 포함해 주세요. 문서, 번역, 공급자 연동, 범위를 좁힌 회귀 테스트 등 모든 기여를 환영합니다.

---

## 🔗 관련 프로젝트

### 업스트림 프로젝트

| 프로젝트 | 설명 |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | 원본 프로젝트 베이스 |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourney 인터페이스 지원 |

### 보조 도구

| 프로젝트 | 설명 |
|------|------|
| [new-api-key-tool](https://github.com/Calcium-Ion/new-api-key-tool) | 키 사용량 쿼터 조회 도구 |
| [new-api-horizon](https://github.com/Calcium-Ion/new-api-horizon) | New API 고성능 최적화 버전 |

---

## 📜 라이선스

본 프로젝트는 [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE) 라이선스 하에 배포됩니다.

AGPLv3 제7조에 따른 추가 조건이 적용됩니다. 수정 버전은 적절한 법적 고지와, 사용자 인터페이스가 보여 주는 눈에 띄는 소개·법적 고지·푸터·저작자 표시 위치에 저작자 표시 문구 `Frontend design and development by New API
contributors.`를 유지해야 합니다.

사용자 인터페이스를 제공하는 수정 버전은 원본 프로젝트로 가는 보이는 링크도 유지해야 합니다: <https://github.com/QuantumNous/new-api>.

본 프로젝트는 [One API](https://github.com/songquanpeng/one-api) (MIT 라이선스)를 기반으로 개발된 오픈소스 프로젝트입니다.

귀하의 조직 정책상 AGPLv3 라이선스 소프트웨어 사용이 허용되지 않거나, AGPLv3의 오픈소스 의무를 회피하고 싶은 경우 문의해 주세요: [support@quantumnous.com](mailto:support@quantumnous.com)

저작자 표시와 의존성 고지는 [NOTICE](./NOTICE)와 [서드파티 라이선스](./THIRD-PARTY-LICENSES.md)를 참고하세요.

---

## 🌟 스타 히스토리

<div align="center">

[![스타 히스토리 차트](https://api.star-history.com/svg?repos=Calcium-Ion/new-api&type=Date)](https://star-history.com/#Calcium-Ion/new-api&Date)

</div>

---

<div align="center">

### 💖 New API를 사용해 주셔서 감사합니다

이 프로젝트가 도움이 되셨다면 ⭐️ 스타를 눌러주세요!

**[공식 문서](https://docs.newapi.ai/en/docs)** • **[이슈 피드백](https://github.com/Calcium-Ion/new-api/issues)** • **[최신 릴리스](https://github.com/Calcium-Ion/new-api/releases)**

<sub>❤️로 제작된 QuantumNous</sub>

</div>
