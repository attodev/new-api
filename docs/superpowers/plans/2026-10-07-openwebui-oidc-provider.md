# Open WebUI Built-in OIDC Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn new-api's existing broker-style OAuth2 provider into a standard OIDC issuer (discovery, authorize, token, userinfo, JWKS, end-session) so an unmodified Open WebUI can log users in, and remove the broker-only code.

**Architecture:** A new `service/oidc` package owns signing keys (RSA in the options table), authorization codes and access tokens (HybridCache), id_token signing/verification, claims and the discovery document. `controller/oauth2_provider.go` is rewritten as thin HTTP handlers over it. `/oauth2/authorize` and `/oauth2/logout` are SPA pages that call same-origin `POST /api/oauth2/authorize` / `POST /api/oauth2/logout`, which sidesteps the `SameSite=Strict` session cookie without weakening it. The Playground "Open in Chat App" button becomes a plain link to `oauth2.app_url`.

**Tech Stack:** Go 1.22 / Gin / GORM, `golang-jwt/jwt/v5` (RS256), `crypto/rsa`, `pkg/cachex.HybridCache`; React 19 + TanStack Router/Query in `web/default`, Bun.

Spec: `docs/superpowers/specs/2026-10-07-openwebui-oidc-provider-design.md`

## Global Constraints

- JSON: only `common.Marshal` / `common.Unmarshal` / `common.DecodeJson` (CLAUDE.md Rule 1); `encoding/json` types may be referenced, its functions may not be called in business code.
- DB: GORM only; everything must work on SQLite, MySQL ≥ 5.7.8, PostgreSQL ≥ 9.6. Signing keys live in the existing `options` table (no new table).
- Frontend tooling: `bun` only. `bun run lint` crashes on this machine (pre-existing); the frontend gate is `bunx tsc --noEmit -p tsconfig.app.json` with zero errors in touched files, `bunx prettier --check` on touched files, `bun run i18n:sync` reporting 0 missing `kr` keys.
- Never modify or remove any `new-api` / `QuantumNous` branding, license headers or attribution.
- Backend i18n: every new key in BOTH `i18n/locales/en.yaml` and `ko.yaml`. Frontend: every new `t('...')` string in BOTH `web/default/src/i18n/locales/en.json` (value = key) and `kr.json`.
- Issuer = `strings.TrimRight(system_setting.ServerAddress, "/")`. Only client = `oauth2.client_id`. `redirect_uri` and `post_logout_redirect_uri` must match the configured values character for character.
- Authorization code: 48 chars, TTL 120 s, single use, bound to `client_id`, `redirect_uri`, PKCE `S256`. access_token: opaque 48 chars, TTL 3600 s, userinfo only. id_token: RS256 with `kid`, `exp = iat + 3600`.
- Signing key option names are `OIDCSigningKeyCurrent` / `OIDCSigningKeyPrevious` (no dot → not routed into `config.GlobalConfig`; suffix `Key` → hidden by `controller.GetOptions`). Previous key is published in JWKS for 24 h after rotation, then deleted.
- Error/log hygiene: never log or return code, client_secret, code_verifier or access_token values. Token endpoint errors use RFC 6749 `{"error","error_description"}`.
- Session cookie `SameSite=Strict` stays unchanged.
- Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.

## File Structure

**Backend — new**

| File | Responsibility |
| --- | --- |
| `service/oidc/keys.go` | RSA signing key: load/create from options, kid, rotation, previous-key cleanup, JWKS document |
| `service/oidc/codes.go` | authorization code + access token stores (HybridCache), PKCE S256 check |
| `service/oidc/claims.go` | `*model.User` → userinfo / id_token claims (roles, groups incl. organization) |
| `service/oidc/idtoken.go` | id_token signing with `kid`; `id_token_hint` verification (current + previous key, expiry optional) |
| `service/oidc/discovery.go` | discovery document |
| `controller/oauth2_provider.go` | rewritten: discovery, jwks, authorize API, token, userinfo, logout API, rotate/status |
| `web/default/src/routes/oauth2/authorize.tsx`, `web/default/src/routes/oauth2/logout.tsx` | SPA pages |
| `web/default/src/features/oidc/api.ts` | `authorizeOidc`, `logoutOidc`, signing key API calls |

**Backend — modified / deleted**

| File | Change |
| --- | --- |
| `setting/system_setting/oauth2_client.go` | `AppUrl`, `PostLogoutRedirectURI` fields |
| `controller/option.go` | validation for `oauth2.*` keys |
| `controller/misc.go` | `oauth2_app_url` in `/api/status` |
| `controller/user.go` | drop `RevokeOAuth2Token` call in `Logout` |
| `service/billing_session.go`, `model/quota_reserve.go` | drop sentinel-token special case / comment |
| `router/oauth2-router.go`, `router/api-router.go` | new routes; `session-init` removed |
| `i18n/keys.go`, `i18n/locales/{en,ko}.yaml` | new keys |
| `model/oauth2_session.go`, `model/oauth2_session_test.go`, `controller/logout_oauth2_test.go`, `controller/oauth2_provider_test.go` | **deleted** (replaced by new tests) |

**Frontend — modified**

| File | Change |
| --- | --- |
| `features/playground/api.ts`, `features/playground/constants.ts`, `features/playground/index.tsx`, `routes/_authenticated/playground/index.tsx` | link to `oauth2_app_url`, no session-init |
| `features/auth/types.ts` | `oauth2_app_url` in status types |
| `features/system-settings/types.ts`, `site/index.tsx`, `site/section-registry.tsx`, `site/openwebui-integration-section.tsx` | new fields, rotate button, kid display |
| `i18n/locales/{en,kr}.json` | new strings |

---

### Task 1: Settings fields, option validation, status flag, i18n keys

**Files:**
- Modify: `setting/system_setting/oauth2_client.go`
- Modify: `setting/system_setting/oauth2_client_test.go` (append)
- Modify: `controller/option.go` (`UpdateOption`, second `switch option.Key`, after the `openwebui.jwt_issuer` case added by the relay plan)
- Create: `controller/option_oauth2_test.go`
- Modify: `controller/misc.go` (status map, next to `oauth2_replace_playground`)
- Modify: `i18n/keys.go`, `i18n/locales/en.yaml`, `i18n/locales/ko.yaml`

**Interfaces:**
- Produces: `system_setting.OAuth2Settings` gains `AppUrl string \`json:"app_url"\`` and `PostLogoutRedirectURI string \`json:"post_logout_redirect_uri"\``.
- Produces: `system_setting.IsAbsoluteHTTPURL(raw string) bool`.
- Produces i18n: `i18n.MsgOptionOAuth2ConfigMissing = "option.oauth2_config_missing"`, `i18n.MsgOptionOAuth2InvalidURL = "option.oauth2_invalid_url"`, `i18n.MsgOptionOAuth2SecretTooShort = "option.oauth2_secret_too_short"`, `i18n.MsgOIDCEmailRequired = "oidc.email_required"`, `i18n.MsgOIDCInvalidRequest = "oidc.invalid_request"`.
- Produces: `/api/status` field `oauth2_app_url` (string).

- [ ] **Step 1: Failing settings test**

Append to `setting/system_setting/oauth2_client_test.go`:

```go
func TestOAuth2Settings_HasAppUrlAndPostLogoutFields(t *testing.T) {
	original := *GetOAuth2Settings()
	t.Cleanup(func() { *GetOAuth2Settings() = original })

	cfg := config.GlobalConfig.Get("oauth2")
	require.NotNil(t, cfg)
	require.NoError(t, config.UpdateConfigFromMap(cfg, map[string]string{
		"app_url":                  "https://chat.example.com",
		"post_logout_redirect_uri": "https://chat.example.com/bye",
	}))
	require.Equal(t, "https://chat.example.com", GetOAuth2Settings().AppUrl)
	require.Equal(t, "https://chat.example.com/bye", GetOAuth2Settings().PostLogoutRedirectURI)
}

func TestIsAbsoluteHTTPURL(t *testing.T) {
	require.True(t, IsAbsoluteHTTPURL("https://chat.example.com/oauth/oidc/login/callback"))
	require.True(t, IsAbsoluteHTTPURL("http://localhost:8080"))
	require.False(t, IsAbsoluteHTTPURL(""))
	require.False(t, IsAbsoluteHTTPURL("chat.example.com"))
	require.False(t, IsAbsoluteHTTPURL("ftp://x"))
	require.False(t, IsAbsoluteHTTPURL("https://"))
}
```

If the file does not already import `config`, add `"github.com/QuantumNous/new-api/setting/config"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./setting/system_setting/ -run 'OAuth2Settings_HasAppUrl|IsAbsoluteHTTPURL' -count=1`
Expected: FAIL — unknown field `AppUrl`, undefined `IsAbsoluteHTTPURL`.

- [ ] **Step 3: Extend the settings struct**

In `setting/system_setting/oauth2_client.go` add the two fields after `RedirectURI` and the helper at the bottom:

```go
	// AppUrl is the external chat app's base URL (Open WebUI WEBUI_URL). The
	// Playground "Open in Chat App" link points here; the app itself starts
	// the OIDC login when opened.
	AppUrl string `json:"app_url"`
	// PostLogoutRedirectURI is the only post_logout_redirect_uri the
	// end-session endpoint will honour (Open WebUI WEBUI_AUTH_SIGNOUT_REDIRECT_URL).
	PostLogoutRedirectURI string `json:"post_logout_redirect_uri"`
```

```go
// IsAbsoluteHTTPURL reports whether raw is an absolute http(s) URL with a host.
func IsAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
```

Add imports `"net/url"` and `"strings"`.

- [ ] **Step 4: Run settings tests**

Run: `go test ./setting/system_setting/ -count=1`
Expected: PASS

- [ ] **Step 5: i18n keys**

Append to `i18n/keys.go`:

```go

// Built-in OIDC provider (Open WebUI login)
const (
	MsgOptionOAuth2ConfigMissing  = "option.oauth2_config_missing"
	MsgOptionOAuth2InvalidURL     = "option.oauth2_invalid_url"
	MsgOptionOAuth2SecretTooShort = "option.oauth2_secret_too_short"
	MsgOIDCEmailRequired          = "oidc.email_required"
	MsgOIDCInvalidRequest         = "oidc.invalid_request"
)
```

Append to `i18n/locales/en.yaml`:

```yaml

# Built-in OIDC provider (Open WebUI login)
option.oauth2_config_missing: "Cannot enable external app access, please set the client ID, a client secret of at least 16 characters and an absolute redirect URI first"
option.oauth2_invalid_url: "Must be an absolute http(s) URL"
option.oauth2_secret_too_short: "The client secret must be at least 16 characters"
oidc.email_required: "Register an email address in your profile before signing in to the external app"
oidc.invalid_request: "The sign-in request is not valid, please contact your administrator"
```

Append to `i18n/locales/ko.yaml`:

```yaml

# Built-in OIDC provider (Open WebUI login)
option.oauth2_config_missing: "외부 앱 접근을 활성화하려면 먼저 클라이언트 ID, 16자 이상의 클라이언트 시크릿, 절대 경로 Redirect URI를 설정해 주세요"
option.oauth2_invalid_url: "절대 경로 http(s) URL이어야 합니다"
option.oauth2_secret_too_short: "클라이언트 시크릿은 16자 이상이어야 합니다"
oidc.email_required: "외부 앱에 로그인하려면 먼저 프로필에서 이메일을 등록해 주세요"
oidc.invalid_request: "로그인 요청이 올바르지 않습니다. 관리자에게 문의해 주세요"
```

Run: `go test ./i18n/ -count=1` → PASS.

- [ ] **Step 6: Failing option-validation test**

`controller/option_oauth2_test.go`:

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOAuth2OptionTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db

	original := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		*system_setting.GetOAuth2Settings() = original
	})
}

func putOAuth2Option(t *testing.T, key string, value any) string {
	t.Helper()
	body, err := common.Marshal(map[string]any{"key": key, "value": value})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateOption(c)
	return rec.Body.String()
}

func TestUpdateOption_OAuth2_EnableRequiresConfig(t *testing.T) {
	setupOAuth2OptionTest(t)

	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":false`)
	require.False(t, system_setting.GetOAuth2Settings().Enabled)

	require.Contains(t, putOAuth2Option(t, "oauth2.client_id", "openwebui"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", "short"), `"success":false`)
	require.Contains(t, putOAuth2Option(t, "oauth2.client_secret", strings.Repeat("s", 16)), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":false`, "redirect_uri still missing")

	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "chat.example.com/cb"), `"success":false`, "not absolute")
	require.Contains(t, putOAuth2Option(t, "oauth2.redirect_uri", "https://chat.example.com/oauth/oidc/login/callback"), `"success":true`)
	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", true), `"success":true`)
	require.True(t, system_setting.GetOAuth2Settings().Enabled)

	require.Contains(t, putOAuth2Option(t, "oauth2.enabled", false), `"success":true`)
}

func TestUpdateOption_OAuth2_OptionalURLs(t *testing.T) {
	setupOAuth2OptionTest(t)
	for _, key := range []string{"oauth2.app_url", "oauth2.post_logout_redirect_uri"} {
		require.Contains(t, putOAuth2Option(t, key, ""), `"success":true`, key)
		require.Contains(t, putOAuth2Option(t, key, "not a url"), `"success":false`, key)
		require.Contains(t, putOAuth2Option(t, key, "https://chat.example.com"), `"success":true`, key)
	}
	require.Equal(t, "https://chat.example.com", system_setting.GetOAuth2Settings().AppUrl)
}
```

- [ ] **Step 7: Run to verify failure**

Run: `go test ./controller/ -run 'UpdateOption_OAuth2' -count=1`
Expected: FAIL — `oauth2.enabled` currently saves without checks.

- [ ] **Step 8: Add validation cases**

In `controller/option.go`, in the second `switch option.Key` (the one with `case "oidc.enabled":`), add after the `openwebui.jwt_issuer` case:

```go
	case "oauth2.enabled":
		if option.Value == "true" {
			s := system_setting.GetOAuth2Settings()
			if s.ClientId == "" || len(s.ClientSecret) < 16 || !system_setting.IsAbsoluteHTTPURL(s.RedirectURI) {
				common.ApiErrorI18n(c, i18n.MsgOptionOAuth2ConfigMissing)
				return
			}
		}
	case "oauth2.client_secret":
		if v := option.Value.(string); v != "" && len(v) < 16 {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuth2SecretTooShort)
			return
		}
	case "oauth2.redirect_uri", "oauth2.app_url", "oauth2.post_logout_redirect_uri":
		if v := strings.TrimSpace(option.Value.(string)); v != "" && !system_setting.IsAbsoluteHTTPURL(v) {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuth2InvalidURL)
			return
		}
