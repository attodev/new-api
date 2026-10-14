# upstream 재기반 단계 C(O6 할인·O7 타이밍·단계 B 이월) 기록

브랜치: develop. 베이스: edf553e5f(단계 C 계획 커밋). 계획: docs/superpowers/plans/2026-10-14-rebase-phase-c.md. spec: §4.6, §4.7, §8 단계 C, §9 결정 3·6·7.

커밋은 `git log --reverse --format='%h %s' edf553e5f..HEAD` 기준이고, Task 번호는 ledger(`.superpowers/sdd/2026-10-14-rebase-phase-c/progress.md`)와 맞췄다. upstream 파일의 훅 위치는 모두 `docs/fork-overlays.md`의 `O6`·`O7`·`O3` 행에 있다.

## 1. O6 할인 (Task 1–5)

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | 65d171b28 | feat(pricing): port the model/vendor discount setting layer |
| 1 | 1dd80fd0e | fix(pricing): keep discount hooks out of upstream alignment and make the vendor resolver lock-free |
| 2 | f26c8d4df | feat(pricing): fold the effective discount into the group ratio and log it separately |
| 2 | 093238b50 | fix(pricing): resolve the discount by origin model name first and keep log ratios consistent |
| 3 | f41ed8245 | feat(web): show model and vendor discounts on the pricing page |
| 3 | 1c542071e | test(web): pin the no-discount pricing display and record hook line ranges |
| 4 | 153e03441 | feat(web): show the applied discount in the usage log billing breakdown |
| 4 | ca93cac99 | test(web): pin the full-discount breakdown row and match the ratio suffix |
| 5 | fb309b523 | feat(web): add the model and vendor discount editors to the group pricing settings |
| 5 | 3590e2161 | test(web): validate discount maps against the real group schema and warn on empty rows |

### Task 1 — 설정 계층
- team `setting/ratio_setting/model_discount.go`(+테스트)를 그대로 복사했다. `validateDiscountJSON`은 이미 `common.UnmarshalJsonStr`을 쓴다. 비숫자 거부 테스트(`TestUpdateModelDiscountRejectsNonNumeric`)는 처음부터 통과했다(이전 값 유지 단언이 핵심).
- 새 fork 파일 `model/pricing_discount.go`: `updateDiscountOption`(저장 성공 시 `InvalidatePricingCache`), `effectiveDiscountPercent`(모델 > 벤더), 모델→벤더명 캐시와 resolver(`SetVendorResolver`).
- 훅: `types/price_data.go:15-17`(`BaseGroupRatio`·`DiscountMultiplier`·`DiscountSource`), `model/option.go:162-163, 662-663`, `model/pricing.go:52, 61, 103, 226, 228, 235, 437`. 빈 줄로 gofmt 정렬 블록을 나눠 upstream 줄은 공백도 바뀌지 않는다.
- 수정 라운드: resolver 캐시가 자체 `RWMutex`를 쓰고 `updatePricingLock`을 잡지 않음, 캐시 키는 `FormatMatchingModelName`으로 정규화.

### Task 2 — 과금 fold와 로그
- `relay/helper/price.go:63` 한 줄 → `relay/helper/price_discount.go`의 `foldDiscountIntoGroupRatio`. `HandleGroupRatio`에서 `BaseGroupRatio`를 남기고 `GroupRatio`에 배수를 한 번 곱한다. PriceData 기반 경로(사전차감·정산·tiered·per-call·재시도·이미지·위반 수수료·MJ)는 모두 이 값을 공유한다.
- `PriceData`를 읽지 않는 경로 두 곳에 명시 곱셈: `service/quota.go:122`(WSS 사전차감), `service/task_billing.go:423`(태스크 토큰 재계산).
- 로그: `service/log_info_generate.go:110` → `service/log_discount_fork.go`의 `appendDiscountInfo`. `discount_multiplier`·`discount_source`를 쓰고 `group_ratio`를 기저값으로 덮어쓴다. 배수 0도 기록한다(team은 0을 건너뛰어 100% 할인 로그가 기저값을 잃었다). WSS·오디오 로그 content 문자열도 기저값(`service/quota.go:220, 225, 360, 365`).
- 조회 순서는 리뷰 ruling으로 **요청 모델 → 그 벤더 → 과금 모델 → 그 벤더**가 됐다(§4). 계획 결정 3(과금 모델명 기준)을 대체한다.
- 테스트: `relay/helper/price_discount_e2e_test.go`의 사전차감 == 정산(ratio 2, 30% 할인, 1000 토큰 → 1400), `service/discount_billing_fork_test.go`의 WSS·태스크 훅(훅을 지우면 실패하는 mutation 확인), 100% 할인·벤더 전용 할인 fold.

### Task 3 — 가격 페이지
- 새 fork 파일: `web/src/features/pricing/lib/discount-color.ts`(히트맵 1–14 에메랄드, 15–29 앰버, 30–49 오렌지, 50+ 빨강), `components/discount-badge.tsx`(`DiscountBadge`, `DiscountedPrice`).
- 훅: `types.ts`, `lib/price.ts`(포맷터 4개에 `discountPercent = 0`), `model-card.tsx`(취소선 정가 + 할인가, 캡션 아래 배지), `model-price-cell.tsx`(표 Price 열), `cached-price-cell.tsx`(표 Cached 열 — 할인가만), `pricing-sidebar.tsx`(벤더 칩, 벤더 자신의 %), `model-details.tsx`.
- 스크린샷 `docs/discount/images/marketplace.png`.

### Task 4 — 사용 로그 상세의 할인 행
- `details-dialog.tsx`의 `BillingBreakdown`에서 `Group Ratio` 행 다음에 `Model discount`/`Vendor discount` 행(`0.7500x`, `formatRatio` 소수 4자리 + `x`). 배수 0도 표시한다.
- team의 "Show formula" 계산식은 upstream 상세에 없다. upstream 가격 행은 모두 정가 기반이고 합계는 `log.quota`라 배수를 다시 곱할 금액이 없다.

