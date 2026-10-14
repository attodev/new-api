# Phase D final review: 4406ef559..223770cdc (develop)

Reviewer: final whole-branch review (read-only), 2026-10-15.
I reviewed in six passes: (1) i18n wiring and tooling, (2) the backend Korean locale and message path, (3) the O8 option, (4) the carry-overs, (5) a ko.json sample and README.kr.md, (6) the docs.
For ko.json I read about 330 values. They were a systematic sample (every 50th key, about 150 values), stratified random samples (70 billing/wallet, 22 each for auth, errors, logs and settings), and every key added after `Subscription status expired`. I also ran four scans over all 7,538 values: ASCII words of 3 letters or more beside hangul, 해요체 and 반말 endings, glossary drift terms, and pricing-label families.

## Strengths

- **Language plumbing is correct end to end.** The frontend maps `ko`, `ko-*`, `ko_*` and the legacy `kr` to `ko` in both `convertDetectedLanguage` (detector) and `normalizeInterfaceLanguage` (saved values, login path). It uses an anchored regex (`/^(kr|ko(-.*)?)$/`), so `kok` and `kos` are not caught. The only raw `i18n.changeLanguage` call sites are the header switcher and the profile card, and both pass normalized codes. `toIntlLocale('ko')` and `localized-text.ts` handle `ko` natively. `kr` never reaches `Intl` (Kanuri).
- **The backend matches the frontend.** `normalizeLang` has the same `kr`/`ko`/`ko-*` rule, runs before the zh branches, and lowercases and replaces `_` with `-` first. `ko.yaml` covers all 66 keys, and a script comparison against `en.yaml` found identical `{{.Var}}` and tag sets in every key. `TestKoreanYamlCoversEveryEnglishKey` guards future upstream keys.
- **The message path is sound.** `common.NewMessage` returns `*Message`, so `errors.Is(err, ErrOrganizationMembershipChanged)` keeps pointer identity. `ApiErrorStatus` uses `errors.As`, so a wrapped `*Message` still yields `message_key`. I scanned all 107 `NewMessage`/`ApiErrorT` literals in the fork sources (plus the two new Phase D files):
  - every key exists in en.json and has a real Korean value;
  - every literal params map matches the key's `{{vars}}` exactly;
  - no key with variables is called without params.
  The English `message` text is unchanged, so API callers see the same strings.
- **The O8 option is clean.** It uses the upstream OptionMap mechanism, a pure parser with tests on the Go side, a `[]`/missing → all-options fallback on the frontend, and keeps the current language selectable. `STATUS_RELATED_KEYS` refreshes `/api/status` after a save, and the zod min(1) error message is translated through `FormMessage`.
- **Carry-over money paths are safe in the ways that matter:**
  - The vendor carry-over only fires when `vendorNameByID` lacks the id. UI vendor deletion reassigns models to vendor 0 before the soft delete (`applyVendorOperation`), so a successful vendor query cannot carry over a stale name except after a manual DB edit (the deferred minor).
  - Expiry runs in one transaction. It filters `status = pending` and `target_type IS NULL OR <> 'organization'`, and never touches organization orders, settled orders or orders under an hour old. It is audited (`topup.expire`) and admin-only (`AdminAuth` group, access-token `billing:write`). The test covers user-scoped and all-users runs, legacy empty `target_type`, other users, settled rows and the org-join unblock sequence.
- **Hook discipline is good.** Every upstream hunk carries `// fork: O8`/`O9`/`O7`/`O3`. I checked every overlay row's `file:line` against `grep -n "fork: O"`, and all of them match.
- **Translation quality is high.** I found zero 해요체 or 반말 endings across all 7,538 values. The glossary drift terms (할당량 except "Total Allocated", 재설정, 공급자, 제공업체, 티어, 벤더, 사전 소비, 텔레그램, 회전문, 상환/교환 코드) are all gone. Auth, error and wallet strings read naturally. The protected names "New API" (7) and "QuantumNous" (12) occur the same number of times in README.kr.md as in README.md.

## Issues

### Critical

None.

### Important

**I1. Half-English values remain in highly visible screens (the Task 12 carry is confirmed and two more were found).**
Six values in `web/src/i18n/locales/ko.json`:

