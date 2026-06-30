Status: PASS

Files changed:
- `web/default/src/features/wallet/types.ts`
- `web/default/src/features/wallet/api.ts`
- `web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts`
- `web/default/src/features/wallet/components/auto-recharge-card.tsx`
- `web/default/src/features/wallet/components/auto-recharge-card.test.ts`
- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/organizations/components/organization-wallet.tsx`

Commits:
- `feat(wallet): select auto recharge presets in wallet UI`

Exact tests and pass/fail summaries:
- `cd web/default && BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts` — PASS (9 pass, 0 fail)
- `cd web/default && BUN_TMPDIR=/tmp bun run build` — PASS

Self-review notes:
- Preset creation now sends only `{ preset_id }` for scheduled and threshold modes.
- Scheduled/threshold tabs are hidden unless an enabled preset exists or an active/pending policy already exists.
- Existing active/pending policies remain visible and cancellable even when no preset is available.
- Wallet UI changes were kept scoped to the frontend wallet files and the organization wallet view used in this repo.

---

Fix worker follow-up:

- Root cause: `useWalletAutoRecharge` exposed only `presets`, so the wallet screens treated the initial `[]` and failed preset refreshes as authoritative empty data and hid scheduled/threshold tabs too early.
- Fix: added separate preset fetch state in `use-wallet-auto-recharge.ts` via `presetsLoading` and `presetsLoaded`, setting `presetsLoaded` only after a successful presets response.
- Visibility logic now keeps both auto-recharge tabs visible until presets have successfully resolved, while still hiding them after a confirmed empty preset list when there is no active/pending policy.
- Existing active/pending policy visibility and cancellation behavior remains unchanged for both user and organization wallet screens.
- Added a focused helper test covering the unresolved preset-fetch case, then re-ran the required wallet test and frontend build successfully.
