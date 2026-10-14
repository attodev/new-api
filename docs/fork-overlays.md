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

## 빌드·테스트 전제
- `web/dist/index.html`이 있어야 루트 패키지가 빌드된다 (`cd web && bun run build`).
- `make test`는 `GOWORK=off`로 루트·relaykit·tokenkit을 돈다.
