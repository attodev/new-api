# upstream 재기반 단계 E(UI 언어 정리·O11 fork 고유 수정·한국어 이월) 기록

브랜치: develop. 베이스: 778ff22c1(단계 E 계획 커밋). 계획: docs/superpowers/plans/2026-10-15-rebase-phase-e.md. spec: §2.1 O11, §4.8, §4.9, §8 단계 E, §9 결정 15–20.

커밋은 `git log --reverse --format='%h %s' 778ff22c1..HEAD` 기준이고, Task 번호는 ledger(`.superpowers/sdd/2026-10-15-rebase-phase-e/progress.md`)와 맞췄다. upstream 파일의 훅 위치는 모두 `docs/fork-overlays.md`의 `O11`·`O9`·`O8`·`O3` 행과 "O11 fork 고유 수정"·"삭제한 upstream 파일" 절에 있다.

| Task | 커밋 | 내용 |
|---|---|---|
| 1 | 0b81e0b68 | feat(i18n): keep only English and Korean as interface languages |
| 1 | 72c587742 | docs(i18n): describe the en/ko language set in env docs and overlays |
| 2 | 671bd28c9 | fix(gemini): cap image prompt tokens by the cached and audio portions |
| 2 | b27403b32 | fix(gemini): cap image tokens only where settlement prices the cached or audio portion |
| 3 | c46ad9a8b | fix(ali): keep thinking_budget when either the client or the upstream model is Qwen |
| 3 | e8753c600 | fix(relay): keep Ali thinking_budget re-insertion out of ParamOverride and stale bodies |
| 4 | 900288125 | fix(quota): keep check-in and invitation transfers within the wallet ceiling |
| 5 | 10481e3c5 | feat(i18n): translate organization errors returned to AI clients |
| 6 | 495762f4c | feat(web): render organization and top-up expiry audit entries as translated sentences |
| 7 | 6d9432af5 | feat(organization): return structured import row errors and render them translated |
| 8 | (이 문서 커밋) | docs: record phase E of the upstream rebase (languages, O11 fixes, Korean carry-overs) |

## 1. UI 언어 정리 (Task 1)

team 66d421d13과 같은 결정이다(계획 결정 1–2). 화면 언어와 백엔드 메시지 언어는 영어와 한국어 둘이다.

- 지운 upstream 파일 8개(`docs/fork-overlays.md` "삭제한 upstream 파일"): `web/src/i18n/locales/{zh,zh-TW,fr,ja,ru,vi}.json`(각 7,543줄), `i18n/locales/{zh-CN,zh-TW}.yaml`(각 76줄). Task 1 커밋은 49파일, −45,864줄이다. git이 무시하는 `_reports/{fr,ja,ru,vi,zh}.untranslated.json`도 지웠다. upstream이 이 파일을 고치면 modify/delete 충돌이 나고, `git rm`으로 삭제를 유지한다(§7 리허설에서 6건 확인).
- 바뀐 upstream 코드(바뀐 줄마다 `// fork: O9`·`// fork: O8`): `config.ts`(`resources`·`supportedLngs` = en, ko), `languages.ts`(옵션 en·ko, 한 줄 `isKorean` = `/^(kr|ko([-_].*)?)$/i`; `normalizeInterfaceLanguage`·`toIntlLocale`은 한국어가 아니면 `en`, `convertDetectedLanguage`는 한국어 태그만 `ko`로 바꾸고 나머지는 `supportedLngs`가 en으로 떨어뜨림), 달력 두 곳(`{ en: enUS, ko }`), `localized-text.ts` JSDoc, `i18n/i18n.go`(`LangZhCN`·`LangZhTW` 삭제, 번들 기본 `language.English`, `SupportedLanguages()` = `{en, ko}`), `common/constants.go:40-43` 주석, `.env.example:12`·`docker-compose.yml:39`의 `DEFAULT_LANGUAGE` 주석.
- `InterfaceLanguages`: `common.ParseInterfaceLanguages`의 허용 코드는 en·ko(`kr`→`ko`)다. 새 `common.NormalizeInterfaceLanguagesOption`이 저장 형식(알려진 코드만 관리자 순서로, 비면 `en`)을 만들고, `model/option.go:277` 훅 `value = normalizeForkOptionValue(key, value)`가 `UpdateOption`에서 이 키에만 적용한다(DB와 OptionMap 같은 값). 이미 DB에 있는 `en,ko,zhCN` 같은 값은 마이그레이션하지 않고 `/api/status`가 읽을 때 거른다. 설정 화면은 체크박스 2개가 됐다(코드 변경 없음).
- 기존 사용자 fallback(Review Focus 1): 저장된 `zhCN`·`zhTW`·`zh-CN`·`ja`·`fr`·`ru`·`vi`는 영어로, `kr`은 한국어로 열린다. 브라우저 언어가 `zh-CN`인 첫 방문자도 영어다. 백엔드는 `zh-CN` 헤더를 영어로, `DEFAULT_LANGUAGE=ko`면 저장된 `zhCN`도 영어로 처리한다(`TestBackendMessageLanguage`).
- 남긴 것: `README.*.md` 번역본(team과 같음), 서버 로그 언어 `common/log_text.zh-CN.json`(`SetLogLanguage("zh-CN")`은 yaml과 별개로 embed를 읽음, `TestServerLogLanguage` 통과), 콘텐츠 언어 값(`features/task-plugins/lib/plugin-changelog.ts`, `features/models/types.ts`의 `SyncLocale`, `features/models/constants.ts`의 `Japanese`, `lib/localized-text.ts`의 `zhcn`→`zh-cn`). `web/scripts/sync-i18n.mjs:229-236`의 `ja/zh/ru/fr/vi` 분기는 파일이 없으면 돌지 않으므로 충돌면을 줄이려고 그대로 뒀다.
- 지운 locale을 import하던 upstream 테스트 **21개**를 ko.json·한국어 기대값으로 바꿨다(계획은 5개; ruling, §5): import만 바꾼 16개, zhCN/fr Intl·달력 동작을 쓰던 5개(date-picker-locale, inference-status, login-session-item, visual-billing-editor, routing)는 "ko"와 "지운 코드는 영어" 케이스로. 중국어 매핑 테스트 2개(`maps browser BCP-47 Chinese tags onto interface codes`, `keeps already-normalized interface codes stable`)는 지웠다(커밋 본문에 기록). 백엔드는 `middleware/auth_test.go`·`distributor_test.go`의 zh 기대값을 ko로.
- 수정 라운드 1(72c587742): 설정 문서 주석 2곳, `languages.ts`의 표식 누락, overlay의 낡은 문구.

