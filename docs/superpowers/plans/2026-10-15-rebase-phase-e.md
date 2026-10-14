# Upstream Rebase 단계 E 구현 계획 — UI 언어 정리, O11 fork 고유 수정, 단계 D 이월

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `develop`의 화면·백엔드 언어를 team처럼 영어와 한국어 둘로 줄이고, team이 2026-10에 고친 fork 고유 수정 중 upstream 베이스에 아직 없는 세 가지(Gemini 이미지 토큰 이중 과금, Ali `thinking_budget` 소실, 출석·추천 적립의 지갑 상한)를 다시 올리고, 단계 D가 넘긴 한국어 공백(AI 클라이언트로 가는 조직 오류, 감사 로그, Excel 가져오기 행 오류)을 닫는다.

**Architecture:** 언어 정리는 upstream locale 파일을 지우고 언어 목록·감지·달력·백엔드 번들을 en/ko만 남기도록 고친다. O11 조사 결과 Epay 트랜잭션 정산과 결제 전 수용량 검증은 upstream이 이미 같은 방식으로 고쳤으므로 다시 적용하지 않는다(스펙 §2.2). 남은 수정은 모두 "upstream 파일에 훅 한 줄 + fork 파일" 규칙으로 넣는다. Gemini 상한은 relaykit을 건드리지 않고(규칙 6) 호스트 정산 진입점 `effectiveBillingUsage`에서, Ali는 Ali 어댑터 안에서 한다. 한국어 이월은 기존 메커니즘(`i18n.T` + yaml, `AUDIT_TEMPLATES`, `common.Message` 키)을 쓴다.

**Tech Stack:** Go 1.26(`GOWORK=off`), Gin, GORM(SQLite/MySQL/PostgreSQL), go-i18n v2, sjson/gjson, React 19 + i18next, vitest.

**Spec:** `docs/superpowers/specs/2026-10-14-upstream-rebase-design.md` §3(규칙 1–6), §4.8(O9), §4.9(O11), §8 단계 E. 이월 출처: `docs/2026-10-15-rebase-phase-d.md` §6, `docs/2026-10-14-rebase-phase-c.md` §5. team 출처: 커밋 `af1eddc8e`, `c20c6d664`, `afbf87cd3`, `70b14d049`, `2ba5cc513`, `66d421d13`(team의 언어 정리), 기록 `git show team:docs/2026-10-13-upstream-sync-phase2.md`, `git show team:docs/2026-10-14-followups-2a.md`.

## Global Constraints

- 작업 트리 `/Users/molla/work/ai/new-api-rebase`, 브랜치 `develop`. `/Users/molla/work/ai/new-api`(team 체크아웃)와 team 브랜치는 읽기 전용이다. team 소스는 `git show team:<path>`/`git show <sha>`로 읽는다.
- Go는 항상 `GOWORK=off`. JSON은 `common.Marshal/Unmarshal/DecodeJson*`만 쓴다. SQLite/MySQL/PostgreSQL을 동시에 지원한다.
- upstream 파일 수정은 훅 한 줄(또는 상수·필드·map 항목 한 줄)로 제한하고 `// fork: O11` / `// fork: O3` / `// fork: O9` / `// fork: O8` 주석을 단다(yaml은 `# fork: O9`). 로직은 fork 파일에 둔다. 모든 훅과 삭제한 upstream 파일은 `docs/fork-overlays.md`에 기록한다.
- relaykit(`relaykit/`)은 upstream 그대로 둔다(규칙 6).
- Task 1 이후 화면 언어는 en과 ko 둘이다. 새 프런트 키는 `web/src/i18n/locales/en.json`(값은 키와 같음)과 `ko.json`(번역)에만 넣는다. 백엔드 yaml은 `en.yaml`과 `ko.yaml`만 있다. 넣은 뒤 `cd web && node scripts/ko-locale.mjs check`가 전부 ok이고 `bun run test -- ko-coverage`가 통과해야 한다.
- 한국어 문자열은 `docs/translation-glossary.ko.md`의 용어와 문체를 따른다.
- `web/src`의 새 TS/TSX 파일은 1행에 AGPL 헤더를 둔다. 손댄 파일마다 `bunx oxlint -c .oxlintrc.json ./src/...`와 `bunx oxfmt --check ./src/...`에서 새 문제가 없어야 한다. `bun run format`은 쓰지 않는다.
- 커밋 트레일러: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. team에서 옮긴 커밋은 `Ported-From: team <sha>`를 추가한다.
- 테스트는 SQLite in-memory(각 패키지의 기존 `TestMain`)로 한다. MySQL/PostgreSQL DSN 테스트와 결제 provider sandbox는 운영자 작업으로 기록한다.

## Review Focus

1. **중국어·일본어 등으로 언어를 저장해 둔 기존 사용자:** 그 언어가 사라진 뒤에도 화면이 깨지거나 키 문자열이 보이면 안 된다. 영어로 열려야 한다. 브라우저 언어가 `zh-CN`인 첫 방문자도 영어다. → Task 1 `languages.test.ts` "falls back to English for removed languages".
2. **캐시가 대부분인 Gemini 이미지 요청:** 이미지 토큰이 캐시·오디오와 겹치면 이미지 비율로 두 번 청구되면 안 된다. 예: P=2000, cached=1500, IMAGE 1089, ImageRatio 2 → 1575(지금 develop은 2753). → Task 2 `TestEffectiveBillingUsageCapsGeminiImageByCachedAndAudio`와 quota 테스트.
3. **Qwen 클라이언트 모델을 커스텀 배포 이름으로 매핑한 채널:** `thinking_budget: 0`(생각 끄기)이 upstream 본문에 남아야 한다. → Task 3 `TestAliDoRequestKeepsThinkingBudgetForQwenClientMappedToCustomName`.
4. **지갑 상한 근처 사용자의 출석:** 보상이 거부되면 그날 출석 기록도 남지 않아야 한다(다시 시도 가능). MySQL/PostgreSQL 경로(트랜잭션)를 SQLite에서도 직접 실행해 확인한다. → Task 4 `TestUserCheckinWithTransactionAtWalletCeilingRollsBack`.
5. **비활성 조직 회원의 API 호출(Accept-Language: ko):** AI 클라이언트가 받는 403 본문이 한국어여야 한다. → Task 5 `TestOrganizationRelayErrorsAreTranslated`.

---

## 파일 구조

