package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// fork: O8 — interface language menu restriction.
func TestParseInterfaceLanguages(t *testing.T) {
	require.Equal(t, []string{"en", "ko"}, ParseInterfaceLanguages("en, ko"))
	require.Equal(t, []string{"ko", "en"}, ParseInterfaceLanguages("kr,EN,unknown,ko"))
	require.Equal(t, []string{"en"}, ParseInterfaceLanguages(""))
	require.Equal(t, []string{"en"}, ParseInterfaceLanguages("xx"))
	require.Equal(t, []string{"en"}, ParseInterfaceLanguages("zhCN,ja"))
	require.Equal(t, []string{"ko"}, ParseInterfaceLanguages(" zhcn ,ZHTW,,ja,fr,ru,vi,ko"))
}

// The option is stored in the parsed form: known codes in the admin's order,
// English when nothing is left.
func TestNormalizeInterfaceLanguagesOption(t *testing.T) {
	require.Equal(t, "ko,en", NormalizeInterfaceLanguagesOption(" kr , EN, xx "))
	require.Equal(t, "en", NormalizeInterfaceLanguagesOption("xx"))
	require.Equal(t, "en", NormalizeInterfaceLanguagesOption(""))
	require.Equal(t, "en,ko", NormalizeInterfaceLanguagesOption("en,zhCN,ko,ja"))
}

func TestInterfaceLanguagesDefault(t *testing.T) {
	require.Equal(t, "en,ko", InterfaceLanguages)
}