```

- [ ] **Step 9: Status flag**

In `controller/misc.go`, after `"oauth2_replace_playground": system_setting.GetOAuth2Settings().ReplacePlayground,` add:

```go
		"oauth2_app_url":            system_setting.GetOAuth2Settings().AppUrl,
```

- [ ] **Step 10: Run and commit**

Run: `go test ./controller/ -run 'UpdateOption_OAuth2' -count=1 && go test ./setting/system_setting/ ./i18n/ -count=1 && go build ./...`
Expected: PASS

```bash
git add setting/system_setting/oauth2_client.go setting/system_setting/oauth2_client_test.go controller/option.go controller/option_oauth2_test.go controller/misc.go i18n/keys.go i18n/locales/en.yaml i18n/locales/ko.yaml
git commit -m "feat(oidc): add app_url and post-logout settings with option validation

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Signing keys (`service/oidc/keys.go`)

**Files:**
- Create: `service/oidc/keys.go`
- Create: `service/oidc/keys_test.go`

**Interfaces:**
- Consumes: `common.OptionMap` + `common.OptionMapRWMutex` (read), `model.UpdateOption(key, value string) error` (write), `common.Marshal/Unmarshal`, `common.GetTimestamp()`.
- Produces:
  - `const OptionSigningKeyCurrent = "OIDCSigningKeyCurrent"`, `const OptionSigningKeyPrevious = "OIDCSigningKeyPrevious"`, `const PreviousKeyRetention = 24 * time.Hour`
  - `type SigningKey struct { Kid string; Private *rsa.PrivateKey; CreatedAt int64 }`
  - `func CurrentKey() (*SigningKey, error)` — loads or creates
  - `func VerificationKeys() ([]*SigningKey, error)` — current + non-expired previous
  - `func RotateKey() (*SigningKey, error)`
  - `func JWKS() (map[string]any, error)` — `{"keys":[{kty,use,alg,kid,n,e}]}`
  - `func ResetKeyCacheForTest()`
  - `func ComputeKid(pub *rsa.PublicKey) string`

- [ ] **Step 1: Failing tests**

`service/oidc/keys_test.go`:

```go
package oidc

import (
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOIDCTestDB gives model.UpdateOption a real options table and an
// in-memory OptionMap, with Redis off so caches use memory.
func setupOIDCTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)
	ResetKeyCacheForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Organization{}))
	model.DB = db

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		ResetKeyCacheForTest()
	})
}

func TestCurrentKey_CreatesOnceAndPersists(t *testing.T) {
	setupOIDCTestDB(t)

	k1, err := CurrentKey()
	require.NoError(t, err)
	require.NotNil(t, k1.Private)
	require.Len(t, k1.Kid, 22, "16 bytes base64url without padding")
	require.NotZero(t, k1.CreatedAt)

	k2, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k1.Kid, k2.Kid)

	// A fresh process (cache reset) reads the same key back from the options table.
	ResetKeyCacheForTest()
	k3, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k1.Kid, k3.Kid)
	require.Equal(t, k1.Private.D, k3.Private.D)

	var count int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("`key` = ?", OptionSigningKeyCurrent).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestComputeKid_IsDeterministic(t *testing.T) {
	setupOIDCTestDB(t)
	k, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, k.Kid, ComputeKid(&k.Private.PublicKey))
	require.Equal(t, ComputeKid(&k.Private.PublicKey), ComputeKid(&k.Private.PublicKey))
}

func TestRotateKey_PublishesBothThenRetiresPrevious(t *testing.T) {
	setupOIDCTestDB(t)
	old, err := CurrentKey()
	require.NoError(t, err)

	rotated, err := RotateKey()
	require.NoError(t, err)
	require.NotEqual(t, old.Kid, rotated.Kid)

	cur, err := CurrentKey()
	require.NoError(t, err)
	require.Equal(t, rotated.Kid, cur.Kid)

	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, rotated.Kid, keys[0].Kid, "current first")
	require.Equal(t, old.Kid, keys[1].Kid)

	jwks, err := JWKS()
	require.NoError(t, err)
	list := jwks["keys"].([]map[string]any)
	require.Len(t, list, 2)
	require.Equal(t, "RSA", list[0]["kty"])
	require.Equal(t, "sig", list[0]["use"])
	require.Equal(t, "RS256", list[0]["alg"])
	require.Equal(t, rotated.Kid, list[0]["kid"])
	require.NotEmpty(t, list[0]["n"])
	require.Equal(t, "AQAB", list[0]["e"])

	// Age the previous key past retention: rewrite its stored created_at.
	stored := readStoredKey(t, OptionSigningKeyPrevious)
	stored.CreatedAt = common.GetTimestamp() - int64(PreviousKeyRetention.Seconds()) - 1
	stored.RotatedAt = common.GetTimestamp() - int64(PreviousKeyRetention.Seconds()) - 1
	writeStoredKey(t, OptionSigningKeyPrevious, stored)
	ResetKeyCacheForTest()

	keys, err = VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, rotated.Kid, keys[0].Kid)

	common.OptionMapRWMutex.RLock()
	_, stillThere := common.OptionMap[OptionSigningKeyPrevious]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, stillThere, "expired previous key is deleted from options")
}

func TestRotateKey_TwiceDropsOldest(t *testing.T) {
	setupOIDCTestDB(t)
	first, err := CurrentKey()
	require.NoError(t, err)
	second, err := RotateKey()
	require.NoError(t, err)
	third, err := RotateKey()
	require.NoError(t, err)

	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, third.Kid, keys[0].Kid)
	require.Equal(t, second.Kid, keys[1].Kid)
	for _, k := range keys {
		require.NotEqual(t, first.Kid, k.Kid)
	}
}

func readStoredKey(t *testing.T, option string) storedSigningKey {
	t.Helper()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[option]
	common.OptionMapRWMutex.RUnlock()
	require.NotEmpty(t, raw)
	var s storedSigningKey
	require.NoError(t, common.UnmarshalJsonStr(raw, &s))
	return s
}

func writeStoredKey(t *testing.T, option string, s storedSigningKey) {
	t.Helper()
	raw, err := common.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(option, string(raw)))
}

func TestVerificationKeys_NoPreviousKey(t *testing.T) {
	setupOIDCTestDB(t)
	cur, err := CurrentKey()
	require.NoError(t, err)
	keys, err := VerificationKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, cur.Kid, keys[0].Kid)
	_ = time.Second
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/oidc/ -count=1`
Expected: FAIL — no non-test files / undefined identifiers.

- [ ] **Step 3: Implement keys.go**

`service/oidc/keys.go`:

```go
// Package oidc implements new-api's built-in OpenID Connect provider used by
// Open WebUI: signing keys, authorization codes, access tokens, id_tokens,
// claims and the discovery document. HTTP handling lives in
// controller/oauth2_provider.go.
package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	// Option names deliberately contain no "." (so they are not routed into
	// config.GlobalConfig) and end in "Key" (so controller.GetOptions hides them).
	OptionSigningKeyCurrent  = "OIDCSigningKeyCurrent"
	OptionSigningKeyPrevious = "OIDCSigningKeyPrevious"

	// PreviousKeyRetention is how long a retired key stays in the JWKS so
	// id_tokens (1 h lifetime) and id_token_hints signed with it still verify.
	PreviousKeyRetention = 24 * time.Hour

	rsaBits = 2048
)

// SigningKey is a parsed RSA key with its stable identifier.
type SigningKey struct {
	Kid       string
	Private   *rsa.PrivateKey
	CreatedAt int64
}

// storedSigningKey is the JSON persisted in the options table.
type storedSigningKey struct {
	Kid        string `json:"kid"`
	PrivatePEM string `json:"private_pem"`
	CreatedAt  int64  `json:"created_at"`
	// RotatedAt is set when the key becomes the previous key; retention counts from here.
	RotatedAt int64 `json:"rotated_at,omitempty"`
}

var (
	keyMu       sync.Mutex
	cachedRaw   = map[string]string{} // option name -> raw JSON last parsed
	cachedKeys  = map[string]*SigningKey{}
	cachedStore = map[string]storedSigningKey{}
)

// ComputeKid derives a deterministic key id: base64url(SHA-256(PKIX DER)[:16]).
func ComputeKid(pub *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func readOption(name string) string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[name]
}

// loadKeyLocked parses the option into a SigningKey, reusing the cached parse
// when the stored JSON is unchanged. Returns (nil, nil) when the option is absent.
func loadKeyLocked(name string) (*SigningKey, storedSigningKey, error) {
	raw := readOption(name)
	if raw == "" {
		delete(cachedRaw, name)
		delete(cachedKeys, name)
		delete(cachedStore, name)
		return nil, storedSigningKey{}, nil
	}
	if cachedRaw[name] == raw {
		return cachedKeys[name], cachedStore[name], nil
	}
	var s storedSigningKey
	if err := common.UnmarshalJsonStr(raw, &s); err != nil {
		return nil, storedSigningKey{}, err
	}
	block, _ := pem.Decode([]byte(s.PrivatePEM))
	if block == nil {
		return nil, storedSigningKey{}, errors.New("oidc: signing key option holds no PEM block")
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, storedSigningKey{}, err
	}
	key := &SigningKey{Kid: s.Kid, Private: priv, CreatedAt: s.CreatedAt}
	cachedRaw[name] = raw
	cachedKeys[name] = key
	cachedStore[name] = s
	return key, s, nil
}

func newStoredKey() (storedSigningKey, *SigningKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return storedSigningKey{}, nil, err
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	now := common.GetTimestamp()
	s := storedSigningKey{Kid: ComputeKid(&priv.PublicKey), PrivatePEM: string(pemBytes), CreatedAt: now}
	return s, &SigningKey{Kid: s.Kid, Private: priv, CreatedAt: now}, nil
}

func saveStoredKey(name string, s storedSigningKey) error {
	raw, err := common.Marshal(s)
	if err != nil {
		return err
	}
	return model.UpdateOption(name, string(raw))
}

// CurrentKey returns the active signing key, generating and persisting one on
// first use. Safe for concurrent callers within a process; across nodes the
// options table is the source of truth and a later writer simply wins.
func CurrentKey() (*SigningKey, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	key, _, err := loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	if key != nil {
		return key, nil
	}
	s, key, err := newStoredKey()
	if err != nil {
		return nil, err
	}
	if err := saveStoredKey(OptionSigningKeyCurrent, s); err != nil {
		return nil, err
	}
	// Re-read so the cache reflects exactly what was stored (another node may have won).
	key, _, err = loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errors.New("oidc: signing key vanished after save")
	}
	return key, nil
}

// previousKeyLocked returns the previous key if it exists and is still within
// retention; an expired one is deleted from the options table.
func previousKeyLocked() (*SigningKey, error) {
	key, s, err := loadKeyLocked(OptionSigningKeyPrevious)
	if err != nil || key == nil {
		return nil, err
	}
	since := s.RotatedAt
	if since == 0 {
		since = s.CreatedAt
	}
	if common.GetTimestamp()-since > int64(PreviousKeyRetention.Seconds()) {
		if err := model.UpdateOption(OptionSigningKeyPrevious, ""); err != nil {
			return nil, err
		}
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, OptionSigningKeyPrevious)
		common.OptionMapRWMutex.Unlock()
		delete(cachedRaw, OptionSigningKeyPrevious)
		delete(cachedKeys, OptionSigningKeyPrevious)
		delete(cachedStore, OptionSigningKeyPrevious)
		return nil, nil
	}
	return key, nil
}

// VerificationKeys returns the keys a token may currently be signed with:
// the current key first, then the still-retained previous key.
func VerificationKeys() ([]*SigningKey, error) {
	cur, err := CurrentKey()
	if err != nil {
		return nil, err
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	prev, err := previousKeyLocked()
	if err != nil {
		return nil, err
	}
	keys := []*SigningKey{cur}
	if prev != nil && prev.Kid != cur.Kid {
		keys = append(keys, prev)
	}
	return keys, nil
}

// RotateKey demotes the current key to previous (stamping RotatedAt) and
// installs a freshly generated current key. Any older previous key is dropped.
func RotateKey() (*SigningKey, error) {
	if _, err := CurrentKey(); err != nil {
		return nil, err
	}
	keyMu.Lock()
	defer keyMu.Unlock()
	_, curStored, err := loadKeyLocked(OptionSigningKeyCurrent)
	if err != nil {
		return nil, err
	}
	curStored.RotatedAt = common.GetTimestamp()
	if err := saveStoredKey(OptionSigningKeyPrevious, curStored); err != nil {
		return nil, err
	}
	s, key, err := newStoredKey()
	if err != nil {
		return nil, err
	}
	if err := saveStoredKey(OptionSigningKeyCurrent, s); err != nil {
		return nil, err
	}
	if _, _, err := loadKeyLocked(OptionSigningKeyCurrent); err != nil {
		return nil, err
	}
	if _, _, err := loadKeyLocked(OptionSigningKeyPrevious); err != nil {
		return nil, err
	}
	return key, nil
}

// JWKS renders the verification keys as a JSON Web Key Set.
func JWKS() (map[string]any, error) {
	keys, err := VerificationKeys()
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		pub := &k.Private.PublicKey
		list = append(list, map[string]any{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": k.Kid,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}
	return map[string]any{"keys": list}, nil
}

// ResetKeyCacheForTest drops the in-process parse cache.
func ResetKeyCacheForTest() {
	keyMu.Lock()
	defer keyMu.Unlock()
	cachedRaw = map[string]string{}
	cachedKeys = map[string]*SigningKey{}
	cachedStore = map[string]storedSigningKey{}
}
```

`model.UpdateOption(name, "")` with an empty value: check `model/option.go` `UpdateOption` — it writes the row and `updateOptionMap` sets `OptionMap[name] = ""`; the explicit `delete` above then removes the empty entry so `readOption` returns "". If `UpdateOption` refuses empty values for some keys, that only applies to named keys in its switch, not to ours.

- [ ] **Step 4: Run tests**

Run: `go test ./service/oidc/ -count=1 -race`
Expected: PASS. RSA generation makes this suite take a few seconds; that is expected.

- [ ] **Step 5: Commit**

