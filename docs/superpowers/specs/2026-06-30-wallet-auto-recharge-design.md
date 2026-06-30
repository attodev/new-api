# Wallet Auto Recharge Design

## Purpose

Add two wallet-based automatic payment policies that are separate from the existing subscription system:

- **정기결제**: charge a user-selected amount on a fixed interval, usually monthly, and add it to the normal wallet balance.
- **자동결제**: when wallet balance falls below a user-selected threshold, charge a predefined amount and add it to the normal wallet balance.

Both policies use Toss billing only. They must not create or modify subscription quota buckets. Successful charges behave like normal wallet top-ups: create a `TopUp` success record and increase the target wallet quota.

## Scope

This feature supports:

- Personal wallets.
- Organization wallets.
- One active scheduled policy and one active threshold policy per target.
- Organization policy management by organization owner only.
- Toss billing-key based charges.
- UI inside the existing wallet pages as tabs: `충전`, `정기결제`, `자동결제`.

This feature does not support:

- Other payment providers.
- Multiple active policies of the same type for one target.
- Changing existing subscription billing semantics.
- Admin-managed plan catalogs.

## Decisions

- Scheduled recharge can optionally charge immediately during setup.
- Threshold recharge uses user-facing money amounts in the UI, but the server stores and compares an internal quota threshold.
- Threshold recharge has both a cooldown and a daily count limit.
- Organization wallets charge the organization owner's Toss billing key.
- Existing subscription tables and APIs remain separate.

## Data Model

Create a new GORM model and table: `wallet_auto_recharges`.

Core fields:

- `id`
- `type`: `scheduled` or `threshold`
- `target_type`: `user` or `organization`
- `target_id`: user id for personal wallet, organization id for organization wallet
- `owner_user_id`: user who owns the Toss billing key
- `billing_key_id`: references existing `user_billing_keys`
- `amount`: amount to charge, in the same user-facing unit used by normal wallet top-up
- `threshold_amount`: user-facing threshold amount for threshold policies
- `threshold_quota`: server-side quota value used for balance comparison
- `interval_unit`: `month`, `day`, or `custom`
- `interval_value`
- `custom_seconds`
- `charge_immediately`
- `next_charge_time`
- `last_charge_time`
- `cooldown_until`
- `daily_charge_count`
- `daily_charge_date`
- `status`: `pending`, `active`, `cancelled`, or `failed`
- `fail_count`
- `last_error`
- `card_company`
- `card_number_masked`
- `last_trade_no`
- `created_at`
- `updated_at`

The model is migrated through GORM AutoMigrate and must avoid raw SQL. The active-one-policy rule is enforced in application transactions rather than partial unique indexes so SQLite, MySQL, and PostgreSQL all work consistently.

## Wallet Targeting

Personal wallet:

- `target_type = user`
- `target_id = current user id`
- `owner_user_id = current user id`

Organization wallet:

- `target_type = organization`
- `target_id = organization id`
- `owner_user_id = organization.owner_user_id`
- Only the organization owner can create, update, or cancel policies.
- Successful charges create a `TopUp` with `target_type = organization` and `target_id = organization id`.

## Toss Billing Flow

Policy setup uses the same Toss billing authorization pattern as existing Toss subscription billing:

1. User submits scheduled or threshold policy settings.
2. Backend validates Toss billing availability, target access, amount limits, and active-policy uniqueness.
3. Backend creates a `wallet_auto_recharges` row in `pending` status.
4. Backend returns Toss billing auth data: `client_key`, `customer_key`, `trade_no`, `success_url`, `fail_url`.
5. Frontend opens Toss billing auth.
6. Toss redirects to success callback with `authKey` and `customerKey`.
7. Backend verifies the pending policy and customer key.
8. Backend exchanges `authKey` for `billingKey`.
9. Backend stores the billing key in `user_billing_keys`.
10. Backend activates the policy and stores masked card metadata.
11. For scheduled policies with `charge_immediately = true`, backend immediately attempts the first wallet top-up.

The confirm/fail callbacks are separate from existing subscription callbacks and live under wallet auto-recharge routes.

## Charging Flow

Add a service task that runs on the master node every minute.

Scheduled policy:

1. Query active scheduled policies with `next_charge_time <= now`.
2. Lock one policy row before charging.
3. Charge Toss with a deterministic trade number and idempotency key.
4. On success, create a successful `TopUp` row and increase target quota.
5. Advance `next_charge_time` from the intended schedule, not from arbitrary job delay.
6. Reset `fail_count`.
7. On failure, increment `fail_count`; after the max failure count, mark policy `failed`.

Threshold policy:

1. Query active threshold policies.
2. Skip if `cooldown_until > now`.
3. Skip if daily limit is reached.
4. Read target wallet quota.
5. Charge only when `quota <= threshold_quota`.
6. On success, create a successful `TopUp` row and increase target quota.
7. Set `cooldown_until = now + 1 hour`.
8. Increment the daily charge count for the current date.
9. On failure, increment `fail_count`; after the max failure count, mark policy `failed`.

