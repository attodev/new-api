# Wallet Auto Recharge Quota Threshold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Change automatic recharge threshold settings from payment-like amounts to internal quota units, and present the user flow as "when remaining quota is below X, charge Y KRW."

**Architecture:** Backend models/API accept and persist `threshold_quota` as the official threshold field while leaving `threshold_amount` only for compatibility. Frontend admin helpers generate preset combinations from `threshold_quota`, and the user auto-recharge card selects threshold quota first via dropdown, then recharge amount via buttons.

**Tech Stack:** Go 1.22+, Gin, GORM v2, React 19, TypeScript, Base UI/shadcn components, Tailwind CSS, Bun test runner.

## Global Constraints

- `threshold_quota` is the official DB/API/FE field for automatic recharge threshold criteria.
- `threshold_amount` must no longer drive automatic recharge threshold behavior.
- No migration of existing `threshold_amount` data.
- Existing automatic recharge presets/policies may be deleted and recreated by admins.
- Recharge `amount` remains KRW for Toss.
- Trigger comparison is `current quota <= threshold_quota`.
- Scheduled recharge UI and behavior are out of scope.
- Backend subscription/auto-recharge/scheduled-recharge mutual exclusion remains out of scope.
- Use `common.*` JSON helpers in Go; do not call `encoding/json` marshal/unmarshal directly.
- Database changes must remain SQLite, MySQL, and PostgreSQL compatible.
- Frontend user-facing text must use `t('English source key')` and be present in locale JSON files.

---

## File Structure

- `model/wallet_auto_recharge.go`
  - Add `ThresholdQuota` to create request and store it directly on policies.
  - Stop deriving threshold quota from `ThresholdAmount`.
  - Keep trigger comparison against `ThresholdQuota`.
- `model/wallet_auto_recharge_preset.go`
  - Add `ThresholdQuota` to preset model/request.
  - Persist `threshold_quota` on create/update.
  - Validate `threshold_quota >= 0` for threshold presets.
- `controller/wallet_auto_recharge.go`
  - Pass preset `ThresholdQuota` into pending policy creation.
- `model/wallet_auto_recharge_test.go`
  - Cover policy creation and threshold-trigger behavior using quota units.
- `controller/wallet_auto_recharge_preset_test.go`
  - Cover preset create/update response with `threshold_quota`.
- `web/default/src/features/wallet/types.ts`
  - Add `threshold_quota` to preset and preset request types.
- `web/default/src/features/wallet/lib/auto-recharge-options.ts`
  - Group threshold preset options by `threshold_quota` first, then recharge amount.
  - Generate admin save plans with `threshold_quota`.
- `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`
  - Cover threshold quota grouping and save plan payloads.
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`
  - Render threshold mode as sentence + dropdown first, recharge amount buttons second.
  - Display threshold quota using `formatQuota`.
- `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
  - Cover the new sentence/dropdown-first threshold UI.
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
  - Rename threshold balance admin field to threshold quota.
  - Send `threshold_quota` in preset requests.
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
  - Cover normalized threshold quota and admin option state.
- `web/default/src/i18n/locales/*.json`
  - Add/update strings for the new threshold quota UI.

---

### Task 1: Backend Threshold Quota Field

**Files:**

- Modify: `model/wallet_auto_recharge.go`
- Modify: `model/wallet_auto_recharge_preset.go`
- Modify: `controller/wallet_auto_recharge.go`
- Test: `model/wallet_auto_recharge_test.go`
- Test: `controller/wallet_auto_recharge_preset_test.go`

**Interfaces:**

- Produces: `WalletAutoRechargePreset.ThresholdQuota int`
- Produces: `WalletAutoRechargePresetRequest.ThresholdQuota int`
- Produces: `CreateWalletAutoRechargeRequest.ThresholdQuota int`
- Preserves: `threshold_amount` JSON fields for compatibility, but they are not the source of threshold trigger logic.

- [ ] **Step 1: Write failing model tests**

Append these tests to `model/wallet_auto_recharge_test.go`:

