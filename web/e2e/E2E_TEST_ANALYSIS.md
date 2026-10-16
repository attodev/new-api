# E2E 테스트 분석 (Organization + 한국어 스모크)

> 대상 파일: [`organization.e2e.ts`](organization.e2e.ts), [`korean.e2e.ts`](korean.e2e.ts)
> 설정 파일: [`playwright.config.ts`](../playwright.config.ts)
> 작성일: 2026-10-16 (team `web/default/e2e/E2E_TEST_ANALYSIS.md`의 4·5·8절을 develop 테스트 기준으로 다시 씀)
>
> 테스트별 서술과 실행 방법은 [`README.md`](README.md)에 있다. 이 문서는 커버리지 매트릭스, 역할-권한 매트릭스, 검증 갭만 다룬다.

develop 기준 **20+1개**: `organization.e2e.ts` 20개(setup 1 + 기능 18 + teardown 1, serial)와 `korean.e2e.ts` 1개(K1). team(19개)과 다른 점:

- **localStorage 인증 주입이 없다.** upstream 인증은 access token을 메모리에만 두고 HttpOnly refresh 쿠키로 복원하므로, `signInWithUi`가 UI로 로그인하고 `openAuthenticatedPage`가 `/sign-in`으로 튕기면 저장된 자격 증명으로 UI 재로그인한다(team의 `restoreAuth` 없음). 기본 경로는 `/`가 아니라 `/dashboard`다.
- **멤버 추가는 조회-추가 플로우다.** 미소속 사용자 목록이 없고, "Username or email" 입력 → "Look up user"로 정확 조회 → 결과 한 줄 확인 → "Add".
- 새 테스트: T8 멤버 정지·재개(조직 범위 `member_status`), K1 한국어 스모크.

---

## 4. 테스트 커버리지 매트릭스

