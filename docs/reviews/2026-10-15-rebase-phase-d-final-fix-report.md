# Phase D final fix wave: report (base 223770cdc → c9d46fdae)

Status: **done**. All FIX items are applied. The DEFER items are recorded in the Phase D record §6 and §8.

## Commits
| Commit | Subject |
|---|---|
| 0e62b83ca | fix(billing): keep checkouts payable for a day before expiry and require user_id |
| 1d657ff31 | fix(i18n): finish half-English and mistranslated Korean values |
| c218351dd | test(i18n): gate merges on Korean coverage and lock in the known-vendor path |
| c9d46fdae | docs: record the phase D final fix wave |

Each commit ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. The repository hooks passed on every commit.

## Per finding

**I3: expiry age gate and required user_id** (0e62b83ca; docs also in c9d46fdae)
- Changes:
  - `personalTopUpExpireAge` and `organizationTopUpExpireAge` are now `24 * time.Hour`.
  - `POST /api/user/topup/expire` rejects a missing or zero `user_id` with `common.ApiErrorT("user_id is required")`. A negative or malformed value still returns `Invalid parameters`.
  - The model has no all-users branch any more. It rejects `userId <= 0`.
  - The disabled-organization override is kept. Its comment, the controller doc and OpenAPI §3.5 state the payment-drop risk.
- New key: `user_id is required`, added to all 8 locales. ko is `user_id는 필수입니다`; the other 6 locales hold an English placeholder.
- Docs updated: OpenAPI §3.5/§3.6 (with a late-payment manual-reconciliation sentence), the manual, the Phase C record (annotated, not rewritten), the Phase D record and spec §9 decision 14.
- Tests, written first (red, then green):
  - Personal and organization tests: orders 25 hours old expire and orders 2 hours old are kept. These failed under the 1h gate.
  - The all-users sweep at the end of the personal test is replaced by an assertion that the other user is untouched.
  - New `TestAdminExpirePendingTopUpsRequiresUserId`: an empty body, `{}` and `{"user_id":0}` are rejected with the `message_key`, nothing changes and no audit row is written. It also asserts that the model's `ExpirePendingPersonalTopUps(0)` returns a `*common.Message`.

**M5** (0e62b83ca, folded into commit 1 because it touches the same files)
- Both Phase D fork files are now in `forkMessageSources`.
- `errPersonalTopUpExpireUser` is now `common.NewMessage("Invalid user ID")`, a key that already exists in all 8 locales.
- The source scan went red on the new `user_id is required` key until that key was added.

**I1, I2, M1–M4 and the ASCII scan** (1d657ff31). All ko.json edits went through a Node merge script.
- I1: `로그 상세`, `키 지문`, `키 출처`, `키 다이제스트`, `요청당`, `인기 모델`.
  - `Key Summary` is now `키 요약` (follow-up commit): the value shows `key_hint`, not a hash.
- I2: `입력 가격`, `출력 가격` and `출력 가격 ($/1M 토큰)`.
  - I kept the space before the parenthesis to match the English exactly.
  - Siblings aligned: `Enter Completion price to calculate ratio` (완료 가격 → 출력 가격) and `Audio comp.` (오디오 완성 → 오디오 출력).
- M1: `운영`. M2: `FunctionCall thoughtSignature 채우기 활성화`.
- M3: `결제가 시작되었습니다`, `잔액 및 충전 관리`, `Merchant ID는` and `Per request` = 요청당. The `Rule {{line}} …` message is fixed together with all 7 of its siblings, so all 8 read `{{line}}번째 규칙…`.
- M4: README.kr.md line 144 now lists 한국어. The protected-name counts are unchanged.
- ASCII scan: 7 further half-translations fixed.
  - The ≥4-letter scan found 5: `Reasoning_content` → `reasoning_content`, `New API 콘솔`, `핵심 API`, `회사 이름` (from Your Company) and `핑 간격`.
  - An extra 2–3-letter scan found 2: `Configure at:` → `설정 위치:` (the value was garbled as `at: 구성`) and `예: 사내 GitLab`.
  - I also removed zero-width spaces from 3 values. One of them also fixes the garbled `$/1,000호 호출` → `$/1K 호출`.
  - The rest of the ASCII words are brands, product or API names, identifiers or protocols, and were left as they are.

**M7** (c218351dd; record wording in c9d46fdae)
- `ko-locale.mjs` now exports `findLocaleIssues`, which `check` reuses. Types are in the new `web/scripts/ko-locale.d.mts`.
- New `web/src/i18n/__tests__/ko-coverage.test.ts` (AGPL header) has two cases:
  - the real en/ko/skip files must produce no issues;
  - a synthetic case proves that a missing key and an English placeholder are both reported.
- It was red before the export existed.
- The overlay and record now name the real post-merge symptom: `untranslated (equals english)` after sync, plus a red `bun run test`.

**M9 and the Task 12 minors** (c9d46fdae)
- Record corrections:
  - Batch fix rounds now read "batches 1–4 one round each (14·9·11·18), batch 5 two rounds".
  - Screenshots: 6 (1 from Task 2 plus 5 from Task 8).
- `seed --force` regenerating ko-batches.json is now noted in the record, the overlay and the spec.
- The `web/e2e/README.md` O9 marker is explained in the overlay's O9 memo. It is a fork file, so it gets no row.

**M10** (c218351dd)
- New `TestApplyPricingDiscountKnownVendorBeatsPreviousSnapshot` covers a renamed vendor and a model moved to another vendor.
- A mutation check (removing the `known` guard) makes it fail. The source was restored afterwards.

**M11** (c218351dd): the `// fork: O9` marker moved to the end of line 43, `var DefaultLanguage string`. The overlay row is now `common/constants.go:40-43`.

**Deferred** (record §6 and §8, no code)
- M6: audit templates for `topup.expire` and all `organization.*` actions.
- M8: accepted.
- The clipped empty state on the keys page. It needs a visual check before release.

## Verification (at c218351dd plus the docs)
- Go:
  - `GOWORK=off go vet ./...`: clean.
  - `GOWORK=off go test ./...`: 44 ok, 39 no test files, 0 FAIL, no `---` lines.
  - `go test -race ./model/ -run 'Vendor|Pricing|Discount'`: ok.
- Web:
  - `bun run typecheck`: 0.
  - `bun run test`: 213 files / 2,417 tests passed.
  - `node scripts/ko-locale.mjs check`: 7539/7539 ok. `status`: 0.
  - `node --test scripts/ko-locale.test.mjs`: 17/17.
  - `bun run i18n:sync`: md5 of the 8 locales unchanged.
  - `bun run build`: succeeded.
  - oxlint and oxfmt are clean on the new and touched files.

## Not done
None.
