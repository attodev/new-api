# Open WebUI Relay JWT Substitution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Open WebUI call new-api with one shared service key plus a per-request HS256 user JWT, and have new-api bill each request to that user's own `openwebui` / `openwebui-task` token.

**Architecture:** `TokenAuth()` gains one substitution point: after the arriving token is validated and its IP limits checked, if the key is the configured Open WebUI service key, `service/openwebui.Resolve()` verifies the JWT, maps `sub` → new-api user (auto-linking by email on first sight), provisions or reuses the user's token row, and returns it so the rest of the existing auth/billing/log pipeline runs unchanged. Mapping lives in a new `openwebui_user_links` table; admin API + UI manage links manually.

**Tech Stack:** Go 1.22 / Gin / GORM, `golang-jwt/jwt/v5`, `golang.org/x/sync/singleflight`, `pkg/cachex.HybridCache` (memory + Redis), SQLite/MySQL/PostgreSQL; React 19 + TanStack Query + react-hook-form + zod in `web/default`, Bun.

Spec: `docs/superpowers/specs/2026-10-07-openwebui-relay-jwt-design.md`

## Global Constraints

- JSON: only `common.Marshal` / `common.Unmarshal` / `common.DecodeJson` (CLAUDE.md Rule 1). `encoding/json` types may be referenced; its functions may not be called in business code.
- DB: every query must work on SQLite, MySQL ≥ 5.7.8 and PostgreSQL ≥ 9.6. Use GORM methods; `LOWER(email)` is the only raw SQL function used and exists on all three.
- Frontend package manager/runner: `bun` (CLAUDE.md Rule 3).
- Never modify or remove any `new-api` / `QuantumNous` branding, license headers or attribution (CLAUDE.md Rule 5).
- Backend i18n: every new key goes into **both** `i18n/locales/en.yaml` and `i18n/locales/ko.yaml` (the test `TestKoreanLocaleCoversEnglishKeys` fails otherwise).
- Frontend i18n: every new `t('...')` string goes into **both** `web/default/src/i18n/locales/en.json` (value = key) and `kr.json` (Korean).
- Option keys ending in `secret`, `Secret`, `Key`, `api_key`, `Token` are **hidden** from `GET /api/option/` (see `controller/option.go:100-107`). The three secrets below are therefore write-only from the UI; an explicit status endpoint reports whether they are set.
- Users see only two reject messages (`openwebui.auth_failed`, `openwebui.not_linked`). Detailed reasons go to server logs only. Never echo `sub`, email or any secret in a response body.
- Commit messages end with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Run Go tests with `go test ./<pkg>/... -count=1`. Frontend: `cd web/default && bun run typecheck && bun run lint`.

## File Structure

**Backend — new**

| File | Responsibility |
| --- | --- |
| `setting/system_setting/openwebui.go` | `OpenWebUISettings` registered as `openwebui.*`; `NormalizeOpenWebUIServiceKey` |
| `model/openwebui_link.go` | `OpenWebUILink` table + CRUD + `FindUsersByEmailFold` |
| `model/openwebui_token.go` | `FindOldestUserTokenByName`, `CreateOpenWebUIToken` |
| `service/openwebui/jwt.go` | HS256-only JWT verification with two secrets |
| `service/openwebui/cache.go` | `sub → user_id` and `(user,name) → token key` hybrid caches, `InvalidateSub`, `ResetCachesForTest` |
| `service/openwebui/link.go` | `ResolveUserId`: cache → DB → email auto-link; `LinkError` reasons |
| `service/openwebui/provision.go` | `GetOrCreateUserToken` with singleflight |
| `service/openwebui/context.go` | gin context setters/getters for sub/task/attributed/unsigned |
| `service/openwebui/resolve.go` | `IsServiceKey`, `Resolve`, `RejectError` — the single entry point for middleware |
| `controller/openwebui_link.go` | Admin API: relay status, get/create/delete link |
| `middleware/auth_openwebui_test.go` | Integration tests of the `TokenAuth()` hook |

**Backend — modified**

| File | Change |
| --- | --- |
| `constant/context_key.go` | 4 new context keys |
| `i18n/keys.go`, `i18n/locales/en.yaml`, `i18n/locales/ko.yaml` | new messages |
| `model/main.go` | add `&OpenWebUILink{}` to both migration lists |
| `middleware/auth.go` | substitution hook in `TokenAuth()` (after IP check, before `GetUserCache`) |
| `service/log_info_generate.go` | `appendOpenWebUIInfo` into `GenerateTextOtherInfo` |
| `controller/option.go` | validation for `openwebui.*` keys in `UpdateOption` |
| `router/api-router.go` | `/api/openwebui/*` admin routes |

**Frontend (`web/default/src`) — new**

| File | Responsibility |
| --- | --- |
| `features/system-settings/site/openwebui-relay-section.tsx` | Relay settings form |
| `features/users/components/user-openwebui-link-section.tsx` | Link view/link/unlink block in the user drawer |

**Frontend — modified**

| File | Change |
| --- | --- |
| `features/system-settings/types.ts`, `site/index.tsx`, `site/section-registry.tsx` | new `openwebui.*` settings keys + section |
| `features/system-settings/api.ts` | `getOpenWebUIRelayStatus` |
| `features/users/api.ts`, `features/users/types.ts` | link API + types |
| `features/users/components/users-mutate-drawer.tsx` | render the link section when updating |
| `i18n/locales/en.json`, `i18n/locales/kr.json` | new strings |

---

### Task 1: Settings, context keys and i18n messages

**Files:**
- Create: `setting/system_setting/openwebui.go`
- Create: `setting/system_setting/openwebui_test.go`
- Modify: `constant/context_key.go` (append inside the `const` block, after `ContextKeyIsStream`)
- Modify: `i18n/keys.go` (append a new const block at the end of file)
- Modify: `i18n/locales/en.yaml`, `i18n/locales/ko.yaml` (append at end)

**Interfaces:**
- Produces: `system_setting.OpenWebUISettings{RelayEnabled bool; ServiceApiKey, JwtSecret, JwtPreviousSecret, JwtIssuer string; JwtLeewaySeconds int; AllowUnsignedRequests bool}`, `system_setting.GetOpenWebUISettings() *OpenWebUISettings`, `system_setting.NormalizeOpenWebUIServiceKey(raw string) string`
- Produces: `constant.ContextKeyOpenWebUISub`, `constant.ContextKeyOpenWebUITask`, `constant.ContextKeyOpenWebUIAttributed`, `constant.ContextKeyOpenWebUIUnsigned`
- Produces: i18n keys `i18n.MsgOpenWebUIAuthFailed`, `i18n.MsgOpenWebUINotLinked`, `i18n.MsgOpenWebUILinkConflict`, `i18n.MsgOpenWebUILinkNotFound`, `i18n.MsgOpenWebUISubRequired`, `i18n.MsgOptionOpenWebUIRelayConfigMissing`, `i18n.MsgOptionOpenWebUIServiceKeyInvalid`, `i18n.MsgOptionOpenWebUISecretTooShort`

- [ ] **Step 1: Write the failing settings test**

`setting/system_setting/openwebui_test.go`:

```go
package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestOpenWebUISettings_Defaults(t *testing.T) {
	s := GetOpenWebUISettings()
	require.False(t, s.RelayEnabled)
	require.Equal(t, "", s.ServiceApiKey)
	require.Equal(t, "open-webui", s.JwtIssuer)
	require.Equal(t, 60, s.JwtLeewaySeconds)
	require.True(t, s.AllowUnsignedRequests)
}

func TestOpenWebUISettings_RegisteredUnderOpenWebUIName(t *testing.T) {
	cfg := config.GlobalConfig.Get("openwebui")
	require.NotNil(t, cfg)
	_, ok := cfg.(*OpenWebUISettings)
	require.True(t, ok)
}

func TestNormalizeOpenWebUIServiceKey(t *testing.T) {
	require.Equal(t, "abc123", NormalizeOpenWebUIServiceKey("  sk-abc123 "))
	require.Equal(t, "abc123", NormalizeOpenWebUIServiceKey("abc123"))
	require.Equal(t, "", NormalizeOpenWebUIServiceKey("sk-"))
	require.Equal(t, "", NormalizeOpenWebUIServiceKey("   "))
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./setting/system_setting/... -run 'OpenWebUI' -count=1`
Expected: FAIL — `undefined: GetOpenWebUISettings`, `undefined: NormalizeOpenWebUIServiceKey`

- [ ] **Step 3: Implement the settings file**

`setting/system_setting/openwebui.go`:

```go
package system_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// OpenWebUISettings configures the Open WebUI relay integration: Open WebUI
// sends every user's request with one shared service key plus a per-request
// HS256 JWT (X-OpenWebUI-User-Jwt) that identifies the person. new-api
// verifies that JWT and bills the request to that person's own token.
//
// ServiceApiKey, JwtSecret and JwtPreviousSecret are intentionally named so
// controller.GetOptions hides them (suffixes "api_key" / "secret").
type OpenWebUISettings struct {
	RelayEnabled bool `json:"relay_enabled"`
	// ServiceApiKey is the key of the token Open WebUI uses as OPENAI_API_KEYS,
	// stored without the "sk-" prefix (the same form TokenAuth parses).
	ServiceApiKey string `json:"service_api_key"`
	// JwtSecret is Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET.
	JwtSecret string `json:"jwt_secret"`
	// JwtPreviousSecret is accepted alongside JwtSecret during rotation.
	JwtPreviousSecret string `json:"jwt_previous_secret"`
	// JwtIssuer is the expected "iss" claim. Open WebUI always sends "open-webui".
	JwtIssuer string `json:"jwt_issuer"`
	// JwtLeewaySeconds is the clock-skew allowance applied to exp and iat.
	JwtLeewaySeconds int `json:"jwt_leeway_seconds"`
	// AllowUnsignedRequests bills requests that carry the service key but no
	// JWT to the service token itself instead of rejecting them with 401.
	AllowUnsignedRequests bool `json:"allow_unsigned_requests"`
}

var defaultOpenWebUISettings = OpenWebUISettings{
	JwtIssuer:             "open-webui",
	JwtLeewaySeconds:      60,
	AllowUnsignedRequests: true,
}

func init() {
	config.GlobalConfig.Register("openwebui", &defaultOpenWebUISettings)
}

func GetOpenWebUISettings() *OpenWebUISettings {
	return &defaultOpenWebUISettings
}

// NormalizeOpenWebUIServiceKey turns whatever the admin pasted into the form
// TokenAuth compares against: trimmed and without the "sk-" prefix.
func NormalizeOpenWebUIServiceKey(raw string) string {
	key := strings.TrimSpace(raw)
	key = strings.TrimPrefix(key, "sk-")
	return strings.TrimSpace(key)
}
```

- [ ] **Step 4: Run the settings tests**

Run: `go test ./setting/system_setting/... -run 'OpenWebUI' -count=1`
Expected: PASS

- [ ] **Step 5: Add context keys**

In `constant/context_key.go`, inside the `const (...)` block, after the line `ContextKeyIsStream ContextKey = "is_stream"` add:

```go

	/* Open WebUI relay keys (set by service/openwebui.Resolve) */
	ContextKeyOpenWebUISub        ContextKey = "openwebui_sub"
	ContextKeyOpenWebUITask       ContextKey = "openwebui_task"
	ContextKeyOpenWebUIAttributed ContextKey = "openwebui_attributed"
	ContextKeyOpenWebUIUnsigned   ContextKey = "openwebui_unsigned"
```

- [ ] **Step 6: Add i18n keys**

Append to the end of `i18n/keys.go`:

```go

// Open WebUI relay integration
const (
	MsgOpenWebUIAuthFailed   = "openwebui.auth_failed"
	MsgOpenWebUINotLinked    = "openwebui.not_linked"
	MsgOpenWebUILinkConflict = "openwebui.link_conflict"
	MsgOpenWebUILinkNotFound = "openwebui.link_not_found"
	MsgOpenWebUISubRequired  = "openwebui.sub_required"

	MsgOptionOpenWebUIRelayConfigMissing = "option.openwebui_relay_config_missing"
	MsgOptionOpenWebUIServiceKeyInvalid  = "option.openwebui_service_key_invalid"
	MsgOptionOpenWebUISecretTooShort     = "option.openwebui_secret_too_short"
)
```

Append to the end of `i18n/locales/en.yaml`:

```yaml

# Open WebUI relay integration
openwebui.auth_failed: "Open WebUI authentication failed, please contact your administrator"
openwebui.not_linked: "Your Open WebUI account is not linked to a new-api account"
openwebui.link_conflict: "This Open WebUI account or new-api user is already linked"
openwebui.link_not_found: "Open WebUI link not found"
openwebui.sub_required: "Open WebUI user id (sub) is required"
option.openwebui_relay_config_missing: "Cannot enable the Open WebUI relay, please set the service key and a JWT secret of at least 32 characters first"
option.openwebui_service_key_invalid: "The Open WebUI service key does not match an enabled token"
option.openwebui_secret_too_short: "The Open WebUI JWT secret must be at least 32 characters"
```

Append to the end of `i18n/locales/ko.yaml`:

```yaml

# Open WebUI relay integration
openwebui.auth_failed: "Open WebUI 인증에 실패했습니다. 관리자에게 문의해 주세요"
openwebui.not_linked: "Open WebUI 계정이 new-api 계정에 연결되어 있지 않습니다"
openwebui.link_conflict: "이 Open WebUI 계정 또는 new-api 사용자는 이미 연결되어 있습니다"
openwebui.link_not_found: "Open WebUI 연결을 찾을 수 없습니다"
openwebui.sub_required: "Open WebUI 사용자 ID(sub)가 필요합니다"
option.openwebui_relay_config_missing: "Open WebUI relay를 활성화하려면 먼저 서비스 키와 32자 이상의 JWT 비밀을 설정해 주세요"
option.openwebui_service_key_invalid: "Open WebUI 서비스 키가 활성화된 토큰과 일치하지 않습니다"
option.openwebui_secret_too_short: "Open WebUI JWT 비밀은 32자 이상이어야 합니다"
```

- [ ] **Step 7: Run i18n and build checks**

Run: `go test ./i18n/... -count=1 && go build ./...`
Expected: PASS (ko coverage test passes) and build succeeds.

- [ ] **Step 8: Commit**

```bash
git add setting/system_setting/openwebui.go setting/system_setting/openwebui_test.go constant/context_key.go i18n/keys.go i18n/locales/en.yaml i18n/locales/ko.yaml
git commit -m "feat(openwebui): add relay settings, context keys and messages

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Mapping table and token helpers (model layer)

**Files:**
- Create: `model/openwebui_link.go`
- Create: `model/openwebui_token.go`
- Create: `model/openwebui_link_test.go`
- Modify: `model/main.go` — add `&OpenWebUILink{},` as the last entry of the `DB.AutoMigrate(` list in `migrateDB()` and `{&OpenWebUILink{}, "OpenWebUILink"},` as the last entry of the `migrations` slice in `migrateDBFast()`

**Interfaces:**
- Produces: `model.OpenWebUILink{Id int; Sub string; UserId int; Email string; LinkedBy string; CreatedAt int64}`, constants `model.OpenWebUILinkedByAuto = "auto"`, `model.OpenWebUILinkedByAdmin = "admin"`, `model.ErrOpenWebUILinkConflict`
- Produces: `model.GetOpenWebUILinkBySub(sub string) (*OpenWebUILink, error)`, `model.GetOpenWebUILinkByUserId(userId int) (*OpenWebUILink, error)`, `model.GetOpenWebUILinkById(id int) (*OpenWebUILink, error)`, `model.CreateOpenWebUILink(link *OpenWebUILink) error`, `model.DeleteOpenWebUILink(id int) error`, `model.FindUsersByEmailFold(email string) ([]*User, error)`
- Produces: `model.FindOldestUserTokenByName(userId int, name string) (*Token, error)`, `model.CreateOpenWebUIToken(userId int, name string) (*Token, error)`
- Not-found errors are `gorm.ErrRecordNotFound` (callers use `errors.Is`).

- [ ] **Step 1: Write the failing model tests**

`model/openwebui_link_test.go`:

```go
package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOpenWebUIModelTestDB(t *testing.T) {
	t.Helper()
	originalDB := DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &OpenWebUILink{}))
	DB = db

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
	})
}

