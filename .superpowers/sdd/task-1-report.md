# Task 1 Report: Backend Toss Topup Quote Model

## Scope
- Files changed: `model/toss_billing.go`, `model/toss_billing_test.go`
- Commit: `2a0be799`

## TDD Execution

### Step 1: Added tests to `model/toss_billing_test.go`
Added:
- `TestQuoteTossTopUpDefaultsToKRWMode`
- `TestQuoteTossTopUpQuotaModeChargesUnitPriceTimesQuota`
- `TestQuoteTossTopUpKRWModeKeepsChargeFixedWhenDiscountApplies`
- `TestQuoteTossTopUpQuotaModeKeepsCreditFixedWhenDiscountApplies`

### Step 2: RED run (expected undefined symbols)
Command:
```bash
GOCACHE=/tmp/go-cache GOTOOLCHAIN=local GOROOT=/home/molla/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.1.linux-amd64 /home/molla/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.1.linux-amd64/bin/go test ./model -run 'TestQuoteTossTopUp' -count=1
```

Observed output (key lines):
```text
model/toss_billing_test.go:25:11: undefined: QuoteTossTopUp
model/toss_billing_test.go:27:19: undefined: TossTopUpAmountModeKRW
model/toss_billing_test.go:47:11: undefined: QuoteTossTopUp
...
FAIL	github.com/QuantumNous/new-api/model [build failed]
```

### Step 3: Implementation
- Added new amount mode constants:
  - `TossTopUpAmountModeKRW = "krw"`
  - `TossTopUpAmountModeQuota = "quota"`
- Added `TossTopUpQuote` struct.
- Added `NormalizeTossTopUpAmountMode`.
- Added `QuoteTossTopUp`, `tossTopUpUnitPrice`, `tossTopUpPriceFactor`.
- Left `TossTopUpChargedKRW` unchanged to preserve legacy auto/scheduled recharge behavior.

### Step 4: GREEN run
Command:
```bash
GOCACHE=/tmp/go-cache GOTOOLCHAIN=local GOROOT=/home/molla/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.1.linux-amd64 /home/molla/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.1.linux-amd64/bin/go test ./model -run 'TestQuoteTossTopUp|TestToss' -count=1
```

Observed output:
```text
ok  	github.com/QuantumNous/new-api/model	0.098s
```

## Self-review
- `amount_mode` defaults to KRW via `NormalizeTossTopUpAmountMode("") -> krw`.
- KRW mode keeps `ChargeKRW` equal to input while credit is de-discounted (`Div` by factor).
- Quota mode computes `ChargeKRW = input * unit * factor` and preserves credit as input.
- Discount/ratio behavior matches existing `TossTopUpChargedKRW` semantics via shared factor logic.
- No changes made to `TossTopUpChargedKRW`, recharge scheduler paths, or Toss webhook/confirm flow.
- `common` wrappers and cross-DB behavior are preserved (quoting logic unchanged, no raw SQL introduced).

## Reviewer Follow-up Fixes

### What I fixed
- Kept quote quota math in exact decimal space:
  - `CreditQuota` is now computed directly from `decimal` credit amounts for both KRW and quota modes.
  - `CreditAmount` is still exposed as `float64` only at the output boundary.
- Removed unit-price fallback in `QuoteTossTopUp`:
  - `tossTopUpUnitPrice()` no longer coerces non-positive config to `1`.
  - `QuoteTossTopUp` now returns a zero/unpayable quote when `TossUnitPrice <= 0` (`UnitPrice=0`, `ChargeKRW=0`, `CreditAmount=0`, `CreditQuota=0`).
- Added direct regression tests:
  - `TestQuoteTossTopUpInvalidModeDefaultsToKRWMode`
  - `TestQuoteTossTopUpNonPositiveUnitPriceReturnsZeroQuote`

### Tests run and output
- `go test ./model -run 'TestQuoteTossTopUp|TestToss' -count=1`
  - output: `ok  	github.com/QuantumNous/new-api/model	0.097s`

### Files changed
- `model/toss_billing.go`
- `model/toss_billing_test.go`
- `.superpowers/sdd/task-1-report.md` (append-only follow-up notes)

### Self-review
- `NormalizeTossTopUpAmountMode` behavior remains the same for explicit/implicit modes.
- Existing `TossTopUpChargedKRW` and `TossUSDEquivalent` were preserved as requested.
- The non-positive unit-price guard keeps quote generation safe for invalid operator configuration without affecting legacy charge paths.

## Re-review Fix (Task 1 Addendum)

### What test I added
- Added `TestQuoteTossTopUpKRWModeFloorsQuotaUsingDecimal` in `model/toss_billing_test.go`.
- Test uses KRW mode with `amount=1`, `TossUnitPrice=3`, `common.QuotaPerUnit=3` to create a non-terminating credit value `1/3`.
- It asserts `CreditQuota == 1` to lock in exact floored quota behavior that a float-based round-trip could undercount (historically to `0`).

### Tests run and output
- `go test ./model -run 'TestQuoteTossTopUp|TestToss' -count=1`
  - output: `ok  	github.com/QuantumNous/new-api/model	0.091s`

### Files changed
- `model/toss_billing_test.go`
- `model/toss_billing.go` (KRW-mode `CreditQuota` calculation now computes quota via `amount * quota / unit / factor` path to avoid precision-loss flooring)
- `.superpowers/sdd/task-1-report.md`

### Self-review
- The newly added KRW test specifically guards a previous float-based precision regression while remaining deterministic and fast.
- `QuoteTossTopUp` remains behavior-compatible for KRW/ quota modes and discount/group factor handling, but now preserves a full quota unit for repeating-decimal credits.