```bash
git add service/oidc/keys.go service/oidc/keys_test.go
git commit -m "feat(oidc): RSA signing keys stored in options with rotation and JWKS

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: Authorization codes, access tokens, PKCE (`service/oidc/codes.go`)

**Files:**
- Create: `service/oidc/codes.go`
- Create: `service/oidc/codes_test.go`

**Interfaces:**
- Consumes: `pkg/cachex.HybridCache[string]` (see `service/openwebui/cache.go` for the construction pattern), `common.GenerateKey()`, `common.Marshal/UnmarshalJsonStr`.
- Produces:
  - `const CodeTTL = 120 * time.Second`, `const AccessTokenTTL = 3600 * time.Second`
  - `type AuthCode struct { UserId int; ClientId, RedirectURI, Nonce, Scope, CodeChallenge, CodeChallengeMethod string; AuthTime int64 }`
  - `func IssueCode(ac AuthCode) (string, error)`
  - `func ConsumeCode(code, clientId, redirectURI, codeVerifier string) (*AuthCode, error)` — single use; returns `ErrInvalidGrant` on any mismatch
  - `type AccessToken struct { UserId int; ClientId, Scope string }`
  - `func IssueAccessToken(at AccessToken) (string, error)`, `func LookupAccessToken(raw string) (*AccessToken, bool, error)`, `func RevokeAccessToken(raw string)`
  - `var ErrInvalidGrant = errors.New("oidc: invalid_grant")`
  - `func VerifyPKCE(challenge, method, verifier string) bool`
  - `func ResetStoresForTest()`

- [ ] **Step 1: Failing tests**

`service/oidc/codes_test.go`:

```go
package oidc

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyPKCE(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	require.True(t, VerifyPKCE(s256(verifier), "S256", verifier))
	require.False(t, VerifyPKCE(s256(verifier), "S256", verifier+"x"))
	require.False(t, VerifyPKCE(s256(verifier), "plain", verifier))
	require.False(t, VerifyPKCE(s256(verifier), "S256", ""))
	require.True(t, VerifyPKCE("", "", "anything"), "no challenge stored → nothing to check")
}

func TestIssueAndConsumeCode_SingleUseAndBound(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	verifier := "a-very-long-code-verifier-string-0123456789"
	code, err := IssueCode(AuthCode{
		UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb",
		Nonce: "n-1", Scope: "openid email profile",
		CodeChallenge: s256(verifier), CodeChallengeMethod: "S256", AuthTime: 1700000000,
	})
	require.NoError(t, err)
	require.Len(t, code, 48)

	// wrong client
	_, err = ConsumeCode(code, "other", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
	// wrong redirect
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb2", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
	// wrong verifier
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "nope")
	require.ErrorIs(t, err, ErrInvalidGrant)
	// missing verifier when a challenge was stored
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "")
	require.ErrorIs(t, err, ErrInvalidGrant)

	// A failed attempt must not have burned the code: the right call succeeds.
	ac, err := ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.NoError(t, err)
	require.Equal(t, 7, ac.UserId)
	require.Equal(t, "n-1", ac.Nonce)
	require.Equal(t, int64(1700000000), ac.AuthTime)

	// Second use fails.
	_, err = ConsumeCode(code, "openwebui", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)

	// Unknown code fails.
	_, err = ConsumeCode("does-not-exist", "openwebui", "https://chat.example.com/cb", verifier)
	require.ErrorIs(t, err, ErrInvalidGrant)
}

func TestConsumeCode_WithoutPKCEIgnoresVerifier(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	code, err := IssueCode(AuthCode{UserId: 7, ClientId: "openwebui", RedirectURI: "https://chat.example.com/cb", Scope: "openid"})
	require.NoError(t, err)
	ac, err := ConsumeCode(code, "openwebui", "https://chat.example.com/cb", "stray-verifier")
	require.NoError(t, err)
	require.Equal(t, 7, ac.UserId)
}

func TestAccessTokenStore(t *testing.T) {
	setupOIDCTestDB(t)
	ResetStoresForTest()
	raw, err := IssueAccessToken(AccessToken{UserId: 7, ClientId: "openwebui", Scope: "openid email profile"})
	require.NoError(t, err)
	require.Len(t, raw, 48)

	at, found, err := LookupAccessToken(raw)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 7, at.UserId)

	_, found, err = LookupAccessToken("unknown")
	require.NoError(t, err)
	require.False(t, found)

	RevokeAccessToken(raw)
	_, found, err = LookupAccessToken(raw)
	require.NoError(t, err)
	require.False(t, found)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/oidc/ -run 'PKCE|Code|AccessToken' -count=1`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement codes.go**

`service/oidc/codes.go`:

```go
package oidc

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/samber/hot"
)

const (
	CodeTTL        = 120 * time.Second
	AccessTokenTTL = 3600 * time.Second

	codeNamespace   = "oauth2:code:v2"
	accessNamespace = "oauth2:access:v1"
	storeCapacity   = 10000
)

// ErrInvalidGrant covers every code-exchange failure (unknown, expired,
// reused, wrong client, wrong redirect_uri, PKCE mismatch). The token
// endpoint maps it to RFC 6749 "invalid_grant" without saying which.
var ErrInvalidGrant = errors.New("oidc: invalid_grant")

// AuthCode is what an authorization code stands for until it is exchanged.
type AuthCode struct {
	UserId              int    `json:"user_id"`
	ClientId            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Nonce               string `json:"nonce,omitempty"`
	Scope               string `json:"scope"`
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
	AuthTime            int64  `json:"auth_time"`
}

// AccessToken is the opaque userinfo-only bearer token's backing record.
type AccessToken struct {
	UserId   int    `json:"user_id"`
	ClientId string `json:"client_id"`
	Scope    string `json:"scope"`
}

var (
	storeMu     sync.Mutex
	codeStore   *cachex.HybridCache[string]
	accessStore *cachex.HybridCache[string]
)

func redisOn() bool { return common.RedisEnabled && common.RDB != nil }

func newStore(ns string, ttl time.Duration) *cachex.HybridCache[string] {
	return cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
		Namespace:    cachex.Namespace(ns),
		Redis:        common.RDB,
		RedisEnabled: redisOn,
		RedisCodec:   cachex.StringCodec{},
		Memory: func() *hot.HotCache[string, string] {
			return hot.NewHotCache[string, string](hot.LRU, storeCapacity).WithTTL(ttl).WithJanitor().Build()
		},
	})
}

func codes() *cachex.HybridCache[string] {
	storeMu.Lock()
	defer storeMu.Unlock()
	if codeStore == nil {
		codeStore = newStore(codeNamespace, CodeTTL)
	}
	return codeStore
}

func accessTokens() *cachex.HybridCache[string] {
	storeMu.Lock()
	defer storeMu.Unlock()
	if accessStore == nil {
		accessStore = newStore(accessNamespace, AccessTokenTTL)
	}
	return accessStore
}

// ResetStoresForTest discards both stores.
func ResetStoresForTest() {
	storeMu.Lock()
	defer storeMu.Unlock()
	codeStore = nil
	accessStore = nil
}

// VerifyPKCE checks base64url(SHA256(verifier)) == challenge for method S256.
// An empty stored challenge means the client did not use PKCE → always true.
func VerifyPKCE(challenge, method, verifier string) bool {
	if challenge == "" {
		return true
	}
	if method != "S256" || verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// IssueCode stores ac under a fresh single-use code.
func IssueCode(ac AuthCode) (string, error) {
	code, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(ac)
	if err != nil {
		return "", err
	}
	if err := codes().SetWithTTL(code, string(raw), CodeTTL); err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeCode validates the binding and deletes the code only on success, so
// an attacker guessing the client_secret or verifier cannot burn a victim's
// code; the code still expires after CodeTTL.
func ConsumeCode(code, clientId, redirectURI, codeVerifier string) (*AuthCode, error) {
	if code == "" {
		return nil, ErrInvalidGrant
	}
	raw, found, err := codes().Get(code)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrInvalidGrant
	}
	var ac AuthCode
	if err := common.UnmarshalJsonStr(raw, &ac); err != nil {
		return nil, ErrInvalidGrant
	}
	if subtle.ConstantTimeCompare([]byte(ac.ClientId), []byte(clientId)) != 1 ||
		ac.RedirectURI != redirectURI ||
		!VerifyPKCE(ac.CodeChallenge, ac.CodeChallengeMethod, codeVerifier) {
		return nil, ErrInvalidGrant
	}
	if _, err := codes().DeleteMany([]string{code}); err != nil {
		return nil, err
	}
	return &ac, nil
}

func IssueAccessToken(at AccessToken) (string, error) {
	token, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	raw, err := common.Marshal(at)
	if err != nil {
		return "", err
	}
	if err := accessTokens().SetWithTTL(token, string(raw), AccessTokenTTL); err != nil {
		return "", err
	}
	return token, nil
}

func LookupAccessToken(raw string) (*AccessToken, bool, error) {
	if raw == "" {
		return nil, false, nil
	}
	stored, found, err := accessTokens().Get(raw)
	if err != nil || !found {
		return nil, false, err
	}
	var at AccessToken
	if err := common.UnmarshalJsonStr(stored, &at); err != nil {
		return nil, false, nil
	}
	return &at, true, nil
}

func RevokeAccessToken(raw string) {
	if raw == "" {
		return
	}
	_, _ = accessTokens().DeleteMany([]string{raw})
}
```

Note on the "delete only on success" choice: with Redis this is a GET followed by a DEL, not atomic. Two racing exchanges of the same code could both succeed within milliseconds. Open WebUI exchanges each code exactly once and the code expires in 120 s, so the window is accepted; mention it in the spec note of Task 9.

- [ ] **Step 4: Run tests**

Run: `go test ./service/oidc/ -count=1 -race`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add service/oidc/codes.go service/oidc/codes_test.go
git commit -m "feat(oidc): authorization code and access token stores with PKCE S256

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Claims, id_token, discovery (`service/oidc/claims.go`, `idtoken.go`, `discovery.go`)

**Files:**
- Create: `service/oidc/claims.go`, `service/oidc/idtoken.go`, `service/oidc/discovery.go`
- Create: `service/oidc/claims_test.go`, `service/oidc/idtoken_test.go`, `service/oidc/discovery_test.go`

**Interfaces:**
- Consumes: `model.User` (fields `Id, Username, DisplayName, Email, Role, Group, OrganizationId`), `model.Organization{Name}` via a small lookup `model.DB.Select("name").Where("id = ?", id).First(&org)`; `common.RoleAdminUser`; `CurrentKey()`, `VerificationKeys()` (Task 2); `system_setting.ServerAddress`.
- Produces:
  - `func Issuer() string`
  - `type Claims struct { Sub, Email, Name, PreferredUsername string; EmailVerified bool; Roles, Groups []string }`
  - `func BuildClaims(user *model.User, orgName string) Claims`
  - `func LookupOrganizationName(orgId int) string` (empty when 0 or missing)
  - `func (c Claims) UserInfo() map[string]any` — all seven claims
  - `func SignIDToken(c Claims, clientId, nonce string, authTime int64, now time.Time) (string, error)`
  - `func VerifyIDTokenHint(raw, clientId string) (*IDTokenClaims, error)` — verifies signature with any verification key, `iss`, `aud`; **ignores expiry**
  - `type IDTokenClaims struct { jwt.RegisteredClaims; Email string; Nonce string; ... }`
  - `func DiscoveryDocument() map[string]any`

- [ ] **Step 1: Failing tests**

`service/oidc/claims_test.go`:

```go
package oidc

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestBuildClaims_UserAndAdminGroupsAndNames(t *testing.T) {
	user := &model.User{Id: 7, Username: "alice", DisplayName: "Alice Kim", Email: "Alice@Example.com", Role: common.RoleCommonUser, Group: "default"}
	c := BuildClaims(user, "")
	require.Equal(t, "7", c.Sub)
	require.Equal(t, "alice@example.com", c.Email)
	require.True(t, c.EmailVerified)
	require.Equal(t, "Alice Kim", c.Name)
	require.Equal(t, "alice", c.PreferredUsername)
	require.Equal(t, []string{"user"}, c.Roles)
	require.Equal(t, []string{"default"}, c.Groups)

	admin := &model.User{Id: 8, Username: "bob", Role: common.RoleAdminUser, Group: "vip", Email: "bob@example.com", OrganizationId: 3}
	c = BuildClaims(admin, "Acme")
	require.Equal(t, "bob", c.Name, "display name falls back to username")
	require.Equal(t, []string{"admin"}, c.Roles)
	require.Equal(t, []string{"vip", "Acme"}, c.Groups)

	root := &model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Group: "default", Email: "root@example.com"}
	require.Equal(t, []string{"admin"}, BuildClaims(root, "").Roles)
}

func TestClaims_UserInfoMap(t *testing.T) {
	c := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Role: common.RoleCommonUser, Group: "default"}, "")
	m := c.UserInfo()
	require.Equal(t, "7", m["sub"])
	require.Equal(t, "a@example.com", m["email"])
	require.Equal(t, true, m["email_verified"])
	require.Equal(t, "alice", m["name"])
	require.Equal(t, "alice", m["preferred_username"])
	require.Equal(t, []string{"user"}, m["roles"])
	require.Equal(t, []string{"default"}, m["groups"])
}

func TestLookupOrganizationName(t *testing.T) {
	setupOIDCTestDB(t)
	require.NoError(t, model.DB.Create(&model.Organization{Id: 3, Name: "Acme", OwnerUserId: 1}).Error)
	require.Equal(t, "Acme", LookupOrganizationName(3))
	require.Equal(t, "", LookupOrganizationName(0))
	require.Equal(t, "", LookupOrganizationName(99))
}
```

`service/oidc/idtoken_test.go`:

```go
package oidc

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func withIssuer(t *testing.T, addr string) {
	t.Helper()
	original := system_setting.ServerAddress
	system_setting.ServerAddress = addr
	t.Cleanup(func() { system_setting.ServerAddress = original })
}

func TestIssuer_TrimsTrailingSlash(t *testing.T) {
	withIssuer(t, "https://newapi.example.com/")
	require.Equal(t, "https://newapi.example.com", Issuer())
}

func TestSignIDToken_ClaimsAndKid(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", DisplayName: "Alice", Email: "a@example.com", Role: common.RoleCommonUser, Group: "default"}, "")
	now := time.Unix(1700000000, 0)

	raw, err := SignIDToken(claims, "openwebui", "nonce-1", 1699999000, now)
	require.NoError(t, err)

	key, err := CurrentKey()
	require.NoError(t, err)
	parsed, err := jwt.ParseWithClaims(raw, &IDTokenClaims{}, func(tok *jwt.Token) (any, error) {
		require.Equal(t, "RS256", tok.Method.Alg())
		require.Equal(t, key.Kid, tok.Header["kid"])
		return &key.Private.PublicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithoutClaimsValidation())
	require.NoError(t, err)
	got := parsed.Claims.(*IDTokenClaims)
	require.Equal(t, "https://newapi.example.com", got.Issuer)
	require.Equal(t, "7", got.Subject)
	require.Equal(t, jwt.ClaimStrings{"openwebui"}, got.Audience)
	require.Equal(t, now.Unix(), got.IssuedAt.Unix())
	require.Equal(t, now.Add(AccessTokenTTL).Unix(), got.ExpiresAt.Unix())
	require.Equal(t, int64(1699999000), got.AuthTime)
	require.Equal(t, "nonce-1", got.Nonce)
	require.Equal(t, "a@example.com", got.Email)
	require.True(t, got.EmailVerified)
	require.Equal(t, "Alice", got.Name)
}

func TestSignIDToken_OmitsEmptyNonce(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Group: "default"}, "")
	raw, err := SignIDToken(claims, "openwebui", "", time.Now().Unix(), time.Now())
	require.NoError(t, err)
	got, err := VerifyIDTokenHint(raw, "openwebui")
	require.NoError(t, err)
	require.Equal(t, "", got.Nonce)
}

