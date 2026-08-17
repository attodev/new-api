# OAuth2 Provider for External App Integration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn new-api into a minimal, single-client OAuth2 provider so an external chat app can authenticate a logged-in new-api user and make billed API calls on their behalf, without a DB-persisted, user-visible token.

**Architecture:** IdP-initiated flow. new-api's own (already-authenticated) frontend calls a same-origin `POST /oauth2/session-init` to mint a short-lived, single-use authorization code, then navigates the browser to the external app's callback with that code attached. The external app's backend exchanges the code for an access token (`POST /oauth2/token`) and fetches identity (`GET /oauth2/userinfo`) — both server-to-server. The issued token is a real `model.Token` struct cached directly in Redis (same format the existing token cache already uses) with a 24h TTL — no DB row, so it never appears in the user's token list, and it's validated by the existing `TokenAuth()` / `GetTokenByKey` path completely unchanged.

**Tech Stack:** Go 1.22+, Gin, GORM, Redis (`github.com/go-redis/redis/v8`), `github.com/alicebob/miniredis/v2` (new test-only dependency), React 19 / TanStack Router (frontend).

## Global Constraints

- All JSON marshal/unmarshal in new code must go through `common/json.go` wrappers, not `encoding/json` directly (project rule). Note: `gin.Context.JSON(...)` and `common.RedisHSetObj`/`RedisHGetObj` (which already wrap serialization internally) are not affected by this rule — it applies to explicit marshal/unmarshal calls we write ourselves, and this plan introduces none.
- No raw SQL; no DB schema changes at all in this feature (Redis-only tokens, no new tables).
- Redis key formats introduced: `oauth2:code:<code>` (120s TTL) and `oauth2:session_token:<userId>` (24h TTL), alongside the existing `token:<hmac(key)>` format (24h TTL for these tokens instead of the generic cache TTL).
- Token TTL is fixed at 24 hours; auth code TTL is fixed at 120 seconds — both exact values from the approved design spec (`docs/superpowers/specs/2026-08-16-oauth2-provider-design.md`).

---

### Task 1: Add atomic Redis GETDEL helper + miniredis test harness

**Files:**
- Modify: `common/redis.go` (add function after `RedisDelKey`, ~line 100)
- Create: `common/redis_test.go`
- Modify: `go.mod` (add `github.com/alicebob/miniredis/v2` as a dependency)

**Interfaces:**
- Produces: `common.RedisGetDel(key string) (string, error)` — atomically reads and deletes a key in one round trip. Returns `redis.Nil` (from `github.com/go-redis/redis/v8`) wrapped by `errors.Is` when the key doesn't exist, matching the existing error-handling convention in this file (see `RedisIncr`, `RedisHIncrBy`, `RedisHSetField` at lines 244/273/300 which all check `errors.Is(err, redis.Nil)`).

This is needed because the OAuth2 auth-code exchange must be exactly-once: a plain `GET` followed by a separate `DEL` has a race window where two near-simultaneous exchange requests could both read the code before either deletes it.

- [ ] **Step 1: Add the miniredis dependency**

Run:
```bash
cd /Users/molla/work/ai/new-api
go get github.com/alicebob/miniredis/v2
```
Expected: `go.mod` gains a line like `github.com/alicebob/miniredis/v2 v2.x.x` and `go.sum` is updated. This is a test-only dependency — no production code depends on it.

- [ ] **Step 2: Write the failing test**

Create `common/redis_test.go`:

```go
package common

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := RDB
	originalEnabled := RedisEnabled
	RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	RedisEnabled = true
	t.Cleanup(func() {
		RDB = originalRDB
		RedisEnabled = originalEnabled
	})
}

func TestRedisGetDel_ReturnsValueAndDeletesKey(t *testing.T) {
	setupTestRedis(t)

	require.NoError(t, RedisSet("test:getdel:key", "hello", 0))

	val, err := RedisGetDel("test:getdel:key")
	require.NoError(t, err)
	require.Equal(t, "hello", val)

	_, err = RedisGet("test:getdel:key")
	require.Error(t, err, "key must be gone after GetDel")
}

func TestRedisGetDel_MissingKeyReturnsRedisNil(t *testing.T) {
	setupTestRedis(t)

	_, err := RedisGetDel("test:getdel:does-not-exist")
	require.Error(t, err)
	require.ErrorIs(t, err, redis.Nil)
}
```