| 영역 | 생성(fork) | 수정(훅)·삭제 |
|---|---|---|
| UI 언어 정리 | — | 삭제 `web/src/i18n/locales/{zh,zh-TW,fr,ja,ru,vi}.json`, `i18n/locales/{zh-CN,zh-TW}.yaml`. 수정 `web/src/i18n/{config.ts,languages.ts,languages.test.ts}`, `web/src/components/{date-picker,datetime-picker}.tsx`, `web/scripts/sync-i18n.mjs`, 다른 locale을 import하는 테스트 5개, `i18n/i18n.go`, `i18n/i18n_test.go`, `common/constants.go`, `common/interface_languages_fork.go`(+test), `web/src/lib/interface-languages.ts`(+test), `model/option.go`(쓰기 시점 정규화) |
| Gemini 상한 | `service/gemini_image_cap_fork.go`, `service/gemini_image_cap_fork_test.go` | `service/billing_usage.go:19-24` |
| Ali 예산 | `relay/channel/ali/thinking_budget_fork.go`, `relay/channel/ali/thinking_budget_fork_test.go` | `relay/channel/ali/text.go:12-19`, `relay/channel/ali/adaptor.go:146,185-187`, `relay/channel/ali/adaptor_test.go:22-60` |
| 지갑 상한 | `model/wallet_ceiling_fork.go`, `model/wallet_ceiling_fork_test.go`, `model/topup_target_epay_test.go` | `model/checkin.go:103-104,133-136`, `model/user.go:617-618` |
| relay 조직 오류 | `i18n/keys_fork.go`, `service/organization_relay_errors_fork_test.go` | `i18n/locales/en.yaml`(끝에 fork 블록), `i18n/locales/ko.yaml`, `service/billing_session_organization.go`, `service/midjourney_organization.go`, `service/quota_organization.go`, `relay/mjproxy_handler.go:229`(기존 fork 훅) |
| 감사 로그 | `web/src/features/usage-logs/lib/fork-audit-templates.ts`, `web/src/features/usage-logs/__tests__/fork-audit-templates.test.ts` | `web/src/features/usage-logs/lib/format.ts:451`(spread 한 줄), en/ko locale |
| Excel 행 오류 | `service/org_import_errors_fork.go` | `service/org_user_export.go`(fork 파일), `web/src/features/organizations/{types.ts,components/org-users-import-dialog.tsx}`, en/ko locale |
| 기록 | `docs/2026-10-15-rebase-phase-e.md` | `docs/fork-overlays.md`, spec §4.8·§4.9·§9 |

---

### Task 1: 영어·한국어 외 UI 언어 제거와 `InterfaceLanguages` 정리

**Files:**
- Delete: `web/src/i18n/locales/zh.json`, `zh-TW.json`, `fr.json`, `ja.json`, `ru.json`, `vi.json`; `i18n/locales/zh-CN.yaml`, `zh-TW.yaml`
- Modify: `web/src/i18n/config.ts:23-48`, `web/src/i18n/languages.ts:19-100`, `web/src/i18n/languages.test.ts`, `web/src/components/date-picker.tsx:20,33-40`, `web/src/components/datetime-picker.tsx:21,36-43`, `web/scripts/sync-i18n.mjs:229-236`, 테스트 5개(`usage-logs/components/__tests__/mobile-filter.test.tsx`, `usage-logs/audit/__tests__/viewer.test.tsx`, `models/__tests__/model-listing.test.tsx`, `system-settings/auth/__tests__/oauth-settings.test.tsx`, `channels/components/__tests__/model-mapping-batch-dialog.test.tsx`), `i18n/i18n.go:19-24,40,44,54-57,191-215`, `i18n/i18n_test.go`, `common/constants.go:39-43`, `common/interface_languages_fork.go`, `common/interface_languages_fork_test.go`, `web/src/lib/interface-languages.ts`, `web/src/lib/interface-languages.test.ts`, `model/option.go`(InterfaceLanguages 저장 경로)

**Interfaces:**
- Produces: `INTERFACE_LANGUAGE_OPTIONS = [{code:'en',label:'English'},{code:'ko',label:'한국어'}]`; `normalizeInterfaceLanguage`가 `ko`/`ko-*`/`kr` → `ko`, 그 밖(중국어 포함) → `en`; `convertDetectedLanguage`가 한국어 태그는 `ko`, 그 밖은 원래 값을 돌려준다(i18next `supportedLngs`가 en으로 떨어뜨림); `toIntlLocale('ko')` → `ko`, 그 밖 → `en`. 백엔드 `SupportedLanguages()` = `[en, ko]`, `normalizeLang`은 `ko`/`ko-*`/`kr` → ko, 그 밖 → en. `common.ParseInterfaceLanguages` 허용 코드 = `en, ko`. `common.NormalizeInterfaceLanguagesOption(value string) string`(저장 형식, 빈 결과는 `"en"`).
- 유지: `README.*.md` 번역본(team과 같음), `common/log_text.zh-CN.json`(운영자 서버 로그 언어. 화면 언어가 아니고 upstream 코드가 embed로 참조), 콘텐츠 언어 값(`features/task-plugins/lib/plugin-changelog.ts`, `features/models/types.ts`의 `SyncLocale`, `features/models/constants.ts`의 `Japanese` 옵션).

- [ ] **Step 1: 실패하는 테스트**

`web/src/i18n/languages.test.ts`에 추가:

```ts
test('offers only English and Korean', () => {
  expect(INTERFACE_LANGUAGE_OPTIONS.map((o) => o.code)).toEqual(['en', 'ko'])
})
test('falls back to English for removed languages', () => {
  for (const saved of ['zhCN', 'zhTW', 'zh-CN', 'ja', 'fr', 'ru', 'vi']) {
    expect(normalizeInterfaceLanguage(saved)).toBe('en')
  }
  expect(normalizeInterfaceLanguage('kr')).toBe('ko')
})
test('maps only Korean browser tags', () => {
  expect(convertDetectedLanguage('ko-KR')).toBe('ko')
  expect(convertDetectedLanguage('zh-CN')).toBe('zh-CN') // not supported → i18next falls back to en
})
```

기존의 중국어 매핑 테스트(`maps browser BCP-47 Chinese tags onto interface codes`, `keeps already-normalized interface codes stable`)는 이 동작과 충돌하므로 지우고, 지운 사실을 커밋 메시지 본문에 적는다. `config.ts`의 `supportedLngs`가 `['en','ko']`인지 단언하는 테스트를 하나 둔다(`import { resources } from './config'`에 키가 en/ko뿐). Go `i18n/i18n_test.go`의 표에서 zh 케이스를 `{"zhCN", LangEn}`, `{"zh-HK", LangEn}`처럼 바꾸고, `StatedLang` 테스트의 중국어 기본값 케이스는 ko 기본값(`DEFAULT_LANGUAGE=ko`)으로 바꾼다. `common/interface_languages_fork_test.go`: `ParseInterfaceLanguages("zhCN,ja")` → `["en"]`, `NormalizeInterfaceLanguagesOption(" kr , EN, xx ")` → `"ko,en"`, `NormalizeInterfaceLanguagesOption("xx")` → `"en"`. model 테스트: `UpdateOption("InterfaceLanguages", " kr ,xx")` 뒤 `common.OptionMap["InterfaceLanguages"]`와 DB 값이 모두 `"ko"`. 실행: `cd web && bun run test -- i18n interface-languages`, `GOWORK=off go test ./i18n/ ./common/ ./model/ -run 'Lang|Interface' -count=1` → FAIL.