func TestVerifyIDTokenHint_AcceptsExpiredAndPreviousKey_RejectsForeign(t *testing.T) {
	setupOIDCTestDB(t)
	withIssuer(t, "https://newapi.example.com")
	claims := BuildClaims(&model.User{Id: 7, Username: "alice", Email: "a@example.com", Group: "default"}, "")

	expired, err := SignIDToken(claims, "openwebui", "n", time.Now().Add(-3*time.Hour).Unix(), time.Now().Add(-3*time.Hour))
	require.NoError(t, err)
	got, err := VerifyIDTokenHint(expired, "openwebui")
	require.NoError(t, err, "expiry is ignored for id_token_hint")
	require.Equal(t, "7", got.Subject)

	// Rotate: the old token must still verify through the previous key.
	_, err = RotateKey()
	require.NoError(t, err)
	_, err = VerifyIDTokenHint(expired, "openwebui")
	require.NoError(t, err)

	// Wrong audience / issuer are rejected.
	_, err = VerifyIDTokenHint(expired, "someone-else")
	require.Error(t, err)
	withIssuer(t, "https://other.example.com")
	_, err = VerifyIDTokenHint(expired, "openwebui")
	require.Error(t, err)

	// A token signed by a key we never had is rejected.
	foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": "https://newapi.example.com", "aud": "openwebui", "sub": "7"}).SignedString([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	_, err = VerifyIDTokenHint(foreign, "openwebui")
	require.Error(t, err)
	_, err = VerifyIDTokenHint("garbage", "openwebui")
	require.Error(t, err)
}
```

`service/oidc/discovery_test.go`:

```go
package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiscoveryDocument(t *testing.T) {
	withIssuer(t, "https://newapi.example.com/")
	d := DiscoveryDocument()
	require.Equal(t, "https://newapi.example.com", d["issuer"])
	require.Equal(t, "https://newapi.example.com/oauth2/authorize", d["authorization_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/token", d["token_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/userinfo", d["userinfo_endpoint"])
	require.Equal(t, "https://newapi.example.com/oauth2/jwks", d["jwks_uri"])
	require.Equal(t, "https://newapi.example.com/oauth2/logout", d["end_session_endpoint"])
	require.Equal(t, []string{"code"}, d["response_types_supported"])
	require.Equal(t, []string{"public"}, d["subject_types_supported"])
	require.Equal(t, []string{"RS256"}, d["id_token_signing_alg_values_supported"])
	require.Equal(t, []string{"S256"}, d["code_challenge_methods_supported"])
	require.Equal(t, []string{"client_secret_basic", "client_secret_post"}, d["token_endpoint_auth_methods_supported"])
	require.Equal(t, []string{"authorization_code"}, d["grant_types_supported"])
	require.Equal(t, []string{"openid", "email", "profile"}, d["scopes_supported"])
	require.Equal(t, []string{"sub", "email", "email_verified", "name", "preferred_username", "roles", "groups"}, d["claims_supported"])
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/oidc/ -run 'Claims|Issuer|IDToken|Discovery|Organization' -count=1`
Expected: FAIL — undefined identifiers.

- [ ] **Step 3: Implement claims.go**

```go
package oidc

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Claims are the user attributes exposed to the OIDC client. Open WebUI reads
// them under its default OAUTH_*_CLAIM names, so no client-side mapping is needed.
type Claims struct {
	Sub               string
	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
	Roles             []string
	Groups            []string
}

// BuildClaims maps a new-api user to OIDC claims. orgName is the user's
// organization name ("" when none); callers resolve it with LookupOrganizationName.
func BuildClaims(user *model.User, orgName string) Claims {
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = user.Username
	}
	role := "user"
	if user.Role >= common.RoleAdminUser {
		role = "admin"
	}
	groups := []string{user.Group}
	if orgName != "" {
		groups = append(groups, orgName)
	}
	return Claims{
		Sub:               strconv.Itoa(user.Id),
		Email:             strings.ToLower(strings.TrimSpace(user.Email)),
		EmailVerified:     true,
		Name:              name,
		PreferredUsername: user.Username,
		Roles:             []string{role},
		Groups:            groups,
	}
}

// LookupOrganizationName returns the organization's name or "" when the user
// has none or it cannot be found.
func LookupOrganizationName(orgId int) string {
	if orgId <= 0 {
		return ""
	}
	var org model.Organization
	if err := model.DB.Select("name").Where("id = ?", orgId).First(&org).Error; err != nil {
		return ""
	}
	return org.Name
}

// UserInfo renders the full claim set for the userinfo endpoint.
func (c Claims) UserInfo() map[string]any {
	return map[string]any{
		"sub":                c.Sub,
		"email":              c.Email,
		"email_verified":     c.EmailVerified,
		"name":               c.Name,
		"preferred_username": c.PreferredUsername,
		"roles":              c.Roles,
		"groups":             c.Groups,
	}
}
```

- [ ] **Step 4: Implement idtoken.go**

```go
package oidc

import (
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is the OIDC issuer: the configured server address without a trailing slash.
func Issuer() string {
	return strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
}

// IDTokenClaims is the id_token payload. roles/groups are deliberately left to
// userinfo to keep the token small; email+name are included so Open WebUI can
// skip the userinfo call when it wants to.
type IDTokenClaims struct {
	jwt.RegisteredClaims
	AuthTime      int64  `json:"auth_time"`
	Nonce         string `json:"nonce,omitempty"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

var ErrIDTokenInvalid = errors.New("oidc: id_token invalid")

// SignIDToken issues an RS256 id_token for clientId, valid for AccessTokenTTL.
func SignIDToken(c Claims, clientId, nonce string, authTime int64, now time.Time) (string, error) {
	key, err := CurrentKey()
	if err != nil {
		return "", err
	}
	claims := IDTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer(),
			Subject:   c.Sub,
			Audience:  jwt.ClaimStrings{clientId},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
		AuthTime:      authTime,
		Nonce:         nonce,
		Email:         c.Email,
		EmailVerified: c.EmailVerified,
		Name:          c.Name,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = key.Kid
	return token.SignedString(key.Private)
}

// VerifyIDTokenHint checks that raw was signed by one of our verification keys
// for clientId and our issuer. Expiry is deliberately NOT enforced: end-session
// receives id_tokens that may be hours old.
func VerifyIDTokenHint(raw, clientId string) (*IDTokenClaims, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrIDTokenInvalid
	}
	keys, err := VerificationKeys()
	if err != nil {
		return nil, err
	}
	claims := &IDTokenClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(Issuer()),
		jwt.WithAudience(clientId),
		jwt.WithoutClaimsValidation(),
	)
	_, err = parser.ParseWithClaims(raw, claims, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		for _, k := range keys {
			if kid == "" || k.Kid == kid {
				return &k.Private.PublicKey, nil
			}
		}
		return nil, ErrIDTokenInvalid
	})
	if err != nil {
		return nil, ErrIDTokenInvalid
	}
	// WithoutClaimsValidation disables iss/aud checks too, so enforce them here.
	if claims.Issuer != Issuer() {
		return nil, ErrIDTokenInvalid
	}
	audOK := false
	for _, a := range claims.Audience {
		if a == clientId {
			audOK = true
		}
	}
	if !audOK {
		return nil, ErrIDTokenInvalid
	}
	return claims, nil
}
```

- [ ] **Step 5: Implement discovery.go**

```go
package oidc

// DiscoveryDocument renders /.well-known/openid-configuration.
func DiscoveryDocument() map[string]any {
	iss := Issuer()
	return map[string]any{
		"issuer":                                iss,
		"authorization_endpoint":                iss + "/oauth2/authorize",
		"token_endpoint":                        iss + "/oauth2/token",
		"userinfo_endpoint":                     iss + "/oauth2/userinfo",
		"jwks_uri":                              iss + "/oauth2/jwks",
		"end_session_endpoint":                  iss + "/oauth2/logout",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"grant_types_supported":                 []string{"authorization_code"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
		"claims_supported":                      []string{"sub", "email", "email_verified", "name", "preferred_username", "roles", "groups"},
	}
}
```

- [ ] **Step 6: Run all package tests**

Run: `go test ./service/oidc/ -count=1 -race && go vet ./service/oidc/`
Expected: PASS. If `TestVerifyIDTokenHint_...` fails at the "wrong issuer" step because `WithIssuer` under `WithoutClaimsValidation` behaves differently than expected, the explicit post-parse checks still enforce it — verify the explicit checks are present rather than changing the test.

- [ ] **Step 7: Commit**

```bash
git add service/oidc/claims.go service/oidc/idtoken.go service/oidc/discovery.go service/oidc/claims_test.go service/oidc/idtoken_test.go service/oidc/discovery_test.go
git commit -m "feat(oidc): claims, RS256 id_token signing and discovery document

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: Rewrite the provider controller (discovery, jwks, authorize, token, userinfo), routes, and remove the broker code

**Files:**
- Rewrite: `controller/oauth2_provider.go` (replace the whole file)
- Create: `controller/oauth2_provider_test.go` (replace the whole file)
- Delete: `model/oauth2_session.go`, `model/oauth2_session_test.go`, `controller/logout_oauth2_test.go`
- Modify: `controller/user.go` (`Logout`), `service/billing_session.go` (sentinel special case), `model/quota_reserve.go` (comment only), `router/oauth2-router.go`, `router/api-router.go`

**Interfaces:**
- Consumes: everything exported from `service/oidc` (Tasks 2–4), `system_setting.GetOAuth2Settings()`, `middleware.UserAuth()`, i18n keys from Task 1.
- Produces handlers: `OAuth2Discovery`, `OAuth2JWKS`, `OAuth2Authorize`, `OAuth2Token`, `OAuth2UserInfo`; helper `oauth2CredentialsConfigured(*system_setting.OAuth2Settings) bool` (kept).
- Produces routes: `GET /.well-known/openid-configuration`, `GET /oauth2/jwks`, `POST /oauth2/token`, `GET /oauth2/userinfo`, `POST /api/oauth2/authorize` (UserAuth).
- API contract of `POST /api/oauth2/authorize` (JSON body = the query params): success `{"success":true,"data":{"redirect_url":"<redirect_uri>?code=…&state=…"}}`; redirectable error `{"success":false,"error":"<code>","data":{"redirect_url":"<redirect_uri>?error=<code>&state=…"}}`; non-redirectable error `{"success":false,"error":"<code>","message":"<i18n text>"}`. Error codes: `invalid_client`, `invalid_redirect_uri`, `email_required` (non-redirectable); `unsupported_response_type`, `invalid_scope`, `invalid_request`, `access_denied` (redirectable).

- [ ] **Step 1: Delete the broker code and fix its call sites**

```bash
git rm -q model/oauth2_session.go model/oauth2_session_test.go controller/logout_oauth2_test.go
```

In `controller/user.go` `Logout`, remove the block:

```go
	if userId, ok := session.Get("id").(int); ok {
		if err := model.RevokeOAuth2Token(userId); err != nil {
			common.SysError("failed to revoke OAuth2 token on logout for user " + strconv.Itoa(userId) + ": " + err.Error())
		}
	}
```

so the function begins `session := sessions.Default(c)` then `session.Clear()`. Remove the `strconv` import only if nothing else in the file uses it (run `go build ./controller/` to find out).

In `service/billing_session.go` replace

```go
	// The OAuth2 token is Redis-only and carries UnlimitedQuota, so there is no
	// row to update and no quota to move -- the same reason the playground's
	// synthetic token is skipped. Attempting it fails with "record not found"
	// and turns a settlement that already succeeded into an error return.
	syntheticToken := s.relayInfo.IsPlayground ||
		model.IsOAuth2SentinelTokenId(s.relayInfo.TokenId)
```

with

```go
	// The playground's synthetic token has no row to update and no quota to
	// move; attempting it would fail with "record not found" and turn a
	// settlement that already succeeded into an error return.
	syntheticToken := s.relayInfo.IsPlayground
```

In `model/quota_reserve.go`, rewrite the comment paragraph that mentions `IssueOAuth2Token` so it no longer references it (keep the behaviour description):

```go
// The unconditional EXPIRE this replaces was right for what the token cache
// normally holds: a short-lived snapshot of a database row, re-readable at any
// time, whose TTL exists to bound staleness. It was wrong for cache entries
// that have no row behind them and whose TTL IS their lifetime; refreshing
// such an entry down to 60 seconds ended it a minute after the last request.
```

- [ ] **Step 2: Write the failing controller tests**

`controller/oauth2_provider_test.go` (full replacement):

```go
package controller

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	oidcTestClientId     = "openwebui"
	oidcTestClientSecret = "0123456789abcdef"
	oidcTestRedirect     = "https://chat.example.com/oauth/oidc/login/callback"
	oidcTestIssuer       = "https://newapi.example.com"
)

type oidcTestEnv struct {
	engine *gin.Engine
}

func setupOIDCProviderTest(t *testing.T) *oidcTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	originalOptionMap := common.OptionMap
	originalAddr := system_setting.ServerAddress
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.OptionMap = make(map[string]string)
	system_setting.ServerAddress = oidcTestIssuer
	oidc.ResetKeyCacheForTest()
	oidc.ResetStoresForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Organization{}, &model.Option{}))
	model.DB = db
	require.NoError(t, db.Create(&model.Organization{Id: 3, Name: "Acme", OwnerUserId: 7}).Error)
	// users.aff_code has a unique index, so every fixture user needs its own.
	for _, u := range []model.User{
		{Id: 7, Username: "alice", Password: "secret-pass", DisplayName: "Alice", Email: "Alice@Example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default", OrganizationId: 3},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleAdminUser, Group: "vip"},
		{Id: 9, Username: "carol", Password: "secret-pass", Email: "carol@example.com", AffCode: "aff-carol", Status: common.UserStatusDisabled, Role: common.RoleCommonUser, Group: "default"},
	} {
		require.NoError(t, db.Create(&u).Error)
	}

	originalSettings := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{
		Enabled: true, ClientId: oidcTestClientId, ClientSecret: oidcTestClientSecret,
		RedirectURI: oidcTestRedirect, AppUrl: "https://chat.example.com",
		PostLogoutRedirectURI: "https://chat.example.com/bye",
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		system_setting.ServerAddress = originalAddr
		*system_setting.GetOAuth2Settings() = originalSettings
		oidc.ResetKeyCacheForTest()
		oidc.ResetStoresForTest()
	})

	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("test-secret"))))
	// test-only login: puts a user into the session exactly like setupLogin does
	engine.POST("/test/login/:id", func(c *gin.Context) {
		var user model.User
		require.NoError(t, model.DB.First(&user, "id = ?", c.Param("id")).Error)
		s := sessions.Default(c)
		s.Set("id", user.Id)
		s.Set("username", user.Username)
		s.Set("role", user.Role)
		s.Set("status", user.Status)
		s.Set("group", user.Group)
		require.NoError(t, s.Save())
		c.Status(http.StatusOK)
	})
	engine.GET("/.well-known/openid-configuration", OAuth2Discovery)
	engine.GET("/oauth2/jwks", OAuth2JWKS)
	engine.POST("/oauth2/token", OAuth2Token)
	engine.GET("/oauth2/userinfo", OAuth2UserInfo)
	authorize := engine.Group("/api/oauth2")
	authorize.Use(middleware.UserAuth())
	authorize.POST("/authorize", OAuth2Authorize)
	return &oidcTestEnv{engine: engine}
}

