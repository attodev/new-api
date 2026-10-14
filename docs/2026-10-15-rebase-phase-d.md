# upstream 재기반 단계 D(O9 한글·O8 인터페이스 언어·메시지 경로·단계 C 이월) 기록

브랜치: develop. 베이스: 4406ef559(단계 D 계획 커밋). 계획: docs/superpowers/plans/2026-10-15-rebase-phase-d.md. spec: §4.8, §2.1 O8·O9, §8 단계 D, §9 결정 4·8–14.

커밋은 `git log --reverse --format='%h %s' 4406ef559..HEAD` 기준이고, Task 번호는 ledger(`.superpowers/sdd/2026-10-15-rebase-phase-d/progress.md`)와 맞췄다. upstream 파일의 훅 위치는 모두 `docs/fork-overlays.md`의 `O9`·`O8`·`O3`·`O6`·`O7` 행에 있다.

## 1. 프런트 ko locale (Task 1–8)

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | 7827adc7a | feat(i18n): seed the Korean locale from team and add the batch validation tool |
| 1 | 33557c594 | fix(i18n): guard the Korean seed, align the glossary with seeded usage, and tighten validation |
| 1 | f817594ae | docs(i18n): settle Quota as 쿼터 and document the manual skip list |
| 2 | 0389c8616 | feat(web): wire the Korean interface language (ko) into i18next, detection and calendars |
| 2 | 43cd2bf9f | fix(web): normalize the saved interface language at login and tighten the Korean match |
| 3 | 3ed7b4f30 | feat(i18n): translate Korean locale batch 1/6 |
| 3 | 7ffca1e21 | fix(i18n): polish Korean batch 1 after review |
| 4 | 072e9fccc | feat(i18n): translate Korean locale batch 2/6 |
| 4 | b0cab3d00 | fix(i18n): polish Korean batch 2 after review |
| 5 | c6bae02d1 | feat(i18n): translate Korean locale batch 3/6 |
| 5 | 21a814290 | fix(i18n): polish Korean batch 3 after review |
| 6 | 85f1bd9da | feat(i18n): translate Korean locale batch 4/6 |
| 6 | d8df5756e | fix(i18n): polish Korean batch 4 after review |
| 7 | 79009f365 | feat(i18n): translate Korean locale batch 5/6 |
| 7 | 79f2a4944 | fix(i18n): polish Korean batch 5 after review |
| 7 | e92cb7fb4 | docs(i18n): settle Tier as 등급 |
| 8 | bb2b45de3 | feat(i18n): translate Korean locale batch 6/6 |
| 8 | 00a48665f | fix(i18n): unify Korean terminology across the locale |
| 8 | 8c07d5ebb | fix(i18n): close the Korean sweep gaps found in review |
| 8 | 58994d4ba | fix(web): label the generated access token with its own key |

### 키 수

`web/src/i18n/locales/ko.json`은 단계 D 끝에 **7,538키**다(en.json과 같은 키·순서). 출처별:

| 구분 | 키 | 설명 |
|---|---|---|
| team 재사용 | 4,412 | `seed`가 team `kr.json`에서 검증(보간·태그·공백·한글 포함)을 통과한 값을 그대로 가져옴. 공백 수선 3건 포함. 일부는 Task 8 통일에서 값이 바뀜 |
| 배치 번역 | 2,873 | 6배치(485×5 + 483) 중 영어 유지 35개를 뺀 수: 479 + 483 + 482 + 473 + 481 + 475 |
| 배치 밖 번역 | 70 | Task 9 fork 메시지 키 66, Task 10 인터페이스 언어 키 3, Task 8 수정 라운드 2의 `Access token` 1 |
| 영어 유지(skip) | 183 | seed 자동 판정 148(숫자·URL·JSON·한 글자·식별자, team과 zh가 모두 영어로 둔 브랜드) + 배치 중 추가 35(브랜드·테마 이름). `docs/i18n/ko-skip.json`과 `ko-locale.mjs`의 `MANUAL_SKIP` |

4,412 + 2,873 + 70 + 183 = 7,538. 한글 값 7,353개, 영어와 같은 값 183개(skip 목록과 정확히 일치), 한글 없이 영어와 다른 값 2개(Gemini 접미사 조각 `HANGUL_OPTIONAL` 1개 포함).

배치별 수정 라운드: 배치 1–5는 리뷰 1라운드씩(14·9·10·18·10키), 배치 5는 용어(Tier) 결정으로 2라운드, 배치 6과 전체 통일(Task 8)은 2라운드(74키, `Access token` 키 분리). Task 1 도구는 2라운드, Task 2 연결은 1라운드.

