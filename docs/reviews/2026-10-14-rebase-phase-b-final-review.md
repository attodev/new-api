# Phase B final whole-branch review (149c567a1..e70d9c0dd)

Reviewer: final whole-branch review, read-only. Reviewed in four passes:
(1) backend model/service/billing + every upstream hook, (2) controllers/routes/authz/access-token map/tests,
(3) frontend (`web/src/features/organizations/**`, org routes, wallet/usage-logs/sidebar extensions, auth store),
(4) Playwright config/e2e scaffolding and docs (fork-overlays hook inventory, OpenAPI/manuals spot checks).
The team branch was consulted (`git show team:...`) only to tell inherited behaviour from new behaviour.

Verification run on this head:

- `GOWORK=off go test ./controller/ ./model/ ./service/... ./router/ ./middleware/`: all ok (controller/model/service reported `(cached)` = same inputs already passed; router 1.2s, middleware 0.8s).
- `GOWORK=off go vet ./...`: exit 0.
- `web/`: `bun run typecheck`: exit 0. `bun run test`: 203 files / 2315 tests passed.
- en.json/zh.json check of every `t('…')` key in the organization files, sidebar and usage-logs org entry points: no missing keys.
- Every upstream file touched has its hook listed in `docs/fork-overlays.md`.

---

## Strengths

- **Hook discipline is good.** Upstream files carry one-line `// fork: O3` hooks; the logic is in fork files (`billing_session_organization.go`, `funding_source_organization.go`, `task_billing_organization.go`, `midjourney_organization.go`, `topup_target.go`). Each billing hook falls through for non-org users (`newOrganizationBillingSession` returns `handled=false`, `taskAdjustOrganizationFunding` returns `handled=false` when `OrganizationId == 0`, `postConsumeOrganizationQuota` returns `false` when nothing is pinned). The inventory in `docs/fork-overlays.md` is complete.
- **Atomic reservations.** `TryReserveOrganizationQuota` uses a single conditional `UPDATE … WHERE quota >= ?`. The image path (`requireAvailableQuota`) reserves member and org, or plan and org, atomically with rollback. The org-plan pre-consume reserves the org wallet atomically and refunds the plan record when that fails, which is what Review Focus 1 asks for.
- **Settlement into the org wallet.** `creditOrganizationQuota` reuses upstream's ceiling-in-predicate pattern (`quota <= maxCurrent`). An org order with no target is refused (`errOrganizationTopUpWithoutTarget`), and so is a zero credit. All 7 settlement sites (Epay, Stripe, manual, Creem, Waffo, Waffo Pancake, PayPal) go through `creditTopUpTarget`/`syncCreditTopUpCache`. `ManualCompleteTopUp`'s `topUp` hoisting is correct.
- **The organization is pinned for async work.** `RelayInfo.BillingOrganizationId` is pinned in `TaskPrivateData.OrganizationId`, and task settle, refund and used-quota go to the pinned org.
- **Data-exposure fixes are real.** `organizationUserView` and the column whitelist are used for users, assignable-users, single user and the subscription list. `ListOrganizationUserSubscriptions` now selects only safe columns. `ORDER BY` uses a whitelist plus `clause.OrderByColumn`, which quotes `group` per dialect. `GetMidjourney`/`GetTask` short-circuit an empty member list, which would otherwise mean "no filter".
- **Strict JSON request bodies** (`DecodeJsonStrict`), pointer scalars on patch DTOs, and ranges validated against `MaxWalletQuota`.
- **Real-DB tests.** Tests run against SQLite rather than mocks. Each of the five plan Review Focus items has its pinned test, and all of them pass.
- **Frontend.** The frontend scopes logs per organization cleanly (`useUsageLogsRoute`, `buildOrganizationLogsParams`, and stats/export hidden in org scope), and routes gate in `beforeLoad` with the same predicate the sidebar uses.

---

## Issues

### Critical (Must Fix)

**C1. An org owner/admin can mint personal platform subscriptions out of an organization "member limit" that no money backs.**
`controller/organization.go:714-719` (UpdateOrganizationUser writes `users.quota` to any value up to 2^53-1 without debiting anything), together with upstream `model/subscription.go:759-797` (`PurchaseSubscriptionWithBalance` spends `users.quota` with no organization check), and `model/organization.go:387-405` (`clearOrganizationMembershipTx` zeroes `users.quota` but leaves `user_subscriptions` alone).