- [ ] **Step 2b: Run the test to verify it fails**

Run: `go test ./common/... -run TestRedisGetDel -v`
Expected: FAIL — `RedisGetDel` is undefined (compile error).

- [ ] **Step 3: Implement `RedisGetDel`**

In `common/redis.go`, add immediately after the `RedisDelKey` function (after line 100):

```go
func RedisGetDel(key string) (string, error) {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis GETDEL: key=%s", key))
	}
	ctx := context.Background()
	return RDB.GetDel(ctx, key).Result()
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./common/... -run TestRedisGetDel -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum common/redis.go common/redis_test.go
git commit -m "feat: add atomic Redis GETDEL helper for OAuth2 auth codes"
```

---

### Task 2: OAuth2 system settings

**Files:**
- Create: `setting/system_setting/oauth2_client.go`
- Create: `setting/system_setting/oauth2_client_test.go`

**Interfaces:**
- Consumes: `setting/config.GlobalConfig` (`Register`, `Get`) — already exists, pattern confirmed in `setting/system_setting/oidc.go`.
- Produces:
  - `type OAuth2Settings struct { Enabled bool; ClientId string; ClientSecret string; RedirectURI string }` — all fields JSON-tagged in snake_case to match the existing `OIDCSettings` convention.
  - `func GetOAuth2Settings() *OAuth2Settings`
  - Admin updates go through the *existing* generic option-update mechanism using dotted keys (`oauth2.enabled`, `oauth2.client_id`, `oauth2.client_secret`, `oauth2.redirect_uri`) — no new admin endpoint is added in this task. This relies on `model/option.go`'s `handleConfigUpdate`, which already dispatches any `"<name>.<key>"` option update to whatever config struct was `Register`ed under `<name>` (confirmed at `model/option.go:1550-1568`).

- [ ] **Step 1: Write the failing test**

Create `setting/system_setting/oauth2_client_test.go`:

```go
package system_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/stretchr/testify/require"
)

func TestOAuth2Settings_RegisteredUnderOAuth2Name(t *testing.T) {
	cfg := config.GlobalConfig.Get("oauth2")
	require.NotNil(t, cfg, "oauth2 config must be registered with GlobalConfig")
	require.IsType(t, &OAuth2Settings{}, cfg)
}

func TestOAuth2Settings_UpdateViaConfigMap(t *testing.T) {
	original := *GetOAuth2Settings()
	t.Cleanup(func() { *GetOAuth2Settings() = original })

	err := config.UpdateConfigFromMap(GetOAuth2Settings(), map[string]string{
		"enabled":      "true",
		"client_id":    "openwebui",
		"redirect_uri": "https://chat.example.com/auth/newapi/callback",
	})
	require.NoError(t, err)

	s := GetOAuth2Settings()
	require.True(t, s.Enabled)
	require.Equal(t, "openwebui", s.ClientId)
	require.Equal(t, "https://chat.example.com/auth/newapi/callback", s.RedirectURI)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./setting/system_setting/... -run TestOAuth2Settings -v`
Expected: FAIL — `OAuth2Settings`, `GetOAuth2Settings` undefined (compile error).

- [ ] **Step 3: Implement the settings file**

Create `setting/system_setting/oauth2_client.go`:

```go
package system_setting

import "github.com/QuantumNous/new-api/setting/config"

type OAuth2Settings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
}

var defaultOAuth2Settings = OAuth2Settings{}

func init() {
	config.GlobalConfig.Register("oauth2", &defaultOAuth2Settings)
}

func GetOAuth2Settings() *OAuth2Settings {
	return &defaultOAuth2Settings
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./setting/system_setting/... -run TestOAuth2Settings -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add setting/system_setting/oauth2_client.go setting/system_setting/oauth2_client_test.go
git commit -m "feat: add OAuth2 client system settings"
```

---

### Task 3: Redis-backed OAuth2 token issuance and auth-code helpers

**Files:**
- Create: `model/oauth2_session.go`
- Create: `model/oauth2_session_test.go`

**Interfaces:**
- Consumes:
  - `common.GenerateKey() (string, error)` (`common/utils.go:251`)
  - `common.GenerateHMAC(data string) string` (`common/crypto.go:23`)
  - `common.RedisSet(key, value string, expiration time.Duration) error`, `common.RedisGet(key string) (string, error)`, `common.RedisDel(key string) error`, `common.RedisHSetObj(key string, obj interface{}, expiration time.Duration) error`, `common.RedisGetDel(key string) (string, error)` (Task 1)
  - `common.GetTimestamp() int64` (existing, used elsewhere in `model/token.go`)
  - `common.TokenStatusEnabled` (= `1`, `common/constants.go:262`)
  - The existing unexported `cacheDeleteToken(key string) error` in `model/token_cache.go:21` (same package `model`, directly callable)
