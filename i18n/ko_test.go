package i18n

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKoreanIsSupported(t *testing.T) {
	require.Equal(t, LangKo, normalizeLang("ko"))
	require.Equal(t, LangKo, normalizeLang("ko-KR"))
	require.Equal(t, LangKo, normalizeLang("ko_KR"))
	require.Equal(t, LangKo, normalizeLang("kr")) // legacy team value
	require.Equal(t, LangEn, normalizeLang("kok"))
	require.Equal(t, LangEn, normalizeLang("kos"))
	require.True(t, IsSupported("ko"))
	require.Contains(t, SupportedLanguages(), LangKo)
	require.Equal(t, LangKo, ParseAcceptLanguage("ko-KR,ko;q=0.9,en;q=0.8"))
}

func TestKoreanYamlCoversEveryEnglishKey(t *testing.T) {
	require.NoError(t, Init())
	en, err := localeFS.ReadFile("locales/en.yaml")
	require.NoError(t, err)
	ko, err := localeFS.ReadFile("locales/ko.yaml")
	require.NoError(t, err)
	for _, key := range yamlKeys(en) {
		require.Contains(t, yamlKeys(ko), key, "ko.yaml lacks %s", key)
	}
	require.NotEqual(t, Translate(LangKo, "common.database_error"), Translate(LangEn, "common.database_error"))
}

var yamlKeyPattern = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+):`)

// yamlKeys lists the top-level keys of a flat locale file.
func yamlKeys(data []byte) []string {
	var keys []string
	for _, match := range yamlKeyPattern.FindAllSubmatch(data, -1) {
		keys = append(keys, string(match[1]))
	}
	return keys
}
