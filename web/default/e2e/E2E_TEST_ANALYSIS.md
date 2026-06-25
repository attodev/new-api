# E2E 테스트 분석 (Organization)

> 대상 파일: [`web/default/e2e/organization.e2e.ts`](organization.e2e.ts)
> 설정 파일: [`web/default/playwright.config.ts`](../playwright.config.ts)
> 작성일: 2026-06-24
>
> 본 문서는 테스트별 서술 문서([`organization.e2e.md`](organization.e2e.md))를 보완하는 **분석 문서**다.
> 커버리지 매트릭스, 역할-권한 매트릭스, 테스트 분류, 검증 갭을 다룬다.

---

## 1. 한눈에 보기

| 항목 | 내용 |
|---|---|
| 테스트 프레임워크 | Playwright (`@playwright/test`) |
| E2E 파일 수 | **1개** (`organization.e2e.ts`) |
| 테스트 케이스 수 | **19개** (프로비저닝 setup 1 + 기능 17 + 정리 teardown 1) |
| 실행 모드 | `serial` (describe 단위 순차 실행) |
| 병렬 실행 | `fullyParallel: false` |
| 브라우저 | 시스템 Chromium (`/usr/bin/chromium-browser`), 1개 프로젝트 |
| 테스트 대상 도메인 | **조직(Organization) 기능 전반** — 그 외 도메인 E2E 없음 |
| 검증 성격 | **스모크 테스트 / 권한·렌더링 검증** (비즈니스 로직 깊은 검증 아님) |
| 타임아웃 | 전역 30s, expect 10s, describe 60s, beforeAll 120s |
| CI 재시도 | CI 환경에서 1회 |

테스트 대상은 **조직 기능 한정**이다. 릴레이/빌링/채널/사용자관리 등 백엔드 핵심 도메인에 대한 E2E는 현재 존재하지 않으며, 해당 영역은 Go 단위/통합 테스트(`*_test.go`)가 담당한다.