### Task 5 — 관리자 편집기
- upstream `GroupRatioVisualEditor`의 탭 `Discounts`로 넣었다(설정 → Billing & Payment → Group Pricing, 저장 `Save group settings`). 새 fork 파일: `models/{model-discount-visual-editor,vendor-discount-visual-editor,discount-settings-panel}.tsx`, `discount-utils.ts`.
- 검증은 `createGroupSchema`의 `discountMapValidation`(0–100 숫자 map, 빈 문자열 거부). 범위 밖이면 저장하지 않고 Discounts 탭을 연다. 이름이 빈 행 경고, 중복 이름 경고(저장은 막지 않음).
- `saveGroupRatios`가 바뀐 키만 `PUT /api/option/`하므로 키 추가만으로 저장된다. 스크린샷 `docs/discount/images/admin-discount-editors.png`.

### 문서
- `docs/discount/admin-guide.md`, `docs/discount/design.md`: team 문서를 가져와 고쳤다(§3 마지막 항목).

## 2. O7 타이밍 (Task 6–7)

| Task | 커밋 | 내용 |
|---|---|---|
| 6 | b3e780def | feat(relay): port the e2e/llm/gateway timing breakdown into usage logs |
| 6 | af5f67506 | fix(relay): keep write deadlines working under the timing writer and scope the internal timing keys |
| 7 | 528724146 | feat(web): show the timing breakdown in the usage log details |

### Task 6 — 백엔드
- team 파일 복사: `middleware/request_timing.go`, `common/timing_response_writer.go`, `relay/channel/timing_body.go`(+테스트). 로그 helper는 `service/log_timing_fork.go`(`*model.LogOther`로 다시 씀).
- 훅: `constant/context_key.go:12`, `router/relay-router.go:16`(CORS 앞 `RequestTiming()`), `relay/common/relay_info.go:94-96`(필드 2개), `:1002-1014`(세터 2개), `relay/channel/api_request.go:563, 566, 571, 587`, `service/log_info_generate.go:112`, `relay/common/relay_info_test.go:8, 403-432`.
- 기록되는 키: public `e2e_ms`(게이트웨이 진입 → 마지막 응답 Write), `llm_ms`(마지막 시도의 업스트림 요청 시작 → 바디 EOF/Close), `gateway_ms`(= e2e − llm, 0 하한). admin 전용 `pre_llm_ms`·`post_llm_ms`(§4 ruling). 모든 값은 int64 ms이고 음수는 0으로 내린다(Review Focus 2).
- **write deadline 결함(Critical, 수정 라운드)**: `TimingResponseWriter`에 `Unwrap()`이 없어서 `http.NewResponseController(c.Writer).SetWriteDeadline`(relay/helper `ExtendWriteDeadline`)이 모든 relay 경로에서 `feature not supported`를 돌려줬다. 스트림 쓰기에 deadline이 걸리지 않는다. `Unwrap() http.ResponseWriter`를 추가하고 `TestTimingResponseWriter_KeepsWriteDeadlineSupport`로 고정했다. **같은 결함이 team에도 있다 — team으로 역이식해야 할 버그다**(§5).
- timing writer로 쓴 바이트가 없으면(hijack된 realtime WebSocket) 타이밍 키를 하나도 쓰지 않는다(§4 ruling).

### Task 7 — 프런트
- 새 fork 파일: `usage-logs/lib/timing-breakdown.ts`(`effectiveUseTimeSec`: `e2e_ms`가 있으면 그 값, 없으면 `use_time`), `components/dialogs/timing-breakdown-section.tsx`.
- 훅: `types.ts:221-223`, `details-dialog.tsx:88, 98, 524, 745-760, 782-783`, `common-logs-columns.tsx:69, 805`, `common-log-mobile-card.tsx:36, 210`, i18n 키 3개(7개 locale 번역).

