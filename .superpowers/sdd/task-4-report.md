# Task 4 Report: Wallet Page Integration and Verification

## Status

DONE_WITH_CONCERNS

## Summary

- Integrated the personal wallet page with the wallet payment-setting helpers.
- Kept ordinary top-up on the left and moved subscription/auto-recharge/scheduled recharge controls into a right-side `Payment settings` card when at least one setting tab is visible.
- Removed wallet-page subscription purchase plans; wallet now only shows active subscription status in this area.
- Loaded self subscription state and wired refresh, billing preference update, and Toss auto-renew cancellation handlers.
- Enforced frontend mutual exclusion by disabling unconfigured payment setting tabs and passing the lock message to auto-recharge creation controls.
- Added an i18n-backed accessible label to the subscription refresh icon button.
- Added translations for new wallet strings in `en`, `zh`, `fr`, `ja`, `kr`, `ru`, and `vi`.
- Cleaned up a focused wallet test fixture so TypeScript uses the helper's narrow input contract.

## Files Changed

- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
- `web/default/src/features/wallet/components/wallet-subscription-status-card.tsx`
- `web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts`
- `web/default/src/i18n/locales/en.json`
- `web/default/src/i18n/locales/fr.json`
- `web/default/src/i18n/locales/ja.json`
- `web/default/src/i18n/locales/kr.json`
- `web/default/src/i18n/locales/ru.json`
- `web/default/src/i18n/locales/vi.json`
- `web/default/src/i18n/locales/zh.json`

## Verification

```text
$ cd web/default && bun run i18n:sync
i18n sync done. Report: /tmp/new-api-wallet-auto-recharge/web/default/src/i18n/locales/_reports/_sync-report.json
```

Sync report showed `missingCount: 0` and `extrasCount: 0` for every locale.

```text
$ cd web/default && bun test src/features/wallet/lib/payment-settings.test.ts src/features/wallet/components/auto-recharge-card.test.ts src/features/wallet/components/wallet-subscription-status-card.test.ts
23 pass
0 fail
Ran 23 tests across 3 files.
```

```text
$ cd web/default && bun test src/features/wallet
33 pass
0 fail
Ran 33 tests across 6 files.
```

```text
$ cd web/default && ./node_modules/.bin/prettier --check ...
All matched files use Prettier code style!
```

```text
$ cd web/default && bun run build:check
$ tsc -b && rsbuild build
```

`build:check` failed during TypeScript checking before Rsbuild ran. The remaining errors are outside the Task 4 wallet integration surface, except an existing `bun:test` type-resolution issue in another wallet test file. Examples from the final run:

- `src/features/organizations/components/organization-dashboard.tsx(720,13): Type '(organizationId: string) => void' is not assignable to type '(value: string | null, ...) => void'.`
- `src/features/organizations/components/organization-users-table.tsx(83,3): 'getActiveOrganizationSubscriptionUserIds' is declared but its value is never read.`
- `src/features/system-settings/billing/index.tsx(27,7): ... missing properties from type 'BillingSettings': PayPalClientId, PayPalClientSecret, PayPalWebhookID, PayPalSandbox, and 2 more.`
- `src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`
- `src/features/usage-logs/components/common-logs-filter-bar.tsx(88,20): No overload matches this call.`
- `src/features/wallet/lib/auto-recharge-options.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`
- `src/i18n/languages.test.ts(1,40): Cannot find module 'bun:test' or its corresponding type declarations.`

I fixed the only `build:check` error that was inside the focused Task 4 test files (`auto-recharge-card.test.ts` excess properties for `getVisibleAutoRechargeModes`). No errors from `web/default/src/features/wallet/index.tsx`, `wallet-subscription-status-card.tsx`, or `auto-recharge-card.test.ts` remained in the final `build:check` output.

## Self-Review

- Confirmed `SubscriptionPlansCard`, `showSubscriptionPanel`, `handleSubscriptionAvailabilityChange`, and top-level wallet payment tabs were removed from `wallet/index.tsx`.
- Confirmed wallet subscription UI is hidden while initial subscription state is loading or when there are no active subscriptions.
- Confirmed subscription, threshold auto recharge, and scheduled recharge tabs are built through `buildWalletPaymentSettingTabs`.
- Confirmed disabled auto-recharge modes receive `creationDisabled` and `WALLET_PAYMENT_SETTING_LOCK_MESSAGE`.
- Confirmed ordinary top-up remains the left-side primary wallet action and `AffiliateRewardsCard` remains below the grid.
- Confirmed new user-facing strings use `t('English source key')` and have locale entries.

## Concerns

- `bun run build:check` is still red because of pre-existing/unrelated TypeScript issues listed above.
- Manual browser inspection was not performed; verification was limited to static review and automated tests/build command output.
- `bun run i18n:sync` generated untracked untranslated report files under `web/default/src/i18n/locales/_reports/`; they were not staged.
