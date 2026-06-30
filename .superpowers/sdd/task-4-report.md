# Task 4 Report

## Status

DONE_WITH_CONCERNS

## Files Changed

- `web/default/src/features/wallet/types.ts`
- `web/default/src/features/wallet/api.ts`
- `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- `web/default/src/features/wallet/hooks/index.ts`

## What Changed

- Added wallet auto-recharge frontend types for policy records, request payloads, and Toss billing-auth responses.
- Added scoped wallet auto-recharge API helpers for user and organization endpoints.
- Added `useWalletAutoRecharge` to fetch policies, launch Toss billing auth for scheduled/threshold flows, and cancel existing policies.
- Added a compact `AutoRechargeCard` for scheduled and threshold modes with current-policy summary, form controls, submit action, and cancel action.
- Exported the new hook from the wallet hooks barrel to match existing project pattern.

## Typecheck Output

Command run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun install
bun run typecheck
```

Install result:

```text
bun install v1.3.14 (0d9b296a)

Done! Checked 1286 packages (no changes) [80.00ms]
```

Typecheck result:

```text
$ tsc -b
src/features/organizations/components/organization-dashboard.tsx(720,13): error TS2322: Type '(organizationId: string) => void' is not assignable to type '(value: string | null, eventDetails: SelectRootChangeEventDetails) => void'.
src/features/organizations/components/organization-users-table.tsx(83,3): error TS6133: 'getActiveOrganizationSubscriptionUserIds' is declared but its value is never read.
src/features/organizations/components/organization-users-table.tsx(104,7): error TS6133: 'ORGANIZATION_ROLES' is declared but its value is never read.
src/features/organizations/components/organization-users-table.tsx(862,41): error TS2322: Property 'asChild' does not exist on type 'IntrinsicAttributes & Props<unknown>'.
src/features/system-settings/billing/index.tsx(27,7): error TS2740: Type '{ ... }' is missing properties from type 'BillingSettings'.
src/features/system-settings/models/vendor-discount-visual-editor.tsx(344,56): error TS2345: Argument of type 'string | null' is not assignable to parameter of type 'string | number'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(88,20): error TS2769: No overload matches this call.
src/features/usage-logs/components/common-logs-filter-bar.tsx(90,48): error TS2769: No overload matches this call.
src/features/usage-logs/components/common-logs-filter-bar.tsx(91,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(92,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(93,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(94,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(95,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(96,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/common-logs-filter-bar.tsx(97,7): error TS2322: Type '{} | undefined' is not assignable to type 'string | undefined'.
src/features/usage-logs/components/task-logs-filter-bar.tsx(82,20): error TS2769: No overload matches this call.
src/features/usage-logs/components/task-logs-filter-bar.tsx(84,48): error TS2769: No overload matches this call.
src/features/usage-logs/components/usage-logs-mobile-card.tsx(203,63): error TS2339: Property 'created_at' does not exist on type 'NonNullable<TData>'.
src/features/usage-logs/components/usage-logs-mobile-card.tsx(204,58): error TS2339: Property 'type' does not exist on type 'NonNullable<TData>'.
src/features/usage-logs/components/usage-logs-table.tsx(92,5): error TS2322: Type 'UseNavigateResult<string>' is not assignable to type 'NavigateFn'.
src/features/usage-logs/index.tsx(201,15): error TS2322: Type '(organizationId: string) => void' is not assignable to type '(value: string | null, eventDetails: SelectRootChangeEventDetails) => void'.
src/i18n/languages.test.ts(1,40): error TS2307: Cannot find module 'bun:test' or its corresponding type declarations.
```

No typecheck errors were reported from the wallet task files above.

## New i18n Keys Introduced

- `Scheduled recharge`
- `Auto recharge`
- `Recharge amount`
- `Threshold balance`
- `Charge immediately`
- `Cancel`
- `Current policy`
- `Charge interval`
- `day`
- `month`
- `Next charge`
- `Card`
- `No policy configured`
- `You do not have permission to manage this.`
- `Interval value`
- `Minimum recharge amount is {{amount}}.`
- `Please enter a valid interval value.`
- `Please enter a valid threshold balance.`

## Self-Review

- Kept the API layer scope-aware for both `/api/user/...` and `/api/organization/...`.
- Matched the existing Toss billing hook behavior by accepting both `success === true` and `message === 'success'`.
- Kept the card compact and dependency-free, reusing existing UI primitives only.
- Avoided touching wallet page composition, organization screens, locales, backend code, and package metadata.

## Concerns

- Project-wide frontend typecheck is currently red because of unrelated existing errors outside the task-owned wallet files.
- `AutoRechargeCard` introduces new translation keys that still need locale entries in Task 5.

## Review Fixes

- Replaced Korean `t('...')` source keys in `auto-recharge-card.tsx` with English source keys to match the repo's frontend i18n convention.
- Added explicit money-input parsing so blank strings are rejected while explicit `0` remains valid for threshold mode.
- Kept existing auto-recharge policies on refresh failures in `use-wallet-auto-recharge.ts` instead of clearing them.
- Added a focused regression test covering the English mode-title keys and blank-vs-zero money parsing.

## Additional Validation

Regression test:

```text
$ BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
bun test v1.3.14 (0d9b296a)

src/features/wallet/components/auto-recharge-card.test.ts:
(pass) auto recharge card helpers > uses English source i18n keys for mode titles
(pass) auto recharge card helpers > treats blank money input as invalid while preserving explicit zero

 2 pass
 0 fail
Ran 2 tests across 1 file.
```

Focused static check:

```text
$ BUN_TMPDIR=/tmp bun run typecheck
$ tsc -b
```

Task-owned file status:

- No typecheck errors were reported from:
  - `src/features/wallet/components/auto-recharge-card.tsx`
  - `src/features/wallet/hooks/use-wallet-auto-recharge.ts`
  - `src/features/wallet/components/auto-recharge-card.test.ts`

Remaining typecheck failures are unrelated pre-existing errors in organizations, system-settings, usage-logs, and `src/i18n/languages.test.ts`.
