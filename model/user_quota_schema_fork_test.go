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

// Ported from team model/user_quota_schema_test.go (3f1cbeeb7).
//
// openUsersSchemaTestDB opens an isolated in-memory SQLite database whose
// users table is created from raw DDL. SQLite reports the declared column
// type verbatim through Migrator().ColumnTypes, which lets the test simulate
// a legacy 32-bit MySQL/PostgreSQL schema.
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
	if quotaType != "" {
		require.NoError(t, db.Exec(fmt.Sprintf(
			"CREATE TABLE users (id integer PRIMARY KEY, quota %s, used_quota bigint, aff_quota bigint, aff_history bigint)",
			quotaType,
		)).Error)
	}
	return db
}

func TestEnsureUserQuotaColumnsSkipsSQLite(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeSQLite))
}

func TestEnsureUserQuotaColumnsRejectsLegacy32BitColumns(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	for _, dbType := range []common.DatabaseType{common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL} {
		err := ensureUserQuotaColumns(db, dbType)
		require.Error(t, err, dbType)
		require.ErrorContains(t, err, "users.quota", dbType)
	}
}

func TestEnsureUserQuotaColumnsAccepts64BitColumns(t *testing.T) {
	db := openUsersSchemaTestDB(t, "bigint")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestEnsureUserQuotaColumnsAcceptsModelSchema(t *testing.T) {
	// The User model declares its wallet columns type:bigint.
	db := setupSQLiteModelTestDB(t)
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestEnsureUserQuotaColumnsSkipsMissingTable(t *testing.T) {
	db := openUsersSchemaTestDB(t, "")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestEnsureUserQuotaColumnsEnvOverride(t *testing.T) {
	db := openUsersSchemaTestDB(t, "integer")
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "true")
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypeMySQL))
	require.NoError(t, ensureUserQuotaColumns(db, common.DatabaseTypePostgreSQL))
}

func TestIs64BitIntegerType(t *testing.T) {
	cases := []struct {
		dbType   common.DatabaseType
		dataType string
		want     bool
	}{
		{common.DatabaseTypeMySQL, "BIGINT", true},
		{common.DatabaseTypeMySQL, "bigint", true},
		{common.DatabaseTypeMySQL, "UNSIGNED BIGINT", true},
		{common.DatabaseTypeMySQL, "bigint unsigned", true},
		{common.DatabaseTypeMySQL, "INT", false},
		{common.DatabaseTypeMySQL, "int", false},
		{common.DatabaseTypeMySQL, "INTEGER", false},
		{common.DatabaseTypePostgreSQL, "INT8", true},
		{common.DatabaseTypePostgreSQL, "int8", true},
		{common.DatabaseTypePostgreSQL, "bigint", true},
		{common.DatabaseTypePostgreSQL, "INT4", false},
		{common.DatabaseTypePostgreSQL, "int4", false},
		{common.DatabaseTypePostgreSQL, "integer", false},
		{common.DatabaseTypeSQLite, "bigint", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, is64BitIntegerType(tc.dbType, tc.dataType), "%s %s", tc.dbType, tc.dataType)
	}
}