## 2. O11 조사 결과 — 이미 같은 동작이라 다시 올리지 않은 것

spec §2.1의 O11 목록과 team 기록 두 건(`docs/2026-10-13-upstream-sync-phase2.md`, `docs/2026-10-14-followups-2a.md`)을 develop과 대조했다(계획 결정 3–4).

| 항목 | team 출처 | develop의 근거 | 판단 |
|---|---|---|---|
| Epay 콜백 트랜잭션 정산 + 실패 시 `fail` | `7ab5a8aa9` 외 | upstream `50e5377ea`(2026-08-10, "settle recharge orders atomically"): `model.RechargeEpay`(`model/topup.go:182`)가 주문을 `FOR UPDATE`로 잠그고 provider·pending 상태를 확인한 뒤 지급까지 한 트랜잭션에서 한다. `EpayNotify`(`controller/topup.go:413`에서 시작; `RechargeEpay` 호출 :475, 정산 오류의 `fail` 응답 :487, `success` 응답 :500)는 오류면 `fail`, 정산·중복이면 `success` | 같음. not-found·다른 provider·만료 주문에도 `fail`을 답해 Epay가 한도까지 재시도하는 차이는 upstream 그대로(사용자 결정). 조직 Epay 주문이 가득 찬 조직 지갑에 걸리면 주문 저장까지 롤백되어 pending으로 남는다 — `model/topup_target_epay_test.go`로 고정(Task 4, 코드 변경 없음) |
| 결제 전 지갑 수용량 검증(PayPal·조직 포함) | `1fa9b3f1b` | upstream `47ba9d2c6`·`2a0ce3475`(2026-08-14)의 `rejectInvalidCreditedQuota`/`rejectInvalidTopUpQuota`(`controller/topup.go:263, 275`). 진입점: `RequestEpay`(:287에서 시작, `rejectInvalidTopUpQuota` 호출 :299), `RequestAmount`(:518), Stripe `RequestAmount`·`RequestPay`(`topup_stripe.go:61, 103`), Creem `RequestPay`(`topup_creem.go:102`), `RequestWaffoAmount`·`RequestWaffoPay`(`topup_waffo.go:127, 164`), `RequestWaffoPancakeAmount`·`RequestWaffoPancakePay`(`topup_waffo_pancake.go:37, 358`), fork `RequestPayPalPay`·`RequestPayPalAmount`(`topup_paypal.go:326, 840`). 두 함수 안의 단계 B O3 훅 `validateTopUpTargetCapacity`(:266, 278)가 조직 주문은 조직 지갑 상한으로 검사하고, 조직 결제 라우트 11개(`router/organization-router.go:58-68`)는 대상을 정한 뒤 같은 함수를 부른다 | 같음. Toss·자동 충전 진입점은 가져오지 않는 기능(spec §2.2) |
| Gemini가 프롬프트만 보고한 사용량 유지, 스트림 누적 프레임 | `499784e79`, `615d39484` | upstream `bb28f2cca`(tokenkit 이전)·`55a6cd2a4`와 relaykit `HasGeminiUsageMetadataTokens`(`relaykit/dto/billing_usage.go:423`, total 0인 prompt-only chunk도 사용량으로 봄), `MergeGeminiUsageMetadataNonZero`(`relaykit/dto/usage_merge.go:218`, 프레임별 0이 아닌 값 병합) | 같음(upstream이 더 넓음). team의 last-frame-wins·`TotalTokenCount != 0` 판정보다 낫다 |
| gpt-5 이상 Chat 능력(`max_completion_tokens` 등) | `d9a7cc12a` | upstream `49ec46966`·`d0cb7347c`의 `GetOpenAIChatCapabilities`(relaykit/dto/openai_request.go). team 커밋 중 develop에 없는 것은 `controller/channel-test.go:831`의 판정 한 줄(`IsOpenAIReasoningOModel` → `GetOpenAIChatCapabilities(...).UseMaxCompletionTokens`)뿐 | 한 줄 교체도 하지 않는다(계획 결정 4). gpt-5 채널 테스트는 upstream 그대로 `max_tokens: 16`을 보낸다 |
| Claude 5.x / Opus 4.7·4.8 thinking·effort 표 | `followups-2a` Task 1 | upstream relaykit `relayconvert/reasoning/claude.go:50-96`(`claude-fable-5`, `mythos-5`, `opus-5-5`, `sonnet-5-5`, `opus-5`, `sonnet-5`, `haiku-5`, `opus-4-8`, `opus-4-7`; 마지막 변경 `036ba376d`) | upstream 설계(클라이언트가 보낸 `thinking.enabled`·`disabled` native passthrough)를 따른다. team의 재작성은 옮기지 않는다(사용자 결정) |