### 용어 통일 (Task 8)

- 전체 통일 1차: **33항목 / 209키**(스크립트 203 + 화면 확인 6). Quota 할당량→쿼터 29, Reset 재설정→초기화 22, Redemption code→충전 코드 22, Product 제품→상품 17, Provider 공급자→제공자 16, Vendor 공급자→공급업체 13, Tier 티어→등급 12, Check-in 출석→체크인 12, 해요체→합쇼체 11 등.
- 리뷰 수정 1차: **74키**(Provider 계열, "New API" 브랜드 훼손 2건 복구, sk- 자격 증명 `Token`→API 키, 접근 토큰 감사 필터→토큰, 반영어 값 29개, Transfer=전송, 상위→업스트림 등).
- 리뷰 수정 2차: **1키**. 액세스 토큰 대화상자가 공용 `Token`(사용 로그의 API 키 열) 대신 새 키 `Access token`을 쓴다(`access-token-dialog.tsx:46`, 8개 locale).
- 용어집 `docs/translation-glossary.ko.md`: 핵심 용어 154행, "통일 완료" 31행, 문체 규칙(합쇼체 메시지, 명사형 라벨, 보간·태그·공백 보존, 브랜드 그대로).

### 도구와 연결 (Task 1–2)

- `web/scripts/ko-locale.mjs`(`bun run i18n:ko`): `seed [--force]`(ko.json이 있으면 `--force` 없이 거부), `check [--batch N]`(보간 `{{…}}`, Trans·이름 태그, printf, 앞뒤 공백, `\n` 수, 한글 포함), `status`(남은 영어 placeholder). 테스트 `ko-locale.test.mjs` 17개.
- 배치 목록 `docs/i18n/ko-batches.json`은 Task 12에서 지웠다(계획 결정 10). `check`(파일 전체)·`status`는 그 파일을 읽지 않는다. `check --batch N`은 이제 "no batch list"로 종료 코드 2를 낸다. `seed`는 ko.json이 있으므로 여전히 `--force` 없이 거부한다.
- 연결: `config.ts`(`resources.ko`, `supportedLngs`), `languages.ts`(옵션 `한국어`, `kr`/`ko`/`ko-*` → `ko`; `kok`·`kos`는 아님), `sync-i18n.mjs:230`(비라틴 locale 판정), 달력 두 곳(`react-day-picker` `ko`), 로그인 직후 저장된 사용자 언어 정규화(`use-auth-redirect.ts:64`).
- 스크린샷: `docs/i18n/images/`(login, dashboard, tokens, usage-logs, wallet, settings).

## 2. 백엔드 한국어와 메시지 경로 (Task 9)

| Task | 커밋 | 내용 |
|---|---|---|
| 9 | 6db471379 | feat(i18n): add the Korean backend locale and accept legacy kr codes |
| 9 | 42aa9a464 | fix(organization): route fork error messages through the translatable message path |
| 9 | 5a5e1652d | fix(i18n): use API 키 in backend Korean errors and close review gaps |

- `i18n/locales/ko.yaml`(fork 파일): develop `en.yaml`의 **66키** 전부, 같은 순서·주석 구조. team `ko.yaml`에서 19개 재사용, 47개 새로 번역. 수정 라운드에서 team 값 6개의 "토큰"을 sk- 자격 증명 뜻대로 "API 키"로 바꿨다(`token.get_info_failed`, `token.invalid`, `token.not_provided`, `token.status_unavailable`, `distributor.token_no_model_access`, `distributor.token_model_forbidden`). `distributor.invalid_request_parse_model`의 이중 접두사와 `rate_limit.total_reached`의 빠진 문장도 고쳤다.
- `i18n.go`: `LangKo`, 파일 목록, localizer, `normalizeLang`(`kr`, `ko`, `ko-*`; `_`는 `-`로 바뀐 뒤), `SupportedLanguages()`. 기존 `TestLocalesRenderEveryKey`가 ko도 돈다.
- fork 메시지 경로: fork 파일에서 웹 콘솔 사용자에게 나가는 오류를 `common.NewMessage`/`ApiErrorT`(message_key)로 바꿨다. **40곳**(grep 문자열 27 + 상수·`c.JSON` 리터럴 13) + 수정 라운드 1곳(`model/topup_target.go:99` `organization not found`) = **41곳**. 영어 문구는 그대로라 기존 테스트가 바뀌지 않았다. 키 66개를 8개 locale JSON 끝(`Subscription status expired` 뒤)에 넣었다(en=키, ko=번역, 나머지 6개는 영어). `controller/organization_i18n_test.go`가 `forkMessageSources`의 키가 en.json에 있는지 검사한다.
- 내부로 남긴 오류 **75곳**(처음 76, 수정 라운드에서 1곳 전환):
  - `controller/topup_paypal.go` `fmt.Errorf` 21개: 로그 전용, 핸들러는 고정 message_key로 응답.
  - 핸들러가 먼저 검증하거나 id가 인증된 actor·DB 행에서 오는 방어 가드: `model/organization.go`(`quota cannot be negative` ×5, `invalid organization member/parameters/assignment`), `model/organization_member_status.go:73`, `model/organization_subscription.go`(`invalid organization subscription plan/user/assignment`, `transaction is nil`, `invalid organizationUserSubscriptionId`, 갱신·다운그레이드·초기화 인자 가드).
  - 내부 sentinel·재시도 신호: `errOrganizationDeleteOwnerChanged`, `errOrganizationTopUpWithoutTarget`.
  - Excel 가져오기 행 오류에만 들어가는 wrap: `model/organization.go:317, 320`, `service/org_user_export.go:22, 28, 78, 84`(ruling, §5).
  - AI 클라이언트로 가는 relay·과금 오류(OpenAI 형식, 웹 콘솔 아님): `model/organization_subscription.go`의 과금 경로, `service/billing_session_organization.go`, `service/funding_source_organization.go`, `service/midjourney_organization.go`.
