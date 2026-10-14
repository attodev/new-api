# Phase C final review — edf553e5f..889040dbb (22 commits, branch develop)

Reviewer: final whole-branch review (single reviewer, no subagents). Read-only; nothing in the working tree, index or HEAD was changed.

## How the review was done (in passes)

1. **O6 backend:** `setting/ratio_setting/model_discount.go`, `model/pricing_discount.go`, `relay/helper/price_discount.go`, `service/log_discount_fork.go`, and the hooks in `model/option.go`, `model/pricing.go`, `types/price_data.go`, `relay/helper/price.go`, `service/quota.go`, `service/task_billing.go` and `service/log_info_generate.go`. I listed every place in `relay/`, `service/`, `controller/` and `pkg/` that computes a group ratio (`grep GetGroupRatio|GroupRatioInfo`) and followed each quota path: text, Claude, tiered, per-call/MJ, task submit, task token recalculation, task tiered completion, image, audio, WSS realtime, Responses-over-WS, retry/auto-group, violation fee and channel test.
2. **O7 backend:** `middleware/request_timing.go`, `common/timing_response_writer.go`, `relay/channel/timing_body.go`, `service/log_timing_fork.go`, the hooks in `api_request.go`, `relay_info.go`, `relay-router.go` and `context_key.go`, plus `relay/helper/stream_scanner.go` (writer/body concurrency), `middleware/audit.go` (other writer wrappers) and `router/main.go` (registration order).
3. **O3 carry-overs:** `controller/organization*.go`, `model/organization*.go`, `model/topup_target.go`, `service/org_user_export.go`, `service/quota_organization.go`, `service/billing_session_organization.go`, `service/midjourney_organization.go`, `controller/user.go`, `controller/checkin.go`, the router, `fork_access_token_routes.go`, authz, and the top-up settlement code in `model/topup.go`.
4. **Frontend:** pricing (`price.ts`, `discount-badge`, `model-price-cell`, `model-card`, `model-details`, `cached-price-cell`, `pricing-sidebar`), usage-logs (`details-dialog`, `timing-breakdown-section`, columns, mobile card), system-settings (discount editors, `discount-utils`, `group-ratio-form`, `ratio-settings-card`, visual editor) and organizations (`api.ts`, `types.ts`, `organization-users-table.tsx`). A script checked that every `t('…')` key in the changed TS/TSX files exists in all 7 locales.
5. **Docs:** `fork-overlays.md` rows O6/O7, the spec §9 decision 6/7 diff, the record §4/§5, and the ledger.

### Commands run

- `GOWORK=off go vet ./...` exited 0.
- `GOWORK=off go test ./relay/helper/ ./service/ ./common/ ./model/ ./controller/ ./middleware/ ./relay/channel/ ./relay/common/ ./setting/ratio_setting/ ./router/ -count=1` (no race detector): all 10 packages `ok`.
- `GOWORK=off go test -race` on the same set gave `FAIL` for relay/helper, service, model, controller and relay/channel. I compared each failure against the base:
  - **relay/helper** (`TestStreamScannerHandler_*`), **service** (`TestUpdateVideoTasks*`), **relay/channel** (`TestProcessHeaderOverride_*`, `TestApplyUpstreamBodyMetadata*`) and **controller** `TestPreConsumePolicyDatabaseMatrix` fail identically on a `git archive edf553e5f` export. These are upstream races.
  - **model** `TestReserveFallsBackToDatabaseWhenRedisIsUnavailable` is flaky. A full `-race ./model/` rerun passed both at HEAD and at base.
  - **controller `TestOrganizationMemberCannotCheckIn` is new in Phase C and fails under -race** (Minor M1).
- From `web/`: `bun run typecheck` exited 0. `bun run test`: 207 files, 2384 tests, all passed.
- The i18n script found 482 keys used in the changed files, with 0 missing in any of en, zh, zh-TW, fr, ja, ru and vi.
- Not run, as instructed: e2e and any git merge. MySQL and PostgreSQL were not run (no containers).

---

## Strengths

- **The discount is applied at a single point.** `foldDiscountIntoGroupRatio` sits at the end of `HandleGroupRatio`. Every quota path that reads `PriceData` therefore gets the discount exactly once, and pre-consume equals settlement. I traced text, Claude, tiered (`tiered_settle.go:148` refreshes from the already-folded ratio), per-call/MJ (`ModelPriceHelperPerCall`), task submit, task tiered completion (frozen `snap.GroupRatio`), image (`image_billing.go:46,68`), audio (`quota.go:327`), Responses-WS (`responses_websocket.go:307`), auto-group retry (`controller/relay.go:287` rebuilds from the base) and the violation fee.
  - Only two paths bypass `PriceData`: `PreWssConsumeQuota` and `RecalculateTaskQuotaByTokens`. Each multiplies exactly once.
  - `TestDiscountedTextPreConsumeMatchesSettlement` pins pre-consume == settlement.
