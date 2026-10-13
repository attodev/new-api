package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openUsersSchemaTestDB opens an isolated in-memory SQLite database whose
// users table is created from raw DDL, so the declared column types (which
// SQLite reports verbatim through Migrator().ColumnTypes) can simulate a
// legacy 32-bit MySQL/PostgreSQL schema.
func openUsersSchemaTestDB(t *testing.T, quotaType string) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.Exec(fmt.Sprintf(
		"CREATE TABLE users (id integer PRIMARY KEY, quota %s, used_quota bigint, aff_quota bigint, aff_history bigint)",
		quotaType,
	)).Error)
	return db
}

func TestEnsureUserQuotaColumnsSkipsSQLite(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeSQLite))
}

func TestEnsureUserQuotaColumnsRejectsLegacy32BitColumns(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	for _, dbType := range []string{common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL} {
		err := ensureUserQuotaColumns(db, dbType)
		require.Error(t, err, dbType)
		require.ErrorContains(t, err, "users.quota")
		require.ErrorContains(t, err, "SKIP_64BIT_QUOTA_SCHEMA_CHECK")
	}
}

func TestEnsureUserQuotaColumnsAccepts64BitColumns(t *testing.T) {
	db := openUsersSchemaTestDB(t, "bigint")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestEnsureUserQuotaColumnsAcceptsModelSchema(t *testing.T) {
	// The shared test DB is migrated from the User model, whose quota columns
	// carry the fork's `type:bigint` tags.
	require.NoError(t, ensureUserQuotaColumns(DB, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(DB, common.DatabaseTypePostgreSQL))
}

func TestEnsureUserQuotaColumnsSkipsMissingTable(t *testing.T) {
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
}

func TestEnsureUserQuotaColumnsEnvOverride(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "true")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestIs64BitIntegerType(t *testing.T) {
	cases := []struct {
		dbType   string
		dataType string
		want     bool
	}{
		{common.DatabaseTypeMySQL, "BIGINT", true},
		{common.DatabaseTypeMySQL, "UNSIGNED BIGINT", true},
		{common.DatabaseTypeMySQL, "bigint unsigned", true},
		{common.DatabaseTypeMySQL, "INT", false},
		{common.DatabaseTypeMySQL, "INTEGER", false},
		{common.DatabaseTypePostgreSQL, "INT8", true},
		{common.DatabaseTypePostgreSQL, "bigint", true},
		{common.DatabaseTypePostgreSQL, "INT4", false},
		{common.DatabaseTypePostgreSQL, "integer", false},
		{common.DatabaseTypeSQLite, "bigint", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, is64BitIntegerType(tc.dbType, tc.dataType), "%s %s", tc.dbType, tc.dataType)
	}
}