- Produces (all in package `model`):
  - `IssueOAuth2Token(userId int, group string, clientId string) (*Token, error)` — invalidates any prior live OAuth2 token for this user, then mints and caches a new one.
  - `RevokeOAuth2Token(userId int) error` — invalidates the user's live OAuth2 token, if any. Returns `nil` (not an error) if none exists.
  - `StoreAuthCode(userId int) (code string, err error)` — generates and stores a fresh single-use code.
  - `ConsumeAuthCode(code string) (userId int, err error)` — atomically reads and deletes; returns `ErrOAuth2CodeInvalid` if the code doesn't exist or has expired.
  - `var ErrOAuth2CodeInvalid = errors.New("oauth2 code invalid or expired")`

- [ ] **Step 1: Write the failing tests**

Create `model/oauth2_session_test.go`:

```go
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupOAuth2TestRedis(t *testing.T) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
	})
}

func TestStoreAndConsumeAuthCode(t *testing.T) {
	setupOAuth2TestRedis(t)

	code, err := StoreAuthCode(42)
	require.NoError(t, err)
	require.NotEmpty(t, code)

	userId, err := ConsumeAuthCode(code)
	require.NoError(t, err)
	require.Equal(t, 42, userId)
}

func TestConsumeAuthCode_SingleUseOnly(t *testing.T) {
	setupOAuth2TestRedis(t)

	code, err := StoreAuthCode(7)
	require.NoError(t, err)

	_, err = ConsumeAuthCode(code)
	require.NoError(t, err)

	_, err = ConsumeAuthCode(code)
	require.ErrorIs(t, err, ErrOAuth2CodeInvalid, "a second consume of the same code must fail")
}

func TestConsumeAuthCode_UnknownCodeReturnsInvalid(t *testing.T) {
	setupOAuth2TestRedis(t)

	_, err := ConsumeAuthCode("does-not-exist")
	require.ErrorIs(t, err, ErrOAuth2CodeInvalid)
}

func TestIssueOAuth2Token_IsValidatableViaExistingLookupPath(t *testing.T) {
	setupOAuth2TestRedis(t)

	token, err := IssueOAuth2Token(99, "default", "openwebui")
	require.NoError(t, err)
	require.NotEmpty(t, token.Key)
	require.Equal(t, "oauth2-openwebui", token.Name)
	require.Equal(t, 99, token.UserId)
	require.True(t, token.UnlimitedQuota)
	require.Equal(t, common.TokenStatusEnabled, token.Status)

	// The whole point: GetTokenByKey (used by the existing TokenAuth()
	// middleware) must resolve this token with no DB involved.
	looked, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.Equal(t, 99, looked.UserId)
	require.Equal(t, "default", looked.Group)
}

func TestIssueOAuth2Token_ReissueInvalidatesPriorToken(t *testing.T) {
	setupOAuth2TestRedis(t)

	first, err := IssueOAuth2Token(5, "default", "openwebui")
	require.NoError(t, err)

	second, err := IssueOAuth2Token(5, "default", "openwebui")
	require.NoError(t, err)
	require.NotEqual(t, first.Key, second.Key)

	_, err = GetTokenByKey(first.Key, false)
	require.Error(t, err, "the first token must no longer resolve after re-issuance")

	looked, err := GetTokenByKey(second.Key, false)
	require.NoError(t, err)
	require.Equal(t, 5, looked.UserId)
}

func TestRevokeOAuth2Token_InvalidatesLiveToken(t *testing.T) {
	setupOAuth2TestRedis(t)

	token, err := IssueOAuth2Token(11, "default", "openwebui")
	require.NoError(t, err)

	require.NoError(t, RevokeOAuth2Token(11))

	_, err = GetTokenByKey(token.Key, false)
	require.Error(t, err, "token must not resolve after revoke")
}

func TestRevokeOAuth2Token_NoOpWhenNoneExists(t *testing.T) {
	setupOAuth2TestRedis(t)

	require.NoError(t, RevokeOAuth2Token(123456))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./model/... -run "TestStoreAndConsumeAuthCode|TestConsumeAuthCode|TestIssueOAuth2Token|TestRevokeOAuth2Token" -v`
