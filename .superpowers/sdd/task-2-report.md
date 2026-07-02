# Task 2 Report: Backend Toss Controllers and Topup Info

## Status
- Implemented Toss controller quote handling and top-up info exposure in the Task 2-owned controller files only.
- Preserved the legacy `getTossPayMoney` helper and existing Toss confirm/webhook flow.
- Kept default `amount_mode` behavior backward-compatible by routing through the model quote normalizer.

## TDD Evidence

### RED
Command:

```bash
go test ./controller -run 'TestGetTossTopUpQuote' -count=1
```

Observed failure:

```text
# github.com/QuantumNous/new-api/controller [github.com/QuantumNous/new-api/controller.test]
controller/topup_toss_test.go:110:11: undefined: getTossTopUpQuote
controller/topup_toss_test.go:136:11: undefined: getTossTopUpQuote
FAIL	github.com/QuantumNous/new-api/controller [build failed]
FAIL
```

Note: the first sandboxed `go test` attempt failed before compilation because the Go build cache path was read-only; rerunning with escalation was required to capture the real RED failure above.

### GREEN
Command:

```bash
go test ./model ./controller -run 'TestQuoteTossTopUp|TestGetTossTopUpQuote|TestGetTossPayMoney|TestValidateTossConfirmAmount' -count=1
```

Observed success:

```text
ok  	github.com/QuantumNous/new-api/model	0.095s
ok  	github.com/QuantumNous/new-api/controller	0.069s
```

## Files Changed
- `controller/topup.go`
- `controller/topup_toss.go`
- `controller/topup_toss_test.go`

## Implementation Notes
- Added `AmountMode string` to `TossPayRequest`.
- Added `getTossTopUpQuote(amount, amountMode, group)` as the controller wrapper around `model.QuoteTossTopUp(...)`.
- Changed `RequestTossAmount` to return the structured quote object and reject zero/invalid charge or quota results with `MsgTopupAmountTooLow2`.
- Changed `RequestTossPay` to:
  - derive all Toss pay amounts from the quote,
  - store `TopUp.Money` as `quote.CreditAmount`,
  - include `amount`, `charge_amount`, `credit_amount`, `credit_quota`, `unit_price`, and `amount_mode` in the pay response.
- Added `toss_unit_price` to `GetTopUpInfo`.
- Preserved `getTossPayMoney` unchanged for legacy callers and existing tests.

## Self-Review
- Confirmed scope stayed inside the three Task 2-owned files.
- Verified Toss-only amount mode changes do not touch Stripe, PayPal, Waffo, Waffo Pancake, Creem, or Epay code paths.
- Confirmed JSON handling changes were not needed beyond existing controller binding and response patterns.
- Confirmed absent `amount_mode` still resolves to KRW mode via `model.NormalizeTossTopUpAmountMode`.

## Concerns
- `go test` requires escalated execution in this environment because the Go build cache lives outside the writable workspace.

---

## Review Follow-up

### What I fixed
- Moved Toss minimum-topup validation in `RequestTossAmount` and `RequestTossPay` from raw `req.Amount` to the computed quote charge (`quote.ChargeKRW` / `chargedKRW`), so quota-mode requests are validated in KRW after conversion instead of being rejected for sending credit units.
- Kept KRW mode behavior backward-compatible because KRW quotes preserve `ChargeKRW == req.Amount`.
- Added controller regression coverage for:
  - `/api/user/toss/amount` quota mode returning the structured quote payload when converted KRW meets the minimum,
  - `/api/user/toss/pay` quota mode returning the structured quote fields,
  - `/api/user/toss/pay` persisting `TopUp.Money` as `quote.CreditAmount`,
  - `/api/organization/toss/amount` inheriting the same quota-mode minimum validation behavior.

### Tests run and output
- `GOCACHE=/tmp/new-api-go-build-cache go test ./controller -run 'TestGetTossTopUpQuote|TestRequestToss|TestOrganizationToss' -count=1`

```text
ok  	github.com/QuantumNous/new-api/controller	0.151s
```

- `GOCACHE=/tmp/new-api-go-build-cache go test ./model ./controller -run 'TestQuoteTossTopUp|TestGetTossTopUpQuote|TestGetTossPayMoney|TestValidateTossConfirmAmount|TestRequestToss|TestOrganizationToss' -count=1`

```text
ok  	github.com/QuantumNous/new-api/model	0.102s
ok  	github.com/QuantumNous/new-api/controller	0.186s
```

### Files changed
- `controller/topup_toss.go`
- `controller/topup_toss_test.go`

### Self-review
- The fix stays inside the Task 2-owned files and does not touch unrelated payment paths.
- The new tests exercise the controller endpoints directly, including the organization wrapper and persisted `TopUp` row.
- `controller/topup.go` already contained `toss_unit_price` in this worktree, so no additional edit was needed there for this follow-up.
