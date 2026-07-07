# Wallet Auto-Recharge Quota Threshold Final Fix Report

Date: 2026-07-02
Branch: `codex/wallet-auto-recharge`
Worktree: `/tmp/new-api-wallet-auto-recharge`

## Summary

Implemented the final-review deployment safety guard for legacy enabled threshold presets with `threshold_quota <= 0`.

The fix keeps admin listing/editing visibility intact while preventing unsafe legacy threshold presets from appearing in user-available preset choices or being used by ID to create new threshold auto-recharge policies. Frontend option grouping and save-plan generation now apply the same positive quota rule.

## Requirements Coverage

- Backend user preset lists exclude enabled threshold presets with `threshold_quota <= 0`: implemented in `ListWalletAutoRechargePresetsForTarget`.
- Backend target lookup rejects threshold presets with `threshold_quota <= 0`: implemented in `GetWalletAutoRechargePresetForTarget`.
- Frontend threshold grouping ignores threshold presets with `threshold_quota <= 0`: implemented in `groupThresholdPresetOptions`.
- Frontend save-plan generation skips threshold quota values `<= 0`: implemented in `buildPresetSavePlan`.
- Admin listing/editing remains able to see legacy rows: `ListWalletAutoRechargePresets(includeDisabled bool)` and admin controller listing were not changed.
- `threshold_amount` remains compatibility-only: no quota derivation was added.
- Cross-DB compatibility: used GORM `Where` with portable comparisons only.

## RED Evidence

Command:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run 'WalletAutoRechargePreset|ListWalletAutoRechargePresetsForTarget|GetWalletAutoRechargePresetForTarget' -count=1
```

Expected RED failures observed:

- `TestListWalletAutoRechargePresetsForTargetExcludesZeroQuotaThresholdPresets` failed because the returned list had 3 rows, including the legacy zero-quota threshold preset.
- `TestGetWalletAutoRechargePresetForTargetRejectsZeroQuotaThresholdPreset` failed because lookup of the legacy zero-quota threshold preset returned nil error.

Command:

```bash
cd web/default && bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected RED failures observed:

- `ignores zero quota threshold presets` failed because grouped threshold quotas were `[0, 500000]`.
- `save plan skips non-positive threshold quotas when creating presets` failed because the plan created 2 threshold preset requests instead of 1.

## Implementation

Files changed:

- `model/wallet_auto_recharge_preset.go`
  - Added a threshold preset validity helper.
  - Added a direct lookup guard for threshold presets with non-positive quota.
  - Added a user-target listing filter: `type <> threshold OR threshold_quota > 0`.
- `model/wallet_auto_recharge_preset_test.go`
  - Added regression coverage for target listing exclusion.
  - Added regression coverage for direct lookup rejection.
  - Updated an older target-filtering fixture to use positive threshold quota because direct threshold lookup now requires it.
- `web/default/src/features/wallet/lib/auto-recharge-options.ts`
  - Filtered threshold grouping to enabled threshold presets with positive quota.
  - Filtered save-plan threshold quotas to positive values before creating requests.
- `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`
  - Added regression coverage for grouping and save-plan generation.
- `.superpowers/sdd/quota-threshold-final-fix-report.md`
  - Added this report.

## GREEN Evidence

Command:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run 'WalletAutoRechargePreset|ListWalletAutoRechargePresetsForTarget|GetWalletAutoRechargePresetForTarget' -count=1
```

Result:

```text
ok  	github.com/QuantumNous/new-api/model	0.237s
```

Command:

```bash
cd web/default && bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Result:

```text
8 pass
0 fail
18 expect() calls
Ran 8 tests across 1 file.
```

## Self-Review

- Scope stayed within the requested ownership files.
- No service files, AutoRechargeCard, admin preset UI, or unrelated backend files were edited.
- Admin list remains unfiltered because `ListWalletAutoRechargePresets(true)` is unchanged.
- The backend guard does not use `threshold_amount`.
- The frontend guard does not derive quota from `threshold_amount`.
- The GORM condition uses only portable SQL comparisons and bound parameters.
- Existing unrelated untracked files were left untouched.

## Concerns

- No migration was added, per policy. Legacy invalid rows remain in storage and remain visible to admin listing/editing; they are blocked only from user selection and policy creation.
- The admin API still permits creating or updating a threshold preset with zero quota. This is intentional for this fix scope because the requested safety boundary is user availability and policy creation, while frontend save planning prevents new zero-quota creates through the managed option UI.
