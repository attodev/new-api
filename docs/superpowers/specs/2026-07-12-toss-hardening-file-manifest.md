# Toss Payments 하드닝 변경 파일 완전 매핑

- 작성일: 2026-07-12
- 통합 기준 HEAD: 4171f875b
- 반영 브랜치: toss
- 범위: 위 공식 문서 감사에서 변경된 Toss 관련 파일 203개

## 1. 이 문서의 역할

Toss 하드닝은 원래 하나의 큰 working tree diff였다. 대형 파일 하나가 여러 안전성 목표를 포함하므로 C01–C18을 실제 파일 단위 커밋으로 완전히 분리하면 중간 상태가 컴파일되지 않을 위험이 있었다. 최종 반영은 backend 121개, Default 56개, Classic 25개, 문서 경계로 나눴다.

이 문서는 누락 검사를 위해 각 파일에 **하나의 primary logical commit**을 지정한다.

- primary는 그 파일을 처음 review할 대표 커밋이다.
- 파일 내부 hunk가 다른 커밋에도 속할 수 있다.
- 실제 stage 시에는 shared file을 git add -p로 나눠야 한다.
- 아래 203개 경로는 중복 없이 한 번씩만 나타난다.

이 문서 요청으로 새로 만든 commit index·detail·history·manifest 문서 6개는 203개 원본 변경 범위에 포함하지 않는다. 원래 감사 산출물인 공식 문서 감사 보고서는 C18d에 포함한다. 따라서 최종 Toss staging·commit 대상은 원본 203개와 신규 문서 6개를 합한 209개다.

## 2. 집계

| 영역 | 파일 수 |
|---|---:|
| Backend·runtime·backend tests | 121 |
| Default·Classic frontend와 원래 감사 문서 | 82 |
| 합계 | 203 |

프론트 분석에서는 운영 파일 4개를 포함해 85개가 확인됐다. 그중 .env.example, docker-compose.yml, main.go는 backend 121개에도 포함되므로 중복 3개를 제거한 전체 합계가 203개다.

## 3. Backend primary owner — 121개

### C01 — provider transport와 request boundary: 5개

~~~text
controller/topup_toss_ambiguous_response_test.go
controller/topup_toss_http_resilience_test.go
controller/toss_request_body.go
controller/toss_request_body_test.go
model/toss_provider_payload_test.go
~~~

### C02 — atomic configuration, encryption, repair: 12개

~~~text
controller/option.go
controller/option_toss_test.go
controller/payment_webhook_availability.go
controller/topup.go
controller/toss_fresh_checkout_gate_test.go
controller/toss_provider_post_operational_gate_test.go
model/option.go
model/option_toss_atomic_test.go
model/toss_option_secret.go
model/toss_option_secret_test.go
setting/payment_toss.go
setting/payment_toss_test.go
~~~

### C03 — MID fingerprint와 credential namespace: 7개

~~~text
controller/topup_toss_mid_test.go
controller/toss_billing_issue_rotation_test.go
model/toss_attempt_credential_pin_test.go
model/toss_billing_issue_rotation_test.go
model/toss_client_fingerprint.go
model/toss_topup_rolling_test.go
model/wallet_auto_recharge_issue_rotation_test.go
~~~

### C04 — immutable top-up quote와 quota snapshot: 3개

~~~text
controller/topup_toss_ui_contract_test.go
model/topup.go
model/topup_toss_test.go
~~~

### C05 — general confirm idempotency와 recovery: 4개

~~~text
controller/topup_toss.go
controller/topup_toss_test.go
controller/topup_toss_confirm_retry_test.go
controller/topup_toss_reconciliation_retry_test.go
~~~

### C06 — settlement와 account·organization lifecycle: 11개

~~~text
controller/organization.go
controller/organization_test.go
model/organization.go
model/organization_subscription.go
model/organization_toss_lifecycle_test.go
model/toss_settlement_cas_test.go
model/toss_target_lifecycle.go
model/toss_target_lifecycle_test.go
model/toss_topup_lifecycle_lock_test.go
model/user.go
service/org_user_export.go
~~~

### C07 — cancellation·refund·reconciliation event: 7개

~~~text
controller/topup_toss_cancellation_validation_test.go
controller/topup_toss_refund_test.go
controller/toss_payment_event_admin.go
controller/toss_payment_event_admin_test.go
controller/toss_subscription_cancel_webhook_test.go
model/toss_payment_event.go
model/toss_payment_event_test.go
~~~

### C08 — webhook source·deadline·route·rate limit: 8개