func TestCreateOpenWebUILink_RejectsDuplicateSubOrUser(t *testing.T) {
	setupOpenWebUIModelTestDB(t)

	first := &OpenWebUILink{Sub: "owui-1", UserId: 7, Email: "a@example.com", LinkedBy: OpenWebUILinkedByAuto}
	require.NoError(t, CreateOpenWebUILink(first))
	require.NotZero(t, first.Id)
	require.NotZero(t, first.CreatedAt)

	sameSub := &OpenWebUILink{Sub: "owui-1", UserId: 8, LinkedBy: OpenWebUILinkedByAdmin}
	require.ErrorIs(t, CreateOpenWebUILink(sameSub), ErrOpenWebUILinkConflict)

	sameUser := &OpenWebUILink{Sub: "owui-2", UserId: 7, LinkedBy: OpenWebUILinkedByAdmin}
	require.ErrorIs(t, CreateOpenWebUILink(sameUser), ErrOpenWebUILinkConflict)

	got, err := GetOpenWebUILinkBySub("owui-1")
	require.NoError(t, err)
	require.Equal(t, 7, got.UserId)

	byUser, err := GetOpenWebUILinkByUserId(7)
	require.NoError(t, err)
	require.Equal(t, "owui-1", byUser.Sub)

	_, err = GetOpenWebUILinkBySub("missing")
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestDeleteOpenWebUILink(t *testing.T) {
	setupOpenWebUIModelTestDB(t)
	link := &OpenWebUILink{Sub: "owui-del", UserId: 9, LinkedBy: OpenWebUILinkedByAdmin}
	require.NoError(t, CreateOpenWebUILink(link))

	require.NoError(t, DeleteOpenWebUILink(link.Id))
	_, err := GetOpenWebUILinkById(link.Id)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
	require.True(t, errors.Is(DeleteOpenWebUILink(link.Id), gorm.ErrRecordNotFound))
}

func TestFindUsersByEmailFold_IgnoresCaseAndSoftDeleted(t *testing.T) {
	setupOpenWebUIModelTestDB(t)
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "x", Email: "Alice@Example.com", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "ghost", Password: "x", Email: "ghost@example.com", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Delete(&User{}, 2).Error) // soft delete

	users, err := FindUsersByEmailFold("alice@example.COM")
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, 1, users[0].Id)
	require.Empty(t, users[0].Password, "password must be omitted")

	deleted, err := FindUsersByEmailFold("ghost@example.com")
	require.NoError(t, err)
	require.Empty(t, deleted)

	none, err := FindUsersByEmailFold("   ")
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestCreateOpenWebUIToken_AndFindOldest(t *testing.T) {
	setupOpenWebUIModelTestDB(t)

	_, err := FindOldestUserTokenByName(7, "openwebui")
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))

	first, err := CreateOpenWebUIToken(7, "openwebui")
	require.NoError(t, err)
	require.Len(t, first.Key, 48)
	require.True(t, first.UnlimitedQuota)
	require.Equal(t, int64(-1), first.ExpiredTime)
	require.Equal(t, common.TokenStatusEnabled, first.Status)
	require.Equal(t, "", first.Group)
	require.False(t, first.ModelLimitsEnabled)

	second, err := CreateOpenWebUIToken(7, "openwebui")
	require.NoError(t, err)
	require.Greater(t, second.Id, first.Id)

	oldest, err := FindOldestUserTokenByName(7, "openwebui")
	require.NoError(t, err)
	require.Equal(t, first.Id, oldest.Id)

	// Soft-deleted tokens are not returned.
	require.NoError(t, first.Delete())
	oldest, err = FindOldestUserTokenByName(7, "openwebui")
	require.NoError(t, err)
	require.Equal(t, second.Id, oldest.Id)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./model/... -run 'OpenWebUI|FindUsersByEmailFold' -count=1`
Expected: FAIL — `undefined: OpenWebUILink` etc.

- [ ] **Step 3: Implement the link model**

`model/openwebui_link.go`:

```go
package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	OpenWebUILinkedByAuto  = "auto"
	OpenWebUILinkedByAdmin = "admin"
)

// ErrOpenWebUILinkConflict is returned when the sub or the user is already
// linked. One Open WebUI account bills exactly one new-api user and vice versa.
var ErrOpenWebUILinkConflict = errors.New("openwebui link conflict: sub or user already linked")

// OpenWebUILink maps an Open WebUI user id (the JWT "sub" claim) to a new-api
// user. Email is the JWT email at link time and is kept for auditing only —
// lookups after the first request go through Sub exclusively.
type OpenWebUILink struct {
	Id        int    `json:"id"`
	Sub       string `json:"sub" gorm:"type:varchar(191);uniqueIndex"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex"`
	Email     string `json:"email" gorm:"type:varchar(191)"`
	LinkedBy  string `json:"linked_by" gorm:"type:varchar(16)"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (OpenWebUILink) TableName() string {
	return "openwebui_user_links"
}

func GetOpenWebUILinkBySub(sub string) (*OpenWebUILink, error) {
	if sub == "" {
		return nil, errors.New("sub is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("sub = ?", sub).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func GetOpenWebUILinkByUserId(userId int) (*OpenWebUILink, error) {
	if userId == 0 {
		return nil, errors.New("user id is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("user_id = ?", userId).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func GetOpenWebUILinkById(id int) (*OpenWebUILink, error) {
	if id == 0 {
		return nil, errors.New("id is empty")
	}
	var link OpenWebUILink
	if err := DB.Where("id = ?", id).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func countOpenWebUILinkConflicts(sub string, userId int) (int64, error) {
	var count int64
	err := DB.Model(&OpenWebUILink{}).Where("sub = ? OR user_id = ?", sub, userId).Count(&count).Error
	return count, err
}

// CreateOpenWebUILink inserts a link, reporting ErrOpenWebUILinkConflict when
// either side is already taken. The pre-check keeps the common case free of
// driver-specific duplicate-key errors; the post-insert re-check covers the
// race where two first requests for the same person arrive together.
func CreateOpenWebUILink(link *OpenWebUILink) error {
	if link == nil || link.Sub == "" || link.UserId == 0 {
		return errors.New("sub and user_id are required")
	}
	if link.LinkedBy == "" {
		link.LinkedBy = OpenWebUILinkedByAuto
	}
	count, err := countOpenWebUILinkConflicts(link.Sub, link.UserId)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrOpenWebUILinkConflict
	}
	link.CreatedAt = common.GetTimestamp()
	if err := DB.Create(link).Error; err != nil {
		if again, checkErr := countOpenWebUILinkConflicts(link.Sub, link.UserId); checkErr == nil && again > 0 {
			return ErrOpenWebUILinkConflict
		}
		return err
	}
	return nil
}

func DeleteOpenWebUILink(id int) error {
	if id == 0 {
		return errors.New("id is empty")
	}
	result := DB.Where("id = ?", id).Delete(&OpenWebUILink{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FindUsersByEmailFold returns the non-deleted users whose email matches
// case-insensitively. Open WebUI stores emails lower-cased, so this is the
// comparison the auto-link uses. LOWER() exists on SQLite, MySQL and PostgreSQL.
func FindUsersByEmailFold(email string) ([]*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, nil
	}
	var users []*User
	err := DB.Omit("password").Where("LOWER(email) = ?", email).Find(&users).Error
	return users, err
}
```

- [ ] **Step 4: Implement the token helpers**

`model/openwebui_token.go`:

```go
package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// FindOldestUserTokenByName returns the user's lowest-id, non-deleted token
// with that name. When a race created two rows, every caller converges on
// the same one.
func FindOldestUserTokenByName(userId int, name string) (*Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("user id and name are required")
	}
	var token Token
	err := DB.Where("user_id = ? AND name = ?", userId, name).Order("id ASC").First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// CreateOpenWebUIToken creates the per-user token the Open WebUI relay bills
// to: unlimited token quota (so the user's wallet is charged), no expiry, no
// group pin (follows the user's group), no model limits. It deliberately
// bypasses the per-user token count limit because it is system-provisioned.
func CreateOpenWebUIToken(userId int, name string) (*Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("user id and name are required")
	}
	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	token := &Token{
		UserId:         userId,
		Name:           name,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    now,
		AccessedTime:   now,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}
	if err := token.Insert(); err != nil {
		return nil, err
	}
	return token, nil
}
```

- [ ] **Step 5: Register the migration**

In `model/main.go`:
- In `migrateDB()`, inside `err := DB.AutoMigrate(` add `&OpenWebUILink{},` as the last entry before the closing `)`.
- In `migrateDBFast()`, in the `migrations` slice add `{&OpenWebUILink{}, "OpenWebUILink"},` as the last entry (after `{&PerfMetric{}, "PerfMetric"},`).

- [ ] **Step 6: Run the model tests**

Run: `go test ./model/... -run 'OpenWebUI|FindUsersByEmailFold' -count=1 && go build ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add model/openwebui_link.go model/openwebui_token.go model/openwebui_link_test.go model/main.go
git commit -m "feat(openwebui): add user link table and per-user token helpers

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: JWT verification

**Files:**
- Create: `service/openwebui/jwt.go`
- Create: `service/openwebui/jwt_test.go`

**Interfaces:**
- Produces: `openwebui.UserClaims{Email, Name, Role string; jwt.RegisteredClaims}` (use `claims.Subject` for `sub`)
- Produces: `openwebui.JWTConfig{Secret, PreviousSecret, Issuer string; Leeway time.Duration}`
- Produces: `openwebui.VerifyUserJWT(raw string, cfg JWTConfig) (*UserClaims, error)`
- Produces: sentinel errors `ErrJWTEmptySecret`, `ErrJWTMalformed`, `ErrJWTBadSignature`, `ErrJWTExpired`, `ErrJWTNotYetValid`, `ErrJWTNoExp`, `ErrJWTBadIssuer`, `ErrJWTNoSub`

- [ ] **Step 1: Write the failing JWT tests**

`service/openwebui/jwt_test.go`:

```go
package openwebui

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const (
	testSecret     = "0123456789abcdef0123456789abcdef"
	testOldSecret  = "fedcba9876543210fedcba9876543210"
	testWrongKey   = "wrongwrongwrongwrongwrongwrongwr"
	testIssuer     = "open-webui"
	testLeeway     = 60 * time.Second
	testSubject    = "7f3c2a1e"
	testEmail      = "kim@example.com"
)

func testConfig() JWTConfig {
	return JWTConfig{Secret: testSecret, PreviousSecret: testOldSecret, Issuer: testIssuer, Leeway: testLeeway}
}

func signHS256(t *testing.T, secret string, claims jwt.Claims) string {
	t.Helper()
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return raw
}

func baseClaims(now time.Time) UserClaims {
	return UserClaims{
		Email: testEmail,
		Name:  "Kim",
		Role:  "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testSubject,
			Issuer:    testIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(300 * time.Second)),
		},
	}
}

func TestVerifyUserJWT_Valid(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now()))
	claims, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
	require.Equal(t, testSubject, claims.Subject)
	require.Equal(t, testEmail, claims.Email)
	require.Equal(t, "user", claims.Role)
}

func TestVerifyUserJWT_AcceptsPreviousSecret(t *testing.T) {
	raw := signHS256(t, testOldSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_RejectsWrongSecret(t *testing.T) {
	raw := signHS256(t, testWrongKey, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_PreviousSecretIgnoredWhenEmpty(t *testing.T) {
	cfg := testConfig()
	cfg.PreviousSecret = ""
	raw := signHS256(t, testOldSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, cfg)
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsExpired(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(-10*time.Minute)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTExpired)
}

func TestVerifyUserJWT_ExpiredWithinLeewayPasses(t *testing.T) {
	// exp 30s in the past, leeway 60s → accepted
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(-330*time.Second)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_RejectsMissingExp(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.ExpiresAt = nil
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNoExp)
}

func TestVerifyUserJWT_RejectsWrongIssuer(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Issuer = "someone-else"
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadIssuer)
}

func TestVerifyUserJWT_IssuedAtAheadWithinLeewayPasses(t *testing.T) {
	// Open WebUI clock 30s ahead of ours.
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(30*time.Second)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}

func TestVerifyUserJWT_IssuedAtBeyondLeewayRejected(t *testing.T) {
	raw := signHS256(t, testSecret, baseClaims(time.Now().Add(5*time.Minute)))
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNotYetValid)
}

func TestVerifyUserJWT_RejectsAlgNone(t *testing.T) {
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, baseClaims(time.Now())).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	_, err = VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, baseClaims(time.Now())).SignedString(key)
	require.NoError(t, err)
	_, err = VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTBadSignature)
}

func TestVerifyUserJWT_RejectsMissingSub(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Subject = ""
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.ErrorIs(t, err, ErrJWTNoSub)
}

func TestVerifyUserJWT_RejectsMalformed(t *testing.T) {
	_, err := VerifyUserJWT("not.a.jwt", testConfig())
	require.ErrorIs(t, err, ErrJWTMalformed)
}

func TestVerifyUserJWT_EmptySecret(t *testing.T) {
	cfg := testConfig()
	cfg.Secret = ""
	raw := signHS256(t, testSecret, baseClaims(time.Now()))
	_, err := VerifyUserJWT(raw, cfg)
	require.ErrorIs(t, err, ErrJWTEmptySecret)
}

func TestVerifyUserJWT_AudiencePresentIsIgnored(t *testing.T) {
	claims := baseClaims(time.Now())
	claims.Audience = jwt.ClaimStrings{"anything"}
	raw := signHS256(t, testSecret, claims)
	_, err := VerifyUserJWT(raw, testConfig())
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/openwebui/... -count=1`
Expected: FAIL — package has no non-test Go files / undefined identifiers.

- [ ] **Step 3: Implement jwt.go**

`service/openwebui/jwt.go`:

```go
// Package openwebui implements the Open WebUI relay integration: verifying
// the per-request user JWT Open WebUI attaches to every call made with the
// shared service key, mapping it to a new-api user, and substituting that
// user's own token so the existing billing pipeline charges the right person.
package openwebui

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// UserClaims is the payload Open WebUI's _mint_forward_user_jwt() produces.
// The subject is the Open WebUI user id; it is the only stable identity key.
type UserClaims struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

type JWTConfig struct {
	Secret         string
	PreviousSecret string
	Issuer         string
	Leeway         time.Duration
}

var (
	ErrJWTEmptySecret  = errors.New("openwebui: jwt secret not configured")
	ErrJWTMalformed    = errors.New("openwebui: jwt malformed")
	ErrJWTBadSignature = errors.New("openwebui: jwt signature or algorithm invalid")
	ErrJWTExpired      = errors.New("openwebui: jwt expired")
	ErrJWTNotYetValid  = errors.New("openwebui: jwt issued in the future")
	ErrJWTNoExp        = errors.New("openwebui: jwt missing exp")
	ErrJWTBadIssuer    = errors.New("openwebui: jwt issuer mismatch")
	ErrJWTNoSub        = errors.New("openwebui: jwt missing sub")
)

// VerifyUserJWT validates raw against cfg.Secret and, on a signature
// failure only, against cfg.PreviousSecret (zero-downtime rotation).
// Only HS256 is accepted; exp is required; iss must match; leeway applies to
// exp and iat; aud and jti are not required (Open WebUI does not send them).
func VerifyUserJWT(raw string, cfg JWTConfig) (*UserClaims, error) {
	if cfg.Secret == "" {
		return nil, ErrJWTEmptySecret
	}
	claims, err := parseWithSecret(raw, cfg, cfg.Secret)
	if errors.Is(err, ErrJWTBadSignature) && cfg.PreviousSecret != "" {
		claims, err = parseWithSecret(raw, cfg, cfg.PreviousSecret)
	}
	if err != nil {
		return nil, err
	}
	if claims.Subject == "" {
		return nil, ErrJWTNoSub
	}
	return claims, nil
}

func parseWithSecret(raw string, cfg JWTConfig, secret string) (*UserClaims, error) {
	claims := &UserClaims{}
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(cfg.Leeway),
	)
	_, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, mapJWTError(err)
	}
	return claims, nil
}