| Line | Key | Current value | Suggested value | Where it shows |
|---|---|---|---|---|
| 3541 | `Log Details` | Log 상세 | 로그 상세 | Title of the usage-log and audit-log detail dialogs |
| 3403 | `Key Fingerprint` | Key 지문 | 키 지문 | Channel-affinity cache stats |
| 3404 | `Key Sources` | Key 출처 | 키 출처 | Channel-affinity rule editor |
| 3405 | `Key Summary` | Key 다이제스트 | 키 요약 | Channel-affinity cache stats. The current value is also a mistranslation: Summary is not digest. |
| 4592 | `Per Request` | Per 요청 | 요청당 | **New finding.** The public pricing page's quota-type filter (`features/pricing/constants.ts:64`) |
| 6546 | `Top Models` | Top 모델 | 인기 모델 or 상위 모델 | **New finding.** The rankings card title (`models-section.tsx:173`) |

- Why `check` misses them: each value contains hangul and differs from the English, so `check` passes it. My scan for capitalised 3-letter words found no other cases.
- Failure scenario: a Korean user opens any log detail and sees the title "Log 상세". An anonymous visitor on the pricing page sees the filter chip "Per 요청".
- Fix: edit the six values. Optionally add a `check` warning for the pattern `^[A-Z][a-z]{2,} [가-힣]`.

**I2. Seeded billing labels are mistranslated in the model-pricing editor.**
- `Input price → 가격 입력` (`ko.json:3264`). This reads as "enter a price". It should be **입력 가격**, which `Enter Input price to calculate ratio` already uses.
- `Completion price → 최종 가격` and `Completion price ($/1M tokens) → 최종 가격 ($/1M 토큰)` (`ko.json:1359-1360`). The glossary row "Completion ratio" says completion is **출력** (출력 가격 / 출력 비율), and the Task 8 sweep removed 최종 from `Completion ratio`. These two seeded values were missed.
- Both labels sit in the pricing preview rows (`model-pricing-core.ts:285,292`, `model-pricing-sheet.tsx:1042`).
- Failure scenario: an admin configuring a model sees "가격 입력: $2 / 최종 가격: $8 / 캐시 읽기 가격 …". 최종 가격 also means "final price" elsewhere on the same screens (`Confirm the final prices…` → 최종 가격), so the admin can misread the output price as a post-discount total.
- Fix: 입력 가격, 출력 가격, 출력 가격 ($/1M 토큰).
- Recommended follow-up (not blocking): grep the seeded values for other `X price`/`X ratio` labels whose word order was flipped. My scan of the Input/Output/Prompt/Cache/Audio families found only these three.

**I3. Personal-order expiry can expire an order the user is still paying for, and a later payment is then never credited.**
`model/topup_expire_fork.go:15,43`, `controller/topup_expire_admin.go:27`.
- The age gate is 1 hour (`personalTopUpExpireAge`). Stripe top-up and subscription sessions are created without `ExpiresAt` (`controller/topup_stripe.go:375`, `subscription_payment_stripe.go:115`), so they stay payable for Stripe's default 24 hours. PayPal approval links and Pancake sessions also outlive an hour.
- Settlement only credits pending orders. `Recharge` returns "invalid top-up order status" and `CompleteSubscriptionOrder` returns `ErrSubscriptionOrderStatusInvalid`.
- An empty body means **every user**. Phase C's organization variant is scoped to one organization; this one is not.
- Failure scenario:
  1. A user opens Stripe checkout at 10:00 and leaves the tab open.
  2. At 11:30 an admin clears one blocked user with `POST /api/user/topup/expire {}` and forgets the body.
  3. At 12:00 the first user pays. The webhook hits an expired order, the money is captured and no quota is credited. Only manual reconciliation recovers it.
- The record lists "개인 주문 만료와 늦은 웹훅 경합" as untested.
- Fix (either is cheap):
  - (a) Raise `personalTopUpExpireAge` to at least 24h plus margin, and do the same for `organizationTopUpExpireAge` for active orgs.
  - (b) Or keep 1h but require `user_id` (reject an empty body).
  - Document in OpenAPI §3.6 that a payment arriving after expiry needs manual reconciliation.

### Minor