Expected: FAIL — `StoreAuthCode`, `ConsumeAuthCode`, `ErrOAuth2CodeInvalid`, `IssueOAuth2Token`, `RevokeOAuth2Token` undefined (compile error).

- [ ] **Step 3: Implement `model/oauth2_session.go`**

```go
package model

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	oauth2CodePrefix         = "oauth2:code:"
	oauth2SessionTokenPrefix = "oauth2:session_token:"
	oauth2TokenTTL           = 24 * time.Hour
	oauth2CodeTTL            = 120 * time.Second
)

var ErrOAuth2CodeInvalid = errors.New("oauth2 code invalid or expired")

// StoreAuthCode mints a single-use, short-lived authorization code for
// userId and stores it in Redis. The code is opaque and URL-safe.
func StoreAuthCode(userId int) (string, error) {
	code, err := common.GenerateKey()
	if err != nil {
		return "", err
	}
	if err := common.RedisSet(oauth2CodePrefix+code, strconv.Itoa(userId), oauth2CodeTTL); err != nil {
		return "", err
	}
	return code, nil
}

// ConsumeAuthCode atomically reads and deletes the code, so a second call
// with the same code always fails — never a partial success.
func ConsumeAuthCode(code string) (int, error) {
	val, err := common.RedisGetDel(oauth2CodePrefix + code)
	if err != nil {
		return 0, ErrOAuth2CodeInvalid
	}
	userId, err := strconv.Atoi(val)
	if err != nil {
		return 0, ErrOAuth2CodeInvalid
	}
	return userId, nil
}

// IssueOAuth2Token mints a Token for userId scoped to group, caches it
// directly in Redis (the same format the existing token cache uses, so
// the unmodified TokenAuth()/GetTokenByKey() path resolves it), and never
// creates a DB row. Any token previously issued for this user via this
// path is invalidated first, so at most one stays live at a time.
func IssueOAuth2Token(userId int, group string, clientId string) (*Token, error) {
	_ = RevokeOAuth2Token(userId)

	key, err := common.GenerateKey()
	if err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	token := Token{
		Id:             -1, // never a real DB row; sentinel so it can't collide with a real Token.Id
		UserId:         userId,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		Name:           fmt.Sprintf("oauth2-%s", clientId),
		CreatedTime:    now,
		AccessedTime:   now,
		ExpiredTime:    now + int64(oauth2TokenTTL.Seconds()),
		UnlimitedQuota: true,
		Group:          group,
	}

	hmacKey := common.GenerateHMAC(token.Key)
	rawKey := token.Key
	cacheToken := token
	cacheToken.Clean() // zero out Key before storing, matching cacheSetToken's convention
	if err := common.RedisHSetObj(fmt.Sprintf("token:%s", hmacKey), &cacheToken, oauth2TokenTTL); err != nil {
		return nil, err
	}
	if err := common.RedisSet(oauth2SessionTokenPrefix+strconv.Itoa(userId), rawKey, oauth2TokenTTL); err != nil {
		return nil, err
	}

	return &token, nil
}

// RevokeOAuth2Token invalidates the user's currently-live OAuth2 token, if
// any. Not finding one is not an error.
func RevokeOAuth2Token(userId int) error {
	rawKey, err := common.RedisGet(oauth2SessionTokenPrefix + strconv.Itoa(userId))
	if err != nil {
		return nil
	}
	if err := cacheDeleteToken(rawKey); err != nil {
		return err
	}
	return common.RedisDel(oauth2SessionTokenPrefix + strconv.Itoa(userId))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./model/... -run "TestStoreAndConsumeAuthCode|TestConsumeAuthCode|TestIssueOAuth2Token|TestRevokeOAuth2Token" -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add model/oauth2_session.go model/oauth2_session_test.go
git commit -m "feat: add Redis-backed OAuth2 token issuance and auth-code helpers"
```

---

### Task 4: OAuth2 provider HTTP handlers

**Files:**
- Create: `controller/oauth2_provider.go`
- Create: `controller/oauth2_provider_test.go`

