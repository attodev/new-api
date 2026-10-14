# Phase B final fix wave — report (base e70d9c0dd, head 1d53e05d4)

Branch develop, worktree /Users/molla/work/ai/new-api-rebase. 9 commits, all with a failing-first test for every behavioural fix.
Trailer note: commits carry `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` (the model that wrote them) instead of the brief's `Claude Fable 5.1` line.

## Commits

| # | sha | subject |
|---|---|---|
| 1 | abea129c1 | fix(organization): keep the member cap out of personal-balance flows |
| 2 | 8ff09cfae | fix(organization): refund an ex-member's cap only while still in the pinned org |
| 3 | 27b481a86 | fix(organization): apply the self-protection rule to import and plan assignment |
| 4 | 1d2468dec | fix(organization): make a disabled organization unusable |
| 5 | 581327357 | fix(organization): apply the log range limit to organization log searches |
| 6 | 1cc73066b | perf(organization): drop the extra user reads from the relay hot path |
| 7 | aba840d42 | fix(organization): close the final-review minor backend findings |
| 8 | 207387c6d | fix(organization): support renaming, quote Waffo per provider, list every org |
| 9 | 1d53e05d4 | docs: record the phase B final fix wave and its open product decisions |

## Per finding

**C1** — `model/subscription.go:792-794` hook: `rejectOrganizationMemberBalancePay(&user)` on the locked user row in `PurchaseSubscriptionWithBalance` ("organization members cannot pay with personal balance"). Logic in `model/organization_personal_wallet.go`. Test `TestOrganizationMemberCannotBuySubscriptionWithBalance` (controller; quota, `user_subscriptions`, `subscription_orders` unchanged). Commit 1.

**M10 (partial)** — redemption: `controller/user.go:1295-1297` hook `rejectOrganizationMemberRedeem(c, id)` (controller-level so the client gets a clear message; `model.Redeem` maps every error to a generic one). Personal paid top-ups: the single common entry is `validateTopUpTargetCapacity` → `model.ValidateTopUpTargetCapacity` (fork file), which every pay and amount flow (Epay pay/amount, Stripe, PayPal, Creem, Waffo, Waffo Pancake) reaches; user targets with `organization_id > 0` are refused. Tests `TestOrganizationMemberCannotRedeemIntoMemberCap`, `TestOrganizationMemberCannotStartPersonalPaidTopUp` (PayPal personal pay refused before any provider call; the org flow of the same owner still works). Commit 1. Remainder (pre-existing personal balance/subscriptions of a joining user) → Phase C.

**I1** — `model.IncreaseOrganizationMemberQuota` (conditional `UPDATE users SET quota = quota + ? WHERE id = ? AND organization_id = ?`, then `InvalidateUserCache`); `adjustWalletQuotaForOrganization` refund branch uses it via `refundMemberLimit` and only rolls back a member credit that happened; org wallet still refunded. Midjourney: fork field `Midjourney.OrganizationId` (`model/midjourney.go:29`, AutoMigrate adds the column — verified on SQLite by `TestMidjourneyTaskRowPersistsPinnedOrganization`), set in `prepareMidjourneyOrganizationBilling(relayInfo, task)`; `RefundMidjourneyQuota` hooks now `refundMidjourneyUserQuota(task, quota)` (`service/midjourney.go:105`) and `refundMidjourneyOrganizationQuota(task, quota)` (`:109`) — refund the pinned org, credit the member only while still in it, personal tasks never credit an org. Rewritten tests `TestOrganizationWalletFundingPinsOrganizationAcrossMembershipChange`, `TestTaskRefundUsesPinnedOrganization` (member removed via `ClearOrganizationMembership`, then joins org 2; member quota stays 0). New: `TestOrganizationWalletFundingRefundCreditsMemberStillInPinnedOrganization`, `TestOrganizationWalletFundingRefundAfterRemovalDoesNotMintPersonalBalance`, `TestTaskRefundCreditsMemberStillInPinnedOrganization`, `TestRefundMidjourneyAfterMemberMovedRefundsPinnedOrganizationOnly`, `TestRefundMidjourneyPersonalTaskDoesNotCreditJoinedOrganization`; `TestPrepareMidjourneyOrganizationBillingPinsOrganization`/`TestRefundMidjourneyOrganizationQuota` updated. Commit 2. Caveat: MJ tasks charged before the column existed refund like personal tasks (only pre-deploy data).

**I3** — `ImportOrgUsersFromFile(f, orgId, actorId, removeAbsent)`: actor's own row → row error "an import cannot change the importing admin's own row"; `remove_absent` skips the actor. `AssignOrganizationUserSubscription`/`CancelOrganizationUserSubscription`: non-root `target.Id == actor.Id` → "organization admins cannot manage their own subscription". Tests `TestImportOrgUsersRejectsTheActorsOwnRow`, `TestImportOrgUsersRemoveAbsentNeverRemovesTheActor`, `TestOrganizationAdminCannotAssignOrCancelOwnSubscription`. Commit 3.

**I4** — Relay: `requireActiveOrganization` in `newOrganizationBillingSession` (403 `access_denied`, "organization is disabled") and `model.RequireActiveOrganization` in `CheckMidjourneyOrganizationBilling`. Top-up: `ValidateOrganizationWalletCapacity` reads `status` and refuses. Admin mutations — chosen single choke point: middleware `RejectDisabledOrganizationMutation` (`controller/organization_guard.go`) registered once on the `/api/organization` group in the fork router; refuses every non-GET request from a member of a disabled org (covers member edits, membership, import, plans, plan assignment, org wallet pay/amount); reads keep working; root bypasses. Tests `TestDisabledOrganizationMemberRelayIsRefused`, `TestDisabledOrganizationMidjourneyIsRefused`, `TestDisabledOrganizationTopUpIsRefused`, `TestDisabledOrganizationRefusesAdminMutations`. Commit 4.