- **A 100% discount is safe.** `BaseGroupRatio` records the pre-discount group ratio, so nothing ever divides by the multiplier (grep confirms, Go and TS). The logs keep the base `group_ratio`, and the WSS/audio log strings use `discountBaseGroupRatio`.
- **The origin-first ruling closes a real gap.** `GetEffectiveDiscountFor` looks up origin model → origin vendor → billing model → billing vendor, so a "-thinking" variant is charged the discount the marketplace shows. The marketplace (`applyPricingDiscount`) and billing use the same key normalisation (`FormatMatchingModelName`) and the same vendor-ID→name map.
- **`TimingResponseWriter` keeps every writer capability.**
  - `Unwrap()` keeps `http.ResponseController` (SetWriteDeadline, EnableFullDuplex) working.
  - Flush, Hijack, CloseNotify, Status and Size are promoted from the embedded `gin.ResponseWriter` interface.
  - No `io.ReaderFrom` shortcut can bypass `Write`.
  - Writes in the stream scanner are serialised by `writeMutex`, and `wg.Wait()` orders the reads of `lastWrite` and `UpstreamResponseEndTime` after the writer goroutines. The race detector did not flag either field.
  - `timingReadCloser` uses `sync.Once`, and each attempt gets a fresh wrapper.
  - Hijacked WSS sessions correctly skip e2e.
