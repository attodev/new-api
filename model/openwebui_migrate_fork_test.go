package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrateForkOpenWebUITablesCreatesLinkTable(t *testing.T) {
	setupOpenWebUIModelTestDB(t)
	require.NoError(t, DB.Migrator().DropTable(&OpenWebUILink{}))
	require.False(t, DB.Migrator().HasTable(&OpenWebUILink{}))

	require.NoError(t, MigrateForkOpenWebUITables(DB))
	require.True(t, DB.Migrator().HasTable("openwebui_user_links"))
	// Idempotent: migrateDB runs on every start.
	require.NoError(t, MigrateForkOpenWebUITables(DB))
}