```go
func TestCreatePendingThresholdWalletAutoRechargeStoresThresholdQuotaDirectly(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)

	policy, err := CreatePendingWalletAutoRecharge(CreateWalletAutoRechargeRequest{
		Type:           WalletAutoRechargeTypeThreshold,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		OwnerUserId:    1,
		CustomerKey:    "customer-quota-threshold",
		AuthTradeNo:    "quota-threshold-auth",
		Amount:         10000,
		ThresholdAmount: 999999,
		ThresholdQuota:  250000,
	})

	require.NoError(t, err)
	require.Equal(t, 250000, policy.ThresholdQuota)
	require.Equal(t, 999999.0, policy.ThresholdAmount)
}

func TestWalletThresholdShouldChargeComparesStoredThresholdQuota(t *testing.T) {
	setupWalletAutoRechargeTestDB(t)
	require.NoError(t, DB.Create(&User{
		Id:       1,
		Username: "quota-threshold-user",
		Quota:    249999,
		AffCode:  "quota-threshold-user",
	}).Error)

	policy := &WalletAutoRecharge{
		Type:           WalletAutoRechargeTypeThreshold,
		TargetType:     TopUpTargetTypeUser,
		TargetId:       1,
		ThresholdAmount: 1,
		ThresholdQuota:  250000,
		Status:         WalletAutoRechargeStatusActive,
	}

	ok, err := walletThresholdShouldCharge(DB, policy, time.Now())

	require.NoError(t, err)
	require.True(t, ok)
}
```

Append this controller test to `controller/wallet_auto_recharge_preset_test.go` or extend the existing preset tests:

```go
func TestCreateWalletAutoRechargePresetAcceptsThresholdQuota(t *testing.T) {
	setupWalletAutoRechargePresetControllerTestDB(t)
	admin := model.User{Id: 1, Username: "admin", Role: common.RoleAdminUser}

	body := `{
		"type":"threshold",
		"target_scope":"all",
		"name":"Quota threshold",
		"description":"",
		"amount":10000,
		"threshold_amount":0,
		"threshold_quota":250000,
		"interval_unit":"month",
		"interval_value":1,
		"custom_seconds":0,
		"charge_immediately":false,
		"sort_order":1,
		"enabled":true
	}`
	res := performOrganizationRequest(
		CreateWalletAutoRechargePreset,
		admin,
		http.MethodPost,
		"/api/admin/wallet/auto-recharge/presets",
		body,
	)

	require.Equal(t, http.StatusOK, res.Code)
	var preset model.WalletAutoRechargePreset
	require.NoError(t, model.DB.Where("name = ?", "Quota threshold").First(&preset).Error)
	require.Equal(t, 250000, preset.ThresholdQuota)
}
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run 'TestCreatePendingThresholdWalletAutoRechargeStoresThresholdQuotaDirectly|TestWalletThresholdShouldChargeComparesStoredThresholdQuota' -count=1
GOCACHE=/tmp/go-build-cache go test ./controller -run TestCreateWalletAutoRechargePresetAcceptsThresholdQuota -count=1
```

Expected: FAIL because request/model/preset types do not yet persist `ThresholdQuota`.

- [ ] **Step 3: Implement backend field flow**

In `model/wallet_auto_recharge_preset.go`, update structs:

```go
type WalletAutoRechargePreset struct {
	// existing fields...
	ThresholdAmount float64 `json:"threshold_amount"`
	ThresholdQuota  int     `json:"threshold_quota"`
	// existing fields...
}

type WalletAutoRechargePresetRequest struct {
	// existing fields...
	ThresholdAmount float64 `json:"threshold_amount"`
	ThresholdQuota  int     `json:"threshold_quota"`
	// existing fields...
}
```

In `normalizeAndValidate`, replace threshold validation with:

```go
case WalletAutoRechargeTypeThreshold:
	if req.ThresholdQuota < 0 {
		return req, errors.New("wallet auto recharge preset threshold quota cannot be negative")
	}
```

In create/update maps include:

```go
ThresholdQuota: req.ThresholdQuota,
```

and