**M1. `Operations` → 작업** (`ko.json:4367`). This is the system-settings nav group that now holds "인터페이스 언어" (`system-settings.config.ts:91`). 작업 is also the translation of `Task`, so the settings group reads like a task area. The record itself says "시스템 설정 → 운영 → 인터페이스 언어". Fix: 운영.

**M2. `Enable FunctionCall thoughtSignature Fill` → "FunctionCall ThinkSignature 채우기 활성화"** (`ko.json:2216`). The value renames the Gemini field identifier. Fix: `FunctionCall thoughtSignature 채우기 활성화`.

**M3. Grammar and particle nits:**
- `Payment initiated` → 결제 시작되었습니다 should be **결제가 시작되었습니다** (`:4560`).
- `Rule {{line}} is missing target path` → {{line}}번째 작업에 대상 경로 없습니다 should be **{{line}}번째 규칙에 대상 경로가 없습니다** (`:5410`).
- `Balance and top-up management` → 잔액 충전 관리 should be **잔액 및 충전 관리** (`:863`).
- `Merchant ID is required` → Merchant ID은(는) should be **Merchant ID는** (`:3672`).
- `Per request` (건당) vs `per request` (요청당) are inconsistent.

**M4. The README.kr.md feature table omits Korean** (`README.kr.md:144`). The list of console languages is a faithful translation of upstream, but this fork ships Korean and the line is in a fork file. Add 한국어 to the list.

**M5. The new Phase D fork files are outside `forkMessageSources`** (`controller/organization_i18n_test.go:18`). `controller/topup_expire_admin.go` and `model/topup_expire_fork.go` are not scanned. `errPersonalTopUpExpireUser = errors.New("invalid user id")` is raw English, though the handler's `< 0` check makes it unreachable today. Add both files to the list.

**M6. Fork audit actions have no `AUDIT_TEMPLATES` entry** (`web/src/features/usage-logs/lib/format.ts`). This covers `topup.expire`, `organization.topup.expire` and every `organization.*` action. A Korean admin sees the raw English `content` in the audit viewer. This confirms the Task 11 deferred minor, and the gap is wider than that minor states.

**M7. The merge gate is manual, and the doc names the wrong symptom.**
- `docs/fork-overlays.md:225` and record §4 say `check` reports `missing` after a merge. The common-i18n overlay row tells the merger to run `bun run i18n:sync`, and sync fills the missing ko keys with English (`reorderLikeBase`), so `check` will report `untranslated (equals english)` instead.
- Either way, nothing in `bun run test` or CI fails, so a merge that skips the manual step ships English silently. English is the fallback, never raw keys.
- Fix: correct the wording, and add a vitest that runs `validateEntry` over en/ko so `bun run test` goes red after the merge.

**M8. Upstream hunks slightly exceed "one line".** The import reshaping in `language-switcher.tsx:30-32` and `language-preferences-card.tsx:26-28` (removing the `INTERFACE_LANGUAGE_OPTIONS` import) and the 6-line import block in `languages.test.ts:21-26` are documented in the overlay rows. They raise the conflict surface a little. Accept.

**M9. Record inconsistencies** (`docs/2026-10-15-rebase-phase-d.md:45`). The text says "배치 1–5는 리뷰 1라운드씩 … 배치 5는 … 2라운드", which contradicts itself. Batch 3's round count also differs: the record says 10 keys, the ledger says 11. These come from the Task 12 deferred minor.

**M10. Test gap: the known-vendor path.** `pricing_discount_refresh_test.go` covers the missing-vendor carry-over. No test asserts that a vendor present in `vendorNameByID` (renamed, or a changed `vendor_id`) beats the previous snapshot. The code is correct by inspection (`known` short-circuits). A one-case test would lock it in.

**M11. Cosmetic.** `common/constants.go:42` appends `// fork: O9` inside a doc-comment sentence ("English when empty. // fork: O9"). It changes 2 upstream comment lines.

## Deferred-minor triage

