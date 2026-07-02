# Wallet Topup Amount Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Toss wallet topup amount modes so users can choose either KRW payment amount or wallet quota amount, while UI previews and backend payment creation use the same unit model.

**Architecture:** Backend exposes `toss_unit_price` and centralizes Toss topup quote calculation by `amount_mode`. Frontend stores the selected topup amount mode, renders mode-specific presets and previews, and sends `amount_mode` to Toss amount/pay APIs for user and organization wallets.

**Tech Stack:** Go 1.22+, Gin, GORM, React 19, TypeScript, Base UI/shadcn-style local components, Bun test runner, i18next flat locale JSON.

## Global Constraints

- All JSON marshal/unmarshal in Go business code must use `common.*` wrappers when needed.
- Database code must stay compatible with SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6.
- Frontend package manager and runner is Bun from `web/default/`.
- Keep protected project identifiers `nеw-аρi` and `QuаntumΝоuѕ` untouched.
- Use existing wallet UI components and patterns; do not introduce a new component library.
- Do not change Toss confirm/webhook approval flow except for the values stored on the `TopUp` order.
- `amount_mode` absent must remain backward-compatible and behave as `krw`.
- Toss-only unit mode changes must not change Stripe, PayPal, Waffo, Waffo Pancake, Creem, or Epay behavior.

---

## File Structure

- Modify `model/toss_billing.go`: define amount-mode constants, quote result type, and authoritative Toss topup quote calculation for normal wallet topups.
- Modify `model/toss_billing_test.go`: unit-test KRW mode, quota mode, compatibility default, unit price exposure, and discount behavior.
- Modify `controller/topup.go`: include `toss_unit_price` in topup info.
- Modify `controller/topup_toss.go`: parse `amount_mode`, return structured amount quotes, store `TopUp.Amount` as KRW and `TopUp.Money` as actual credit amount.
- Modify `controller/topup_toss_test.go`: test controller helper behavior and amount-mode request compatibility.
- Modify `web/default/src/features/wallet/types.ts`: add amount-mode and Toss quote response types.
- Modify `web/default/src/features/wallet/api.ts`: accept `amount_mode` in Toss amount/pay requests and organization Toss API helpers.
- Modify `web/default/src/features/organizations/api.ts`: add missing organization Toss amount/pay API helpers.
- Create `web/default/src/features/wallet/lib/topup-amount-mode.ts`: UI-only preset lists, formatting helpers, and local preview calculations.
- Create `web/default/src/features/wallet/lib/topup-amount-mode.test.ts`: pure helper tests.
- Modify `web/default/src/features/wallet/hooks/use-payment.ts`: send `amount_mode` for Toss amount calculation and parse structured quote responses.
- Modify `web/default/src/features/wallet/hooks/use-toss-payment.ts`: send `amount_mode` for Toss pay.
- Modify `web/default/src/features/wallet/components/recharge-form-card.tsx`: render amount mode selector, mode-specific presets, custom preview, and Toss-specific labels.
- Create `web/default/src/features/wallet/components/recharge-form-card.test.tsx`: static render tests for mode-specific UI.
- Modify `web/default/src/features/wallet/components/dialogs/payment-confirm-dialog.tsx`: show both payment amount and credit amount for Toss amount modes.
- Modify `web/default/src/features/wallet/index.tsx`: own `topupAmountMode` state and pass it through user wallet payment flow.
- Modify `web/default/src/features/organizations/components/organization-wallet.tsx`: own `topupAmountMode`, use organization Toss APIs, and pass mode through organization wallet payment flow.
- Modify `web/default/src/i18n/locales/{en,zh,fr,ja,ru,vi}.json`: add new UI keys through i18n sync.

---

### Task 1: Backend Toss Topup Quote Model

**Files:**
- Modify: `model/toss_billing.go`
- Modify: `model/toss_billing_test.go`

**Interfaces:**
- Produces: `const TossTopUpAmountModeKRW = "krw"`, `const TossTopUpAmountModeQuota = "quota"`
- Produces: `type TossTopUpQuote struct { AmountMode string; InputAmount int64; ChargeKRW int64; CreditAmount float64; CreditQuota int; UnitPrice float64 }`
- Produces: `func QuoteTossTopUp(amount int64, amountMode string, group string) TossTopUpQuote`
- Produces: `func NormalizeTossTopUpAmountMode(amountMode string) string`
- Consumes: `setting.TossUnitPrice`, `common.QuotaPerUnit`, `common.GetTopupGroupRatio`, `operation_setting.GetPaymentSetting().AmountDiscount`
- Preserves: existing `TossTopUpChargedKRW` behavior for auto recharge and legacy tests.

- [ ] **Step 1: Write failing model tests**

Append these tests to `model/toss_billing_test.go`:

```go
func TestQuoteTossTopUpDefaultsToKRWMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(13000, "", "default")

	require.Equal(t, TossTopUpAmountModeKRW, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
	require.Equal(t, 1300.0, quote.UnitPrice)
}

func TestQuoteTossTopUpQuotaModeChargesUnitPriceTimesQuota(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, TossTopUpAmountModeQuota, quote.AmountMode)
	require.Equal(t, int64(13000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}

func TestQuoteTossTopUpKRWModeKeepsChargeFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10000: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10000, TossTopUpAmountModeKRW, "default")

	require.Equal(t, int64(10000), quote.ChargeKRW)
	require.InDelta(t, 20.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 10000000, quote.CreditQuota)
}

func TestQuoteTossTopUpQuotaModeKeepsCreditFixedWhenDiscountApplies(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1000
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.5}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := QuoteTossTopUp(10, TossTopUpAmountModeQuota, "default")

	require.Equal(t, int64(5000), quote.ChargeKRW)
	require.InDelta(t, 10.0, quote.CreditAmount, 0.000001)
	require.Equal(t, 5000000, quote.CreditQuota)
}
```

- [ ] **Step 2: Run model tests and verify RED**

Run:

```bash
go test ./model -run 'TestQuoteTossTopUp' -count=1
```

Expected: FAIL with undefined `QuoteTossTopUp`, `TossTopUpAmountModeKRW`, or `TossTopUpAmountModeQuota`.

- [ ] **Step 3: Implement quote model**

Add to `model/toss_billing.go` near the existing Toss topup helpers:

```go
const (
	TossTopUpAmountModeKRW   = "krw"
	TossTopUpAmountModeQuota = "quota"
)

type TossTopUpQuote struct {
	AmountMode   string  `json:"amount_mode"`
	InputAmount  int64   `json:"input_amount"`
	ChargeKRW    int64   `json:"charge_amount"`
	CreditAmount float64 `json:"credit_amount"`
	CreditQuota  int     `json:"credit_quota"`
	UnitPrice    float64 `json:"unit_price"`
}

func NormalizeTossTopUpAmountMode(amountMode string) string {
	switch amountMode {
	case TossTopUpAmountModeQuota:
		return TossTopUpAmountModeQuota
	default:
		return TossTopUpAmountModeKRW
	}
}

func tossTopUpUnitPrice() float64 {
	unit := setting.TossUnitPrice
	if unit <= 0 {
		return 1
	}
	return unit
}

func tossTopUpPriceFactor(amount int64, group string) float64 {
	ratio := common.GetTopupGroupRatio(group)
	if ratio == 0 {
		ratio = 1
	}
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}
	return ratio * discount
}

func QuoteTossTopUp(amount int64, amountMode string, group string) TossTopUpQuote {
	mode := NormalizeTossTopUpAmountMode(amountMode)
	unit := tossTopUpUnitPrice()
	factor := tossTopUpPriceFactor(amount, group)

	quote := TossTopUpQuote{
		AmountMode:  mode,
		InputAmount: amount,
		UnitPrice:   unit,
	}
	if amount <= 0 {
		return quote
	}

	switch mode {
	case TossTopUpAmountModeQuota:
		credit := decimal.NewFromInt(amount)
		charge := credit.
			Mul(decimal.NewFromFloat(unit)).
			Mul(decimal.NewFromFloat(factor)).
			Round(0).
			IntPart()
		quote.ChargeKRW = charge
		quote.CreditAmount = credit.InexactFloat64()
	default:
		charge := decimal.NewFromInt(amount)
		credit := charge.
			Div(decimal.NewFromFloat(unit)).
			Div(decimal.NewFromFloat(factor))
		quote.ChargeKRW = amount
		quote.CreditAmount = credit.InexactFloat64()
	}

	quote.CreditQuota = int(decimal.NewFromFloat(quote.CreditAmount).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		IntPart())
	return quote
}
```

Do not change `TossTopUpChargedKRW` in this task. Auto recharge and scheduled recharge still call that legacy helper, and this feature is limited to normal Toss wallet topup mode selection.

- [ ] **Step 4: Run model tests and verify GREEN**

Run:

```bash
go test ./model -run 'TestQuoteTossTopUp|TestToss' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add model/toss_billing.go model/toss_billing_test.go
git commit -m "feat(wallet): add toss topup amount quotes"
```

---

### Task 2: Backend Toss Controllers and Topup Info

**Files:**
- Modify: `controller/topup.go`
- Modify: `controller/topup_toss.go`
- Modify: `controller/topup_toss_test.go`

