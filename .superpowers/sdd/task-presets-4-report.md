Status: PASS

Files changed:
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx`
- `web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
- `web/default/src/features/system-settings/billing/section-registry.tsx`
- `web/default/src/features/wallet/api.ts`
- `web/default/src/features/wallet/types.ts`

Commits:
- `8008c99e feat(settings): manage wallet auto recharge presets`

Exact tests and pass/fail summaries:
- `cd web/default && BUN_TMPDIR=/tmp bun test ./src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts` — PASS (2 pass, 0 fail)
- `cd web/default && BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts` — PASS (2 pass, 0 fail)
- `cd web/default && BUN_TMPDIR=/tmp bun run build` — PASS

Self-review notes:
- Added admin-only wallet auto recharge preset CRUD API helpers and a shared preset request type without changing user/org subscription behavior.
- Registered a new billing settings section for auto recharge presets and kept `billing/index.tsx` untouched because registry integration was sufficient.
- Built the admin section around existing system-settings and shadcn/ui patterns, including grouped scheduled/threshold tables, dialog-based create/edit flows, and guarded delete confirmation.
- Helper coverage is focused on the required normalization and summary behavior; broader locale sync remains out of scope for Task 5.
## Task 4 Fix Worker Report - 2026-06-30

- Fixed `normalizePresetForm` to normalize by preset mode instead of leaking hidden form state:
  - scheduled presets now force `threshold_amount` to `0`
  - threshold presets now force scheduled-only fields to safe defaults (`interval_unit: 'month'`, `interval_value: 1`, `custom_seconds: 0`, `charge_immediately: false`)
  - `custom_seconds` is cleared whenever `interval_unit !== 'custom'`
- Added helper coverage for stale hidden-value cases, including a type-switch scenario.
- Updated summary helpers to support injected translation and wired rendered summaries through `t(...)` so the displayed preset summaries are i18n-compliant.
- Verified with:
  - `cd web/default && BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
  - `cd web/default && BUN_TMPDIR=/tmp bun run build`

## Task 4 Fix Worker Report - 2026-06-30 (i18n follow-up)

- Updated scheduled preset summaries so interval units no longer interpolate raw backend tokens like `month` or `day`; month/day now flow through translation keys before interpolation, while custom-second summaries stay on the existing translated path.
- Added a translator-spy helper test that proves scheduled summaries request `Month(s)` instead of passing raw `month` through the rendered string path.
- Localized preset CRUD fallback errors at the throw-site so toast messages surface `t('Failed to ... preset')` values when the backend does not provide a message.
- Added locale entries for the new interval-unit and fallback-error keys in `en`, `zh`, `fr`, `ru`, `ja`, `vi`, and `kr`.
- Verified with:
  - `cd web/default && BUN_TMPDIR=/tmp bun test src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts`
  - `cd web/default && BUN_TMPDIR=/tmp bun run build`
