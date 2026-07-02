# Task 2 Report: Auto Recharge Creation Lock

## Status
- Implemented creation locking in `AutoRechargeCard`.
- Added component tests for locked preset creation and active-policy cancellation.
- Kept the change scoped to the requested component and test files.

## TDD Evidence

### RED
Command:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Observed failure:

```text
AssertionError: The input did not match the regular expression /Cancel the current payment setting before choosing another one\./.
(fail) auto recharge preset UI helpers > disables scheduled preset creation when another payment setting is active

15 pass
1 fail
Ran 16 tests across 1 file. [277.00ms]
```

### GREEN
Command:

```bash
cd web/default
bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Observed success:

```text
16 pass
0 fail
Ran 16 tests across 1 file. [274.00ms]
```

## Files Changed
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- `web/default/src/features/wallet/components/auto-recharge-card.test.ts`

## Implementation Notes
- Added `creationDisabled?: boolean` and `creationDisabledMessageKey?: string` props.
- Added the exact default lock message key from the brief.
- Created `creationLocked = creationDisabled && !activePolicy`.
- Preset selection buttons now use `creationButtonDisabled`; cancel still uses the original `disabled` value.
- Rendered the lock message through `t(creationDisabledMessageKey)`.

## Concerns
- Locale sync is intentionally deferred to Task 4 per the brief.
- Existing unrelated untracked file `new-api-bin` was left untouched.