// mapJWTError collapses jwt/v5's joined errors into our sentinels. Order
// matters: a token can be both expired and badly signed, and the signature
// verdict must win so the previous-secret retry happens.
func mapJWTError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return ErrJWTBadSignature
	case errors.Is(err, jwt.ErrTokenMalformed):
		return ErrJWTMalformed
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return ErrJWTNoExp
	case errors.Is(err, jwt.ErrTokenExpired):
		return ErrJWTExpired
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued), errors.Is(err, jwt.ErrTokenNotValidYet):
		return ErrJWTNotYetValid
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return ErrJWTBadIssuer
	}
	return ErrJWTMalformed
}
```

- [ ] **Step 4: Run the JWT tests**

Run: `go test ./service/openwebui/... -count=1 -v`
Expected: all `TestVerifyUserJWT_*` PASS. If `TestVerifyUserJWT_RejectsAlgNone` or `RejectsRS256` fail with a different sentinel, inspect the error text printed by adding `t.Log(err)`; jwt/v5 reports a disallowed `alg` as `ErrTokenSignatureInvalid` ("signing method ... is invalid"), so the mapping above is what the library produces.

- [ ] **Step 5: Commit**

```bash
git add service/openwebui/jwt.go service/openwebui/jwt_test.go
git commit -m "feat(openwebui): verify Open WebUI user JWTs with HS256 and secret rotation

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Caches and `sub → user` resolution

**Files:**
- Create: `service/openwebui/cache.go`
- Create: `service/openwebui/link.go`
- Create: `service/openwebui/link_test.go`

**Interfaces:**
- Consumes: `model.GetOpenWebUILinkBySub`, `model.CreateOpenWebUILink`, `model.ErrOpenWebUILinkConflict`, `model.FindUsersByEmailFold`, `model.GetUserCache`, `model.OpenWebUILinkedByAuto` (Task 2); `UserClaims` (Task 3)
- Produces: `openwebui.ResolveUserId(claims *UserClaims) (int, error)`; `openwebui.LinkError{Reason string}` with reasons `ReasonNoUser="no_user"`, `ReasonAmbiguousEmail="ambiguous_email"`, `ReasonUserAlreadyLinked="user_already_linked"`, `ReasonUserMissing="user_missing"`
- Produces: `openwebui.InvalidateSub(sub string)`, `openwebui.ResetCachesForTest()`, unexported `subCache()`, `tokenKeyCache()`, `cacheTTL`

- [ ] **Step 1: Write the failing link tests**

`service/openwebui/link_test.go`:

```go
package openwebui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupServiceTestDB gives the openwebui package a real SQLite database with
// the three tables it touches and disables Redis so every cache falls back
// to memory. Caches are reset so tests never see each other's entries.
func setupServiceTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	ResetCachesForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.OpenWebUILink{}))
	model.DB = db

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		ResetCachesForTest()
	})
}

func createTestUser(t *testing.T, id int, username, email string) {
	t.Helper()
	// users.aff_code has a unique index, so every fixture user needs its own.
	require.NoError(t, model.DB.Create(&model.User{
		Id: id, Username: username, Password: "secret-pass", Email: email, AffCode: "aff-" + username,
		Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Group: "default",
	}).Error)
}

func claimsFor(sub, email string) *UserClaims {
	return &UserClaims{Email: email, RegisteredClaims: jwt.RegisteredClaims{Subject: sub}}
}

func TestResolveUserId_UsesExistingLink(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-7", UserId: 7}))

	// Email deliberately different: once linked, only sub matters.
	userId, err := ResolveUserId(claimsFor("owui-7", "changed@example.com"))
	require.NoError(t, err)
	require.Equal(t, 7, userId)

	// Second call served from cache: delete the row and resolve again.
	link, err := model.GetOpenWebUILinkBySub("owui-7")
	require.NoError(t, err)
	require.NoError(t, model.DeleteOpenWebUILink(link.Id))
	userId, err = ResolveUserId(claimsFor("owui-7", "changed@example.com"))
	require.NoError(t, err)
	require.Equal(t, 7, userId)

	// After invalidation the DB is consulted and the sub is gone → auto-link by email fails.
	InvalidateSub("owui-7")
	_, err = ResolveUserId(claimsFor("owui-7", "nobody@example.com"))
	var linkErr *LinkError
	require.True(t, errors.As(err, &linkErr))
	require.Equal(t, ReasonNoUser, linkErr.Reason)
}

func TestResolveUserId_AutoLinksByEmailCaseInsensitive(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "Alice@Example.com")

	userId, err := ResolveUserId(claimsFor("owui-new", "alice@example.COM"))
	require.NoError(t, err)
	require.Equal(t, 7, userId)

	link, err := model.GetOpenWebUILinkBySub("owui-new")
	require.NoError(t, err)
	require.Equal(t, 7, link.UserId)
	require.Equal(t, "alice@example.com", link.Email)
	require.Equal(t, model.OpenWebUILinkedByAuto, link.LinkedBy)
}

func TestResolveUserId_Failures(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")
	createTestUser(t, 8, "dup1", "dup@example.com")
	createTestUser(t, 9, "dup2", "DUP@example.com")
	createTestUser(t, 10, "bound", "bound@example.com")
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-old", UserId: 10}))
	createTestUser(t, 11, "gone", "gone@example.com")
	require.NoError(t, model.CreateOpenWebUILink(&model.OpenWebUILink{Sub: "owui-gone", UserId: 11}))
	require.NoError(t, model.DB.Delete(&model.User{}, 11).Error)

	cases := []struct {
		name   string
		claims *UserClaims
		reason string
	}{
		{"no email claim", claimsFor("owui-a", ""), ReasonNoUser},
		{"no user with email", claimsFor("owui-b", "nobody@example.com"), ReasonNoUser},
		{"ambiguous email", claimsFor("owui-c", "dup@example.com"), ReasonAmbiguousEmail},
		{"user already linked to another sub", claimsFor("owui-recreated", "bound@example.com"), ReasonUserAlreadyLinked},
		{"linked user deleted", claimsFor("owui-gone", "gone@example.com"), ReasonUserMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveUserId(tc.claims)
			var linkErr *LinkError
			require.True(t, errors.As(err, &linkErr), "got %v", err)
			require.Equal(t, tc.reason, linkErr.Reason)
		})
	}
}

func TestCacheTTLIsTenMinutes(t *testing.T) {
	require.Equal(t, 10*time.Minute, cacheTTL)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/openwebui/... -run 'ResolveUserId|CacheTTL' -count=1`
Expected: FAIL — `undefined: ResolveUserId`, `undefined: ResetCachesForTest`

- [ ] **Step 3: Implement cache.go**

`service/openwebui/cache.go`:

```go
package openwebui

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"

	"github.com/samber/hot"
)

// cacheTTL bounds how long a sub→user or (user,name)→token-key entry is
// trusted without touching the database. Admin link/unlink invalidates the
// sub entry explicitly, so the TTL only matters for out-of-band DB edits.
const cacheTTL = 10 * time.Minute

const (
	subCacheNamespace      = "openwebui:sub:v1"
	tokenKeyCacheNamespace = "openwebui:token_key:v1"
	cacheCapacity          = 10000
)

var (
	cacheMu           sync.Mutex
	subCacheInst      *cachex.HybridCache[int]
	tokenKeyCacheInst *cachex.HybridCache[string]
)

func redisOn() bool {
	return common.RedisEnabled && common.RDB != nil
}

// subCache maps Open WebUI sub → new-api user id.
func subCache() *cachex.HybridCache[int] {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if subCacheInst == nil {
		subCacheInst = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace:    cachex.Namespace(subCacheNamespace),
			Redis:        common.RDB,
			RedisEnabled: redisOn,
			RedisCodec:   cachex.IntCodec{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, cacheCapacity).WithTTL(cacheTTL).WithJanitor().Build()
			},
		})
	}
	return subCacheInst
}

// tokenKeyCache maps "<userId>:<tokenName>" → token key.
func tokenKeyCache() *cachex.HybridCache[string] {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if tokenKeyCacheInst == nil {
		tokenKeyCacheInst = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace:    cachex.Namespace(tokenKeyCacheNamespace),
			Redis:        common.RDB,
			RedisEnabled: redisOn,
			RedisCodec:   cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, cacheCapacity).WithTTL(cacheTTL).WithJanitor().Build()
			},
		})
	}
	return tokenKeyCacheInst
}

// InvalidateSub drops the cached mapping for sub. Called by the admin API
// after linking or unlinking so the next relay request re-reads the table.
func InvalidateSub(sub string) {
	if sub == "" {
		return
	}
	_, _ = subCache().DeleteMany([]string{sub})
}

// ResetCachesForTest discards both caches so tests start clean.
func ResetCachesForTest() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	subCacheInst = nil
	tokenKeyCacheInst = nil
}
```

- [ ] **Step 4: Implement link.go**

`service/openwebui/link.go`:

```go
package openwebui

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	ReasonNoUser            = "no_user"
	ReasonAmbiguousEmail    = "ambiguous_email"
	ReasonUserAlreadyLinked = "user_already_linked"
	ReasonUserMissing       = "user_missing"
)

// LinkError means the JWT was valid but could not be attributed to a new-api
// user. Reason is for server logs only; the client sees a generic 403.
type LinkError struct {
	Reason string
}

func (e *LinkError) Error() string {
	return "openwebui: link failed: " + e.Reason
}

// ResolveUserId maps a verified JWT to a new-api user id.
//
//  1. cached sub → user id
//  2. openwebui_user_links row by sub
//  3. first sight: exactly one non-deleted user with the same email
//     (case-insensitive) who is not already linked → create the link
//
// Any other outcome is a *LinkError. Non-link failures (DB errors) are
// returned as-is so the caller can answer 500 instead of 403.
func ResolveUserId(claims *UserClaims) (int, error) {
	if claims == nil || claims.Subject == "" {
		return 0, &LinkError{Reason: ReasonNoUser}
	}
	sub := claims.Subject

	if userId, found, err := subCache().Get(sub); err == nil && found && userId > 0 {
		return requireUserExists(userId)
	}

	link, err := model.GetOpenWebUILinkBySub(sub)
	if err == nil {
		_ = subCache().SetWithTTL(sub, link.UserId, cacheTTL)
		return requireUserExists(link.UserId)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	return autoLinkByEmail(claims)
}

func autoLinkByEmail(claims *UserClaims) (int, error) {
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return 0, &LinkError{Reason: ReasonNoUser}
	}
	users, err := model.FindUsersByEmailFold(email)
	if err != nil {
		return 0, err
	}
	switch len(users) {
	case 0:
		return 0, &LinkError{Reason: ReasonNoUser}
	case 1:
	default:
		return 0, &LinkError{Reason: ReasonAmbiguousEmail}
	}
	user := users[0]

	link := &model.OpenWebUILink{
		Sub:      claims.Subject,
		UserId:   user.Id,
		Email:    email,
		LinkedBy: model.OpenWebUILinkedByAuto,
	}
	if err := model.CreateOpenWebUILink(link); err != nil {
		if !errors.Is(err, model.ErrOpenWebUILinkConflict) {
			return 0, err
		}
		// Either a concurrent first request just linked this sub (fine —
		// use it), or this user is already bound to a different sub
		// (Open WebUI account was recreated → admin must unlink first).
		if existing, lookupErr := model.GetOpenWebUILinkBySub(claims.Subject); lookupErr == nil {
			_ = subCache().SetWithTTL(claims.Subject, existing.UserId, cacheTTL)
			return requireUserExists(existing.UserId)
		}
		return 0, &LinkError{Reason: ReasonUserAlreadyLinked}
	}
	_ = subCache().SetWithTTL(claims.Subject, user.Id, cacheTTL)
	return user.Id, nil
}

// requireUserExists turns a dangling link (user deleted after linking) into
// a LinkError instead of letting TokenAuth surface a 500 later.
func requireUserExists(userId int) (int, error) {
	if _, err := model.GetUserCache(userId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, &LinkError{Reason: ReasonUserMissing}
		}
		return 0, err
	}
	return userId, nil
}
```

- [ ] **Step 5: Run the link tests**

Run: `go test ./service/openwebui/... -count=1`
Expected: PASS. If `linked user deleted` fails because `model.GetUserCache` wraps the not-found error differently, check `model/user_cache.go:hydrateUserCache` → it returns `GetUserById`'s error unchanged, which is `gorm.ErrRecordNotFound` for a soft-deleted user.

- [ ] **Step 6: Commit**

```bash
git add service/openwebui/cache.go service/openwebui/link.go service/openwebui/link_test.go
git commit -m "feat(openwebui): resolve Open WebUI sub to new-api user with email auto-link

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: Per-user token provisioning

**Files:**
- Create: `service/openwebui/provision.go`
- Create: `service/openwebui/provision_test.go`

**Interfaces:**
- Consumes: `model.FindOldestUserTokenByName`, `model.CreateOpenWebUIToken`, `model.GetTokenByKey` (Task 2), `tokenKeyCache()`, `cacheTTL` (Task 4)
- Produces: `openwebui.TokenNameChat = "openwebui"`, `openwebui.TokenNameTask = "openwebui-task"`, `openwebui.TokenNameForTask(task string) string`, `openwebui.GetOrCreateUserToken(userId int, name string) (*model.Token, error)`

- [ ] **Step 1: Write the failing provisioning tests**

`service/openwebui/provision_test.go`:

```go
package openwebui

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestTokenNameForTask(t *testing.T) {
	require.Equal(t, TokenNameChat, TokenNameForTask(""))
	require.Equal(t, TokenNameChat, TokenNameForTask("   "))
	require.Equal(t, TokenNameTask, TokenNameForTask("title_generation"))
}

func TestGetOrCreateUserToken_CreatesThenReuses(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	first, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.Equal(t, 7, first.UserId)
	require.Equal(t, TokenNameChat, first.Name)
	require.NotEmpty(t, first.Key)

	again, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.Equal(t, first.Id, again.Id)
	require.Equal(t, first.Key, again.Key, "key must be populated on cache hits too")

	task, err := GetOrCreateUserToken(7, TokenNameTask)
	require.NoError(t, err)
	require.NotEqual(t, first.Id, task.Id)
	require.Equal(t, TokenNameTask, task.Name)

	var count int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", 7).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestGetOrCreateUserToken_RecreatesAfterUserDeletesIt(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	first, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.NoError(t, model.DeleteTokenById(first.Id, 7))

	second, err := GetOrCreateUserToken(7, TokenNameChat)
	require.NoError(t, err)
	require.NotEqual(t, first.Id, second.Id)
	require.NotEqual(t, first.Key, second.Key)
}