Default safety values:

- Threshold cooldown: 1 hour.
- Threshold daily charge limit: 3 successful automatic charges per policy.
- Max failure count: 3.

## TopUp Integration

Successful automatic charges use the existing wallet crediting path:

- Personal: increase `users.quota`.
- Organization: increase `organizations.quota`.
- Create `TopUp` with:
  - `payment_method = toss`
  - `payment_provider = toss`
  - `status = success`
  - `money = amount`
  - `amount = charged KRW`
  - `target_type` and `target_id` for organization targets
  - `provider_order_id` or provider payload containing Toss charge metadata

This makes automatic recharge visible in existing wallet top-up history.

## API Design

Personal wallet:

- `GET /api/wallet/auto-recharge`
- `POST /api/wallet/auto-recharge/scheduled`
- `POST /api/wallet/auto-recharge/threshold`
- `DELETE /api/wallet/auto-recharge/:id`

Organization wallet:

- `GET /api/organization/wallet/auto-recharge`
- `POST /api/organization/wallet/auto-recharge/scheduled`
- `POST /api/organization/wallet/auto-recharge/threshold`
- `DELETE /api/organization/wallet/auto-recharge/:id`

Callbacks:

- `GET /api/wallet/auto-recharge/toss/confirm`
- `GET /api/wallet/auto-recharge/toss/fail`

The callback resolves the pending policy by trade number and applies the policy target recorded in the database. Personal and organization setup flows use the same callback endpoints; organization routing is derived from `target_type = organization` and `target_id`.

## Frontend Design

Personal wallet page:

- Add tabs inside the existing wallet page:
  - `충전`
  - `정기결제`
  - `자동결제`
- Keep the existing top-up UI in `충전`.
- `정기결제` shows amount, interval, immediate-charge toggle, card/status, next charge time, and cancel action.
- `자동결제` shows threshold amount, recharge amount, cooldown/daily-limit explanation, card/status, last charge time, and cancel action.

Organization wallet page:

- Add the same tabs.
- Non-owner organization users can view policy status but cannot create or cancel policies.
- Organization owner sees setup and cancel actions.

Copy must avoid the word "구독" for this feature. Use wallet language:

- 정기결제: "정해진 주기마다 지갑에 자동 충전"
- 자동결제: "잔액이 기준 이하일 때 지갑에 자동 충전"

## Validation

Common validation:

- Toss billing must be enabled.
- Payment compliance must be confirmed.
- Amount must be at least `TossMinTopUp`.
- Active policy of the same type must not already exist for the same target.
- Pending stale policies can be expired or overwritten by a new setup attempt.

Scheduled validation:

- Interval unit must be supported.
- Interval value must be positive unless custom seconds is used.
- Custom seconds must be positive.

Threshold validation:

- Threshold amount must be non-negative.
- Threshold quota is calculated server-side using current quota conversion rules.
- Recharge amount must be positive and at least `TossMinTopUp`.

Organization validation:

- Current user must be the organization owner.
- Owner's Toss customer key is used for billing authorization.

## Error Handling

Toss billing authorization failure:

- Pending policy is marked `cancelled` or `failed`.
- User is redirected back to the wallet page.

Toss charge failure:

- `fail_count` increments.
- `last_error` stores a short error message.
- Policy is marked `failed` after the max failure count.

Charge succeeded but local credit failed:

- Log an explicit reconciliation-required error with trade number, user/organization, amount, and policy id.
- Do not silently retry with a new trade number.

Duplicate job execution:

- Policy rows are locked in transactions.
- Toss idempotency key is based on the policy id and scheduled/trigger cycle.
- TopUp trade numbers are unique.

## Tests

Backend tests:

- Personal scheduled policy with immediate charge creates successful `TopUp` and increases `users.quota`.
- Personal scheduled policy without immediate charge activates without increasing quota.
- Scheduled job charges due policies and advances `next_charge_time`.
- Threshold job skips when balance is above threshold.
- Threshold job charges when balance is at or below threshold.
- Threshold cooldown prevents repeated charge.
- Threshold daily limit prevents additional same-day charge.
- Organization owner can create organization wallet policies.
- Non-owner cannot create or cancel organization wallet policies.
- Organization charge increases `organizations.quota` and creates `TopUp target_type=organization`.
- Toss failure increments `fail_count` and eventually marks policy failed.
- Existing subscription purchase and Toss subscription renewal tests continue to pass.

Frontend tests:

- Wallet page renders `충전`, `정기결제`, and `자동결제` tabs.
- Scheduled form validates amount and interval.
- Threshold form validates threshold and amount.
- Organization non-owner cannot submit setup/cancel actions.
- Existing wallet recharge flow remains reachable from the `충전` tab.

## Rollout Notes

- Existing users and subscription data require no migration.
- New table is additive.
- Existing Toss billing settings are reused.
- Existing subscription Toss auto-renew remains untouched.
- The feature can be deployed disabled implicitly when Toss billing is not configured.
