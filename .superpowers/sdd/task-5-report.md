# Task 5 Report

## Status

DONE_WITH_CONCERNS

## Files Changed

- `web/default/src/features/wallet/index.tsx`
- `web/default/src/features/organizations/components/organization-wallet.tsx`
- `web/default/src/i18n/locales/en.json`
- `web/default/src/i18n/locales/kr.json`
- `web/default/src/i18n/locales/zh.json`
- `web/default/src/i18n/locales/fr.json`
- `web/default/src/i18n/locales/ja.json`
- `web/default/src/i18n/locales/ru.json`
- `web/default/src/i18n/locales/vi.json`

## What Changed

- Added `Tabs` integration to the personal wallet page so the existing recharge/subscription UI remains under `Top up`, with `Scheduled recharge` and `Auto recharge` tabs rendering Task 4's `AutoRechargeCard`.
- Added the same tabbed wallet management layout to the organization wallet page.
- Wired organization auto-recharge permissions to the current auth user id and `organization.owner_user_id`; management is disabled when there is no current user or the ids do not match.
- Kept organization wallet payment/top-up flow intact under the `Top up` tab and used the wallet auto-recharge scope directly from `useWalletAutoRecharge('organization', canManageAutoRecharge)`.
- Added the required English-source i18n keys to `en`, `zh`, `fr`, `ja`, `ru`, and `vi`.
- Added matching Korean translations in `kr.json` because this branch's `i18n:sync` uses `kr.json` as the base locale; without that, the required English keys were stripped back out during sync.

## i18n Keys Added

- `Top up`
- `Scheduled recharge`
- `Auto recharge`
- `Recharge amount`
- `Threshold balance`
- `Charge immediately`
- `Current policy`
- `Charge interval`
- `day`
- `month`
- `Next charge`
- `Card`
- `No policy configured`
- `You do not have permission to manage this.`
- `Interval value`
- `Minimum recharge amount is {{amount}}.`
- `Please enter a valid interval value.`
- `Please enter a valid threshold balance.`

## Validation

### Temporary red-green check

Command:

```bash
cd /tmp/new-api-wallet-auto-recharge/web/default
BUN_TMPDIR=/tmp bun test /tmp/task5-org-wallet-permission.test.ts
```

Red:

```text
SyntaxError: Export named 'canManageOrganizationAutoRecharge' not found
```

Green:

```text
(pass) organization wallet auto-recharge permission > requires the current auth user to match the organization owner
```

### i18n sync

Command:

```bash
cd /tmp/new-api-wallet-auto-recharge/web/default
BUN_TMPDIR=/tmp bun run i18n:sync
```

Result:

```text
$ node scripts/sync-i18n.mjs
i18n sync done. Report: /tmp/new-api-wallet-auto-recharge/web/default/src/i18n/locales/_reports/_sync-report.json
```

Observed sync report before restoring the tracked report file:

```json
{
  "base": "en.json",
  "locales": {
    "en": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 0 },
    "fr": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 1094 },
    "ja": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 4281 },
    "kr": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 64 },
    "ru": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 4281 },
    "vi": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 1094 },
    "zh": { "missingCount": 0, "extrasCount": 0, "untranslatedCount": 4281 }
  }
}
```

Key presence check after sync:

```text
en: all keys present
zh: all keys present
fr: all keys present
ja: all keys present
ru: all keys present
vi: all keys present
kr: all keys present
```

### Task 4 regression test

Command:

```bash
cd /tmp/new-api-wallet-auto-recharge/web/default
BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts
```

Result:

```text
3 pass
0 fail
```

### Typecheck

Command:

```bash
cd /tmp/new-api-wallet-auto-recharge/web/default
BUN_TMPDIR=/tmp bun run typecheck
```

Result:

```text
$ tsc -b
```

Existing unrelated errors remain in:

- `src/features/organizations/components/organization-dashboard.tsx`
- `src/features/organizations/components/organization-users-table.tsx`
- `src/features/system-settings/billing/index.tsx`
- `src/features/system-settings/models/vendor-discount-visual-editor.tsx`
- `src/features/usage-logs/components/*`
- `src/features/usage-logs/index.tsx`
- `src/i18n/languages.test.ts`

No typecheck errors were reported from:

- `src/features/wallet/index.tsx`
- `src/features/organizations/components/organization-wallet.tsx`

### Targeted lint attempt

Command:

```bash
cd /tmp/new-api-wallet-auto-recharge/web/default
BUN_TMPDIR=/tmp bunx eslint src/features/wallet/index.tsx src/features/organizations/components/organization-wallet.tsx
```

