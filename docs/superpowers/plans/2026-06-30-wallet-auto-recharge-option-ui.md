# Wallet Auto Recharge Option UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build option-style admin and user UI for wallet scheduled recharge and threshold auto recharge while keeping the existing preset DB/API contract.

**Architecture:** Keep `WalletAutoRechargePreset` as the only persisted preset unit. Add frontend grouping/planning helpers that interpret existing preset rows as period/amount/threshold options, then update the wallet card and admin settings section to consume those helpers. No backend API or migration changes are required.

**Tech Stack:** React 19, TypeScript, Bun test runner, react-i18next, existing shadcn/Base UI components, existing wallet preset API.

## Global Constraints

- Existing DB/API/policy creation flow stays unchanged.
- Existing active policies are not modified automatically.
- User-facing preset amounts and thresholds are KRW values.
- Toss remains the only supported payment method for these flows.
- Frontend i18n keys must exist in `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`.
- Use Bun for frontend tests and build commands from `web/default/`.
- Preserve protected project identity strings from `AGENTS.md`.

---

## File Structure

- Create `web/default/src/features/wallet/lib/auto-recharge-options.ts`
  - Pure grouping and diff helpers for both user and admin UI.
  - No React imports.
- Create `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`
  - Bun tests for grouping, single-option auto selection, and admin save planning.
- Modify `web/default/src/features/wallet/components/auto-recharge-card.tsx`
  - Replace flat preset card list with period/amount/threshold button flow.
- Modify `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
  - Add server-rendered assertions for grouped UI and preset ID selection helpers.
- Modify `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
  - Replace individual preset dialog/table editing with option-set editing and save orchestration.
- Modify `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
  - Add admin option form normalization and save-plan tests.
- Modify locale files under `web/default/src/i18n/locales/`
  - Add new UI keys for option labels and action copy.

---

### Task 1: Pure Option Helpers

**Files:**
- Create: `web/default/src/features/wallet/lib/auto-recharge-options.ts`
- Test: `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`

**Interfaces:**
- Consumes: `WalletAutoRechargePreset`, `WalletAutoRechargePresetRequest`, `WalletAutoRechargeIntervalUnit`, `WalletAutoRechargeTargetScope` from `web/default/src/features/wallet/types.ts`.
- Produces:
  - `getScheduledPeriodKey(preset: WalletAutoRechargePreset): string`
  - `getScheduledPeriodLabelKey(period: ScheduledPeriodOption): string`
  - `groupScheduledPresetOptions(presets: WalletAutoRechargePreset[]): ScheduledPresetGroup[]`
  - `groupThresholdPresetOptions(presets: WalletAutoRechargePreset[]): ThresholdAmountGroup[]`
  - `buildAdminOptionState(presets: WalletAutoRechargePreset[]): AdminAutoRechargeOptionState`
  - `buildPresetSavePlan(current: WalletAutoRechargePreset[], desired: AdminAutoRechargeOptionState): PresetSavePlan`

- [ ] **Step 1: Write the failing helper tests**

Create `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`:

```ts
import { describe, expect, test } from 'bun:test'
import {
  buildAdminOptionState,
  buildPresetSavePlan,
  groupScheduledPresetOptions,
  groupThresholdPresetOptions,
} from './auto-recharge-options'
import type { WalletAutoRechargePreset } from '../types'

const preset = (
  patch: Partial<WalletAutoRechargePreset>
): WalletAutoRechargePreset => ({
  id: patch.id ?? 1,
  type: patch.type ?? 'scheduled',
  target_scope: patch.target_scope ?? 'all',
  name: patch.name ?? 'preset',
  description: patch.description ?? '',
  amount: patch.amount ?? 10000,
  threshold_amount: patch.threshold_amount ?? 0,
  interval_unit: patch.interval_unit ?? 'month',
  interval_value: patch.interval_value ?? 1,
  custom_seconds: patch.custom_seconds ?? 0,
  charge_immediately: patch.charge_immediately ?? true,
  sort_order: patch.sort_order ?? 0,
  enabled: patch.enabled ?? true,
})