```go
"threshold_quota": req.ThresholdQuota,
```

In `model/wallet_auto_recharge.go`, update request and policy creation:

```go
type CreateWalletAutoRechargeRequest struct {
	// existing fields...
	ThresholdAmount float64
	ThresholdQuota  int
	// existing fields...
}
```

Validation:

```go
case WalletAutoRechargeTypeThreshold:
	if req.ThresholdQuota < 0 {
		return req, errors.New("wallet auto recharge threshold quota cannot be negative")
	}
```

Policy creation:

```go
ThresholdAmount: req.ThresholdAmount,
ThresholdQuota:  req.ThresholdQuota,
```

In `controller/wallet_auto_recharge.go`, pass:

```go
ThresholdQuota: preset.ThresholdQuota,
```

- [ ] **Step 4: Run backend tests to verify GREEN**

Run:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run 'TestCreatePendingThresholdWalletAutoRechargeStoresThresholdQuotaDirectly|TestWalletThresholdShouldChargeComparesStoredThresholdQuota' -count=1
GOCACHE=/tmp/go-build-cache go test ./controller -run 'TestCreateWalletAutoRechargePresetAcceptsThresholdQuota|TestWalletAutoRecharge' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit Task 1**

```bash
git add model/wallet_auto_recharge.go model/wallet_auto_recharge_preset.go controller/wallet_auto_recharge.go model/wallet_auto_recharge_test.go controller/wallet_auto_recharge_preset_test.go
git commit -m "feat(wallet): store auto recharge threshold quota"
```

---

### Task 2: Frontend Types and Option Helpers

**Files:**

- Modify: `web/default/src/features/wallet/types.ts`
- Modify: `web/default/src/features/wallet/lib/auto-recharge-options.ts`
- Test: `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`

**Interfaces:**

- Consumes: backend JSON field `threshold_quota`.
- Produces: `ThresholdQuotaGroup { thresholdQuota: number; amounts: ThresholdRechargeAmountOption[] }`
- Produces: admin option state `threshold.thresholdQuotas: number[]`
- Produces: save plan requests with `threshold_quota`.

- [ ] **Step 1: Write failing helper tests**

In `web/default/src/features/wallet/lib/auto-recharge-options.test.ts`, change threshold test fixtures to include `threshold_quota`, and add:

```ts
test('groups threshold presets by quota before recharge amount', () => {
  const groups = groupThresholdPresetOptions([
    preset({ id: 1, type: 'threshold', amount: 10000, threshold_quota: 500000 }),
    preset({ id: 2, type: 'threshold', amount: 30000, threshold_quota: 500000 }),
    preset({ id: 3, type: 'threshold', amount: 10000, threshold_quota: 1000000 }),
  ])

  expect(groups.map((group) => group.thresholdQuota)).toEqual([500000, 1000000])
  expect(groups[0].amounts.map((item) => [item.amount, item.preset.id])).toEqual([
    [10000, 1],
    [30000, 2],
  ])
})

test('builds admin option state with threshold quota values', () => {
  const state = buildAdminOptionState([
    preset({ id: 1, type: 'threshold', amount: 30000, threshold_quota: 500000 }),
  ])

  expect(state.threshold.rechargeAmounts).toEqual([30000])
  expect(state.threshold.thresholdQuotas).toEqual([500000])
})

test('save plan writes threshold_quota and clears threshold_amount', () => {
  const plan = buildPresetSavePlan([], {
    scheduled: { targetScope: 'all', chargeImmediately: true, periods: [] },
    threshold: {
      targetScope: 'all',
      rechargeAmounts: [10000],
      thresholdQuotas: [500000],
    },
  })

  expect(plan.create[0]).toMatchObject({
    type: 'threshold',
    amount: 10000,
    threshold_amount: 0,
    threshold_quota: 500000,
  })
})
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
cd web/default
bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: FAIL because `threshold_quota`, `thresholdQuotas`, and new grouping shape are missing.

- [ ] **Step 3: Implement helper type changes**

In `web/default/src/features/wallet/types.ts` add `threshold_quota?: number` to `WalletAutoRechargePreset` and `threshold_quota: number` to `WalletAutoRechargePresetRequest`.

In `auto-recharge-options.ts`, replace threshold interfaces with:

```ts
export interface ThresholdRechargeAmountOption {
  amount: number
  preset: WalletAutoRechargePreset
}