**Interfaces:**
- Consumes: `model.QuoteTossTopUp(amount, amountMode, group) model.TossTopUpQuote`
- Preserves: `getTossPayMoney(amountKRW, group)` legacy helper and its existing discount behavior for current tests.
- Produces: topup info field `toss_unit_price`
- Produces: `TossPayRequest.AmountMode string`
- Produces: `/api/user/toss/amount` and `/api/organization/toss/amount` data object with `charge_amount`, `credit_amount`, `credit_quota`, `unit_price`, `amount_mode`
- Produces: `/pay` response fields include `amount`, `charge_amount`, `credit_amount`, `credit_quota`, `unit_price`, `amount_mode`

- [ ] **Step 1: Write failing controller tests**

Append to `controller/topup_toss_test.go`:

```go
func TestGetTossTopUpQuoteDefaultsToKRWMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := getTossTopUpQuote(13000, "", "default")

	if quote.AmountMode != model.TossTopUpAmountModeKRW {
		t.Fatalf("AmountMode = %q want %q", quote.AmountMode, model.TossTopUpAmountModeKRW)
	}
	if quote.ChargeKRW != 13000 {
		t.Fatalf("ChargeKRW = %d want 13000", quote.ChargeKRW)
	}
	if quote.CreditQuota != 5000000 {
		t.Fatalf("CreditQuota = %d want 5000000", quote.CreditQuota)
	}
}

func TestGetTossTopUpQuoteQuotaMode(t *testing.T) {
	originalUnitPrice := setting.TossUnitPrice
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscount := operation_setting.GetPaymentSetting().AmountDiscount
	setting.TossUnitPrice = 1300
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{}
	defer func() {
		setting.TossUnitPrice = originalUnitPrice
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscount
	}()

	quote := getTossTopUpQuote(10, model.TossTopUpAmountModeQuota, "default")

	if quote.AmountMode != model.TossTopUpAmountModeQuota {
		t.Fatalf("AmountMode = %q want quota", quote.AmountMode)
	}
	if quote.ChargeKRW != 13000 {
		t.Fatalf("ChargeKRW = %d want 13000", quote.ChargeKRW)
	}
	if quote.CreditQuota != 5000000 {
		t.Fatalf("CreditQuota = %d want 5000000", quote.CreditQuota)
	}
}
```

Add imports if missing:

```go
import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)
```

- [ ] **Step 2: Run controller tests and verify RED**

Run:

```bash
go test ./controller -run 'TestGetTossTopUpQuote' -count=1
```

Expected: FAIL with undefined `getTossTopUpQuote` or missing request fields.

- [ ] **Step 3: Implement controller quote usage**

In `controller/topup_toss.go`, update `TossPayRequest`:

```go
type TossPayRequest struct {
	Amount        int64  `json:"amount"`
	AmountMode    string `json:"amount_mode"`
	PaymentMethod string `json:"payment_method"`
}
```

Add this helper while keeping the existing `getTossPayMoney` helper unchanged:

```go
func getTossTopUpQuote(amount int64, amountMode string, group string) model.TossTopUpQuote {
	return model.QuoteTossTopUp(amount, amountMode, group)
}
```

In `RequestTossAmount`, replace the charged-only response:

```go
quote := getTossTopUpQuote(req.Amount, req.AmountMode, user.Group)
if quote.ChargeKRW <= 0 || quote.CreditQuota <= 0 {
	common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
	return
}
c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote})
```

In `RequestTossPay`, replace `chargedKRW := getTossPayMoney(...)` with:

```go
quote := getTossTopUpQuote(req.Amount, req.AmountMode, user.Group)
chargedKRW := quote.ChargeKRW
if chargedKRW <= 0 || quote.CreditQuota <= 0 {
	common.ApiErrorI18n(c, i18n.MsgTopupAmountTooLow2)
	return
}
```

Set `TopUp.Money` to actual credit amount:

```go
Money: quote.CreditAmount,
```

Include quote fields in the response data:

```go
"amount":        chargedKRW,
"charge_amount": chargedKRW,
"credit_amount": quote.CreditAmount,
"credit_quota":  quote.CreditQuota,
"unit_price":    quote.UnitPrice,
"amount_mode":   quote.AmountMode,
```

In `controller/topup.go`, add `toss_unit_price` to `GetTopUpInfo` response map:

```go
"toss_unit_price": setting.TossUnitPrice,
```

- [ ] **Step 4: Run backend tests and verify GREEN**

Run:

```bash
go test ./model ./controller -run 'TestQuoteTossTopUp|TestGetTossTopUpQuote|TestGetTossPayMoney|TestValidateTossConfirmAmount' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add controller/topup.go controller/topup_toss.go controller/topup_toss_test.go
git commit -m "feat(wallet): expose toss amount mode quotes"
```

---

### Task 3: Frontend Toss Amount Mode Types, APIs, and Helpers

**Files:**
- Modify: `web/default/src/features/wallet/types.ts`
- Modify: `web/default/src/features/wallet/api.ts`
- Modify: `web/default/src/features/organizations/api.ts`
- Create: `web/default/src/features/wallet/lib/topup-amount-mode.ts`
- Create: `web/default/src/features/wallet/lib/topup-amount-mode.test.ts`

