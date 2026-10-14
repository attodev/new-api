package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// fork: O8 — InterfaceLanguages is stored in its normalized form, in the
// database and in the option map alike.
func TestUpdateOptionNormalizesInterfaceLanguages(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previousMap := common.OptionMap
	previousLanguages := common.InterfaceLanguages
	t.Cleanup(func() {
		common.OptionMap = previousMap
		common.InterfaceLanguages = previousLanguages
	})
	common.OptionMap = map[string]string{}

	require.NoError(t, UpdateOption("InterfaceLanguages", " kr ,xx"))
	require.Equal(t, "ko", requireOptionValue(t, db, "InterfaceLanguages"))
	require.Equal(t, "ko", common.OptionMap["InterfaceLanguages"])
	require.Equal(t, "ko", common.InterfaceLanguages)

	require.NoError(t, UpdateOption("InterfaceLanguages", "zhCN,ja"))
	require.Equal(t, "en", requireOptionValue(t, db, "InterfaceLanguages"))
	require.Equal(t, "en", common.OptionMap["InterfaceLanguages"])
}