export interface ThresholdQuotaGroup {
  thresholdQuota: number
  amounts: ThresholdRechargeAmountOption[]
}
```

Update admin state:

```ts
export interface AdminThresholdOptionState {
  targetScope: WalletAutoRechargeTargetScope
  rechargeAmounts: number[]
  thresholdQuotas: number[]
}
```

Update grouping to use `preset.threshold_quota ?? 0` as the group key:

```ts
export function groupThresholdPresetOptions(
  presets: WalletAutoRechargePreset[]
): ThresholdQuotaGroup[] {
  const map = new Map<number, ThresholdQuotaGroup>()
  for (const preset of [...presets]
    .filter((item) => item.enabled && item.type === 'threshold')
    .sort(byPresetOrder)) {
    const thresholdQuota = preset.threshold_quota ?? 0
    const group = map.get(thresholdQuota) ?? { thresholdQuota, amounts: [] }
    if (!group.amounts.some((item) => item.amount === preset.amount)) {
      group.amounts.push({ amount: preset.amount, preset })
    }
    map.set(thresholdQuota, group)
  }
  return [...map.values()]
    .sort((left, right) => left.thresholdQuota - right.thresholdQuota)
    .map((group) => ({
      ...group,
      amounts: group.amounts.sort((left, right) => left.amount - right.amount),
    }))
}
```

Update `thresholdRequest`:

```ts
function thresholdRequest(
  amount: number,
  thresholdQuota: number,
  state: AdminThresholdOptionState,
  sortOrder: number
): WalletAutoRechargePresetRequest {
  return {
    type: 'threshold',
    target_scope: state.targetScope,
    name: `Auto ${amount} below quota ${thresholdQuota}`,
    description: '',
    amount,
    threshold_amount: 0,
    threshold_quota: thresholdQuota,
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
    charge_immediately: false,
    sort_order: sortOrder,
    enabled: true,
  }
}
```

Update combo key to include `threshold_quota`:

```ts
if (preset.type === 'threshold') {
  return `threshold:${preset.amount}:${preset.threshold_quota ?? 0}`
}
```

- [ ] **Step 4: Run helper tests to verify GREEN**

Run:

```bash
cd web/default
bun test src/features/wallet/lib/auto-recharge-options.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Task 2**

```bash
git add web/default/src/features/wallet/types.ts web/default/src/features/wallet/lib/auto-recharge-options.ts web/default/src/features/wallet/lib/auto-recharge-options.test.ts
git commit -m "feat(wallet): use quota threshold preset helpers"
```

---

### Task 3: Admin Preset UI Sends Threshold Quota

**Files:**