- **Hook discipline is clean in Go.** Every upstream Go file has 0 removed lines except the 4 intended `group_ratio` content lines in `service/quota.go`. There is no gofmt realignment, because the fork fields sit in blank-line-separated blocks. All logic is in fork files.
- **The O3 carry-overs are careful:**
  - The org admin's member updates are scoped (`WHERE id=? AND organization_id=?`, with the MySQL 0-rows nuance handled).
  - The old `status`/`remark` keys are rejected by `DecodeJsonStrict`.
  - Excel refuses the retired columns as a whole sheet.
  - Owner and self rows are guarded in both the API and the import.
  - The suspension read is merged into the existing single membership read, so there is **no extra DB read** on the relay path.
  - LIKE uses the `!` escape (not `\`, which MySQL also treats as a string escape).
  - New-user import is atomic (`CreateUserInOrganization`).
  - Expire is root + casbin + access-token scoped, and the route count of 39 is pinned.
- **Tests and docs.** The review-focus tests named in the plan all exist and pass. All i18n keys are present in all 7 locales. The overlay table rows match the hooks. The merge rehearsal is documented.

---

## Issues

### Critical (Must Fix)

None found.

### Important (Should Fix)

#### I1. Rebuilding the vendor-name cache can drop vendor discounts (overcharge) and puts a pricing rebuild on the relay hot path

**Where:** `model/pricing_discount.go:193-233` (`resetModelVendorNameCache`, `resolveVendorName`, `applyPricingDiscount`) and `model/pricing.go:103`.

**What's wrong:**
- `InvalidatePricingCache()` now sets `modelVendorNameCache = nil`.
- `applyPricingDiscount` then rebuilds the global map **one entry at a time** inside the `updatePricing` loop.
- `resolveVendorName` treats any non-nil map as "built".

**Why it matters:**
- Upstream calls `InvalidatePricingCache` on every channel cache sync (`model/channel_cache.go:29,104,308`) and on every `billing_setting.*` option sync (`model/option.go:745`). `SyncOptions` runs every `SYNC_FREQUENCY` (60 s by default) on every node (`main.go:121`), so the reset is routine, not rare.
- **Partial-map overcharge:** after each reset, a billed request that resolves while another goroutine is still filling the map sees a non-nil map without its model. It gets `""`, loses the vendor discount, and is **charged list price**. The loss is silent and intermittent.
- **Hot-path latency:** the first billed request after each reset (with any vendor discount configured) runs `GetPricing()` → `updatePricing()` synchronously. That is several DB queries over abilities, models and vendors. Concurrent resolvers block on `updatePricingLock` meanwhile.
- **Retry storm on DB failure:** if `updatePricing` returns early (`GetAllEnableAbilityWithChannels` error, `pricing.go:200`), the map stays nil. Every billed request then retries the rebuild, a DB-query storm at the moment the DB is struggling. The deferred "empty pricing table" minor is a narrower case of this.

**Failure scenario:**
1. `VendorDiscount={"OpenAI":10}`, about 500 rps of `gpt-4o-mini`, memory cache on.
2. At the 60 s channel sync, `InvalidatePricingCache` sets the map to nil.
3. Request A calls `GetPricing` and starts the loop.
4. Request B arrives while the loop is at row 200 of 800. `gpt-4o-mini` (row 600) is not in the map yet, so `GetEffectiveDiscountFor` returns `(1,"")`.
5. B is billed 375 instead of 338, and its log has no `discount_*` keys.
6. This repeats on every node, every minute.

**How to fix:**
- Build the model→vendor map in a local variable inside `updatePricing` (pass it to `applyPricingDiscount`), and publish it with one assignment under `modelVendorNameMu` after the loop. This also drops stale entries for removed models.
- In `InvalidatePricingCache`, do **not** nil the map. Vendor membership changes rarely, so serving the previous map until the rebuild publishes is correct.
- Have `resolveVendorName` call `GetPricing()` only on a true cold start (map never published), or never from the relay path, and accept "no vendor discount" until the first `/api/pricing` or background build.
- Add a test that resolves concurrently with `updatePricing` under `-race`.

#### I2. Personal orders created before joining can complete after joining, which defeats decision 6

**Where:** `model/organization.go:317-327` (the join refusal in `assignUserToOrganizationTx`), the order-creation guard `model/topup_target.go:71`, and the settlement functions in `model/topup.go` (`RechargeEpay` etc., which check only `status == pending`).

**What's wrong:**
- The refusal checks `quota`, `aff_quota` and active subscriptions at join time.
- `RejectOrganizationMemberPersonalTopUp` runs only when an order is **created**. Settlement credits `users.quota` without re-checking membership.

**Why it matters:** decision 6 exists so that joining never turns someone's own money into an admin-controlled cap. The spec §9 text says the balance becomes an org-controlled cap and is zeroed on removal. After this sequence, the user's payment:
- buys nothing (the org wallet pays for usage), and
- is wiped by `clearOrganizationMembershipTx` (`quota: 0`) when they leave or are removed.

**Failure scenario:**
1. User U (quota 0) opens a $50 Stripe/Epay checkout.
2. An org admin looks U up and adds them. The refusal passes because quota is 0.
3. U completes the payment and the webhook credits 25,000,000 to `users.quota`. That amount is now U's member cap.
4. Usage is charged to the org wallet.
5. The admin removes U and the quota is set to 0. The $50 is gone.

The window is a single checkout session, so it is narrow but realistic for a user being onboarded.

**How to fix (cheapest first):**
- In the `requireClearedBalance` block, also refuse when U has a pending personal top-up or subscription order. That is one `COUNT` on `top_ups` (`user_id=? AND status='pending' AND target_type IN ('', 'user')`) and the subscription-order table, inside the same transaction. Abandoned orders can be cleared by provider expiry or root, as with org orders.
- Alternative: in settlement, when the payer is now an org member, mark the order for refund instead of crediting.

The ledger moved this to Phase D. I recommend fixing it before merge, because it is a money-loss path in the very feature Phase C delivered and the fix is small. If it is not fixed, record an explicit owner sign-off in the record §4.

### Minor (Nice to Have)

#### M1. A new test fails under `go test -race ./controller/`

**Where:** `controller/organization_personal_wallet_test.go:132-136`.

**What's wrong:** the "outsider still checks in" leg calls `DoCheckin` → `IncreaseUserQuota`. That spawns the async cache-update goroutine (`cacheIncrUserQuota`), which reads `common.RedisEnabled` while the `setupOrganizationControllerTestDB` cleanup writes it.

**Failure scenario:** `-race` reports "race detected during execution of test", so any race-enabled CI run of `./controller/` gets a new red.

**How to fix:** either drop the positive leg (non-member check-in is already covered upstream), or keep Redis off and wait for the goroutine to finish before cleanup.

#### M2. A 100%-discounted model turns into a "free model" and skips the org/suspension checks when free-model pre-consume is off

**Where:** `relay/helper/price.go:140-144`, and `relay/request_billing.go:63` (which skips `PreConsumeBilling`).

**What's wrong:** a 100% discount sets `GroupRatio` to 0. With `EnableFreeModelPreConsume=false` (the default is `true`), `FreeModel=true` and the billing session is never created. A suspended member, or a member of a disabled organization, can then call that model.

This extends the deferred "zero-priced models bypass suspension" minor to models that are paid but fully discounted.

**Failure scenario:**
1. Operator settings: `EnableFreeModelPreConsume=false`, `ModelDiscount={"gpt-4o":100}`.
2. A suspended member calls `gpt-4o` and gets 200 instead of 403.

**How to fix:** run the org-status and member-status check independently of `FreeModel` (for example in `newOrganizationBillingSession`'s caller, or before the free-model short-circuit). Otherwise, add this case to the deferred-free-model note in the record §5.

#### M3. The usage-log list row shows the base ratio without the discount

**Where:** `web/src/features/usage-logs/components/columns/common-logs-columns.tsx:96-111, 315-330` and `common-log-mobile-card.tsx:77-80`.

**What's wrong:** `group_ratio` is now the pre-discount base. The row summary ("Group Ratio 1x") has no discount segment, while the Cost column reflects the discount. Only the details dialog explains the gap.

**Failure scenario:** a user sees `gpt-4`, "Group Ratio 1x", Cost $0.0675, and cannot reconcile that with the list price ($0.09) without opening the dialog.

**How to fix:** add a `Model discount 0.75x` / `Vendor discount …` segment when `discount_multiplier` is present (the i18n keys already exist).

#### M4. The Excel join of an existing unaffiliated user is not atomic

**Where:** `service/org_user_export.go:301-307`.

**What's wrong:** `AssignUserToOrganization` and the following `UpdateOrganizationMemberFields` run in separate transactions. If the field write fails, the user has joined without the sheet's `member_status=suspended`/note, and the row is still counted as `Assigned`.

**How to fix:** mirror `CreateUserInOrganization` by running `assignUserToOrganizationTx` and `updateOrganizationMemberFieldsTx` in one `DB.Transaction`.

#### M5. Two user-row reads per non-GET `/api/organization/*` request

**Where:** `controller/organization_guard.go:36-56`.

**What's wrong:** `IsUserOrganizationDisabled` and then `RequireActiveOrganizationMember` each read the user row.

**How to fix:** read `organization_id` and `organization_member_status` once via `GetUserOrganizationMembership`, then check the organization status. This is a performance nit on a cold path.

#### M6. Three hook lines have no `fork:` marker, and one manual heading is wrong (carried from the Task 11 review)

**What's wrong:**
- `details-dialog.tsx:170` (`export function BillingBreakdown`), `group-ratio-form.tsx:69` (`export type GroupFormValues`) and `relay/common/relay_info_test.go:8` (`"time"` import) are overlay-table rows with no `// fork:` marker, so `git grep "fork: O"` audits miss them.
- The heading of the manual's troubleshooting entry quotes neither the toast text nor the API message.

**How to fix:** add trailing markers to the three lines (a JSX/TS comment at the end of the line is enough), and fix the heading.

---

## Deferred-minor triage (ledger)

| Ledger item | Verdict | Why |
|---|---|---|
| T1: ported ratio_setting tests don't clean up global maps | Leave | Test-order hygiene only. The new tests clean up. |
| T1: empty pricing table → `GetPricing()` per resolve | **Fix before merge (as part of I1)** | It is one symptom of the cache-lifecycle defect in I1. Invalidation happens every sync, not only on an empty table. |
| T2: MJ/task/violation-fee logs carry the discounted `group_ratio` without discount fields; task recalculation passes the origin name twice | Leave (Phase D) | Display only. The amounts billed are correct and consistent. |
| T3: Cached column has no strike-through; badge placement; model details drawer unreachable; dynamic models show the badge beside the undiscounted computed price | Leave (Phase D) | Display only. The dynamic-price case is the most misleading and should be first in Phase D. |
| T4: any `discount_source` other than "vendor" is labelled "Model discount" | Leave | The backend emits only `model`/`vendor`. |
| T5: duplicate names last-wins with a warning; vendor free-text input before the query resolves; rows lost on Visual↔JSON toggle | Leave | Admin UX. Validation still blocks invalid saves. |
| T6: `DoWssRequest`/Bedrock SDK/coze record no `llm_ms`; `SetWriteDeadline` test wraps the writer by hand | Leave | Documented as the same as team. The section hides when `llm_ms` is absent. |
| T7: mobile tokens/s divides by `use_time`; no `gateway_ms` 0 test; DetailsDialog wiring untested; org usage-logs page not browser-checked | Leave | Cosmetic. Same component as the checked page. |
| T8: personal top-up/subscription orders created before joining complete after joining | **Fix before merge (I2)**, or record explicit owner sign-off | Money loss for the user, and it defeats decision 6's purpose. The fix is small. |
| T8: root `CreateOrganization` assigns the owner without the balance check | Leave | Operator-driven and documented. |
| T9: free models bypass suspension; an open realtime WS is not cut on suspension; `users.remark` notes not migrated | Leave, but extend the note with M2 (100% discount + `EnableFreeModelPreConsume=false`) | Operator decisions and documented limits. |
| T11: missing `fork:` markers (3) and the manual heading | **Fix before merge (M6)** | Trivial, and the overlay audit relies on grep. |

---

## Declined to judge

- **Upstream `-race` failures** (stream scanner, video tasks, header override, pre-consume matrix): they reproduce at edf553e5f, so they are not Phase C.
- **MySQL's case-insensitive collation** making `username = ?` lookup case-insensitive: these are upstream username semantics, and the spec does not define "exact" at collation level.
- **Lookup as an email-existence oracle for org admins:** this is the accepted design of decision 6 (exact lookup, identity-only response).
- **Lookup returning platform-disabled accounts:** the table badges them ("Disabled by platform"), and the spec is silent. A reasonable admin sees the state.
- **The 1 h expire age gate and the late-webhook race:** an explicit ruling in the ledger and OpenAPI §3.5. Settlement only credits `pending` under a row lock, so I verified nothing beyond the documented race.
- **Violation fee discounted (100% → no CSAM fee), Excel-created users keeping the `QuotaForNewUser` bonus as their cap, `pre/post_llm_ms` admin-only, WSS timing omitted:** explicit rulings.
- **`gateway_ms` absorbing failed-attempt time on retries:** ruled and pinned by a test.
- **Translation quality of the 7 locales:** review is deferred to Phase D.
- **Badge colours and placement versus the team design:** visual design, outside the plan's correctness scope.
- **Phase B I5 (org log/dashboard scoping to the join time):** listed as not covered in Phase C.
- **MySQL/PostgreSQL execution and the real payment-provider sandbox:** not available in this environment. I reviewed the SQL fragments for parity instead (`LOWER(email)`, `LIKE … ESCAPE '!'`, GORM conditional UPDATEs, ADD-COLUMN-only migration with defaults 1 and `''`).
- **The `RequestTiming` writer wrapping web/static routes registered after the relay router:** same as team and harmless (one small allocation per request).

## Hot-path accounting (requested)

**Extra DB reads per relay request introduced by Phase C:**

| Caller | HTTP relay | WSS realtime, per response | Non-GET `/api/organization/*` |
|---|---|---|---|
| Non-org user | 0 | 0 | — |
| Org member | 0 (the existing single membership read now selects 2 columns) | +1 (`GetOrganizationQuota` pre-check) | +1 (M5) |

The vendor resolver reads 0 rows in steady state. After each pricing invalidation, one request runs a full pricing rebuild synchronously (I1).

**Locks per request:**
- Shared read locks only: `RWMap` RLocks on the model and vendor discount maps (≤4), plus `modelVendorNameMu.RLock` (≤2, only when vendor discounts exist).
- No exclusive lock, except while blocking behind a rebuild on `updatePricingLock` (I1).

**Timing wrapper cost:** one small heap allocation per request on the routes it wraps, plus one `time.Now()` per `Write`.

## Recommendations

1. Fix I1 (local build + atomic swap, no nil on invalidate, no `GetPricing` from the billing path) and add a concurrent `-race` test for it.
2. Fix I2 with the pending-order `COUNT` in the join refusal. Add a test: pending personal order → assign refused; after expiry → assign allowed.
3. Fix M1 and M6 (both trivial). Consider M3 (one log-row segment) and M4 (one transaction).
4. Before production: run the MySQL/PostgreSQL DSN-gated tests and the schema rehearsal (record §5), and backport `TimingResponseWriter.Unwrap()` to team.

## Assessment

**Ready to merge? With fixes.**

The billing core is sound: the discount is folded once at a single point, pre-consume equals settlement on every path I traced, a 100% discount never divides, and the writer wrapping and org authorization changes are correct. Two money issues should be fixed first:
- I1: the vendor-name cache lifecycle silently drops vendor discounts and puts a pricing rebuild on the relay path roughly every sync interval.
- I2: pending personal orders let a user's payment be absorbed by joining an organization.

M1 and M6 are cheap and should go in the same fix wave.