- [ ] **Step 2: 프런트 구현**

```bash
git rm web/src/i18n/locales/zh.json web/src/i18n/locales/zh-TW.json web/src/i18n/locales/fr.json web/src/i18n/locales/ja.json web/src/i18n/locales/ru.json web/src/i18n/locales/vi.json
```

`config.ts`: import는 `en`과 `ko`만, `resources = { en, ko }`, `supportedLngs: ['en', 'ko']`. `languages.ts`: 목록을 en/ko 둘로 줄이고, zh 분기들을 지운다. `normalizeInterfaceLanguage`는 Korean이면 `ko`, 아니면 `en`이다. `convertDetectedLanguage`는 Korean 태그만 `ko`로 바꾸고 나머지는 그대로 둔다. `toIntlLocale`은 ko/en만 처리한다. 이 파일들은 upstream 파일이므로 바뀐 줄마다 `// fork: O9`를 붙이고 overlay 표에 "team과 같은 en/ko 축소"로 기록한다. 달력 두 파일: import를 `enUS, ko`로 줄이고 맵을 `{ en: enUS, ko }`로 바꾼다(`// fork: O9`). `sync-i18n.mjs:229-236`은 `ja/zh/ru/fr/vi` 분기를 그대로 둬도 동작하므로 손대지 않는다(충돌면 최소화). 다른 locale을 import하는 테스트 5개는 해당 import와 단언을 en(또는 ko)으로 바꾼다. `web/src/lib/interface-languages.ts`의 허용 코드 표를 en/ko로 줄이고, 설정 화면(`general/interface-languages-section.tsx`)은 `INTERFACE_LANGUAGE_OPTIONS`를 쓰므로 자동으로 두 개가 된다(테스트 기대값만 맞춘다).

- [ ] **Step 3: 백엔드 구현**

```bash
git rm i18n/locales/zh-CN.yaml i18n/locales/zh-TW.yaml
```

`i18n/i18n.go`: 번들 기본 언어 `language.Chinese` → `language.English`(:40), 파일 목록 `{"locales/en.yaml", "locales/ko.yaml"}`(:44), localizer는 en/ko 둘(:54-57), `normalizeLang`의 zh 분기를 지우고 ko 외에는 `DefaultLang`(en)을 돌려준다, `SupportedLanguages()` = `{LangEn, LangKo}`. `LangZhCN`/`LangZhTW` 상수가 i18n 패키지 밖에서 쓰이지 않음을 `grep -rn "LangZh" --include='*.go' .`로 확인하고 지운다. 쓰는 곳이 있으면 상수는 남기고 매핑만 바꾼다. 바뀐 줄마다 `// fork: O9`를 붙인다. `common/constants.go:39-43`의 `DEFAULT_LANGUAGE` 설명은 "en or ko"로 바꾼다. 서버 로그 언어(`log_text.zh-CN.json`)는 그대로 두고 설명에 "server log lines also accept zh-CN"을 남긴다. `common/interface_languages_fork.go`의 허용 표를 en/ko로 줄이고 `NormalizeInterfaceLanguagesOption`을 더한다. `model/option.go`에서 InterfaceLanguages 값이 DB와 OptionMap에 저장되기 전 지점(`validateOptionValue` `:238` 또는 `UpdateOption` 진입부. 실제 순서를 읽고 고른다)에 `value = common.NormalizeInterfaceLanguagesOption(value) // fork: O8` 한 줄을 둔다.

- [ ] **Step 4: 검증**

`cd web && bun run test`(전체), `bun run typecheck`, `bun run build`, 손댄 파일 oxlint/oxfmt, `node scripts/ko-locale.mjs check`, `bun run i18n:sync`(en/ko 두 파일만 남았는지, 다시 생성되지 않는지 확인). `GOWORK=off go vet ./... && GOWORK=off go test ./i18n/ ./common/ ./model/ ./controller/ ./middleware/ -count=1`. `docs/fork-overlays.md`에 "삭제한 upstream 파일" 절을 만들고 8개 파일과 병합 처리법을 적는다: upstream이 이 파일을 고치면 modify/delete 충돌이 나고, 해결은 `git rm <file>`로 삭제를 유지하는 것이다.

- [ ] **Step 5: 커밋**

```bash
git add -A web/src i18n common model docs/fork-overlays.md
git commit -m "feat(i18n): keep only English and Korean as interface languages

Ported-From: team 66d421d13
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Gemini 이미지 토큰 상한(캐시·오디오와 겹치는 부분)

**Files:**
- Create: `service/gemini_image_cap_fork.go`, `service/gemini_image_cap_fork_test.go`
- Modify: `service/billing_usage.go:19-24`(`effectiveBillingUsage`)

**Interfaces:**
- Consumes: `usageFromBillingUsage(usage) (*dto.Usage, bool)`(`service/billing_usage.go:63`, `BillingUsage.CanonicalUsage()`가 매번 새 `Usage`를 만든다), `dto.BillingUsageSemanticGemini`, `dto.NewGeminiChatBillingUsage(metadata)`, `calculateTextQuotaSummary(ctx, relayInfo, usage) textQuotaSummary`(`service/text_quota.go:223`).
- Produces: `capGeminiImagePromptTokens(usage *dto.Usage) *dto.Usage`. Gemini 의미의 canonical usage일 때만 `ImageTokens`를 `max(PromptTokens - CachedTokens - AudioTokens, 0)`로 자르고, 잘라 낸 만큼을 `TextTokens`에 돌려 detail 합이 `PromptTokens`가 되게 한다. 그 밖의 usage는 그대로 돌려준다.

- [ ] **Step 1: 실패하는 테스트**

`service/gemini_image_cap_fork_test.go`(package `service`). `dto.GeminiUsageMetadata`의 실제 필드 이름(`PromptTokenCount`, `CachedContentTokenCount`, `CandidatesTokenCount`, `PromptTokensDetails`와 그 원소 타입)은 `relaykit/dto`에서 확인해 맞춘다.

```go
func geminiUsage(prompt, cached, completion int, details map[string]int) *dto.Usage {
	md := &dto.GeminiUsageMetadata{PromptTokenCount: prompt, CachedContentTokenCount: cached, CandidatesTokenCount: completion, TotalTokenCount: prompt + completion}
	for modality, n := range details {
		md.PromptTokensDetails = append(md.PromptTokensDetails, dto.GeminiPromptTokensDetail{Modality: modality, TokenCount: n})
	}
	u := &dto.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	u.BillingUsage = dto.NewGeminiChatBillingUsage(md)
	return u
}