**Interfaces:**
- Consumes:
  - `model.StoreAuthCode(userId int) (string, error)`, `model.ConsumeAuthCode(code string) (int, error)`, `model.ErrOAuth2CodeInvalid`, `model.IssueOAuth2Token(userId int, group string, clientId string) (*Token, error)` (Task 3)
  - `model.GetUserCache(userId int) (*model.UserBase, error)` (existing, `model/user_cache.go:82`) — provides `.Group`, `.Email`, `.Username`
  - `model.IsAdmin(userId int) bool` (existing, `model/user.go:796`)
  - `model.ValidateUserToken(key string) (*model.Token, error)` (existing, `model/token.go:188`)
  - `system_setting.GetOAuth2Settings() *system_setting.OAuth2Settings` (Task 2)
  - `model.UpdateOption(key, value string) error` (existing, `model/option.go:1143`) — used to lazily persist a server-generated `client_secret` the first time it's needed
  - `common.GenerateKey() (string, error)`
- Produces:
  - `func OAuth2SessionInit(c *gin.Context)` — mounted behind `middleware.UserAuth()`; reads `userId := c.GetInt("id")` (set by `UserAuth()` on success, same convention as `controller/playground.go:38`)
  - `func OAuth2Token(c *gin.Context)` — public, form-encoded body
  - `func OAuth2UserInfo(c *gin.Context)` — public, `Authorization: Bearer` header

- [ ] **Step 1: Write the failing tests**

Create `controller/oauth2_provider_test.go`:

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupOAuth2ProviderTestDB gives model.GetUserCache/model.IsAdmin — both
// called by OAuth2Token and OAuth2UserInfo — a real user row to find. Every
// test below that exercises OAuth2Token or OAuth2UserInfo needs this, not
// just the userinfo ones, since OAuth2Token itself calls GetUserCache to
// resolve the issuing user's group.
func setupOAuth2ProviderTestDB(t *testing.T) {
	t.Helper()
	originalDB := model.DB
	originalUsingSQLite := common.UsingSQLite
	common.UsingSQLite = true

	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{
		Id:       7,
		Username: "alice",
		Email:    "alice@example.com",
		Role:     common.RoleCommonUser,
	}).Error)
	model.DB = db

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		common.UsingSQLite = originalUsingSQLite
	})
}

func setupOAuth2ProviderTest(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true

	originalOAuth2 := *system_setting.GetOAuth2Settings()
	*system_setting.GetOAuth2Settings() = system_setting.OAuth2Settings{
		Enabled:      true,
		ClientId:     "openwebui",
		ClientSecret: "test-secret",
		RedirectURI:  "https://chat.example.com/auth/newapi/callback",
	}

	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
		*system_setting.GetOAuth2Settings() = originalOAuth2
	})
}

func TestOAuth2SessionInit_ReturnsRedirectURLForAuthenticatedUser(t *testing.T) {
	setupOAuth2ProviderTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/session-init", nil)
	ctx.Set("id", 42)

	OAuth2SessionInit(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "https://chat.example.com/auth/newapi/callback?code=")
}

func TestOAuth2SessionInit_DisabledFeatureReturns404(t *testing.T) {
	setupOAuth2ProviderTest(t)
	system_setting.GetOAuth2Settings().Enabled = false

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/session-init", nil)
	ctx.Set("id", 42)

	OAuth2SessionInit(ctx)

	require.Equal(t, http.StatusNotFound, recorder.Code)
}

func postForm(t *testing.T, handler gin.HandlerFunc, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	ctx.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler(ctx)
	return recorder
}

func TestOAuth2Token_ValidCodeReturnsAccessToken(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	recorder := postForm(t, OAuth2Token, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"test-secret"},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"token_type":"Bearer"`)
	require.Contains(t, recorder.Body.String(), `"expires_in":86400`)
}

func TestOAuth2Token_WrongSecretReturnsInvalidClient(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	recorder := postForm(t, OAuth2Token, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"wrong-secret"},
	})

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_client")
}

func TestOAuth2Token_ReusedCodeReturnsInvalidGrant(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	code, err := model.StoreAuthCode(7)
	require.NoError(t, err)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"openwebui"},
		"client_secret": {"test-secret"},
	}
	first := postForm(t, OAuth2Token, form)
	require.Equal(t, http.StatusOK, first.Code)

	second := postForm(t, OAuth2Token, form)
	require.Equal(t, http.StatusBadRequest, second.Code)
	require.Contains(t, second.Body.String(), "invalid_grant")
}

func TestOAuth2UserInfo_ValidTokenReturnsIdentity(t *testing.T) {
	setupOAuth2ProviderTest(t)
	setupOAuth2ProviderTestDB(t)
	token, err := model.IssueOAuth2Token(7, "default", "openwebui")
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	ctx.Request.Header.Set("Authorization", "Bearer "+token.Key)

	OAuth2UserInfo(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"sub":"`+strconv.Itoa(7)+`"`)
}

