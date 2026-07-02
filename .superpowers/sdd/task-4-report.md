# Task 4 Report: User Wallet Toss Amount Mode Flow

## Status

DONE_WITH_CONCERNS

## Summary

- Added user-wallet Toss amount mode state with `krw` as the default.
- Passed `amount_mode` to Toss amount calculation and Toss payment session creation.
- Parsed structured Toss quote responses so payment amount uses `charge_amount` while legacy string responses still work.
- Added a Toss mode selector to the recharge card with English i18n source keys: `KRW based`, `Quota based`, `Credit`, `Pay`, `Payment amount`, and `Credit amount`.
- Rendered KRW presets as won payment amounts with wallet credit preview.
- Rendered quota presets as wallet quota amounts with KRW payment preview.
- Updated custom amount preview to mirror the active Toss mode.
- Updated the payment confirmation dialog to show Toss KRW payment amount and wallet credit amount clearly.
- Preserved non-Toss preset, custom amount, payment amount, and confirmation behavior.

## TDD Evidence

### RED

```text
$ cd web/default && bun test src/features/wallet/components/recharge-form-card.test.tsx
AssertionError: The input did not match the regular expression /KRW based/.
AssertionError: The input did not match the regular expression /Quota based/.
0 pass
2 fail
```

This was the expected failure because `RechargeFormCard` did not yet render the Toss amount mode selector or Toss mode-specific preset previews.

### GREEN

```text
$ cd web/default && bun test src/features/wallet/components/recharge-form-card.test.tsx
2 pass
0 fail
Ran 2 tests across 1 file.
```

```text
$ cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts src/features/wallet/components/recharge-form-card.test.tsx
7 pass
0 fail
Ran 7 tests across 2 files.
```

## Verification

```text
$ cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts src/features/wallet/components/recharge-form-card.test.tsx
7 pass
0 fail
Ran 7 tests across 2 files.
```

```text
$ cd web/default && bun run typecheck
$ tsc -b
```

`bun run typecheck` fails from pre-existing unrelated TypeScript errors outside the Task 4 edited files. Representative errors from the final run:

- `src/features/organizations/components/organization-dashboard.tsx(720,13): Type '(organizationId: string) => void' is not assignable to type '(value: string | null, ...) => void'.`
- `src/features/organizations/components/organization-users-table.tsx(83,3): 'getActiveOrganizationSubscriptionUserIds' is declared but its value is never read.`
- `src/features/system-settings/billing/index.tsx(27,7): ... missing properties from type 'BillingSettings': PayPalClientId, PayPalClientSecret, PayPalWebhookID, PayPalSandbox, and 2 more.`
- `src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`
- `src/features/usage-logs/components/common-logs-filter-bar.tsx(88,20): No overload matches this call.`
- `src/features/wallet/lib/auto-recharge-options.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`
- `src/i18n/languages.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`

No errors from these edited files remained in the final typecheck output:

- `web/default/src/features/wallet/hooks/use-payment.ts`
- `web/default/src/features/wallet/hooks/use-toss-payment.ts`
- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/wallet/components/recharge-form-card.tsx`
- `web/default/src/features/wallet/components/recharge-form-card.test.tsx`
- `web/default/src/features/wallet/components/dialogs/payment-confirm-dialog.tsx`

```text
$ git diff --check
```

No whitespace errors.

## Files Changed

- `web/default/src/features/wallet/hooks/use-payment.ts`
- `web/default/src/features/wallet/hooks/use-toss-payment.ts`
- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/wallet/components/recharge-form-card.tsx`
- `web/default/src/features/wallet/components/recharge-form-card.test.tsx`
- `web/default/src/features/wallet/components/dialogs/payment-confirm-dialog.tsx`
- `.superpowers/sdd/task-4-report.md`

## Self-Review

- Confirmed new visible UI text uses English source keys and locale JSON files were not edited.
- Confirmed organization wallet files and backend files were not edited.
- Confirmed non-Toss amount requests do not receive `amount_mode`.
- Confirmed Toss quota-mode minimum checks compare the estimated KRW charge rather than the quota input value.
- Confirmed missing `amountMode` defaults to `krw` to preserve backward-compatible Toss semantics.

## Concerns

- `bun run typecheck` remains red due to unrelated pre-existing TypeScript errors listed above.
