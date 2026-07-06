# Wallet Auto Recharge Threshold Balance Admin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins configure automatic recharge threshold values in the same balance unit shown in the wallet UI while preserving raw `threshold_quota` storage.

**Architecture:** Frontend admin helpers convert raw quota units to wallet-display balance units for editing, then convert back to raw quota units for API requests. Backend storage and user-facing automatic recharge behavior remain unchanged.

**Tech Stack:** React 19, TypeScript, Base UI/shadcn components, Bun test runner.

## Global Constraints

- DB/API field remains `threshold_quota`.
- Trigger comparison remains `current quota <= threshold_quota`.
- Admin threshold inputs use wallet-display balance numbers.
- User wallet and automatic recharge cards keep using `formatQuota(threshold_quota)`.
- Frontend user-facing text must use `t('English source key')` and locale JSON entries when new text is added.
- Do not touch backend behavior for this change.

---

## File Structure

- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
  - Convert individual preset form threshold value between raw quota and display balance.
  - Convert admin option-state threshold lists between raw quota and display balance.
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
  - Cover form state and normalize conversion.
- `web/default/src/features/wallet/lib/auto-recharge-options.ts`
  - Add a small helper for building save plans from display-balance threshold inputs.
- `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`
  - Cover display-balance threshold save plan conversion.

### Task 1: Admin Balance Conversion

**Files:**

- Modify: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- Modify: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
- Modify: `web/default/src/features/wallet/lib/auto-recharge-options.ts`
- Modify: `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`

**Interfaces:**

- Consumes: `quotaUnitsToDollars(units: number): number`
- Consumes: `parseQuotaFromDollars(amount: number): number`
- Produces: admin form state `threshold_quota` containing wallet-display balance text.
- Produces: API request `threshold_quota` containing raw quota units.

- [ ] **Step 1: Write failing tests**

Add tests for:

```ts
expect(
  toPresetFormState({
    id: 1,
    type: 'threshold',
    target_scope: 'user',
    name: 'Auto',
    amount: 20000,
    threshold_quota: 500000,
    enabled: true,
  }).threshold_quota
).toBe('1')
```

```ts
expect(
  normalizePresetForm({
    type: 'threshold',
    target_scope: 'organization',
    name: 'Low balance',
    description: '',
    amount: '25000',
    threshold_amount: '0',
    threshold_quota: '1',
    interval_unit: 'month',
    interval_value: '1',
    custom_seconds: '',
    charge_immediately: false,
    sort_order: '3',
    enabled: true,
  }).threshold_quota
).toBe(500000)
```

Add helper/save-plan test:

```ts
const plan = buildPresetSavePlanFromBalanceThresholds([], {
  scheduled: { targetScope: 'all', chargeImmediately: true, periods: [] },
  threshold: {
    targetScope: 'all',
    rechargeAmounts: [10000],
    thresholdQuotas: [1, 5],
  },
})

expect(plan.create.map((item) => item.threshold_quota)).toEqual([
  500000,
  2500000,
])
```

- [ ] **Step 2: Run RED**

```bash
cd web/default
bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: FAIL because admin state still uses raw quota values.

- [ ] **Step 3: Implement conversion**

- Import `parseQuotaFromDollars` and `quotaUnitsToDollars`.
- Export `toPresetFormState` for test coverage.
- Convert existing preset quota with `quotaUnitsToDollars()`.
- Convert form value back with `parseQuotaFromDollars()`.
- Add `buildPresetSavePlanFromBalanceThresholds()` that converts `threshold.thresholdQuotas` from display balances to raw quota before delegating to `buildPresetSavePlan()`.
- Use the new helper in admin save.
- Convert existing raw option state to display balance values before rendering admin option inputs.

- [ ] **Step 4: Run GREEN**

```bash
cd web/default
bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: PASS.

- [ ] **Step 5: Build verification**

```bash
cd web/default
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../../VERSION) bun run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/specs/2026-07-02-wallet-auto-recharge-threshold-balance-admin-design.md docs/superpowers/plans/2026-07-02-wallet-auto-recharge-threshold-balance-admin.md web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts web/default/src/features/wallet/lib/auto-recharge-options.ts web/default/src/features/wallet/lib/auto-recharge-options.test.ts
git commit -m "feat(wallet): edit auto recharge thresholds as balance"
```