**Interfaces:**
- Produces: `export type TopupAmountMode = 'krw' | 'quota'`
- Produces: `export interface TossTopupQuote`
- Produces: `export const TOSS_KRW_PRESETS`
- Produces: `export const TOSS_QUOTA_PRESETS`
- Produces: `export function getTossPreview(amount, mode, unitPrice)`
- Produces: organization API helpers `calculateOrganizationTossAmount`, `requestOrganizationTossPayment`

- [ ] **Step 1: Write failing helper tests**

Create `web/default/src/features/wallet/lib/topup-amount-mode.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  TOSS_KRW_PRESETS,
  TOSS_QUOTA_PRESETS,
  formatWonAmount,
  getTossPreview,
  parseTossQuoteData,
} from './topup-amount-mode'

describe('topup amount mode helpers', () => {
  test('uses requested KRW and quota preset values', () => {
    assert.deepEqual(TOSS_KRW_PRESETS, [
      10000, 50000, 100000, 200000, 500000, 1000000,
    ])
    assert.deepEqual(TOSS_QUOTA_PRESETS, [10, 20, 50, 100, 200, 500])
  })

  test('previews KRW mode as payment fixed and credit estimated', () => {
    const preview = getTossPreview(13000, 'krw', 1300)
    assert.equal(preview.chargeAmount, 13000)
    assert.equal(preview.creditAmount, 10)
  })

  test('previews quota mode as credit fixed and payment estimated', () => {
    const preview = getTossPreview(10, 'quota', 1300)
    assert.equal(preview.chargeAmount, 13000)
    assert.equal(preview.creditAmount, 10)
  })

  test('formats won amounts with unit suffix', () => {
    assert.equal(formatWonAmount(10000), '10,000원')
  })

  test('parses structured and legacy Toss amount responses', () => {
    assert.deepEqual(parseTossQuoteData('13000'), {
      charge_amount: 13000,
    })
    assert.deepEqual(
      parseTossQuoteData({
        charge_amount: 13000,
        credit_amount: 10,
        credit_quota: 5000000,
        unit_price: 1300,
        amount_mode: 'quota',
      }),
      {
        charge_amount: 13000,
        credit_amount: 10,
        credit_quota: 5000000,
        unit_price: 1300,
        amount_mode: 'quota',
      }
    )
  })
})
```

- [ ] **Step 2: Run helper tests and verify RED**

Run:

```bash
cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts
```

Expected: FAIL because `topup-amount-mode.ts` does not exist.

- [ ] **Step 3: Implement types, APIs, and helpers**

In `web/default/src/features/wallet/types.ts`, add:

```ts
export type TopupAmountMode = 'krw' | 'quota'

export interface TossTopupQuote {
  amount_mode?: TopupAmountMode
  input_amount?: number
  charge_amount: number
  credit_amount?: number
  credit_quota?: number
  unit_price?: number
}
```

Update `TopupInfo`:

```ts
  /** Toss unit price in KRW for 1 wallet credit unit */
  toss_unit_price?: number
```

Update `TossPaymentResponse` data:

```ts
  charge_amount?: number
  credit_amount?: number
  credit_quota?: number
  unit_price?: number
  amount_mode?: TopupAmountMode
```

Update `PaymentRequest` and `AmountRequest`:

```ts
  /** Toss topup amount interpretation */
  amount_mode?: TopupAmountMode
```

Create `web/default/src/features/wallet/lib/topup-amount-mode.ts`:

```ts
import type { TopupAmountMode, TossTopupQuote } from '../types'

export const TOSS_KRW_PRESETS = [
  10000, 50000, 100000, 200000, 500000, 1000000,
]

export const TOSS_QUOTA_PRESETS = [10, 20, 50, 100, 200, 500]

export interface TossTopupPreview {
  chargeAmount: number
  creditAmount: number
}

export function normalizeTossUnitPrice(unitPrice: number | null | undefined) {
  return unitPrice && unitPrice > 0 ? unitPrice : 1
}

export function getTossPreview(
  amount: number,
  mode: TopupAmountMode,
  unitPrice: number | null | undefined
): TossTopupPreview {
  const unit = normalizeTossUnitPrice(unitPrice)
  if (mode === 'quota') {
    return {
      chargeAmount: Math.round(amount * unit),
      creditAmount: amount,
    }
  }
  return {
    chargeAmount: amount,
    creditAmount: amount / unit,
  }
}

export function formatWonAmount(amount: number): string {
  return `${new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 0,
  }).format(amount)}원`
}

export function parseTossQuoteData(data: unknown): Partial<TossTopupQuote> {
  if (typeof data === 'string' || typeof data === 'number') {
    const charge = Number(data)
    return Number.isFinite(charge) ? { charge_amount: charge } : {}
  }
  if (!data || typeof data !== 'object') return {}
  const record = data as Record<string, unknown>
  const mode = record.amount_mode === 'quota' ? 'quota' : record.amount_mode === 'krw' ? 'krw' : undefined
  return {
    amount_mode: mode,
    input_amount:
      typeof record.input_amount === 'number' ? record.input_amount : undefined,
    charge_amount:
      typeof record.charge_amount === 'number' ? record.charge_amount : 0,
    credit_amount:
      typeof record.credit_amount === 'number'
        ? record.credit_amount
        : undefined,
    credit_quota:
      typeof record.credit_quota === 'number' ? record.credit_quota : undefined,
    unit_price:
      typeof record.unit_price === 'number' ? record.unit_price : undefined,
  }
}
```