## 3. 이번에 올린 세 수정 (Task 2–4)

### 3.1 Gemini 이미지 토큰 상한 (Task 2)

문제: Gemini가 보고한 IMAGE modality 토큰이 캐시·오디오 토큰과 겹치면 이미지 비율로 한 번, 캐시·오디오 단가로 또 한 번 청구된다.

as-built(계획 결정 5와 다름 — ruling, §5): 처음 커밋(671bd28c9)은 계획대로 `effectiveBillingUsage`(`service/billing_usage.go`) 한 지점에 상한을 뒀지만, 리뷰가 `cr`을 쓰지 않는 티어 식에서 캐시와 겹친 이미지 토큰이 텍스트 단가로 떨어지는 문제를 찾았다. 수정(b27403b32)은 그 훅을 되돌리고 정산 소비자 두 곳에서 "그 경로가 따로 값을 매기는 몫"만 뺀다.
- 정률 `service/text_quota.go:248`: `capGeminiImagePromptTokens(usage, true, operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName) > 0)` — 캐시는 항상, 오디오는 모델에 Gemini 오디오 입력 단가가 있을 때만(:329-331과 같은 조건).
- 티어 `service/tiered_settle.go:27`(`BuildTieredTokenParams` 첫 줄): `capGeminiImagePromptTokens(usage, usedVars["cr"], usedVars["ai"])` — `pkg/billingexpr/expr.md`의 p 자동 제외 규칙대로.
- helper(`service/gemini_image_cap_fork.go`)는 Gemini canonical usage(`UsageSemantic` gemini)에만 걸리고 `ImageTokens = max(P − [cached] − [audio], 0)`으로 자른 얕은 사본을 돌려준다(원본 불변). 잘린 양은 `TextTokens = max(TextTokens, P − Audio − Image)`로 옮긴다(정산은 TextTokens를 읽지 않음).

수치(테스트 고정값):
- 정률, P=2000, cached=1500, IMAGE 1089, ImageRatio 2 → **1575**(상한 전 develop 2753). ImageRatio 1 → 1075(전 1664).
- 정률, `gemini-2.5-flash`(오디오 단가 있음), P=2000, cached=1000, TEXT 700/IMAGE 800/AUDIO 500 → **1200**(상한 없으면 1500).
- 정률, `gemini-3-flash-preview`(오디오 단가 0), 같은 입력, ImageRatio 2, completion 50 → ImageTokens 800 유지, **2250**(base 200 + cached 250 + image 1600 + completion 200).
- 티어: `p + cr*0.25 + img*2` → **1375**(상한 없으면 2553), `p + img*2` → **3089**(`effectiveBillingUsage` 방식이면 2500), `p + cr*0.25 + img*2 + ai*3`(cached 1000, audio 500) → **2750**, 같은 입력 ai 없이 `p + cr*0.25 + img*2` → **2050**.

옮기지 않은 것: team af1eddc8e의 VIDEO·DOCUMENT → `TextTokens` 귀속(relaykit 영역, 표시 차이뿐 — base가 `P − cached − image − audio`라 금액은 같다). raw usage로 과금하는 `PostAudioConsumeQuota`(service/quota.go)와 채널 테스트 정산(`controller/channel-test.go:544` `settleTestQuota`)은 상한이 없다(§6).

### 3.2 Ali `thinking_budget` either-name (Task 3)

문제: Qwen 클라이언트 모델을 커스텀 배포 이름으로 매핑하면 `thinking_budget`(예: `0` = 생각 끄기)이 업스트림 본문에서 사라진다. develop은 예산을 두 곳에서 지운다: `requestOpenAI2Ali`와 relaykit `GeneralOpenAIRequest.MarshalJSON`(매핑 뒤 `r.Model` 기준, 규칙 6으로 수정 불가).