// login returns the session cookie for userId.
func (e *oidcTestEnv) login(t *testing.T, userId string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/test/login/"+userId, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "session" {
			return ck
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func (e *oidcTestEnv) authorize(t *testing.T, cookie *http.Cookie, userId string, params map[string]string) (int, map[string]any) {
	t.Helper()
	body, err := common.Marshal(params)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/authorize", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
		req.Header.Set("New-Api-User", userId)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	if rec.Body.Len() > 0 {
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	}
	return rec.Code, res
}

func baseAuthorizeParams(verifierChallenge string) map[string]string {
	p := map[string]string{
		"response_type": "code", "client_id": oidcTestClientId, "redirect_uri": oidcTestRedirect,
		"scope": "openid email profile", "state": "st-1", "nonce": "n-1",
	}
	if verifierChallenge != "" {
		p["code_challenge"] = verifierChallenge
		p["code_challenge_method"] = "S256"
	}
	return p
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func redirectURL(t *testing.T, res map[string]any) *url.URL {
	t.Helper()
	data, _ := res["data"].(map[string]any)
	require.NotNil(t, data, "expected data.redirect_url in %v", res)
	u, err := url.Parse(data["redirect_url"].(string))
	require.NoError(t, err)
	return u
}

func (e *oidcTestEnv) token(t *testing.T, form url.Values, basic bool) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		req.SetBasicAuth(oidcTestClientId, oidcTestClientSecret)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func (e *oidcTestEnv) userinfo(t *testing.T, bearer string) (int, map[string]any, http.Header) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res), rec.Body.String())
	return rec.Code, res, rec.Header()
}

func TestOIDC_DiscoveryAndJWKS(t *testing.T) {
	env := setupOIDCProviderTest(t)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &doc))
	require.Equal(t, oidcTestIssuer, doc["issuer"])
	require.Equal(t, oidcTestIssuer+"/oauth2/logout", doc["end_session_endpoint"])

	rec = httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth2/jwks", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var jwks map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &jwks))
	require.Len(t, jwks["keys"].([]any), 1)

	system_setting.GetOAuth2Settings().Enabled = false
	rec = httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOIDC_Authorize_RequiresLogin(t *testing.T) {
	env := setupOIDCProviderTest(t)
	code, _ := env.authorize(t, nil, "", baseAuthorizeParams(""))
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestOIDC_FullCodeFlow_WithPKCEAndBasicAuth(t *testing.T) {
	env := setupOIDCProviderTest(t)
	ck := env.login(t, "7")
	verifier := "a-very-long-code-verifier-string-0123456789"

	status, res := env.authorize(t, ck, "7", baseAuthorizeParams(pkceChallenge(verifier)))
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, res["success"], res)
	u := redirectURL(t, res)
	require.Equal(t, "https://chat.example.com/oauth/oidc/login/callback", u.Scheme+"://"+u.Host+u.Path)
	code := u.Query().Get("code")
	require.Len(t, code, 48)
	require.Equal(t, "st-1", u.Query().Get("state"))

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {verifier}}
	status, tok, hdr := env.token(t, form, true)
	require.Equal(t, http.StatusOK, status, tok)
	require.Equal(t, "Bearer", tok["token_type"])
	require.Equal(t, float64(3600), tok["expires_in"])
	require.Equal(t, "openid email profile", tok["scope"])
	require.Equal(t, "no-store", hdr.Get("Cache-Control"))
	access := tok["access_token"].(string)
	require.Len(t, access, 48)

	// id_token verifies with the published key and carries the nonce.
	key, err := oidc.CurrentKey()
	require.NoError(t, err)
	parsed, err := jwt.ParseWithClaims(tok["id_token"].(string), &oidc.IDTokenClaims{}, func(*jwt.Token) (any, error) { return &key.Private.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(oidcTestIssuer), jwt.WithAudience(oidcTestClientId))
	require.NoError(t, err)
	idc := parsed.Claims.(*oidc.IDTokenClaims)
	require.Equal(t, "7", idc.Subject)
	require.Equal(t, "n-1", idc.Nonce)
	require.Equal(t, "alice@example.com", idc.Email)
	require.Equal(t, "Alice", idc.Name)

	// userinfo
	status, info, _ := env.userinfo(t, access)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "7", info["sub"])
	require.Equal(t, "alice@example.com", info["email"])
	require.Equal(t, []any{"user"}, info["roles"])
	require.Equal(t, []any{"default", "Acme"}, info["groups"])

	// code reuse fails
	status, res2, _ := env.token(t, form, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res2["error"])
}

func TestOIDC_Token_ClientSecretPostAndFailures(t *testing.T) {
	env := setupOIDCProviderTest(t)
	ck := env.login(t, "7")
	verifier := "another-very-long-code-verifier-string-0123456789"

	mint := func() string {
		_, res := env.authorize(t, ck, "7", baseAuthorizeParams(pkceChallenge(verifier)))
		return redirectURL(t, res).Query().Get("code")
	}

	// client_secret_post works
	code := mint()
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {verifier}, "client_id": {oidcTestClientId}, "client_secret": {oidcTestClientSecret}}
	status, _, _ := env.token(t, form, false)
	require.Equal(t, http.StatusOK, status)

	// wrong secret → 401 invalid_client with challenge header
	code = mint()
	form.Set("code", code)
	form.Set("client_secret", "wrong-secret-wrong")
	status, res, hdr := env.token(t, form, false)
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "invalid_client", res["error"])
	require.Contains(t, hdr.Get("WWW-Authenticate"), "Basic")
	// ...and the code was not burned by the failed attempt
	form.Set("client_secret", oidcTestClientSecret)
	status, _, _ = env.token(t, form, false)
	require.Equal(t, http.StatusOK, status)

	// wrong verifier
	code = mint()
	bad := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {oidcTestRedirect}, "code_verifier": {"nope"}}
	status, res, _ = env.token(t, bad, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res["error"])

	// wrong redirect_uri
	bad.Set("code_verifier", verifier)
	bad.Set("redirect_uri", "https://evil.example.com/cb")
	status, res, _ = env.token(t, bad, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "invalid_grant", res["error"])

	// unsupported grant type
	status, res, _ = env.token(t, url.Values{"grant_type": {"client_credentials"}}, true)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "unsupported_grant_type", res["error"])

	// disabled → 404
	system_setting.GetOAuth2Settings().Enabled = false
	status, _, _ = env.token(t, form, true)
	require.Equal(t, http.StatusNotFound, status)
}

func TestOIDC_Authorize_Rejections(t *testing.T) {
	env := setupOIDCProviderTest(t)
	ck := env.login(t, "7")

	p := baseAuthorizeParams("")
	p["redirect_uri"] = "https://evil.example.com/cb"
	_, res := env.authorize(t, ck, "7", p)
	require.Equal(t, false, res["success"])
	require.Equal(t, "invalid_redirect_uri", res["error"])
	_, hasData := res["data"]
	require.False(t, hasData, "must not hand the browser to an unregistered redirect_uri")
	require.NotEmpty(t, res["message"])

	p = baseAuthorizeParams("")
	p["client_id"] = "other"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_client", res["error"])

	p = baseAuthorizeParams("")
	p["scope"] = "email profile"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_scope", res["error"])
	require.Equal(t, "invalid_scope", redirectURL(t, res).Query().Get("error"))
	require.Equal(t, "st-1", redirectURL(t, res).Query().Get("state"))

	p = baseAuthorizeParams("")
	p["response_type"] = "token"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "unsupported_response_type", redirectURL(t, res).Query().Get("error"))

	p = baseAuthorizeParams("x")
	p["code_challenge_method"] = "plain"
	_, res = env.authorize(t, ck, "7", p)
	require.Equal(t, "invalid_request", redirectURL(t, res).Query().Get("error"))

	// no email → non-redirectable email_required
	ck8 := env.login(t, "8")
	_, res = env.authorize(t, ck8, "8", baseAuthorizeParams(""))
	require.Equal(t, "email_required", res["error"])
	_, hasData = res["data"]
	require.False(t, hasData)
	require.Equal(t, i18n.Translate(i18n.LangKo, i18n.MsgOIDCEmailRequired), res["message"])

	// disabled user → access_denied redirect
	ck9 := env.login(t, "9")
	_, res = env.authorize(t, ck9, "9", baseAuthorizeParams(""))
	require.Equal(t, "access_denied", redirectURL(t, res).Query().Get("error"))
}

func TestOIDC_UserInfo_Failures(t *testing.T) {
	env := setupOIDCProviderTest(t)
	status, res, hdr := env.userinfo(t, "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, "invalid_token", res["error"])
	require.Contains(t, hdr.Get("WWW-Authenticate"), "Bearer")

	status, _, _ = env.userinfo(t, "unknown-token")
	require.Equal(t, http.StatusUnauthorized, status)

	// token for a user who is disabled afterwards
	access, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: 7, ClientId: oidcTestClientId, Scope: "openid"})
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 7).Update("status", common.UserStatusDisabled).Error)
	status, _, _ = env.userinfo(t, access)
	require.Equal(t, http.StatusUnauthorized, status)
	_, found, _ := oidc.LookupAccessToken(access)
	require.False(t, found, "token revoked once the user is found disabled")
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go build ./... ; go test ./controller/ -run 'OIDC_' -count=1`
Expected: build FAILS (`model.StoreAuthCode` etc. gone, `OAuth2Discovery` undefined) — that is the RED state.

- [ ] **Step 4: Rewrite the controller**

`controller/oauth2_provider.go` (full replacement):

```go
package controller

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// oauth2CredentialsConfigured reports whether the provider is on and has a
// usable client registration. From a caller's perspective an unconfigured
// provider must look identical to a disabled one.
func oauth2CredentialsConfigured(settings *system_setting.OAuth2Settings) bool {
	return settings.Enabled && settings.ClientId != "" && settings.ClientSecret != ""
}

func oauth2NotEnabled(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
}

// OAuth2Discovery serves /.well-known/openid-configuration.
func OAuth2Discovery(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	c.JSON(http.StatusOK, oidc.DiscoveryDocument())
}

// OAuth2JWKS serves the signing public keys.
func OAuth2JWKS(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	jwks, err := oidc.JWKS()
	if err != nil {
		logger.LogError(c, "oidc: jwks: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, jwks)
}

type oauth2AuthorizeRequest struct {
	ResponseType        string `json:"response_type"`
	ClientId            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	Scope               string `json:"scope"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
}

func appendQuery(base string, values url.Values) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + values.Encode()
}

// OAuth2Authorize is the same-origin API the SPA page /oauth2/authorize calls.
// It runs behind UserAuth, so an unauthenticated browser gets 401 and the page
// sends it to /sign-in. No consent screen: the only client is first-party.
func OAuth2Authorize(c *gin.Context) {
	var req oauth2AuthorizeRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "invalid_request", "message": common.TranslateMessage(c, i18n.MsgOIDCInvalidRequest)})
		return
	}
	settings := system_setting.GetOAuth2Settings()
	ip := c.ClientIP()

	hardReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected ("+code+") client_id="+req.ClientId+" ip="+ip)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": code, "message": common.TranslateMessage(c, i18n.MsgOIDCInvalidRequest)})
	}
	redirectReject := func(code string) {
		logger.LogWarn(c, "oidc: authorize rejected ("+code+") client_id="+req.ClientId+" ip="+ip)
		q := url.Values{"error": {code}}
		if req.State != "" {
			q.Set("state", req.State)
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "error": code, "data": gin.H{"redirect_url": appendQuery(req.RedirectURI, q)}})
	}

	if !oauth2CredentialsConfigured(settings) || req.ClientId != settings.ClientId {
		hardReject("invalid_client")
		return
	}
	if req.RedirectURI == "" || req.RedirectURI != settings.RedirectURI {
		hardReject("invalid_redirect_uri")
		return
	}
	if req.ResponseType != "code" {
		redirectReject("unsupported_response_type")
		return
	}
	if !common.StringsContains(strings.Fields(req.Scope), "openid") {
		redirectReject("invalid_scope")
		return
	}
	if req.CodeChallenge != "" && req.CodeChallengeMethod != "S256" {
		redirectReject("invalid_request")
		return
	}

	user, err := model.GetUserById(c.GetInt("id"), false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			redirectReject("access_denied")
			return
		}
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		redirectReject("access_denied")
		return
	}
	if strings.TrimSpace(user.Email) == "" {
		logger.LogWarn(c, "oidc: authorize rejected (email_required) user="+user.Username)
		c.JSON(http.StatusOK, gin.H{"success": false, "error": "email_required", "message": common.TranslateMessage(c, i18n.MsgOIDCEmailRequired)})
		return
	}

	code, err := oidc.IssueCode(oidc.AuthCode{
		UserId: user.Id, ClientId: req.ClientId, RedirectURI: req.RedirectURI,
		Nonce: req.Nonce, Scope: req.Scope,
		CodeChallenge: req.CodeChallenge, CodeChallengeMethod: req.CodeChallengeMethod,
		AuthTime: common.GetTimestamp(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	q := url.Values{"code": {code}}
	if req.State != "" {
		q.Set("state", req.State)
	}
	common.ApiSuccess(c, gin.H{"redirect_url": appendQuery(req.RedirectURI, q)})
}

func tokenError(c *gin.Context, status int, code, description string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Basic realm="oauth2"`)
	}
	c.JSON(status, gin.H{"error": code, "error_description": description})
}