In `web/default/src/features/wallet/api.ts`, no URL changes are needed for user APIs because `PaymentRequest` and `AmountRequest` now include `amount_mode`.

In `web/default/src/features/organizations/api.ts`, import `TossPaymentResponse` and add:

```ts
export async function calculateOrganizationTossAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/organization/toss/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

export async function requestOrganizationTossPayment(
  request: PaymentRequest
): Promise<TossPaymentResponse> {
  const res = await api.post('/api/organization/toss/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}
```

- [ ] **Step 4: Run helper tests and typecheck target files**

Run:

```bash
cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts
```

Expected: PASS.

Run:

```bash
cd web/default && bun run typecheck
```

Expected: PASS or only unrelated pre-existing errors. Any errors from edited files must be fixed before proceeding.

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/wallet/types.ts web/default/src/features/wallet/api.ts web/default/src/features/organizations/api.ts web/default/src/features/wallet/lib/topup-amount-mode.ts web/default/src/features/wallet/lib/topup-amount-mode.test.ts
git commit -m "feat(wallet): add topup amount mode frontend helpers"
```

---

### Task 4: User Wallet Toss Mode Flow

**Files:**
- Modify: `web/default/src/features/wallet/hooks/use-payment.ts`
- Modify: `web/default/src/features/wallet/hooks/use-toss-payment.ts`
- Modify: `web/default/src/features/wallet/index.tsx`
- Modify: `web/default/src/features/wallet/components/recharge-form-card.tsx`
- Create: `web/default/src/features/wallet/components/recharge-form-card.test.tsx`
- Modify: `web/default/src/features/wallet/components/dialogs/payment-confirm-dialog.tsx`

**Interfaces:**
- Consumes: `TopupAmountMode`, `getTossPreview`, `formatWonAmount`, `TOSS_KRW_PRESETS`, `TOSS_QUOTA_PRESETS`, `parseTossQuoteData`
- Produces: `RechargeFormCard` props `amountMode`, `onAmountModeChange`, `tossUnitPrice`
- Produces: `usePayment.calculatePaymentAmount(topupAmount, paymentType, amountMode?)`
- Produces: `useTossPayment.processTossPayment(topupAmount, amountMode?)`

- [ ] **Step 1: Write failing RechargeFormCard tests**

Create `web/default/src/features/wallet/components/recharge-form-card.test.tsx`:

```tsx
import assert from 'node:assert/strict'
import { before, describe, test } from 'node:test'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import i18next from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { RechargeFormCard } from './recharge-form-card'
import type { TopupInfo } from '../types'

before(async () => {
  if (!i18next.isInitialized) {
    await i18next.use(initReactI18next).init({
      lng: 'en',
      fallbackLng: 'en',
      resources: { en: { translation: {} } },
      interpolation: { escapeValue: false },
    })
  }
})

function renderWithI18n(element: React.ReactElement) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18next}>{element}</I18nextProvider>
  )
}

const topupInfo: TopupInfo = {
  enable_online_topup: false,
  enable_stripe_topup: false,
  enable_paypal_topup: false,
  enable_toss_topup: true,
  pay_methods: [{ name: 'Toss', type: 'toss', min_topup: 1000 }],
  min_topup: 1,
  stripe_min_topup: 1,
  amount_options: [10, 20, 50, 100, 200, 500],
  discount: {},
  toss_min_topup: 1000,
  toss_unit_price: 1300,
}