as-built(계획 결정 6과 다름 — ruling, §5):
- `relay/channel/ali/text.go:12`: `requestOpenAI2Ali(request, keepThinkingBudget bool)`; 클라이언트 이름(`OriginModelName`)이나 매핑 뒤 이름 중 하나가 Qwen/QwQ면 유지(이름이 비면 upstream처럼 `request.Model`).
- `adaptor.go:146`(`ConvertOpenAIRequest`): 유지인데 마샬러가 지울 경우(매핑 뒤 이름이 Qwen이 아님)에만 raw 값(`json.RawMessage`, `0`도 그대로)을 gin 키 `fork_ali_thinking_budget`에 둔다. 매번 키를 다시 써서 재시도에 낡은 값이 남지 않는다.
- `adaptor.go:186`(`DoRequest` → `doAliRequest`): 본문에 필드가 없을 때만 `sjson`으로 다시 넣는다. 넣지 않는 경우: 본문 패스스루(전역·채널), `info.RelayMode`가 chat completions가 아님(Responses 경유 포함), 본문 `model`이 `info.UpstreamModelName`과 다름, **채널 ParamOverride를 마샬한 문자열에 `thinking_budget`이 있음**(운영자가 지운 필드는 되살아나지 않고, 값을 넣는 override는 그대로 이김).
- 예: `qwen3-max` → `my-qwen-deploy`, `thinking_budget: 0`/`128` → 업스트림 본문에 `0`/`128`. 같은 매핑에 ParamOverride `{"operations":[{"mode":"delete","path":"thinking_budget"}]}` → 필드 없음. `{"thinking_budget":7}` → 7. 테스트 11개, 변이 검사 4개(override 가드·relay mode·model 일치·convert 시 reset을 각각 끄면 실패).

### 3.3 출석·초대 전송의 지갑 상한 (Task 4)

upstream에는 없지만 team 2ba5cc513이 막은 구멍이라 fork로 유지한다(계획 결정 7). 두 경로 모두 새 쿼터를 만들므로 `common.MaxWalletQuota`를 넘지 못한다.
- `model/checkin.go:103`: `userCheckinWithTransaction`의 쿼터 UPDATE를 `creditCheckinReward(tx, userId, quotaAwarded)`로(원자적 `quota <= max` UPDATE, 초과면 `ErrCheckinWalletLimit` = "Check-in failed: wallet quota limit reached", 출석 행도 롤백되어 다시 시도 가능).
- `model/checkin.go:124`: SQLite 경로 `userCheckinWithoutTransaction`도 같은 트랜잭션을 부른다(수동 롤백 `DB.Delete(checkin)`과 상한 없는 `IncreaseUserQuota`가 사라짐, −15/+1).
- `model/user.go:618`: `TransferAffQuotaToQuota`에서 잠근 행의 `user.Quota`로 `checkAffTransferCeiling` 검사(`ErrAffTransferWalletLimit`).
- 프런트 키 2개(en·ko): `체크인 실패: 지갑 쿼터 한도에 도달했습니다`, `초대 쿼터 전송 실패: 지갑 쿼터 한도에 도달했습니다`.

## 4. 한국어 이월 (Task 5–7)

단계 D §6이 넘긴 한국어 공백 세 가지를 닫았다.

- **AI 클라이언트로 가는 조직 오류(Task 5, 계획 결정 8):** `i18n/keys_fork.go`의 키 9개를 upstream `en.yaml` 끝과 `ko.yaml` 끝의 `# fork: O3 — organization relay errors` 블록에 넣었다(각 66 → 75키). `organization.disabled`, `member_suspended`, `wallet_insufficient`, `member_quota_insufficient`, `mj_plan_unsupported`, `quota_insufficient`, `quota_insufficient_remaining`(ruling), `pre_charge_insufficient`·`wallet_reserve_insufficient`(ruling). `service/organization_relay_errors_fork.go`의 `organizationError`가 요청이 밝힌 언어(`i18n.StatedLang(c)`, 세션 경로는 세션 `lang`)로 렌더하고, `localizedError.Unwrap()`으로 `errors.Is`(조직 sentinel)를 지킨다. 영어 값은 기존 문구와 같아 Accept-Language 없는 클라이언트의 응답은 바이트가 같다. 훅: `service/billing_session.go:235`(`c` 전달), `relay/mjproxy_handler.go:229, 556`(둘 다 `c` 전달, ruling), `i18n/i18n_test.go:139`(`forkDeclaredKeys`). realtime WSS(`quota_organization.go`)와 `prepareMidjourneyOrganizationBilling` 오류는 로그로만 가므로 영어로 둔다.
- **감사 로그(Task 6):** `web/src/features/usage-logs/lib/fork-audit-templates.ts`의 14개 액션(`organization.create` … `organization.topup.expire`, `topup.expire`)을 `format.ts:31, 453` 훅(`...FORK_AUDIT_TEMPLATES` — upstream 키가 뒤에 와서 충돌 시 upstream이 이김)으로 `AUDIT_TEMPLATES`에 넣었다. 템플릿 변수는 Go 호출부에서 항상 있는 파라미터만 쓴다. 키 14개(en·ko).
- **Excel 가져오기 행 오류(Task 7, 계획 결정 9):** `service/org_import_errors_fork.go`의 `OrgImportRowError{line, username, message_key, params}`와 `addRowError`로 23곳을 바꿨다. 응답은 기존 `errors []string`(영어 문장이 바이트 단위로 같음 — 18개 형태 테스트)에 `error_details`를 더한다. 중첩 오류가 `*common.Message`면 `error_key`·`error_params`로 함께 번역한다. 프런트 `lib/org-import-row-error.ts`가 `{{line}}행 (사용자): <번역>`으로 렌더하고, `error_details`가 없으면 `errors`를 그대로 보여 준다. 키 23개(en·ko), 값이 `{{error}}`뿐인 키 하나는 skip 목록에.