func TestEffectiveBillingUsageCapsGeminiImageByCachedAndAudio(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 1500, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 500, u.PromptTokensDetails.ImageTokens)
	require.Equal(t, 2000, u.PromptTokensDetails.ImageTokens+u.PromptTokensDetails.TextTokens+u.PromptTokensDetails.AudioTokens)

	u = effectiveBillingUsage(geminiUsage(478, 300, 20, map[string]int{"TEXT": 100, "IMAGE": 258, "AUDIO": 50, "VIDEO": 30, "DOCUMENT": 40}))
	require.Equal(t, 128, u.PromptTokensDetails.ImageTokens) // 478-300-50
	require.Equal(t, 50, u.PromptTokensDetails.AudioTokens)
}

func TestEffectiveBillingUsageLeavesUncappedGeminiAndNonGeminiAlone(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 0, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 1089, u.PromptTokensDetails.ImageTokens)

	plain := &dto.Usage{PromptTokens: 100, CompletionTokens: 10}
	plain.PromptTokensDetails.ImageTokens = 90
	plain.PromptTokensDetails.CachedTokens = 50
	require.Same(t, plain, effectiveBillingUsage(plain))
	require.Equal(t, 90, plain.PromptTokensDetails.ImageTokens)
}
```

quota 테스트는 team `service/text_quota_test.go`의 `TestCalculateTextQuotaSummaryGeminiCachedImagePromptIsNotOvercharged`와 `...GeminiImageAudioCachedPromptIsNotOvercharged`를 옮긴다(`git show team:service/text_quota_test.go | sed -n 969,1080p`). usage는 위 `geminiUsage`로 만들고 `effectiveBillingUsage`를 거친 값을 `calculateTextQuotaSummary`에 넣는다. 기대값은 team 그대로다: 상한 없는 전부 텍스트 1075, 상한 적용 이미지(ImageRatio 2) 1575, 이미지+오디오+캐시 1200과 상한 없는 1500. 실행: `GOWORK=off go test ./service/ -run 'GeminiImage|EffectiveBillingUsage' -v` → FAIL(ImageTokens 1089, quota 2753 등).

- [ ] **Step 2: 구현**

`service/gemini_image_cap_fork.go`:

```go
package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// capGeminiImagePromptTokens keeps Gemini image prompt tokens out of the
// cached and audio portions. Gemini promptTokensDetails include cached tokens,
// and text settlement bills base = prompt - cached - image - audio plus
// cached*cacheRatio + image*imageRatio + audio price, so image tokens that
// overlap the cached or audio portions would be billed twice. The capped-off
// amount returns to TextTokens so the details still sum to PromptTokens.
// Ported from team af1eddc8e/c20c6d664 into the host (relaykit stays upstream).
func capGeminiImagePromptTokens(usage *dto.Usage) *dto.Usage {
	if usage == nil || usage.BillingUsage == nil || usage.BillingUsage.Semantic != dto.BillingUsageSemanticGemini {
		return usage
	}
	d := &usage.PromptTokensDetails
	maxImage := max(usage.PromptTokens-d.CachedTokens-d.AudioTokens, 0)
	if d.ImageTokens > maxImage {
		d.TextTokens += d.ImageTokens - maxImage
		d.ImageTokens = maxImage
	}
	return usage
}
```

`CanonicalUsage()`가 돌려준 `Usage`에 `BillingUsage`가 실려 있는지 확인한다. 실려 있지 않으면 helper 시그니처를 `capGeminiImagePromptTokens(canonical *dto.Usage, semantic string)`으로 바꾸고 원래 `usage.BillingUsage.Semantic`을 넘긴다(테스트도 맞춘다). 훅은 `service/billing_usage.go:19-24`에 둔다.

```go
func effectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return capGeminiImagePromptTokens(billingUsage) // fork: O11
	}
	return usage
}
```

실행: Step 1 PASS, `GOWORK=off go test ./service/... ./relay/... -count=1`. `PostAudioConsumeQuota` 경로(raw usage)는 이 상한을 거치지 않는다. 기록에 남긴다.

- [ ] **Step 3: 커밋**

```bash
git add service/gemini_image_cap_fork.go service/gemini_image_cap_fork_test.go service/billing_usage.go docs/fork-overlays.md
git commit -m "fix(gemini): cap image prompt tokens by the cached and audio portions

Ported-From: team af1eddc8e c20c6d664
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Ali `thinking_budget` either-name 규칙

**Files:**
- Create: `relay/channel/ali/thinking_budget_fork.go`, `relay/channel/ali/thinking_budget_fork_test.go`
- Modify: `relay/channel/ali/text.go:12-19`, `relay/channel/ali/adaptor.go:146`(변환 호출), `:185-187`(`DoRequest`), `relay/channel/ali/adaptor_test.go:22-60`

**Interfaces:**
- Consumes: `dto.IsQwenThinkingBudgetModel(name string) bool`, `info.OriginModelName`, `info.UpstreamModelName`, `channel.DoApiRequest(a, c, info, body)`.
- Produces: `aliKeepsThinkingBudget(info *relaycommon.RelayInfo) bool`; `stashAliThinkingBudget(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) bool`; `doAliRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error)`. 이 함수는 stash가 있으면 본문에 `thinking_budget`을 원래 raw 값 그대로 다시 넣는다. `requestOpenAI2Ali(request, keepThinkingBudget bool)`는 계속 `*dto.GeneralOpenAIRequest`를 돌려준다(`relay/compatible_handler.go`가 이 타입을 단언한다).

배경: develop은 두 곳에서 예산을 지운다. `requestOpenAI2Ali`(upstream 이름 기준)와 `GeneralOpenAIRequest.MarshalJSON`(매핑 뒤의 `r.Model` 기준, relaykit)이다. relaykit은 고칠 수 없으므로(규칙 6) 마샬 뒤 Ali 어댑터의 `DoRequest`에서 다시 넣는다. 그 결과 `ParamOverride`와 `RemoveDisabledFields`는 다시 넣은 필드를 보지 못한다(기록에 남긴다).

- [ ] **Step 1: 실패하는 테스트**

