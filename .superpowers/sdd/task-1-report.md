## Task 1: Payment Setting Helper

## What I implemented

- Added `web/default/src/features/wallet/lib/payment-settings.test.ts` with the required helper tests from the brief.
- Added `web/default/src/features/wallet/lib/payment-settings.ts` with:
  - `WalletPaymentSettingKind`
  - `WalletPaymentSettingTab`
  - `WalletPaymentSettingInput`
  - `WALLET_PAYMENT_SETTING_LOCK_MESSAGE`
  - `getConfiguredAutoRechargeTypes`
  - `buildWalletPaymentSettingTabs`
  - `getInitialWalletPaymentSetting`

## RED/GREEN TDD evidence

### RED

Command:

```bash
cd web/default
bun test src/features/wallet/lib/payment-settings.test.ts
```

Relevant output:

```text
error: Cannot find module './payment-settings' from '/tmp/new-api-wallet-auto-recharge/web/default/src/features/wallet/lib/payment-settings.test.ts'

0 pass
1 fail
1 error
Ran 1 test across 1 file. [137.00ms]
```

### GREEN

Command:

```bash
cd web/default
bun test src/features/wallet/lib/payment-settings.test.ts
```

Relevant output:

```text
(pass) wallet payment setting helpers > hides subscription when there is no active subscription [3.78ms]
(pass) wallet payment setting helpers > shows subscription and disables unconfigured recharge choices when subscription is active [0.30ms]
(pass) wallet payment setting helpers > keeps configured auto recharge enabled and disables scheduled recharge [0.13ms]
(pass) wallet payment setting helpers > keeps every already configured exception tab enabled so users can cancel [0.11ms]
(pass) wallet payment setting helpers > ignores cancelled and failed auto recharge policies [0.55ms]

5 pass
0 fail
Ran 5 tests across 1 file. [127.00ms]
```

## Files changed

- `web/default/src/features/wallet/lib/payment-settings.ts`
- `web/default/src/features/wallet/lib/payment-settings.test.ts`
- `.superpowers/sdd/task-1-report.md`

## Constraints check

- Worked in `/tmp/new-api-wallet-auto-recharge` on branch `codex/wallet-auto-recharge`.
- Did not change backend APIs or database models.
- Did not modify protected project identifiers.
- Used `bun` from `web/default`.
- Added no UI text or i18n keys.
- Left unrelated untracked `new-api-bin` untouched.

## Concerns

- None.
