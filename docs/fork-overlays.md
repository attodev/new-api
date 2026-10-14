# fork overlays — upstream 파일에 넣은 훅 목록

upstream(QuantumNous/new-api)과 동기화할 때 충돌이 나는 파일은 이 목록과 일치해야 한다.
규칙: upstream 파일에는 "훅 한 줄" 또는 "구조체 필드 + `// fork: O<n>` 주석"만 둔다. 로직은 fork 전용 파일에 둔다.

| overlay | upstream 파일 | 훅 | fork 전용 파일 |
|---|---|---|---|
| 공통(quota) | model/user.go:97-104 | Quota/UsedQuota/AffQuota/AffHistoryQuota 태그 type:bigint | model/user_quota_columns_fork_test.go |
| O5 문의 메일 | common/constants.go:113 | `var ContactEmail = ""` | controller/contact.go, controller/email.go, middleware/fork_access_token_routes.go |
| O5 문의 메일 | model/option.go:77 | InitOptionMap `OptionMap["ContactEmail"]` 한 줄 | controller/contact.go |
| O5 문의 메일 | model/option.go:488-489 | updateOptionMap `case "ContactEmail":` | controller/contact.go |
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
| O2 PayPal | model/option.go:537-548 | updateOptionMap `case "PayPal*":` 6개 | setting/payment_paypal.go |
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

O8 메모: 옵션 64-bit 파싱은 적용하지 않는다 (upstream의 Go `int`가 이미 64-bit이므로 no-op). 정적 자산 rate limit/캐시는 O1의 `landingStatic`이 담당하며 SPA 자산은 upstream 정책을 유지한다.

## 빌드·테스트 전제
- `web/dist/index.html`이 있어야 루트 패키지가 빌드된다 (`cd web && bun run build`).
- `make test`는 `GOWORK=off`로 루트·relaykit·tokenkit을 돈다.