키 수: ko.json·en.json **7,578키**(단계 D 끝 7,539 + Task 4 2 + Task 6 14 + Task 7 23), skip 목록 184(+1, `{{error}}`).

## 5. 결정 사항

계획 결정(계획 하단 1–12): 1–4, 7–12는 §1–4·§6에 반영했고 spec §9 결정 15–20으로 옮겼다. 결정 5와 6은 실행 중 ruling으로 바뀌었다(아래 as-built).
- 결정 5(문안: Gemini 상한은 `effectiveBillingUsage` 한 지점, `PostAudioConsumeQuota`는 상한 밖) → as-built: 상한은 소비자별로 `service/text_quota.go:248`(정률)와 `service/tiered_settle.go:27`(티어, 식이 따로 값을 매기는 `cr`·`ai` 몫만)에 있고, `effectiveBillingUsage`는 upstream 그대로다. `TextTokens` VIDEO·DOCUMENT 표시 차이와 `PostAudioConsumeQuota`·채널 테스트 경로의 상한 없음은 문안대로다.
- 결정 6(문안: Ali `DoRequest`에서 다시 넣고 `ParamOverride`·`RemoveDisabledFields`가 못 보는 점은 기록만) → as-built: 채널 ParamOverride가 `thinking_budget`을 언급하면 다시 넣지 않아 그 사각은 닫혔다. 다시 넣기는 chat completions 본문이면서 `model`이 업스트림 이름과 같을 때만 한다. `RemoveDisabledFields`는 마샬러가 지울 필드에는 영향이 없다(이미 없는 필드).
- 결정 12: Redis 쿼터 캐시 TTL은 이 단계에서 upstream 상태로 두고 다음 단계에서 따로 다룬다(§6 첫 항목).

실행 중 ruling(ledger `Ruling` 줄, 오래된 순; 대가 = 틀렸을 때 비용):
- (Task 1) 지운 locale을 import하던 upstream 테스트 21개를 ko로 다시 가리키는 것을 수용한다(지워도 modify/delete로 충돌). 대가: 동기화 때마다 충돌 집합에 upstream 테스트 파일이 더 들어온다.
- (Task 2) 상한은 정산이 따로 값을 매기는 몫만 뺀다 — 정률은 캐시 항상·오디오는 오디오 입력 단가가 있을 때, 티어는 식이 `cr`(과 expr.md의 캐시 쓰기 변수)을 쓸 때 캐시·오디오 변수를 쓸 때 오디오 — 그래서 `effectiveBillingUsage` 한 곳 대신 두 소비자에 플래그와 함께 둔다. 대가: Gemini 티어 식의 이미지 줄이 team의 정률 전용 상한과 다르다.
- (Task 3) 채널 ParamOverride가 `thinking_budget`을 언급하면 stash·재삽입을 건너뛴다(계획의 "기록만 하는 사각"을 뒤집음 — 운영자의 명시적 삭제가 이겨야 하고, 수정은 fork 파일만). 대가: 이 부분 문자열을 포함하는 다른 키만 바꾸거나 넣는 override가 있는 채널은 either-name 유지를 잃는다(운영자가 override를 빼면 됨).
- (Task 3) m1(chat completions 본문에만 재삽입)과 m2(convert 시 reset 테스트)를 수정 라운드 1에 넣고 m3(`text.go:12` 훅이 두 줄)는 overlay에 기록만 한다. 대가: 테스트 하나 더.
- (Task 5) 두 번째 MJ 훅 `relay/mjproxy_handler.go:556`도 `c`를 넘긴다(브리프는 :229만, 두 호출부 모두 메시지를 클라이언트에 돌려줌). 대가: 없음(같은 한 줄 훅).
- (Task 5) `billing_session_organization.go:38/165`용 키 `organization.quota_insufficient_remaining`(`{{.Remaining}}`)을 더하고 `organization.quota_insufficient`는 MJ용으로 파라미터 없이 둔다(남은 양을 빼면 지금 클라이언트가 보는 정보를 잃음). 대가: yaml 키 한 쌍.
- (Task 5) 번역한 오류도 모델 sentinel에 대한 `errors.Is`를 지킨다(`Unwrap`으로 감쌈). 대가: fork 파일의 작은 wrapper 타입.
- (Task 5) 추가 키 `organization.pre_charge_insufficient`·`wallet_reserve_insufficient`를 수용한다 — 둘 다 relay 403 응답에 나간다. 대가: yaml 키 두 쌍.
- (Task 8) 계획 결정 5·6이 낡은 곳은 as-built를 기록한다(Gemini 상한은 `text_quota.go`·`tiered_settle.go` 소비자, Ali ParamOverride 가드가 기록된 사각을 닫음) — ledger ruling이 계획 문안보다 우선한다. 대가: 문서 수정.

이미 확정해 둔 사실(재확인):
- 조직 로그·대시보드는 현재 멤버의 전체 기록을 보여 준다(계획 결정 10, 사용자 결정). 조직에 넣는 사용자는 새 계정이거나 거의 새 계정이라고 가정한다.
- team 브랜치(`TimingResponseWriter.Unwrap()` 역이식 등)는 이 계획에서 다루지 않았다(계획 결정 11).

## 6. 다음 단계와 단계 F로 넘기는 것