// clientCredentials reads client_secret_basic first, then client_secret_post.
func clientCredentials(c *gin.Context) (string, string) {
	if id, secret, ok := c.Request.BasicAuth(); ok {
		return id, secret
	}
	return c.PostForm("client_id"), c.PostForm("client_secret")
}

// OAuth2Token exchanges an authorization code for an access token and id_token.
// Called server-to-server by Open WebUI.
func OAuth2Token(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !oauth2CredentialsConfigured(settings) {
		oauth2NotEnabled(c)
		return
	}
	clientId, clientSecret := clientCredentials(c)
	if clientId != settings.ClientId || subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		logger.LogWarn(c, "oidc: token rejected (invalid_client) client_id="+clientId+" ip="+c.ClientIP())
		tokenError(c, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	if c.PostForm("grant_type") != "authorization_code" {
		tokenError(c, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
		return
	}
	ac, err := oidc.ConsumeCode(c.PostForm("code"), clientId, c.PostForm("redirect_uri"), c.PostForm("code_verifier"))
	if err != nil {
		if errors.Is(err, oidc.ErrInvalidGrant) {
			logger.LogWarn(c, "oidc: token rejected (invalid_grant) client_id="+clientId+" ip="+c.ClientIP())
			tokenError(c, http.StatusBadRequest, "invalid_grant", "authorization code is invalid, expired or already used")
			return
		}
		logger.LogError(c, "oidc: token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	user, err := model.GetUserById(ac.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		tokenError(c, http.StatusBadRequest, "invalid_grant", "user is not available")
		return
	}
	claims := oidc.BuildClaims(user, oidc.LookupOrganizationName(user.OrganizationId))
	idToken, err := oidc.SignIDToken(claims, clientId, ac.Nonce, ac.AuthTime, time.Now())
	if err != nil {
		logger.LogError(c, "oidc: sign id_token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	access, err := oidc.IssueAccessToken(oidc.AccessToken{UserId: user.Id, ClientId: clientId, Scope: ac.Scope})
	if err != nil {
		logger.LogError(c, "oidc: issue access token: "+err.Error())
		tokenError(c, http.StatusInternalServerError, "server_error", "temporary failure")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(http.StatusOK, gin.H{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(oidc.AccessTokenTTL.Seconds()),
		"id_token":     idToken,
		"scope":        ac.Scope,
	})
}

func userinfoUnauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
}

// OAuth2UserInfo resolves an access token to the user's claims.
func OAuth2UserInfo(c *gin.Context) {
	if !oauth2CredentialsConfigured(system_setting.GetOAuth2Settings()) {
		oauth2NotEnabled(c)
		return
	}
	bearer := strings.TrimSpace(c.GetHeader("Authorization"))
	raw := ""
	if len(bearer) > 7 && strings.EqualFold(bearer[:7], "Bearer ") {
		raw = strings.TrimSpace(bearer[7:])
	}
	at, found, err := oidc.LookupAccessToken(raw)
	if err != nil {
		logger.LogError(c, "oidc: userinfo lookup: "+err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found {
		userinfoUnauthorized(c)
		return
	}
	user, err := model.GetUserById(at.UserId, false)
	if err != nil || user.Status != common.UserStatusEnabled {
		oidc.RevokeAccessToken(raw)
		userinfoUnauthorized(c)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, oidc.BuildClaims(user, oidc.LookupOrganizationName(user.OrganizationId)).UserInfo())
}
```

`common.StringsContains(slice []string, s string) bool` exists (used in `controller/model.go`). `logger.LogError/LogWarn(ctx, msg)` exist.

- [ ] **Step 5: Routes**

`router/oauth2-router.go` (full replacement):

```go
package router

import (
	"github.com/QuantumNous/new-api/controller"

	"github.com/gin-gonic/gin"
)

// SetOAuth2Router registers the public OIDC provider endpoints. The browser-
// facing /oauth2/authorize and /oauth2/logout are SPA pages (served by the web
// router's NoRoute fallback) that call /api/oauth2/* — see api-router.go.
func SetOAuth2Router(router *gin.Engine) {
	router.GET("/.well-known/openid-configuration", controller.OAuth2Discovery)
	oauth2Router := router.Group("/oauth2")
	{
		oauth2Router.GET("/jwks", controller.OAuth2JWKS)
		oauth2Router.POST("/token", controller.OAuth2Token)
		oauth2Router.GET("/userinfo", controller.OAuth2UserInfo)
	}
}
```

In `router/api-router.go`, after the `openWebUIRoute` group added by the relay plan, add:

```go
		oidcRoute := apiRouter.Group("/oauth2")
		{
			oidcRoute.POST("/authorize", middleware.UserAuth(), controller.OAuth2Authorize)
		}
```

(Task 6 adds the logout/rotate/status routes to this same group.)

- [ ] **Step 6: Build, run tests**

Run: `go build ./... && go vet ./controller/ ./service/... ./model/ && go test ./controller/ -run 'OIDC_|UpdateOption_OAuth2' -count=1 && go test ./model/ ./service/... -count=1`
Expected: build clean (no remaining references to the deleted functions — `grep -rn "IssueOAuth2Token\|StoreAuthCode\|ConsumeAuthCode\|RevokeOAuth2Token\|IsOAuth2SentinelTokenId" --include='*.go' .` prints nothing), all PASS.

- [ ] **Step 7: Commit**

```bash
git add -A controller/oauth2_provider.go controller/oauth2_provider_test.go controller/user.go service/billing_session.go model/quota_reserve.go router/oauth2-router.go router/api-router.go model/oauth2_session.go model/oauth2_session_test.go controller/logout_oauth2_test.go
git commit -m "feat(oidc): standard discovery, authorize, token and userinfo endpoints; remove broker flow

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: End-session API, signing-key rotation and status endpoints

**Files:**
- Modify: `controller/oauth2_provider.go` (append handlers)
- Create: `controller/oauth2_logout_test.go`
- Modify: `router/api-router.go` (`oidcRoute` group from Task 5)

**Interfaces:**
- Consumes: `oidc.VerifyIDTokenHint`, `oidc.RotateKey`, `oidc.CurrentKey`, `oidc.VerificationKeys` (Tasks 2, 4), `system_setting.GetOAuth2Settings()`, `sessions.Default(c)`.
- Produces handlers: `OAuth2Logout` (POST, no auth middleware — reads the session itself), `OAuth2RotateSigningKey` (RootAuth), `OAuth2SigningKeyStatus` (RootAuth).
- Produces routes: `POST /api/oauth2/logout`, `POST /api/oauth2/rotate-signing-key`, `GET /api/oauth2/signing-key-status`.
- Contract of `POST /api/oauth2/logout` (JSON body `{id_token_hint, post_logout_redirect_uri, state}`): always `{"success":true,"data":{"redirect_url":"…"}}`. Signature failure → session untouched, `redirect_url:"/"`. Otherwise session cleared; `redirect_url` = configured `post_logout_redirect_uri` (+`?state=` when given) only when the request's value matches it exactly, else `"/sign-in"`.
- Contract of `GET /api/oauth2/signing-key-status`: `{"success":true,"data":{"current":{"kid","created_at"},"previous":{"kid","created_at"}|null}}`. `POST /rotate-signing-key` returns the same shape after rotating.

- [ ] **Step 1: Failing tests**

`controller/oauth2_logout_test.go`:

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/oidc"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// registerLogoutRoutes adds the Task 6 routes plus a probe that reports
// whether the session still carries a user.
func registerLogoutRoutes(env *oidcTestEnv) {
	env.engine.POST("/api/oauth2/logout", OAuth2Logout)
	env.engine.GET("/test/whoami", func(c *gin.Context) {
		id := sessions.Default(c).Get("id")
		c.JSON(http.StatusOK, gin.H{"id": id})
	})
	root := env.engine.Group("/api/oauth2")
	root.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleRootUser); c.Next() })
	root.POST("/rotate-signing-key", OAuth2RotateSigningKey)
	root.GET("/signing-key-status", OAuth2SigningKeyStatus)
}

func (e *oidcTestEnv) postLogout(t *testing.T, ck *http.Cookie, body map[string]string) (map[string]any, []*http.Cookie) {
	t.Helper()
	raw, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/oauth2/logout", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	if ck != nil {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
	return res, rec.Result().Cookies()
}

func (e *oidcTestEnv) whoami(t *testing.T, cookies []*http.Cookie) any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/test/whoami", nil)
	for _, ck := range cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	var res map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
	return res["id"]
}

func mintIDToken(t *testing.T, userId int, issuedAt time.Time) string {
	t.Helper()
	user, err := model.GetUserById(userId, false)
	require.NoError(t, err)
	raw, err := oidc.SignIDToken(oidc.BuildClaims(user, ""), oidcTestClientId, "n", issuedAt.Unix(), issuedAt)
	require.NoError(t, err)
	return raw
}

func redirectOf(res map[string]any) string {
	return res["data"].(map[string]any)["redirect_url"].(string)
}

func TestOIDC_Logout_ClearsSessionAndHonoursRegisteredRedirect(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	require.Equal(t, float64(7), env.whoami(t, []*http.Cookie{ck}))

	hint := mintIDToken(t, 7, time.Now().Add(-5*time.Hour)) // expired hints are fine
	res, cookies := env.postLogout(t, ck, map[string]string{
		"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye", "state": "s1",
	})
	require.Equal(t, true, res["success"])
	require.Equal(t, "https://chat.example.com/bye?state=s1", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies), "session must be cleared")
}

func TestOIDC_Logout_UnregisteredRedirectFallsBackToSignIn(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]string{
		"id_token_hint": mintIDToken(t, 7, time.Now()), "post_logout_redirect_uri": "https://evil.example.com/",
	})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))

	// No hint at all: still logs out the current session.
	ck = env.login(t, "7")
	res, cookies = env.postLogout(t, ck, map[string]string{})
	require.Equal(t, "/sign-in", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

func TestOIDC_Logout_BadHintLeavesSessionAlone(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]string{"id_token_hint": "not.a.token", "post_logout_redirect_uri": "https://chat.example.com/bye"})
	require.Equal(t, "/", redirectOf(res))
	all := append([]*http.Cookie{ck}, cookies...)
	require.Equal(t, float64(7), env.whoami(t, all), "forged hint must not end the session")
}

func TestOIDC_Logout_HintSignedByPreviousKey(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)
	hint := mintIDToken(t, 7, time.Now())
	_, err := oidc.RotateKey()
	require.NoError(t, err)
	ck := env.login(t, "7")
	res, cookies := env.postLogout(t, ck, map[string]string{"id_token_hint": hint, "post_logout_redirect_uri": "https://chat.example.com/bye"})
	require.Equal(t, "https://chat.example.com/bye", redirectOf(res))
	require.Nil(t, env.whoami(t, cookies))
}

func TestOIDC_SigningKeyStatusAndRotate(t *testing.T) {
	env := setupOIDCProviderTest(t)
	registerLogoutRoutes(env)

	get := func() map[string]any {
		rec := httptest.NewRecorder()
		env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/oauth2/signing-key-status", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var res map[string]any
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &res))
		return res["data"].(map[string]any)
	}
	before := get()
	cur := before["current"].(map[string]any)
	require.NotEmpty(t, cur["kid"])
	require.NotZero(t, cur["created_at"])
	require.Nil(t, before["previous"])

	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/oauth2/rotate-signing-key", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var rotated map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &rotated))
	after := rotated["data"].(map[string]any)
	require.NotEqual(t, cur["kid"], after["current"].(map[string]any)["kid"])
	require.Equal(t, cur["kid"], after["previous"].(map[string]any)["kid"])

	// Response never contains key material.
	require.NotContains(t, rec.Body.String(), "PRIVATE KEY")
	require.NotContains(t, rec.Body.String(), "private_pem")
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./controller/ -run 'OIDC_Logout|OIDC_SigningKey' -count=1`
Expected: FAIL — `undefined: OAuth2Logout`, `OAuth2RotateSigningKey`, `OAuth2SigningKeyStatus`.

- [ ] **Step 3: Implement the handlers**

Append to `controller/oauth2_provider.go` (add `"github.com/gin-contrib/sessions"` to its imports):

```go
type oauth2LogoutRequest struct {
	IDTokenHint            string `json:"id_token_hint"`
	PostLogoutRedirectURI  string `json:"post_logout_redirect_uri"`
	State                  string `json:"state"`
}

// OAuth2Logout is the same-origin API behind the SPA page /oauth2/logout
// (the OIDC end_session_endpoint). A verifiable id_token_hint — or no hint at
// all — ends the new-api session; a hint that fails verification is ignored
// so this URL cannot be used to log strangers out.
func OAuth2Logout(c *gin.Context) {
	var req oauth2LogoutRequest
	_ = common.DecodeJson(c.Request.Body, &req) // an empty body is a plain logout
	settings := system_setting.GetOAuth2Settings()

	if strings.TrimSpace(req.IDTokenHint) != "" {
		if _, err := oidc.VerifyIDTokenHint(req.IDTokenHint, settings.ClientId); err != nil {
			logger.LogWarn(c, "oidc: end-session ignored: id_token_hint failed verification ip="+c.ClientIP())
			common.ApiSuccess(c, gin.H{"redirect_url": "/"})
			return
		}
	}

	session := sessions.Default(c)
	session.Clear()
	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}

	redirectURL := "/sign-in"
	if settings.PostLogoutRedirectURI != "" && req.PostLogoutRedirectURI == settings.PostLogoutRedirectURI {
		redirectURL = settings.PostLogoutRedirectURI
		if req.State != "" {
			redirectURL = appendQuery(redirectURL, url.Values{"state": {req.State}})
		}
	}
	common.ApiSuccess(c, gin.H{"redirect_url": redirectURL})
}

func signingKeyStatusPayload() (gin.H, error) {
	keys, err := oidc.VerificationKeys()
	if err != nil {
		return nil, err
	}
	payload := gin.H{
		"current":  gin.H{"kid": keys[0].Kid, "created_at": keys[0].CreatedAt},
		"previous": nil,
	}
	if len(keys) > 1 {
		payload["previous"] = gin.H{"kid": keys[1].Kid, "created_at": keys[1].CreatedAt}
	}
	return payload, nil
}

// OAuth2SigningKeyStatus shows the admin which keys are published. Root only.
func OAuth2SigningKeyStatus(c *gin.Context) {
	payload, err := signingKeyStatusPayload()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, payload)
}