Result:

```text
TypeError: (0 , brace_expansion_1.expand) is not a function
```

This appears to be an environment/tooling issue in the branch's ESLint dependency graph, not a file-specific lint finding.

## Self-Review

- Kept the existing recharge flow reachable without extra clicks under the first tab for both personal and organization wallets.
- Reused Task 4's `AutoRechargeCard` and `useWalletAutoRecharge` directly instead of extending organization API wrappers.
- Bound org management to owner identity only, with `null` current-user state safely treated as no permission.
- Preserved all new visible UI strings as `t('English source key')`.

## Concerns

- The task brief expected six locale files, but this worktree originally only shipped `en.json` and `kr.json`, with frontend language config still limited to `en`/`kr`. I added the requested locale files, but they are not wired into `src/i18n/config.ts` or `src/i18n/languages.ts` because those files were outside the requested task scope.
- To make `bun run i18n:sync` preserve the new English-source keys, I had to update `kr.json` as well because this branch's sync base would otherwise strip those keys from every locale file.
- `i18n:sync` generated untracked untranslated-report files under `web/default/src/i18n/locales/_reports/`; they were not staged for commit.

## Review Fix Round

### Findings addressed

1. Restored the regressed Toss-related English locale values in `web/default/src/i18n/locales/en.json` while keeping the Task 5 wallet auto-recharge keys.
2. Moved `AffiliateRewardsCard` inside the personal wallet `Top up` tab content so it no longer appears alongside the scheduled/threshold auto-recharge tabs.
3. Added an optional `permissionMessageKey` override to `AutoRechargeCard` and passed `Only the organization owner can change auto payments` from the organization wallet tabs.
4. Added the new owner-specific permission key to `en`, `kr`, `zh`, `fr`, `ja`, `ru`, and `vi`.
5. Extended `auto-recharge-card.test.ts` to cover the restored English Toss strings and the organization-specific permission copy override.

### Validation rerun

#### `cd web/default && BUN_TMPDIR=/tmp bun run i18n:sync`

```text
$ node scripts/sync-i18n.mjs
i18n sync done. Report: /tmp/new-api-wallet-auto-recharge/web/default/src/i18n/locales/_reports/_sync-report.json
```

#### `cd web/default && BUN_TMPDIR=/tmp bun test src/features/wallet/components/auto-recharge-card.test.ts`

```text
bun test v1.3.14 (0d9b296a)

src/features/wallet/components/auto-recharge-card.test.ts:
(pass) auto recharge card helpers > uses English source i18n keys for mode titles
(pass) auto recharge card helpers > treats blank money input as invalid while preserving explicit zero
(pass) auto recharge card helpers > preserves explicit zero values when hydrating an active policy
(pass) auto recharge card helpers > keeps existing Toss English locale values in en.json
(pass) auto recharge card helpers > renders organization-specific permission copy when provided

 5 pass
 0 fail
```

#### `cd web/default && BUN_TMPDIR=/tmp bun run typecheck`

```text
$ tsc -b
src/features/organizations/components/organization-dashboard.tsx(720,13): error TS2322
src/features/organizations/components/organization-users-table.tsx(83,3): error TS6133
src/features/organizations/components/organization-users-table.tsx(104,7): error TS6133
src/features/organizations/components/organization-users-table.tsx(862,41): error TS2322
src/features/system-settings/billing/index.tsx(27,7): error TS2740
src/features/system-settings/models/vendor-discount-visual-editor.tsx(344,56): error TS2345
src/features/usage-logs/components/common-logs-filter-bar.tsx(88,20): error TS2769
src/features/usage-logs/components/common-logs-filter-bar.tsx(90,48): error TS2769
src/features/usage-logs/components/common-logs-filter-bar.tsx(91,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(92,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(93,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(94,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(95,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(96,7): error TS2322
src/features/usage-logs/components/common-logs-filter-bar.tsx(97,7): error TS2322
src/features/usage-logs/components/task-logs-filter-bar.tsx(82,20): error TS2769
src/features/usage-logs/components/task-logs-filter-bar.tsx(84,48): error TS2769
src/features/usage-logs/components/usage-logs-mobile-card.tsx(203,63): error TS2339
src/features/usage-logs/components/usage-logs-mobile-card.tsx(204,58): error TS2339
src/features/usage-logs/components/usage-logs-table.tsx(92,5): error TS2322
src/features/usage-logs/index.tsx(201,15): error TS2322
src/i18n/languages.test.ts(1,40): error TS2307
```

These are unchanged unrelated branch errors; no new typecheck failures were introduced by the Task 5 review fixes.