`thinking_budget_fork_test.go`: `httptest.NewServer`로 upstream을 흉내 내고 받은 본문을 기록한다. 요청은 `{"model":"qwen3-max","messages":[{"role":"user","content":"hi"}],"enable_thinking":true,"thinking_budget":0}`이고 채널 매핑은 `{"qwen3-max":"my-qwen-deploy"}`다. 컨텍스트에 `model_mapping`을 넣고 `relayhelper.ModelMappedHelper`를 거친다. `ConvertOpenAIRequest` → `common.Marshal` → `(&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))`를 실행한다(`info.ChannelMeta`의 BaseURL을 테스트 서버로. `relay/channel/api_request_redirect_test.go:19`의 하네스를 참고한다). 단언: 서버가 받은 본문의 `model`이 `my-qwen-deploy`, `thinking_budget`이 존재하고 값이 정확히 `0`, `enable_thinking`이 true. 두 번째 테스트: 클라이언트 `deepseek-v3`, `thinking_budget:100` → 본문에 `thinking_budget`이 없다. 세 번째: 클라이언트 `qwen-plus` → upstream `deepseek-r1`(매핑) → 128이 남는다. `adaptor_test.go:22`의 기존 케이스 "unsupported upstream overrides qwen request"(`wantBudget: false`)는 새 규칙과 충돌하므로 `wantBudget: true, wantValue: 128`로 바꾸고 이름을 "qwen client mapped to unsupported upstream keeps budget"으로, 테스트 함수 이름을 `TestConvertOpenAIRequestKeepsThinkingBudgetWhenEitherNameIsQwen`으로 바꾼다. 실행: `GOWORK=off go test ./relay/channel/ali/ -run ThinkingBudget -v` → FAIL.

- [ ] **Step 2: 구현**

`thinking_budget_fork.go`:

```go
package ali

const aliThinkingBudgetContextKey = "fork_ali_thinking_budget"

// aliKeepsThinkingBudget is the team either-name rule (70b14d049): a request
// keeps thinking_budget when the client model or the mapped upstream model is
// a Qwen/QwQ model, so a Qwen client mapped to a custom deployment name keeps
// an explicit budget (including 0, which turns thinking off).
func aliKeepsThinkingBudget(info *relaycommon.RelayInfo) bool {
	return dto.IsQwenThinkingBudgetModel(info.OriginModelName) || dto.IsQwenThinkingBudgetModel(info.UpstreamModelName)
}

func stashAliThinkingBudget(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) bool {
	keep := aliKeepsThinkingBudget(info)
	if keep && request.ThinkingBudget != nil {
		raw, err := common.Marshal(request.ThinkingBudget)
		if err == nil {
			c.Set(aliThinkingBudgetContextKey, json.RawMessage(raw))
		}
	}
	return keep
}

// doAliRequest re-inserts a stashed thinking_budget after the generic request
// marshaler (relaykit, upstream) dropped it by the mapped model name.
func doAliRequest(a *Adaptor, c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	raw, ok := c.Get(aliThinkingBudgetContextKey)
	if !ok {
		return channel.DoApiRequest(a, c, info, body)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(data, "thinking_budget").Exists() {
		if data, err = sjson.SetRawBytes(data, "thinking_budget", raw.(json.RawMessage)); err != nil {
			return nil, err
		}
	}
	return channel.DoApiRequest(a, c, info, bytes.NewReader(data))
}
```

`ThinkingBudget`의 실제 타입(포인터 정수 등)에 맞춰 마샬한다. 0이 사라지면 안 된다. `relay/channel/api_request.go`에서 `requestBody`를 어떻게 다루는지 확인하고, 길이 헤더 등이 필요하면 upstream helper로 본문을 다시 만든다. `text.go:12`: 시그니처를 `requestOpenAI2Ali(request dto.GeneralOpenAIRequest, keepThinkingBudget bool) *dto.GeneralOpenAIRequest // fork: O11`로 바꾸고 판정을 `if !keepThinkingBudget { request.ThinkingBudget = nil }`로 바꾼다. 줄 수가 늘지 않게 기존 `modelName` 블록을 대체한다. 다른 호출부가 있으면 함께 바꾼다(`grep -rn requestOpenAI2Ali relay/`). `adaptor.go:146`: `aliReq := requestOpenAI2Ali(*request, stashAliThinkingBudget(c, info, request)) // fork: O11`. `adaptor.go:185-187`의 `DoRequest` 본문은 `return doAliRequest(a, c, info, requestBody) // fork: O11` 한 줄로 바꾼다. 실행: Step 1 PASS, `GOWORK=off go test ./relay/... -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add relay/channel/ali docs/fork-overlays.md
git commit -m "fix(ali): keep thinking_budget when either the client or the upstream model is Qwen

Ported-From: team afbf87cd3 70b14d049
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: 출석·추천 적립의 지갑 상한, 조직 Epay 롤백 테스트

**Files:**
- Create: `model/wallet_ceiling_fork.go`, `model/wallet_ceiling_fork_test.go`, `model/topup_target_epay_test.go`
- Modify: `model/checkin.go:103-104`(트랜잭션 경로), `:133-136`(SQLite 경로), `model/user.go:617-618`(`TransferAffQuotaToQuota`), `controller/organization_i18n_test.go`(`forkMessageSources`), en/ko locale

**Interfaces:**
- Consumes: `creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]any) error`(`model/topup.go:95`, 원자적 `quota <= max` UPDATE), `ErrTopUpQuotaLimitExceeded`(`model/topup.go:56`), `common.MaxWalletQuota`.
- Produces: `creditCheckinReward(tx *gorm.DB, userId, quota int) error`. `quota <= 0`이면 nil, 상한 초과면 `ErrCheckinWalletLimit = common.NewMessage("Check-in failed: wallet quota limit reached")`, 그 밖이면 `common.NewMessage("Check-in failed: quota update error")`. `checkAffTransferCeiling(currentQuota, quota int) error`는 상한 초과 시 `ErrAffTransferWalletLimit = common.NewMessage("Invitation quota transfer failed: wallet quota limit reached")`.

- [ ] **Step 1: 실패하는 테스트**

team `git show team:model/checkin_quota_test.go`의 세 테스트를 옮긴다(`TestUserCheckinCreditsReward`, `TestUserCheckinAtWalletCeilingIsRejectedAndNotRecorded`, `TestUserCheckinWithTransactionAtWalletCeilingRollsBack`). `common.MaxQuota`는 `common.MaxWalletQuota`로, `insertUserWithQuota`는 develop helper `insertUserForPaymentGuardTest(t, id, quota)`(`model/payment_method_guard_test.go:12`)로 바꾸고, `resetUserQuotaBatchState`가 없으면 빼거나 develop의 batch 비활성 방식을 따른다. 기대 메시지는 `Check-in failed: wallet quota limit reached`다. 추가: `TestTransferAffQuotaToQuotaRespectsWalletCeiling`(quota `MaxWalletQuota-10`, aff 1,000,000 → `ErrAffTransferWalletLimit`이고 quota와 aff_quota는 변하지 않는다). `topup_target_epay_test.go`의 `TestRechargeEpayOrganizationOrderStaysPendingWhenWalletFull`: 조직 쿼터가 `MaxWalletQuota-10`인 조직의 epay 주문 → `RechargeEpay` 오류, 주문은 pending, 조직 쿼터는 그대로(fixture는 `controller/organization_payment_test.go`·`model/topup_target_test.go`를 참고). 실행: `GOWORK=off go test ./model/ -run 'Checkin|AffQuota|RechargeEpayOrganization' -v`. 트랜잭션 경로와 aff 테스트는 FAIL이어야 하고, Epay 테스트는 이미 PASS일 수 있다(회귀 고정용).

- [ ] **Step 2: 구현**

`model/wallet_ceiling_fork.go`:

```go
package model

