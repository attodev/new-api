# Final fix wave — report (base 889040dbb → HEAD 37dce8101)

Status: all eight findings fixed. All verification gates passed.

## Commits
| sha | subject | findings |
|---|---|---|
| 2cb7cd7a7 | fix(discount): publish the vendor-name map as one snapshot and keep it across invalidation | I1 |
| dc1d30110 | fix(organization): refuse joins with pending personal orders and make the existing-user import atomic | I2, M4 (+ M6 manual heading) |
| b07f61b42 | fix(discount,organization): keep fully discounted models off the free-model gate, read the org actor once, de-race the check-in test | M1, M2, M5 |
| 675e797ae | feat(usage-logs): hint the model/vendor discount next to the list row's group ratio | M3 |
| 37dce8101 | docs: record the phase C final fix wave and mark the last unmarked overlay hooks | M6 markers, record and overlays |

## Per finding
- **I1:** `updatePricing` now fills a local `modelVendorNames` map and swaps it in with `publishModelVendorNames` after the loop.
  - `InvalidatePricingCache` no longer clears the map (its hook was removed).
  - The resolver reads only the snapshot. On a cold start (no snapshot yet), `coldBuildModelVendorNames` calls `GetPricing()` at most once per minute. This is the **negative-cache timestamp** option, serialised by its own mutex.
  - An empty table publishes an empty non-nil snapshot.
  - Tests (model): `TestResolveVendorNameReadsPreviousSnapshotDuringRebuild` (a), `TestInvalidatePricingCacheKeepsVendorNameSnapshot` (b), `TestResolveVendorNameColdStartBuildsAtMostOncePerWindow` (c), and `TestUpdatePricingPublishesVendorNamesAtomically`, a concurrent invalidate/rebuild vs resolve run that passes under `-race`.
  - Overlay row `model/pricing.go:225,227,234,436,454`.
- **I2:** `hasPendingPersonalOrderTx` runs inside the `requireClearedBalance` block. It counts:
  - `top_ups` rows with status pending and `target_type IS NULL OR <> 'organization'` (same rule as `EffectiveTargetType`);
  - `subscription_orders` rows with status pending. These orders are always personal; the status constant is `common.TopUpStatusPending`.
  - The API message is now `user still has a personal balance, affiliate balance, subscription or pending orders; ...` and the toast is `This user still has a personal balance, subscription or pending orders` (7 locales, frontend matcher constant and test). OpenAPI, the management, user and test manuals were updated.
  - Tests (controller): `TestAssignRefusesUserWithPendingPersonalTopUp` (target_type `user` and `''`, the org-wallet order does not block, joining succeeds after expiry), `TestAssignRefusesUserWithPendingSubscriptionOrder` (joining succeeds after `ExpireSubscriptionOrder`). Excel: `TestImportOrgUsersRefusesUnaffiliatedUserWithPersonalBalance` gained a pending-order row.
  - Test DBs now migrate `SubscriptionOrder`. In the upstream `service/task_billing_test.go` this is a one-line `// fork: O3` hook; its overlay row was updated.
- **M4:** new `model.AssignUserToOrganizationWithMemberFields` runs the assignment and the field write in one transaction. A field failure is reported as `OrganizationMemberFieldsError`, and the import keeps the `failed to set member status or note:` prefix. Test: `TestImportOrgUsersKeepsExistingUserOutsideWhenMemberFieldWriteFails`.
- **M1:** there is no completion signal and no switch for the `IncreaseUserQuota` cache goroutine. The outsider leg now asserts `rejectOrganizationMemberPersonalInflow` lets a non-member through and writes nothing; the full `DoCheckin` was what spawned the goroutine. Before the fix, `-race ... 'CheckIn|Organization' -count=3` reported a race. After the fix it passes.
- **M2:** the four free-model conditions in `relay/helper/price.go` (142, 248, 262, 387) use `freeModelGroupRatio`: `BaseGroupRatio` when a discount was folded in, otherwise `GroupRatio`.
  - Tests (relay): `TestFullDiscountStillRefusesSuspendedOrganizationMember` returns 403 from the real `PrepareRequestBilling`. `TestFullDiscountBillsActiveOrganizationMemberZero` shows the session runs, reserves 0, leaves the wallet unchanged, and logs `discount_multiplier` 0.
  - Tests (helper): `TestFreeModelGroupRatioIgnoresDiscount`, `TestFullDiscountPerCallModelIsNotFree`.
  - Documented in `docs/discount/admin-guide.md` and `design.md`.
- **M5:** the guard reads the actor row once (`GetUserById`). It checks the organization status (new `model.IsOrganizationDisabled`) and suspension, then stores the row under `organizationGuardActorKey`. `getOrganizationActor` returns a copy of it.
  - Tests: `TestGuardedOrganizationWriteReadsActorRowOnce` counts membership reads of the actor during a guarded PATCH: 3 before, 1 after. The audit log's username-only lookup is excluded. `TestGetOrganizationActorReusesGuardRow` also passes.
- **M3:** new `lib/discount-hint.ts` (`formatDiscountHint`) and `components/discount-hint.tsx`. The hint appears in the desktop token cell (also when the ratio is 1x and hidden), in the summary fallback segment, and in the mobile card (whose condition now also covers a discount at 1x). Tests: `lib/__tests__/discount-hint.test.ts` and `components/__tests__/discount-hint.test.tsx`.
- **M6:** the three markers were added.
  - oxfmt moves a trailing `//` comment into the type body, so the two TS markers sit on the line just above the export; the overlay rows now read `170-171` and `69-70`. The Go marker is at the end of the line.
  - The manual heading now quotes the toast, and the body gives the API message.
  - The overlay rows for every touched upstream file were checked against the markers by a script, which also fixed drift in O3/O7 rows of the same files.

## Verification
- `go vet ./...`: exit 0.
- `go test ./... -count=1`: ok 44, no test files 39, FAIL 0.
- `go test -race ./controller/ -run 'CheckIn|Organization'`: ok (`-count=1` and `-count=3`).
- web:
  - typecheck: exit 0.
  - `bun run test`: 209 files, 2398 tests passed.
  - build: OK.
  - oxlint and oxfmt on touched files: clean.
- e2e (fresh SQLite, root/12345678): 20 passed (49.0s).

## Choices the brief left open
- I1 (c): negative-cache timestamp (1 minute), not a once-flag, so a node recovers after a DB outage without another pricing caller.
- I2: an abandoned pending personal order that the provider never expires (Epay top-up) blocks the join until an operator sets the order's status to `expired` in the DB. There is no root endpoint for personal orders. This is documented in OpenAPI, the manual and record §5 as a Phase D candidate.
- M1: the guard is asserted directly, because there is no goroutine completion signal.
- Commit trailer: `Co-Authored-By: Claude Opus 5.5` (the session's attribution) instead of the constraints file's `Claude Fable 5.1`.

## Noted, not fixed (record §5)
- `service/tiered_settle.go:180` (upstream): an auto-group retry from a free group to a 100%-discounted paid group still skips session creation.
- `common-log-mobile-card.tsx:381` (O3 `&& showUserInfo`) has an overlay row but no line marker.