describe('auto recharge option helpers', () => {
  test('groups scheduled presets by period and keeps first duplicate by sort order', () => {
    const groups = groupScheduledPresetOptions([
      preset({ id: 2, amount: 30000, interval_unit: 'month', interval_value: 1, sort_order: 2 }),
      preset({ id: 1, amount: 10000, interval_unit: 'month', interval_value: 1, sort_order: 1 }),
      preset({ id: 3, amount: 10000, interval_unit: 'month', interval_value: 1, sort_order: 3 }),
      preset({ id: 4, amount: 5000, interval_unit: 'day', interval_value: 7, sort_order: 4 }),
    ])

    expect(groups.map((group) => group.period.kind)).toEqual(['monthly', 'weekly'])
    expect(groups[0].amounts.map((amount) => [amount.amount, amount.preset.id])).toEqual([
      [10000, 1],
      [30000, 2],
    ])
  })

  test('groups threshold presets by recharge amount then threshold amount', () => {
    const groups = groupThresholdPresetOptions([
      preset({ id: 7, type: 'threshold', amount: 10000, threshold_amount: 5000, sort_order: 2 }),
      preset({ id: 6, type: 'threshold', amount: 10000, threshold_amount: 1000, sort_order: 1 }),
      preset({ id: 8, type: 'threshold', amount: 30000, threshold_amount: 5000, sort_order: 3 }),
    ])

    expect(groups.map((group) => group.amount)).toEqual([10000, 30000])
    expect(groups[0].thresholds.map((threshold) => [threshold.thresholdAmount, threshold.preset.id])).toEqual([
      [1000, 6],
      [5000, 7],
    ])
  })

  test('builds admin option state from existing presets', () => {
    const state = buildAdminOptionState([
      preset({ id: 1, type: 'scheduled', amount: 10000, interval_unit: 'day', interval_value: 1 }),
      preset({ id: 2, type: 'threshold', amount: 30000, threshold_amount: 5000 }),
    ])

    expect(state.scheduled.periods[0].period.kind).toBe('daily')
    expect(state.scheduled.periods[0].amounts).toEqual([10000])
    expect(state.threshold.rechargeAmounts).toEqual([30000])
    expect(state.threshold.thresholdAmounts).toEqual([5000])
  })

  test('plans create, update, and disable operations for option save', () => {
    const current = [
      preset({ id: 1, type: 'scheduled', amount: 10000, interval_unit: 'month', interval_value: 1, enabled: true }),
      preset({ id: 2, type: 'threshold', amount: 30000, threshold_amount: 5000, enabled: true }),
    ]
    const desired = buildAdminOptionState(current)
    desired.scheduled.periods[0].amounts = [10000, 50000]
    desired.threshold.thresholdAmounts = [1000]

    const plan = buildPresetSavePlan(current, desired)

    expect(plan.create.map((item) => [item.type, item.amount, item.threshold_amount])).toEqual([
      ['scheduled', 50000, 0],
      ['threshold', 30000, 1000],
    ])
    expect(plan.update.map((item) => item.id)).toEqual([1])
    expect(plan.disable.map((item) => item.id)).toEqual([2])
  })
})
```

- [ ] **Step 2: Run helper tests and verify RED**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: FAIL because `src/features/wallet/lib/auto-recharge-options.ts` does not exist.

- [ ] **Step 3: Implement the pure helpers**

Create `web/default/src/features/wallet/lib/auto-recharge-options.ts` with exported types and functions:

```ts
import type {
  WalletAutoRechargeIntervalUnit,
  WalletAutoRechargePreset,
  WalletAutoRechargePresetRequest,
  WalletAutoRechargeTargetScope,
} from '../types'

export type ScheduledPeriodKind = 'daily' | 'weekly' | 'monthly' | 'custom'

export interface ScheduledPeriodOption {
  key: string
  kind: ScheduledPeriodKind
  interval_unit: WalletAutoRechargeIntervalUnit
  interval_value: number
  custom_seconds: number
}

export interface ScheduledAmountOption {
  amount: number
  preset: WalletAutoRechargePreset
}

export interface ScheduledPresetGroup {
  period: ScheduledPeriodOption
  amounts: ScheduledAmountOption[]
}