**바로 다음 단계(단계 F 전): Redis 쿼터 캐시 TTL**(계획 결정 12). 지금은 upstream 상태다(`SYNC_FREQUENCY` 기본 60초, 사용 중 TTL 갱신 없음). 이것만 따로 다룬다: team `10ade7045`(배치 대기 중 DB 재수화 거부), `cf2cf300f`(예약·환불 때 TTL 갱신), `9ef93eb8c`(`QUOTA_CACHE_TTL` 기본 600초)와 develop의 배치 모드 재수화 구멍(캐시 만료가 배치 반영 전에 오면 오래된 DB 잔액으로 다시 채워짐)을 테스트로 재현한 뒤 선택지 A–D 중 하나로 정한다. 세션 캐시와 인증 fence가 사용자 캐시 TTL을 공유한다는 점도 그때 함께 정한다.

이 단계의 미뤄 둔 minor(ledger `minor (deferred)`):
- (Task 1) `routing.test.tsx`의 "updates formatting after a language switch"는 en과 ko가 같은 자리 구분(`100,000`)이라 언어 전환 뒤 형식 차이를 더는 구별하지 못한다.
- (Task 2) `PostAudioConsumeQuota`와 채널 테스트 정산(`settleTestQuota`)은 raw usage로 과금해 Gemini 이미지 상한이 걸리지 않는다(두 곳의 티어 분기도 raw usage에 `UsageSemantic`이 없어 마찬가지). Gemini `TextTokens`의 VIDEO·DOCUMENT 표시 차이(relaykit `addGeminiInputTokenDetail`이 버림 — detail 합이 PromptTokens보다 작을 수 있음, 금액 영향 없음)도 그대로다.
- (Task 3) Responses API 경로(`ConvertOpenAIResponsesRequest`)는 여전히 매핑 뒤 이름으로 `thinking_budget`을 지운다(team도 다루지 않음). `text.go:12` 훅은 두 줄에 걸친다(overlay 행이 둘 다 적음).
- (Task 4) upstream 주석 `model/checkin.go:51-52, 84`는 여전히 옛 SQLite 수동 롤백 경로를 설명한다(upstream hunk를 늘리지 않으려고 둠, overlay 행에 위임을 적음). `userCheckinWithoutTransaction`이라는 이름도 이제 맞지 않는다.
- (Task 5) `{{.Error}}` 파라미터는 영어 sentinel 문구라 ko 메시지에 영어가 섞이고(예: `멤버 쿼터가 부족합니다: wallet quota insufficient`), 정산 경로의 영어 문구는 같은 구절을 되풀이한다. 언어를 밝히지 않은 요청의 테스트는 `DefaultLanguage=en`을 전제한다.
- (Task 6) 할당 문장의 `organization_role`이 enum 원문으로 렌더된다. 템플릿 키에 굽은 따옴표(“ ”)를 쓴다. ko 테스트는 14개 중 12개를 한글 포함으로만 확인한다.
- (Task 7) `ko-locale.mjs`의 `MANUAL_SKIP`에 있는 `{{error}}`는 `ko-skip.json` 항목과 중복이다. `org-users-import-dialog.test.tsx:77`의 테스트 이름이 fallback 범위를 과장한다.
- (Task 6·7 관찰) 새 템플릿·행 오류 키는 동적으로만 참조된다(`static-keys.ts`에 없음). `i18n:sync`는 소스를 훑지 않아 지우지 않는다(리뷰가 확인, 이번 검증의 sync도 locale md5 불변).

단계 C·D에서 넘어와 이번에도 다루지 않은 것:
- 할인: MJ·태스크·위반 수수료 로그의 할인 항, 동적가격 모델의 정가 표시, 할인 편집기의 중복 이름·전환 시 행 손실.
- 타이밍: `doRequest`를 우회하는 어댑터(`DoWssRequest`, AWS SDK, coze 폴링)의 `llm_ms` 공백.
- 과금·조직: 무료 모델(`EnableFreeModelPreConsume` 끔) 세션 우회와 tiered 자동 그룹 가장자리, root의 조직 생성 시 owner 잔액 미검사, 이미 열린 realtime WebSocket은 정지해도 끊기지 않음, WSS 조직 지갑 사전 검사의 소폭 초과, 떠난 회원 사용량을 조직 기록에 남기기·조직 대시보드 기간 제한(가입 시점 제한은 하지 않기로 함 — 계획 결정 10), 단계 B 이전 `users.remark`의 조직 메모를 `organization_remark`로 옮길지(운영자 결정).
- 언어: team 사용자의 `language: 'kr'`을 `ko`로 바꾸는 데이터 마이그레이션(지금은 읽기 시점 정규화), 지운 언어 코드가 남은 `InterfaceLanguages` DB 값(다음 저장 때 정규화, 읽을 때 거름), `<html lang>`이 ko에서도 `en`, ISO 639-2 `kor` 미매핑, shadcn sr-only 영어, 남은 경계 값·용어 혼용(Billing 결제/청구, `Compact`), API 키 화면 빈 상태 설명 잘림(화면 확인 필요), README 문서 링크 `docs.newapi.ai/en`.
- 수동 DB 수정으로 지운 벤더를 가리키는 모델의 이전 벤더명·할인 유지, 1분 넘은 스냅숏의 백그라운드 벤더명 재구축 빈도.
- team 브랜치의 `TimingResponseWriter.Unwrap()` 결함은 team 쪽에서 별도로 확인한다(계획 결정 11).

