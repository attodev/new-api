Task 6 verification report
Timestamp: 2026-06-30T07:53:17Z
Worktree: /tmp/new-api-wallet-auto-recharge
Branch: codex/wallet-auto-recharge

Summary
- Focused backend verification passed.
- Frontend wallet test passed.
- Frontend language test failed due to expectations in `web/default/src/i18n/languages.test.ts` no longer matching the current multilingual `web/default/src/i18n/languages.ts`.
- `bun run typecheck` failed in multiple non-wallet areas plus the same language test file.
- `bun run build` passed.
- Root `go build -o new-api-bin .` failed because `main.go` embeds `web/classic/dist`, but that directory is absent in this worktree.

Command results
1. `env GOCACHE=/tmp/go-build-cache go test ./model -run WalletAutoRecharge -count=1`
   - PASS
   - Output: `ok github.com/QuantumNous/new-api/model`

2. `env GOCACHE=/tmp/go-build-cache go test ./controller -run WalletAutoRecharge -count=1`
   - PASS
   - Output: `ok github.com/QuantumNous/new-api/controller`

3. `env GOCACHE=/tmp/go-build-cache go test ./service -run WalletAutoRecharge -count=1`
   - PASS
   - Output: `ok github.com/QuantumNous/new-api/service`

4. `env GOCACHE=/tmp/go-build-cache go test ./router -count=1`
   - PASS
   - Output: `? github.com/QuantumNous/new-api/router [no test files]`

5. `cd web/default && BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts`
   - PASS
   - 5 tests passed, 0 failed

6. `cd web/default && BUN_TMPDIR=/tmp bun test src/i18n/languages.test.ts`
   - FAIL
   - Task ownership: pre-existing unrelated to wallet auto-recharge
   - Evidence:
     - `INTERFACE_LANGUAGE_OPTIONS` in `web/default/src/i18n/languages.ts` exposes `en`, `zh`, `fr`, `ja`, `kr`, `ru`, `vi`
     - `web/default/src/i18n/languages.test.ts` still expects only `en`, `kr`
     - Test also expects `normalizeInterfaceLanguage('zh-CN')` and `('fr')` to fall back to `en`, but implementation intentionally maps supported languages through

7. `cd web/default && BUN_TMPDIR=/tmp bun run i18n:sync`
   - PASS
   - Generated/updated report: `web/default/src/i18n/locales/_reports/_sync-report.json`

8. `cd web/default && BUN_TMPDIR=/tmp bun run typecheck`
   - FAIL
   - Task-owned failures by path:
     - none found
   - Pre-existing unrelated failures by path:
     - `web/default/src/features/organizations/components/organization-dashboard.tsx`
     - `web/default/src/features/organizations/components/organization-users-table.tsx`
     - `web/default/src/features/system-settings/billing/index.tsx`
     - `web/default/src/features/system-settings/models/vendor-discount-visual-editor.tsx`
     - `web/default/src/features/usage-logs/components/common-logs-filter-bar.tsx`
     - `web/default/src/features/usage-logs/components/task-logs-filter-bar.tsx`
     - `web/default/src/features/usage-logs/components/usage-logs-mobile-card.tsx`
     - `web/default/src/features/usage-logs/components/usage-logs-table.tsx`
     - `web/default/src/features/usage-logs/index.tsx`
     - `web/default/src/i18n/languages.test.ts` (`bun:test` type resolution plus stale expectations against current multilingual implementation)

9. `cd web/default && BUN_TMPDIR=/tmp bun run build`
   - PASS
   - Rsbuild completed successfully

10. `env GOCACHE=/tmp/go-build-cache go build -o new-api-bin .`
    - FAIL
    - Task ownership: pre-existing unrelated to wallet auto-recharge
    - Error: `main.go:44:12: pattern web/classic/dist: no matching files found`
    - Read-only follow-up:
      - `main.go` embeds `web/classic/dist` and `web/classic/dist/index.html`
      - `web/classic/` exists in this worktree, but no `dist/` directory is present

Source edits
- No source files were edited.
- Non-source/generated change from verification:
  - `web/default/src/i18n/locales/_reports/_sync-report.json`

Commits
- None created.

Overall assessment
- Wallet auto-recharge focused backend tests, wallet frontend tests, frontend build, and router package check are acceptable.
- Verification is not fully green because of pre-existing unrelated failures in `languages.test.ts`, broader frontend typecheck, and missing `web/classic/dist` for root backend build.

Task 6 fix update
Timestamp: 2026-06-30T08:01:00Z

Fix details
- Updated `web/default/src/i18n/languages.test.ts` to match Task 5 runtime language support.
- UI language list now expects `en`, `zh`, `fr`, `ja`, `kr`, `ru`, `vi`.
- Preserved Korean normalization coverage and added an underscore variant (`ko_KR`).
- Added supported browser language variant coverage for `zh-CN`, `fr-CA`, `ja-JP`, `ru-RU`, and `vi-VN`.
- Kept unknown-language fallback coverage to English.

Validation rerun
1. `cd web/default && BUN_TMPDIR=/tmp bun test src/i18n/languages.test.ts`
   - PASS
   - 4 tests passed, 0 failed
   - Output:
     - `interface languages > exposes all supported interface languages in the UI language list`
     - `interface languages > normalizes Korean browser language variants to kr`
     - `interface languages > normalizes supported browser language variants`
     - `interface languages > falls back unknown languages to English`

2. `cd web/default && BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts`
   - PASS
   - 5 tests passed, 0 failed

3. `cd web/default && BUN_TMPDIR=/tmp bun run typecheck`
   - FAIL
   - Remaining failures are unrelated frontend type errors in:
     - `src/features/organizations/components/organization-dashboard.tsx`
     - `src/features/organizations/components/organization-users-table.tsx`
     - `src/features/system-settings/billing/index.tsx`
     - `src/features/system-settings/models/vendor-discount-visual-editor.tsx`
     - `src/features/usage-logs/components/common-logs-filter-bar.tsx`
     - `src/features/usage-logs/components/task-logs-filter-bar.tsx`
     - `src/features/usage-logs/components/usage-logs-mobile-card.tsx`
     - `src/features/usage-logs/components/usage-logs-table.tsx`
     - `src/features/usage-logs/index.tsx`
   - Also still fails on `src/i18n/languages.test.ts` because `tsc -b` cannot resolve `bun:test` types in the current project typecheck setup.

Commits
- Commit created for this fix: `test: update interface language expectations`