이 스위트는 더 이상 사전 시드된 조직 계정에 의존하지 않는다. root(admin) 계정만 시드 기본값(`E2E_ADMIN_USERNAME` / `E2E_ADMIN_PASSWORD`)을 쓰고, owner / org admin / member 계정과 테스트 조직은 setup 테스트(#1)에서 **UI로 직접 생성(self-provision)** 하며 teardown 테스트(#19)에서 삭제한다. 모든 런타임 리소스는 `runId = Date.now().toString(36)` 기반 고유 이름(`e2e_owner_<runId>` / `e2e_admin_<runId>` / `e2e_member_<runId>`, `E2E Org <runId>`)을 사용해 실행 간 충돌을 방지한다.

---

## 2. 실행 아키텍처

```
playwright.config.ts
  webServer[0]  go run . --port <backendPort>   (rate limit 3종 비활성화)
  webServer[1]  bun run dev (프론트엔드, VITE_REACT_APP_SERVER_URL=backend)
        │  reuseExistingServer: true → 이미 떠 있으면 재사용
        ▼
organization.e2e.ts  beforeAll  (root만 로그인)
  ├── contexts[0] → rootPage               (admin / 루트 관리자)  ← 로그인
  ├── contexts[1] → ownerPage              (조직 owner)   ← 컨텍스트만 생성
  ├── contexts[2] → organizationAdminPage  (조직 admin)   ← 컨텍스트만 생성
  └── contexts[3] → memberPage             (조직 member)  ← 컨텍스트만 생성
        │  + addInitScript: TanStack Router/Query devtools 오버레이 버튼 숨김
        ▼
setup 테스트 (#1)  (UI로 런타임 프로비저닝)
  ├── root → e2e_owner_<runId> / e2e_admin_<runId> / e2e_member_<runId> + E2E Org <runId> 생성
  ├── owner 로그인 → 나머지 두 계정 멤버 추가 → 한 명 org admin 승격
  └── org admin / member 로그인
```

핵심 설계 선택:
- **4개 BrowserContext 분리** — 역할별로 쿠키·localStorage가 완전히 격리됨. `beforeAll`에서는 **root만 로그인**하고 나머지 세 컨텍스트는 빈 채로 둔다. owner/admin/member 계정·조직은 setup 테스트(#1)에서 UI로 생성·로그인하며, 이후 17개 기능 테스트가 그 페이지들을 재사용한다.
- **self-provisioning** — 사전 시드 조직 계정에 의존하지 않는다. setup(#1)이 생성하고 teardown(#19)이 삭제한다. teardown은 **owner가 조직에서 멤버 제거(owner/admin만 가능) → root가 조직 삭제(cascade) → root가 계정 삭제** 순으로 진행한다.
- **`serial` 모드** — 테스트 간 공유 페이지(`rootPage` 등)와 setup→기능→teardown 순서 의존성 때문에 순차 실행 필수.
- **`reuseExistingServer: true`** — 로컬에서 이미 띄운 백엔드/프론트를 재사용. 단, 이 경우 config의 `env`(rate limit off)가 **적용되지 않으므로** 수동 기동 시 직접 꺼야 한다(§6 참조).

---

## 3. 인증 헬퍼 분석

세 개의 헬퍼가 인증 상태 유지의 핵심이다.

| 헬퍼 | 역할 | 핵심 메커니즘 |
|---|---|---|
| `signInWithUi` | UI 로그인 + 인증 상태 저장 | `/api/user/login` 응답을 가로채 `success` 검증 → localStorage(`user`,`uid`)를 `authStorage` WeakMap + `context.addInitScript`에 저장 |
| `restoreAuth` | localStorage 복원 | 페이지의 localStorage에 저장된 `user`/`uid` 재주입 |
| `openAuthenticatedPage` | **모든 페이지 이동의 표준 진입점** | 이동 전 복원 → goto → `networkidle` 대기 → sign-in 리디렉트 시 복원 후 재시도, 그래도 `/sign-in`이면 페이지별 저장 자격증명으로 **UI 전체 재로그인** |

### 왜 `openAuthenticatedPage`를 반드시 써야 하는가

- TanStack Router `_authenticated` 레이아웃은 `beforeLoad`에서 `getSelf()`(`/api/user/self`)를 호출해 세션을 재검증한다.
- `page.goto()`는 항상 **완전한 페이지 리로드**를 유발 → 모듈 수준 `sessionVerified` 플래그가 리셋 → 매 이동마다 `getSelf()` 재호출.
- 이 호출이 rate limit에 걸리면 sign-in으로 리디렉트되어 테스트가 깨진다.
- `openAuthenticatedPage`는 (1) 이동 전 localStorage 선주입, (2) `networkidle`로 `getSelf()` 완료 대기, (3) 실패 시 복원·재시도로 이 레이스를 흡수한다.

> **테스트 작성 규칙:** 신규 테스트에서도 `page.goto()` 직접 호출 대신 `openAuthenticatedPage()`를 사용해야 한다. (단, member의 차단 검증처럼 *의도적으로* 리디렉트를 확인하는 경우는 `page.goto()` 직접 사용 — 테스트 6, 11 참조)

---

## 4. 테스트 커버리지 매트릭스

| # | 테스트 | 역할(Page) | 대상 경로 | 검증 분류 | 핵심 단언 |
|---|---|---|---|---|---|
| 1 | **프로비저닝 setup** | root→owner→admin→member | 사용자 관리·`/organization` | **쓰기(생성)·셋업** | UI로 3계정+조직 생성, 멤버 추가·org admin 승격, 4역할 로그인 완료 |
| 2 | nav & pages 노출 | root | `/`→dashboard→subscriptions | 권한(긍정)·라우팅 | 사이드바 5개 메뉴, heading·URL |
| 3 | 조직 선택 유지 | root | dashboard↔usage-logs | 상태 보존 | combobox 선택값 유지 (조직 없으면 skip) |
| 4 | owner/admin 접근 | owner, admin | `/`→`/organization`→subscriptions | 권한(긍정) | 메뉴·heading·URL (2역할 순회) |
| 5 | usage logs 렌더 | root | usage-logs/common | 렌더링 | Usage logs 텍스트·Search·Rows per page |
| 6 | member 차단 | member | dashboard/usage-logs/subscriptions | **권한(부정)** | 메뉴 0개, `/403`·`/sign-in` 리디렉트 |
| 7 | users 테이블 구조 | admin | `/organization` | 렌더링 | Username/Role/Status 컬럼헤더 |
| 8 | 사용자 검색 | admin | `/organization` | 상호작용 | 무매칭 검색어 → "No data" |
| 9 | export 다운로드 | admin | `/organization` | 상호작용·다운로드 | `org-users*.xlsx` 다운로드 발생 |
| 10 | import 다이얼로그 | admin | `/organization` | 상호작용·모달 | dialog 열림 → Escape로 닫힘 |
| 11 | 멤버 추가 가시성 | admin, member | `/organization` | 권한(긍정+부정) | admin Username 입력 보임 / member 리디렉트 |
| 12 | owner 컨트롤 | owner | `/organization` | 권한(긍정) | heading·Role 컬럼헤더 |
| 13 | 대시보드 프리셋 | admin | `/organization/dashboard` | 상호작용 | Today/7d/30d 클릭 시 에러 토스트 없음 |
| 14 | 구독 폼 렌더 | admin | `/organization/subscriptions` | 렌더링 | Plan configuration·Plan title·Create |
| 15 | 구독 폼 유효성 | admin | `/organization/subscriptions` | 폼 유효성 | 빈 제목 Create → "Plan title is required" |
| 16 | 구독 플랜 생성 | admin | `/organization/subscriptions` | **쓰기(생성)** | 플랜 생성 → "Organization plan saved" + 플랜 테이블에 노출 |
| 17 | 구독 플랜 할당 | admin | `/organization/subscriptions` | **쓰기(할당)** | 멤버에게 플랜 할당 → "Organization plan assigned" + 구독 테이블 행 |
| 18 | root 조직 사용자 로딩 (회귀) | root | `/organization` | 권한(긍정)·회귀 | 조직 선택 후 "organization_id is required" 오류 없이 멤버 로드 |
| 19 | **정리 teardown** | owner(멤버 제거), root(조직·계정 삭제) | `/organization`·사용자 관리 | **쓰기(삭제)·정리** | owner가 멤버 2명 제거 → root가 조직 삭제(cascade) → root가 계정 3개 삭제 (best-effort) |

### 검증 분류 분포

| 분류 | 테스트 수 | 비고 |
|---|---|---|
| 쓰기(생성/삭제) — 셋업·정리·구독 | 1, 16, 17, 19 | UI로 계정·조직·구독 플랜 생성/할당/삭제 |
| 권한(긍정) — 접근 가능 확인 | 2, 4, 11, 12, 18 | root/owner/admin (18=root 조직 사용자 로딩 회귀) |
| 권한(부정) — 접근 차단 확인 | 6, 11 | member 403/sign-in |
| 렌더링 — 요소 존재 확인 | 2, 5, 7, 14 | heading·컬럼·폼 |
| 상호작용 — 클릭/입력/다운로드 | 8, 9, 10, 13 | 검색·export·import·프리셋 |
| 폼 유효성 | 15 | 빈 입력 차단 |
| 상태 보존 | 3 | 조직 선택 유지 |

---

## 5. 역할-권한 매트릭스 (테스트로 검증된 것)

| 기능 | root | owner | org admin | member |
|---|---|---|---|---|
| 계정·조직 생성 (UI) | ✅(T1) | ✅(T1, 멤버 추가·승격) | — | — |
| 계정·조직 삭제 (UI) | ✅(T19) | — | — | — |
| 사이드바 Organization 메뉴 노출 | ✅(T2) | ✅(T4) | ✅(T4) | ❌ 없음(T6) |
| `/organization` (Users) 접근 | ✅(T18, 조직 선택) | ✅(T4) | ✅(T4,7) | ❌ 403/sign-in(T6,11) |
| `/organization/dashboard` 접근 | ✅(T2) | — | ✅(T13) | ❌(T6) |
| `/organization/usage-logs/common` 접근 | ✅(T5) | — | — | ❌(T6) |
| `/organization/subscriptions` 접근 | ✅(T2) | ✅(T4) | ✅(T4,14,15) | ❌(T6) |
| 멤버 추가(Username 입력) | — | ✅(T1) | ✅(T11) | ❌(T11) |
| 조직에서 멤버 제거 | — | ✅(T19) | — | — |
| 멤버 → org admin 승격 | — | ✅(T1) | — | — |
| Role 컬럼/셀렉터 노출 | — | ✅(T12) | ✅(T7) | — |
| Export / Import | — | — | ✅(T9,10) | — |
| 구독 플랜 생성·멤버 할당 | — | — | ✅(T16,17) | — |

`—` = 해당 역할로 명시적 검증이 없는 조합(= 커버리지 갭 후보).

---

## 6. 실행 방법

```bash
# 1) 백엔드 수동 기동 시 — rate limit 3종 반드시 비활성화
SQL_DSN="postgresql://..." \
  CRITICAL_RATE_LIMIT_ENABLE=false \
  GLOBAL_API_RATE_LIMIT_ENABLE=false \
  GLOBAL_WEB_RATE_LIMIT_ENABLE=false \
  ./new-api --port 3000

# 2) 테스트 실행
cd web/default
E2E_BACKEND_URL=http://127.0.0.1:3000 npx playwright test e2e/organization.e2e.ts
# 또는 package.json 스크립트
bun run e2e          # playwright test
bun run e2e:headed   # 브라우저 표시
bun run e2e:install  # chromium 설치
```

### 환경 변수

| 변수 | 기본값 | 설명 |
|---|---|---|
| `E2E_ADMIN_USERNAME` | `admin` | 루트 관리자 (DB에 선존재해야 함) |
| `E2E_ADMIN_PASSWORD` | `atto1234` | 공통 비밀번호 (생성되는 모든 계정에 재사용) |
| `E2E_BACKEND_URL` | `http://127.0.0.1:3100` | 백엔드 URL |
| `E2E_BASE_URL` | `http://127.0.0.1:3101` | 프론트 URL (webServer) |
| `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` | `/usr/bin/chromium-browser` | Chromium 경로 재정의 |

> **admin-only 시딩:** 이전의 `E2E_ORGANIZATION_OWNER_USERNAME` / `E2E_ORGANIZATION_ADMIN_USERNAME` / `E2E_ORGANIZATION_MEMBER_USERNAME` / `E2E_ORGANIZATION_PASSWORD`는 **제거되었다**. root(admin) 계정만 시드 기본값을 쓰며, owner/admin/member 계정과 조직은 setup 테스트가 UI로 런타임 생성한다(`e2e_*_<runId>`, `E2E Org <runId>`, admin 비밀번호 공유).

---

## 7. 알려진 제약사항

1. **Rate limit 비활성화 필수** — `/api/user/self`가 `GLOBAL_API_RATE_LIMIT`(기본 180 req/3분, `middleware.GlobalAPIRateLimit()`)에 걸리면 테스트 중 sign-in 리디렉트 발생. config `webServer.env`에 설정돼 있으나 `reuseExistingServer: true`로 기존 서버를 재사용하면 적용 안 됨 → 수동 기동 시 직접 끌 것.
2. **Snap Chromium (Ubuntu 26.04+)** — 내장 Chromium 미지원으로 시스템 Chromium 사용. ffmpeg 미지원 → `video: 'off'`.
3. **TanStack Router 세션 재검증** — `page.goto()`가 매번 `getSelf()`를 재호출하므로 페이지 이동은 반드시 `openAuthenticatedPage()`를 경유.
4. **admin 계정만 선존재 필요** — root(admin) 계정만 DB에 선존재하면 된다(`E2E_ADMIN_USERNAME`/`E2E_ADMIN_PASSWORD`). 조직·역할 계정은 setup 테스트가 UI로 self-provision하므로 더 이상 외부 시드에 의존하지 않는다.
5. **teardown 삭제 순서 (백엔드 제약)** — teardown 테스트(#19)는 반드시 **owner가 아닌 멤버 → 조직 → owner** 순으로 삭제해야 한다. 백엔드는 owner가 아닌 멤버가 남아 있는 조직의 삭제를 거부한다(`model.DeleteOrganization`).
6. **serial 중단 시 잔여물** — serial 모드에서 중간 테스트가 실패하면 이후 테스트가 모두 "did not run"으로 건너뛰어져 **teardown(#19)도 실행되지 않을 수 있다.** 이때 `e2e_*` 계정·조직이 DB에 남지만, run-scoped 고유 이름(`runId`) 덕분에 다음 실행과 충돌하지는 않는다.
7. **프론트엔드 토스트 결함 우회** — `handleDeleteOrganization`은 API가 `success:false`를 200으로 반환해도 "Organization deleted" 토스트를 띄운다. 그래서 `deleteOrganizationViaUi`는 토스트가 아니라 **카드 사라짐**(`toHaveCount(0)`)으로 삭제를 단언한다. (프론트엔드 결함 자체 수정은 별도 작업.)

### 검증 결과 (2026-06-24)
빈 SQLite로 rate limit을 끄고 admin만 시드한 격리 백엔드에서 **16/16 통과**, 종료 후 API 조회로 `e2e_*` 사용자 0개·조직 0개를 확인하여 **자원이 완전히 정리됨**을 검증했다.

---

## 8. 커버리지 갭 / 개선 제안

현재 테스트는 **권한 게이트 + 화면 렌더링 스모크**에 충실하나, 다음은 미검증 영역이다.

### 이제 커버되는 영역 (이전 갭)
- **멤버 추가·역할 변경:** setup 테스트(#1)가 owner로 멤버 추가 및 org admin 승격을 UI로 실제 수행한다(이전엔 T10/T11이 *가시성*만 확인).
- **조직·사용자 생성/삭제:** setup(#1)이 계정 3개와 조직을 UI로 생성하고, teardown(#19)이 멤버 → 조직 → owner 순으로 삭제한다 — 실제 mutation 경로가 검증된다.
- **구독 플랜 생성 성공 경로 + 멤버 할당:** T16이 플랜을 실제 생성("Organization plan saved" + 플랜 테이블 노출), T17이 멤버에게 할당("Organization plan assigned" + 구독 테이블 행)한다. 플랜·구독은 조직 삭제 시 cascade로 정리된다.

### 기능 갭
- **구독 할당 결과 단언 약함:** T17은 할당 후 구독 테이블에 행이 보이는지만 확인하며, 쿼터 차감·기간 만료 등 구독 동작 자체는 검증하지 않음.
- **멤버 추가/승격 결과 단언 약함:** setup(#1)은 흐름을 수행하지만, 테이블에 반영된 최종 역할·멤버 수를 깊게 단언하지는 않는다(셋업 목적의 best-effort 성격).
- **Import 실제 업로드 미검증:** T9는 다이얼로그 *열림/닫힘*만. 파일 업로드·파싱·결과 미검증.
- **Export 내용 미검증:** T8은 다운로드 *발생*만. 파일 내용/행 수 미검증.
- **Task Logs 페이지:** 사이드바 링크 존재(T1)만 확인, 페이지 자체 렌더 테스트 없음.
- **대시보드 차트 데이터:** T12는 에러 토스트 부재만 확인, 실제 데이터/차트 렌더 미검증.

### 역할 조합 갭 (§5의 `—`)
- owner의 dashboard/usage-logs 접근, admin의 Role *셀렉터 실제 변경* 동작 등 미검증.
- "owner는 가능하지만 admin은 불가" 같은 owner↔admin 권한 경계 테스트 부재.

### 구조 개선
- E2E가 단일 도메인(조직)에만 존재 → 인증/로그인, 토큰/채널 관리 등 다른 프론트 핵심 플로우는 E2E 공백.
- 테스트 데이터 셋업을 fixture/seed 스크립트로 코드화하면 환경 재현성 향상.
- `serial` + 공유 페이지 구조상 한 테스트 실패가 후속에 영향 가능 → 독립성 높은 테스트는 별도 context로 분리 고려.

---

## 9. 부록 — 파일 인덱스

| 파일 | 설명 |
|---|---|
| [`organization.e2e.ts`](organization.e2e.ts) | 테스트 본문 (19 케이스: setup 1 + 기능 17 + teardown 1) |
| [`organization.e2e.md`](organization.e2e.md) | 테스트별 상세 서술 문서 |
| [`playwright.config.ts`](../playwright.config.ts) | Playwright 설정 (webServer, projects) |
| `package.json` | `e2e`, `e2e:headed`, `e2e:install` 스크립트 |