var (
	ErrCheckinWalletLimit     = common.NewMessage("Check-in failed: wallet quota limit reached")
	errCheckinQuotaUpdate     = common.NewMessage("Check-in failed: quota update error")
	ErrAffTransferWalletLimit = common.NewMessage("Invitation quota transfer failed: wallet quota limit reached")
)

// creditCheckinReward credits a check-in reward within the wallet ceiling in
// the check-in transaction, so a rejected reward rolls the check-in row back
// (team 2ba5cc513; upstream checkin.go credits without a ceiling).
func creditCheckinReward(tx *gorm.DB, userId, quota int) error {
	if quota <= 0 {
		return nil
	}
	if err := creditTopUpQuota(tx, userId, quota, nil); err != nil {
		if errors.Is(err, ErrTopUpQuotaLimitExceeded) {
			return ErrCheckinWalletLimit
		}
		return errCheckinQuotaUpdate
	}
	return nil
}

func checkAffTransferCeiling(currentQuota, quota int) error {
	if quota > common.MaxWalletQuota-currentQuota {
		return ErrAffTransferWalletLimit
	}
	return nil
}
```

`checkin.go:103-104`의 두 줄(`tx.Model(&User{})… Update(...)` + 오류 반환)을 `if err := creditCheckinReward(tx, userId, quotaAwarded); err != nil { return err } // fork: O11`로 바꾼다(gofmt가 펼치면 첫 줄에 marker). SQLite 경로(`:133-136`)는 `IncreaseUserQuota` 실패를 상한 오류와 구분하거나, 함수 본문을 트랜잭션 경로를 쓰는 fork 함수 호출 한 줄로 바꾼다. 둘 중 hunk가 작은 쪽을 고르고 보고서에 적는다. `user.go:617-618`(`user.AffQuota -= quota; user.Quota += quota`) 바로 앞에 `if err := checkAffTransferCeiling(user.Quota, quota); err != nil { return err } // fork: O11`. 새 메시지 키 3개를 en/ko locale에 넣는다. `controller/organization_i18n_test.go`의 `forkMessageSources`에 `model/wallet_ceiling_fork.go`를 추가한다. 실행: Step 1 PASS, `GOWORK=off go test ./model/ ./controller/ -count=1`, `cd web && node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋**

```bash
git add model web/src/i18n/locales controller/organization_i18n_test.go docs/fork-overlays.md
git commit -m "fix(quota): keep check-in and invitation transfers within the wallet ceiling

Ported-From: team 2ba5cc513
Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: AI 클라이언트로 가는 조직 오류의 번역

**Files:**
- Create: `i18n/keys_fork.go`, `service/organization_relay_errors_fork_test.go`
- Modify: `i18n/locales/en.yaml`(파일 끝에 `# fork: O3` 블록), `i18n/locales/ko.yaml`, `service/billing_session_organization.go`(오류 생성 지점), `service/midjourney_organization.go:15,38,45,69`, `service/quota_organization.go`, `relay/mjproxy_handler.go:229`(기존 fork 훅)

**Interfaces:**
- Consumes: `i18n.T(c *gin.Context, key string, args ...map[string]any) string`(`i18n/i18n.go:93`), 기존 패턴 `errors.New(i18n.T(c, i18n.MsgQuotaUserInsufficient, map[string]any{...}))`(`service/billing_session.go:244`).
- Produces: `i18n.MsgOrganizationDisabled = "organization.disabled"`, `MsgOrganizationMemberSuspended = "organization.member_suspended"`, `MsgOrganizationWalletInsufficient = "organization.wallet_insufficient"`(param `Error`), `MsgOrganizationMemberQuotaInsufficient = "organization.member_quota_insufficient"`(param `Error`), `MsgOrganizationMjPlanUnsupported = "organization.mj_plan_unsupported"`, `MsgOrganizationQuotaInsufficient = "organization.quota_insufficient"`. `CheckMidjourneyOrganizationBilling(c *gin.Context, userId int) error`(c 추가).

- [ ] **Step 1: 실패하는 테스트**

`service/organization_relay_errors_fork_test.go`: `Accept-Language: ko` 컨텍스트에서 비활성 조직 회원의 `NewBillingSession`(Phase B·C의 `service/billing_session_organization_test.go` fixture)이 돌려준 `*types.NewAPIError`의 메시지가 `ko.yaml`의 `organization.disabled` 값과 같다. 정지 회원과 조직 지갑 부족도 같은 방식으로 확인한다. `Accept-Language` 없음 → 영어 원문(기존 테스트 문구 유지). MJ: `CheckMidjourneyOrganizationBilling(c, userId)`가 ko 메시지를 돌려준다. 실행: `GOWORK=off go test ./service/ -run OrganizationRelayErrors -v` → FAIL.

- [ ] **Step 2: 구현**

`i18n/keys_fork.go`에 위 상수를 둔다. `en.yaml` 끝에 `# fork: O3 — organization relay errors` 주석과 6개 키(값은 현재 영어 원문: `organization is disabled`, `organization membership is suspended`, `organization wallet quota insufficient: {{.Error}}`, `member quota insufficient: {{.Error}}`, `legacy Midjourney billing does not support organization plans`, `organization quota insufficient`), `ko.yaml`에 같은 키를 용어집으로 번역(예: `조직이 비활성화되었습니다`, `조직 멤버십이 일시 정지되었습니다`, `조직 지갑 쿼터가 부족합니다: {{.Error}}`). 생성 지점은 `types.NewErrorWithStatusCode(errors.New(i18n.T(c, i18n.MsgOrganizationDisabled)), …)`처럼 바꾼다. `errors.Is(err, model.ErrOrganizationDisabled)` 판정은 그대로 두고, 클라이언트로 나가는 메시지만 번역한다. `relay/mjproxy_handler.go:229`의 기존 fork 훅을 `CheckMidjourneyOrganizationBilling(c, info.UserId)`로 바꾼다. `TestKoreanYamlCoversEveryEnglishKey`가 ko 누락을 잡는다. 실행: Step 1 PASS, `GOWORK=off go test ./i18n/ ./service/... ./relay/ ./controller/ -count=1`.

