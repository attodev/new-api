# fork overlays — upstream 파일에 넣은 훅 목록

upstream(QuantumNous/new-api)과 동기화할 때 충돌이 나는 파일은 이 목록과 일치해야 한다.
규칙: upstream 파일에는 "훅 한 줄" 또는 "구조체 필드 + `// fork: O<n>` 주석"만 둔다. 로직은 fork 전용 파일에 둔다.

| overlay | upstream 파일 | 훅 | fork 전용 파일 |
|---|---|---|---|
| 공통(quota) | model/user.go:97-98,105-106 | Quota/UsedQuota/AffQuota/AffHistoryQuota 태그 type:bigint (gofmt가 이웃 줄 94-96, 99의 주석 정렬을 바꿈 — 공백만) | model/user_quota_columns_fork_test.go |
| O5 문의 메일 | common/constants.go:113 | `var ContactEmail = ""` | controller/contact.go, controller/email.go, middleware/fork_access_token_routes.go |
| O5 문의 메일 | model/option.go:77 | InitOptionMap `OptionMap["ContactEmail"]` 한 줄 | controller/contact.go |
| O5 문의 메일 | model/option.go:495-496 | updateOptionMap `case "ContactEmail":` | controller/contact.go |
| O5 문의 메일 | router/api-router.go:284 | `registerForkPublicRoutes(apiRouter)` | router/fork-router.go |
| O5 문의 메일 | router/api-router.go:96 | `registerForkSelfRoutes(selfRoute)` (O2가 채움) | router/fork-router.go |
| O5 문의 메일 | router/api-router.go:157 | `registerForkAdminUserRoutes(adminRoute)` (O4가 채움) | router/fork-router.go |
| O5 문의 메일 | router/access_token_scope_test.go | exempt 목록에 `"POST /api/contact"` 1줄 | router/fork-router.go |
| O4 사용자 Excel | router/api-router.go:157 | `registerForkAdminUserRoutes(adminRoute)` (위 O5 행과 동일 훅) | router/fork-router.go, controller/user_export.go, service/user_export.go, model/user_export.go |
| O4 사용자 Excel | middleware/access_token_routes.go (레지스트리) | 없음 — `GET /api/user/export`(user:read), `POST /api/user/import`(user:write)를 fork 레지스트리에 선언 | middleware/fork_access_token_routes.go |
| O2 PayPal | model/topup.go:26 | `TopUp.ProviderOrderId` 필드 + `// fork: O2` | model/topup_paypal.go, setting/payment_paypal.go |
| O2 PayPal | model/topup.go:35,45 | `PaymentMethodPayPal` / `PaymentProviderPayPal` 상수 | model/topup_paypal.go |
| O2 PayPal | model/topup.go:488 | `ManualCompleteTopUp` Stripe 조건에 `PaymentProviderPayPal` OR 추가 | model/topup_paypal.go |
| O2 PayPal | model/option.go:102-108 | InitOptionMap `PayPal*` OptionMap 6줄 | setting/payment_paypal.go |
| O2 PayPal | model/option.go:537-549 | updateOptionMap `case "PayPal*":` 6개 | setting/payment_paypal.go |
| O2 PayPal | controller/topup.go:78 | `payMethods = appendPayPalPayMethod(payMethods)` | controller/payment_availability_paypal.go |
| O2 PayPal | controller/topup.go:105 | data map `"enable_paypal_topup"` | controller/payment_availability_paypal.go |
| O2 PayPal | controller/topup.go:122 | data map `"paypal_min_topup"` | controller/payment_availability_paypal.go |
| O2 PayPal | router/access_token_scope_test.go | exempt 목록에 `"GET /api/paypal/capture"`, `"POST /api/paypal/webhook"` 2줄 | router/fork-router.go, controller/topup_paypal.go |
| O2 PayPal | middleware/access_token_routes.go (레지스트리) | 없음 — `POST /api/user/paypal/pay`(wallet:write), `POST /api/user/paypal/amount`(wallet:read)를 fork 레지스트리에 선언 | middleware/fork_access_token_routes.go |
| O1 랜딩·가이드 | router/web-router.go:20-22 | 빈 줄 + `WebAssets.LandingPublicFS` / `LandingTemplatesFS` 필드 (`fs.FS`, `io/fs` import) | router/landing-router.go, web/landing/{templates,public} |
| O1 랜딩·가이드 | router/web-router.go:39 | NoRoute 체인 `landingStatic(assets),` (AccessTokenAudit 다음, GlobalWebRateLimit 앞 — 정적 자산은 웹 제한 밖) | router/landing-router.go |
| O1 랜딩·가이드 | router/web-router.go:41 | NoRoute 체인 `landingPages(assets),` (GlobalWebRateLimit 다음, Cache 앞 — 페이지 4개 + 레거시 .html 301, 플러그인 디스패처 뒤) | router/landing-router.go |
| O1 랜딩·가이드 | main.go:50-54 | `//go:embed web/landing/public`, `//go:embed web/landing/templates` 변수 2개 | web/landing/{public,templates} |
| O1 랜딩·가이드 | main.go:214-215 | `router.WebAssets{...}`에 빈 줄 + `LandingPublicFS`, `LandingTemplatesFS` 필드 | router/landing-router.go |
| O1 랜딩·가이드 | web/public/favicon.ico, web/public/logo.png | 브랜드 파일 2개를 web/landing/public 것으로 교체 | web/landing/public/favicon.ico, web/landing/public/logo.png |
| O1 랜딩·가이드 | web/.oxlintrc.json:25 | `ignorePatterns`에 `"landing/**"` (`bun run lint`가 web/landing을 건너뜀) | web/landing/public/*.js |
| O1 랜딩·가이드 | web/.oxfmtrc.json:26 | `ignorePatterns`에 `"landing/**"` (`bun run format`이 web/landing을 건너뜀) | web/landing/public/*.{js,css} |
| O8 OIDC 도메인 | controller/oauth.go:314-317 | `handleOAuthLogin`에서 payload 디코딩 직후, `findOrCreateOAuthUser` 앞에 `enforceOIDCEmailDomain(provider, oauthUser)` 4줄 (사용자 행 생성 전에 거부) (`// fork: O8`) | controller/oauth_email_domain.go |
| O8 로그 라우팅 숨김 | model/log_other.go:259 | `formatLogOtherJSON` user 분기 끝에 `deleteLogOtherKeys(values, forkUserHiddenLogOtherKeys)` 한 줄 (`is_model_mapped`, `upstream_model_name`) | model/log_other_fork.go |
| O10 진단 도구 | .gitignore | `tools/unode-diagnostics/data/` 3줄 (`# fork: O10` 주석 + 경로, 진단 이력 디렉터리 무시) | tools/unode-diagnostics/ |
| O4 사용자 Excel | go.mod, go.sum | `github.com/xuri/excelize/v2 v2.10.1` direct require + 간접 의존 5개 (efp, nfp, mscfb, msoleps, go-deepcopy) | service/user_export.go |
| O4 사용자 Excel (프런트) | web/src/features/users/api.ts, types.ts | 내보내기/가져오기 API 함수 2개와 결과 타입 (파일 끝에 추가) | web/src/features/users/components/users-import-dialog.tsx |
| O4 사용자 Excel (프런트) | web/src/features/users/components/users-primary-buttons.tsx | 내보내기 버튼, 가져오기 버튼과 `UsersImportDialog` 마운트 (오류는 토스트로 표시) | web/src/features/users/components/users-import-dialog.tsx |
| O5 문의 메일 (프런트) | web/src/features/system-settings/integrations/email-settings-section.tsx | `ContactEmail` 수신자 입력과 SMTP 테스트 메일 전송 컨트롤 | — |
| O5 문의 메일 (프런트) | web/src/features/system-settings/types.ts, api.ts | `ContactEmail` 설정 키, 테스트 메일 API 함수 | — |
| O2 PayPal (프런트) | web/src/features/system-settings/integrations/payment-settings-section.tsx | PayPal 탭(설정 6개 키 폼) | — |
| O2 PayPal (프런트) | web/src/features/system-settings/types.ts, billing/index.tsx, billing/section-registry.tsx, operations/index.tsx, operations/section-registry.tsx | `PayPal*` 6개 설정 키 기본값·로딩 매핑(billing), `ContactEmail` 기본값·로딩 매핑(operations) | — |
| O2 PayPal (프런트) | web/src/features/wallet/types.ts, constants.ts, api.ts, lib/payment.ts | `enable_paypal_topup`/`paypal_min_topup` 타입, `PAYMENT_TYPES.PAYPAL`·색상, `calculatePayPalAmount`/`requestPayPalPayment`, `isPayPalPayment`, PayPal 최소 충전액 분기 | web/src/features/wallet/hooks/use-payment.ts |
| O2 PayPal (프런트) | web/src/features/wallet/hooks/use-payment.ts (+ use-payment.test.ts), components/recharge-form-card.tsx | `use-payment.ts`의 PayPal 분기(금액 계산기, 주문 생성, 승인 URL로 현재 창 이동), 충전 카드의 `enable_paypal_topup` 조건 한 줄 | — |
| O8 가격 페이지 모델 상세 숨김 | web/src/features/pricing/index.tsx, components/model-card.tsx, components/model-card-grid.tsx, components/pricing-table.tsx (+ `__tests__/model-cards.test.tsx`, `__tests__/task-price-display.test.tsx`) | 상세 드로어와 `onModelClick`/`Details` 버튼·행 클릭 제거 | — |
| O8 가격 페이지 모델 상세 숨김 | web/src/routes/pricing/$modelId/index.tsx | `beforeLoad`가 `/pricing`으로 리다이렉트, `ModelDetails` 컴포넌트 연결 제거 | — |
| O8 가격 페이지 모델 상세 숨김 | web/src/features/rankings/components/entity-links.tsx | `ModelLink`가 링크 대신 일반 텍스트 렌더링 | — |
| O8 큰 수 약어 | web/src/lib/currency.ts, web/src/lib/format.ts | `SUFFIX_TIERS` (k/M/B/T), `formatTokens` K/M/B/T 구간 (`web/src/lib/__tests__/number-suffix.test.ts`) | web/src/lib/__tests__/number-suffix.test.ts |
| O3 조직 | model/user.go:101-102 | `User.OrganizationId`, `User.OrganizationRole` 필드 (Group 다음) | model/organization.go, model/organization_migrate.go |
| O3 조직 | model/user.go:537 | `GetSelfUserById` Select 목록에 `"organization_id", "organization_role"` 한 줄 | model/organization.go |
| O3 조직 | model/topup.go:27-28 | `TopUp.TargetType`, `TopUp.TargetId` 필드 (gofmt가 26행 O2 주석 정렬을 바꿈 — 공백만) | model/organization.go |
| O3 조직 | model/task.go:112 | `TaskPrivateData.OrganizationId` 필드 | model/organization.go |
| O3 조직 | model/task.go:262 | `InitTask`에서 `privateData.OrganizationId = relayInfo.BillingOrganizationId` (`relayInfo != nil` 검사 안) | model/organization.go |
| O3 조직 | relay/common/relay_info.go:148 | `RelayInfo.BillingOrganizationId` 필드 (SubscriptionId 다음, 빈 줄로 gofmt 정렬 분리) | model/organization.go |
| O3 조직 | model/main.go:383 | `migrateDB` AutoMigrate 오류 검사 뒤 `MigrateForkOrganizationTables(DB)` | model/organization_migrate.go |
| O3 조직 | controller/user.go:512-513 | `buildSelfUserData`에 `"organization_id"`, `"organization_role"` 두 줄 | model/organization.go |
| O3 조직 | model/task_cas_test.go:63,97 | TestMain AutoMigrate에 조직 모델 4개, `truncateTables`에 `organizations` | model/organization_quota_reserve_test.go |
| O3 조직 | model/topup.go (Recharge/Stripe Recharge/ManualCompleteTopUp/RechargeCreem/RechargeWaffo/RechargeWaffoPancake) | `creditTopUpQuota`→`creditTopUpTarget(tx, topUp, …)`, `syncCreditUserQuotaCache`→`syncCreditTopUpCache(topUp, …)` 12줄(정산 6곳 × 2). `ManualCompleteTopUp`은 사후 sync가 `topUp`을 보도록 `var topUp *TopUp`을 클로저 밖으로 올림(선언 1줄 + 대입 1줄) | model/topup_target.go |
| O3 조직 | model/topup_paypal.go | `RechargePayPal`의 `creditTopUpTarget`/`syncCreditTopUpCache` 2줄 | model/topup_target.go |
| O3 조직 과금 | service/billing.go:17-18 | const 블록에 빈 줄 + `BillingSourceOrganizationSubscription = "organization_subscription"` (빈 줄로 gofmt 정렬 분리) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing.go:83 | `SettleBilling` 알림 분기 조건에 `\|\| isOrganizationSubscriptionSource(relayInfo.BillingSource)` (조직 플랜도 구독 잔여량 알림) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:80 | `Settle`의 `SubscriptionPostDelta` 조건에 `\|\| isOrganizationSubscriptionSource(s.funding.Source())` | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:122 | `Refund` goroutine, 구독 extraReserved 환급 블록 뒤 `refundOrganizationExtraReserved(funding, subscriptionId, extraReserved)` | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:151-153 | `needsRefundLocked`의 `return false` 앞 `if organizationFundingNeedsRefund(s.funding) { return true }` | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:246 | `preConsume` 오류 매핑 조건에 `\|\| isOrganizationSubscriptionPreConsumeError(errMsg)` ("no active organization subscription"을 403 구독 부족으로) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:294-295 | `reserveFunding` `default:` 앞 `case *OrganizationWalletFunding, *OrganizationSubscriptionFunding:` + `return reserveOrganizationFunding(s.funding, delta, requireAvailableQuota, s.lang)` (이미지 수량 예약은 회원 한도·조직 지갑을 원자적으로 예약, 부족하면 403) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:313-314 | `rollbackFundingReserve` 마지막 case 뒤 같은 case + `rollbackOrganizationFundingReserve(s.funding, delta)` | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:352-354 | `shouldTrust` `case BillingSourceWallet:` 첫 줄 `if _, ok := s.funding.(*OrganizationWalletFunding); ok { return false }` (조직 지갑은 신뢰 우회 없음) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:362-363 | `shouldTrust` `case BillingSourceOrganizationSubscription:` + `return false` | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:387 | `syncRelayInfo` 끝 `syncOrganizationRelayInfo(s.funding, info, s.extraReserved)` (BillingOrganizationId·조직 구독 필드) | service/billing_session_organization.go |
| O3 조직 과금 | service/billing_session.go:401-403 | `NewBillingSession` `pref := …` 다음 `newOrganizationBillingSession(...)` 3줄 (조직 회원은 개인 과금 선호를 쓰지 않음) | service/billing_session_organization.go |
| O3 조직 과금 | service/quota.go:443-447 | `postConsumeQuotaWithResult` 자금 분기 앞 `postConsumeOrganizationQuota` 4줄, 기존 `if relayInfo != nil && … BillingSourceSubscription {`가 `} else if …`로 바뀜 (gofmt가 `else`와 `if`를 한 줄로 합침) | service/billing_session_organization.go |
| O3 조직 과금 | service/quota.go:235, 375 | `UpdateUserUsedQuotaAndRequestCount` 뒤 `UpdateOrganizationUsedQuotaForUser(relayInfo.UserId, quota)` (2곳) | service/funding_source_organization.go |
| O3 조직 과금 | service/text_quota.go:449 | `UpdateUserUsedQuotaAndRequestCount` 뒤 `UpdateOrganizationUsedQuotaForUser(relayInfo.UserId, summary.Quota)` | service/funding_source_organization.go |
| O3 조직 과금 | service/violation_fee.go:131 | `UpdateUserUsedQuotaAndRequestCount` 뒤 `UpdateOrganizationUsedQuotaForUser(relayInfo.UserId, feeQuota)` | service/funding_source_organization.go |
| O3 조직 과금 | service/task_billing.go:92 | `LogTaskConsumption` `UpdateUserUsedQuotaAndRequestCount` 뒤 `UpdateOrganizationUsedQuotaForUser(info.UserId, info.PriceData.Quota)` | service/funding_source_organization.go |
| O3 조직 과금 | service/task_billing.go:118-120 | `taskAdjustFunding` 첫 줄 `if handled, err := taskAdjustOrganizationFunding(task, delta); handled { return err }` (제출 시 고정한 조직으로 정산·환급) | service/task_billing_organization.go, service/funding_source_organization.go |
| O3 조직 과금 | service/task_billing.go:284 | `RefundTaskQuota` `UpdateUserUsedQuota(task.UserId, -quota)` 뒤 `taskAdjustOrganizationUsedQuota(task, -quota)` | service/task_billing_organization.go |
| O3 조직 과금 | service/task_billing.go:352 | `RecalculateTaskQuota` `UpdateUserUsedQuota(task.UserId, quotaDelta)` 뒤 `taskAdjustOrganizationUsedQuota(task, quotaDelta)` | service/task_billing_organization.go |
| O3 조직 과금 | service/log_info_generate.go:196 | 구독 로그 블록 조건에 `\|\| isOrganizationSubscriptionSource(relayInfo.BillingSource)` | service/billing_session_organization.go |
| O3 조직 과금 | service/log_info_generate.go:199-201 | `subscription_id` 설정 뒤 조직 구독이면 `organization_user_subscription_id` 공개 필드 3줄 | service/billing_session_organization.go |
| O3 조직 과금 | service/midjourney.go:50 | `PrepareMidjourneyTaskBilling` 구독 거부 조건에 `\|\| isOrganizationSubscriptionSource(relayInfo.BillingSource)` | service/billing_session_organization.go |
| O3 조직 과금 | service/midjourney.go:53-55 | `PrepareMidjourneyTaskBilling` 구독 거부 뒤 `if err := prepareMidjourneyOrganizationBilling(relayInfo); err != nil { return false, err }` (조직 고정 → `postConsumeOrganizationQuota`가 회원 한도 + 조직 지갑 차감; 조직 플랜 회원은 거부) | service/midjourney_organization.go |
| O3 조직 과금 | service/midjourney.go:109 | `RefundMidjourneyQuota` 사용자 환급 성공 뒤 `refundMidjourneyOrganizationQuota(task.UserId, quota)` (조직 지갑·used_quota만 복원, 현재 소속 기준) | service/midjourney_organization.go |
| O3 조직 과금 | relay/mjproxy_handler.go:301, 684 | `UpdateUserUsedQuotaAndRequestCount` 뒤 `service.UpdateOrganizationUsedQuotaForUser(…UserId, midjourneyTask.Quota)` (2곳) | service/funding_source_organization.go |
| O3 조직 과금 | relay/mjproxy_handler.go:229-231 | `RelaySwapFace` 회원 한도 검사 뒤, 업스트림 호출 전 `if err := service.CheckMidjourneyOrganizationBilling(info.UserId); err != nil { return &dto.MidjourneyResponse{Code: 4, Description: err.Error()} }` (조직 플랜 회원·빈 조직 지갑 거부) | service/midjourney_organization.go |
| O3 조직 과금 | relay/mjproxy_handler.go:556-558 | `RelayMidjourneySubmit` 회원 한도 검사 뒤, 업스트림 호출 전 같은 가드 (`consumeQuota && err != nil`일 때만 거부 — upstream 한도 검사와 같은 조건) | service/midjourney_organization.go |
| O3 조직 과금 | service/task_billing_test.go:66-68, 90-93 | TestMain AutoMigrate 뒤 `model.MigrateForkOrganizationTables(db)` 3줄, `truncate`에 조직 테이블 4개 `DELETE` 4줄 | service/organization_billing_test.go, service/funding_source_organization_test.go |

O8 메모: 옵션 64-bit 파싱은 적용하지 않는다 (upstream의 Go `int`가 이미 64-bit이므로 no-op). 정적 자산 rate limit/캐시는 O1의 `landingStatic`이 담당하며 SPA 자산은 upstream 정책을 유지한다.

## 빌드·테스트 전제
- `web/dist/index.html`이 있어야 루트 패키지가 빌드된다 (`cd web && bun run build`).
- `make test`는 `GOWORK=off`로 루트·relaykit·tokenkit을 돈다.
