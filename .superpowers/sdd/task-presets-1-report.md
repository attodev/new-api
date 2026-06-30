status: DONE_WITH_CONCERNS

files changed:
- model/main.go
- model/wallet_auto_recharge_preset.go
- model/wallet_auto_recharge_preset_test.go
- .superpowers/sdd/task-presets-1-report.md

commits created:
- feat(wallet): add auto recharge preset model

tests run:
- `env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRechargePreset -count=1` — PASS
- `env GOCACHE=/tmp/go-build-cache go test ./model -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1` — PASS
- `env GOCACHE=/tmp/go-build-cache go test ./model -count=1` — FAIL; unrelated existing model tests fail in the shared in-memory SQLite setup with missing-table errors and one pre-existing wallet-auto-recharge unique-constraint failure

self-review notes:
- Added the preset model and request validation with DB-compatible GORM usage and timestamp fields.
- Registered the new table in both full and fast migration paths.
- Kept the change scoped to the Task 1 files plus the required report file.
- Full package verification is still noisy because unrelated model tests in this repo currently assume tables that are not present in their setup.

fix report:
status: fixed
files changed:
- model/wallet_auto_recharge_preset.go
- model/wallet_auto_recharge_preset_test.go
- .superpowers/sdd/task-presets-1-report.md
commits created:
- fix(wallet): harden auto recharge preset model
tests run:
- `env GOCACHE=/tmp/go-build-cache go test ./model -run TestDisableWalletAutoRechargePresetReturnsErrorForMissingRow -count=1` — FAIL as expected before the fix; disable returned nil for a missing row
- `env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRechargePreset -count=1` — PASS
- `env GOCACHE=/tmp/go-build-cache go test ./model -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1` — PASS
self-review notes:
- `DisableWalletAutoRechargePreset` now checks `RowsAffected` and returns a not-found error when no row matches.
- Added regression coverage for missing-row disable handling, update persistence, disabled-row filtering, and `sort_order` ordering.
- Kept the scope limited to the preset model and its tests; no optional field-normalization changes were introduced.
