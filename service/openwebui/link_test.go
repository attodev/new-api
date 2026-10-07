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