// OAuth2RotateSigningKey installs a new signing key; the old one stays in the
// JWKS for oidc.PreviousKeyRetention. Root only.
func OAuth2RotateSigningKey(c *gin.Context) {
	if _, err := oidc.RotateKey(); err != nil {
		common.ApiError(c, err)
		return
	}
	logger.LogInfo(c, "oidc: signing key rotated by user "+strconv.Itoa(c.GetInt("id")))
	payload, err := signingKeyStatusPayload()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, payload)
}
```

Add `"strconv"` to the imports.

- [ ] **Step 4: Routes**

In `router/api-router.go`, extend the `oidcRoute` group:

```go
		oidcRoute := apiRouter.Group("/oauth2")
		{
			oidcRoute.POST("/authorize", middleware.UserAuth(), controller.OAuth2Authorize)
			oidcRoute.POST("/logout", controller.OAuth2Logout)
			oidcRoute.POST("/rotate-signing-key", middleware.RootAuth(), controller.OAuth2RotateSigningKey)
			oidcRoute.GET("/signing-key-status", middleware.RootAuth(), controller.OAuth2SigningKeyStatus)
		}
```

- [ ] **Step 5: Run tests and commit**

Run: `go build ./... && go test ./controller/ -run 'OIDC_' -count=1 && go vet ./controller/`
Expected: PASS

```bash
git add controller/oauth2_provider.go controller/oauth2_logout_test.go router/api-router.go
git commit -m "feat(oidc): end-session API and signing key rotation endpoints

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: Frontend — `/oauth2/authorize` and `/oauth2/logout` SPA pages

**Files:**
- Create: `web/default/src/features/oidc/api.ts`
- Create: `web/default/src/routes/oauth2/authorize.tsx`
- Create: `web/default/src/routes/oauth2/logout.tsx`
- Modify: `web/default/src/i18n/locales/en.json`, `kr.json`

**Interfaces:**
- Consumes: `POST /api/oauth2/authorize`, `POST /api/oauth2/logout` (Tasks 5–6); `api` from `@/lib/api` (adds `New-Api-User` from localStorage `uid`); `AuthLayout` from `@/features/auth/auth-layout`; `Button` from `@/components/ui/button`; `Spinner` from `@/components/ui/spinner` (if its export name differs, use `Loader2` from `lucide-react` with `animate-spin`).
- Produces: `authorizeOidc(params): Promise<OidcAuthorizeResponse>`, `logoutOidc(params): Promise<{success: boolean; data?: {redirect_url: string}}>`, routes `/oauth2/authorize`, `/oauth2/logout`.

- [ ] **Step 1: API module**

`web/default/src/features/oidc/api.ts` (license header copied verbatim from `features/users/api.ts`):

```ts
import { api } from '@/lib/api'

export type OidcAuthorizeParams = {
  response_type?: string
  client_id?: string
  redirect_uri?: string
  scope?: string
  state?: string
  nonce?: string
  code_challenge?: string
  code_challenge_method?: string
}

export type OidcAuthorizeResponse = {
  success: boolean
  error?: string
  message?: string
  data?: { redirect_url: string }
}

/**
 * Same-origin half of the OIDC authorize endpoint. The browser lands on
 * /oauth2/authorize from Open WebUI (a cross-site navigation that carries
 * no SameSite=Strict session cookie); this XHR is same-origin, so the
 * cookie is sent and the server can tell whether the user is logged in.
 * A 401 means "not logged in" and is handled by the page, so the global
 * 401 toast/reset is suppressed.
 */
export async function authorizeOidc(
  params: OidcAuthorizeParams
): Promise<{ status: number; body: OidcAuthorizeResponse | null }> {
  try {
    const res = await api.post<OidcAuthorizeResponse>(
      '/api/oauth2/authorize',
      params,
      { skipErrorHandler: true, skipBusinessError: true }
    )
    return { status: res.status, body: res.data }
  } catch (err) {
    const status = (err as { response?: { status?: number } })?.response?.status ?? 0
    const body = (err as { response?: { data?: OidcAuthorizeResponse } })?.response?.data ?? null
    return { status, body }
  }
}

export type OidcLogoutParams = {
  id_token_hint?: string
  post_logout_redirect_uri?: string
  state?: string
}

export async function logoutOidc(
  params: OidcLogoutParams
): Promise<{ success: boolean; data?: { redirect_url: string } }> {
  const res = await api.post('/api/oauth2/logout', params, {
    skipErrorHandler: true,
    skipBusinessError: true,
  })
  return res.data
}
```

- [ ] **Step 2: Authorize page**

`web/default/src/routes/oauth2/authorize.tsx` (license header first):

```tsx
import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { AuthLayout } from '@/features/auth/auth-layout'
import { useAuthStore } from '@/stores/auth-store'
import { authorizeOidc, type OidcAuthorizeParams } from '@/features/oidc/api'

const searchSchema = z.object({
  response_type: z.string().optional(),
  client_id: z.string().optional(),
  redirect_uri: z.string().optional(),
  scope: z.string().optional(),
  state: z.string().optional(),
  nonce: z.string().optional(),
  code_challenge: z.string().optional(),
  code_challenge_method: z.string().optional(),
})

type Phase =
  | { kind: 'working' }
  | { kind: 'email_required' }
  | { kind: 'invalid'; message: string }

/**
 * OIDC authorization endpoint, rendered as a page because the cross-site
 * navigation from Open WebUI carries no SameSite=Strict session cookie.
 * The page immediately calls the same-origin authorize API and acts on
 * the answer: 401 → sign in and come back; success → follow the redirect;
 * redirectable error → follow the redirect (it carries ?error=);
 * non-redirectable error → explain here and never trust redirect_uri.
 */
function OidcAuthorizePage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const search = Route.useSearch()
  const [phase, setPhase] = useState<Phase>({ kind: 'working' })

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      const { status, body } = await authorizeOidc(search as OidcAuthorizeParams)
      if (cancelled) return

      if (status === 401) {
        // Not logged in (or the New-Api-User header was missing because the
        // auth store is empty): send to sign-in and return to this exact URL.
        useAuthStore.getState().auth.reset()
        const here = window.location.pathname + window.location.search
        navigate({ to: '/sign-in', search: { redirect: here }, replace: true })
        return
      }

      const redirectUrl = body?.data?.redirect_url
      if (redirectUrl) {
        window.location.replace(redirectUrl)
        return
      }
      if (body?.error === 'email_required') {
        setPhase({ kind: 'email_required' })
        return
      }
      setPhase({
        kind: 'invalid',
        message: body?.message || t('The sign-in request is not valid, please contact your administrator'),
      })
    })()
    return () => {
      cancelled = true
    }
    // The search params are stable for the life of this page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        {phase.kind === 'working' && (
          <>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
            <p className='text-muted-foreground text-sm'>
              {t('Signing you in to the external app...')}
            </p>
          </>
        )}
        {phase.kind === 'email_required' && (
          <>
            <h2 className='text-lg font-semibold'>{t('Email address required')}</h2>
            <p className='text-muted-foreground text-sm'>
              {t(
                'The external app needs your email address. Register one in your profile, then open the external app again.'
              )}
            </p>
            <Button onClick={() => navigate({ to: '/profile' })}>
              {t('Go to profile')}
            </Button>
          </>
        )}
        {phase.kind === 'invalid' && (
          <>
            <h2 className='text-lg font-semibold'>{t('Sign-in request rejected')}</h2>
            <p className='text-muted-foreground text-sm'>{phase.message}</p>
            <Button variant='outline' onClick={() => navigate({ to: '/' })}>
              {t('Back to home')}
            </Button>
          </>
        )}
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/authorize')({
  validateSearch: searchSchema,
  component: OidcAuthorizePage,
})
```

If `navigate({ to: '/profile' })` fails typecheck because the typed route is `/profile/` or lives under `_authenticated`, use the route id TanStack generated in `routeTree.gen.ts` (search for `profile`).

- [ ] **Step 3: Logout page**

`web/default/src/routes/oauth2/logout.tsx` (license header first):

```tsx
import { useEffect } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Loader2 } from 'lucide-react'
import { AuthLayout } from '@/features/auth/auth-layout'
import { useAuthStore } from '@/stores/auth-store'
import { logoutOidc } from '@/features/oidc/api'

const searchSchema = z.object({
  id_token_hint: z.string().optional(),
  post_logout_redirect_uri: z.string().optional(),
  state: z.string().optional(),
})

/**
 * OIDC end_session_endpoint as a page: the same-origin API call below
 * carries the session cookie that the cross-site GET from Open WebUI did
 * not, so the new-api session really ends. The server decides where to
 * send the browser next (registered post-logout URI or /sign-in).
 */
function OidcLogoutPage() {
  const { t } = useTranslation()
  const search = Route.useSearch()

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      let target = '/sign-in'
      try {
        const res = await logoutOidc(search)
        if (res?.data?.redirect_url) target = res.data.redirect_url
      } catch {
        /* fall back to /sign-in */
      }
      if (cancelled) return
      useAuthStore.getState().auth.reset()
      window.location.replace(target)
    })()
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <AuthLayout>
      <div className='mx-auto flex w-full max-w-sm flex-col items-center gap-4 text-center'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
        <p className='text-muted-foreground text-sm'>{t('Signing you out...')}</p>
      </div>
    </AuthLayout>
  )
}

export const Route = createFileRoute('/oauth2/logout')({
  validateSearch: searchSchema,
  component: OidcLogoutPage,
})
```

