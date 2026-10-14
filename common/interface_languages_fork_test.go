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
	require.Equal(t, []string{"zhCN", "zhTW", "ja"}, ParseInterfaceLanguages(" zhcn ,ZHTW,,ja,zhCN"))
}

func TestInterfaceLanguagesDefault(t *testing.T) {
	require.Equal(t, "en,ko", InterfaceLanguages)
}