### 로그 상세의 Timing Breakdown
사용 로그(또는 조직 사용 로그) → 로그 행 상세 → **Log Details**의 `Response Time` 아래 **Timing Breakdown** 섹션.
- `Model Provider`: `llm_ms` — 업스트림 모델 제공자 호출 시간(마지막 시도만).
- `Gateway Overhead`: `gateway_ms`와 `e2e_ms` 대비 비율(`0.4s (57.3%)`). `e2e_ms`가 0이면 `0.0%`.
- `Response Time`과 표·모바일 카드의 응답 시간은 `e2e_ms`가 있으면 그 값을 쓴다(없으면 기존 `use_time`).
- 세 키가 모두 있을 때만 섹션이 보인다. `doRequest`를 거치지 않는 어댑터(AWS Bedrock SDK, coze 완료 폴링, `DoWssRequest`)는 `llm_ms`가 없어 섹션이 없다.
- 채널 재시도가 있으면 실패한 시도의 시간이 `Gateway Overhead`에 들어간다(Review Focus 3, `TestGenerateTextOtherInfo_TimingBreakdown_RetryAttributesFailedAttemptTimeToGateway`). 수동 실행에서 재시도 1건이 `Gateway Overhead 0.4s (57.3%)`로 보였다(§6 #8).
- 일반 사용자·조직 관리자에게는 public 키 세 개만 내려간다. `pre_llm_ms`·`post_llm_ms`는 admin 로그 조회에만 있고 화면에는 표시하지 않는다.

수동 시나리오 결과 표는 §6에 있다.

## 3. 단계 B 이월 (Task 8–10)

| Task | 커밋 | 내용 |
|---|---|---|
| 8 | 5e803c608 | feat(organization): require a clear personal balance to join and replace the assignable list with an exact lookup |
| 8 | e09146087 | feat(web): add organization members by exact lookup and explain the personal-balance refusal |
| 8 | 66ad7ac69 | fix(organization): refuse affiliate balances on join and keep existing members out of the add flow |
| 9 | dd94729a5 | feat(organization): give organization admins an org-scoped member status and note instead of platform fields |
| 9 | afb13b3cb | feat(web): suspend, resume and annotate organization members with org-scoped fields |
| 9 | f2c0d335d | fix(organization): block suspended members from organization writes and scope member updates to the org |
| 10 | b0da7d671 | fix(organization): expire abandoned org top-ups, fence affiliate/check-in inflows, escape member search, and tidy |
| 10 | 005b796ca | fix(organization): age-gate org top-up expiry, charge WSS checks to the pinned org, and make import creation atomic |

### 결정 6(I2) 확정 — Task 8
- 초대·수락 단계는 없다. `GET /api/organization/assignable-users`를 없애고 `GET /api/organization/users/lookup`(새 fork 파일 `controller/organization_member_lookup.go`)으로 바꿨다. `?username=` 정확 일치 또는 `?email=` 정확 일치(대소문자 무시), 범위는 `role < admin`이고 미소속이거나 같은 조직. 정확히 한 명일 때만 `{id, username, display_name, organization_id}`, 아니면 404 `user not found`. 라우트 수는 38 그대로.
- 미소속 사용자는 `quota > 0`, `aff_quota > 0`, 활성 개인 구독 또는 결제 대기 중인 개인 주문(최종 수정 I2)이 있으면 할당이 거부된다(`AssignUserToOrganization` 트랜잭션 안, 사용자 행을 잠근 직후). 같은 조직 구성원의 역할 변경은 해당 없다.
- 화면: `Username or email` → `Look up user` → 결과 줄 → `Add`. username을 먼저 조회하고, 없으면서 `@`가 있으면 email로 다시 조회한다. 이미 구성원이면 `Already a member`로 `Add`를 막는다. 거부되면 `This user still has a personal balance, subscription or pending orders`(최종 수정 I2 전에는 `This user still has a personal balance or subscription`).
- Excel 가져오기: 기존 미소속 사용자 행은 같은 규칙으로 행 오류. 가져오기가 새로 만드는 사용자는 검사하지 않는다(§4 ruling).

### 결정 7(I7) 확정 — Task 9
- `users.organization_member_status`(int, 기본 1; 1 active / 2 suspended)와 `users.organization_remark`(varchar(255)) 컬럼(`model/user.go:118-119`, AutoMigrate, 구조체 끝에 두어 upstream 정렬 무변경).
- 조직 API는 `member_status`·`organization_remark`만 받는다. 옛 키 `status`·`remark`는 `unsupported field: …`로 거부한다. 플랫폼 `status`·`remark`는 root 전용이 됐다. 목록 뷰에서 `remark`가 빠지고, 플랫폼 상태는 읽기 전용 배지(`Disabled by platform`)로만 보인다.
- 정지된 회원: relay는 403 `organization membership is suspended`(`service/billing_session_organization.go`, 단일 진입점 `NewBillingSession`), 레거시 MJ는 MJ `code: 4`, `/api/organization/*`의 GET이 아닌 요청은 모두 403(§4 ruling, `RejectDisabledOrganizationMutation`). 로그인·`/wallet` 요약·`subscription/self`·`topup/self`는 된다. 조직을 떠나면 두 값이 초기화된다. `subscription/self`는 `organization_remark`를 비운다.
- 회원 쓰기는 `UPDATE … WHERE id=? AND organization_id=?`(`UpdateOrganizationMemberFields`)로 조직 범위에 묶였다.
- Excel 열이 `member_status`(`active`/`suspended`)·`organization_remark`로 바뀌었다. 옛 `status`·`remark` 열이 있는 시트는 통째로 거부된다(단계 B 내보내기 파일과 호환되지 않음, 계획 결정 9).

### Task 10 — 소형 강화
| # | 항목 | 내용 |
|---|---|---|
| 1 | M6 조직 충전 만료 | root 전용 `POST /api/organizations/:id/topups/expire`(새 fork 파일 `controller/organization_topup_admin.go`, casbin `organization:write`, 감사 `organization.topup.expire`). 활성 조직은 1시간 지난 pending 주문만 만료하고 나머지는 `skipped_recent`로 센다. 비활성 조직은 모두 만료. 라우트 수 38 → 39 |
| 2 | M10 추천·출석 유입 | `controller/user.go:427-429`(`TransferAffQuota`), `controller/checkin.go:51-53`(`DoCheckin`)에서 조직 회원 거부 |
| 3 | M7 WSS 조직 지갑 | `service/quota.go:145-147` — realtime 사전 검사가 세션에 고정한 과금 조직(`BillingOrganizationId`)의 지갑 잔액도 본다(새 fork 파일 `service/quota_organization.go`) |
| 4 | LIKE 이스케이프 | 조직 사용자 검색 `keyword`의 `%`·`_`·`!`를 `ESCAPE '!'`로 문자 그대로 검색(`model/organization_keyword.go`) |
| 5 | 가져오기 트랜잭션 | 새 사용자 생성 + 조직 배정 + 회원 필드를 한 트랜잭션(`CreateUserInOrganization`)으로 — 배정 실패 시 고아 사용자가 남지 않음 |
| 6 | dead code | `MaxTopUpAmount`, `rejectTopUpAmountTooLarge` 삭제 |
| 7 | 화면 | `Top up organization wallet` 버튼은 소유자에게만 |

- M11: casbin `organization` 리소스는 `/api/organizations` 앞의 `RootAuth` 때문에 실제로 root 전용이다. 계획 결정 8대로 코드는 바꾸지 않고 이 기록과 `docs/openapi/organization-rest-api.md` §2(RootAuth + casbin)로 문서화해 닫는다.
- M7의 레거시 MJ 사전 가드는 호출 전에 가격을 알 수 없어 조직 지갑 `> 0` 검사를 유지한다(계획 결정 7). 요청 1건만큼 초과할 수 있다.

### 문서
- `docs/openapi/organization-rest-api.md`, `docs/manuals/organization-management-manual.md`, `docs/manuals/organization-test-guide.md`는 Task 8–10이 고쳤다. Task 11에서 관리 매뉴얼에 남은 것(관리자에게도 충전 버튼이 보인다는 문장, 조직 삭제 전 만료 API, 조직 회원의 추천·출석 거부, 로그 상세의 할인·타이밍)을 고쳤다. 화면 매뉴얼 `organization-user-manual.md`에 남아 있던 단계 B 흐름(할당 후보 목록, `Disable`, 플랫폼 remark)도 조회·정지·조직 메모로 바꿨다.
- 할인 문서(`docs/discount/`): team 문서 두 개를 가져와 고쳤다. 경로 `web/default/src` → `web/src`, "Show formula" 절 → `Billing Details`의 `Model discount 0.7500x` 행, 설정 위치 → Billing & Payment → Group Pricing → Discounts 탭·저장 `Save group settings`, 파일 맵 → 이 계획의 파일 구조(`tool_billing.go`는 develop에 없음), 우선순위 → 요청 모델 우선 4단계, `pre/post_llm_ms`는 admin 전용. 스크린샷은 Task 3·5에서 develop 화면으로 찍은 두 장이다.

## 3a. 최종 수정 웨이브 (전체 브랜치 리뷰 I1·I2·M1–M6)

리뷰: `docs/reviews/2026-10-14-rebase-phase-c-final-review.md`. 결과: `final-fix-report.md`.

| 커밋 | 항목 | 내용 |
|---|---|---|
| 2cb7cd7a7 | I1 | `updatePricing`이 모델→벤더 맵을 지역 맵으로 만들고 루프 뒤 `publishModelVendorNames`로 한 번에 교체한다. `InvalidatePricingCache`는 맵을 지우지 않는다. resolver는 스냅숏만 읽고, 스냅숏이 아직 없을 때(첫 시작)만 1분에 한 번 `GetPricing()`을 부른다(음수 캐시 타임스탬프) |
| dc1d30110 | I2·M4 | 가입 거부에 결제 대기 개인 주문 추가(`top_ups`의 pending이면서 조직 지갑이 아닌 것, `subscription_orders`의 pending). 메시지 `…subscription or pending orders…`, 화면 안내 `This user still has a personal balance, subscription or pending orders`(7개 locale). Excel 기존 사용자 배정 + 회원 필드를 한 트랜잭션(`AssignUserToOrganizationWithMemberFields`)으로 |
| b07f61b42 | M1·M2·M5 | 출석 테스트의 비회원 경로를 가드 직접 검사로 바꿔 `-race` 경합 제거. 무료 모델 판정 4곳이 할인 전 group ratio(`freeModelGroupRatio`)를 본다. `/api/organization/*` 비-GET 가드가 actor 행을 한 번 읽어 gin context에 두고 `getOrganizationActor`가 재사용(회원 정보 읽기 3 → 1) |
| 675e797ae | M3 | 사용 로그 목록(데스크톱 토큰 칸·요약 세그먼트·모바일 카드)의 group ratio 옆에 `−{percent}%` |
| (이 문서 커밋) | M6 | `details-dialog.tsx:170`, `group-ratio-form.tsx:69`(oxfmt가 줄 끝 주석을 옮기므로 export 바로 앞 줄), `relay_info_test.go:8`(줄 끝)에 `fork:` 표식. 관리 매뉴얼 문제 해결 제목이 화면 안내 문구를 인용하게 한 것은 I2 문구 변경과 함께 dc1d30110에 들어갔다 |

## 4. 결정 사항

계획 단계(계획 하단 1–10): §1–3에 반영. 계획 결정 3(과금 모델명 기준 fold)은 아래 첫 ruling으로 대체됐고, 계획 결정 4의 "pre/post는 저장만"은 아래 셋째 ruling으로 admin 범위가 정해졌다.

실행 중 ruling(ledger `Ruling:` 줄, 오래된 순):
- (Task 2) 할인 조회는 fold·WSS 사전차감·태스크 재계산·벤더 resolver 모두 **요청(origin) 모델명 먼저, 과금 모델명은 fallback**이다 — 마켓플레이스가 요청 모델명으로 보여 주는 할인이 청구 할인과 같아야 한다. 대가: 기본 이름에만 할인을 건 관리자는 변형 모델도 같은 할인으로 청구된다(관리자 가이드 §4에 0% 설정으로 피하는 법을 적음).
- (Task 2) 위반 수수료도 다른 과금처럼 할인을 받는다(team과 같음, 코드 변경 없음). 대가: 100% 할인 모델은 CSAM 위반 수수료가 0이 된다.
- (Task 6) `pre_llm_ms`·`post_llm_ms`는 `SetAdmin`으로 쓴다(team 설계상 내부 구간). 대가: 조직 관리자 등 비관리자는 이 두 구간을 볼 수 없다(UI는 원래 표시하지 않음).
- (Task 6) timing writer에 쓴 기록이 없으면(hijack된 realtime WebSocket) `e2e_ms`를 포함해 타이밍 키를 쓰지 않는다. 세션 나이를 지연 시간으로 보고하지 않기 위해서다. 대가: WSS 로그에는 타이밍이 없다(원래 의미 있는 값이 없었음).
- (Task 8) Excel 가져오기가 **새로 만드는** 사용자는 개인 잔액 검사 없이 배정한다(개인 돈이 없었고, 가입 보너스는 결제 잔액이 아니며 시트의 quota 칸이 한도를 정함). 대가: 시트가 quota를 덮지 않으면 `QuotaForNewUser` 보너스가 그 회원의 한도로 시작한다.
- (Task 9) 정지된 회원은 `/api/organization/*`의 GET이 아닌 요청을 하나도 할 수 없다(Phase B 미들웨어에서 root가 아닌 actor에 적용; owner는 정지할 수 없어 충전은 영향 없음). 대가: 정지된 관리자는 재개될 때까지 관리 화면에서 아무것도 바꿀 수 없다.
- (최종 수정 I1) 벤더명 맵은 불변 스냅숏이다. 재구축은 지역 맵을 채운 뒤 한 번에 바꾸고, `InvalidatePricingCache`는 맵을 nil로 만들지 않는다. 대가: 스냅숏은 기동 시와 `GetPricing()`이 불릴 때(`/api/pricing`, `/api/rankings`, 모델 메타 목록)만 다시 만들어진다. relay 요청만 받는 노드에서는 그 뒤로 재구축이 일어나지 않으므로, 벤더 소속이 바뀌거나 새로 추가된 모델은 그 노드에서 다음 `GetPricing()` 호출까지 이전(또는 없는) 벤더 할인으로 청구된다. 최종 재리뷰가 지적한 잔여 항목으로, 스냅숏이 1분 넘게 오래되면 비차단 `go GetPricing()`(TryLock)으로 갱신하는 보완을 단계 D 후보로 둔다(§5). 첫 시작에 가격 재구축이 실패하면 1분 동안은 벤더 할인 없이 청구된다(음수 캐시, DB 장애 때 재구축 폭주 방지).
- (최종 수정 I2) 결제 대기 중인 개인 충전·구독 주문이 있으면 조직 가입을 거부한다(정산은 가입 여부를 다시 보지 않으므로, 가입 전에 연 결제가 가입 뒤 끝나면 사용자 돈이 조직 한도가 된다). 대가: 결제사가 만료시키지 않는 버려진 주문(예: Epay 충전)은 만료될 때까지 가입을 막는다(§5; 단계 D 작업 11 이후 관리자 API로 만료).
- (최종 수정 M2) 할인(100% 포함)은 `FreeModel`을 바꾸지 않는다. 무료 모델 판정은 할인 전 group ratio로 한다. 100% 할인 모델도 결제 세션을 거쳐 0을 예약하므로 정지된 회원·비활성 조직은 막히고, 로그에는 `discount_multiplier` 0이 남는다.
- (Task 10) root가 만료시킨 "결제 완료됐지만 아직 pending인" 조직 주문은 뒤늦은 웹훅으로도 적립되지 않는다(upstream Stripe 만료와 같음). 1시간 나이 기준과 OpenAPI §3.5의 운영자 안내로 경합을 좁힌다. 대가: 만료 호출 중에 결제된 주문은 provider 환불로만 복구된다.

이미 확정해 둔 사실(재확인):
- 할인 조회 우선순위는 요청 모델 → 요청 모델의 벤더 → 과금 모델 → 과금 모델의 벤더이고, 처음 찾은 값 하나만 쓴다(0%도 값으로 친다).
- `GroupRatioInfo.BaseGroupRatio`가 있어 100% 할인(배수 0)에서도 로그의 `group_ratio`가 기저값으로 남고 나눗셈이 없다.
- `pre_llm_ms`/`post_llm_ms`는 admin 전용, hijack된 WSS 로그에는 타이밍 키가 없다.
- `TimingResponseWriter.Unwrap()`이 write deadline 공백을 막는다. 같은 공백이 team에도 있다 — team으로 역이식할 버그다.
- 결정 6·7은 Task 8·9로 구현됐다: 초대 흐름 없음, 정확 조회, quota·aff_quota·활성 구독·결제 대기 개인 주문(최종 수정 I2)이면 거부, 조직 범위 `organization_member_status`·`organization_remark`, 정지된 회원은 relay·MJ·`/api/organization/*`의 모든 비-GET에서 차단.
- 조직 충전 만료 엔드포인트는 조직이 비활성이 아니면 1시간 나이 기준이 있다.

## 5. 단계 D로 넘기는 것

번역·표시:
- 할인·타이밍·조직 단계 C 키는 7개 locale에 번역을 넣었지만 검수는 하지 않았다. 단계 D의 ko locale(O9)에는 이 키들도 포함해야 한다. 단계 B 조직 키 126개 이상은 여전히 영문 fallback이다.
- MJ·태스크·위반 수수료 로그에는 할인 항이 없다. 접힌(할인된) `group_ratio`만 남고, 그래서 목록의 `−{percent}%` 표시(최종 수정 M3)도 없다. 태스크 재계산은 요청 모델명을 두 번 넘긴다(태스크는 과금 모델명을 저장하지 않음).
- 동적가격(billingexpr) 모델은 배지 옆에 정가 계산값이 보인다(과금은 할인됨). 표의 Cached 열은 취소선 없이 할인가만 보인다. 모델 상세(`ModelDetails`)는 upstream 화면에서 열 수 없어 단위 테스트로만 확인했다.
- 모바일 카드의 tokens/s는 여전히 `use_time`으로 나눠서, `use_time` 0이고 `e2e_ms > 0`이면 tokens/s가 나오지 않는다.
- 할인 편집기: 중복 이름은 경고만 하고 마지막 값이 저장된다. 저장되지 않은 행은 Visual ↔ JSON 전환 때 사라진다. 벤더 목록 조회가 끝나기 전에는 자유 입력칸이 잠깐 보인다.

동작:
- `doRequest`를 우회하는 어댑터(`DoWssRequest`, AWS Bedrock SDK, coze 완료 폴링, dify·replicate 업로드와 baidu 토큰 같은 보조 호출)는 `llm_ms`가 없거나 일부만 잡힌다(team과 같음). 보조 호출 시간은 `pre_llm_ms`에 들어간다.
- root의 조직 생성은 owner를 잔액 검사 없이 배정한다(운영자 작업).
- 결제사 만료 콜백이 없는 버려진 개인 주문(예: Epay 충전)은 pending으로 남아 그 사용자의 조직 가입을 막는다(최종 수정 I2). 단계 D 작업 11에서 관리자 `POST /api/user/topup/expire`(1시간 지난 개인 충전 주문 만료, `topup.expire` 감사)를 추가해 DB 직접 수정이 필요 없다(OpenAPI 3.6). 구독 주문(`subscription_orders`)의 버려진 pending은 여전히 대상이 아니다.
- 무료(가격·배율 또는 group ratio 0) 모델은 `EnableFreeModelPreConsume`이 꺼져 있으면 과금 세션을 거치지 않아 정지된 회원·비활성 조직도 막지 않는다. 100% 할인 모델은 더 이상 여기에 해당하지 않는다(최종 수정 M2). 남은 가장자리: tiered 모델의 자동 그룹 재시도가 무료 그룹에서 100% 할인된 유료 그룹으로 바뀌면 `service/tiered_settle.go`가 접힌 `snap.GroupRatio == 0`을 보고 세션을 만들지 않는다(upstream 파일, 드묾). 이미 열린 realtime WebSocket은 정지해도 바로 끊기지 않는다(연결 시작 때만 검사).
- 단계 B 이전에 조직 관리자가 `users.remark`에 쓴 메모는 `organization_remark`로 옮기지 않았다(root도 쓴 컬럼이라 운영자 결정).
- WSS 조직 지갑 검사는 읽고-청구하는 사전 검사(예약 없음)라 동시 realtime 세션은 조금 초과할 수 있다(upstream 사용자 잔액 검사와 같음). 레거시 MJ 사전 가드는 조직 지갑 `> 0`만 본다(M7).
- 단계 B I5 나머지: 조직 로그·대시보드를 가입 시점 이후로 제한하고 떠난 회원 사용량을 조직 기록에 남기기, 조직 대시보드 기간 제한 — 단계 C에서 다루지 않았다.
- `TimingResponseWriter.Unwrap()` 결함을 team 브랜치에 역이식(§2).

환경:
- MySQL/PostgreSQL 미실행(컨테이너 런타임 없음). 새 컬럼 2개(`organization_member_status` int, `organization_remark` varchar(255))는 `organization_role`과 같은 패턴이다. 새 SQL 조각은 GORM `Where` 안의 `LOWER(email) = ?`와 LIKE `ESCAPE '!'`뿐이고 세 DB 공통 문법이다. 실행 명령: `TEST_MYSQL_DSN=… TEST_POSTGRES_DSN=… GOWORK=off go test ./service/ ./model/ ./controller/ -run 'Organization|Discount'`, 스키마 리허설 `./scripts/schema-rehearsal/run.sh all`.
- 실제 결제 provider 정산·만료 경합은 sandbox에서 확인하지 않았다(단계 E 결제 작업과 함께).

테스트 공백:
- 사용 로그 목록의 할인 표시(`−30%`)는 숫자뿐이라 번역이 없고, 그룹 이름도 배율도 없는 행(드묾)에는 데스크톱 토큰 칸에 단독으로 보인다.
- `common-log-mobile-card.tsx:381`(O3 `&& showUserInfo`)은 행에는 있지만 줄에 `fork:` 표식이 없다(JSX 조건 중간).
- ratio_setting 이식 테스트가 전역 map을 정리하지 않는다(순서 의존, 지금은 무해). `SetWriteDeadline` 테스트는 writer를 손으로 감싼다(common이 middleware를 import할 수 없음).
- `gateway_ms` 0 + `e2e_ms > 0` → `0.0%` 단언 없음, `DetailsDialog` 안의 Timing 섹션 배치는 직접 테스트하지 않음(섹션 컴포넌트만), `discount_source`가 `vendor`가 아니면 모두 `Model discount`로 표시(백엔드는 model/vendor만 보냄).
- 조직 사용 로그 화면의 할인·타이밍 행은 브라우저로 보지 않았다(같은 `DetailsDialog`).

- (최종 재리뷰 잔여) 벤더명 스냅숏 신선도: relay 전용 노드는 기동 후 재구축이 없다. 보완: 스냅숏이 1분 넘게 오래되면 비차단 재구축(TryLock) 또는 옵션 변경 시 재구축 신호. `updatePricing`이 벤더 조회 오류를 무시하면 벤더명 없는 스냅숏이 발행되는 점도 같이 본다.

## 6. 검증 (Task 11, 2026-10-14)

HEAD 005b796ca(이 문서 커밋 직전)에서 실행.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `make test`: 종료 코드 0, `ok` 54 / `no test files` 46 / FAIL 0 (100 패키지, 이 중 9개는 캐시 결과). 단계 B와 같은 패키지 구성이다.
- `bun run typecheck`(`tsgo -b`): 종료 코드 0. `bun run test`: 207 파일 / 2384 테스트 pass(단계 B 끝 203 / 2318에서 +4 / +66). `bun run build`: 성공, `routeTree.gen.ts` 변경 없음.
- `bun run e2e`: 새 SQLite(`$TMPDIR/new-api-e2e.db`, HEAD로 빌드한 바이너리, `POST /api/setup` root/12345678, rate limit 3종 끔, `E2E_REUSE_SERVER=1`)로 **20 passed (48.5s)**. Task 9가 정지·재개 시나리오를 더해 19 → 20.
- `docs/fork-overlays.md`: `git grep -n "fork: O"`와 행을 대조했다. 단계 C 파일의 `// fork:` 표시 줄은 모두 행 범위 안에 있다. 고친 것(단계 C 훅이 줄을 밀어 생긴 drift): O5 `model/option.go:495-496` → 497-498, O3 `model/user.go:538` → 541·`:253` → 256, `relay/common/relay_info.go:148` → 151, `service/log_info_generate.go:196` → 198·`199-201` → 201-203, `common-logs-columns.tsx:396, 403-407, 561, 570` → 397, 404-408, 562, 571, `common-log-mobile-card.tsx:67, 377` → 68, 378, O6 `details-dialog.tsx:168, 242-255` → 170, 244-257. 추가한 행: O7 `relay/common/relay_info_test.go:8, 403-432`. 공통 i18n 행에 단계 C의 번역된 키를 적었다. `edf553e5f..HEAD`에서 바뀐 upstream 파일 41개(locale 7개 포함)가 모두 표에 있다. 행 수: O3 89, O6 26, O7 12.

### 최종 수정 웨이브 검증 (2026-10-15)

HEAD 675e797ae + 이 문서 커밋의 표식 변경(주석만)에서 실행.
- `GOWORK=off go vet ./...`: 종료 코드 0. `GOWORK=off go test ./... -count=1`: 종료 코드 0, `ok` 44 / `no test files` 39 / FAIL 0.
- `GOWORK=off go test -race ./controller/ -run 'CheckIn|Organization' -count=1`: ok. 수정 전에는 같은 명령을 `-count=3`으로 돌리면 `TestOrganizationMemberCannotCheckIn`이 `race detected`로 실패했고, 수정 뒤 `-count=3`도 ok.
- `bun run typecheck`: 종료 코드 0. `bun run test`: 209 파일 / 2398 테스트 pass(Task 11의 207 / 2384에서 +2 / +14). `bun run build`: 성공, `routeTree.gen.ts` 변경 없음. 바뀐 프런트 파일의 `oxlint`·`oxfmt --check`: 새 문제 없음.
- `bun run e2e`: 새 SQLite, 이 HEAD로 빌드한 바이너리, `POST /api/setup` root/12345678, rate limit 3종 끔, `E2E_REUSE_SERVER=1`로 **20 passed (49.0s)**.
- `docs/fork-overlays.md`: 이번에 건드린 upstream 파일(`model/pricing.go`, `relay/helper/price.go`, `service/task_billing_test.go`, `common-logs-columns.tsx`, `common-log-mobile-card.tsx`, `details-dialog.tsx`, `group-ratio-form.tsx`, `relay_info_test.go`)의 행 줄 번호를 스크립트로 `fork:` 표식과 대조해 고쳤다. 새 행: O6 `relay/helper/price.go:142,248,262,387`, O6 프런트 `common-logs-columns.tsx`·`common-log-mobile-card.tsx`의 할인 표시.

### 수동 시나리오 실행

환경: HEAD 005b796ca를 `go build`한 바이너리를 `--port 3100`으로, `SQLITE_PATH=$TMPDIR/phase-c-task11.db`(새 파일), `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING` 해제, rate limit 3종 끔. `VITE_REACT_APP_SERVER_URL=http://127.0.0.1:3100 bun run dev`(3101). `POST /api/setup`(root/12345678). relay용으로 로컬 OpenAI 호환 mock(3199, usage 1000/1000 토큰 고정, 응답 0.3초; `Authorization`에 `fail`이 있으면 0.4초 뒤 500)을 채널 두 개로 등록했다: `mock-good`(우선순위 0, `gpt-4,gpt-4o-mini,gpt-4o,gpt-3.5-turbo`), `mock-fail`(우선순위 10, `gpt-3.5-turbo`). `RetryTimes=1`, 벤더 `OpenAI`를 API로 만들었다. 이번에는 디스크 사용률(91%)이 성능 보호 기준 아래라 `monitor_enabled`를 끌 필요가 없었다. 화면 확인은 저장소에 넣지 않은 일회용 Playwright 스크립트, 나머지는 API(Bearer 세션 토큰)로 했다. 결제 provider 호출은 없다. 끝난 뒤 서버 세 개를 내리고 DB 파일을 지웠다.

| # | 시나리오 | 방법 | 결과 |
|---|---|---|---|
| 1 | 관리자 가이드 §5 1–4: Discounts 탭에서 gpt-4 25%, OpenAI 10% 저장 | Playwright(Add model·Add vendor(벤더 Select 미리 선택)·`Save group settings`) | 통과 — `PUT /api/option/` 두 번(`ModelDiscount={"gpt-4":25}`, `VendorDiscount={"OpenAI":10}`) |
| 2 | 마켓플레이스 배지·색 | Playwright `/pricing` | 통과 — gpt-4 `25% off` 앰버(`bg-amber-500/15`), 나머지 3개 모델과 `OpenAI` 벤더 칩 `10% off` 에메랄드(`bg-emerald-500/15`), gpt-4 `$30`/`$60`/`$15` 취소선 → `$22.5`/`$45`/`$11.25` |
| 3 | 모델 할인 요청 과금 | gpt-4 relay 1회 | 통과 — quota 33,750(= 45,000 × 0.75), `other` `discount_multiplier` 0.75·`discount_source` model·`group_ratio` 1 |
| 4 | 벤더 할인 요청 과금 | gpt-4o-mini relay 1회 | 통과 — quota 338(= 375 × 0.9, 반올림), `discount_multiplier` 0.9·source vendor |
| 5 | 로그 상세의 할인 행 | Playwright `/usage-logs/common` → 상세 | 통과 — `Group Ratio 1.0000x` 다음 `Model discount 0.7500x`(gpt-4), `Vendor discount 0.9000x`(gpt-4o-mini) |
| 6 | 100% 할인 모델 | `ModelDiscount={"gpt-4":25,"gpt-4o":100}` → relay gpt-4o | 통과 — `/api/pricing` `discount_percent` 100, 사용자 quota 변화 0, 로그 quota 0, `discount_multiplier` 0·source model·`group_ratio` 1(기저값), 상세 `Model discount 0.0000x` |
| 7 | Timing Breakdown | 3번 요청의 로그·상세 | 통과 — `e2e_ms` 315 / `llm_ms` 303 / `gateway_ms` 12, 상세 `Model Provider 0.3s`, `Gateway Overhead 0.0s (3.8%)`, `Response Time 0.3s` |
| 8 | 채널 재시도 1건 | gpt-3.5-turbo relay(첫 시도 `mock-fail` 500 → `mock-good`) | 통과 — `admin_info.use_channel` [2, 1], `e2e_ms` 710 / `llm_ms` 303 / `gateway_ms` 407(실패한 시도 시간), `pre_llm_ms` 406, 상세 `Gateway Overhead 0.4s (57.3%)`, quota 675(벤더 할인 0.9) |
| 9 | `pre/post_llm_ms` admin 전용 | 조직 회원의 `GET /api/log/self`와 root `GET /api/log/` | 통과 — 회원 `other`에 `e2e_ms`·`llm_ms`·`gateway_ms`·할인 키만 있고 `admin_info` 없음, root는 `admin_info.pre_llm_ms` 보임 |
| 10 | 회원 정지 | root가 `Scn Org`(지갑 5,000,000) 생성 → owner가 미소속 회원 배정·한도 1,000,000 → 회원 relay → owner `PATCH {"member_status":2}` → relay → 재개 → relay | 통과 — 정지 중 relay 403 `organization membership is suspended`(code `access_denied`), `GET /api/organization/subscription/self` 200, `GET /api/user/self` 200(플랫폼 status 1 그대로), 재개 뒤 relay 200, 조직 지갑 5,000,000 → 4,932,500(2 × 33,750) |
| 11 | 조직 사용 로그 화면의 할인·타이밍 행 | — | 미실행 — 같은 `DetailsDialog`라 #5·#7로 갈음. 브라우저 확인은 하지 않음(§5) |
| 12 | realtime(WSS) 요청의 할인·타이밍 | — | 미실행 — mock에 realtime 엔드포인트 없음. `TestPreWssConsumeQuotaAppliesDiscount`, `…OmitsAllWhenNothingWritten`으로만 확인 |
| 13 | 스트리밍 중 클라이언트 끊김 | — | 미실행 — 수동 재현 수단 없음. Review Focus 2 테스트 `TestAppendTimingBreakdown_NegativeGatewayFloorsToZero` |
| 14 | MJ·비동기 작업 과금의 할인 | — | 미실행 — MJ/작업 upstream 없음. `TestRecalculateTaskQuotaByTokensAppliesDiscount` |
| 15 | 결제 provider 결제·만료 경합 | — | 미실행 — provider 계정·sandbox 없음(이 단계의 결제 호출 없음) |
| 16 | MySQL/PostgreSQL에서 같은 흐름 | — | 미실행 — 컨테이너 런타임 없음(§5) |

통과 10, 미실행 6. 결함은 찾지 못했다.

### upstream 병합 리허설

`git fetch qn` 뒤 `qn/main`이 7de10f2c6 → c9f7b6208로 3커밋 움직였다(`4013729f6` 민감어 필터 기본값 끔, `a0bf6a8a4` vertex API Key 모드 project_id, `c9f7b6208` 가격 모델 정렬). develop의 merge-base는 7de10f2c6.

`git merge --no-commit --no-ff qn/main` 출력:
```
Auto-merging web/src/features/pricing/lib/price.ts
Auto-merging web/src/i18n/locales/en.json
Auto-merging web/src/i18n/locales/fr.json
Auto-merging web/src/i18n/locales/ja.json
Auto-merging web/src/i18n/locales/ru.json
Auto-merging web/src/i18n/locales/vi.json
Auto-merging web/src/i18n/locales/zh-TW.json
Auto-merging web/src/i18n/locales/zh.json
Automatic merge went well; stopped before committing as requested
```
- `git diff --name-only --diff-filter=U`: 빈 출력(충돌 0). 병합이 만든 변경은 23파일(`git diff --cached --stat`: 507 insertions, 43 deletions). 참고로 `git diff --stat HEAD..MERGE_HEAD | tail -1`은 `403 files changed, 899 insertions(+), 52494 deletions(-)`인데, 이것은 develop 트리와 upstream 트리의 차이(fork overlay 전체)라 병합 크기가 아니다.
- 충돌 집합 검사: 충돌 파일이 없으므로 "충돌 ⊆ overlay 집합"은 공집합으로 성립한다. 양쪽이 모두 바꾼 파일(자동 병합 8개: `web/src/features/pricing/lib/price.ts`, locale 7개)은 모두 `docs/fork-overlays.md`의 upstream 파일(O6 `price.ts` 행, 공통 i18n 행)이다. upstream이 바꾼 나머지 15파일은 develop이 베이스에서 건드리지 않은 파일이다.
- 자동 병합 결과 확인: 병합 트리의 `price.ts`에 `// fork: O6` 8줄이 그대로 있고(upstream은 `calculateTokenPrice`를 export만 함), 병합 상태에서 `bun run test -- pricing`이 20 파일 / 368 테스트 pass.
- `git merge --abort` 종료 코드 0, MERGE_HEAD 없음, `git status --short` 0줄(문서 수정 전에 리허설했다). 실제 병합은 다음 동기화에서 한다.