Check `useAuthStore.getState().auth.reset` exists (it is called in `lib/api.ts`'s 401 handler, so it does).

- [ ] **Step 4: Translations**

Add to `en.json` (value = key) and `kr.json`:

```
"Signing you in to the external app..."        → "외부 앱에 로그인하는 중..."
"Email address required"                        → "이메일 주소가 필요합니다"
"The external app needs your email address. Register one in your profile, then open the external app again." → "외부 앱은 이메일 주소가 필요합니다. 프로필에서 이메일을 등록한 뒤 외부 앱을 다시 열어 주세요."
"Go to profile"                                 → "프로필로 이동"
"Sign-in request rejected"                      → "로그인 요청이 거부되었습니다"
"The sign-in request is not valid, please contact your administrator" → "로그인 요청이 올바르지 않습니다. 관리자에게 문의해 주세요"
"Back to home"                                  → "홈으로"
"Signing you out..."                            → "로그아웃하는 중..."
```

Grep first; add only missing keys. Then `cd web/default && bun run i18n:sync` and commit the regenerated `src/i18n/locales/_reports/*.json`.

- [ ] **Step 5: Gates**

Run: `cd web/default && bunx tsc --noEmit -p tsconfig.app.json 2>&1 | grep -E 'oauth2/|features/oidc' ; bunx prettier --check src/routes/oauth2/*.tsx src/features/oidc/api.ts`
Expected: no TS errors in those files; prettier clean (run `--write` once if needed). The TanStack route tree (`routeTree.gen.ts`) regenerates on `bun run dev`/build; if typecheck complains the routes are unknown, run `bunx @tanstack/router-cli generate` (or the project's equivalent in package.json scripts) and commit the regenerated tree.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/oidc/api.ts web/default/src/routes/oauth2/authorize.tsx web/default/src/routes/oauth2/logout.tsx web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json web/default/src/i18n/locales/_reports web/default/src/routeTree.gen.ts
git commit -m "feat(oidc): SPA pages for the OIDC authorize and end-session endpoints

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

(Drop `routeTree.gen.ts` from the `git add` list if it did not change.)

---

### Task 8: Frontend — Playground link, settings fields, signing-key rotation UI

**Files:**
- Modify: `web/default/src/features/auth/types.ts` (both status blocks)
- Modify: `web/default/src/features/playground/constants.ts`, `web/default/src/features/playground/api.ts`, `web/default/src/features/playground/index.tsx`, `web/default/src/routes/_authenticated/playground/index.tsx`
- Modify: `web/default/src/features/system-settings/types.ts`, `site/index.tsx`, `site/section-registry.tsx`, `site/openwebui-integration-section.tsx`, `features/system-settings/api.ts`
- Modify: `web/default/src/i18n/locales/en.json`, `kr.json`

**Interfaces:**
- Consumes: `/api/status` `oauth2_app_url` (Task 1); `GET /api/oauth2/signing-key-status`, `POST /api/oauth2/rotate-signing-key` (Task 6).
- Produces: `getOidcSigningKeyStatus()`, `rotateOidcSigningKey()` in `features/system-settings/api.ts`; `SiteSettings` keys `'oauth2.app_url': string`, `'oauth2.post_logout_redirect_uri': string`.

- [ ] **Step 1: Status type**

In `web/default/src/features/auth/types.ts`, in BOTH places where `oauth2_replace_playground?: boolean` appears, add after it:

```ts
    oauth2_app_url?: string
```

- [ ] **Step 2: Playground becomes a plain link**

`features/playground/constants.ts`: delete the line `OAUTH2_SESSION_INIT: '/oauth2/session-init',`.

`features/playground/api.ts`: delete `getOpenInChatAppUrl` and its doc comment entirely.

`features/playground/index.tsx`:
- change the import to `import { getUserModels, getUserGroups } from './api'`
- add `const appUrl = typeof status?.oauth2_app_url === 'string' ? status.oauth2_app_url : ''` next to the other `status?.oauth2_*` reads in `Playground()`, and pass it to the replaced component: `<PlaygroundReplacedByChatApp openInNewWindow={openInNewWindow} appUrl={appUrl} />`
- replace `PlaygroundReplacedByChatApp` with:

```tsx
function PlaygroundReplacedByChatApp({
  openInNewWindow,
  appUrl,
}: {
  openInNewWindow: boolean
  appUrl: string
}) {
  const { t } = useTranslation()

  useEffect(() => {
    if (!openInNewWindow && appUrl) {
      openChatApp(appUrl, false)
    }
  }, [openInNewWindow, appUrl])

  if (!appUrl) {
    return (
      <div className='flex size-full flex-col items-center justify-center gap-4'>
        <p className='text-muted-foreground text-sm'>
          {t('External chat app is not configured')}
        </p>
      </div>
    )
  }

  if (openInNewWindow) {
    return (
      <div className='flex size-full flex-col items-center justify-center gap-4'>
        <p className='text-muted-foreground text-sm'>
          {t('Playground has been replaced by the external chat app')}
        </p>
        <Button onClick={() => openChatApp(appUrl, true)}>
          <ExternalLinkIcon className='mr-1 size-4' />
          {t('Open Chat App')}
        </Button>
      </div>
    )
  }

  return (
    <div className='flex size-full flex-col items-center justify-center gap-4'>
      <p className='text-muted-foreground text-sm'>
        {t('Redirecting to the external chat app...')}
      </p>
    </div>
  )
}
```

- replace `handleOpenInChatApp` with:

```tsx
  const handleOpenInChatApp = useCallback(() => {
    if (!appUrl) {
      toast.error(t('External chat app is not configured'))
      return
    }
    openChatApp(appUrl, openInNewWindow)
  }, [t, appUrl, openInNewWindow])
```

- show the toolbar button only when a URL exists: `{oauth2Enabled && appUrl && (` … `)}`.
- remove `useCallback`/`useState` imports only if they become unused (typecheck will tell).

`routes/_authenticated/playground/index.tsx`: remove the `getOpenInChatAppUrl` import and replace the block from `let redirectUrl: string` through `window.location.href = redirectUrl` with:

```ts
    const appUrl = typeof status?.oauth2_app_url === 'string' ? status.oauth2_app_url : ''
    if (!appUrl) return

    const openInNewWindow = Boolean(status?.oauth2_open_in_new_window)
    if (openInNewWindow) {
      window.open(appUrl, '_blank', 'noopener,noreferrer')
      throw redirect({ to: '/dashboard' })
    }
    window.location.href = appUrl
```

- [ ] **Step 3: Settings types, defaults, registry**

`features/system-settings/types.ts`: after `'oauth2.redirect_uri': string` add

```ts
  'oauth2.app_url': string
  'oauth2.post_logout_redirect_uri': string
```

and near `OpenWebUIRelayStatus` add:

```ts
export type OidcSigningKeyInfo = { kid: string; created_at: number }
export type OidcSigningKeyStatus = {
  current: OidcSigningKeyInfo
  previous: OidcSigningKeyInfo | null
}
```

`site/index.tsx` `defaultSiteSettings`: after `'oauth2.redirect_uri': '',` add `'oauth2.app_url': '',` and `'oauth2.post_logout_redirect_uri': '',`.

`site/section-registry.tsx`, in the `openwebui-integration` entry's `defaultValues`, add:

```tsx
          'oauth2.app_url': settings['oauth2.app_url'],
          'oauth2.post_logout_redirect_uri':
            settings['oauth2.post_logout_redirect_uri'],
```

`features/system-settings/api.ts` (import `OidcSigningKeyStatus` from `./types`):

```ts
export async function getOidcSigningKeyStatus() {
  const res = await api.get<{ success: boolean; message?: string; data?: OidcSigningKeyStatus }>(
    '/api/oauth2/signing-key-status'
  )
  return res.data
}

export async function rotateOidcSigningKey() {
  const res = await api.post<{ success: boolean; message?: string; data?: OidcSigningKeyStatus }>(
    '/api/oauth2/rotate-signing-key'
  )
  return res.data
}
```

- [ ] **Step 4: Settings section**

In `site/openwebui-integration-section.tsx`:

1. Extend `oauth2Schema`, `FlatOauth2Defaults`, `buildFormDefaults`, `normalizeFormValues` with `app_url: z.string()` / `'oauth2.app_url': string` and `post_logout_redirect_uri: z.string()` / `'oauth2.post_logout_redirect_uri': string` (same shape as `redirect_uri`).
2. Replace the `oauth2.redirect_uri` field's placeholder and description:
   - placeholder: `'https://chat.example.com/oauth/oidc/login/callback'`
   - description: `t("Open WebUI's OPENID_REDIRECT_URI — must match exactly. Open WebUI's default is {WEBUI_URL}/oauth/oidc/login/callback.")`
3. After the `oauth2.redirect_uri` field add two fields with the same `Input` pattern:
   - `oauth2.app_url`: label `t('App URL')`, placeholder `'https://chat.example.com'`, description `t("Open WebUI's WEBUI_URL. The Playground \"Open in Chat App\" link opens this address; the app then signs in through new-api automatically.")`
   - `oauth2.post_logout_redirect_uri`: label `t('Post-logout redirect URI')`, placeholder `'https://chat.example.com'`, description `t("Open WebUI's WEBUI_AUTH_SIGNOUT_REDIRECT_URL. The only address the end-session endpoint will redirect to after logging out; leave empty to send users to the new-api sign-in page.")`
4. Update the `oauth2.enabled` description to: `t('Let Open WebUI sign in as new-api users through the built-in OpenID Connect provider. Point Open WebUI\'s OPENID_PROVIDER_URL at this server\'s /.well-known/openid-configuration.')`
5. Add a signing-key block at the end of the form (inside `SettingsForm`, after the `replace_playground` field):

```tsx
          <SigningKeyPanel />
```

with, in the same file:

```tsx
function SigningKeyPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const statusQuery = useQuery({
    queryKey: ['oidc-signing-key-status'],
    queryFn: getOidcSigningKeyStatus,
    staleTime: 60 * 1000,
  })
  const rotate = useMutation({
    mutationFn: rotateOidcSigningKey,
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Signing key rotated'))
        queryClient.invalidateQueries({ queryKey: ['oidc-signing-key-status'] })
      } else {
        toast.error(res.message || t('Failed to rotate signing key'))
      }
    },
    onError: () => toast.error(t('Failed to rotate signing key')),
  })
  const [confirmOpen, setConfirmOpen] = useState(false)
  const data = statusQuery.data?.data
  const fmt = (ts: number) => new Date(ts * 1000).toLocaleString()

  return (
    <div className='flex flex-col gap-2 rounded-md border p-4'>
      <h4 className='text-sm font-medium'>{t('ID token signing key')}</h4>
      <p className='text-muted-foreground text-xs'>
        {t(
          'id_tokens are signed with RS256. Rotating installs a new key immediately; the previous key stays published for 24 hours so tokens signed with it keep verifying.'
        )}
      </p>
      {data ? (
        <div className='text-muted-foreground grid gap-1 font-mono text-xs'>
          <span>
            {t('Current')}: {data.current.kid} · {fmt(data.current.created_at)}
          </span>
          {data.previous && (
            <span>
              {t('Previous')}: {data.previous.kid} · {fmt(data.previous.created_at)}
            </span>
          )}
        </div>
      ) : (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      )}
      <div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => setConfirmOpen(true)}
          disabled={rotate.isPending || !data}
        >
          {t('Rotate signing key')}
        </Button>
      </div>
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Rotate the signing key?')}
        desc={t(
          'A new key will be used for all new id_tokens. Open WebUI picks it up from the JWKS endpoint automatically; existing sessions are not affected.'
        )}
        confirmText={t('Rotate')}
        isLoading={rotate.isPending}
        handleConfirm={() => {
          rotate.mutate()
          setConfirmOpen(false)
        }}
      />
    </div>
  )
}
```

Imports to add: `useState` (react), `useMutation, useQuery, useQueryClient` (`@tanstack/react-query`), `Button` (`@/components/ui/button`), `ConfirmDialog` (`@/components/confirm-dialog`), `getOidcSigningKeyStatus, rotateOidcSigningKey` (`../api`). The signing-key endpoints are RootAuth; the Site settings page is root-only already (`/api/option/` is RootAuth), so no extra guard is needed.

- [ ] **Step 5: Translations**

New strings for `en.json` (value = key) and `kr.json`:

```
"App URL" → "앱 URL"
"Open WebUI's WEBUI_URL. The Playground \"Open in Chat App\" link opens this address; the app then signs in through new-api automatically." → "Open WebUI의 WEBUI_URL입니다. Playground의 \"Open in Chat App\" 링크가 이 주소를 열며, 앱이 new-api를 통해 자동으로 로그인합니다."
"Post-logout redirect URI" → "로그아웃 후 리다이렉트 URI"
"Open WebUI's WEBUI_AUTH_SIGNOUT_REDIRECT_URL. The only address the end-session endpoint will redirect to after logging out; leave empty to send users to the new-api sign-in page." → "Open WebUI의 WEBUI_AUTH_SIGNOUT_REDIRECT_URL입니다. 로그아웃 뒤 end-session 엔드포인트가 이동을 허용하는 유일한 주소이며, 비우면 new-api 로그인 페이지로 보냅니다."
"Open WebUI's OPENID_REDIRECT_URI — must match exactly. Open WebUI's default is {WEBUI_URL}/oauth/oidc/login/callback." → "Open WebUI의 OPENID_REDIRECT_URI이며 정확히 일치해야 합니다. Open WebUI 기본값은 {WEBUI_URL}/oauth/oidc/login/callback 입니다."
"Let Open WebUI sign in as new-api users through the built-in OpenID Connect provider. Point Open WebUI's OPENID_PROVIDER_URL at this server's /.well-known/openid-configuration." → "내장 OpenID Connect 제공자를 통해 Open WebUI가 new-api 사용자로 로그인하게 합니다. Open WebUI의 OPENID_PROVIDER_URL을 이 서버의 /.well-known/openid-configuration 으로 지정하세요."
"ID token signing key" → "ID 토큰 서명 키"
"id_tokens are signed with RS256. Rotating installs a new key immediately; the previous key stays published for 24 hours so tokens signed with it keep verifying." → "id_token은 RS256으로 서명됩니다. 교체하면 새 키가 즉시 사용되며, 이전 키는 24시간 동안 함께 게시되어 그 키로 서명된 토큰도 계속 검증됩니다."
"Current" → "현재"
"Previous" → "이전"
"Rotate signing key" → "서명 키 교체"
"Rotate the signing key?" → "서명 키를 교체할까요?"
"A new key will be used for all new id_tokens. Open WebUI picks it up from the JWKS endpoint automatically; existing sessions are not affected." → "새 id_token은 모두 새 키로 서명됩니다. Open WebUI는 JWKS 엔드포인트에서 자동으로 새 키를 가져오며 기존 세션에는 영향이 없습니다."
"Rotate" → "교체"
"Signing key rotated" → "서명 키를 교체했습니다"
"Failed to rotate signing key" → "서명 키 교체에 실패했습니다"
```

Grep for existing keys (`Current`, `Previous`, `Rotate`, `Loading...` may exist) and add only the missing ones. Remove the now-unused old descriptions for `oauth2.enabled` and `oauth2.redirect_uri` from both locale files (search for the old English text and delete those entries). Run `bun run i18n:sync`.

- [ ] **Step 6: Gates and browser check**

Run: `cd web/default && bunx tsc --noEmit -p tsconfig.app.json 2>&1 | grep -E 'playground|system-settings/(site|types|api)|features/auth/types' ; bunx prettier --check <touched files>; bun run i18n:sync`
Expected: no TS errors in touched files; prettier clean; 0 missing kr keys. `grep -rn "session-init\|getOpenInChatAppUrl" web/default/src` prints nothing.

Browser (if a backend is available via `preview_start`): Playground shows "Open in Chat App" only when `app_url` is set and opens it; the settings section shows the two new fields and the signing-key panel with a kid; rotating shows a second kid as "Previous".

- [ ] **Step 7: Commit**

```bash
git add web/default/src/features/auth/types.ts web/default/src/features/playground web/default/src/routes/_authenticated/playground/index.tsx web/default/src/features/system-settings/types.ts web/default/src/features/system-settings/api.ts web/default/src/features/system-settings/site web/default/src/i18n/locales
git commit -m "feat(oidc): link Playground to the chat app URL and add signing-key rotation to settings

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Full verification, supersede notes, spec touch-up

**Files:**
- Modify: `docs/superpowers/specs/2026-08-16-oauth2-provider-design.md` and `docs/superpowers/specs/2026-08-16-oauth2-openwebui-integration-handoff.md` (prepend a note)
- Modify: `docs/superpowers/specs/2026-10-07-openwebui-oidc-provider-design.md`

- [ ] **Step 1: Backend suite**

Run: `go build ./... && go test ./... -count=1`
Expected: all green. `go vet ./...` shows only the pre-existing warnings in `relay/channel/dify`, `relay/channel/tencent`, `common/custom-event.go`.

- [ ] **Step 2: Frontend suite**

Run: `cd web/default && bunx tsc --noEmit -p tsconfig.app.json 2>&1 | grep -cE "error TS"` (compare with the pre-existing count on the base commit; no new errors) and `bun test src`.

- [ ] **Step 3: Manual flow with curl + browser against a local backend**

With the backend running and the provider configured (client id/secret/redirect_uri set, enabled):

```bash
curl -s localhost:3000/.well-known/openid-configuration | head -c 400
curl -s localhost:3000/oauth2/jwks
```

Then, in a browser logged in to new-api, open
`http://localhost:3000/oauth2/authorize?response_type=code&client_id=<id>&redirect_uri=<registered>&scope=openid%20email%20profile&state=abc&nonce=n1`
and confirm the browser lands on `<registered>?code=…&state=abc`. Exchange it:

```bash
curl -s -u "<client_id>:<client_secret>" -d grant_type=authorization_code -d code=<code> -d "redirect_uri=<registered>" localhost:3000/oauth2/token
curl -s -H "Authorization: Bearer <access_token>" localhost:3000/oauth2/userinfo
```

Expected: JSON with `id_token` and `access_token`; userinfo with `sub`, `email`, `roles`, `groups`. Open `http://localhost:3000/oauth2/logout?id_token_hint=<id_token>` → new-api session ends and the browser lands on `/sign-in`. Open the authorize URL again while logged out → sign-in page → after login the browser returns to the callback with a fresh code.

- [ ] **Step 4: Supersede notes and spec touch-up**

Prepend to `docs/superpowers/specs/2026-08-16-oauth2-provider-design.md` and to `docs/superpowers/specs/2026-08-16-oauth2-openwebui-integration-handoff.md`:

```markdown
> **Superseded (2026-10-07).** The broker-style flow described here (`/oauth2/session-init`, Redis-only relay token) was replaced by the standard OIDC provider in `2026-10-07-openwebui-oidc-provider-design.md`. Open WebUI now needs no custom callback route. Kept for history.
```

In `2026-10-07-openwebui-oidc-provider-design.md`:
- §2.2: add "code 교환은 HybridCache의 GET → DEL 두 단계라 Redis에서 원자적이지 않다. 같은 code를 수 ms 안에 두 번 교환하면 둘 다 성공할 수 있다. Open WebUI는 code를 한 번만 교환하고 code는 120초 뒤 사라지므로 수용한다."
- §4 `email_required` row: the profile button goes to `/profile` without a `redirect` parameter; after registering an email the user opens Open WebUI again (its auto-redirect restarts the flow).
- §2.4: option names for keys are `OIDCSigningKeyCurrent` / `OIDCSigningKeyPrevious` (not `oauth2.signing_*`), hidden by the `Key` suffix rule; status via `GET /api/oauth2/signing-key-status`.
- §6.2: the 401 `WWW-Authenticate` header value is `Basic realm="oauth2"`.
- Add to §9 (범위 밖) nothing new; confirm §7 cleanup list matches what was deleted.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-08-16-oauth2-provider-design.md docs/superpowers/specs/2026-08-16-oauth2-openwebui-integration-handoff.md docs/superpowers/specs/2026-10-07-openwebui-oidc-provider-design.md
git commit -m "docs(oidc): mark broker specs superseded and align OIDC spec with implementation

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```


- [ ] **Step 6: Hand off**

Use `superpowers:finishing-a-development-branch`.
