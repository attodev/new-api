## What I implemented

- Added `model/wallet_auto_recharge.go` with:
  - `WalletAutoRecharge` model and related constants
  - `CreateWalletAutoRechargeRequest`
  - `CreatePendingWalletAutoRecharge`
  - `ActivateWalletAutoRechargeFromToss`
  - `CancelWalletAutoRecharge`
  - `ListWalletAutoRecharges`
  - `GetDueScheduledWalletAutoRecharges`
  - `GetActiveThresholdWalletAutoRecharges`
  - `ProcessWalletAutoRecharge`
  - `ProcessWalletAutoRechargeWithConfiguredCharger`
  - supporting validation, scheduling, threshold, success, and failure helpers
- Added `model/wallet_auto_recharge_test.go` covering:
  - duplicate active policy rejection
  - successful scheduled recharge crediting user wallet and creating a Toss top-up row
  - threshold policy skipping charge during cooldown / at daily limit
- Added `&WalletAutoRecharge{}` to the main `DB.AutoMigrate(...)` list in `model/main.go`.

## RED/GREEN TDD evidence

### RED

Command:

```bash
env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
```

Relevant output:

```text
# github.com/QuantumNous/new-api/model [github.com/QuantumNous/new-api/model.test]
model/wallet_auto_recharge_test.go:26:92: undefined: WalletAutoRecharge
model/wallet_auto_recharge_test.go:50:12: undefined: CreatePendingWalletAutoRecharge
model/wallet_auto_recharge_test.go:50:44: undefined: CreateWalletAutoRechargeRequest
FAIL	github.com/QuantumNous/new-api/model [build failed]
```

### GREEN

Commands:

```bash
gofmt -w model/wallet_auto_recharge.go model/wallet_auto_recharge_test.go model/main.go
env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
```

Relevant output:

```text
ok  	github.com/QuantumNous/new-api/model	0.149s
```

## Files changed

- `model/wallet_auto_recharge.go`
- `model/wallet_auto_recharge_test.go`
- `model/main.go`

## Self-review findings

- Kept all DB access in GORM APIs and avoided raw SQL.
- Stayed within the owned file boundaries from the brief.
- Preserved existing Toss/top-up behavior by treating auto-recharge `Amount` like existing `TopUp.Money`, then deriving KRW with `setting.TossUnitPrice` and quota with `common.QuotaPerUnit`.
- Used an in-transaction billing-key reader during processing to avoid SQLite in-memory connection hangs in tests while preserving the same decrypt/status checks as `GetTossBillingKeyPlain`.

## Concerns

- The brief’s sample validation compared `Amount` directly to `setting.TossMinTopUp`, but the sample tests and existing top-up model clearly treat the amount as a wallet/top-up unit rather than raw KRW. I adjusted validation to compare the converted KRW amount instead.
- `ActivateWalletAutoRechargeFromToss` is implemented conservatively from the briefed interface, but there were no existing callers or task-local tests for that path in this task.

## Review fix follow-up

- Stopped `CancelWalletAutoRecharge` from revoking the shared `UserBillingKey`; cancelling the wallet policy now only marks the policy cancelled and clears its active uniqueness key.
- Added `ActiveKey *string` as a nullable unique key for pending/active policies, populated from `target_type:target_id:type`, and cleared when policies become cancelled or permanently failed.
- Replaced the mutable-value validation path with normalization that returns the updated request, so custom intervals consistently persist `IntervalValue=1` when omitted.
- Extended tests to cover:
  - cancellation preserving `UserBillingKey.Status`
  - duplicate rejection through the `ActiveKey` unique-key path
  - custom interval normalization persisting to storage

### Review fix verification

Command:

```bash
env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
```

Relevant output:

```text
ok  	github.com/QuantumNous/new-api/model	0.171s
```
