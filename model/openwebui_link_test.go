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
	require.NoError(t, DB.Create(&User{Id: 1, Username: "alice", Password: "x", Email: "Alice@Example.com", AffCode: "aff-alice", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, DB.Create(&User{Id: 2, Username: "ghost", Password: "x", Email: "ghost@example.com", AffCode: "aff-ghost", Status: common.UserStatusEnabled}).Error)
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