func TestGetOrCreateUserToken_ConcurrentFirstRequestsCreateOneRow(t *testing.T) {
	setupServiceTestDB(t)
	createTestUser(t, 7, "alice", "alice@example.com")

	const n = 10
	ids := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := GetOrCreateUserToken(7, TokenNameChat)
			if err == nil {
				ids[i] = tok.Id
			}
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ? AND name = ?", 7, TokenNameChat).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/openwebui/... -run 'TokenNameForTask|GetOrCreateUserToken' -count=1`
Expected: FAIL — `undefined: GetOrCreateUserToken`

- [ ] **Step 3: Implement provision.go**

`service/openwebui/provision.go`:

```go
package openwebui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const (
	// TokenNameChat bills ordinary conversations, model listing, embeddings,
	// images and audio.
	TokenNameChat = "openwebui"
	// TokenNameTask bills Open WebUI background tasks (X-Task header set:
	// title_generation, tags_generation, ...), kept separate for auditing.
	TokenNameTask = "openwebui-task"
)

var provisionGroup singleflight.Group

// TokenNameForTask picks the per-user token name from the X-Task header value.
func TokenNameForTask(task string) string {
	if strings.TrimSpace(task) == "" {
		return TokenNameChat
	}
	return TokenNameTask
}

// GetOrCreateUserToken returns the user's token named name, creating it on
// first use. The (user,name)→key cache keeps the hot path off the token
// table; a cached key whose row has since been deleted (the user removed the
// token from their key list) falls through and provisions a fresh one.
// Concurrent first requests are collapsed with singleflight and converge on
// the oldest row.
func GetOrCreateUserToken(userId int, name string) (*model.Token, error) {
	if userId == 0 || name == "" {
		return nil, errors.New("openwebui: user id and token name are required")
	}
	cacheKey := fmt.Sprintf("%d:%s", userId, name)

	if key, found, err := tokenKeyCache().Get(cacheKey); err == nil && found && key != "" {
		token, err := model.GetTokenByKey(key, false)
		if err == nil {
			if token.Key == "" {
				token.Key = key
			}
			return token, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		_, _ = tokenKeyCache().DeleteMany([]string{cacheKey})
	}

	v, err, _ := provisionGroup.Do(cacheKey, func() (any, error) {
		token, err := model.FindOldestUserTokenByName(userId, name)
		if err == nil {
			return token, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if _, err := model.CreateOpenWebUIToken(userId, name); err != nil {
			return nil, err
		}
		// Re-read so a racing insert on another node resolves to one row.
		return model.FindOldestUserTokenByName(userId, name)
	})
	if err != nil {
		return nil, err
	}
	token := v.(*model.Token)
	_ = tokenKeyCache().SetWithTTL(cacheKey, token.Key, cacheTTL)
	return token, nil
}
```

- [ ] **Step 4: Run the provisioning tests**

Run: `go test ./service/openwebui/... -count=1 -race`
Expected: PASS (the `-race` flag also checks the singleflight path).

- [ ] **Step 5: Commit**

```bash
git add service/openwebui/provision.go service/openwebui/provision_test.go
git commit -m "feat(openwebui): provision per-user openwebui tokens on demand

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Request context and the `Resolve` entry point

**Files:**
- Create: `service/openwebui/context.go`
- Create: `service/openwebui/resolve.go`
- Create: `service/openwebui/resolve_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1, 3, 4, 5; `model.ValidateUserToken`, `model.ErrDatabase`; `i18n` keys; `logger.LogWarn`, `logger.LogInfo`
- Produces: `openwebui.HeaderUserJWT = "X-OpenWebUI-User-Jwt"`, `openwebui.HeaderTask = "X-Task"`
- Produces: `openwebui.IsServiceKey(key string) bool`
- Produces: `openwebui.RejectError{Status int; MessageKey string; LogReason string}` (implements `error`)
- Produces: `openwebui.Resolve(c *gin.Context, serviceToken *model.Token) (*model.Token, *RejectError)`
- Produces: `openwebui.SetIdentity(c, sub, task string)`, `openwebui.SetUnsigned(c)`, `openwebui.GetSub(c) string`, `openwebui.GetTask(c) string`, `openwebui.IsAttributed(c) bool`, `openwebui.IsUnsigned(c) bool`

- [ ] **Step 1: Write the failing resolve tests**

`service/openwebui/resolve_test.go`:

```go
package openwebui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func setupResolveTest(t *testing.T) *model.Token {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setupServiceTestDB(t)
	require.NoError(t, i18n.Init())

	createTestUser(t, 1, "owui-svc", "svc@example.com")
	createTestUser(t, 7, "alice", "alice@example.com")
	createTestUser(t, 8, "bob", "bob@example.com")
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 8).Update("status", common.UserStatusDisabled).Error)

	serviceToken, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled:          true,
		ServiceApiKey:         serviceToken.Key,
		JwtSecret:             testSecret,
		JwtPreviousSecret:     testOldSecret,
		JwtIssuer:             testIssuer,
		JwtLeewaySeconds:      60,
		AllowUnsignedRequests: true,
	}
	t.Cleanup(func() { *system_setting.GetOpenWebUISettings() = original })
	return serviceToken
}

func newRelayContext(t *testing.T, headers map[string]string) *gin.Context {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

func userJWT(t *testing.T, secret, sub, email string) string {
	t.Helper()
	now := time.Now()
	return signHS256(t, secret, UserClaims{
		Email: email, Name: "Alice", Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: sub, Issuer: testIssuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(300 * time.Second)),
		},
	})
}

func TestIsServiceKey(t *testing.T) {
	svc := setupResolveTest(t)
	require.True(t, IsServiceKey(svc.Key))
	require.False(t, IsServiceKey("other"))
	require.False(t, IsServiceKey(""))

	system_setting.GetOpenWebUISettings().RelayEnabled = false
	require.False(t, IsServiceKey(svc.Key))
}

func TestResolve_SubstitutesUserChatToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})

	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, 7, token.UserId)
	require.Equal(t, TokenNameChat, token.Name)
	require.NotEmpty(t, token.Key)
	require.Equal(t, "owui-7", GetSub(c))
	require.Equal(t, "", GetTask(c))
	require.True(t, IsAttributed(c))
	require.False(t, IsUnsigned(c))
}

func TestResolve_TaskHeaderSelectsTaskToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{
		HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com"),
		HeaderTask:    "title_generation",
	})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, TokenNameTask, token.Name)
	require.Equal(t, "title_generation", GetTask(c))
}

func TestResolve_PreviousSecretAccepted(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testOldSecret, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.Nil(t, rej)
}

func TestResolve_UnsignedAllowedBillsServiceToken(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{
		"X-OpenWebUI-User-Email": "alice@example.com", // plaintext fallback must be ignored
	})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)
	require.Equal(t, svc.Id, token.Id)
	require.True(t, IsUnsigned(c))
	require.False(t, IsAttributed(c))
	require.Equal(t, "", GetSub(c))
}

func TestResolve_UnsignedRejectedWhenDisallowed(t *testing.T) {
	svc := setupResolveTest(t)
	system_setting.GetOpenWebUISettings().AllowUnsignedRequests = false
	c := newRelayContext(t, nil)
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUIAuthFailed, rej.MessageKey)
}

func TestResolve_InvalidJWTAlwaysRejected(t *testing.T) {
	svc := setupResolveTest(t)
	// allow_unsigned_requests is true here; a *present but bad* JWT must still fail.
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testWrongKey, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUIAuthFailed, rej.MessageKey)
	require.Contains(t, rej.LogReason, "jwt_invalid")
}

func TestResolve_EmptySecretRejects(t *testing.T) {
	svc := setupResolveTest(t)
	system_setting.GetOpenWebUISettings().JwtSecret = ""
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Contains(t, rej.LogReason, "misconfigured")
}

func TestResolve_UnlinkedUserGets403WithoutLeakingIdentity(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-x", "nobody@example.com")})
	_, rej := Resolve(c, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusForbidden, rej.Status)
	require.Equal(t, i18n.MsgOpenWebUINotLinked, rej.MessageKey)
	require.Contains(t, rej.LogReason, "link_failed: no_user")
	// The user-facing text must not contain the sub or the email.
	msg := i18n.Translate(i18n.LangEn, rej.MessageKey)
	require.NotContains(t, msg, "owui-x")
	require.NotContains(t, msg, "nobody@example.com")
}

func TestResolve_DisabledUserTokenRejected(t *testing.T) {
	svc := setupResolveTest(t)
	c := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	token, rej := Resolve(c, svc)
	require.Nil(t, rej)

	// Alice disables her openwebui token from the key list.
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("status", common.TokenStatusDisabled).Error)
	c2 := newRelayContext(t, map[string]string{HeaderUserJWT: userJWT(t, testSecret, "owui-7", "alice@example.com")})
	_, rej = Resolve(c2, svc)
	require.NotNil(t, rej)
	require.Equal(t, http.StatusUnauthorized, rej.Status)
	require.Equal(t, i18n.MsgTokenInvalid, rej.MessageKey)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/openwebui/... -run 'IsServiceKey|Resolve_' -count=1`
Expected: FAIL — `undefined: Resolve`, `undefined: HeaderUserJWT`

- [ ] **Step 3: Implement context.go**

`service/openwebui/context.go`:

```go
package openwebui

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

// SetIdentity records that this request was attributed to the Open WebUI
// user sub (and, when non-empty, the background task that triggered it).
func SetIdentity(c *gin.Context, sub, task string) {
	common.SetContextKey(c, constant.ContextKeyOpenWebUISub, sub)
	common.SetContextKey(c, constant.ContextKeyOpenWebUITask, task)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIAttributed, true)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, false)
}

// SetUnsigned records that the request carried the service key but no JWT
// and is being billed to the service token itself.
func SetUnsigned(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyOpenWebUIAttributed, false)
	common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, true)
}

func GetSub(c *gin.Context) string {
	return common.GetContextKeyString(c, constant.ContextKeyOpenWebUISub)
}

func GetTask(c *gin.Context) string {
	return common.GetContextKeyString(c, constant.ContextKeyOpenWebUITask)
}

func IsAttributed(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyOpenWebUIAttributed)
}

func IsUnsigned(c *gin.Context) bool {
	return common.GetContextKeyBool(c, constant.ContextKeyOpenWebUIUnsigned)
}
```

- [ ] **Step 4: Implement resolve.go**

`service/openwebui/resolve.go`:

```go
package openwebui

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const (
	// HeaderUserJWT carries the per-request HS256 identity minted by Open WebUI.
	HeaderUserJWT = "X-OpenWebUI-User-Jwt"
	// HeaderTask is Open WebUI's connection-header template "{{TASK}}": empty
	// for ordinary chat, a task name (title_generation, ...) for background work.
	HeaderTask = "X-Task"
)

// RejectError is what Resolve returns instead of a token. MessageKey is the
// i18n key for the (deliberately vague) user-facing text; LogReason is the
// precise cause and must never be sent to the client.
type RejectError struct {
	Status     int
	MessageKey string
	LogReason  string
}

func (e *RejectError) Error() string {
	return e.LogReason
}

// IsServiceKey reports whether the parsed bearer key is the configured Open
// WebUI service key and the relay is enabled. Constant-time comparison.
func IsServiceKey(key string) bool {
	s := system_setting.GetOpenWebUISettings()
	if !s.RelayEnabled || s.ServiceApiKey == "" || key == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(key), []byte(s.ServiceApiKey)) == 1
}

// Resolve is the single entry point middleware.TokenAuth uses once it has
// established that the arriving key is the service key and that the service
// token's IP limits pass. It returns the token the rest of the request
// should run as:
//
//   - a valid JWT → the mapped user's "openwebui" or "openwebui-task" token
//   - no JWT and AllowUnsignedRequests → the service token itself
//   - anything else → a RejectError
func Resolve(c *gin.Context, serviceToken *model.Token) (*model.Token, *RejectError) {
	s := system_setting.GetOpenWebUISettings()
	raw := strings.TrimSpace(c.GetHeader(HeaderUserJWT))

	if raw == "" {
		if !s.AllowUnsignedRequests {
			return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "unsigned_request_rejected")
		}
		SetUnsigned(c)
		logger.LogInfo(c, fmt.Sprintf("openwebui: unsigned request billed to service token | path=%s", requestPath(c)))
		return serviceToken, nil
	}

	if s.JwtSecret == "" {
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "misconfigured: empty_secret")
	}

	claims, err := VerifyUserJWT(raw, JWTConfig{
		Secret:         s.JwtSecret,
		PreviousSecret: s.JwtPreviousSecret,
		Issuer:         s.JwtIssuer,
		Leeway:         time.Duration(s.JwtLeewaySeconds) * time.Second,
	})
	if err != nil {
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgOpenWebUIAuthFailed, "jwt_invalid: "+err.Error())
	}

	userId, err := ResolveUserId(claims)
	if err != nil {
		var linkErr *LinkError
		if errors.As(err, &linkErr) {
			return nil, reject(c, http.StatusForbidden, i18n.MsgOpenWebUINotLinked,
				fmt.Sprintf("link_failed: %s | sub=%s email=%s", linkErr.Reason, claims.Subject, claims.Email))
		}
		return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError, "link_lookup_error: "+err.Error())
	}

	task := strings.TrimSpace(c.GetHeader(HeaderTask))
	token, err := GetOrCreateUserToken(userId, TokenNameForTask(task))
	if err != nil {
		return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError,
			fmt.Sprintf("provision_error: user=%d: %v", userId, err))
	}

	// Run the substituted token through the same validity checks a directly
	// presented key gets, so a user who disabled their openwebui token is
	// refused with the ordinary token error.
	validated, err := model.ValidateUserToken(token.Key)
	if err != nil {
		if errors.Is(err, model.ErrDatabase) {
			return nil, reject(c, http.StatusInternalServerError, i18n.MsgDatabaseError, "user_token_db_error: "+err.Error())
		}
		return nil, reject(c, http.StatusUnauthorized, i18n.MsgTokenInvalid,
			fmt.Sprintf("user_token_invalid: user=%d token=%d: %v", userId, token.Id, err))
	}
	if validated.Key == "" {
		validated.Key = token.Key
	}

	SetIdentity(c, claims.Subject, task)
	return validated, nil
}

func reject(c *gin.Context, status int, messageKey, logReason string) *RejectError {
	logger.LogWarn(c, fmt.Sprintf("openwebui: rejected (%d) %s | path=%s ip=%s", status, logReason, requestPath(c), c.ClientIP()))
	return &RejectError{Status: status, MessageKey: messageKey, LogReason: logReason}
}

func requestPath(c *gin.Context) string {
	if c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.Path
	}
	return ""
}
```

- [ ] **Step 5: Run all package tests**

Run: `go test ./service/openwebui/... -count=1 -race`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add service/openwebui/context.go service/openwebui/resolve.go service/openwebui/resolve_test.go
git commit -m "feat(openwebui): add Resolve entry point that swaps the service token for the user's

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: Hook `Resolve` into `TokenAuth()` and test the middleware end to end

**Files:**
- Modify: `middleware/auth.go` (`TokenAuth()`, between the IP-limit block ending at the `logger.LogDebug(c, "Client IP %s passed ...")` line and `userCache, err := model.GetUserCache(token.UserId)`)
- Create: `middleware/auth_openwebui_test.go`

**Interfaces:**
- Consumes: `openwebui.IsServiceKey`, `openwebui.Resolve`, `openwebui.RejectError` (Task 6)
- Produces: no new exported API. Behaviour: when the parsed key is the service key, the request continues as the substituted token.

- [ ] **Step 1: Write the failing integration tests**

`middleware/auth_openwebui_test.go`:

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	owuiTestSecret = "0123456789abcdef0123456789abcdef"
	owuiTestIssuer = "open-webui"
)

type owuiFixture struct {
	serviceKey string
}

type owuiEcho struct {
	Id         int    `json:"id"`
	TokenId    int    `json:"token_id"`
	TokenName  string `json:"token_name"`
	Sub        string `json:"sub"`
	Task       string `json:"task"`
	Attributed bool   `json:"attributed"`
	Unsigned   bool   `json:"unsigned"`
}