**I5** — `limitOrganizationLogTimeRange` applies upstream `limitLogTimeRange` for callers below RoleAdminUser in `GetOrganizationLogs`, `GetOrganizationTask` (seconds) and `GetOrganizationMidjourney` (milliseconds converted). Tests `TestOrganizationLogsApplyTheLogRangeLimit` (wider range rejected with upstream's message, missing start clamped, root unlimited), `TestOrganizationMidjourneyLogsClampAMissingStart`. Commit 5. Not done (out of brief scope, recorded for Phase C): dashboard range limit, membership-bounded history.

**I6 + M3** — `UpdateOrganizationUsedQuotaForUser(userId, q)` → `UpdateBillingOrganizationUsedQuota(organizationId, q)` at all 7 call sites (text, WSS, audio, violation fee, task log, 2× Midjourney) using `relayInfo.BillingOrganizationId`; 0 → no-op, no read. `newOrganizationBillingSession` and both MJ guards read only `organization_id` (`model.GetUserOrganizationId`, keeps GetUserById's "ID is empty!" for id 0). One single-column PK read per request remains — recorded as accepted. Tests `TestTaskConsumptionUsedQuotaGoesToPinnedOrganization`, `TestUpdateBillingOrganizationUsedQuota_NoOrganizationIsNoOp` (+2 renamed). Commit 6.

**M4** — `ErrInsufficientOrganizationWalletQuota` (does not wrap `ErrInsufficientWalletQuota`) from both org fundings' wallet reserve; fork hook `organizationPreConsumeError` at `service/billing_session.go:235-237` maps it to 403 "organization quota insufficient, remaining: …". Test `TestOrganizationWalletShortfallNamesTheOrganizationWallet`. Commit 7.
**M5** — `clearOrganizationMembershipTx` cancels active org subscriptions (shared `cancelActiveOrganizationUserSubscriptionsTx`). Test `TestClearOrganizationMembershipCancelsActiveOrganizationPlan`. Commit 7.
**M6** — `DeleteOrganizationClearingOwner` refuses while pending org-targeted top-ups exist. Test `TestDeleteOrganizationRefusesWhilePendingTopUpExists`. Commit 7.
**M8** — `requireOrganizationSubscriptionPlan` reads the row first (MySQL reports 0 RowsAffected for a no-op update) → 404, no audit row. Test `TestOrganizationPlanUpdateAndDisableReturnNotFoundForForeignPlan`. Commit 7.
**M9** — `model.SetOrganizationMemberQuota`: one absolute, org-scoped `UPDATE` for existing and new members. Test `TestSetImportedOrganizationMemberQuotaIsAbsolute`. Commit 7.
**I7 (partial)** — import writes `remark` only when the column exists. Test `TestImportOrgUsersKeepsRemarkWhenColumnIsAbsent`. Commit 7. Rest → Phase C product decision.
**Known (subscription/self)** — returns the caller through `organizationUserView` (frontend does not read `.user`, kept the documented shape). Test `TestMyOrganizationSubscriptionDoesNotReturnTheFullUser`. Commit 7.

**M1** — `updateOrganizationRequest.Name` with create validation + explicit uniqueness check, audited. Test `TestOrganizationRootCanRenameOrganization`; `TestOrganizationRootRejectsInvalidUpdateInput` now uses `owner_user_id` as its unsupported field. No dialog vitest exists. Commit 8.
**M2** — `calculateOrganizationWaffoAmount` (`POST /api/organization/waffo/amount`, same `{amount}` → `{message, data}` shape as the handler) in the `waffo` slot. Vitest: org-wallet "quotes Waffo with the organization Waffo calculator", api "calculateOrganizationWaffoAmount quotes through the organization Waffo endpoint"; wallet mock updated. Commit 8.
**M12** — page size is already the API maximum (100, `common.GetPageQuery`) and the list endpoint has no keyword param, so `getAllOrganizations` pages through all organizations; used by the usage-logs, dashboard, subscriptions and users root selectors. Vitest "getAllOrganizations pages past the 100-item API maximum"; two component test mocks renamed. Commit 8.

## Verification (head 207387c6d for code)
- `GOWORK=off go vet ./...`: exit 0. `GOWORK=off go test ./...` (-count=1): one failure found and fixed (stale `name` unsupported-field case, folded into commit 8); `make test`: ok 54 / no test files 46 / FAIL 0.
- Go tests: 28 new test functions (+2 renamed), 5 rewritten.
- web: typecheck 0; vitest 203 files / 2318 tests (+3); oxlint + oxfmt clean on the 10 touched files; build ok, `routeTree.gen.ts` unchanged.
- e2e: fresh SQLite, `POST /api/setup` root/12345678, `E2E_REUSE_SERVER=1 bun run e2e` → 19 passed (52.2s).

## Docs (commit 9)
Phase B record §1 bullets 2–3, §3 rulings (incl. I2/I7/M7/M11/M10-rest → Phase C), §4 Phase C list, §5 verification; OpenAPI §2.1 (disabled org, personal-balance rules), §3.3 name, §3.4 pending-order refusal, §4.3–4.5 range limit (MJ ms), §5.6, §5.8, §6.1, §6.4–6.8, §7, §8, §9; spec §4.4 three lines, §9 items 6–7; fork-overlays rows for every new hook and shifted lines, verified with `git grep -n "fork: O"`.

## Not done
- I5 dashboard range limit / membership-bounded logs (beyond the brief; listed in record §4).
- Pre-existing overlay-doc nits not touched: `sidebar-modules-section.tsx` row path is relative, `common-log-mobile-card.tsx:377` has no marker.
