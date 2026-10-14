package model

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// fork: wallet columns must stay bigint (docs/fork-overlays.md, spec §5).
func TestUserQuotaColumnsDeclareBigint(t *testing.T) {
	userType := reflect.TypeOf(User{})
	for _, name := range []string{"Quota", "UsedQuota", "AffQuota", "AffHistoryQuota"} {
		field, ok := userType.FieldByName(name)
		require.True(t, ok, name)
		require.Contains(t, field.Tag.Get("gorm"), "type:bigint",
			"User.%s must declare type:bigint so AutoMigrate never narrows the wallet column", name)
	}
}

func TestUserQuotaColumnsSurviveAutoMigrate(t *testing.T) {
	cases := []struct {
		dialect string
		env     string
		dbType  common.DatabaseType
	}{
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	}
	for _, tc := range cases {
		t.Run(tc.dialect, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			t.Setenv("QUOTA_WIDTH_TEST_DSN", dsn)
			db, _, err := chooseDB("QUOTA_WIDTH_TEST_DSN", false)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			const table = "users_quota_width_test"
			t.Cleanup(func() { _ = db.Migrator().DropTable(table) })
			require.NoError(t, db.Table(table).AutoMigrate(&User{}))
			// A second pass is what production startup does; it must not narrow.
			require.NoError(t, db.Table(table).AutoMigrate(&User{}))

			columns, err := db.Table(table).Migrator().ColumnTypes(&User{})
			require.NoError(t, err)
			seen := 0
			for _, column := range columns {
				for _, expected := range userQuotaColumns {
					if strings.EqualFold(column.Name(), expected) {
						seen++
						require.True(t, is64BitIntegerType(tc.dbType, column.DatabaseTypeName()),
							"%s is %s", expected, column.DatabaseTypeName())
					}
				}
			}
			require.Equal(t, len(userQuotaColumns), seen)

			big := &User{Username: "quota_width_" + tc.dialect, Quota: 5_000_000_000, Status: common.UserStatusEnabled}
			require.NoError(t, db.Table(table).Create(big).Error)
			var got User
			require.NoError(t, db.Table(table).Where("id = ?", big.Id).First(&got).Error)
			require.Equal(t, 5_000_000_000, got.Quota)
		})
	}
}
