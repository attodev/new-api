status: DONE

files changed:
- controller/wallet_auto_recharge.go
- controller/wallet_auto_recharge_preset.go
- controller/wallet_auto_recharge_preset_test.go
- controller/wallet_auto_recharge_test.go
- model/wallet_auto_recharge.go
- router/api-router.go

commits created:
- 5ff573b9 feat(wallet): create auto recharge policies from presets

tests run with exact commands and pass/fail output summary:
- `env GOCACHE=/tmp/go-build-cache go test ./controller -run "WalletAutoRechargePreset|RequiresPreset" -count=1`
  - FAIL
  - Summary: missing `CreateWalletAutoRechargePreset`, `ListWalletAutoRechargePresets`, `DeleteWalletAutoRechargePreset`, `GetWalletAutoRechargePresets`, and `model.WalletAutoRecharge.PresetId`.
- `env GOCACHE=/tmp/go-build-cache go test ./controller -run "WalletAutoRechargePreset|WalletAutoRecharge" -count=1`
  - PASS
  - Summary: `ok  	github.com/QuantumNous/new-api/controller	0.432s`
- `env GOCACHE=/tmp/go-build-cache go test ./router -count=1`
  - PASS
  - Summary: `?   	github.com/QuantumNous/new-api/router	[no test files]`

self-review notes:
- Preset-backed create requests now reject missing `preset_id` and copy billing policy fields from the preset snapshot instead of trusting client-supplied schedule data.
- Added user, organization, and admin preset handlers using `common.DecodeJson` for request bodies.
- Added `preset_id` persistence to wallet auto recharge policies so downstream flows can trace the originating preset.
- Kept route ordering safe by inserting the new `/wallet/auto-recharge/presets` routes ahead of create/delete patterns and preserving `/pending/:trade_no` before `/:id`.

---

status: DONE

files changed:
- router/api-router.go
- router/api_router_wallet_auto_recharge_test.go

commit:
- current HEAD (`fix(router): move wallet preset admin routes`)

tests:
- `env GOCACHE=/tmp/new-api-gocache GOTMPDIR=/tmp/new-api-gotmp go test ./router -run TestSetApiRouterRegistersWalletAutoRechargePresetRoutes -count=1`
  - PASS
  - Summary: `ok  	github.com/QuantumNous/new-api/router	0.058s`
- `env GOCACHE=/tmp/new-api-gocache GOTMPDIR=/tmp/new-api-gotmp go test ./controller -run 'TestAdminCanCreateListAndDisableWalletAutoRechargePreset|TestUserPresetListFiltersByWalletTarget|TestWalletAutoRechargeCreateRequiresPresetAndCopiesSnapshot' -count=1`
  - PASS
  - Summary: `ok  	github.com/QuantumNous/new-api/controller	0.192s`

self-review notes:
- Root cause was the preset admin handlers being mounted from `userRoute`, which both misplaced them under `/api/user/...` and caused Gin to panic on duplicate `GET /api/user/wallet/auto-recharge/presets`.
- Moved only the wallet auto recharge preset admin handlers to a dedicated `/api/admin` group guarded by `middleware.AdminAuth()`, leaving the existing `/api/user` admin surface unchanged.
- Added a focused router regression test that proves `/api/admin/wallet/auto-recharge/presets`, `/api/user/wallet/auto-recharge/presets`, and `/api/organization/wallet/auto-recharge/presets` are all registered together.