| Ledger item | Verdict |
|---|---|
| T12 carry: `Log 상세`, `Key 지문`, `Key 출처`, `Key 다이제스트` | **Must fix before merge** (I1, together with `Per 요청` and `Top 모델`) |
| T11: audit actions without a frontend label | Should fix in Phase E. It is broader than stated (M6) and is not a merge blocker. |
| T12: e2e README O9 marker without a row | Accept. `web/e2e/README.md` is a fork file (O3 row), so no row is needed. Optionally drop the marker. |
| T12: record batch 3/5 round counts, screenshot count | Fix with the I1/I2 edits (docs only, M9). The screenshot count (6) is correct. |
| T12: seed `--force` regenerates the batch list | Accept, with one note: `seed --force` also overwrites ko.json from team. The header comment already says this. |
| T11: dangling `vendor_id` after a manual DB edit keeps the old vendor discount | Accept. UI deletion reassigns models first. |
| T11: README links to docs.newapi.ai/en | Accept. No Korean docs exist. |
| T10: no write-time validation; all 8 languages on the first visit; frontend parser duplicates the Go table; fixed menu order | Accept. The read path filters both sides. |
| T8: clipped empty state on the keys page under ko | Should check before release (a high-traffic page), but it is not a merge blocker. I could not verify it visually. |
| T8: borderline ASCII values | Accept, except `Merchant ID은(는)` → `Merchant ID는` (M3). |
| T8: English sr-only text in shadcn components | Accept (upstream). |
| T2: calendar weekday under ko not visually checked; `use-auth-redirect` wiring not directly tested; `kor` not mapped | Accept. `react-day-picker/locale` `ko` is the standard bundle, and browsers do not send `kor`. |
| T1: stale scratch report; glossary count 22 vs 24 | Accept. |

## Declined to judge

- **The 1h age gate as such.** It was plan decision 8. I flag only its interaction with provider session lifetimes and the all-users default (I3).
- **Quota = 쿼터, Tier = 등급, Reset = 초기화.** These are settled rulings; I checked only that they are applied consistently.
- **Formal 합쇼체 questions** (`계정이 없으십니까?`, `비밀번호를 잊으셨습니까?`). They are stiff but follow the glossary's register rule.
- **The six other locales' 66 + 3 English placeholders.** Ruling (plan decision 2).
- **Excel import row errors staying English.** Ruling, recorded for Phase E.
- **Untranslated relay and billing errors that go to AI clients** (`service/billing_session_organization.go` etc.). They are OpenAI-format API errors, not console text.
- **MySQL/PostgreSQL behaviour of the expiry UPDATE.** No containers were available. The queries are GORM-only, and `IS NULL`/`<>` are portable.
- **Visual checks** (clipped keys empty state, calendar weekdays, the Korean e2e smoke). E2E and the browser were out of scope.
- **The merge outcome itself.** Merging was out of scope. I relied on the recorded rehearsal (1 conflict, go.sum).
- **Unsynchronized reads of `lastGetPricingTime` in `GetPricing`.** The race is in upstream code, not Phase D.
- **`gopool.Go` panic recovery semantics.** I trusted the library.
- **Whether `ParseAcceptLanguage` should honour q-weights beyond the first tag.** That is upstream behaviour.
- **Payment-provider sandbox behaviour.** Not runnable here.
- **The quality of all 7,538 values.** I sampled about 330 and scanned all of them mechanically, so I cannot rule out other seeded mistranslations like I2.

## Recommendations

1. Apply the I1 and I2 value edits (9 keys) plus M1–M3. Re-run `node scripts/ko-locale.mjs check`.
2. Fix I3 with a ≥24h age gate or a required `user_id`, and add a sentence to OpenAPI §3.6 about late payments.
3. Add the M7 vitest guard, so the post-merge ko gap fails `bun run test` instead of depending on a manual step.
4. Phase E: audit templates for fork actions (M6), and a seeded-value audit of the billing and pricing editors (the I2 class).

## Verification run by the reviewer

- `GOWORK=off go vet ./... && GOWORK=off go test ./...`: vet clean, 44 packages ok, **0 FAIL**.
- `GOWORK=off go test -race ./model/ -run 'Vendor|Pricing|Discount'`: ok.
- `web: bun run typecheck`: clean. `bun run test`: **212 files / 2,415 tests passed**.
- `node scripts/ko-locale.mjs check`: `7538/7538 ok`. `status`: `english placeholders left: 0`. `node --test scripts/ko-locale.test.mjs`: 17/17.

## Assessment

**Ready to merge: With fixes.** Before merge:
- the I1/I2 ko.json value fixes (9 keys);
- I3 (raise the expiry age gate or require `user_id`).

Everything else is minor or Phase E.