- [ ] **Step 3: 커밋**

```bash
git add i18n service relay/mjproxy_handler.go docs/fork-overlays.md
git commit -m "feat(i18n): translate organization errors returned to AI clients

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: fork 감사 로그 템플릿

**Files:**
- Create: `web/src/features/usage-logs/lib/fork-audit-templates.ts`, `web/src/features/usage-logs/__tests__/fork-audit-templates.test.ts`
- Modify: `web/src/features/usage-logs/lib/format.ts:451`(`AUDIT_TEMPLATES` 객체 안 spread 한 줄), en/ko locale

**Interfaces:**
- Consumes: `renderAuditContent(other, t)`(`format.ts:602-639`: `AUDIT_TEMPLATES[op.action]`이 있으면 `t(template, op.params)`), 감사 params(각 `recordOrganizationAudit` 호출부의 `organizationAuditParams(base, updates)`, `controller/topup_expire_admin.go:44-50`).
- Produces: `FORK_AUDIT_TEMPLATES: Record<string, string>`. 14개 액션: `organization.create`, `organization.update`, `organization.delete`, `organization.user.assign`, `organization.user.update`, `organization.user.remove`, `organization.user.import`, `organization.plan.create`, `organization.plan.update`, `organization.plan.disable`, `organization.subscription.assign`, `organization.subscription.cancel`, `organization.topup.expire`, `topup.expire`.

- [ ] **Step 1: 실패하는 테스트**

각 액션의 실제 params 키를 Go 호출부에서 읽는다(`grep -n 'recordOrganizationAudit(c, "' -A 4 controller/*.go`). 템플릿은 그 키만 보간한다(예: `organization.user.update` → `'Updated member {{user_id}} of organization {{organization_id}}'`, `topup.expire` → `'Expired {{expired}} pending top-ups and {{expired_subscriptions}} subscription orders of user {{user_id}}'`). 테스트는 각 액션마다 실제 params 모양으로 `renderAuditContent({op:{action, params}}, t)`를 부르고 결과가 null이 아니며 `{{`가 남지 않음을 단언한다. ko로 바꾼 i18n에서 한 액션이 한국어로 렌더되는지도 확인한다(Phase D `server-error-message.test.ts`의 언어 전환 방식). 실행: `cd web && bun run test -- fork-audit-templates` → FAIL.

- [ ] **Step 2: 구현**

`fork-audit-templates.ts`(AGPL 헤더)에 맵을 둔다. `format.ts:451` 객체 첫 줄 다음에 `...FORK_AUDIT_TEMPLATES, // fork: O3` 한 줄과 import 한 줄을 넣는다. 14개 템플릿 문자열을 en/ko locale에 넣는다(ko는 용어집으로 번역: 조직, 멤버, 플랜, 할당, 일시 정지, 만료). 실행: Step 1 PASS, `bun run test -- usage-logs`, typecheck, 파일별 oxlint/oxfmt, `node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋**

```bash
git add web/src docs/fork-overlays.md
git commit -m "feat(web): render organization and top-up expiry audit entries as translated sentences

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Excel 조직 가져오기 행 오류의 구조화와 번역

**Files:**
- Create: `service/org_import_errors_fork.go`
- Modify: `service/org_user_export.go`(fork 파일, `OrgImportResult`와 오류 추가 지점 약 25곳), `web/src/features/organizations/types.ts:113`, `web/src/features/organizations/components/org-users-import-dialog.tsx:155-165`, `web/src/features/organizations/__tests__/org-users-import-dialog.test.tsx`(없으면 생성), `controller/organization_i18n_test.go`, en/ko locale

**Interfaces:**
- Produces: Go `type OrgImportRowError struct { Line int \`json:"line"\`; Username string \`json:"username,omitempty"\`; MessageKey string \`json:"message_key"\`; Params map[string]any \`json:"params,omitempty"\` }`; `OrgImportResult.ErrorDetails []OrgImportRowError \`json:"error_details"\``. 기존 `Errors []string`은 영어 문자열로 그대로 남긴다. helper `(r *OrgImportResult) addRowError(line int, username, key string, params map[string]any)`는 `Errors`(영어, 기존 형식 `line N (user): …`)와 `ErrorDetails`에 함께 추가한다. 중첩된 오류는 `params["error"]`에 문자열로 넣고, 그 오류가 `*common.Message`면 `params["error_key"]`/`params["error_params"]`도 넣는다. TS `OrgImportRowError`, `OrgImportResult.error_details?: OrgImportRowError[]`.

- [ ] **Step 1: 실패하는 테스트**

Go: `service/org_user_export_test.go`에 "빈 username 행과 잘못된 역할 행 → `ErrorDetails`에 line·username·message_key(`username is empty`, `invalid organization_role {{role}} (only member or admin can be imported)`)가 들어가고 `Errors` 문자열은 기존과 같다". 프런트: 다이얼로그 테스트 "renders translated row errors and falls back to plain errors". `error_details`가 있으면 `t('Line {{line}}', …)`, 사용자 이름, `t(message_key, params)`로 그리고, 없는 옛 응답이면 `errors` 문자열을 그대로 그린다. ko에서 한 행이 한국어로 나오는지 확인한다. 실행 → FAIL.

- [ ] **Step 2: 구현**

`org_user_export.go`의 `result.Errors = append(result.Errors, fmt.Sprintf(...))` 지점을 모두 `result.addRowError(lineNum, username, "<영어 템플릿>", params)`로 바꾼다. 영어 템플릿은 `{{name}}` 보간 형식이고, 같은 의미의 기존 문장은 같은 키를 쓴다(예: `failed to set quota: {{error}}`). remove-absent 오류는 line 0이다. 새 키(템플릿 약 18개 + `Line {{line}}`)를 en/ko에 넣는다. 다이얼로그는 `error_details ?? []`로 그리고 없으면 `errors`로 fallback한다. `controller/organization_i18n_test.go`의 키 검사 정규식에 `addRowError\([^,]+, [^,]+, "…"`를 추가한다. 실행: Step 1 PASS, `GOWORK=off go test ./service/ ./controller/ -count=1`, `cd web && bun run test -- organizations`, typecheck, oxlint/oxfmt, `node scripts/ko-locale.mjs check`.

- [ ] **Step 3: 커밋**

```bash
git add service web/src controller/organization_i18n_test.go docs/fork-overlays.md
git commit -m "feat(organization): return structured import row errors and render them translated

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: 기록·overlay 표·전체 검증·병합 리허설

**Files:**
- Create: `docs/2026-10-15-rebase-phase-e.md`
- Modify: `docs/fork-overlays.md`(O11 절 신설, "삭제한 upstream 파일" 절, 이번 단계의 O3·O8·O9 행), spec §4.8·§4.9·§9

- [ ] **Step 1: 전체 검증** — `GOWORK=off go vet ./... && GOWORK=off go test ./... 2>&1 | grep -E "^(FAIL|---)"`; `GOWORK=off go test -race ./model/ ./service/ -run 'Checkin|Gemini|Vendor' -count=1`; `cd web && bun run typecheck && bun run test && bun run build && node scripts/ko-locale.mjs check && node scripts/ko-locale.mjs status && bun run e2e`(fresh SQLite, root/12345678; 21 통과 기대). 정확한 수치를 기록한다.
- [ ] **Step 2: 병합 리허설** — `git fetch qn && git merge --no-commit --no-ff qn/main`. 충돌 목록을 기록하고, 각 충돌이 overlay 표의 upstream 파일이거나 "삭제한 upstream 파일"(modify/delete)인지 확인한다. modify/delete 충돌은 `git rm`으로 풀리는지 확인한 뒤 `git merge --abort`. ko.json이 upstream 새 키를 못 받은 상태에서 `bun run test -- ko-coverage`가 실패하는지도 리허설 트리에서 확인한다(확인 뒤 abort).
- [ ] **Step 3: 기록** — §1 UI 언어 정리(삭제 파일, 남긴 것: README 번역본, 서버 로그 zh-CN, 콘텐츠 언어 값; 기존 사용자 fallback), §2 O11 조사 결과(이미 equivalent인 항목과 근거: upstream `RechargeEpay` `50e5377ea`, `rejectInvalid*` 진입점 목록, Gemini 프롬프트만 사용량·누적 프레임, gpt-5 capability, Claude 5.x 표), §3 이번에 올린 세 수정(수치 예시 포함), §4 한국어 이월(감사 로그, Excel, relay 오류), §5 결정 사항(아래 계획 결정 + 실행 중 ruling), §6 단계 F로 넘기는 것(Phase C·D 잔여 중 남은 것: 할인 로그 MJ·태스크, `doRequest` 우회 `llm_ms`, 무료 모델 세션 우회, `kr` 데이터 마이그레이션, `<html lang>`, Gemini `TextTokens` 표시 차이와 `PostAudioConsumeQuota` 경로, team 브랜치의 `TimingResponseWriter.Unwrap()` 결함은 team 쪽에서 별도 확인), §7 검증. overlay 표 줄 번호를 `grep -rn "fork: O" …`로 검증한다. spec §4.8에 "UI 언어는 en/ko"를, §4.9에 "Epay·수용량 검증은 upstream이 해결"과 실제 구현 위치를 적는다.
- [ ] **Step 4: 커밋** — `docs: record phase E of the upstream rebase (languages, O11 fixes, Korean carry-overs)` (트레일러 동일).

---

## 계획 결정 사항

1. UI 언어는 team처럼 en/ko만 둔다. upstream locale 파일 8개(프런트 6, 백엔드 2)를 지운다. 대가는 upstream이 그 파일을 고칠 때마다 생기는 modify/delete 충돌이며, 해결은 `git rm`으로 삭제를 유지하는 것이다(overlay 표에 처리법을 둔다). README 번역본, 서버 로그 언어(`log_text.zh-CN.json`), 콘텐츠 언어 값은 남긴다.
2. 중국어·일본어 등을 저장해 둔 기존 사용자와 그 언어 브라우저는 영어로 연다. 데이터 마이그레이션은 하지 않는다.
3. Epay 트랜잭션 정산과 "fail" 응답, 결제 전 수용량 검증(PayPal·조직 포함)은 upstream과 단계 A·B가 이미 같은 동작을 하므로 다시 적용하지 않는다. develop이 not-found·다른 provider·만료 주문에도 "fail"을 답해 Epay가 한도까지 재시도하는 동작은 upstream 그대로 둔다(사용자 결정).
4. Claude 5.x native passthrough(클라이언트가 보낸 `thinking.enabled`·`disabled`를 그대로 전달)는 upstream 설계를 따른다. team의 재작성은 옮기지 않는다(사용자 결정). gpt-5 channel-test의 한 줄 교체도 하지 않는다.
5. Gemini 이미지 상한은 relaykit을 고치지 않고 호스트의 `effectiveBillingUsage` 한 지점에 둔다. `TextTokens`에 VIDEO·DOCUMENT를 더하는 표시 차이는 과금에 영향이 없어 옮기지 않는다. `PostAudioConsumeQuota` 경로는 상한을 거치지 않는다(기록).
6. Ali 예산은 relaykit 마샬러가 지우므로 Ali 어댑터의 `DoRequest`에서 다시 넣는다. `ParamOverride`·`RemoveDisabledFields`는 다시 넣은 필드를 보지 못한다(기록).
7. 출석 보상과 추천 적립 이전에 지갑 상한을 둔다. upstream에는 상한이 없지만 team이 막았던 구멍이라 fork로 유지한다.
8. AI 클라이언트로 가는 조직 오류는 upstream의 `i18n.T` + yaml 경로를 쓴다. upstream `en.yaml`에는 끝의 fork 블록만 더한다.
9. Excel 행 오류는 기존 `errors []string`을 유지하고 `error_details`를 더해 호환을 지킨다.
10. 조직 로그·대시보드는 현재 멤버의 전체 기록을 그대로 보여 준다. 조직에 넣는 사용자는 새로 만들었거나 거의 새 계정이라 이전 기록이 없거나 공개돼도 무시할 수 있다고 가정한다(사용자 결정). 가입 시점 제한은 하지 않는다.
11. team 브랜치(`TimingResponseWriter.Unwrap()` 역이식 등)는 이 계획에서 다루지 않는다. team 쪽은 별도로 확인한다(사용자 결정).
12. Redis 쿼터 캐시 TTL은 이번 단계에서 upstream 상태(`SYNC_FREQUENCY` 기본 60초, 사용 중 갱신 없음)로 둔다(사용자 결정). 다음 단계에서 이것만 따로 다룬다: team `10ade7045`(배치 대기 중 DB 재수화 거부), `cf2cf300f`(예약·환불 때 TTL 갱신), `9ef93eb8c`(`QUOTA_CACHE_TTL` 기본 600초)와 develop의 배치 모드 재수화 구멍(캐시 만료가 배치 반영 전에 오면 오래된 DB 잔액으로 다시 채워짐)을 테스트로 재현한 뒤 선택지 A–D 중 하나로 정한다. 세션 캐시와 인증 fence가 사용자 캐시 TTL을 공유한다는 점도 그때 함께 정한다.