~~~text
controller/toss_webhook_security.go
controller/toss_webhook_security_test.go
middleware/rate-limit.go
middleware/rate_limit_test.go
model/toss_webhook_context_test.go
router/api-router.go
router/api_router_toss_rate_limit_test.go
router/api_router_toss_webhook_deadline_test.go
~~~

### C09 — Transaction API reconciliation: 5개

~~~text
controller/toss_transaction_reconciliation.go
controller/toss_transaction_reconciliation_resilience_test.go
controller/toss_transaction_reconciliation_test.go
model/toss_transaction_reconciliation.go
model/toss_transaction_reconciliation_test.go
~~~

### C10 — subscription snapshot·reservation·plan barrier: 7개

~~~text
controller/subscription.go
model/subscription.go
model/subscription_plan_snapshot.go
model/subscription_plan_snapshot_test.go
model/subscription_timing_bounds_test.go
model/toss_plan_mutation_barrier_test.go
model/toss_subscription_reservation_test.go
~~~

### C11 — billing ISSUE와 initial charge recovery: 10개

~~~text
controller/subscription_payment_toss.go
controller/subscription_payment_toss_test.go
controller/subscription_payment_toss_initial_recovery_test.go
controller/subscription_toss_issue_cleanup_claim_test.go
controller/toss_issue_authorization_expiry_test.go
controller/toss_issue_corrupt_snapshot_test.go
model/toss_billing.go
model/toss_billing_test.go
model/toss_billing_issue_cleanup_claim_test.go
model/toss_initial_charge_recovery_test.go
~~~

### C12 — billing-key revocation과 subscription cancel: 2개

~~~text
model/toss_shared_billing_lifecycle_test.go
model/toss_subscription_cancel_test.go
~~~

### C13 — renewal contract와 recurring protocol: 8개

~~~text
controller/toss_opaque_restore_route_test.go
model/toss_opaque_restore_guard_test.go
model/toss_recurring_migration_guard.go
model/toss_recurring_migration_guard_test.go
model/toss_recurring_order_id_protocol.go
model/toss_recurring_order_id_protocol_test.go
model/toss_renewal_contract_migration_test.go
model/toss_renewal_lifecycle_race_test.go
~~~

### C14 — wallet auto-recharge lifecycle: 9개

~~~text
controller/wallet_auto_recharge.go
controller/wallet_auto_recharge_preset_test.go
controller/wallet_auto_recharge_test.go
controller/wallet_toss_issue_cleanup_claim_test.go
model/wallet_auto_recharge.go
model/wallet_auto_recharge_issue_cleanup_claim_test.go
model/wallet_auto_recharge_preset.go
model/wallet_auto_recharge_preset_test.go
model/wallet_auto_recharge_test.go
~~~

### C15 — DB clock, bounded worker, cleanup fairness: 12개

~~~text
model/db_time.go
model/toss_maintenance_batch.go
model/toss_maintenance_batch_test.go
model/toss_pending_cleanup_test.go
service/payment_batch.go
service/payment_batch_test.go
service/toss_billing_task.go
service/toss_billing_task_test.go
service/toss_pending_cleanup_task.go
service/toss_pending_cleanup_task_test.go
service/wallet_auto_recharge_task.go
service/wallet_auto_recharge_task_test.go
~~~

### C18a — logging·redaction·GORM safety: 5개

~~~text
middleware/logger.go
middleware/logger_test.go
model/log.go
model/main.go
model/payment_log_redaction_test.go
~~~

### C18b — backend i18n: 3개

~~~text
i18n/keys.go
i18n/locales/en.yaml
i18n/locales/ko.yaml
~~~

### C18c — rollout·startup operations: 3개

~~~text
.env.example
docker-compose.yml
main.go
~~~

Backend 합계:

~~~text
5 + 12 + 7 + 3 + 4 + 11 + 7 + 8 + 5 + 7 + 10 + 2 + 8 + 9 + 12 + 5 + 3 + 3 = 121
~~~

## 4. Default·Classic frontend와 원래 감사 문서 — 82개

### C16a — safe browser session primitives: 13개