단계 D §6에서 이번에 닫힌 것: 다른 6개 locale의 영어 placeholder(파일 삭제), relay·과금 조직 오류의 `ko.yaml`(Task 5), 감사 로그 M6(Task 6), Excel 행 오류(Task 7), `InterfaceLanguages` 쓰기 시점 검증(Task 1 정규화 훅)과 첫 방문 시 8개 언어 메뉴(이제 2개).

최종 리뷰에서 나온 minor(수정 wave에서 출석 보상 한도 오류 표시만 고쳤고 나머지는 기록만 한다):
- (Gemini 상한 표시) 식에 `cr`이 없는 티어 과금에서 로그의 이미지/텍스트 분해(`service/text_quota.go:248`의 정률 상한으로 만든 `summary`, 캐시는 항상 뺌)가 티어 식이 실제로 값을 매긴 토큰(`service/tiered_settle.go:27`)과 다를 수 있다. 금액은 맞고 표시만 다르다. 예: P=2000, cached=1500, IMAGE=1089이면 로그는 이미지 500토큰, 티어는 1089로 계산. 나중에 고친다면: 티어 과금이 적용됐을 때 티어 상한을 건 usage의 `ImageTokens`/`TextTokens`를 `summary`에 복사한다.
- (언어) `i18n.ParseAcceptLanguage`(`i18n/i18n.go:~177`, upstream)는 Accept-Language의 첫 태그만 따른다. `zh-CN, ko;q=0.9`이면 웹 UI는 한국어(i18next가 `navigator.languages`를 차례로 본다)지만 relay 오류는 영어가 된다. 이후 단계에서 fork 훅 한 줄로 다룰 후보.

환경:
- MySQL/PostgreSQL 미실행(컨테이너 런타임 없음). 이 단계는 스키마 변경이 없다. 새 쿼리는 출석의 원자적 `quota <= max` UPDATE(기존 `creditTopUpQuota` 재사용, GORM)뿐이고 세 DB 공통이다. 출석 트랜잭션 경로는 SQLite에서 직접 실행해 확인했다(Review Focus 4).
- 결제 provider sandbox 미실행.

## 7. 검증

### 전체 검증 (Task 8, 2026-10-15)

HEAD 6d9432af5(작업 트리 깨끗)에서 문서 수정 전에 실행했다.
- `GOWORK=off go vet ./...`: 출력 없음, 종료 코드 0.
- `GOWORK=off go test ./... -count=1`: 종료 코드 0, `ok` 44 / `no test files` 39 / FAIL 0(`grep -E "^(FAIL|---)"` 0줄). 캐시를 쓴 첫 실행도 같았다. 이미 있던 upstream 실패는 없다.
- `GOWORK=off go test -race ./model/ ./service/ -run 'Checkin|Gemini|Vendor' -count=1`: `ok model 2.2s`, `ok service 1.9s`.
- `bun run typecheck`(`tsgo -b`): 종료 코드 0. `bun run test`: **215 파일 / 2,430 테스트 pass**(단계 D 끝 213 / 2,417 → Task 1 213 / 2,409 → 새 테스트 파일 2개 `fork-audit-templates`·`org-users-import-dialog`). `bun run build`: 성공, `routeTree.gen.ts` 변경 없음.
- `node scripts/ko-locale.mjs check`: `7578/7578 ok`(종료 코드 0). `status`: `english placeholders left: 0`. `node --test scripts/ko-locale.test.mjs`: 17/17.
- `bun run i18n:sync`: 종료 코드 0, en.json·ko.json md5 불변(지운 locale을 다시 만들지 않음). `_reports/ko.untranslated.json` 22키(skip 목록, 단계 D와 같음; git 무시).
- `bun run e2e`: 새 SQLite(scratchpad, 저장소 밖), 이 트리로 `go build`한 바이너리, `POST /api/setup` root/12345678, rate limit 3종 끔, `SQL_DSN`·`LOG_SQL_DSN`·`REDIS_CONN_STRING` 해제, `E2E_REUSE_SERVER=1`(dev 서버 3101은 Playwright가 띄움)로 **21 passed (47.1s)** — 조직 20 + 한국어 1. 끝난 뒤 DB에는 사용자 1명(root)과 조직 0개, 서버를 내리고 DB를 지웠다.

### upstream 병합 리허설 (2026-10-15)

`git fetch qn` 뒤에도 `qn/main`은 **718a4e7f9**(단계 D 리허설과 같음)이고 merge-base는 7de10f2c6이다. HEAD 6d9432af5, 작업 트리가 깨끗할 때 했다.

