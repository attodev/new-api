# Task 5 Report: Integration Verification

## What Was Verified

- Backend wallet auto-recharge model tests.
- Backend wallet auto-recharge controller tests.
- Frontend wallet and admin auto-recharge focused tests.
- Frontend i18n sync.
- Go production binary build.
- Default frontend production build.
- Whitespace sanity with `git diff --check`.

## Commands and Results

```bash
GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1
```

Result: PASS

```text
ok  	github.com/QuantumNous/new-api/model	0.460s
```

```bash
GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1
```

Result: PASS

```text
ok  	github.com/QuantumNous/new-api/controller	0.468s
```

```bash
cd web/default
bun test src/features/wallet src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
```

Result: PASS

```text
50 pass
0 fail
30 expect() calls
Ran 50 tests across 7 files.
```

```bash
cd web/default
bun run i18n:sync
```

Result: PASS

```text
i18n sync done. Report: /tmp/new-api-wallet-auto-recharge/web/default/src/i18n/locales/_reports/_sync-report.json
```

```bash
GOCACHE=/tmp/go-build-cache go build -buildvcs=false -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api-bin
```

Result: PASS

```bash
cd web/default
DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../../VERSION) bun run build
```

Result: PASS

```text
Rsbuild v2.0.7
ready   built in 6.46 s
```

```bash
git diff --check
```

Result: PASS

## Legacy Preset Rollout Check

- Read-only SQLite check of `/home/molla/new-api/one-api.db` found no `wallet_auto_recharge_presets` table before deploying this build, so there are no legacy enabled threshold presets in that default SQLite DB.
- Reading live systemd environment variables was not performed because it could expose DB credentials.
- If the running service uses an external DB through secret environment configuration, admins should delete and recreate old threshold auto-recharge presets after deployment, as agreed in the no-migration policy.

## Working Tree Notes

- No tracked file diffs remain after verification.
- `new-api-bin` is an untracked build output and is intentionally not committed.
- Existing untracked locale `_reports/*.untranslated.json` files remain untracked.

## Concerns

- None for the focused verification required by the plan.
- Broad `bun run typecheck` was observed by task workers to fail on pre-existing unrelated project-wide TypeScript issues; it is not part of the required final verification for this plan.