export interface ThresholdOption {
  thresholdAmount: number
  preset: WalletAutoRechargePreset
}

export interface ThresholdAmountGroup {
  amount: number
  thresholds: ThresholdOption[]
}

export interface AdminScheduledPeriodOption {
  period: ScheduledPeriodOption
  amounts: number[]
}

export interface AdminScheduledOptionState {
  targetScope: WalletAutoRechargeTargetScope
  chargeImmediately: boolean
  periods: AdminScheduledPeriodOption[]
}

export interface AdminThresholdOptionState {
  targetScope: WalletAutoRechargeTargetScope
  rechargeAmounts: number[]
  thresholdAmounts: number[]
}

export interface AdminAutoRechargeOptionState {
  scheduled: AdminScheduledOptionState
  threshold: AdminThresholdOptionState
}

export interface PresetSavePlan {
  create: WalletAutoRechargePresetRequest[]
  update: Array<{ id: number; request: WalletAutoRechargePresetRequest }>
  disable: WalletAutoRechargePreset[]
}

const byPresetOrder = (
  left: WalletAutoRechargePreset,
  right: WalletAutoRechargePreset
) => {
  const sort = (left.sort_order ?? 0) - (right.sort_order ?? 0)
  return sort !== 0 ? sort : left.id - right.id
}

const uniqueNumbers = (values: number[]) =>
  [...new Set(values.filter((value) => Number.isFinite(value)))]
    .sort((left, right) => left - right)

export function getScheduledPeriodOption(
  preset: Pick<
    WalletAutoRechargePreset,
    'interval_unit' | 'interval_value' | 'custom_seconds'
  >
): ScheduledPeriodOption {
  const intervalUnit = preset.interval_unit ?? 'month'
  const intervalValue = preset.interval_value || 1
  const customSeconds = preset.custom_seconds || 0
  let kind: ScheduledPeriodKind = 'custom'
  if (intervalUnit === 'day' && intervalValue === 1) kind = 'daily'
  if (intervalUnit === 'day' && intervalValue === 7) kind = 'weekly'
  if (intervalUnit === 'month' && intervalValue === 1) kind = 'monthly'
  const key =
    kind === 'custom'
      ? `${intervalUnit}:${intervalValue}:${customSeconds}`
      : kind
  return { key, kind, interval_unit: intervalUnit, interval_value: intervalValue, custom_seconds: customSeconds }
}

export function getScheduledPeriodKey(preset: WalletAutoRechargePreset) {
  return getScheduledPeriodOption(preset).key
}

export function getScheduledPeriodLabelKey(period: ScheduledPeriodOption) {
  if (period.kind === 'daily') return 'Daily'
  if (period.kind === 'weekly') return 'Weekly'
  if (period.kind === 'monthly') return 'Monthly'
  return 'Custom period'
}

export function groupScheduledPresetOptions(
  presets: WalletAutoRechargePreset[]
): ScheduledPresetGroup[] {
  const map = new Map<string, ScheduledPresetGroup>()
  for (const preset of [...presets].filter((item) => item.enabled && item.type === 'scheduled').sort(byPresetOrder)) {
    const period = getScheduledPeriodOption(preset)
    const group = map.get(period.key) ?? { period, amounts: [] }
    if (!group.amounts.some((item) => item.amount === preset.amount)) {
      group.amounts.push({ amount: preset.amount, preset })
    }
    map.set(period.key, group)
  }
  return [...map.values()].map((group) => ({
    ...group,
    amounts: group.amounts.sort((left, right) => left.amount - right.amount),
  }))
}