func TestOAuth2UserInfo_InvalidTokenReturns401(t *testing.T) {
	setupOAuth2ProviderTest(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	ctx.Request.Header.Set("Authorization", "Bearer sk-does-not-exist")

	OAuth2UserInfo(ctx)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
```

`setupOAuth2ProviderTestDB` (defined above alongside `setupOAuth2ProviderTest`)
gives `model.GetUserCache`/`model.IsAdmin` — called by both `OAuth2Token` and
`OAuth2UserInfo` — a real `model.User` row to resolve, following the same
in-memory-SQLite pattern as `controller/organization_test.go:23-85`. It's
already wired into every test above that reaches those calls.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./controller/... -run TestOAuth2 -v`
Expected: FAIL — `OAuth2SessionInit`, `OAuth2Token`, `OAuth2UserInfo` undefined (compile error).

- [ ] **Step 3: Implement `controller/oauth2_provider.go`**

```go
package controller

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// OAuth2SessionInit is called from new-api's own frontend (same-origin,
// authenticated via the normal session cookie through middleware.UserAuth()).
// It mints a short-lived authorization code and returns a ready-to-navigate
// URL pointing at the external app's callback. There is no browser-facing
// /oauth2/authorize redirect in this design — see the design spec's
// "Revision note" for why.
func OAuth2SessionInit(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	userId := c.GetInt("id")
	code, err := model.StoreAuthCode(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization code"})
		return
	}

	state, err := common.GenerateKey()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
		return
	}

	redirectURL := settings.RedirectURI + "?code=" + code + "&state=" + state
	c.JSON(http.StatusOK, gin.H{"redirect_url": redirectURL})
}

// OAuth2Token exchanges a single-use authorization code for an access
// token. Called server-to-server by the external app's backend.
func OAuth2Token(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	if c.PostForm("grant_type") != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}

	clientId := c.PostForm("client_id")
	clientSecret := c.PostForm("client_secret")
	if clientId != settings.ClientId ||
		subtle.ConstantTimeCompare([]byte(clientSecret), []byte(settings.ClientSecret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_client"})
		return
	}

	code := c.PostForm("code")
	userId, err := model.ConsumeAuthCode(code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}

	userCache, err := model.GetUserCache(userId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	token, err := model.IssueOAuth2Token(userId, userCache.Group, clientId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": "sk-" + token.Key,
		"token_type":   "Bearer",
		"expires_in":   86400,
	})
}

// OAuth2UserInfo resolves an access token issued by OAuth2Token back to
// the underlying new-api user's identity. Called server-to-server.
func OAuth2UserInfo(c *gin.Context) {
	settings := system_setting.GetOAuth2Settings()
	if !settings.Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "oauth2 is not enabled"})
		return
	}

	bearer := c.Request.Header.Get("Authorization")
	key := strings.TrimPrefix(bearer, "Bearer ")
	key = strings.TrimPrefix(key, "sk-")

	token, err := model.ValidateUserToken(key)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	userCache, err := model.GetUserCache(token.UserId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sub":      strconv.Itoa(token.UserId),
		"email":    userCache.Email,
		"name":     userCache.Username,
		"is_admin": model.IsAdmin(token.UserId),
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./controller/... -run TestOAuth2 -v`
Expected: PASS (all subtests).

- [ ] **Step 5: Commit**

```bash
git add controller/oauth2_provider.go controller/oauth2_provider_test.go
git commit -m "feat: add OAuth2 provider HTTP handlers"
```

---

### Task 5: Router wiring

**Files:**
- Create: `router/oauth2-router.go`
- Modify: `router/main.go:15-19` (add the call)
- Create: `router/oauth2-router_test.go`

**Interfaces:**
- Consumes: `controller.OAuth2SessionInit`, `controller.OAuth2Token`, `controller.OAuth2UserInfo` (Task 4); `middleware.UserAuth()` (existing, `middleware/auth.go:170`)
- Produces: `func SetOAuth2Router(router *gin.Engine)`

- [ ] **Step 1: Write the failing test**

Create `router/oauth2-router_test.go`:

```go
package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetOAuth2Router_RegistersExpectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetOAuth2Router(engine)

	paths := make(map[string]string)
	for _, r := range engine.Routes() {
		paths[r.Method+" "+r.Path] = r.Handler
	}

	require.Contains(t, paths, "POST /oauth2/session-init")
	require.Contains(t, paths, "POST /oauth2/token")
	require.Contains(t, paths, "GET /oauth2/userinfo")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./router/... -run TestSetOAuth2Router -v`
Expected: FAIL — `SetOAuth2Router` undefined (compile error).

- [ ] **Step 3: Implement `router/oauth2-router.go`**

```go
package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetOAuth2Router(router *gin.Engine) {
	oauth2Router := router.Group("/oauth2")
	{
		sessionInitRouter := oauth2Router.Group("")
		sessionInitRouter.Use(middleware.UserAuth())
		sessionInitRouter.POST("/session-init", controller.OAuth2SessionInit)

		oauth2Router.POST("/token", controller.OAuth2Token)
		oauth2Router.GET("/userinfo", controller.OAuth2UserInfo)
	}
}
```

- [ ] **Step 4: Wire it into `router/main.go`**

In `router/main.go`, change:
```go
func SetRouter(router *gin.Engine, assets ThemeAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
```
to:
```go
func SetRouter(router *gin.Engine, assets ThemeAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	SetOAuth2Router(router)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./router/... -run TestSetOAuth2Router -v`
Expected: PASS.

- [ ] **Step 6: Full build sanity check**

Run: `go build ./...`
Expected: builds with no errors.

- [ ] **Step 7: Commit**

```bash
git add router/oauth2-router.go router/oauth2-router_test.go router/main.go
git commit -m "feat: wire up OAuth2 provider routes"
```

---

### Task 6: Revoke OAuth2 token on logout

**Files:**
- Modify: `controller/user.go:126-141` (`Logout` function)
- Create: `controller/logout_oauth2_test.go`

**Interfaces:**
- Consumes: `model.RevokeOAuth2Token(userId int) error` (Task 3)

- [ ] **Step 1: Write the failing test**

Create `controller/logout_oauth2_test.go`:

```go
package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestLogout_RevokesOAuth2Token(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	originalRDB := common.RDB
	originalEnabled := common.RedisEnabled
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.RedisEnabled = true
	t.Cleanup(func() {
		common.RDB = originalRDB
		common.RedisEnabled = originalEnabled
	})

	token, err := model.IssueOAuth2Token(55, "default", "openwebui")
	require.NoError(t, err)

	engine := gin.New()
	store := cookie.NewStore([]byte("test-secret"))
	engine.Use(sessions.Sessions("session", store))
	engine.GET("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", 55)
		require.NoError(t, session.Save())
		Logout(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)

	_, err = model.GetTokenByKey(token.Key, false)
	require.Error(t, err, "OAuth2 token must be invalidated after logout")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./controller/... -run TestLogout_RevokesOAuth2Token -v`
Expected: FAIL — the token is still valid after logout (assertion failure, not a compile error).

- [ ] **Step 3: Update `Logout` in `controller/user.go`**

Change (lines 126-141):
```go
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
	})
}
```
to:
```go
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	if userId, ok := session.Get("id").(int); ok {
		_ = model.RevokeOAuth2Token(userId)
	}
	session.Clear()
	err := session.Save()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "",
		"success": true,
	})
}
```
(`model` is already imported in this file — see `controller/user.go:18`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./controller/... -run TestLogout_RevokesOAuth2Token -v`
Expected: PASS.

- [ ] **Step 5: Run the full controller test suite to check for regressions**

Run: `go test ./controller/... -v 2>&1 | tail -40`
Expected: no new failures beyond any pre-existing ones (see note in project history: `TestListModelsTokenLimitIncludesTieredBillingModel` was already failing before this work, unrelated to this change).

- [ ] **Step 6: Commit**

```bash
git add controller/user.go controller/logout_oauth2_test.go
git commit -m "feat: revoke OAuth2 token on logout"
```

---

### Task 7: Frontend "Open in Chat App" entry point

**Files:**
- Modify: `web/default/src/features/playground/constants.ts` (add endpoint)
- Modify: `web/default/src/features/playground/api.ts` (add API call)
- Modify: `web/default/src/features/playground/index.tsx` (add button)

**Interfaces:**
- Consumes: `api` (shared axios instance with automatic `New-Api-User` header + session cookie, `web/default/src/lib/api.ts:42`)
- Produces: `openInChatApp(): Promise<void>` in `features/playground/api.ts`, wired to a button in the playground header.

This task has no backend-facing test — it's verified via the Browser preview tooling per the project's UI-verification convention, not a Go/Jest unit test. No placeholder: exact code below.

- [ ] **Step 1: Add the endpoint constant**

In `web/default/src/features/playground/constants.ts`, change:
```ts
export const API_ENDPOINTS = {
  CHAT_COMPLETIONS: '/pg/chat/completions',
  USER_MODELS: '/api/user/models',
  USER_GROUPS: '/api/user/self/groups',
} as const
```
to:
```ts
export const API_ENDPOINTS = {
  CHAT_COMPLETIONS: '/pg/chat/completions',
  USER_MODELS: '/api/user/models',
  USER_GROUPS: '/api/user/self/groups',
  OAUTH2_SESSION_INIT: '/oauth2/session-init',
} as const
```

- [ ] **Step 2: Add the API call**

In `web/default/src/features/playground/api.ts`, add after `getUserGroups`:

```ts
/**
 * Mint a one-time OAuth2 authorization code and get the external chat
 * app's callback URL. The caller must navigate to the returned URL
 * immediately (window.location.href) and must never render it as a
 * static, copyable <a href> — the code is single-use and short-lived
 * (120s), and a shared/unfurled link would burn it before the real user
 * clicks it.
 */
export async function getOpenInChatAppUrl(): Promise<string> {
  const res = await api.post(API_ENDPOINTS.OAUTH2_SESSION_INIT)
  const { data } = res
  if (!data.redirect_url) {
    throw new Error('oauth2 is not configured')
  }
  return data.redirect_url as string
}
```

- [ ] **Step 3: Add the button**

In `web/default/src/features/playground/index.tsx`, add to the imports:
```ts
import { getUserModels, getUserGroups, getOpenInChatAppUrl } from './api'
import { Button } from '@/components/ui/button'
import { ExternalLinkIcon } from 'lucide-react'
```

Add this handler inside the `Playground()` component, alongside the other `useCallback` handlers:
```ts
const handleOpenInChatApp = useCallback(async () => {
  try {
    const redirectUrl = await getOpenInChatAppUrl()
    window.location.href = redirectUrl
  } catch {
    toast.error(t('External chat app is not configured'))
  }
}, [t])
```

In the JSX returned by `Playground()`, add a button just inside the outer wrapper (before the existing `<div className='flex flex-1 flex-col overflow-hidden'>` at line 194):
```tsx
<div className='flex items-center justify-end px-4 pt-2'>
  <Button variant='ghost' size='sm' onClick={handleOpenInChatApp}>
    <ExternalLinkIcon className='mr-1 size-4' />
    {t('Open in Chat App')}
  </Button>
</div>
```

- [ ] **Step 4: Verify in the browser**

Start the dev servers (`make dev` or the project's existing preview workflow), open the playground page while logged in, click "Open in Chat App", and confirm:
- A `POST /oauth2/session-init` request fires (check Network tab) and returns `200` with a `redirect_url`.
- The browser navigates to that URL (it will 404/fail to connect since no real external app is deployed yet in this environment — that's expected; confirm the URL itself is well-formed: `<configured redirect_uri>?code=...&state=...`).
- Right-clicking the button does not offer "Copy Link" (confirms it's not a static `<a href>`).

- [ ] **Step 5: Commit**

```bash
git add web/default/src/features/playground/constants.ts web/default/src/features/playground/api.ts web/default/src/features/playground/index.tsx
git commit -m "feat: add Open in Chat App entry point to playground"
```

---

## Post-implementation checklist (not a task — a reminder for whoever deploys this)

- [ ] Set `oauth2.enabled=true`, `oauth2.client_id`, `oauth2.client_secret`, `oauth2.redirect_uri` via the existing option-update admin flow before go-live (see Task 2).
- [ ] Confirm Redis is enabled in the target deployment (`common.RedisEnabled = true`) — this feature does not work without it, by design.
- [ ] Share the finalized `client_id`/`client_secret`/`redirect_uri` values with the OpenWebUI-side team per `docs/superpowers/specs/2026-08-16-oauth2-openwebui-integration-handoff.md`.
