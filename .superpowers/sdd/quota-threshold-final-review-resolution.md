# Final Review Resolution: Quota Threshold Auto Recharge

## Important Finding: Legacy Preset Cleanup

Reviewer concern:

- Existing enabled threshold presets created before `threshold_quota` can appear with zero quota and would trigger only at `quota <= 0`.

Resolution:

- This is an accepted rollout constraint from the user-approved no-migration policy.
- A read-only check of the default SQLite DB at `/home/molla/new-api/one-api.db` showed there is no `wallet_auto_recharge_presets` table before deploying this build.
- Therefore, in that default SQLite DB there are no legacy enabled threshold presets to migrate or delete.
- Live systemd environment variables were not read because that can expose DB credentials.
- If this service is configured to use a secret external DB instead of the default SQLite DB, the required rollout action is to delete and recreate old threshold auto-recharge presets from the admin UI after deployment.

## Minor Finding: Missing Task 5 Report

Resolution:

- Recreated `/tmp/new-api-wallet-auto-recharge/.superpowers/sdd/quota-threshold-task-5-report.md`.
- The report now includes exact focused test/build command evidence and the legacy preset rollout check.

## Minor Finding: Static User UI Tests

Resolution:

- Accepted as a non-blocking follow-up.
- Current tests verify the rendered quota-first sentence, formatted quota display, KRW amount buttons, confirmation button, and active policy quota summary.