- **What's wrong.** Inside an organization, `users.quota` is a spending cap that admins set freely. It is not paid balance. But the upstream personal-wallet consumer `POST /api/subscription/balance/pay` (UserAuth only; `AllowBalancePay` defaults to true) treats it as money.
- **Failure scenario.**
  1. An org owner registers a second account. `assignable-users` lists every unaffiliated user, and assignment needs no consent.
  2. The owner adds it: `PUT /api/organization/users/:id/membership`.
  3. The owner sets its limit with `PATCH /api/organization/users/:id {"quota": 9007199254740991}`. The org wallet is not touched.
  4. The puppet calls `POST /api/subscription/balance/pay {"plan_id": <most expensive plan>}`. The subscription and its order are created, paid from the cap.
  5. The owner removes the puppet. `users.quota` is zeroed, but the personal `user_subscription` survives.
  6. Once out of the org, `NewBillingSession` uses upstream preference logic, so the puppet spends a platform subscription it never paid for.

  A non-owner org admin can skip the second account and raise their own cap through the import (see I3).
- **Why it matters.** Any tenant admin can mint unlimited free platform product. The hole was inherited from team (team's `PurchaseSubscriptionWithBalance` has no org guard either), but this branch ships it.
- **Fix.** Reject balance purchase while `OrganizationId > 0`: lock the user row in `PurchaseSubscriptionWithBalance` and return an error when it is in an org. A fork hook in the upstream model or controller is enough. Add a controller test: org member + balance pay → error, and quota and subscriptions unchanged. While there, decide the same for the personal paid top-up and redemption entry points (see M10).

### Important (Should Fix)

**I1. Refunds after a member leaves credit the ex-member's personal wallet (quota minted); Midjourney refunds go to the wrong organization.**
`service/funding_source_organization.go:50-61` (refund branch of `adjustWalletQuotaForOrganization`) and `service/midjourney_organization.go:374-390`.

- **What's wrong.** `OrganizationWalletFunding.Refund`/`Settle(<0)` and `taskAdjustOrganizationFunding(-q)` pin the org correctly. But they always run `IncreaseUserQuota(memberId, refund)` on the member, even when the member was removed meanwhile, and removal zeroes `users.quota` precisely so the cap never becomes personal money. Legacy Midjourney pins nothing:
  - upstream `RefundMidjourneyQuota` credits `users.quota`;
  - `refundMidjourneyOrganizationQuota` credits the member's *current* org.
- **Failure scenarios.**
  - A member's async video task pre-consumes 2,000,000 from member cap and org wallet A. An admin removes the member (quota → 0). The task fails, so `RefundTaskQuota` runs. Org A gets 2,000,000 back, and the ex-member now owns 2,000,000 of personal balance that nobody paid for.
  - Midjourney: a user is charged as a member of A, removed, then added to B before the task fails. B is credited with A's money, A never gets it back, and the user's personal `users.quota` is credited too.
  - Midjourney charged to a personal user who joins an org before the refund: the org is credited with quota it never paid.
- **Why the tests miss it.** The pinned tests (`organization_billing_test.go:105`, `funding_source_organization_test.go:64`) simulate the move with a raw `UPDATE organization_id`, so the quota zeroing never happens and the user side is never asserted.
- **Fix.** For the member side, credit only while the user is still in the pinned org (conditional `UPDATE users SET quota = quota + ? WHERE id = ? AND organization_id = ?`, then invalidate the cache); otherwise skip the member credit. For Midjourney, pin the organization on the `midjourneys` row (a fork column) or refund the org only when the current org equals the pinned one. Rewrite the tests to remove the member through `ClearOrganizationMembership`.

**I2. Adding a user who isn't in any organization needs no consent: it turns their paid personal balance into an org-controlled cap and exposes every such user to every org admin.**
`controller/organization.go:614-646` (`ListAssignableOrganizationUsers`: `organization_id = ? OR organization_id = 0`), `controller/organization.go:747-805`, `model/organization.go:180-213`, and `service/org_user_export.go:222-234` (an import row with any existing unaffiliated username).

- **What's wrong.**
  - Any org admin can page through every non-admin platform user who isn't in an organization, seeing username, display name, `quota`, `used_quota`, `group` and the platform `remark`.
  - The admin can assign any of them with no opt-in.
  - On assignment, the victim's purchased `users.quota` becomes a member cap. The admin can then `PATCH quota=0` or remove them (zeroed by design), destroying the balance. Until then the victim's spending debits both their own money and the org wallet.
- **Failure scenario.** A user who topped up $500 personally is added to an org by a careless or malicious org admin, then removed a week later. The $500 is gone, and the only record is an audit row's `previous_quota`.
- **Why it matters.** Innocent users lose money. The OpenAPI doc describes the query and the zeroing, but not this consequence. A reasonable user would not expect someone else to be able to wipe their wallet.
- **Fix.**
  - Refuse to assign a user whose `users.quota > 0` (or whose personal subscriptions are active), or keep the personal balance separate.
  - Restrict `assignable-users` to an exact username/email lookup instead of a full listing.
  - Drop `remark`/`quota` from that view.
  - Consider an invite/accept step. This needs a product decision; record it in §9.

**I3. The "organization admins cannot modify themselves" rule can be bypassed through the import and plan assignment.**
`service/org_user_export.go:177-221` (`ImportOrgUsersFromFile` never receives the actor), `service/org_user_export.go:299-322` (remove-absent), `controller/organization_subscription.go:186-194` (no `target.Id == actor.Id` check).

- **What's wrong.** `rejectOrganizationSelfOrOwnerTarget` blocks self-edits on PATCH and membership, but:
  - (a) a non-owner admin can list their own username in an xlsx with `quota=9007199254740991` (raising their own cap past what the owner set, which also feeds C1) or `status=disabled`;
  - (b) `remove_absent=true` with the admin's own row missing removes the admin themselves;
  - (c) `PUT /api/organization/subscription/users/<own id>` lets an admin assign themselves a `total_amount: 0` (unlimited) plan they just created.
- **Failure scenario.** The owner caps admin A at $10. A uploads a sheet containing `A, quota=1000000`. A now spends up to $1M of the org wallet. Equivalently, A creates an unlimited plan and assigns it to themselves.
- **Fix.** Pass the actor into `ImportOrgUsersFromFile`, skip or reject the actor's own row, and never remove the actor. Add the self check to `AssignOrganizationUserSubscription`/`CancelOrganizationUserSubscription` for non-root actors. Add tests for each.

**I4. Disabling an organization has no runtime effect.**
`service/billing_session_organization.go:49-111` (no `Status` check), `controller/topup_target.go:37-51`, `controller/organization.go:223-240`.

- **What's wrong.** `status=2` only stops new assignments (`AssignUserToOrganization`). Members keep relaying against the org wallet and plans, the owner can keep topping it up, and org admins keep managing members.
- **Failure scenario.** Root disables a delinquent tenant org. Its members keep draining (and refilling) the org wallet as if nothing happened.
- **Why it matters.** Team shares the gap, but a reasonable operator expects "disabled" to mean "cannot be used".
- **Fix.** Load the org status in `newOrganizationBillingSession` (and in `CheckMidjourneyOrganizationBilling`) and return 403 when it is not enabled. Refuse org top-up orders and org-admin mutations for a disabled org.

**I5. Org logs and dashboard follow current membership with no time bound: they expose a new member's full personal history, drop departed members' history, and bypass the log range limit.**
`controller/organization.go:523-558`, `controller/organization.go:302-318`, `model/organization_dashboard.go:95-113`.

- **What's wrong.** `user_id IN (current members)`, with no `limitLogTimeRange` (upstream applies it to every non-admin `GetUserLogs`, `controller/log.go:78`).
- **Failure scenarios.**
  - Combined with I2, an org admin assigns any user and immediately reads their whole pre-membership usage log (models, tokens, token names, timestamps, IP where recorded), then removes them.
  - A departed member's spend disappears from the org's usage history and dashboard.
  - Org admins (ordinary users) can run unbounded log scans that upstream forbids for non-admins.
- **Fix.** Apply `limitLogTimeRange` in the org log handlers. Consider bounding queries by membership start: store a `joined_at`, or tag logs and `quota_data` with the billing organization via `other`.

**I6. Every relay request from every user now makes two extra uncached `users` reads.**
`service/billing_session_organization.go:50` (`GetUserById` in `newOrganizationBillingSession`) and `service/funding_source_organization.go:88-99` (`UpdateOrganizationUsedQuotaForUser`, called from `PostTextConsumeQuota`, WSS, audio, tasks, violation fee and Midjourney).

- **What's wrong.** Decision 3 keeps org fields out of the cache, so non-org users now pay two primary-key reads on the hot path (three on Midjourney, counting `CheckMidjourneyOrganizationBilling`). Upstream added the user cache specifically to avoid this. This breaks "no upstream behaviour altered for non-org users" at the performance level.
- **Fix.** In `UpdateOrganizationUsedQuotaForUser`, use the pinned `relayInfo.BillingOrganizationId` rather than re-resolving the user. That also fixes the stats drift in M3. Narrow the session read to `Select("organization_id")`, or add `organization_id` to the cached `UserBase` and re-validate under lock only for org members.

**I7. Org admins can write platform-level user fields: `status` (unbanning root bans) and `remark`.**
`controller/organization.go:707-712`, `controller/organization.go:721-728`, `service/org_user_export.go:202-205`.

- **What's wrong.** `users.status` is the platform ban flag and `remark` is the platform admin note. An org admin can set `status=1` on a member that root disabled for abuse, and read or overwrite root's remark. The import also clears `remark` whenever the column is missing (`updates["remark"] = getCell(...)` runs unconditionally).
- **Failure scenario.** Root bans an org member for abuse. The org admin re-enables them through PATCH or the import, and the ban is silently undone.
- **Fix.** Give org admins a separate org-scoped "membership suspended" flag, or allow them to only *disable* (never re-enable a user that root disabled). Keep `remark` out of the org view, or use an org-scoped note. Only write `remark` on import when the column is present.

### Minor (Nice to Have)

- **M1. Editing an organization's name in the UI always fails.** `web/src/features/organizations/components/organization-users-table.tsx:446-448` sends `name`, but `updateOrganizationRequest` (`controller/organization.go:38-42`) has no `name` field and `DecodeJsonStrict` rejects it. Root changes the name and saves; nothing is saved (description and quota changes included) and the toast says "unsupported field: name". Either support renaming server-side or make the name read-only when editing.
- **M2. The org Waffo quote may not match the actual Waffo charge.** `organization-wallet.tsx:81` prices Waffo with the epay calculator (`calculateOrganizationAmount`), although `POST /api/organization/waffo/amount` exists and upstream prices Waffo with `getWaffoPayMoney`. With separate Waffo pricing or discounts, the owner is shown one price and charged another. (This is separate from the known missing confirm dialog.)
- **M3. Org `used_quota` follows the member's current org instead of the pinned one.** `UpdateOrganizationUsedQuotaForUser` in `text_quota.go:449`, `quota.go:235/375`, `violation_fee.go:131` and `mjproxy_handler.go:301/684`. If a member is removed mid-request, the usage is lost from the org stats (or attributed to a new org). Use `relayInfo.BillingOrganizationId`.
- **M4. Misleading error when an org plan member's org wallet is empty.** `ErrInsufficientWalletQuota` from `OrganizationSubscriptionFunding.PreConsume` (`funding_source_organization.go:192`) is mapped by upstream `preConsume` (`billing_session.go:235-243`) to "user quota insufficient, remaining <personal users.quota>". The status (403) is right, but the text points at the wrong balance. Return a distinct org error.
- **M5. Removing a member leaves their org subscription active.** `controller/organization.go:845`. If they are re-added later, the stale plan silently resumes. Cancel it in `ClearOrganizationMembership`.
- **M6. Org top-ups in flight when an org is deleted cannot settle.** `model/topup_target.go:86-92`. After `DELETE /api/organizations/:id`, a provider callback for a paid org order fails with "organization not found", rolls back, and leaves a paid order pending with no recovery path. Refuse deletion while org orders are pending, or document a manual refund.
- **M7. The legacy Midjourney and realtime paths only check the org wallet loosely.** `CheckMidjourneyOrganizationBilling` checks `organizationQuota > 0`, not `>= price` (`midjourney_organization.go:340`). `PreWssConsumeQuota` (`quota.go:145-149`) checks only `users.quota`. The org wallet can go negative through these paths.
- **M8. Plan update/disable reports success on a foreign or missing plan id.** `controller/organization_subscription.go:93-103` and `:116-125` scope correctly but ignore `RowsAffected`, so `success:true` is returned and an audit row is written for a no-op. Return 404 when nothing matched.
- **M9. Import sets an existing member's cap from a stale read.** `service/org_user_export.go:211`. The delta is computed from `existingUser.Quota` read without a lock, so concurrent spend or refund skews the result. Set the absolute value in one `UPDATE`.
- **M10. Personal paid top-ups and redemptions by org members land in the member cap.** The user `/api/user/pay|stripe/pay|…` and `/api/user/topup` entry points do not check membership. The money is zeroed on removal, and it raises the cap the org admin set. The UI hides these entry points, but the API does not. (Team guarded only Toss.)
- **M11. The `organization` casbin resource is decorative.** `/api/organizations` is wrapped in `RootAuth()` and every handler re-checks root, so granting `organization:*` to an admin does nothing (`router/organization-router.go:24`). Either drop RootAuth in favour of casbin, or note that the permission is root-only.
- **M12. The root org selector loads only the first 100 organizations.** `organization-logs-selection.ts:56` (`size: 100`). Organizations beyond 100 cannot be selected for logs.

---

## Declined to judge

- **`GET /api/organization/subscription/self` returning the full `User`, stale record/OpenAPI sections, unrun MySQL/PG DSN tests and schema rehearsal, dropped Toss/auto-recharge, no Waffo org confirm dialog.** Already known and queued; not re-reported.
- **Plan auto-renewal continuing for disabled plans (`getActiveOrRenewOrganizationUserSubscriptionTx`).** The manual says disabling a plan only excludes it from new assignments.
- **Org quota refunds not checking the ceiling (`IncreaseOrganizationQuota`).** Plan decision 2.
- **Org fields kept out of the user cache.** Plan decision 3; I6 only flags its hot-path cost.
- **Org wallet allowed to go negative at settle (positive delta) and on non-image reserve.** Mirrors upstream wallet debt semantics.
- **Org admins (non-owner) promoting or demoting other admins.** Explicitly specified in OpenAPI §5.5.
- **`total_amount: 0` meaning an unlimited plan.** Team semantics; the org wallet still bounds spend. Only the self-assignment angle is flagged (I3).
- **Owner-only org top-up and no owner transfer.** Specified.
- **Root setting the org quota as an absolute value.** Same model as upstream admin user edits.
- **LIKE case-sensitivity differences across DBs in member search.** Matches upstream user search behaviour.
- **Playwright reusing a tmpdir SQLite file between runs.** E2E was not to be run. A test-hygiene choice, not a correctness issue.
- **Dashboard loading full `users` rows into memory.** Not exposed in the response; performance only, bounded by org size.
- **Legacy soft-deleted users keeping `organization_id` after org deletion.** No reachable effect found.

---

## Recommendations

1. Fix C1 before merge with a one-line org guard in `PurchaseSubscriptionWithBalance` (fork hook) plus a test. Decide M10 at the same time.
2. Fix I1 with membership-conditional member credits, and rewrite the two pinning tests to remove the member through `ClearOrganizationMembership` and assert the member side as well.
3. Fix I3 (actor passed to the import, self checks on plan assignment); it is small and closes the cap-raising route into C1.
4. Take I2, I4, I5 and I7 to a product decision and record the rulings in record §3 / spec §9. Each changes documented team behaviour, so they shouldn't be silently "fixed".
5. Fix I6 and M3 together: use `relayInfo.BillingOrganizationId` for used-quota and narrow the session user read.
6. Fix M1 (rename) and M2 (Waffo quote) in the frontend fix wave.

## Assessment — **Ready to merge? With fixes**

The billing session, top-up targeting and data-exposure work is solid and well tested. But C1 lets any org admin mint free platform subscriptions, and I1/I3 leak money or authority around membership changes. These three should land, with tests, before `develop` is treated as releasable. I2/I4/I5/I7 need explicit product rulings rather than silent acceptance.