func setupOpenWebUIMiddlewareTest(t *testing.T) (*gin.Engine, owuiFixture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	openwebui.ResetCachesForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.OpenWebUILink{}))
	model.DB = db

	mkUser := func(id int, name, email string, status int) {
		// users.aff_code has a unique index, so every fixture user needs its own.
		require.NoError(t, db.Create(&model.User{
			Id: id, Username: name, Password: "secret-pass", Email: email, AffCode: "aff-" + name,
			Status: status, Role: common.RoleCommonUser, Group: "default",
		}).Error)
	}
	mkUser(1, "owui-svc", "svc@example.com", common.UserStatusEnabled)
	mkUser(7, "alice", "alice@example.com", common.UserStatusEnabled)
	mkUser(8, "bob", "bob@example.com", common.UserStatusDisabled)

	serviceToken, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	originalSettings := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled:          true,
		ServiceApiKey:         serviceToken.Key,
		JwtSecret:             owuiTestSecret,
		JwtIssuer:             owuiTestIssuer,
		JwtLeewaySeconds:      60,
		AllowUnsignedRequests: true,
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		*system_setting.GetOpenWebUISettings() = originalSettings
		openwebui.ResetCachesForTest()
	})

	engine := gin.New()
	engine.Use(TokenAuth())
	echo := func(c *gin.Context) {
		c.JSON(http.StatusOK, owuiEcho{
			Id:         c.GetInt("id"),
			TokenId:    c.GetInt("token_id"),
			TokenName:  c.GetString("token_name"),
			Sub:        openwebui.GetSub(c),
			Task:       openwebui.GetTask(c),
			Attributed: openwebui.IsAttributed(c),
			Unsigned:   openwebui.IsUnsigned(c),
		})
	}
	engine.POST("/v1/chat/completions", echo)
	engine.GET("/v1/models", echo)
	return engine, owuiFixture{serviceKey: serviceToken.Key}
}

func owuiJWT(t *testing.T, sub, email string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"sub": sub, "email": email, "name": "Test", "role": "user",
		"iss": owuiTestIssuer, "iat": now.Unix(), "exp": now.Add(300 * time.Second).Unix(),
	}
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(owuiTestSecret))
	require.NoError(t, err)
	return raw
}

func owuiDo(t *testing.T, engine *gin.Engine, method, path, bearer string, headers map[string]string) (*httptest.ResponseRecorder, owuiEcho) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.RemoteAddr = "10.0.0.5:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var echo owuiEcho
	if rec.Code == http.StatusOK {
		require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &echo))
	}
	return rec, echo
}

func TestTokenAuth_OpenWebUI_SubstitutesUserToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", "sk-"+fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, openwebui.TokenNameChat, echo.TokenName)
	require.Equal(t, "owui-7", echo.Sub)
	require.True(t, echo.Attributed)

	tok, err := model.GetTokenById(echo.TokenId)
	require.NoError(t, err)
	require.Equal(t, 7, tok.UserId)
}

func TestTokenAuth_OpenWebUI_TaskHeaderUsesTaskToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey, map[string]string{
		openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com"),
		openwebui.HeaderTask:    "title_generation",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, openwebui.TokenNameTask, echo.TokenName)
	require.Equal(t, "title_generation", echo.Task)
}

func TestTokenAuth_OpenWebUI_UnsignedBillsServiceToken(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{"X-OpenWebUI-User-Email": "alice@example.com"})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, echo.Id, "billed to the service account, plaintext header ignored")
	require.Equal(t, "openwebui-service", echo.TokenName)
	require.True(t, echo.Unsigned)
	require.False(t, echo.Attributed)
}

func TestTokenAuth_OpenWebUI_UnsignedRejectedWhenDisallowed(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	system_setting.GetOpenWebUISettings().AllowUnsignedRequests = false
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"new_api_error"`)
}

func TestTokenAuth_OpenWebUI_BadJWTIs401EvenWhenUnsignedAllowed(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: "garbage.jwt.value"})
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.NotContains(t, rec.Body.String(), "garbage")
}

func TestTokenAuth_OpenWebUI_UnlinkedIs403WithoutIdentity(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-zz", "nobody@example.com")})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "owui-zz")
	require.NotContains(t, rec.Body.String(), "nobody@example.com")
}

func TestTokenAuth_OpenWebUI_DisabledUserIs403(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-8", "bob@example.com")})
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestTokenAuth_OpenWebUI_ServiceTokenIPLimitAppliesBeforeJWT(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	svc, err := model.GetTokenByKey(fx.serviceKey, true)
	require.NoError(t, err)
	allow := "192.168.1.0/24"
	svc.AllowIps = &allow
	require.NoError(t, model.DB.Save(svc).Error)

	// Even an unparseable JWT must not be the reason: IP rejection comes first with 403.
	rec, _ := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: "garbage"})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "allowed access list")
}

func TestTokenAuth_OpenWebUI_RelayDisabledFallsBackToNormalAuth(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	system_setting.GetOpenWebUISettings().RelayEnabled = false
	rec, echo := owuiDo(t, engine, http.MethodPost, "/v1/chat/completions", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, echo.Id, "JWT ignored: plain token auth")
	require.Equal(t, "", echo.Sub)
}

func TestTokenAuth_OpenWebUI_OrdinaryKeysUnaffected(t *testing.T) {
	engine, _ := setupOpenWebUIMiddlewareTest(t)
	own, err := model.CreateOpenWebUIToken(7, "my-key")
	require.NoError(t, err)
	rec, echo := owuiDo(t, engine, http.MethodGet, "/v1/models", "sk-"+own.Key,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-8", "bob@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, "my-key", echo.TokenName)
	require.False(t, echo.Attributed)
}

func TestTokenAuth_OpenWebUI_ModelsEndpointRunsAsUser(t *testing.T) {
	engine, fx := setupOpenWebUIMiddlewareTest(t)
	rec, echo := owuiDo(t, engine, http.MethodGet, "/v1/models", fx.serviceKey,
		map[string]string{openwebui.HeaderUserJWT: owuiJWT(t, "owui-7", "alice@example.com")})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 7, echo.Id)
	require.Equal(t, openwebui.TokenNameChat, echo.TokenName)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./middleware/... -run 'TokenAuth_OpenWebUI' -count=1`
Expected: FAIL — `SubstitutesUserToken` reports `Id` = 1 (no substitution yet), unsigned/relay-disabled tests may pass by accident; that is fine.

- [ ] **Step 3: Add the hook to `TokenAuth()`**

In `middleware/auth.go`, add the import `"github.com/QuantumNous/new-api/service/openwebui"` and insert the following block **after** the IP-limit `if len(allowIps) > 0 { ... }` block and **before** `userCache, err := model.GetUserCache(token.UserId)`:

```go
		// Open WebUI relay: the shared service key arrived. Swap in the token of
		// the person identified by X-OpenWebUI-User-Jwt (or keep the service
		// token for an allowed unsigned request). IP limits above were checked
		// against the service token on purpose; the substituted token's own IP
		// list is not consulted.
		if openwebui.IsServiceKey(key) {
			substituted, rejectErr := openwebui.Resolve(c, token)
			if rejectErr != nil {
				abortWithOpenAiMessage(c, rejectErr.Status, common.TranslateMessage(c, rejectErr.MessageKey))
				return
			}
			token = substituted
			c.Set("id", token.UserId)
		}

```

`c.Set("id", token.UserId)` is required because the earlier `if token != nil { if c.GetInt("id") == 0 { c.Set("id", token.UserId) } }` already stored the **service** user's id; `SetupContextForToken` sets it again later, but `GetUserCache`/status checks between here and there read `token.UserId` directly, so both are consistent.

- [ ] **Step 4: Run the middleware tests**

Run: `go test ./middleware/... -count=1`
Expected: all PASS, including pre-existing middleware tests.

- [ ] **Step 5: Commit**

```bash
git add middleware/auth.go middleware/auth_openwebui_test.go
git commit -m "feat(openwebui): substitute the user's token in TokenAuth for service-key requests

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: Record Open WebUI identity in usage-log `other`

**Files:**
- Modify: `service/log_info_generate.go`
- Create: `service/log_info_openwebui_test.go`

**Interfaces:**
- Consumes: `constant.ContextKeyOpenWebUI*` (Task 1)
- Produces: `other["owui_sub"]`, `other["owui_task"]`, `other["owui_unsigned"]` on every consume log produced via `GenerateTextOtherInfo` (which `GenerateWssOtherInfo`, `GenerateAudioOtherInfo`, `GenerateClaudeOtherInfo` all call)

- [ ] **Step 1: Write the failing test**

`service/log_info_openwebui_test.go`:

```go
package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAppendOpenWebUIInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("attributed request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "title_generation")
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, "owui-7", other["owui_sub"])
		require.Equal(t, "title_generation", other["owui_task"])
		_, hasUnsigned := other["owui_unsigned"]
		require.False(t, hasUnsigned)
	})

	t.Run("chat request has no task key", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUISub, "owui-7")
		common.SetContextKey(c, constant.ContextKeyOpenWebUITask, "")
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, "owui-7", other["owui_sub"])
		_, hasTask := other["owui_task"]
		require.False(t, hasTask)
	})

	t.Run("unsigned request", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		common.SetContextKey(c, constant.ContextKeyOpenWebUIUnsigned, true)
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Equal(t, true, other["owui_unsigned"])
		_, hasSub := other["owui_sub"]
		require.False(t, hasSub)
	})

	t.Run("ordinary request adds nothing", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		other := map[string]interface{}{}
		appendOpenWebUIInfo(c, other)
		require.Empty(t, other)
		appendOpenWebUIInfo(nil, other)
		appendOpenWebUIInfo(c, nil)
	})
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./service/ -run TestAppendOpenWebUIInfo -count=1`
Expected: FAIL — `undefined: appendOpenWebUIInfo`

- [ ] **Step 3: Implement**

In `service/log_info_generate.go`, add after `appendRequestPath`:

```go
// appendOpenWebUIInfo records which Open WebUI user (JWT sub) and, for
// background work, which task a request was attributed to, or that it was
// an unsigned service-key request. Keys are prefixed owui_ so they are easy
// to find in the log detail view and to count for the "unsigned requests"
// operational metric.
func appendOpenWebUIInfo(ctx *gin.Context, other map[string]interface{}) {
	if ctx == nil || other == nil {
		return
	}
	if sub := common.GetContextKeyString(ctx, constant.ContextKeyOpenWebUISub); sub != "" {
		other["owui_sub"] = sub
		if task := common.GetContextKeyString(ctx, constant.ContextKeyOpenWebUITask); task != "" {
			other["owui_task"] = task
		}
	}
	if common.GetContextKeyBool(ctx, constant.ContextKeyOpenWebUIUnsigned) {
		other["owui_unsigned"] = true
	}
}
```

In `GenerateTextOtherInfo`, directly after the line `appendRequestPath(ctx, relayInfo, other)` add:

```go
	appendOpenWebUIInfo(ctx, other)
```

- [ ] **Step 4: Run the tests**

Run: `go test ./service/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add service/log_info_generate.go service/log_info_openwebui_test.go
git commit -m "feat(openwebui): record sub, task and unsigned flag in consume log other

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Validate `openwebui.*` options on save

**Files:**
- Modify: `controller/option.go` (`UpdateOption`, the second `switch option.Key {` block — add cases next to `case "oidc.enabled":`)
- Create: `controller/option_openwebui_test.go`

**Interfaces:**
- Consumes: `system_setting.GetOpenWebUISettings`, `system_setting.NormalizeOpenWebUIServiceKey` (Task 1), `model.ValidateUserToken`, i18n option keys (Task 1)
- Behaviour: `openwebui.relay_enabled=true` requires a configured service key and a ≥32-char `jwt_secret`; `openwebui.service_api_key` is normalised and must match an enabled token (empty clears it); `openwebui.jwt_secret` / `openwebui.jwt_previous_secret` must be empty or ≥32 chars.

- [ ] **Step 1: Write the failing tests**

`controller/option_openwebui_test.go`:

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

func setupOpenWebUIOptionTest(t *testing.T) *model.Token {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	// model.UpdateOption writes into common.OptionMap, which is nil until
	// InitOptionMap runs at server start.
	originalOptionMap := common.OptionMap
	common.OptionMap = make(map[string]string)
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Option{}))
	model.DB = db
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "svc", Password: "secret-pass", AffCode: "aff-svc", Status: common.UserStatusEnabled}).Error)
	token, err := model.CreateOpenWebUIToken(1, "openwebui-service")
	require.NoError(t, err)

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		common.OptionMap = originalOptionMap
		*system_setting.GetOpenWebUISettings() = original
	})
	return token
}

func putOption(t *testing.T, key string, value any) (int, string) {
	t.Helper()
	body, err := common.Marshal(map[string]any{"key": key, "value": value})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	UpdateOption(c)
	return rec.Code, rec.Body.String()
}

func TestUpdateOption_OpenWebUI_SecretTooShort(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	for _, key := range []string{"openwebui.jwt_secret", "openwebui.jwt_previous_secret"} {
		_, body := putOption(t, key, "short")
		require.Contains(t, body, `"success":false`, key)
		require.Equal(t, "", system_setting.GetOpenWebUISettings().JwtSecret)
	}
}

func TestUpdateOption_OpenWebUI_SecretAccepted(t *testing.T) {
	setupOpenWebUIOptionTest(t)
	secret := strings.Repeat("s", 32)
	_, body := putOption(t, "openwebui.jwt_secret", secret)
	require.Contains(t, body, `"success":true`)
	require.Equal(t, secret, system_setting.GetOpenWebUISettings().JwtSecret)

	// Clearing is allowed.
	_, body = putOption(t, "openwebui.jwt_previous_secret", "")
	require.Contains(t, body, `"success":true`)
}

func TestUpdateOption_OpenWebUI_ServiceKeyMustBeEnabledToken(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)

	_, body := putOption(t, "openwebui.service_api_key", "sk-does-not-exist")
	require.Contains(t, body, `"success":false`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().ServiceApiKey)

	_, body = putOption(t, "openwebui.service_api_key", "  sk-"+token.Key+" ")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, token.Key, system_setting.GetOpenWebUISettings().ServiceApiKey, "normalised without sk- prefix")

	_, body = putOption(t, "openwebui.service_api_key", "")
	require.Contains(t, body, `"success":true`)
	require.Equal(t, "", system_setting.GetOpenWebUISettings().ServiceApiKey)
}