| #   | 테스트                                                                | 역할(Page)                             | 대상 경로                                                            | 검증 분류           | 핵심 단언                                                                                                                                                                      |
| --- | --------------------------------------------------------------------- | -------------------------------------- | -------------------------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | admin provisions organization, users, and roles via UI                | root→owner→admin→member                | `/users`·`/organization`                                             | **쓰기(생성)·셋업** | root가 3계정+조직 생성("organization created"), owner가 조회-추가로 멤버 2명 추가("organization member assigned"), Role 셀렉터로 org admin 승격("changes saved"), 4역할 로그인 |
| 2   | root admin can see organization management navigation and pages       | root                                   | `/dashboard`→`/organization/dashboard`→`/organization/subscriptions` | 권한(긍정)·라우팅   | 사이드바 5개 링크(Users/Dashboard/Usage Logs/Task Logs/Subscription), 클릭 후 URL·heading                                                                                      |
| 3   | organization selection survives navigation between organization pages | root                                   | dashboard↔`usage-logs/common`                                        | 상태 보존           | 첫 combobox 선택값 유지 (선택값 없으면 skip)                                                                                                                                   |
| 4   | organization owner and admin can access organization management pages | owner, admin                           | `/dashboard`→`/organization`→`/organization/subscriptions`           | 권한(긍정)          | Users·Subscription 링크, URL·heading (2역할 순회)                                                                                                                              |
| 5   | organization usage logs render for admins                             | root                                   | `/organization/usage-logs/common`                                    | 렌더링              | "Usage logs"·Search 버튼·"Rows per page"                                                                                                                                       |
| 6   | organization member cannot open management pages directly             | member                                 | dashboard·usage-logs·subscriptions                                   | **권한(부정)**      | Users/Dashboard/Subscription 링크 0개, 직접 URL → `/403`·`/sign-in`                                                                                                            |
| 7   | organization users table renders members for admin                    | admin                                  | `/organization`                                                      | 렌더링              | heading, Username/Role/Status/Organization note 컬럼 헤더                                                                                                                      |
| 8   | admin suspends and resumes a member (organization member status)      | admin                                  | `/organization`                                                      | **쓰기(상태 변경)** | Active → Suspend → "Pending suspension" → Save → Suspended → Resume → Save → Active                                                                                            |
| 9   | organization users search filters table results                       | admin                                  | `/organization`                                                      | 상호작용            | 무매칭 검색어 → "No data"                                                                                                                                                      |
| 10  | export button is visible for admin and triggers download              | admin                                  | `/organization`                                                      | 상호작용·다운로드   | `org-users*.xlsx` 다운로드 발생                                                                                                                                                |
| 11  | import button is visible for admin and opens dialog                   | admin                                  | `/organization`                                                      | 상호작용·모달       | dialog 열림 → Escape로 닫힘                                                                                                                                                    |
| 12  | add member section is visible for admin but not for member            | admin, member                          | `/organization`                                                      | 권한(긍정+부정)     | admin: "Username or email" 입력란, 없는 사용자 조회 → "User not found" / member: `/403`·`/sign-in`                                                                             |
| 13  | owner sees all controls including role selector in table              | owner                                  | `/organization`                                                      | 권한(긍정)·렌더링   | heading·Role 컬럼 헤더                                                                                                                                                         |
| 14  | dashboard preset buttons change the chart range                       | admin                                  | `/organization/dashboard`                                            | 상호작용            | Today/7d/30d 클릭 후 "error" 텍스트 없음 (버튼이 안 보이면 건너뜀)                                                                                                             |
| 15  | subscription page renders plan configuration form for admin           | admin                                  | `/organization/subscriptions`                                        | 렌더링              | heading, Plan configuration·Plan title·Create                                                                                                                                  |
| 16  | subscription page create plan validates empty title                   | admin                                  | `/organization/subscriptions`                                        | 폼 유효성           | 빈 제목 Create → "Plan title is required"                                                                                                                                      |
| 17  | admin creates an organization subscription plan                       | admin                                  | `/organization/subscriptions`                                        | **쓰기(생성)**      | "Organization plan saved" + Organization plans 표에 제목                                                                                                                       |
| 18  | admin assigns the organization subscription plan to a member          | admin                                  | `/organization/subscriptions`                                        | **쓰기(할당)**      | "Organization plan assigned" + 멤버 표에 플랜 셀                                                                                                                               |
| 19  | root admin can open organization users page without org_id error      | root                                   | `/organization`                                                      | 권한(긍정)·회귀     | 조직 `<select>` 선택 후 "organization_id is required" 없음, owner 행 표시                                                                                                      |
| 20  | admin removes organization members, organization, and users via UI    | owner(멤버 제거), root(조직·계정 삭제) | `/organization`·`/users`                                             | **쓰기(삭제)·정리** | owner가 멤버 2명 제거("changes saved") → root가 조직 삭제(카드 사라짐) → root가 계정 3개 삭제(step-up 인증, "user deleted successfully"), best-effort                          |
| K1  | a ko-KR browser sees the sign-in page and the console in Korean       | root                                   | `/sign-in`→`/dashboard`                                              | 렌더링(i18n)·로그인 | `로그인` 버튼, 한국어 placeholder로 로그인 성공, 사이드바 `대시보드`                                                                                                           |

### 검증 분류 분포

| 분류                 | 테스트              | 비고                                 |
| -------------------- | ------------------- | ------------------------------------ |
| 쓰기(생성/변경/삭제) | 1, 8, 17, 18, 20    | 계정·조직·멤버십·회원 상태·구독 플랜 |
| 권한(긍정)           | 2, 4, 12, 13, 19    | root/owner/admin                     |
| 권한(부정)           | 6, 12               | member `/403`·`/sign-in`             |
| 렌더링               | 2, 5, 7, 13, 15, K1 | heading·컬럼·폼·한국어 라벨          |
| 상호작용             | 9, 10, 11, 14       | 검색·export·import·프리셋            |
| 폼 유효성            | 16                  | 빈 제목 차단                         |
| 상태 보존            | 3                   | 조직 선택 유지                       |

---

## 5. 역할-권한 매트릭스 (테스트로 검증된 것)