~~~text
web/default/src/features/wallet/api.ts
web/default/src/features/wallet/hooks/index.ts
web/default/src/features/wallet/hooks/use-payment.ts
web/default/src/features/wallet/hooks/use-toss-payment-lifecycle.ts
web/default/src/features/wallet/hooks/use-toss-payment.ts
web/default/src/features/wallet/lib/index.ts
web/default/src/features/wallet/lib/payment.toss.test.ts
web/default/src/features/wallet/lib/payment.ts
web/default/src/features/wallet/lib/topup-amount-mode.test.ts
web/default/src/features/wallet/lib/topup-amount-mode.ts
web/default/src/features/wallet/lib/toss-payment-lifecycle.test.ts
web/default/src/features/wallet/lib/toss-payment-lifecycle.ts
web/default/src/features/wallet/types.ts
~~~

### C16b — immutable subscription checkout terms: 12개

~~~text
web/default/src/features/subscriptions/api.ts
web/default/src/features/subscriptions/components/dialogs/subscription-purchase-dialog.tsx
web/default/src/features/subscriptions/hooks/use-toss-billing.ts
web/default/src/features/subscriptions/lib/index.ts
web/default/src/features/subscriptions/lib/toss-checkout.test.ts
web/default/src/features/subscriptions/lib/toss-checkout.ts
web/default/src/features/subscriptions/types.ts
web/default/src/features/wallet/components/cancel-all-toss-auto-renew.tsx
web/default/src/features/wallet/components/subscription-plans-card.test.js
web/default/src/features/wallet/components/subscription-plans-card.tsx
web/default/src/features/wallet/components/wallet-subscription-status-card.test.ts
web/default/src/features/wallet/components/wallet-subscription-status-card.tsx
~~~

### C16c — wallet preset terms와 auth session: 8개

~~~text
web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.test.ts
web/default/src/features/system-settings/integrations/wallet-auto-recharge-presets-section.tsx
web/default/src/features/wallet/components/auto-recharge-card.test.ts
web/default/src/features/wallet/components/auto-recharge-card.tsx
web/default/src/features/wallet/hooks/use-wallet-auto-recharge.ts
web/default/src/features/wallet/lib/auto-recharge-options.ts
web/default/src/features/wallet/lib/wallet-auto-recharge-session.test.ts
web/default/src/features/wallet/lib/wallet-auto-recharge-session.ts
~~~

### C16d — personal·organization wallet integration: 7개

~~~text
web/default/src/features/organizations/api.ts
web/default/src/features/organizations/components/organization-wallet.test.ts
web/default/src/features/organizations/components/organization-wallet.tsx
web/default/src/features/wallet/components/recharge-form-card.tsx
web/default/src/features/wallet/index.tsx
web/default/src/routes/_authenticated/organization/wallet.tsx
web/default/src/routes/_authenticated/wallet/index.tsx
~~~

### C16e — atomic repair·maintenance admin UI: 7개

~~~text
web/default/src/features/system-settings/api.ts
web/default/src/features/system-settings/billing/index.tsx
web/default/src/features/system-settings/billing/section-registry.tsx
web/default/src/features/system-settings/integrations/payment-settings-section.tsx
web/default/src/features/system-settings/integrations/toss-option-updates.test.ts
web/default/src/features/system-settings/integrations/toss-option-updates.ts
web/default/src/features/system-settings/types.ts
~~~

### C17 — Classic Toss parity: 16개

~~~text
web/classic/bun.lock
web/classic/package.json
web/classic/src/App.jsx
web/classic/src/components/topup/RechargeCard.jsx
web/classic/src/components/topup/SubscriptionPlansCard.jsx
web/classic/src/components/topup/WalletAutoRechargeCard.jsx
web/classic/src/components/topup/index.jsx
web/classic/src/components/topup/modals/SubscriptionPurchaseModal.jsx
web/classic/src/components/topup/tossPaymentLifecycle.js
web/classic/src/components/topup/tossPaymentLifecycle.test.js
web/classic/src/components/topup/tossSubscriptionCheckout.js
web/classic/src/components/topup/tossSubscriptionCheckout.test.js
web/classic/src/components/topup/tossTargetRouting.js
web/classic/src/components/topup/tossTargetRouting.test.js
web/classic/src/components/topup/tossWalletAutoRecharge.js
web/classic/src/components/topup/tossWalletAutoRecharge.test.js
~~~

### C18b — frontend locale와 report: 18개