- Modify: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- Test: `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
- Modify: `web/default/src/i18n/locales/*.json`

**Interfaces:**

- Consumes: `AdminThresholdOptionState.thresholdQuotas`
- Produces: admin form requests with `threshold_quota`.

- [ ] **Step 1: Write failing admin tests**

In `wallet-auto-recharge-presets-section.test.ts`, update form expectations:

```ts
test('normalizes threshold preset with threshold quota', () => {
  expect(
    normalizePresetForm({
      type: 'threshold',
      target_scope: 'organization',
      name: 'Low quota',
      description: 'uses quota threshold',
      amount: '25000',
      threshold_amount: '7000',
      threshold_quota: '500000',
      interval_unit: 'custom',
      interval_value: '6',
      custom_seconds: '900',
      charge_immediately: true,
      sort_order: '3',
      enabled: false,
    })
  ).toEqual({
    type: 'threshold',
    target_scope: 'organization',
    name: 'Low quota',
    description: 'uses quota threshold',
    amount: 25000,
    threshold_amount: 0,
    threshold_quota: 500000,
    interval_unit: 'month',
    interval_value: 1,
    custom_seconds: 0,
    charge_immediately: false,
    sort_order: 3,
    enabled: false,
  })
})
```

Update the admin option state test to expect `thresholdQuotas`.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
cd web/default
bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: FAIL because form state and normalize helpers do not have `threshold_quota`.

- [ ] **Step 3: Implement admin form changes**

Add `threshold_quota` to the form state:

```ts
threshold_quota: string
```

Initialize it from `preset.threshold_quota`, defaulting to `''`.

In `normalizePresetForm`, return:

```ts
threshold_amount: 0,
threshold_quota: type === 'threshold' ? Number(form.threshold_quota || 0) : 0,
```

Rename option state UI labels:

```tsx
{t('Threshold quotas')}
```

Change state property updates from `thresholdAmounts` to `thresholdQuotas`.

Change placeholder to quota-like examples:

```tsx
placeholder='500000, 2500000, 5000000'
```

- [ ] **Step 4: Add i18n keys**

Run:

```bash
cd web/default
bun run i18n:sync
```

Ensure locale JSON files include:

- `Threshold quotas`
- `Threshold quota`

For `kr.json`, use:

- `Threshold quotas`: `기준 할당량`
- `Threshold quota`: `기준 할당량`

- [ ] **Step 5: Run admin tests to verify GREEN**

Run:

```bash
cd web/default
bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit Task 3**

```bash
git add web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts web/default/src/i18n/locales
git commit -m "feat(wallet): manage auto recharge threshold quotas"
```

---

### Task 4: User Auto Recharge Threshold UI

**Files:**

- Modify: `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- Test: `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
- Modify: `web/default/src/i18n/locales/*.json`

**Interfaces:**

- Consumes: `ThresholdQuotaGroup[]` from `groupThresholdPresetOptions`.
- Consumes: `formatQuota` from `@/lib/format`.
- Produces: threshold UI sentence with dropdown first, then KRW recharge buttons.

- [ ] **Step 1: Write failing threshold UI tests**

In `auto-recharge-card.test.ts`, update threshold tests:

```ts
test('renders threshold quota sentence before recharge amount choices', () => {
  const html = renderWithI18n(
    React.createElement(AutoRechargeCard, {
      mode: 'threshold',
      policies: [],
      presets: [
        {
          id: 3,
          type: 'threshold',
          target_scope: 'all',
          name: 'Auto 10000',
          amount: 10000,
          threshold_amount: 0,
          threshold_quota: 500000,
          enabled: true,
        },
        {
          id: 4,
          type: 'threshold',
          target_scope: 'all',
          name: 'Auto 30000',
          amount: 30000,
          threshold_amount: 0,
          threshold_quota: 500000,
          enabled: true,
        },
      ],
      loading: false,
      processing: false,
      canManage: true,
      onCreateScheduled: async () => false,
      onCreateThreshold: async () => false,
      onCancel: async () => false,
    })
  )

  assert.match(html, /When remaining quota is below/)
  assert.match(html, /charge the following amount/)
  assert.match(html, /Choose recharge amount/)
  assert.match(html, /10000원/)
  assert.match(html, /30000원/)
})
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: FAIL because UI still shows recharge amount first and raw numbers.

- [ ] **Step 3: Implement threshold UI**

Import select and quota formatter:

```ts
import { formatQuota } from '@/lib/format'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
```

Rename threshold state:

```ts
const [selectedThresholdQuota, setSelectedThresholdQuota] = useState<number | null>(null)
```

Resolve selected group:

```ts
const selectedThresholdGroup =
  thresholdGroups.find((group) => group.thresholdQuota === selectedThresholdQuota) ??
  thresholdGroups[0]
```

When rendering threshold mode:

```tsx
{thresholdGroups.length > 0 ? (
  <div className='space-y-3'>
    <div className='text-sm font-medium'>
      {t('When remaining quota is below')}
    </div>
    <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
      <span className='text-muted-foreground text-sm'>
        {t('When remaining quota is below')}
      </span>
      <Select
        value={String(selectedThresholdGroup?.thresholdQuota ?? '')}
        onValueChange={(value) => {
          setSelectedThresholdQuota(Number(value))
          setSelectedPresetId(null)
        }}
        disabled={creationButtonDisabled}
      >
        <SelectTrigger className='w-full sm:w-44'>
          <SelectValue placeholder={t('Threshold quota')} />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {thresholdGroups.map((group) => (
              <SelectItem
                key={group.thresholdQuota}
                value={String(group.thresholdQuota)}
              >
                {formatQuota(group.thresholdQuota)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <span className='text-muted-foreground text-sm'>
        {t('charge the following amount')}
      </span>
    </div>
  </div>
) : null}
```

Render recharge amount buttons from `selectedThresholdGroup.amounts`:

```tsx
{selectedThresholdGroup ? (
  <div className='space-y-2'>
    <div className='text-sm font-medium'>{t('Choose recharge amount')}</div>
    <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
      {selectedThresholdGroup.amounts.map((option) => (
        <Button
          key={`${selectedThresholdGroup.thresholdQuota}:${option.amount}`}
          type='button'
          variant={selectedPresetId === option.preset.id ? 'default' : 'outline'}
          disabled={creationButtonDisabled}
          onClick={() => setSelectedPresetId(option.preset.id)}
          className='h-10'
        >
          {option.amount}원
        </Button>
      ))}
    </div>
  </div>
) : null}
```

- [ ] **Step 4: Add i18n keys**

Run:

```bash
cd web/default
bun run i18n:sync
```

Ensure locale JSON files include:

- `When remaining quota is below`
- `charge the following amount`

For `kr.json`, use:

- `When remaining quota is below`: `남은 할당량이`
- `charge the following amount`: `보다 적어지면 다음 금액을 충전합니다.`

- [ ] **Step 5: Run component tests to verify GREEN**

Run:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit Task 4**

```bash
git add web/default/src/features/wallet/components/auto-recharge-card.tsx web/default/src/features/wallet/components/auto-recharge-card.test.ts web/default/src/i18n/locales
git commit -m "feat(wallet): select auto recharge quota before amount"
```

---

### Task 5: Integration Verification

**Files:**

- Test: backend model/controller wallet auto recharge tests
- Test: frontend wallet/admin auto recharge tests
- Build: default frontend and Go binary

**Interfaces:**

- Consumes all earlier tasks.
- Produces a verified branch ready for service replacement.

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
```

Expected: PASS.

- [ ] **Step 2: Run focused frontend tests**

Run:

```bash
cd web/default
bun test src/features/wallet src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Expected: PASS.

- [ ] **Step 3: Run i18n sync check**

Run:

```bash
cd web/default
bun run i18n:sync
```

Expected: `missingCount: 0` and `extrasCount: 0` in `_reports/_sync-report.json`.

- [ ] **Step 4: Run production frontend build**

Run:

```bash
cd web/default
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../../VERSION) bun run build
```

Expected: PASS.

- [ ] **Step 5: Run Go build**

Run:

```bash
GOCACHE=/tmp/go-build-cache go build -buildvcs=false -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api-bin
```

Expected: PASS.

- [ ] **Step 6: Commit final verification notes only if code changed**

If verification found and fixed issues:

```bash
git add model/wallet_auto_recharge.go model/wallet_auto_recharge_preset.go controller/wallet_auto_recharge.go web/default/src/features/wallet web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts web/default/src/i18n/locales
git commit -m "fix(wallet): polish quota threshold auto recharge"
```

If no code changed, do not create a commit.

---

## Final Verification

Before claiming completion, run:

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
cd web/default && bun test src/features/wallet src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
cd web/default && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../../VERSION) bun run build
GOCACHE=/tmp/go-build-cache go build -buildvcs=false -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api-bin
```

Expected:

- Backend focused tests pass.
- Frontend focused tests pass.
- Default frontend production build passes.
- Go binary build passes.

`bun run build:check` may remain blocked by unrelated existing TypeScript errors in organization, system-settings, usage-logs, and `bun:test` type resolution. If run, report exact failures instead of claiming it passes.