func TestUpdateOption_OpenWebUI_RelayEnabledRequiresConfig(t *testing.T) {
	token := setupOpenWebUIOptionTest(t)

	_, body := putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":false`)
	require.False(t, system_setting.GetOpenWebUISettings().RelayEnabled)

	_, _ = putOption(t, "openwebui.service_api_key", token.Key)
	_, body = putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":false`, "secret still missing")

	_, _ = putOption(t, "openwebui.jwt_secret", strings.Repeat("s", 32))
	_, body = putOption(t, "openwebui.relay_enabled", true)
	require.Contains(t, body, `"success":true`)
	require.True(t, system_setting.GetOpenWebUISettings().RelayEnabled)

	// Turning it off never needs validation.
	_, body = putOption(t, "openwebui.relay_enabled", false)
	require.Contains(t, body, `"success":true`)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./controller/ -run 'UpdateOption_OpenWebUI' -count=1`
Expected: FAIL — short secrets and bogus keys are currently accepted (`"success":true` where `false` expected).

- [ ] **Step 3: Add the validation cases**

In `controller/option.go`, inside `UpdateOption`'s second `switch option.Key {` (the one containing `case "oidc.enabled":`), add these cases right after the `case "oidc.enabled": {...}` block:

```go
	case "openwebui.relay_enabled":
		if option.Value == "true" {
			s := system_setting.GetOpenWebUISettings()
			if s.ServiceApiKey == "" || len(s.JwtSecret) < 32 {
				common.ApiErrorI18n(c, i18n.MsgOptionOpenWebUIRelayConfigMissing)
				return
			}
		}
	case "openwebui.service_api_key":
		normalized := system_setting.NormalizeOpenWebUIServiceKey(option.Value.(string))
		if normalized != "" {
			if _, err := model.ValidateUserToken(normalized); err != nil {
				common.ApiErrorI18n(c, i18n.MsgOptionOpenWebUIServiceKeyInvalid)
				return
			}
		}
		option.Value = normalized
	case "openwebui.jwt_secret", "openwebui.jwt_previous_secret":
		if v := option.Value.(string); v != "" && len(v) < 32 {
			common.ApiErrorI18n(c, i18n.MsgOptionOpenWebUISecretTooShort)
			return
		}
```

`option.Value` is already a string at this point (`optionValueToString` ran above the switch), so the `.(string)` assertions are safe, matching the existing `case "GroupRatio":` usage.

- [ ] **Step 4: Run the tests**

Run: `go test ./controller/ -run 'UpdateOption' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add controller/option.go controller/option_openwebui_test.go
git commit -m "feat(openwebui): validate relay options before saving

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: Admin API for relay status and manual links

**Files:**
- Create: `controller/openwebui_link.go`
- Create: `controller/openwebui_link_test.go`
- Modify: `router/api-router.go` — add a route group after the `adminRoute := apiRouter.Group("/admin")` block

**Interfaces:**
- Consumes: model link functions (Task 2), `openwebui.InvalidateSub`, `openwebui.TokenNameChat/Task` (Tasks 4–5), `canManageTargetRole` (`controller/user.go`), settings (Task 1)
- Produces HTTP (all `AdminAuth`):
  - `GET /api/openwebui/relay-status` → `{relay_enabled, service_key_configured, jwt_secret_configured, jwt_previous_secret_configured, jwt_issuer, jwt_leeway_seconds, allow_unsigned_requests}`
  - `GET /api/openwebui/links?user_id=N` or `?sub=S` → `data: {id, sub, user_id, email, linked_by, created_at, tokens:[{name, exists, status}]}` or `data: null`
  - `POST /api/openwebui/links` `{user_id, sub}` → created link (same shape); `success:false` with `openwebui.link_conflict` message on conflict
  - `DELETE /api/openwebui/links/:id` → `success:true`

- [ ] **Step 1: Write the failing controller tests**

`controller/openwebui_link_test.go`:

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
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOpenWebUILinkControllerTest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	originalDB := model.DB
	originalSQLite := common.UsingSQLite
	originalRedis := common.RedisEnabled
	common.UsingSQLite = true
	common.RedisEnabled = false
	openwebui.ResetCachesForTest()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.OpenWebUILink{}))
	model.DB = db
	for _, u := range []model.User{
		// users.aff_code has a unique index, so every fixture user needs its own.
		{Id: 7, Username: "alice", Password: "secret-pass", Email: "alice@example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{Id: 8, Username: "bob", Password: "secret-pass", Email: "bob@example.com", AffCode: "aff-bob", Status: common.UserStatusEnabled, Role: common.RoleCommonUser},
		{Id: 100, Username: "root", Password: "secret-pass", AffCode: "aff-root", Status: common.UserStatusEnabled, Role: common.RoleRootUser},
	} {
		require.NoError(t, db.Create(&u).Error)
	}

	original := *system_setting.GetOpenWebUISettings()
	*system_setting.GetOpenWebUISettings() = system_setting.OpenWebUISettings{
		RelayEnabled: true, ServiceApiKey: "svc", JwtSecret: strings.Repeat("s", 32),
		JwtIssuer: "open-webui", JwtLeewaySeconds: 60, AllowUnsignedRequests: true,
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalSQLite
		common.RedisEnabled = originalRedis
		*system_setting.GetOpenWebUISettings() = original
		openwebui.ResetCachesForTest()
	})

	engine := gin.New()
	// Simulate AdminAuth having run as an admin (role 10).
	engine.Use(func(c *gin.Context) {
		c.Set("id", 50)
		c.Set("role", common.RoleAdminUser)
		c.Next()
	})
	engine.GET("/api/openwebui/relay-status", GetOpenWebUIRelayStatus)
	engine.GET("/api/openwebui/links", GetOpenWebUILink)
	engine.POST("/api/openwebui/links", CreateOpenWebUILink)
	engine.DELETE("/api/openwebui/links/:id", DeleteOpenWebUILink)
	return engine
}

func linkCall(t *testing.T, engine *gin.Engine, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var parsed map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &parsed), rec.Body.String())
	return rec.Code, parsed
}

func TestOpenWebUIRelayStatus(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	code, res := linkCall(t, engine, http.MethodGet, "/api/openwebui/relay-status", nil)
	require.Equal(t, http.StatusOK, code)
	data := res["data"].(map[string]any)
	require.Equal(t, true, data["relay_enabled"])
	require.Equal(t, true, data["service_key_configured"])
	require.Equal(t, true, data["jwt_secret_configured"])
	require.Equal(t, false, data["jwt_previous_secret_configured"])
	_, leaks := data["service_api_key"]
	require.False(t, leaks)
}

func TestOpenWebUILink_CreateGetDelete(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)

	code, res := linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, true, res["success"])
	require.Nil(t, res["data"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"], res["message"])
	data := res["data"].(map[string]any)
	require.Equal(t, "owui-7", data["sub"])
	require.Equal(t, "admin", data["linked_by"])
	tokens := data["tokens"].([]any)
	require.Len(t, tokens, 2)
	require.Equal(t, false, tokens[0].(map[string]any)["exists"])

	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?sub=owui-7", nil)
	require.Equal(t, float64(7), res["data"].(map[string]any)["user_id"])

	// Provisioned token shows up in the status list.
	_, err := model.CreateOpenWebUIToken(7, openwebui.TokenNameChat)
	require.NoError(t, err)
	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	tokens = res["data"].(map[string]any)["tokens"].([]any)
	require.Equal(t, true, tokens[0].(map[string]any)["exists"])
	require.Equal(t, float64(common.TokenStatusEnabled), tokens[0].(map[string]any)["status"])

	id := int(data["id"].(float64))
	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, true, res["success"])
	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links?user_id=7", nil)
	require.Nil(t, res["data"])

	// Tokens survive an unlink.
	_, err = model.FindOldestUserTokenByName(7, openwebui.TokenNameChat)
	require.NoError(t, err)

	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_Conflicts(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 8, "sub": "owui-7"})
	require.Equal(t, false, res["success"])
	require.NotEmpty(t, res["message"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-other"})
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_Validation(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "   "})
	require.Equal(t, false, res["success"])

	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 999, "sub": "owui-x"})
	require.Equal(t, false, res["success"])

	_, res = linkCall(t, engine, http.MethodGet, "/api/openwebui/links", nil)
	require.Equal(t, false, res["success"])

	// An admin may not touch a root user's link.
	_, res = linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 100, "sub": "owui-root"})
	require.Equal(t, false, res["success"])
}

func TestOpenWebUILink_InvalidatesResolveCache(t *testing.T) {
	engine := setupOpenWebUILinkControllerTest(t)
	_, res := linkCall(t, engine, http.MethodPost, "/api/openwebui/links", map[string]any{"user_id": 7, "sub": "owui-7"})
	require.Equal(t, true, res["success"])

	// Warm the resolver cache, then unlink via the API; the next resolve must miss.
	userId, err := openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.NoError(t, err)
	require.Equal(t, 7, userId)

	id := int(res["data"].(map[string]any)["id"].(float64))
	_, res = linkCall(t, engine, http.MethodDelete, "/api/openwebui/links/"+itoa(id), nil)
	require.Equal(t, true, res["success"])

	_, err = openwebui.ResolveUserId(&openwebui.UserClaims{RegisteredClaims: jwtRegistered("owui-7")})
	require.Error(t, err, "cache must have been invalidated")
}
```

Add these two helpers at the bottom of the same test file (they keep the test bodies short):

```go
func itoa(i int) string { return strconv.Itoa(i) }

func jwtRegistered(sub string) jwt.RegisteredClaims { return jwt.RegisteredClaims{Subject: sub} }
```

and add `"strconv"` and `"github.com/golang-jwt/jwt/v5"` to the test file's imports.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./controller/ -run 'OpenWebUILink|OpenWebUIRelayStatus' -count=1`
Expected: FAIL — `undefined: GetOpenWebUIRelayStatus` etc.

- [ ] **Step 3: Implement the controller**

`controller/openwebui_link.go`:

```go
package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/openwebui"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type openWebUITokenStatus struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	Status int    `json:"status"`
}

type openWebUILinkResponse struct {
	Id        int                    `json:"id"`
	Sub       string                 `json:"sub"`
	UserId    int                    `json:"user_id"`
	Email     string                 `json:"email"`
	LinkedBy  string                 `json:"linked_by"`
	CreatedAt int64                  `json:"created_at"`
	Tokens    []openWebUITokenStatus `json:"tokens"`
}

// GetOpenWebUIRelayStatus tells the admin UI which write-only secrets are
// set. The secrets themselves are hidden from GET /api/option/ by name.
func GetOpenWebUIRelayStatus(c *gin.Context) {
	s := system_setting.GetOpenWebUISettings()
	common.ApiSuccess(c, gin.H{
		"relay_enabled":                  s.RelayEnabled,
		"service_key_configured":         s.ServiceApiKey != "",
		"jwt_secret_configured":          s.JwtSecret != "",
		"jwt_previous_secret_configured": s.JwtPreviousSecret != "",
		"jwt_issuer":                     s.JwtIssuer,
		"jwt_leeway_seconds":             s.JwtLeewaySeconds,
		"allow_unsigned_requests":        s.AllowUnsignedRequests,
	})
}

func buildOpenWebUILinkResponse(link *model.OpenWebUILink) (*openWebUILinkResponse, error) {
	resp := &openWebUILinkResponse{
		Id: link.Id, Sub: link.Sub, UserId: link.UserId, Email: link.Email,
		LinkedBy: link.LinkedBy, CreatedAt: link.CreatedAt,
		Tokens: make([]openWebUITokenStatus, 0, 2),
	}
	for _, name := range []string{openwebui.TokenNameChat, openwebui.TokenNameTask} {
		status := openWebUITokenStatus{Name: name}
		token, err := model.FindOldestUserTokenByName(link.UserId, name)
		if err == nil {
			status.Exists = true
			status.Status = token.Status
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		resp.Tokens = append(resp.Tokens, status)
	}
	return resp, nil
}

// requireManageableUser loads the target user and enforces the same role
// rule as the other admin user endpoints: admins manage users below them,
// root manages everyone.
func requireManageableUser(c *gin.Context, userId int) (*model.User, bool) {
	user, err := model.GetUserById(userId, false)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		} else {
			common.ApiError(c, err)
		}
		return nil, false
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
		return nil, false
	}
	return user, true
}

// GetOpenWebUILink answers ?user_id= or ?sub=. "No link" is a successful
// response with data: null so the UI can render the link form.
func GetOpenWebUILink(c *gin.Context) {
	var link *model.OpenWebUILink
	var err error
	switch {
	case c.Query("user_id") != "":
		userId, convErr := strconv.Atoi(c.Query("user_id"))
		if convErr != nil || userId <= 0 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		link, err = model.GetOpenWebUILinkByUserId(userId)
	case strings.TrimSpace(c.Query("sub")) != "":
		link, err = model.GetOpenWebUILinkBySub(strings.TrimSpace(c.Query("sub")))
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiSuccess(c, nil)
			return
		}
		common.ApiError(c, err)
		return
	}
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func CreateOpenWebUILink(c *gin.Context) {
	var req struct {
		UserId int    `json:"user_id"`
		Sub    string `json:"sub"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	req.Sub = strings.TrimSpace(req.Sub)
	if req.Sub == "" {
		common.ApiErrorI18n(c, i18n.MsgOpenWebUISubRequired)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, ok := requireManageableUser(c, req.UserId)
	if !ok {
		return
	}
	link := &model.OpenWebUILink{
		Sub:      req.Sub,
		UserId:   user.Id,
		Email:    strings.ToLower(strings.TrimSpace(user.Email)),
		LinkedBy: model.OpenWebUILinkedByAdmin,
	}
	if err := model.CreateOpenWebUILink(link); err != nil {
		if errors.Is(err, model.ErrOpenWebUILinkConflict) {
			common.ApiErrorI18n(c, i18n.MsgOpenWebUILinkConflict)
			return
		}
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	resp, err := buildOpenWebUILinkResponse(link)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, resp)
}

func DeleteOpenWebUILink(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	link, err := model.GetOpenWebUILinkById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorI18n(c, i18n.MsgOpenWebUILinkNotFound)
			return
		}
		common.ApiError(c, err)
		return
	}
	// A deleted user leaves a dangling link; admins must still be able to
	// remove it, so only enforce the role rule when the user exists.
	if user, getErr := model.GetUserById(link.UserId, false); getErr == nil {
		if !canManageTargetRole(c.GetInt("role"), user.Role) {
			common.ApiErrorI18n(c, i18n.MsgAuthInsufficientPrivilege)
			return
		}
	} else if !errors.Is(getErr, gorm.ErrRecordNotFound) {
		common.ApiError(c, getErr)
		return
	}
	if err := model.DeleteOpenWebUILink(id); err != nil {
		common.ApiError(c, err)
		return
	}
	openwebui.InvalidateSub(link.Sub)
	common.ApiSuccess(c, nil)
}
```

`i18n.MsgUserNotExists` (`user.not_exists`) already exists in `i18n/keys.go`.

- [ ] **Step 4: Register the routes**

In `router/api-router.go`, after the closing `}` of the `adminRoute := apiRouter.Group("/admin")` block, add:

```go
		openWebUIRoute := apiRouter.Group("/openwebui")
		openWebUIRoute.Use(middleware.AdminAuth())
		{
			openWebUIRoute.GET("/relay-status", controller.GetOpenWebUIRelayStatus)
			openWebUIRoute.GET("/links", controller.GetOpenWebUILink)
			openWebUIRoute.POST("/links", controller.CreateOpenWebUILink)
			openWebUIRoute.DELETE("/links/:id", controller.DeleteOpenWebUILink)
		}
```

- [ ] **Step 5: Run the tests and build**

Run: `go test ./controller/ -run 'OpenWebUI' -count=1 && go build ./... && go vet ./controller/ ./service/openwebui/ ./middleware/ ./model/`
Expected: PASS, build and vet clean.

- [ ] **Step 6: Commit**

```bash
git add controller/openwebui_link.go controller/openwebui_link_test.go router/api-router.go
git commit -m "feat(openwebui): admin API for relay status and manual user links

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: Frontend — relay settings section

**Files:**
- Modify: `web/default/src/features/system-settings/types.ts` (`SiteSettings`)
- Modify: `web/default/src/features/system-settings/site/index.tsx` (`defaultSiteSettings`)
- Modify: `web/default/src/features/system-settings/site/section-registry.tsx` (new section entry)
- Modify: `web/default/src/features/system-settings/api.ts` (`getOpenWebUIRelayStatus`)
- Create: `web/default/src/features/system-settings/site/openwebui-relay-section.tsx`
- Modify: `web/default/src/i18n/locales/en.json`, `web/default/src/i18n/locales/kr.json`