~~~text
web/classic/src/i18n/locales/en.json
web/classic/src/i18n/locales/fr.json
web/classic/src/i18n/locales/ja.json
web/classic/src/i18n/locales/kr.json
web/classic/src/i18n/locales/ru.json
web/classic/src/i18n/locales/vi.json
web/classic/src/i18n/locales/zh-CN.json
web/classic/src/i18n/locales/zh-TW.json
web/classic/src/i18n/locales/zh.json
web/default/src/i18n/locales/_reports/_sync-report.json
web/default/src/i18n/locales/_reports/kr.untranslated.json
web/default/src/i18n/locales/en.json
web/default/src/i18n/locales/fr.json
web/default/src/i18n/locales/ja.json
web/default/src/i18n/locales/kr.json
web/default/src/i18n/locales/ru.json
web/default/src/i18n/locales/vi.json
web/default/src/i18n/locales/zh.json
~~~

### C18d — 원래 공식 문서 감사 보고서: 1개

~~~text
docs/superpowers/specs/2026-07-12-toss-payments-official-docs-audit-and-hardening.md
~~~

Frontend·원래 감사 문서 합계:

~~~text
13 + 12 + 8 + 7 + 7 + 16 + 18 + 1 = 82
~~~

## 5. shared file의 secondary commit

아래 파일은 primary 한 곳에 배정했지만 실제로는 여러 커밋 hunk를 포함한다.

| 파일 | Primary | Secondary touch |
|---|---|---|
| controller/topup_toss.go | C05 | C01, C03, C04, C06, C07, C08, C09 |
| controller/subscription_payment_toss.go | C11 | C01, C03, C07, C10, C12, C13 |
| controller/wallet_auto_recharge.go | C14 | C01, C02, C03, C07, C11, C12 |
| model/option.go | C02 | C03, C10, C13 |
| model/topup.go | C04 | C03, C05, C06, C07, C14, C15 |
| model/subscription.go | C10 | C06, C07, C11, C12, C13, C15 |
| model/toss_billing.go | C11 | C02, C03, C04, C05, C07, C10, C12, C13, C15 |
| model/wallet_auto_recharge.go | C14 | C03, C06, C07, C11, C12, C13, C15 |
| model/main.go | C18a | C01, C02, C03, C07, C09, C13, C18c |
| router/api-router.go | C08 | C02, C07, C09, C11, C14 |
| main.go | C18c | C02, C09, C13, C15 |
| setting/payment_toss.go | C02 | C03, C04, C13, C14 |
| payment-settings-section.tsx | C16e | C02, C14, C18b |
| web/default wallet/index.tsx | C16d | C16a, C16b, C16c |
| web/classic topup/index.jsx | C17 | C16a–C16d와 동일한 backend contract |

## 6. 실제 staging 시 주의

### 파일 전체 staging이 가능한 범위

대부분 신규 helper·test 파일은 primary commit에 파일 전체를 stage할 수 있다.

예:

- model/toss_client_fingerprint.go
- model/toss_payment_event.go
- controller/toss_webhook_security.go
- controller/toss_transaction_reconciliation.go
- model/subscription_plan_snapshot.go
- model/toss_recurring_order_id_protocol.go
- model/toss_maintenance_batch.go
- controller/toss_request_body.go
- Default·Classic 신규 lifecycle helper와 test

### 반드시 hunk staging이 필요한 범위

- controller/topup_toss.go
- controller/subscription_payment_toss.go
- controller/wallet_auto_recharge.go
- model/option.go
- model/topup.go
- model/subscription.go
- model/toss_billing.go
- model/wallet_auto_recharge.go
- model/main.go
- router/api-router.go
- main.go
- Default payment-settings-section.tsx
- Default organization-wallet.tsx
- Default wallet index.tsx
- Classic topup index.jsx
- locale JSON과 generated reports

### 중간 commit compile 보장

실제 split에서 가장 중요한 검증은 최종 작업 트리 테스트가 아니라 **각 중간 commit checkout에서 compile·test가 되는지**다.

권장:

1. 새 type·column·dual-reader를 먼저 추가한다.
2. migration을 추가한다.
3. runtime writer를 전환한다.
4. cleanup·backfill·activation gate를 추가한다.
5. UI를 연결한다.
6. old writer drain 문서를 마지막에 추가한다.

## 7. 범위에서 제외한 현재 작업 트리 파일

다음은 Toss 하드닝과 관계없는 별도 사용자 변경으로 분류했다.

~~~text
.aionrs/
.understand-anything/
outputs/
docs/images/organization-manual/
docs/superpowers/plans/2026-06-03-organization-billing-owner.md
docs/superpowers/plans/2026-06-03-quota-money-input.md
docs/superpowers/plans/2026-06-04-organization-usage-dashboard.md
docs/superpowers/plans/2026-06-07-organization-dashboard-charts.md
~~~

이 파일들은 Toss commit에 stage하거나 수정·삭제하면 안 된다.