`git merge --no-commit --no-ff qn/main` 출력(종료 코드 1):
```
Auto-merging go.mod
Auto-merging go.sum
CONFLICT (content): Merge conflict in go.sum
Auto-merging service/text_quota.go
Auto-merging web/src/features/pricing/lib/price.ts
Auto-merging web/src/i18n/locales/en.json
CONFLICT (modify/delete): web/src/i18n/locales/fr.json deleted in HEAD and modified in qn/main.  Version qn/main of web/src/i18n/locales/fr.json left in tree.
CONFLICT (modify/delete): web/src/i18n/locales/ja.json deleted in HEAD and modified in qn/main.  ...
CONFLICT (modify/delete): web/src/i18n/locales/ru.json deleted in HEAD and modified in qn/main.  ...
CONFLICT (modify/delete): web/src/i18n/locales/vi.json deleted in HEAD and modified in qn/main.  ...
CONFLICT (modify/delete): web/src/i18n/locales/zh-TW.json deleted in HEAD and modified in qn/main.  ...
CONFLICT (modify/delete): web/src/i18n/locales/zh.json deleted in HEAD and modified in qn/main.  ...
Automatic merge failed; fix conflicts and then commit the result.
```

충돌 **7건**, 모두 overlay 표에 있다:

| 파일 | 종류 | 분류 | 리허설 트리에서의 해소 |
|---|---|---|---|
| `go.sum` | content | O4 `go.mod, go.sum` 행(단계 D와 같은 moejs checksum 인접 충돌: develop alpha.6 vs upstream alpha.7) | `git checkout --theirs go.sum` + `GOWORK=off go mod tidy` → `go.mod`·`go.sum` 각 2줄 차이, excelize 줄 유지, `GOWORK=off go build ./...` 성공 |
| `web/src/i18n/locales/{fr,ja,ru,vi,zh-TW,zh}.json` | modify/delete(6) | "삭제한 upstream 파일" | `git rm` 6개로 모두 풀림. yaml 2개는 upstream이 건드리지 않음 |

- 병합이 만든 변경: 35파일, 1,752 insertions, 266 deletions(`git diff --cached --stat`). 양쪽이 모두 바꾼 파일 중 충돌 없이 합쳐진 `go.mod`, `service/text_quota.go`, `web/src/features/pricing/lib/price.ts`, `en.json`도 모두 표에 있다(O4, O3 `text_quota.go:450`·O11 `:248`, O6 `price.ts`, 공통 (i18n)). `text_quota.go`(표식 2개, 병합 뒤 :252 O11·:454 O3)와 `price.ts`(8개)의 표식 수는 병합 전과 같다. 표에 없는 충돌(기록되지 않은 훅)은 없다.
- en.json: 7,578 → 7,585키(upstream +9: 가격 정렬 6, `Vertex AI Project ID`, `e.g., my-project-123`, Vertex 리전 안내 1; −2: `Price: High to Low`·`Low to High`). fork 키(`Interface languages`, `Access token`, `Line {{line}}`, 지갑 상한 키 등)는 남았다. 병합 트리에서 지운 locale을 import하는 upstream 테스트는 없었다.
- ko 게이트: 리허설 트리(sync 전)에서 `bun run test -- ko-coverage`가 **1 failed | 1 passed**로 실패했고(새 9키 `missing`), `node scripts/ko-locale.mjs check`는 `7576/7585 ok`(종료 코드 1, 같은 9키 `missing`). 실제 병합 뒤에는 overlay O9 메모대로 sync하고 9키를 번역해 넣는다.
- `git merge --abort` 종료 코드 0, `MERGE_HEAD` 없음, `git status --short` 0줄, HEAD 6d9432af5 그대로, locale 디렉터리는 en·ko만. 실제 병합은 다음 동기화에서 한다.

### overlay 표 검증

`grep -rn "fork: O" --include='*.go' --include='*.ts' --include='*.tsx' --include='*.yaml' --include='*.yml' --include='*.mjs' --include='*.md' --include='*.example' .`(node_modules·dist·`.superpowers`·`docs/` 제외)의 표식 **839개**를 스크립트로 `docs/fork-overlays.md`와 대조했다(qn/main에 있는 파일 = upstream 파일; 표식이 행의 줄 범위 안에 있거나, 주석만 있는 표식 줄이 범위 바로 윗줄이면 맞음).
- upstream 파일의 표식 543개: 줄 범위와 맞음 418, 줄 번호 없이 파일·함수로 적은 행에 속함 125, 표에 없음 0, 줄 drift 0. fork 파일의 표식 296개(110파일)는 행 대상이 아니다.
- merge-base 7de10f2c6 이후 바뀐(추가 아닌) upstream 파일 185개(삭제 8 포함)가 모두 표에 있다(일부는 같은 행의 상대 경로 `api.ts`, `task-logs-filter-bar.tsx` 등).
- 고친 것: O3 `service/text_quota.go:449` → **450**(Task 2의 O11 한 줄이 :248에 들어가 아래 줄을 민 것; ±1 허용 대조에서는 보이지 않아 엄격 대조로 찾음). O3 usage-logs 행(`log-cost-display.tsx`, `details-dialog.tsx`)에 줄 번호가 없어 `details-dialog.tsx:510, 1307`의 O3 표식이 O6·O7 행 범위 밖으로만 보였다 → `log-cost-display.tsx:85`, `details-dialog.tsx:510, 1307-1314`를 적었다. "삭제한 upstream 파일" 절에 이번 리허설 결과(modify/delete 6건 → `git rm`)를 더했다. O11 절·삭제 파일 절·이번 단계의 O3(Task 5·6)·O8(`option.go:277`)·O9(en/ko 축소) 행은 Task 1–6이 이미 넣었고 줄 번호가 맞았다. Task 7은 fork 파일만 바꿔 새 행이 없다.