**Interfaces:**
- Consumes: `GET /api/openwebui/relay-status` (Task 10); option keys `openwebui.*` (Task 1); `useUpdateOption`, `SettingsSection`, `SettingsForm`, `SettingsSwitchItem`, `SettingsSwitchContent`, `SettingsPageFormActions` (existing)
- Produces: `OpenWebUIRelaySection({ defaultValues })`, `getOpenWebUIRelayStatus(): Promise<ApiResponse<OpenWebUIRelayStatus>>`, section id `'openwebui-relay'`

- [ ] **Step 1: Extend the settings types and defaults**

In `web/default/src/features/system-settings/types.ts`, append inside `SiteSettings` after `'oauth2.replace_playground': boolean`:

```ts
  /**
   * Open WebUI relay: Open WebUI calls new-api with one shared service key
   * plus a signed per-request user JWT; new-api bills the identified user.
   * The three secrets are write-only (hidden from GET /api/option/).
   */
  'openwebui.relay_enabled': boolean
  'openwebui.service_api_key': string
  'openwebui.jwt_secret': string
  'openwebui.jwt_previous_secret': string
  'openwebui.jwt_issuer': string
  'openwebui.jwt_leeway_seconds': number
  'openwebui.allow_unsigned_requests': boolean
```

Also add, near the other response types in the same file:

```ts
export type OpenWebUIRelayStatus = {
  relay_enabled: boolean
  service_key_configured: boolean
  jwt_secret_configured: boolean
  jwt_previous_secret_configured: boolean
  jwt_issuer: string
  jwt_leeway_seconds: number
  allow_unsigned_requests: boolean
}
```

In `web/default/src/features/system-settings/site/index.tsx`, append to `defaultSiteSettings` after `'oauth2.replace_playground': false,`:

```ts
  'openwebui.relay_enabled': false,
  'openwebui.service_api_key': '',
  'openwebui.jwt_secret': '',
  'openwebui.jwt_previous_secret': '',
  'openwebui.jwt_issuer': 'open-webui',
  'openwebui.jwt_leeway_seconds': 60,
  'openwebui.allow_unsigned_requests': true,
```

- [ ] **Step 2: Add the status API call**

In `web/default/src/features/system-settings/api.ts`, add the import of `OpenWebUIRelayStatus` from `./types` and:

```ts
export async function getOpenWebUIRelayStatus() {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: OpenWebUIRelayStatus
  }>('/api/openwebui/relay-status')
  return res.data
}
```

- [ ] **Step 3: Create the section component**

`web/default/src/features/system-settings/site/openwebui-relay-section.tsx` (copy the license header block from `openwebui-integration-section.tsx` verbatim at the top):

```tsx
import { useEffect, useMemo, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { getOpenWebUIRelayStatus } from '../api'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * Relay half of the Open WebUI integration. Open WebUI sends every user's
 * model call with one shared service key plus a per-request HS256 JWT
 * (X-OpenWebUI-User-Jwt); new-api verifies it and bills the identified
 * user's own `openwebui` token. The login half (OIDC) lives in the sibling
 * "OpenWebUI Integration" section.
 *
 * service_api_key / jwt_secret / jwt_previous_secret are write-only: the
 * backend hides them from GET /api/option/, so the form shows whether each
 * is configured (from /api/openwebui/relay-status) and only sends a value
 * when the admin types one.
 */
const relaySchema = z.object({
  openwebui: z.object({
    relay_enabled: z.boolean(),
    service_api_key: z.string(),
    jwt_secret: z
      .string()
      .refine((v) => v === '' || v.length >= 32, {
        message: 'JWT secret must be at least 32 characters',
      }),
    jwt_previous_secret: z
      .string()
      .refine((v) => v === '' || v.length >= 32, {
        message: 'JWT secret must be at least 32 characters',
      }),
    jwt_issuer: z.string().min(1),
    jwt_leeway_seconds: z.coerce.number().int().min(0).max(600),
    allow_unsigned_requests: z.boolean(),
  }),
})

type RelayFormValues = z.infer<typeof relaySchema>

type FlatRelayDefaults = {
  'openwebui.relay_enabled': boolean
  'openwebui.service_api_key': string
  'openwebui.jwt_secret': string
  'openwebui.jwt_previous_secret': string
  'openwebui.jwt_issuer': string
  'openwebui.jwt_leeway_seconds': number
  'openwebui.allow_unsigned_requests': boolean
}

const buildFormDefaults = (d: FlatRelayDefaults): RelayFormValues => ({
  openwebui: {
    relay_enabled: d['openwebui.relay_enabled'],
    service_api_key: d['openwebui.service_api_key'] ?? '',
    jwt_secret: d['openwebui.jwt_secret'] ?? '',
    jwt_previous_secret: d['openwebui.jwt_previous_secret'] ?? '',
    jwt_issuer: d['openwebui.jwt_issuer'] || 'open-webui',
    jwt_leeway_seconds: d['openwebui.jwt_leeway_seconds'] ?? 60,
    allow_unsigned_requests: d['openwebui.allow_unsigned_requests'],
  },
})

const normalizeFormValues = (v: RelayFormValues): FlatRelayDefaults => ({
  'openwebui.relay_enabled': v.openwebui.relay_enabled,
  'openwebui.service_api_key': v.openwebui.service_api_key,
  'openwebui.jwt_secret': v.openwebui.jwt_secret,
  'openwebui.jwt_previous_secret': v.openwebui.jwt_previous_secret,
  'openwebui.jwt_issuer': v.openwebui.jwt_issuer,
  'openwebui.jwt_leeway_seconds': v.openwebui.jwt_leeway_seconds,
  'openwebui.allow_unsigned_requests': v.openwebui.allow_unsigned_requests,
})

// Secrets are write-only: an empty field means "keep the current value",
// never "clear it". Save order matters — the backend refuses to enable the
// relay before the key and secret exist, so relay_enabled goes last.
const SECRET_KEYS: Array<keyof FlatRelayDefaults> = [
  'openwebui.service_api_key',
  'openwebui.jwt_secret',
  'openwebui.jwt_previous_secret',
]

interface OpenWebUIRelaySectionProps {
  defaultValues: FlatRelayDefaults
}

function ConfiguredBadge({ configured }: { configured: boolean | undefined }) {
  const { t } = useTranslation()
  if (configured === undefined) return null
  return (
    <Badge variant={configured ? 'default' : 'outline'} className='ml-2'>
      {configured ? t('Configured') : t('Not configured')}
    </Badge>
  )
}

export function OpenWebUIRelaySection(props: OpenWebUIRelaySectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const queryClient = useQueryClient()

  const statusQuery = useQuery({
    queryKey: ['openwebui-relay-status'],
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const status = statusQuery.data?.data

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<RelayFormValues>({
    resolver: zodResolver(relaySchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatRelayDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: RelayFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatRelayDefaults>
    )
      .filter((key) => normalized[key] !== baselineRef.current[key])
      // An untouched (empty) secret field is not a change.
      .filter((key) => !(SECRET_KEYS.includes(key) && normalized[key] === ''))
      .sort((a, b) => {
        if (a === 'openwebui.relay_enabled') return 1
        if (b === 'openwebui.relay_enabled') return -1
        return 0
      })

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      const result = await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
      if (!result.success) {
        // useUpdateOption already toasted the backend message; stop here so
        // relay_enabled is not attempted after a failed secret save.
        return
      }
    }

    // Secrets were accepted by the server; blank them again in the form so
    // the field goes back to "keep current value".
    const nextBaseline: FlatRelayDefaults = {
      ...normalized,
      'openwebui.service_api_key': '',
      'openwebui.jwt_secret': '',
      'openwebui.jwt_previous_secret': '',
    }
    baselineRef.current = nextBaseline
    baselineSerializedRef.current = JSON.stringify(nextBaseline)
    form.reset(buildFormDefaults(nextBaseline))
    queryClient.invalidateQueries({ queryKey: ['openwebui-relay-status'] })
  }

  return (
    <SettingsSection title={t('OpenWebUI Relay')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <FormField
            control={form.control}
            name='openwebui.relay_enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Open WebUI relay')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Bill requests that arrive with the service key to the Open WebUI user identified by the signed X-OpenWebUI-User-Jwt header. Requires the service key and a JWT secret below.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.service_api_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Service key')}
                  <ConfiguredBadge configured={status?.service_key_configured} />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='off'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The key of an enabled token that Open WebUI uses as OPENAI_API_KEYS. Paste it with or without the sk- prefix. IP limits on that token restrict where Open WebUI may call from.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('JWT secret')}
                  <ConfiguredBadge configured={status?.jwt_secret_configured} />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    "Must equal Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET (32+ random characters)."
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_previous_secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Previous JWT secret')}
                  <ConfiguredBadge
                    configured={status?.jwt_previous_secret_configured}
                  />
                </FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Leave empty to keep the current value')}
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Accepted alongside the current secret while rotating. Move the old secret here, set the new one above, restart Open WebUI, then clear this field via the relay-status API or by saving a new rotation.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_issuer'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('JWT issuer')}</FormLabel>
                <FormControl>
                  <Input
                    autoComplete='off'
                    value={field.value ?? ''}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t('Expected iss claim. Open WebUI always sends open-webui.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.jwt_leeway_seconds'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Clock skew allowance (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={600}
                    value={field.value ?? 60}
                    onChange={(e) => field.onChange(e.target.value)}
                    name={field.name}
                    onBlur={field.onBlur}
                    ref={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Applied to exp and iat. Open WebUI JWTs live 300 seconds by default; 60 is a safe start.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='openwebui.allow_unsigned_requests'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Allow requests without a user JWT')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Some Open WebUI audio routes send the service key without a JWT. On: bill them to the service token itself (logged as unsigned). Off: reject them with 401. A present but invalid JWT is always rejected.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
```

Clearing `jwt_previous_secret` after a rotation: because an empty secret field means "keep", the form cannot clear it. The description above tells the admin how; the DELETE-style clear is intentionally out of 1st-phase scope (spec §5.3 step 3 is satisfied by setting a new rotation or via `PUT /api/option/ {"key":"openwebui.jwt_previous_secret","value":""}`, which Task 9 accepts). Document this in the spec in Task 13.

If `@/components/ui/badge` does not exist, replace `<Badge …>` with `<span className='text-muted-foreground ml-2 text-xs'>…</span>`.

- [ ] **Step 4: Register the section**

In `web/default/src/features/system-settings/site/section-registry.tsx`:
- add `import { OpenWebUIRelaySection } from './openwebui-relay-section'`
- append to `SITE_SECTIONS` after the `openwebui-integration` entry:

```tsx
  {
    id: 'openwebui-relay',
    titleKey: 'OpenWebUI Relay',
    build: (settings: SiteSettings) => (
      <OpenWebUIRelaySection
        defaultValues={{
          'openwebui.relay_enabled': settings['openwebui.relay_enabled'],
          'openwebui.service_api_key': settings['openwebui.service_api_key'],
          'openwebui.jwt_secret': settings['openwebui.jwt_secret'],
          'openwebui.jwt_previous_secret':
            settings['openwebui.jwt_previous_secret'],
          'openwebui.jwt_issuer': settings['openwebui.jwt_issuer'],
          'openwebui.jwt_leeway_seconds':
            settings['openwebui.jwt_leeway_seconds'],
          'openwebui.allow_unsigned_requests':
            settings['openwebui.allow_unsigned_requests'],
        }}
      />
    ),
  },
```

- [ ] **Step 5: Add translations**

Add to `web/default/src/i18n/locales/en.json` inside `"translation"` (value = key for every entry):

```
"OpenWebUI Relay"
"Enable Open WebUI relay"
"Bill requests that arrive with the service key to the Open WebUI user identified by the signed X-OpenWebUI-User-Jwt header. Requires the service key and a JWT secret below."
"Service key"
"Leave empty to keep the current value"
"The key of an enabled token that Open WebUI uses as OPENAI_API_KEYS. Paste it with or without the sk- prefix. IP limits on that token restrict where Open WebUI may call from."
"JWT secret"
"Must equal Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET (32+ random characters)."
"Previous JWT secret"
"Accepted alongside the current secret while rotating. Move the old secret here, set the new one above, restart Open WebUI, then clear this field via the relay-status API or by saving a new rotation."
"JWT issuer"
"Expected iss claim. Open WebUI always sends open-webui."
"Clock skew allowance (seconds)"
"Applied to exp and iat. Open WebUI JWTs live 300 seconds by default; 60 is a safe start."
"Allow requests without a user JWT"
"Some Open WebUI audio routes send the service key without a JWT. On: bill them to the service token itself (logged as unsigned). Off: reject them with 401. A present but invalid JWT is always rejected."
"Configured"
"Not configured"
```

Add the same keys to `kr.json` with these values, in order:

```
"OpenWebUI Relay": "OpenWebUI Relay"
"Enable Open WebUI relay": "Open WebUI relay 활성화"
"Bill requests ...": "서비스 키로 들어온 요청을 서명된 X-OpenWebUI-User-Jwt 헤더가 가리키는 Open WebUI 사용자에게 청구합니다. 아래 서비스 키와 JWT 비밀이 필요합니다."
"Service key": "서비스 키"
"Leave empty to keep the current value": "비워 두면 현재 값을 유지합니다"
"The key of an enabled token ...": "Open WebUI가 OPENAI_API_KEYS로 쓰는 활성 토큰의 키입니다. sk- 접두사가 있어도 됩니다. 이 토큰의 IP 제한이 Open WebUI가 호출할 수 있는 위치를 제한합니다."
"JWT secret": "JWT 비밀"
"Must equal Open WebUI's FORWARD_USER_INFO_HEADER_JWT_SECRET (32+ random characters).": "Open WebUI의 FORWARD_USER_INFO_HEADER_JWT_SECRET과 같아야 합니다(32자 이상 무작위)."
"Previous JWT secret": "이전 JWT 비밀"
"Accepted alongside ...": "교체 중에 현재 비밀과 함께 허용됩니다. 구 비밀을 여기로 옮기고 위에 새 비밀을 넣은 뒤 Open WebUI를 재시작하세요. 안정되면 relay-status API나 새 교체 저장으로 이 값을 비웁니다."
"JWT issuer": "JWT 발급자"
"Expected iss claim. Open WebUI always sends open-webui.": "기대하는 iss 클레임입니다. Open WebUI는 항상 open-webui를 보냅니다."
"Clock skew allowance (seconds)": "시계 오차 허용(초)"
"Applied to exp and iat. ...": "exp와 iat에 적용됩니다. Open WebUI JWT 수명은 기본 300초이며 60으로 시작하면 안전합니다."
"Allow requests without a user JWT": "사용자 JWT 없는 요청 허용"
"Some Open WebUI audio routes ...": "Open WebUI의 일부 음성 경로는 JWT 없이 서비스 키만 보냅니다. 켜면 서비스 토큰 자체로 청구하고 미서명으로 기록합니다. 끄면 401로 거부합니다. JWT가 있는데 잘못된 경우는 항상 거부합니다."
"Configured": "설정됨"
"Not configured": "설정되지 않음"
```

(Use the full English sentence as the key for the abbreviated `...` rows.) Then run `cd web/default && bun run i18n:sync` and confirm it reports no missing keys for `kr`.

- [ ] **Step 6: Typecheck and lint**

Run: `cd web/default && bun run typecheck && bun run lint`
Expected: no errors. Fix any unused-import or type complaints inline.

- [ ] **Step 7: Verify in the browser**

Start the dev server with `preview_start` (`.claude/launch.json` entry for `web/default`, `bun run dev`) against a running backend, open System Settings → Site → "OpenWebUI Relay", confirm: the two "Not configured" badges render, saving a 10-char secret shows the zod error, saving a 32-char secret + valid service key then enabling the relay succeeds and the badges flip to "Configured".

- [ ] **Step 8: Commit**

