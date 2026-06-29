# Toss Payments Integration — Design

**Date:** 2026-06-29
**Status:** Approved (design), pending implementation plan
**Scope:** Add Toss Payments (토스페이먼츠) as a payment provider for both one-time top-up (충전) and recurring subscription billing (빌링키 자동 정기결제).

---

## 1. Background & Goals

new-api already supports several payment providers (Epay, Stripe, PayPal, Creem, Waffo, Waffo Pancake) wired through a consistent layered architecture:

```
Router → Controller (topup_<provider>.go / subscription_payment_<provider>.go)
       → Service / Model (TopUp, SubscriptionOrder, quota crediting)
       → Setting (payment_<provider>.go + model/option.go persistence)
       → Frontend (web/default/src/features/wallet)
```

We add **Toss Payments** following the closest existing analog (**PayPal** for top-up: redirect + server-side confirm + webhook; **Stripe** for subscription order shape) while accommodating two Toss-specific differences:

1. **Frontend SDK opens the payment window directly.** Other providers return a redirect URL and the browser navigates to it. Toss requires the Toss JS SDK (`@tosspayments/tosspayments-sdk`) to call `requestPayment()` / `requestBillingAuth()` client-side.
2. **Billing (구독) is server-driven recurring.** Stripe offloads recurrence to Stripe Checkout subscription mode. Toss issues a **billing key (빌링키)** that our server stores and charges on a schedule. This requires a **new recurring-charge cron subsystem** that does not exist today (the current `service/subscription_reset_task.go` only expires/resets, it never re-charges).

### Decisions locked in brainstorming

- **Frontend:** `web/default` (modern React 19 / Rsbuild). Backend is shared.
- **Scope:** Top-up **and** subscription via **true billing-key auto-recurring**.
- **Currency:** KRW direct — the user-entered top-up amount is treated as Korean won and passed to Toss `amount` as-is. A single `TossUnitPrice` constant maps KRW to internal quota.
- **Test mode:** Supported via a `TossTestMode` toggle separating test vs. live client/secret keys.
- **Phasing:** Phase 1 (top-up) ships first as the foundation; Phase 2 (billing subscription) builds on it. Each phase is independently functional and verifiable.

---

## 2. Toss API Reference (as used here)

### One-time payment
- Frontend: `loadTossPayments(clientKey)` → `payment.requestPayment({ method, amount: { currency: 'KRW', value }, orderId, orderName, successUrl, failUrl })`.
- On success the browser is redirected to `successUrl?paymentKey=...&orderId=...&amount=...`.
- Server confirm: `POST https://api.tosspayments.com/v1/payments/confirm`
  - Auth: `Authorization: Basic base64(secretKey + ":")`
  - Body: `{ "paymentKey", "orderId", "amount" }`
  - Response Payment object: `status` (`DONE` on success), `totalAmount`, `method`, `approvedAt`, `orderId`, `paymentKey`.

### Billing (recurring)
- Frontend: `payment.requestBillingAuth({ method: 'CARD', customerKey, successUrl, failUrl })`.
- On success: `successUrl?authKey=...&customerKey=...`.
- Issue billing key: `POST https://api.tosspayments.com/v1/billing/authorizations/issue`
  - Auth: Basic (secret key). Body: `{ "authKey", "customerKey" }`. Response contains `billingKey` (+ card metadata).
- Charge with billing key: `POST https://api.tosspayments.com/v1/billing/{billingKey}`
  - Auth: Basic (secret key). Body: `{ "customerKey", "amount", "orderId", "orderName" }`. Response is a Payment object (`status` `DONE`, `card`, etc.).

> All API base host is `https://api.tosspayments.com`. Toss does not use separate sandbox hosts; test vs. live is determined purely by which **key pair** is used. `TossTestMode` selects the key pair.

---

## 3. Shared Foundation (built in Phase 1)

### 3.1 Settings — `setting/payment_toss.go` (new)

```go
package setting

var TossEnabled = false
var TossTestMode = false
var TossClientKey = ""      // live
var TossSecretKey = ""      // live
var TossTestClientKey = ""  // test
var TossTestSecretKey = ""  // test
var TossUnitPrice = 1300.0  // KRW per 1 internal unit (USD-equivalent)
var TossMinTopUp = 1000     // minimum top-up in KRW

func TossActiveClientKey() string { if TossTestMode { return TossTestClientKey }; return TossClientKey }
func TossActiveSecretKey() string { if TossTestMode { return TossTestSecretKey }; return TossSecretKey }
```

### 3.2 Persistence — `model/option.go`

Mirror the PayPal pattern in both directions:
- In the option-map writer: `common.OptionMap["TossClientKey"] = setting.TossClientKey`, etc. (secret keys included so they persist, but never returned to the browser — see §6).
- In the `SetOption`/load switch: `case "TossClientKey": setting.TossClientKey = value`, `case "TossTestMode": setting.TossTestMode = value == "true"`, `case "TossUnitPrice": setting.TossUnitPrice, _ = strconv.ParseFloat(value, 64)`, etc.