export function groupThresholdPresetOptions(
  presets: WalletAutoRechargePreset[]
): ThresholdAmountGroup[] {
  const map = new Map<number, ThresholdAmountGroup>()
  for (const preset of [...presets].filter((item) => item.enabled && item.type === 'threshold').sort(byPresetOrder)) {
    const group = map.get(preset.amount) ?? { amount: preset.amount, thresholds: [] }
    const thresholdAmount = preset.threshold_amount ?? 0
    if (!group.thresholds.some((item) => item.thresholdAmount === thresholdAmount)) {
      group.thresholds.push({ thresholdAmount, preset })
    }
    map.set(preset.amount, group)
  }
  return [...map.values()]
    .sort((left, right) => left.amount - right.amount)
    .map((group) => ({
      ...group,
      thresholds: group.thresholds.sort((left, right) => left.thresholdAmount - right.thresholdAmount),
    }))
}
```

Then add `buildAdminOptionState` and `buildPresetSavePlan` in the same file, using the grouped helpers to build cross-product create/update/disable plans.

- [ ] **Step 4: Run helper tests and verify GREEN**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit helper task**

Run:

```bash
git add web/default/src/features/wallet/lib/auto-recharge-options.ts web/default/src/features/wallet/lib/auto-recharge-options.test.ts
git commit -m "feat(wallet): add auto recharge option helpers"
```

---

### Task 2: User Wallet Option Selection UI

**Files:**
- Modify: `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- Modify: `web/default/src/features/wallet/components/auto-recharge-card.test.ts`

**Interfaces:**
- Consumes Task 1 helpers:
  - `groupScheduledPresetOptions(presets)`
  - `groupThresholdPresetOptions(presets)`
  - `getScheduledPeriodLabelKey(period)`
- Produces:
  - Button-based period/amount/threshold selection UI.
  - Existing `buildPresetCreatePayload(presetId)` stays unchanged.

- [ ] **Step 1: Write failing user UI tests**

Append tests to `web/default/src/features/wallet/components/auto-recharge-card.test.ts`:

```ts
test('renders scheduled presets as period then amount buttons', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'scheduled',
      policies: [],
      presets: [
        { id: 1, type: 'scheduled', target_scope: 'all', name: 'Daily 10000', amount: 10000, interval_unit: 'day', interval_value: 1, enabled: true },
        { id: 2, type: 'scheduled', target_scope: 'all', name: 'Monthly 30000', amount: 30000, interval_unit: 'month', interval_value: 1, enabled: true },
      ],
      loading: false,
      processing: false,
      canManage: true,
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.match(html, /Choose recharge period/)
  assert.match(html, /Daily/)
  assert.match(html, /Monthly/)
})

test('hides scheduled period selector when only one period exists', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'scheduled',
      policies: [],
      presets: [
        { id: 1, type: 'scheduled', target_scope: 'all', name: 'Monthly 10000', amount: 10000, interval_unit: 'month', interval_value: 1, enabled: true },
        { id: 2, type: 'scheduled', target_scope: 'all', name: 'Monthly 30000', amount: 30000, interval_unit: 'month', interval_value: 1, enabled: true },
      ],
      loading: false,
      processing: false,
      canManage: true,
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.doesNotMatch(html, /Choose recharge period/)
  assert.match(html, /Choose recharge amount/)
  assert.match(html, /10000/)
  assert.match(html, /30000/)
})

test('renders threshold presets as recharge amount choices', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'threshold',
      policies: [],
      presets: [
        { id: 3, type: 'threshold', target_scope: 'all', name: 'Auto 10000', amount: 10000, threshold_amount: 1000, enabled: true },
        { id: 4, type: 'threshold', target_scope: 'all', name: 'Auto 30000', amount: 30000, threshold_amount: 5000, enabled: true },
      ],
      loading: false,
      processing: false,
      canManage: true,
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.match(html, /Choose recharge amount/)
  assert.match(html, /10000/)
  assert.match(html, /30000/)
})
```

- [ ] **Step 2: Run user UI tests and verify RED**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: FAIL because the current UI still renders `Available presets` cards.

- [ ] **Step 3: Implement scheduled option UI**

In `auto-recharge-card.tsx`:

- Import `useState` and Task 1 helpers.
- Create `selectedScheduledPeriodKey` state.
- Build `scheduledGroups` with `groupScheduledPresetOptions(availablePresets)`.
- Use the first group when only one period exists.
- Render period buttons only when there are two or more groups.
- Render amount buttons for the selected group.
- Each amount button calls `handleSelectPreset(option.preset.id)`.

- [ ] **Step 4: Implement threshold option UI**