```bash
git add web/default/src/features/system-settings/types.ts web/default/src/features/system-settings/site/index.tsx web/default/src/features/system-settings/site/section-registry.tsx web/default/src/features/system-settings/api.ts web/default/src/features/system-settings/site/openwebui-relay-section.tsx web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json
git commit -m "feat(openwebui): relay settings section in system settings

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 12: Frontend — Open WebUI link block in the user drawer

**Files:**
- Modify: `web/default/src/features/users/types.ts` (new types)
- Modify: `web/default/src/features/users/api.ts` (link API)
- Create: `web/default/src/features/users/components/user-openwebui-link-section.tsx`
- Modify: `web/default/src/features/users/components/users-mutate-drawer.tsx` (render the section when `isUpdate`)
- Modify: `web/default/src/i18n/locales/en.json`, `web/default/src/i18n/locales/kr.json`

**Interfaces:**
- Consumes: `GET /api/openwebui/relay-status`, `GET/POST /api/openwebui/links`, `DELETE /api/openwebui/links/:id` (Task 10); `getOpenWebUIRelayStatus` (Task 11, re-exported via `@/features/system-settings/api`)
- Produces: `OpenWebUILink`, `OpenWebUITokenStatus` types; `getOpenWebUILink(userId)`, `createOpenWebUILink(userId, sub)`, `deleteOpenWebUILink(id)`; component `UserOpenWebUILinkSection({ userId })`

- [ ] **Step 1: Add types and API functions**

Append to `web/default/src/features/users/types.ts`:

```ts
// ============================================================================
// Open WebUI relay link
// ============================================================================

export type OpenWebUITokenStatus = {
  name: string
  exists: boolean
  status: number
}

export type OpenWebUILink = {
  id: number
  sub: string
  user_id: number
  email: string
  linked_by: 'auto' | 'admin'
  created_at: number
  tokens: OpenWebUITokenStatus[]
}
```

Append to `web/default/src/features/users/api.ts` (add `OpenWebUILink` to the type import list):

```ts
// ============================================================================
// Open WebUI relay link (admin)
// ============================================================================

export async function getOpenWebUILink(
  userId: number
): Promise<ApiResponse<OpenWebUILink | null>> {
  const res = await api.get(`/api/openwebui/links?user_id=${userId}`)
  return res.data
}

export async function createOpenWebUILink(
  userId: number,
  sub: string
): Promise<ApiResponse<OpenWebUILink>> {
  const res = await api.post('/api/openwebui/links', { user_id: userId, sub })
  return res.data
}

export async function deleteOpenWebUILink(
  id: number
): Promise<ApiResponse<null>> {
  const res = await api.delete(`/api/openwebui/links/${id}`)
  return res.data
}
```

- [ ] **Step 2: Create the section component**

`web/default/src/features/users/components/user-openwebui-link-section.tsx` (license header first, copied verbatim from `users-mutate-drawer.tsx`):

```tsx
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getOpenWebUIRelayStatus } from '@/features/system-settings/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SideDrawerSection } from '@/components/drawer-layout'
import {
  createOpenWebUILink,
  deleteOpenWebUILink,
  getOpenWebUILink,
} from '../api'
import type { OpenWebUITokenStatus } from '../types'

const TOKEN_STATUS_LABEL: Record<number, string> = {
  1: 'Enabled',
  2: 'Disabled',
  3: 'Expired',
  4: 'Exhausted',
}

function tokenStatusText(token: OpenWebUITokenStatus, t: (k: string) => string) {
  if (!token.exists) return t('Not created yet')
  return t(TOKEN_STATUS_LABEL[token.status] ?? 'Unknown')
}

/**
 * Admin view of the Open WebUI ↔ new-api user link. Links are created
 * automatically by email on the first relay request; this block exists for
 * the cases that cannot auto-link (no matching email, duplicate emails, or
 * a recreated Open WebUI account whose new sub must replace the old one).
 * Hidden entirely while the relay is disabled.
 */
export function UserOpenWebUILinkSection({ userId }: { userId: number }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [sub, setSub] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  const statusQuery = useQuery({
    queryKey: ['openwebui-relay-status'],
    queryFn: getOpenWebUIRelayStatus,
    staleTime: 60 * 1000,
  })
  const relayEnabled = statusQuery.data?.data?.relay_enabled === true

  const linkQuery = useQuery({
    queryKey: ['openwebui-link', userId],
    queryFn: () => getOpenWebUILink(userId),
    enabled: relayEnabled && userId > 0,
  })
  const link = linkQuery.data?.data ?? null

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['openwebui-link', userId] })

  const linkMutation = useMutation({
    mutationFn: () => createOpenWebUILink(userId, sub.trim()),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Open WebUI account linked'))
        setSub('')
        refresh()
      } else {
        toast.error(res.message || t('Failed to link Open WebUI account'))
      }
    },
    onError: () => toast.error(t('Failed to link Open WebUI account')),
  })

  const unlinkMutation = useMutation({
    mutationFn: (id: number) => deleteOpenWebUILink(id),
    onSuccess: (res) => {
      if (res.success) {
        toast.success(t('Open WebUI account unlinked'))
        refresh()
      } else {
        toast.error(res.message || t('Failed to unlink Open WebUI account'))
      }
      setConfirmOpen(false)
    },
    onError: () => {
      toast.error(t('Failed to unlink Open WebUI account'))
      setConfirmOpen(false)
    },
  })

  if (!relayEnabled) return null

  return (
    <SideDrawerSection>
      <h3 className='text-sm font-medium'>{t('Open WebUI Link')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Requests from Open WebUI are billed to this user once their Open WebUI account (sub) is linked. Linking happens automatically by email on the first request; use this when it did not.'
        )}
      </p>

      {linkQuery.isLoading ? (
        <p className='text-muted-foreground text-xs'>{t('Loading...')}</p>
      ) : link ? (
        <div className='flex flex-col gap-3'>
          <div>
            <Label className='text-muted-foreground text-xs'>
              {t('Open WebUI user id (sub)')}
            </Label>
            <Input value={link.sub} disabled className='mt-1 font-mono' />
          </div>
          <div className='text-muted-foreground grid grid-cols-2 gap-2 text-xs'>
            <span>
              {t('Linked by')}:{' '}
              {link.linked_by === 'auto' ? t('Automatic (email)') : t('Admin')}
            </span>
            <span>
              {t('Linked at')}:{' '}
              {new Date(link.created_at * 1000).toLocaleString()}
            </span>
            {link.tokens.map((token) => (
              <span key={token.name}>
                {token.name}: {tokenStatusText(token, t)}
              </span>
            ))}
          </div>
          <div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setConfirmOpen(true)}
              disabled={unlinkMutation.isPending}
            >
              {t('Unlink')}
            </Button>
          </div>
          <ConfirmDialog
            open={confirmOpen}
            onOpenChange={setConfirmOpen}
            title={t('Unlink Open WebUI account?')}
            desc={t(
              'The next request from this Open WebUI account will try to auto-link by email again. The openwebui tokens are kept so usage history stays intact.'
            )}
            destructive
            confirmText={t('Unlink')}
            isLoading={unlinkMutation.isPending}
            handleConfirm={() => unlinkMutation.mutate(link.id)}
          />
        </div>
      ) : (
        <div className='flex flex-col gap-2'>
          <Label className='text-muted-foreground text-xs'>
            {t('Open WebUI user id (sub)')}
          </Label>
          <div className='flex gap-2'>
            <Input
              value={sub}
              onChange={(e) => setSub(e.target.value)}
              placeholder={t('Paste the sub from the server log or Open WebUI admin page')}
              className='flex-1 font-mono'
            />
            <Button
              type='button'
              size='sm'
              onClick={() => linkMutation.mutate()}
              disabled={sub.trim() === '' || linkMutation.isPending}
            >
              {t('Link')}
            </Button>
          </div>
        </div>
      )}
    </SideDrawerSection>
  )
}
```

Check `ConfirmDialog`'s `desc` prop accepts a string (it does: `desc: React.JSX.Element | string`). Check `Button` accepts `size='sm'`; if not, drop the prop.

- [ ] **Step 3: Render it in the drawer**

In `web/default/src/features/users/components/users-mutate-drawer.tsx`:
- add `import { UserOpenWebUILinkSection } from './user-openwebui-link-section'`
- directly **before** the `{/* Binding Information (Read-only) */}` comment, add:

```tsx
              {/* Open WebUI relay link (admin-managed) */}
              {isUpdate && currentRow && (
                <UserOpenWebUILinkSection userId={currentRow.id} />
              )}
```

- [ ] **Step 4: Add translations**

`en.json` (value = key):

```
"Open WebUI Link"
"Requests from Open WebUI are billed to this user once their Open WebUI account (sub) is linked. Linking happens automatically by email on the first request; use this when it did not."
"Open WebUI user id (sub)"
"Linked by"
"Automatic (email)"
"Linked at"
"Not created yet"
"Enabled"
"Disabled"
"Expired"
"Exhausted"
"Unknown"
"Unlink"
"Unlink Open WebUI account?"
"The next request from this Open WebUI account will try to auto-link by email again. The openwebui tokens are kept so usage history stays intact."
"Paste the sub from the server log or Open WebUI admin page"
"Link"
"Open WebUI account linked"
"Failed to link Open WebUI account"
"Open WebUI account unlinked"
"Failed to unlink Open WebUI account"
"Loading..."
```

Several of these (`Enabled`, `Disabled`, `Expired`, `Exhausted`, `Unknown`, `Admin`, `Loading...`) very likely already exist — `grep -c '"Enabled":' web/default/src/i18n/locales/en.json` before adding; add only the missing ones.

`kr.json` values for the new ones:

```
"Open WebUI Link": "Open WebUI 연결"
"Requests from Open WebUI are billed ...": "Open WebUI 계정(sub)이 연결되면 Open WebUI에서 온 요청이 이 사용자에게 청구됩니다. 첫 요청 때 이메일로 자동 연결되며, 자동 연결이 되지 않았을 때 여기서 연결합니다."
"Open WebUI user id (sub)": "Open WebUI 사용자 ID(sub)"
"Linked by": "연결 방식"
"Automatic (email)": "자동(이메일)"
"Linked at": "연결 일시"
"Not created yet": "아직 생성되지 않음"
"Unlink": "연결 해제"
"Unlink Open WebUI account?": "Open WebUI 계정 연결을 해제할까요?"
"The next request from this Open WebUI account ...": "이 Open WebUI 계정의 다음 요청에서 이메일 자동 연결을 다시 시도합니다. 사용 기록을 유지하기 위해 openwebui 토큰은 삭제하지 않습니다."
"Paste the sub from the server log or Open WebUI admin page": "서버 로그나 Open WebUI 관리자 화면의 sub 값을 붙여 넣으세요"
"Link": "연결"
"Open WebUI account linked": "Open WebUI 계정을 연결했습니다"
"Failed to link Open WebUI account": "Open WebUI 계정 연결에 실패했습니다"
"Open WebUI account unlinked": "Open WebUI 계정 연결을 해제했습니다"
"Failed to unlink Open WebUI account": "Open WebUI 계정 연결 해제에 실패했습니다"
```

Then `cd web/default && bun run i18n:sync`.

- [ ] **Step 5: Typecheck, lint, browser check**

Run: `cd web/default && bun run typecheck && bun run lint`
Expected: clean.

Browser (preview_start): with the relay enabled, open Users → edit a user → the "Open WebUI Link" block shows the sub input; paste a sub, click Link → block switches to the linked view with two "Not created yet" token rows; Unlink → confirm dialog → back to the input. With the relay disabled the block is absent.

- [ ] **Step 6: Commit**

```bash
git add web/default/src/features/users/types.ts web/default/src/features/users/api.ts web/default/src/features/users/components/user-openwebui-link-section.tsx web/default/src/features/users/components/users-mutate-drawer.tsx web/default/src/i18n/locales/en.json web/default/src/i18n/locales/kr.json
git commit -m "feat(openwebui): manage Open WebUI user links from the user drawer

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 13: Full verification, spec touch-up, final commit

**Files:**
- Modify: `docs/superpowers/specs/2026-10-07-openwebui-relay-jwt-design.md` (§2.1 field names, §4.2 secret visibility, §5.3 clearing the previous secret)

- [ ] **Step 1: Run the whole backend suite**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected: build/vet clean; all tests PASS. The `./controller/...` and `./service/...` packages are what `test.sh` runs in CI, so they must be green.

- [ ] **Step 2: Run the frontend checks**

Run: `cd web/default && bun run typecheck && bun run lint && bun test src`
Expected: clean.

- [ ] **Step 3: Manual end-to-end with curl against a local backend**

With the backend running (`go run main.go`, SQLite), an admin session, a service token `SVC` and a user `alice@example.com` existing:

```bash
# enable relay via options (secret 32+ chars)
curl -s -X PUT localhost:3000/api/option/ -H 'Content-Type: application/json' -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" -H "New-Api-User: $ADMIN_ID" -d '{"key":"openwebui.service_api_key","value":"'"$SVC"'"}'
curl -s -X PUT localhost:3000/api/option/ -H 'Content-Type: application/json' -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" -H "New-Api-User: $ADMIN_ID" -d '{"key":"openwebui.jwt_secret","value":"0123456789abcdef0123456789abcdef"}'
curl -s -X PUT localhost:3000/api/option/ -H 'Content-Type: application/json' -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" -H "New-Api-User: $ADMIN_ID" -d '{"key":"openwebui.relay_enabled","value":true}'
```

Mint a JWT (Python one-liner, `pip install pyjwt` if needed):

```bash
JWT=$(python3 -c "import jwt,time;print(jwt.encode({'sub':'owui-1','email':'alice@example.com','name':'Alice','role':'user','iss':'open-webui','iat':int(time.time()),'exp':int(time.time())+300},'0123456789abcdef0123456789abcdef',algorithm='HS256'))")
```

Then:

```bash
curl -s localhost:3000/v1/models -H "Authorization: Bearer $SVC" -H "X-OpenWebUI-User-Jwt: $JWT" | head -c 300
```

Expected: a model list; in the dashboard, alice now owns a token named `openwebui`; a second call with `-H 'X-Task: title_generation'` creates `openwebui-task`. A call with a wrong-secret JWT returns 401 with `"type":"new_api_error"`, and the server log shows `openwebui: rejected (401) jwt_invalid: ...`. A call with no JWT returns 200 and the consume log `other` contains `owui_unsigned: true`.

- [ ] **Step 4: Update the spec to match what was built**

In the spec:
- §2.1 table: rename `service_key` → `service_api_key`, `jwt_secret_previous` → `jwt_previous_secret`; add a sentence: "세 비밀은 이름 규칙(`api_key`/`secret` 접미사) 때문에 `GET /api/option/`에서 숨겨지며, 설정 여부는 `GET /api/openwebui/relay-status`로 확인한다."
- §4.1 table: add the `GET /relay-status` row.
- §4.2: replace "조회 응답에서 … 관리자에게 반환" with "조회 응답에 포함되지 않는다(쓰기 전용). UI는 relay-status로 설정 여부만 표시하고, 빈 입력은 '현재 값 유지'로 처리한다."
- §4.3 first bullet: the relay block is its own section `openwebui-relay` ("OpenWebUI Relay") next to the existing one, not a block inside it.
- §5.3 step 3: "`jwt_previous_secret`을 비우려면 `PUT /api/option/` `{"key":"openwebui.jwt_previous_secret","value":""}`를 보낸다(UI의 빈 입력은 유지를 뜻한다)."
- Field name `allow_unsigned_requests` and all `openwebui.*` keys: confirm they match `setting/system_setting/openwebui.go`.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-10-07-openwebui-relay-jwt-design.md
git commit -m "docs(openwebui): align relay spec with implemented option names and secret handling

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

- [ ] **Step 6: Hand off**

Use `superpowers:finishing-a-development-branch` to decide between merging into `OpenWebUI`, opening a PR against `main`, or continuing with the OIDC spec.