### 3.3 Model constants — `model/topup.go`

```go
const PaymentMethodToss   = "toss"
const PaymentProviderToss = "toss"
```

### 3.4 KRW → quota conversion (locked)

- The user enters a **KRW amount** (`amountKRW`). Frontend shows it and passes it to Toss `amount`.
- Minimum enforced server-side: `amountKRW >= TossMinTopUp`.
- Quota credited on success:
  `quota = round( (amountKRW / TossUnitPrice) * QuotaPerUnit )`
  using `shopspring/decimal` for precision (same library `Recharge` uses).
- `TopUp.Amount` stores `amountKRW` (the KRW charged); `TopUp.Money` stores the USD-equivalent `amountKRW / TossUnitPrice` for reporting consistency with other providers.
- `topupGroupRatio` (group discount) and `AmountDiscount` are applied to the **charged KRW**, matching `getPayPalPayMoney` semantics: a Toss-specific `getTossPayMoney(amountKRW, group)` returns the final KRW to charge.

> This is the single business-critical constant. `TossUnitPrice` is admin-configurable in settings.

---

## 4. Phase 1 — One-time Top-up

### 4.1 Backend — `controller/topup_toss.go` (new)

| Handler | Route | Auth | Responsibility |
|---|---|---|---|
| `RequestTossPay` | `POST /api/user/toss/pay` | user | Validate (provider, min amount, redirect URL trust). Generate `orderId` (= `TopUp.TradeNo`, prefix `toss_` + sha1). Compute charged KRW via `getTossPayMoney`. Insert pending `TopUp`. Return `{ clientKey, orderId, orderName, amount, successUrl, failUrl }`. |
| `RequestTossAmount` | `POST /api/user/toss/amount` | user | Amount preview (returns charged KRW). |
| `TossConfirm` | `GET /api/toss/confirm` | public | successUrl handler. Read `paymentKey`,`orderId`,`amount`. `LockOrder`. Look up `TopUp` by `orderId`; validate provider + status pending + `amount == stored charged KRW`. Call confirm API. On `status==DONE` → `RechargeToss` (idempotent) → redirect `/console/log`. On mismatch/failure → log, redirect `/console/topup`. |
| `TossFail` | `GET /api/toss/fail` | public | failUrl handler. Mark order failed (best-effort), redirect `/console/topup`. |
| `TossWebhook` | `POST /api/toss/webhook` | public | Async settlement (e.g. virtual account `DONE`). Verify event, look up order, idempotent credit via same `RechargeToss`. |

Validation helpers mirror `validatePayPalTopUpOrder` (provider match, order id match, amount match). All crediting goes through `LockOrder`/`UnlockOrder` + DB `FOR UPDATE`.

### 4.2 Model — `model/topup.go`

`RechargeToss(referenceId, callerIp)` mirroring `RechargePayPal`:
- Transaction + `FOR UPDATE` on trade_no.
- Guard: provider == toss, status == pending.
- Set status success + complete time.
- `quota = decimal(amountKRW / TossUnitPrice).Mul(QuotaPerUnit)`.
- `CreditTopUpTarget(tx, topUp, quota)` (supports user + organization targets).
- `RecordTopupLog`.

### 4.3 Routes — `router/api-router.go`

```
# public (no auth)
GET  /api/toss/confirm   → TossConfirm
GET  /api/toss/fail      → TossFail
POST /api/toss/webhook   → TossWebhook

# user (auth)
POST /api/user/toss/pay     → RequestTossPay
POST /api/user/toss/amount  → RequestTossAmount

# organization mirror (auth)
POST /api/organization/toss/pay → RequestOrganizationTossPay
```

`successUrl = system_setting.ServerAddress + "/api/toss/confirm"`,
`failUrl = system_setting.ServerAddress + "/api/toss/fail"`.

### 4.4 Frontend — `web/default`

- Add dependency `@tosspayments/tosspayments-sdk` via Bun.
- `features/wallet/api.ts`: `requestTossPayment`, `calculateTossAmount`.
- `features/wallet/hooks/use-toss-payment.ts` (new): call `/api/user/toss/pay` → `loadTossPayments(clientKey)` → `payment.requestPayment({ method:'CARD', amount:{currency:'KRW', value}, orderId, orderName, successUrl, failUrl })`. SDK redirects to confirm route.
- `features/wallet/components/recharge-form-card.tsx` + `hooks/use-topup-info.ts` + `lib/payment.ts`: surface Toss as a payment method when `TossEnabled`.
- New settings UI in `web/default` following the existing payment-settings pattern for entering keys, test toggle, unit price, min top-up.
- Backend `GetTopUpInfo` extended to expose `toss_enabled` (and any non-secret display config) to the frontend.

---

## 5. Phase 2 — Billing-key Recurring Subscription

### 5.1 Billing-key issuance flow