In `auto-recharge-card.tsx`:

- Create `selectedThresholdAmount` state.
- Build `thresholdGroups` with `groupThresholdPresetOptions(availablePresets)`.
- Render recharge amount buttons.
- If selected group has one threshold, clicking the amount button immediately calls `handleSelectPreset(threshold.preset.id)`.
- If selected group has multiple thresholds, set `selectedThresholdAmount` and render threshold buttons.
- Each threshold button calls `handleSelectPreset(option.preset.id)`.

- [ ] **Step 5: Run user UI tests and verify GREEN**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit user UI task**

Run:

```bash
git add web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/wallet/components/auto-recharge-card.test.ts
git commit -m "feat(wallet): simplify auto recharge preset selection"
```

---

### Task 3: Admin Option Management UI

**Files:**
- Modify: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- Modify: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`

**Interfaces:**
- Consumes Task 1 helpers:
  - `buildAdminOptionState(presets)`
  - `buildPresetSavePlan(current, desired)`
  - `getScheduledPeriodLabelKey(period)`
- Uses existing API functions from `web/default/src/features/wallet/api.ts`:
  - `createAdminWalletAutoRechargePreset(request)`
  - `updateAdminWalletAutoRechargePreset(id, request)`
  - `deleteAdminWalletAutoRechargePreset(id)`

- [ ] **Step 1: Write failing admin helper tests**

Extend `wallet-auto-recharge-presets-section.test.ts`:

```ts
import {
  buildAdminOptionState,
  buildPresetSavePlan,
} from '@/features/wallet/lib/auto-recharge-options'

test('converts preset rows into admin option state', () => {
  const state = buildAdminOptionState([
    {
      id: 1,
      type: 'scheduled',
      target_scope: 'all',
      name: 'Monthly',
      amount: 10000,
      interval_unit: 'month',
      interval_value: 1,
      custom_seconds: 0,
      enabled: true,
    },
    {
      id: 2,
      type: 'threshold',
      target_scope: 'all',
      name: 'Auto',
      amount: 30000,
      threshold_amount: 5000,
      enabled: true,
    },
  ])

  expect(state.scheduled.periods[0].amounts).toEqual([10000])
  expect(state.threshold.rechargeAmounts).toEqual([30000])
  expect(state.threshold.thresholdAmounts).toEqual([5000])
})

test('builds save plan that disables removed admin combinations', () => {
  const current = [
    {
      id: 1,
      type: 'scheduled',
      target_scope: 'all',
      name: 'Monthly',
      amount: 10000,
      interval_unit: 'month',
      interval_value: 1,
      custom_seconds: 0,
      charge_immediately: true,
      enabled: true,
      sort_order: 0,
    },
  ]
  const desired = buildAdminOptionState(current)
  desired.scheduled.periods = []

  const plan = buildPresetSavePlan(current, desired)

  expect(plan.disable.map((preset) => preset.id)).toEqual([1])
})
```

- [ ] **Step 2: Run admin tests and verify RED**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: FAIL until Task 1 helpers are fully available or until section exports/imports are updated.

- [ ] **Step 3: Replace table/dialog management with option editor**

In `wallet-auto-recharge-presets-section.tsx`:

- Keep the existing query and mutation setup.
- Remove the individual edit dialog path from the rendered main UI.
- Create local `optionState` initialized from `buildAdminOptionState(presets)`.
- Render two cards:
  - `Scheduled recharge options`
  - `Auto recharge options`
- Scheduled card:
  - target scope select
  - charge immediately switch
  - period rows with period label, amount inputs, add/remove buttons
  - quick add buttons for daily, weekly, monthly
- Threshold card:
  - target scope select
  - recharge amount inputs
  - threshold amount inputs
  - add/remove buttons
- Add one save button that calls `buildPresetSavePlan(presets, optionState)`.

- [ ] **Step 4: Implement save orchestration**

In `wallet-auto-recharge-presets-section.tsx`, implement `handleSaveOptions`:

```ts
const plan = buildPresetSavePlan(presets, optionState)
for (const item of plan.create) await createAdminWalletAutoRechargePreset(item)
for (const item of plan.update) await updateAdminWalletAutoRechargePreset(item.id, item.request)
for (const item of plan.disable) await deleteAdminWalletAutoRechargePreset(item.id)
```

After all operations:

- show success toast
- invalidate `PRESET_QUERY_KEY`
- refetch preset data

On error:

- show error toast
- invalidate `PRESET_QUERY_KEY`

- [ ] **Step 5: Run admin tests and verify GREEN**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit admin UI task**

Run:

```bash
git add web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
git commit -m "feat(settings): manage auto recharge options"
```

---

### Task 4: i18n and Full Verification

**Files:**
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`
- Modify: `web/default/src/i18n/locales/_reports/_sync-report.json`