- upstream 파일의 같은 패턴(`model/topup.go` 등)은 훅 규칙상 바꾸지 않았다(계획 결정 6).
- 수동 확인(§7 #7, #8): `Accept-Language: ko`(및 레거시 `kr`)로 잘못된 키 relay → `유효하지 않은 API 키입니다`. 조직 거부는 응답 `message`가 영어이고 `message_key`를 웹 콘솔이 ko.json으로 번역해 토스트에 띄운다.

## 3. O8 인터페이스 언어 (Task 10)

| Task | 커밋 | 내용 |
|---|---|---|
| 10 | 71b086eb3 | feat(settings): restrict the interface language menu through the InterfaceLanguages option |

- 옵션 `InterfaceLanguages`(쉼표 코드, 기본 `en,ko`): `common/constants.go:17`, `model/option.go:82, 619-620`. `/api/status`의 `interface_languages`는 `common.ParseInterfaceLanguages`(새 fork 파일)가 만든다: 허용 코드 `en, ko, zhCN, zhTW, fr, ru, ja, vi`, 대소문자 무시, `kr`→`ko`, 관리자 순서 유지, 중복·미지원 제거, 비면 `["en"]`.
- 프런트: `lib/interface-languages.ts`(`filterInterfaceLanguages`, `useInterfaceLanguageOptions`), `status-query.ts`·`system-config-store.ts`(`interfaceLanguages`), `use-update-option.ts`(저장 후 `/api/status` 갱신), 헤더 `language-switcher.tsx`와 프로필 `language-preferences-card.tsx`가 걸러진 목록을 쓴다. 사용자의 현재 언어는 목록에 없어도 남는다. 리소스는 8개 locale을 모두 로드한다.
- 관리 화면: 시스템 설정 → 운영 → 인터페이스 언어(`general/interface-languages-section.tsx`, 체크박스, 하나 이상 필수 — 비우면 "언어를 하나 이상 선택하세요.").
- team의 하드코딩(`[en, kr]`) 대신 옵션으로 만들었다(계획 결정 4).

## 4. README와 단계 C 이월 4건 (Task 11), 병합 리허설

| Task | 커밋 | 내용 |
|---|---|---|
| 11 | 1365c8754 | docs(readme): add the Korean README and link it from every language nav |
| 11 | 555a434ea | feat(billing): refresh stale vendor names and let admins expire abandoned personal top-ups |
| 11 | b0a05076d | fix(usage-logs): compute mobile tokens per second from the precise end-to-end time |
| 11 | 9c0ec8dbb | fix(billing): expire abandoned subscription orders and keep vendor names on a failed vendor query |
| 11 | 6275fd416 | docs(readme): align Korean README terms with the glossary and fix the nav order |

- README: `README.kr.md`(359줄)는 upstream 신판 `README.md`를 절 단위로 옮겼다(team 구판의 같은 절 문장은 재사용; 링크·배지·코드 블록은 원문 그대로, "New API"/"QuantumNous" 출현 수 같음). 6개 README의 nav 14행에 한 줄씩(`<!-- fork: O9 -->`, `日本語` 앞).
- 이월 1 — 벤더명 스냅숏 신선도(단계 C 최종 재리뷰 잔여): 스냅숏이 1분보다 오래되면 `resolveVendorName`이 비차단 재구축(`gopool.Go`, `TryLock`, 시도 창 1분)을 걸고 이전 스냅숏을 바로 돌려준다. 빈 맵 재구축은 이전 스냅숏을 지우지 않는다. 벤더 조회가 실패해 `VendorID`의 이름이 없으면 이전 스냅숏의 이름을 쓴다(수정 라운드).
- 이월 2 — 버려진 개인 주문: 관리자 `POST /api/user/topup/expire`(body `{"user_id": n}` 선택, 없으면 전체). 1시간 지난 pending 개인 충전 주문과 pending 구독 주문(수정 라운드)을 만료하고 `{expired, skipped_recent, expired_subscriptions, skipped_recent_subscriptions}`를 돌려준다. 감사 `topup.expire`, 접근 토큰 `billing:write`. 라우트는 기존 `registerForkAdminUserRoutes` 훅(`router/api-router.go:157`)에 등록해 upstream 새 줄이 없다(ruling, §5). OpenAPI §3.6.
- 이월 3 — 모바일 카드 tokens/s가 `effectiveUseTimeSec`(`e2e_ms` 우선)으로 나눈다(`common-log-mobile-card.tsx:83, 204-205`).
- 이월 4 — 모바일 카드 `showUserInfo` 조건에 `/* fork: O3 */` 표식(`:382`, 단계 C §5 테스트 공백).

### upstream 병합 리허설 (2026-10-15)

`git fetch qn` 뒤 `qn/main`이 c9f7b6208(단계 C 리허설) → **718a4e7f9**로 6커밋 움직였다(`1b63c28dc` claude 0 usage 보존, `76cd5114e` moejs v0.1.0-alpha.7, `150ab3cd4` 라이선스 목록, `bfdad293c` vidu 과금, `f196934ce` 플러그인 채널 연결, `718a4e7f9` vertex 멀티 리전). develop의 merge-base는 그대로 7de10f2c6이다. 리허설은 이 단계의 문서·도구 수정 전, 작업 트리가 깨끗한 HEAD 6275fd416에서 했다.

`git merge --no-commit --no-ff qn/main` 출력(종료 코드 1):
```
Auto-merging go.mod
Auto-merging go.sum
CONFLICT (content): Merge conflict in go.sum
Auto-merging service/text_quota.go
Auto-merging web/src/features/pricing/lib/price.ts
Auto-merging web/src/i18n/locales/en.json
Auto-merging web/src/i18n/locales/fr.json
Auto-merging web/src/i18n/locales/ja.json
Auto-merging web/src/i18n/locales/ru.json
Auto-merging web/src/i18n/locales/vi.json
Auto-merging web/src/i18n/locales/zh-TW.json
Auto-merging web/src/i18n/locales/zh.json
Automatic merge failed; fix conflicts and then commit the result.
```

- `git diff --name-only --diff-filter=U`: `go.sum` 하나. 병합이 만든 변경은 41파일(`git diff --cached --stat`: 1806 insertions, 278 deletions).
- 충돌 원인: develop의 `go mod tidy`(O4 excelize)가 지운 moejs `v0.1.0-alpha.5` checksum 두 줄 자리와, upstream이 alpha.6 → alpha.7로 바꾼 두 줄이 붙어 있다. 해소: upstream 쪽(alpha.7)을 받고 `GOWORK=off go mod tidy`로 excelize 간접 의존 줄을 되살린다(`docs/fork-overlays.md` O4 메모).
- 충돌 집합 검사: 충돌 파일 {`go.sum`} ⊆ overlay upstream 파일 집합(O4 `go.mod, go.sum` 행). 양쪽이 모두 바꾼 11파일(`go.mod`, `go.sum`, `service/text_quota.go`, `web/src/features/pricing/lib/price.ts`, locale 7개)도 모두 표에 있다(O4, O3 `service/text_quota.go:449`, O6 `price.ts`, 공통 i18n 행). 표에 없는 충돌(기록되지 않은 훅)은 없다. 예상 후보였던 README nav 줄과 `languages.ts`는 upstream이 이번 범위에서 건드리지 않아 충돌하지 않았다.
- 자동 병합 확인: locale JSON 7개는 충돌 표식 없이 병합됐고 키가 7,538 → 7,545(upstream +9, −2)가 됐다. fork 키(`Interface languages`, `Access token`, `{{field}} is required` 등)는 남아 있다. `service/text_quota.go`(`// fork: O3` 1줄)·`price.ts`(`// fork: O6` 8줄)의 표식 수는 병합 전과 같다.
- ko.json은 fork 파일이라 병합되지 않는다. 실제 병합 뒤에는 upstream의 새 9키(가격 정렬 `Input/Output/Per-request Price: High to Low/Low to High` 6개, `Vertex AI Project ID`, `e.g., my-project-123`, Vertex 리전 안내 1개)가 ko.json에 없으므로 `ko-locale.mjs check`가 `missing`으로 알린다. 번역해 넣고 `check`·`status`를 0으로 맞춘다(overlay O9 메모).
- `git merge --abort` 종료 코드 0, MERGE_HEAD 없음, `git status --short` 0줄. 실제 병합은 다음 동기화에서 한다.

## 5. 결정 사항

계획 단계(계획 하단 1–10): §1–4에 반영. 계획 결정 8의 경로 `POST /api/topup/expire`는 아래 마지막 ruling으로 `POST /api/user/topup/expire`가 됐다. spec §9에 결정 8–14로 옮겼다.

실행 중 ruling(ledger `Ruling` 줄, 오래된 순; 대가 = 틀렸을 때 비용):
- (Task 1 전) 한국어 코드는 `ko`, team `kr`은 normalizer에서만 받는다. 대가: 관찰된 것 없음(`Intl`은 `kr`을 카누리어로 읽음).
- (Task 1 전) 다른 6개 locale의 영어 placeholder는 이 단계에서 번역하지 않는다(사용자에게 묻고 반대 없음). 대가: zh·ja 등의 사용자는 다음 번역 작업까지 fork 문구를 영어로 본다.
- (Task 1 전) spec §9 결정 4대로 기계(LLM) 초안 + 용어집 검수(기본값, 사용자 "진행"). 대가: 나중에 사람 번역 전면 재작업.
- (Task 1) placeholder 2,934개가 남은 채로 ko.json을 앱에 연결(Task 2)해도 된다 — develop은 배포되지 않고 배치가 바로 이어진다. 대가: Task 8 전까지 개발 빌드에 한·영이 섞여 보인다.
- (Task 1) 용어집은 재사용 값의 다수를 따른다: Reset = 초기화, Vendor = 공급업체(Provider = 제공자와 구분). 대가: 나중에 용어를 바꾸면 약 50문자열.
- (Task 1, 앞 ruling 정정) Quota = 쿼터(재사용 값 쿼터 51 vs 할당량 30). 대가: Task 8 통일이 약 30문자열을 반대로 바꿔야 함.
- (Task 7) Tier = 등급(17 vs 11, 가격 등급에 자연스러움). 대가: 약 11문자열이 반대로 바뀜.
- (Task 8) 커밋 트레일러는 모델 전환 뒤 `Co-Authored-By: Claude Opus 5.5`, 이전 커밋은 다시 쓰지 않는다. 대가: 단계 D 안에서 트레일러가 섞임.
- (Task 9) Excel 조직 가져오기의 행 오류는 이 단계에서 영어로 남긴다 — 약 20개의 `fmt.Sprintf` 형식에 중첩 오류가 문자열로 들어가 있어, 번역하려면 구조화된 행 오류(row, username, message_key, params)와 프런트 변경이 필요하다. 대가: 한국어 조직 관리자가 가져오기 실패 시 영어 행 오류를 본다.
- (Task 11) 개인 주문 만료 라우트는 기존 관리자 사용자 그룹의 `registerForkAdminUserRoutes`에 둔다(실제 경로 `POST /api/user/topup/expire`, 범위 `billing:write`) — upstream 새 줄 없음, `/topup/complete`와 같은 그룹. 대가: 경로가 계획 문안과 다르다(문서는 실제 경로로 고침).

이미 확정해 둔 사실(재확인):
- 기존 team 사용자의 `setting.language = 'kr'`는 데이터 마이그레이션 없이 읽을 때 정규화한다(프런트 로그인 직후 `normalizeInterfaceLanguage`, 백엔드 `normalizeLang`). 이 단계의 수동 확인(§7 #9)에서 `kr`로 저장된 사용자가 영어 브라우저로 로그인해도 한국어 화면을 받았다.
- `ko-skip.json`은 `check`·`status`의 입력이라 남긴다. 배치 목록은 지웠다.
- `TimingResponseWriter.Unwrap()` 결함의 team 역이식은 단계 C에서 열린 채로 남아 있다(§6).

## 6. 단계 E로 넘기는 것

번역·표시:
- 다른 6개 locale(zh, zh-TW, fr, ru, ja, vi)의 영어 placeholder: 단계 A–C의 fork 키 160개에 단계 D가 locale마다 69개(fork 메시지 66 + 인터페이스 언어 3)를 더했다(`Access token`은 8개 모두 번역). 이 단계에서는 번역하지 않았다(ruling).
- 반영어 ko 값: 수동 확인에서 사용 로그 상세 대화상자 제목이 `Log 상세`(`Log Details`)로 보였다. 같은 꼴이 `Key 지문`(`Key Fingerprint`), `Key 출처`(`Key Sources`), `Key 다이제스트`(`Key Summary`)에도 있다. `check`는 한글만 있으면 통과시키므로 잡지 못한다. 그 밖의 경계 값(`Merchant ID은(는)`, `Thinking을 콘텐츠로 변환`, `Discovery 구성…`, `Root 불가`, `© 2025 Your Company`)은 Task 8에서 브랜드·기술 용어로 두었다.
- 남은 용어 혼용: Billing 결제/청구(`Billing Details` = 청구 상세), `Compact` = 촘촘하게 vs 설명의 컴팩트.
- Excel 조직 가져오기 행 오류는 영어(ruling). 구조화된 행 오류와 프런트 표시가 필요하다.
- relay·과금의 조직 오류(AI 클라이언트로 가는 OpenAI 형식)는 `ko.yaml`에 없다. 번역하려면 `i18n/keys.go`에 키를 더해야 한다.
- 감사 액션 `topup.expire`, `organization.topup.expire`는 프런트 라벨이 없어 원래 액션 이름으로 보인다.
- shadcn 기본 컴포넌트의 sr-only 영어(`Toggle Sidebar`, `Command Palette`)는 upstream 코드라 그대로다. API 키 화면의 빈 상태 설명이 표 오른쪽에서 잘린다(ko 문구 길이).
- `<html lang>`은 ko에서도 `en`이다(기존 동작).
- README의 문서 링크는 `docs.newapi.ai/en` 그대로다(upstream에 한국어 문서 사이트 없음).

동작:
- `InterfaceLanguages`는 쓰기 시점 검증이 없다(읽을 때 거름; 화면은 빈 값 저장을 막지만 API로는 빈 값을 저장할 수 있고 `/api/status`는 `["en"]`). 브라우저 첫 방문에는 `/api/status`가 오기 전까지 8개 언어가 모두 보인다. 프런트 파서가 Go 코드 표를 복제한다(함께 고쳐야 함). 메뉴는 고정 순서이고 관리자 순서는 옵션 값에만 남는다.
- 레거시 데이터: team 사용자의 `language: 'kr'`를 `ko`로 바꾸는 데이터 마이그레이션을 할지(지금은 읽기 시점 정규화만). ISO 639-2 `kor`는 `ko`로 매핑되지 않는다. `use-auth-redirect.ts:64` 훅은 직접 테스트하지 않았다(normalizer 테스트와 이번 수동 확인으로 갈음).
- 수동 DB 수정으로 지운 벤더를 가리키는 모델은 재구축 뒤에도 이전 벤더 이름과 할인을 유지한다.
- 백그라운드 벤더명 재구축은 1분 넘은 스냅숏이면 과금 resolve마다 시작될 수 있다(시도 창 1분으로 제한).
- `TimingResponseWriter.Unwrap()` 결함을 team 브랜치에 역이식(단계 C §2·§5, 아직 열림).

단계 C §5에서 넘어와 이번에도 다루지 않은 것:
- 단계 B I5 나머지: 조직 로그·대시보드를 가입 시점 이후로 제한, 떠난 회원 사용량을 조직 기록에 남기기, 조직 대시보드 기간 제한.
- MJ·태스크·위반 수수료 로그의 할인 항, 동적가격 모델의 정가 표시, 할인 편집기의 중복 이름·전환 시 행 손실.
- `doRequest`를 우회하는 어댑터의 `llm_ms` 공백, root의 조직 생성 시 owner 잔액 미검사, 무료 모델(`EnableFreeModelPreConsume` 끔) 세션 우회와 tiered 자동 그룹 가장자리, 이미 열린 realtime WebSocket은 정지해도 끊기지 않음, WSS 조직 지갑 사전 검사의 소폭 초과.
- 단계 B 이전 `users.remark`의 조직 메모를 `organization_remark`로 옮길지(운영자 결정).

환경:
- MySQL/PostgreSQL 미실행(컨테이너 런타임 없음). 이 단계는 스키마 변경이 없다. 새 SQL 조각은 `ExpirePendingPersonalTopUps`의 GORM `Where`(`target_type IS NULL OR target_type <> ?`, `create_time < ?`)뿐이고 세 DB 공통이다.
- 결제 provider sandbox 미실행(개인 주문 만료와 늦은 웹훅 경합 포함).

## 7. 검증

### 전체 검증 (Task 12, 2026-10-15)

HEAD 6275fd416 + 이 단계 Task 12의 도구·테스트 변경(`web/e2e/korean.e2e.ts` 추가, `ko-locale.mjs`의 `--batch` 처리, `docs/i18n/ko-batches.json` 삭제)에서, 문서 커밋 전에 실행했다. 그 변경은 9d8df54aa(test(e2e): add a Korean sign-in smoke and retire the translation batch list)로, 기록·overlay 표·spec은 이 문서 커밋으로 들어갔다.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `GOWORK=off go test ./... -count=1`: 종료 코드 0, `ok` 44 / `no test files` 39 / FAIL 0.
- `bun run typecheck`(`tsgo -b`): 종료 코드 0. `bun run test`: **212 파일 / 2,415 테스트 pass**(Task 10의 212 / 2,414에 Task 11 모바일 카드 테스트 +1). `bun run build`: 성공, `routeTree.gen.ts` 변경 없음.
- `node scripts/ko-locale.mjs check`: `7538/7538 ok`(종료 코드 0). `status`: `english placeholders left: 0`. `node --test scripts/ko-locale.test.mjs`: 17/17. `check --batch 1`: 배치 목록이 없다는 안내와 종료 코드 2. `seed`(옵션 없음): 거부, 종료 코드 1.
- `bun run i18n:sync`: 종료 코드 0, 8개 locale 파일 md5가 실행 전과 같다. `_reports/ko.untranslated.json`은 22키이고 모두 `ko-skip.json`에 있다(`_reports/`는 `web/.gitignore` 대상이라 커밋하지 않음).
- `bun run e2e`: 새 SQLite(`$TMPDIR/phase-d-task12.db`), 이 트리로 `go build`한 바이너리, `POST /api/setup` root/12345678, rate limit 3종 끔, `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING` 해제, dev 서버 3101, `E2E_REUSE_SERVER=1`로 **21 passed (47.0s)** — 조직 20 + 한국어 1(`korean.e2e.ts`: `ko-KR` 컨텍스트에서 `로그인` 버튼, 한국어 placeholder로 root 로그인, 사이드바 `대시보드`).
- 검증 뒤 바뀐 코드는 `web/src/i18n/languages.test.ts`의 `// fork: O9` 주석 한 줄뿐이다(overlay 표식). `bun run test -- languages` 3 파일 / 16 테스트 pass, `oxfmt --check`·`oxlint` 깨끗.

### 수동 시나리오

환경: 위 e2e와 같은 백엔드(3100)·dev 서버(3101)·DB. relay용으로 로컬 OpenAI 호환 mock(3199, usage 1000/1000 토큰 고정, 응답 0.3초)을 채널 `mock-good`(`gpt-4,gpt-4o-mini`)으로 등록하고 `ModelDiscount={"gpt-4":25}`. 사용자 `owner1`(조직 `Scn Org` 소유자, 지갑 1,000,000), `mem1`(회원), `rich1`(미소속, 개인 잔액 5,000 — `POST /api/user/manage add_quota`). 화면 확인은 저장소에 넣지 않은 일회용 Playwright 스크립트(`locale: 'ko-KR'`), 나머지는 API와 curl. 끝난 뒤 서버 세 개를 내리고 DB 파일을 지웠다.

| # | 시나리오 | 방법 | 결과 |
|---|---|---|---|
| 1 | ko 로그인 → 대시보드 | Playwright `/sign-in`(`로그인`, `사용자 이름 또는 이메일 입력`, `비밀번호 입력`) | 통과 — `/dashboard/overview`, 사이드바 `대시보드` |
| 2 | API 키 생성 | `/keys` → `API 키 생성` → `이름 입력`에 `scn-key` → `변경 사항 저장` | 통과 — `POST /api/token` 200, 목록에 `scn-key` |
| 3 | 사용 로그 상세의 할인·타이밍 행 | 2번 키로 gpt-4 relay 1회 → `/usage-logs/common` → `전체 상세 보기` | 통과 — `모델 할인 0.7500x`, `타이밍 분석`, `모델 제공자`, `게이트웨이 오버헤드`, `응답 시간`. 대화상자 제목은 `Log 상세`(반영어 값, §6) |
| 4 | 지갑 | `/wallet` | 통과 — `지갑`, `현재 잔액 $199.93`, `총 사용량 $0.0675`(33,750 = 45,000 × 0.75), 충전 비활성 안내 한국어 |
| 5 | 조직 화면(정지·조회 문구) | `owner1`로 `/organization` | 통과 — `사용자 조회` 버튼, `사용자 이름 또는 이메일` 입력, `mem1` 행 선택 시 `일시 정지` 버튼(title `일시 정지된 멤버는 API를 호출할 수 없습니다`) |
| 6 | 인터페이스 언어에서 ja 해제 | root: 헤더 `언어 변경` 메뉴 → 설정 → 운영 → `인터페이스 언어`에서 `日本語` 체크·`변경 사항 저장` → 해제·저장 | 통과 — 기본 메뉴 `English, 한국어`; `PUT {"key":"InterfaceLanguages","value":"en,ko,ja"}` 200 → 메뉴 3개; `"en,ko"` 200 → 메뉴 `English, 한국어`만 |
| 7 | 조직 거부(백엔드 오류)가 한국어 토스트로 | `owner1`이 `rich1` 조회 → `추가` | 통과 — `PUT /api/organization/users/7/membership` 응답 200(거부), `message_key` `user still has a personal balance, affiliate balance, subscription or pending orders; …`, 토스트 `이 사용자에게 아직 개인 잔액, 구독 또는 대기 중인 주문이 있습니다` |
| 8 | relay 오류 본문이 `ko.yaml` 문구 | `curl -H 'Accept-Language: ko' -H 'Authorization: Bearer sk-invalid' …/v1/chat/completions` | 통과 — 401 `유효하지 않은 API 키입니다 (request id: …)`(`token.invalid`). `Accept-Language: kr`도 같은 한국어, `en`은 `Invalid token` |
| 9 | 레거시 `kr` 사용자 | `mem1`이 `PUT /api/user/self {"language":"kr"}` → `en-US` 브라우저로 로그인 | 통과 — 한국어 화면(사이드바 `대시보드`), `localStorage.i18nextLng = ko` |
| 10 | ko 달력 | API 키 드로어 `만료되지 않음` → 달력 | 통과 — 요일 `일 월 화 수 목 금 토`, 캡션 `2026년 10월`(Task 2에서 미확인으로 남긴 항목) |
| 11 | 결제 provider 결제·조직 충전 | — | 미실행 — provider 계정·sandbox 없음 |
| 12 | MySQL/PostgreSQL에서 같은 흐름 | — | 미실행 — 컨테이너 런타임 없음 |
| 13 | Excel 조직 가져오기 행 오류 | — | 미실행 — 이 단계에서 영어로 남기기로 함(ruling, §5) |
| 14 | 모바일 폭의 한국어 레이아웃 | — | 미실행 — 이번 범위의 브라우저 확인은 데스크톱 폭만 |

통과 10, 미실행 4. 새로 찾은 결함은 반영어 값 4개(§6)뿐이다.

### overlay 표 검증

`grep -rn "fork: O" --include='*.go' --include='*.ts' --include='*.tsx' --include='*.mjs' --include='*.md' .`(node_modules 제외)와 `docs/fork-overlays.md`의 `파일:줄`을 스크립트로 대조했다(행의 줄 범위 안이나 바로 윗줄에 `fork:` 표식). `4406ef559..HEAD`에서 바뀐 upstream 파일 39개(README 6, Go 5, 프런트 21, locale 7)가 모두 표에 있다.

고친 drift(대부분 Task 10의 O8 한 줄이 `common/constants.go:17`·`model/option.go:82`에 들어가 아래 줄을 민 것):
- O5 `common/constants.go:113` → 114, O5 `model/option.go:497-498` → 498-499.
- O2 `model/option.go:102-108` → 103-109, `537-549` → 540-552.
- O6 `model/option.go:162-163` → 163-164, `662-663` → 665-666.
- O3 e2e `web/package.json:93` → 94(Task 1의 `i18n:ko` 줄).
- O9 `common/constants.go:40-41` → 40-42(표식은 42행 주석 끝).
- O9 `ko.json` 행: 삭제한 배치 목록 대신 병합 뒤 절차(빠진 키 번역)와 `korean.e2e.ts`.

추가한 행·표식:
- O9 `web/src/i18n/languages.test.ts:21-26, 56-88`: Task 2가 upstream 테스트 파일에 넣은 import 확장과 Korean `describe`에 표식이 없었다. 56행에 `// fork: O9` 주석을 넣고 행을 만들었다.
- O7 `web/src/features/usage-logs/components/__tests__/mobile-card.test.tsx:341-356`(Task 11 테스트, 표식은 있었고 행이 없었음).
- O9 메모(병합 뒤 ko.json·ko.yaml 빠진 키 확인 절차), O4 메모(go.sum 충돌 해소).

표식 없이 행만 있는 곳(의도): JSON(`web/package.json:27`, `:19-21, 94`, `.oxlintrc.json`, `.oxfmtrc.json`), 단계 C의 `details-dialog.tsx:746-761`(Response Time 세 곳), `languages.test.ts:21-26` import 블록.