1. Frontend `payment.requestBillingAuth({ method:'CARD', customerKey, successUrl, failUrl })`.
2. Browser redirected to `/api/toss/billing/confirm?authKey=...&customerKey=...`.
3. Server `POST /v1/billing/authorizations/issue` `{authKey, customerKey}` → `billingKey` (+ card metadata).
4. Store billing key **encrypted** (see §6), then immediately charge the first period and activate the subscription.

`customerKey` is a stable per-user identifier (e.g. `cust_<userId>_<random>`), generated once and reused.

### 5.2 New model — `model/toss_billing.go`

`UserBillingKey`:
- `Id`, `UserId` (index), `CustomerKey` (index), `BillingKey` (encrypted blob), `CardCompany`, `CardNumberMasked`, `Status` (`active`/`revoked`), `CreateTime`.

`UserSubscription` additions (migration, SQLite-safe `ADD COLUMN`):
- `AutoRenew bool`, `NextBillingTime int64` (index), `BillingKeyId int`.

### 5.3 Subscription controller — `controller/subscription_payment_toss.go` (new)

| Handler | Route | Responsibility |
|---|---|---|
| `SubscriptionRequestTossBilling` | `POST /api/subscription/toss/pay` | Validate plan + compliance. Create pending `SubscriptionOrder`. Return `{ clientKey, customerKey, successUrl, failUrl }` for `requestBillingAuth`. |
| `TossBillingConfirm` | `GET /api/toss/billing/confirm` | Issue + store billing key, charge first period via `POST /v1/billing/{billingKey}`, `CompleteSubscriptionOrder`, set `AutoRenew=true`, `NextBillingTime = end of period`. Redirect `/console/topup`. |
| `TossBillingFail` | `GET /api/toss/billing/fail` | Mark order failed, redirect. |
| (mgmt) | `POST /api/user/toss/billing/cancel` | Revoke auto-renew / billing key. |

### 5.4 Recurring charge cron — `service/toss_billing_task.go` (new)

Modeled on `service/subscription_reset_task.go`:
- Periodically select `UserSubscription` rows where `AutoRenew = true` AND `NextBillingTime <= now`, in batches.
- For each: charge `POST /v1/billing/{billingKey}` `{customerKey, amount, orderId, orderName}` with a fresh idempotent `orderId`.
- On success: extend subscription period, advance `NextBillingTime`, record a `SubscriptionOrder` + log.
- On failure: bounded retry; after N failures, suspend auto-renew and downgrade per existing `ExpireDueSubscriptions` semantics.
- All under order locks; idempotent on `orderId`.

### 5.5 Frontend (Phase 2)

- `features/wallet/components/subscription-plans-card.tsx`: Toss subscribe button calling `requestBillingAuth`.
- Billing-key / card management UI (view registered card, cancel auto-renew).

---

## 6. Error Handling, Security, Idempotency

- **Idempotency:** reuse `LockOrder`/`UnlockOrder` (refcounted mutex map) + DB row lock (`FOR UPDATE`). Confirm and webhook paths both no-op when an order is already `success`.
- **Amount tamper protection:** `TossConfirm` rejects unless the `amount` query param equals the stored charged KRW for that order before calling the confirm API.
- **Secret keys:** secret keys live server-side only. `GetTopUpInfo` and any settings read returned to the browser expose **only** the active client key and non-secret display config; secret keys are never serialized to API responses.
- **Billing key at rest:** stored encrypted using the project's existing crypto helpers in `common/`.
- **Test/live isolation:** every Toss API call and the client key handed to the frontend resolve through `TossActiveClientKey()` / `TossActiveSecretKey()`, branching on `TossTestMode`.
- **DB compatibility:** all new tables/columns use GORM abstractions and SQLite-safe `ADD COLUMN` migrations; JSON stored as `TEXT`; reserved-word columns use the existing `commonXxxCol` helpers if needed (Rule 2).
- **JSON:** all marshal/unmarshal via `common.*` wrappers (Rule 1).
- **Request DTOs:** optional scalars as pointers with `omitempty` where zero values must round-trip (Rule 6).

## 7. Testing

- Go unit tests:
  - `getTossPayMoney` KRW conversion incl. group ratio + amount discount.
  - `TossConfirm` validation (amount mismatch, provider mismatch, status guards).
  - `RechargeToss` idempotency (double-confirm + webhook race).
  - Phase 2: billing-key charge success/failure handling, `NextBillingTime` advancement.
- Follow the existing `setting/operation_setting/payment_setting_test.go` style.
- Frontend: extend wallet component tests where a Toss path is added.

## 8. Out of Scope (v1)

- Non-CARD billing methods (account-based 자동결제).
- Partial refunds / cancellation API (beyond marking orders failed).
- Multi-currency for Toss (KRW only).

## 9. Implementation Order

1. **Phase 1** — settings + persistence + model constants + `topup_toss.go` + `RechargeToss` + routes + frontend top-up + tests. Fully shippable.
2. **Phase 2** — `UserBillingKey` model + `UserSubscription` migration + `subscription_payment_toss.go` + `toss_billing_task.go` cron + frontend subscription + tests.