**Interfaces:**
- Consumes new `t('...')` keys from Tasks 2 and 3.
- Produces complete locale coverage and build-ready frontend.

- [ ] **Step 1: Run i18n sync and verify missing keys**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun run i18n:sync
```

Expected: The report lists any missing new keys.

- [ ] **Step 2: Add translations**

Add these keys to all locale files if used by the implementation:

```json
{
  "Choose recharge period": "Choose recharge period",
  "Choose recharge amount": "Choose recharge amount",
  "Choose threshold balance": "Choose threshold balance",
  "Daily": "Daily",
  "Weekly": "Weekly",
  "Monthly": "Monthly",
  "Custom period": "Custom period",
  "Scheduled recharge options": "Scheduled recharge options",
  "Auto recharge options": "Auto recharge options",
  "Recharge amounts": "Recharge amounts",
  "Threshold balances": "Threshold balances",
  "Add daily": "Add daily",
  "Add weekly": "Add weekly",
  "Add monthly": "Add monthly",
  "Add custom period": "Add custom period",
  "Save options": "Save options",
  "Options saved": "Options saved"
}
```

Use Korean-equivalent meaning only in user-facing prose if a Korean locale exists; this project locales are `en`, `zh`, `fr`, `ja`, `ru`, `vi`, so translate each key into those languages.

- [ ] **Step 3: Run focused tests**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/lib/auto-recharge-options.test.ts
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
BUN_TMPDIR=/tmp bun test src/i18n/languages.test.ts
```

Expected: all tests PASS.

- [ ] **Step 4: Run frontend build**

Run:

```bash
cd web/default
BUN_TMPDIR=/tmp bun run build
```

Expected: build completes successfully.

- [ ] **Step 5: Commit verification/i18n task**

Run:

```bash
git add web/default/src/i18n/locales web/default/src/features
git commit -m "chore(i18n): update auto recharge option translations"
```

---

### Task 5: Final Build and Service Replacement

**Files:**
- Build output only: `/tmp/new-api-wallet-auto-recharge/new-api-bin`
- Service binary: `/home/molla/new-api/new-api`

**Interfaces:**
- Consumes completed frontend dist and Go backend.
- Produces a service binary running on `new-api.service`.

- [ ] **Step 1: Build Go binary**

Run:

```bash
env GOCACHE=/tmp/go-build-cache go build -buildvcs=false -o /tmp/new-api-wallet-auto-recharge/new-api-bin .
```

Expected: exit code 0.

- [ ] **Step 2: Backup running binary**

Run:

```bash
cp /home/molla/new-api/new-api /home/molla/new-api/new-api.bak.wallet-option-ui-20260630
```

Expected: backup file exists.

- [ ] **Step 3: Stop, replace, and start service**

Run:

```bash
sudo systemctl stop new-api
cp /tmp/new-api-wallet-auto-recharge/new-api-bin /home/molla/new-api/new-api
sudo systemctl start new-api
```

Expected: commands exit 0.

- [ ] **Step 4: Verify service**

Run:

```bash
sudo systemctl status new-api --no-pager
curl -sS -o /tmp/new-api-status.json -w '%{http_code}' http://127.0.0.1:3000/api/status
```

Expected: systemd status is `active (running)` and curl prints `200`.

- [ ] **Step 5: Final status**

Run:

```bash
git status --porcelain=v1 --untracked-files=no
git log --oneline -5
```

Expected: no tracked changes remain after final commits.