| 기능                              | root              | owner                     | org admin                          | member                    |
| --------------------------------- | ----------------- | ------------------------- | ---------------------------------- | ------------------------- |
| 계정·조직 생성 (UI)               | ✅(T1)            | —                         | —                                  | —                         |
| 멤버 조회-추가                    | —                 | ✅(T1, 실제 추가)         | ✅(T12, 입력란·"User not found"만) | ❌ 페이지 차단(T12)       |
| Role 셀렉터로 org admin 승격      | —                 | ✅(T1)                    | —                                  | —                         |
| 멤버 정지·재개 (`member_status`)  | —                 | —                         | ✅(T8)                             | —                         |
| 조직에서 멤버 제거                | —                 | ✅(T20)                   | —                                  | —                         |
| 조직·계정 삭제 (UI)               | ✅(T20)           | —                         | —                                  | —                         |
| 사이드바 Organization 메뉴        | ✅ 5개(T2)        | ✅ Users·Subscription(T4) | ✅ Users·Subscription(T4)          | ❌ 없음(T6)               |
| `/organization` (Users)           | ✅ 조직 선택(T19) | ✅(T4, 13)                | ✅(T4, 7–12)                       | ❌ `/403`·`/sign-in`(T12) |
| `/organization/dashboard`         | ✅(T2, 3)         | —                         | ✅(T14)                            | ❌(T6)                    |
| `/organization/usage-logs/common` | ✅(T3, 5)         | —                         | —                                  | ❌(T6)                    |
| `/organization/subscriptions`     | ✅(T2)            | ✅(T4)                    | ✅(T4, 15–18)                      | ❌(T6)                    |
| Role 컬럼 노출                    | —                 | ✅(T13)                   | ✅(T7)                             | —                         |
| Export / Import                   | —                 | —                         | ✅(T10, 11)                        | —                         |
| 구독 플랜 생성·멤버 할당          | —                 | —                         | ✅(T17, 18)                        | —                         |
| 한국어 로그인·콘솔                | ✅(K1)            | —                         | —                                  | —                         |

`—` = 해당 역할로 명시적 검증이 없는 조합(커버리지 갭 후보).

---

## 8. 커버리지 갭 / 개선 제안

현재 테스트는 **권한 게이트 + 화면 렌더링 스모크 + 핵심 쓰기 경로**까지 덮는다. 다음은 미검증이다.

### team 대비 새로 커버되는 영역

- **조직 회원 상태:** T8이 org admin의 정지 → 저장 → 재개 → 저장을 화면 상태(Active/Pending suspension/Suspended)로 확인한다.
- **한국어 UI:** K1이 ko-KR 브라우저의 로그인 화면과 로그인 후 콘솔 사이드바를 한국어 라벨로 확인한다.
- **조회-추가 실패 경로:** T12가 없는 사용자 조회 시 "User not found"를 확인한다.

### 기능 갭

- **정지의 실제 효과:** T8은 표의 상태 표시만 본다. 정지된 멤버의 로그인·조직 기능·relay 사용이 막히는지는 검증하지 않는다.
- **구독 할당 결과:** T18은 멤버 표에 플랜 셀이 보이는지만 확인한다. 쿼터 차감·기간 만료·구독 취소는 없다.
- **멤버 추가/승격 결과:** T1은 토스트("organization member assigned", "changes saved")까지만 단언하고, 표에 반영된 최종 역할·멤버 수는 단언하지 않는다.
- **Import 업로드 / Export 내용:** T11은 다이얼로그 열림·닫힘, T10은 다운로드 발생만 본다.
- **Task Logs 페이지(`/organization/usage-logs/task`):** T2가 사이드바 링크만 확인하고 페이지 렌더 테스트는 없다.
- **대시보드 차트 데이터:** T14는 "error" 텍스트 부재만 보고, 프리셋 버튼이 안 보이면 아무것도 하지 않는다.
- **조직 선택 유지:** T3은 선택값이 비어 있으면 skip한다.
- **한국어 범위:** K1은 로그인 화면과 사이드바 `대시보드`만 본다. 조직 화면의 한국어 문구는 E2E가 없고, `organization.e2e.ts`는 영문 locale을 전제로 한다(예: `promoteMemberToAdminViaUi`의 `admin` 옵션).
- **E2E 밖:** `/wallet` 화면, 구독 취소, 실제 relay 과금(조직 테스트 가이드의 수동 시나리오로 확인).

### 역할 조합 갭 (§5의 `—`)

- owner의 dashboard·usage-logs 접근, org admin의 usage-logs 접근.
- owner의 정지·재개, owner의 Export/Import.
- "owner는 가능하지만 org admin은 불가" 경계(org admin이 Role을 바꿀 수 없음 등)를 부정 단언하는 테스트가 없다.
- member의 `/organization/usage-logs/task`(Task Logs) 직접 접근 차단.

### 구조 개선

- E2E는 조직 도메인과 한국어 스모크뿐이다. 인증·토큰·채널 관리 등 다른 핵심 플로우는 E2E가 없다.
- `serial` + 공유 페이지 구조라 한 테스트가 실패하면 이후 테스트(teardown T20 포함)가 실행되지 않는다. run-scoped 이름(`runId`)으로 다음 실행과 충돌은 피하지만 잔여물은 남는다.