describe('RechargeFormCard amount modes', () => {
  test('renders KRW presets and credit preview in KRW mode', () => {
    const html = renderWithI18n(
      <RechargeFormCard
        topupInfo={topupInfo}
        presetAmounts={[]}
        selectedPreset={10000}
        onSelectPreset={() => undefined}
        topupAmount={10000}
        onTopupAmountChange={() => undefined}
        paymentAmount={10000}
        calculating={false}
        onPaymentMethodSelect={() => undefined}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => undefined}
        onRedeem={() => undefined}
        redeeming={false}
        amountMode='krw'
        onAmountModeChange={() => undefined}
        tossUnitPrice={1300}
      />
    )

    assert.match(html, /원 기준/)
    assert.match(html, /할당량 기준/)
    assert.match(html, /10,000원/)
    assert.match(html, /50,000원/)
    assert.match(html, /충전/)
  })

  test('renders quota presets and KRW payment preview in quota mode', () => {
    const html = renderWithI18n(
      <RechargeFormCard
        topupInfo={topupInfo}
        presetAmounts={[]}
        selectedPreset={10}
        onSelectPreset={() => undefined}
        topupAmount={10}
        onTopupAmountChange={() => undefined}
        paymentAmount={13000}
        calculating={false}
        onPaymentMethodSelect={() => undefined}
        paymentLoading={null}
        redemptionCode=''
        onRedemptionCodeChange={() => undefined}
        onRedeem={() => undefined}
        redeeming={false}
        amountMode='quota'
        onAmountModeChange={() => undefined}
        tossUnitPrice={1300}
      />
    )

    assert.match(html, /10/)
    assert.match(html, /500/)
    assert.match(html, /결제/)
    assert.match(html, /13,000원/)
  })
})
```

- [ ] **Step 2: Run component test and verify RED**

Run:

```bash
cd web/default && bun test src/features/wallet/components/recharge-form-card.test.tsx
```

Expected: FAIL because `RechargeFormCard` does not accept `amountMode` props and does not render mode UI.

- [ ] **Step 3: Implement hooks and user wallet state**

In `use-payment.ts`, update signature:

```ts
async (topupAmount: number, paymentType: string, amountMode?: TopupAmountMode)
```

When Toss:

```ts
response = await calculateTossAmount({
  amount: topupAmount,
  amount_mode: amountMode,
})
```

Parse response:

```ts
const quote = isToss ? parseTossQuoteData(response.data) : null
const calculatedAmount = quote?.charge_amount ?? parseFloat(String(response.data))
```

In `use-toss-payment.ts`, update:

```ts
const processTossPayment = useCallback(
  async (topupAmount: number, amountMode?: TopupAmountMode) => {
    const amount = Math.floor(topupAmount)
    const response = await requestTossPayment({
      amount,
      amount_mode: amountMode,
      payment_method: 'toss',
    })
```

In `wallet/index.tsx`, add state:

```ts
const [topupAmountMode, setTopupAmountMode] = useState<TopupAmountMode>('krw')
```

Pass `topupAmountMode` to `calculatePaymentAmount` only for Toss:

```ts
calculatePaymentAmount(amount, getCurrentPaymentType(), topupAmountMode)
```

Pass to `processTossPayment`:

```ts
success = await processTossPayment(topupAmount, topupAmountMode)
```

Pass props to `RechargeFormCard` and `PaymentConfirmDialog`:

```tsx
amountMode={topupAmountMode}
onAmountModeChange={setTopupAmountMode}
tossUnitPrice={topupInfo?.toss_unit_price}
```

- [ ] **Step 4: Implement RechargeFormCard UI and confirm dialog**

In `RechargeFormCardProps`, add:

```ts
  amountMode?: TopupAmountMode
  onAmountModeChange?: (mode: TopupAmountMode) => void
  tossUnitPrice?: number
```

In `RechargeFormCard`, derive:

```ts
const isTossOnlyMode = !!topupInfo?.enable_toss_topup
const activeAmountMode = amountMode ?? 'quota'
const tossPresets =
  activeAmountMode === 'krw' ? TOSS_KRW_PRESETS : TOSS_QUOTA_PRESETS
```

Render a two-button segmented control before amount buttons when Toss is enabled:

```tsx
{topupInfo?.enable_toss_topup && onAmountModeChange ? (
  <div className='grid grid-cols-2 gap-1 rounded-lg border bg-muted/30 p-1'>
    <Button type='button' variant={activeAmountMode === 'krw' ? 'default' : 'ghost'} onClick={() => onAmountModeChange('krw')}>
      {t('원 기준')}
    </Button>
    <Button type='button' variant={activeAmountMode === 'quota' ? 'default' : 'ghost'} onClick={() => onAmountModeChange('quota')}>
      {t('할당량 기준')}
    </Button>
  </div>
) : null}
```

For Toss-enabled presets, render `tossPresets` instead of `presetAmounts`:

```tsx
const preview = getTossPreview(presetValue, activeAmountMode, tossUnitPrice)
const primary =
  activeAmountMode === 'krw' ? formatWonAmount(presetValue) : formatNumber(presetValue)
const secondary =
  activeAmountMode === 'krw'
    ? `${t('충전')} +${formatCurrencyFromUSD(preview.creditAmount)}`
    : `${t('결제')} ${formatWonAmount(preview.chargeAmount)}`
```

For custom preview:

```tsx
const customPreview = getTossPreview(topupAmount, activeAmountMode, tossUnitPrice)
const customPreviewText =
  activeAmountMode === 'krw'
    ? `${t('충전')} +${formatCurrencyFromUSD(customPreview.creditAmount)}`
    : formatWonAmount(paymentAmount || customPreview.chargeAmount)
```

In `PaymentConfirmDialog`, add optional props:

```ts
amountMode?: TopupAmountMode
tossUnitPrice?: number
```

For Toss, show:

```tsx
<span>{t('Payment amount')}</span>
<span>{formatWonAmount(paymentAmount)}</span>
<span>{t('Credit amount')}</span>
<span>{formatCurrencyFromUSD(getTossPreview(topupAmount, amountMode ?? 'quota', tossUnitPrice).creditAmount)}</span>
```

- [ ] **Step 5: Run user wallet frontend tests**

Run:

```bash
cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts src/features/wallet/components/recharge-form-card.test.tsx
```

Expected: PASS.

Run:

```bash
cd web/default && bun run typecheck
```

Expected: PASS or only unrelated pre-existing errors. Any errors from edited files must be fixed before proceeding.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/wallet/hooks/use-payment.ts web/default/src/features/wallet/hooks/use-toss-payment.ts web/default/src/features/wallet/index.tsx web/default/src/features/wallet/components/recharge-form-card.tsx web/default/src/features/wallet/components/recharge-form-card.test.tsx web/default/src/features/wallet/components/dialogs/payment-confirm-dialog.tsx
git commit -m "feat(wallet): add toss topup amount mode UI"
```

---

### Task 5: Organization Wallet Toss Mode Flow and i18n

**Files:**
- Modify: `web/default/src/features/organizations/components/organization-wallet.tsx`
- Modify: `web/default/src/i18n/locales/en.json`
- Modify: `web/default/src/i18n/locales/zh.json`
- Modify: `web/default/src/i18n/locales/fr.json`
- Modify: `web/default/src/i18n/locales/ja.json`
- Modify: `web/default/src/i18n/locales/ru.json`
- Modify: `web/default/src/i18n/locales/vi.json`

**Interfaces:**
- Consumes: `calculateOrganizationTossAmount`, `requestOrganizationTossPayment`
- Consumes: `TopupAmountMode`
- Produces: organization wallet uses `/api/organization/toss/amount` and `/api/organization/toss/pay` when selected payment method is Toss.

- [ ] **Step 1: Write failing organization Toss flow expectation**

Add this focused helper test only if `organization-wallet.tsx` already exports or can safely export a pure selector. Export this helper from `organization-wallet.tsx`:

```ts
export function getOrganizationAmountModeRequest(
  amount: number,
  paymentType: string,
  amountMode: TopupAmountMode
) {
  return isTossPayment(paymentType)
    ? { amount, amount_mode: amountMode }
    : { amount }
}
```

Create `web/default/src/features/organizations/components/organization-wallet.test.ts`:

```ts
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getOrganizationAmountModeRequest } from './organization-wallet'

describe('organization wallet topup amount mode request', () => {
  test('includes amount mode only for Toss requests', () => {
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'toss', 'quota'), {
      amount: 10,
      amount_mode: 'quota',
    })
    assert.deepEqual(getOrganizationAmountModeRequest(10, 'stripe', 'quota'), {
      amount: 10,
    })
  })
})
```

- [ ] **Step 2: Run organization test and verify RED**

Run:

```bash
cd web/default && bun test src/features/organizations/components/organization-wallet.test.ts
```

Expected: FAIL because helper is not exported.

- [ ] **Step 3: Implement organization wallet Toss flow**

In `organization-wallet.tsx`, import:

```ts
  isTossPayment,
```

from wallet lib, and import:

```ts
  calculateOrganizationTossAmount,
  requestOrganizationTossPayment,
```

from organization API.

Add state:

```ts
const [topupAmountMode, setTopupAmountMode] = useState<TopupAmountMode>('krw')
```

Use the helper in calculation:

```ts
const request = getOrganizationAmountModeRequest(
  amount,
  paymentType,
  topupAmountMode
)
const response = isStripePayment(paymentType)
  ? await calculateOrganizationStripeAmount(request)
  : isPayPalPayment(paymentType)
    ? await calculateOrganizationPayPalAmount(request)
    : isWaffoPancakePayment(paymentType)
      ? await calculateOrganizationWaffoPancakeAmount(request)
      : isTossPayment(paymentType)
        ? await calculateOrganizationTossAmount(request)
        : await calculateOrganizationAmount(request)
```

Use organization Toss pay in confirm:

```ts
const response = isStripePayment(paymentType)
  ? await requestOrganizationStripePayment({ amount, payment_method: 'stripe' })
  : isPayPalPayment(paymentType)
    ? await requestOrganizationPayPalPayment({ amount, payment_method: 'paypal' })
    : isWaffoPancakePayment(paymentType)
      ? await requestOrganizationWaffoPancakePayment({ amount })
      : isTossPayment(paymentType)
        ? await requestOrganizationTossPayment({
            amount,
            amount_mode: topupAmountMode,
            payment_method: 'toss',
          })
        : await requestOrganizationPayment({ amount, payment_method: paymentType })
```

Pass mode props to `RechargeFormCard` and `PaymentConfirmDialog`.

- [ ] **Step 4: Add i18n keys**

Add these keys to every locale file under `web/default/src/i18n/locales/*.json`:

English `en.json`:

```json
"원 기준": "KRW based",
"할당량 기준": "Quota based",
"충전": "Credit",
"결제": "Pay",
"Credit amount": "Credit amount",
"Payment amount": "Payment amount"
```

Chinese `zh.json`:

```json
"원 기준": "按韩元",
"할당량 기준": "按额度",
"충전": "充值",
"결제": "支付",
"Credit amount": "充值额度",
"Payment amount": "支付金额"
```

French `fr.json`:

```json
"원 기준": "Basé sur le KRW",
"할당량 기준": "Basé sur le quota",
"충전": "Crédit",
"결제": "Payer",
"Credit amount": "Montant crédité",
"Payment amount": "Montant du paiement"
```

Japanese `ja.json`:

```json
"원 기준": "ウォン基準",
"할당량 기준": "割当量基準",
"충전": "チャージ",
"결제": "支払い",
"Credit amount": "チャージ量",
"Payment amount": "支払い金額"
```

Russian `ru.json`:

```json
"원 기준": "По KRW",
"할당량 기준": "По квоте",
"충전": "Пополнение",
"결제": "Оплата",
"Credit amount": "Сумма пополнения",
"Payment amount": "Сумма платежа"
```

Vietnamese `vi.json`:

```json
"원 기준": "Theo KRW",
"할당량 기준": "Theo hạn mức",
"충전": "Nạp",
"결제": "Thanh toán",
"Credit amount": "Số dư được nạp",
"Payment amount": "Số tiền thanh toán"
```

Then run:

```bash
cd web/default && bun run i18n:sync
```

Expected: locale files sorted; generated `_reports` files may change and should not be committed unless already tracked.

- [ ] **Step 5: Run frontend verification**

Run:

```bash
cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts src/features/wallet/components/recharge-form-card.test.tsx src/features/organizations/components/organization-wallet.test.ts
```

Expected: PASS.

Run:

```bash
cd web/default && bun run typecheck
```

Expected: PASS or only unrelated pre-existing errors. Any errors from edited files must be fixed before proceeding.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/organizations/components/organization-wallet.tsx web/default/src/features/organizations/components/organization-wallet.test.ts web/default/src/i18n/locales/en.json web/default/src/i18n/locales/zh.json web/default/src/i18n/locales/fr.json web/default/src/i18n/locales/ja.json web/default/src/i18n/locales/ru.json web/default/src/i18n/locales/vi.json
git commit -m "feat(wallet): support topup amount modes in organization wallet"
```

---

### Task 6: Final Integration Verification

**Files:**
- No planned source edits unless verification exposes a defect.

**Interfaces:**
- Consumes: all previous tasks.
- Produces: final confidence that backend, frontend, i18n, and build integration are coherent.

- [ ] **Step 1: Run backend focused tests**

Run:

```bash
go test ./model ./controller -run 'TestQuoteTossTopUp|TestGetTossTopUpQuote|TestGetTossPayMoney|TestValidateTossConfirmAmount' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run frontend focused tests**

Run:

```bash
cd web/default && bun test src/features/wallet/lib/topup-amount-mode.test.ts src/features/wallet/components/recharge-form-card.test.tsx src/features/organizations/components/organization-wallet.test.ts
```

Expected: PASS.

- [ ] **Step 3: Run frontend typecheck/build**

Run:

```bash
cd web/default && bun run typecheck
```

Expected: PASS.

Run:

```bash
cd web/default && bun run build
```

Expected: PASS.

- [ ] **Step 4: Inspect git status**

Run:

```bash
git status --short
```

Expected: only intended tracked changes are present. Known untracked generated artifacts such as `new-api-bin` or locale `_reports/*.untranslated.json` remain uncommitted unless the user explicitly asks to commit them.

- [ ] **Step 5: Resolve verification failures through the owning task**

If backend quote tests fail, return to Task 1 or Task 2 and repeat that task's RED/GREEN/commit loop. If frontend helper or component tests fail, return to Task 3, Task 4, or Task 5 and repeat that task's RED/GREEN/commit loop. Do not create an empty verification commit.
