# Task 3 Report: API와 Toss Billing Auth 콜백

## Summary

Implemented wallet auto recharge controller APIs and Toss billing auth callbacks for user and organization wallet targets, plus router wiring for the new endpoints.

## TDD Evidence

### RED

Command:

```bash
env GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
```

Result:

```text
# github.com/QuantumNous/new-api/controller [github.com/QuantumNous/new-api/controller.test]
controller/wallet_auto_recharge_test.go:74:3: undefined: RequestOrganizationWalletScheduledRecharge
controller/wallet_auto_recharge_test.go:108:3: undefined: RequestOrganizationWalletScheduledRecharge
controller/wallet_auto_recharge_test.go:182:3: undefined: CancelOrganizationWalletAutoRecharge
controller/wallet_auto_recharge_test.go:218:3: undefined: WalletAutoRechargeTossFail
FAIL	github.com/QuantumNous/new-api/controller [build failed]
FAIL
```

### GREEN

Commands:

```bash
gofmt -w controller/wallet_auto_recharge.go controller/wallet_auto_recharge_test.go router/api-router.go
env GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
env GOCACHE=/tmp/go-build-cache go test ./router -count=1
```

Results:

```text
ok  	github.com/QuantumNous/new-api/controller	0.194s
?   	github.com/QuantumNous/new-api/router	[no test files]
```

## Files Changed

- `controller/wallet_auto_recharge.go`
- `controller/wallet_auto_recharge_test.go`
- `router/api-router.go`

## Self-Review

- Added shared request handling for scheduled and threshold wallet auto recharge creation.
- Enforced organization owner-only create/cancel behavior with the required `"organization owner permission required"` error.
- Bound Toss callback `customerKey` to the stored pending policy before billing key persistence.
- Used `model.StoreTossBillingKey` followed by `model.ActivateWalletAutoRechargeFromToss` in confirm flow as requested.
- Kept subscription behavior untouched and did not edit frontend/service/docs.
- Avoided model changes; controller performs the pending-policy lookup directly.

## Concerns

- None.

## Review Fix Follow-Up

### Fix Details

- Tightened organization wallet ownership checks to require both `actor.OrganizationRole == model.OrganizationRoleOwner` and `org.OwnerUserId == actor.Id`.
- Added `LockOrder(tradeNo)` / `UnlockOrder(tradeNo)` around wallet auto-recharge Toss confirm handling, and mirrored the same serialization on the fail callback for the same trade number.
- Stopped duplicate confirm processing from re-running activation by returning success immediately when the loaded policy is already active under the order lock.
- Released pending policies on every confirm-path failure after load (`customerKey` mismatch, billing-key issue failure, billing-key store failure, activation failure) so the pending row no longer holds the unique `active_key`.
- Added focused controller regressions for strict org-owner role enforcement and confirm failure release behavior, including proof that a new pending policy can be created after the failed confirm path.

### Review TDD RED

Command:

```bash
env GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
```

Result:

```text
# github.com/QuantumNous/new-api/controller [github.com/QuantumNous/new-api/controller.test]
controller/wallet_auto_recharge_test.go:231:20: undefined: walletAutoRechargeBillingKeyIssuer
controller/wallet_auto_recharge_test.go:232:2: undefined: walletAutoRechargeBillingKeyIssuer
controller/wallet_auto_recharge_test.go:236:24: undefined: time
controller/wallet_auto_recharge_test.go:241:3: undefined: walletAutoRechargeBillingKeyIssuer
FAIL	github.com/QuantumNous/new-api/controller [build failed]
FAIL
```

### Review GREEN

Commands:

```bash
gofmt -w controller/wallet_auto_recharge.go controller/wallet_auto_recharge_test.go
env GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
```

Results:

```text
ok  	github.com/QuantumNous/new-api/controller	0.233s
```
