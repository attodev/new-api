package common

import "strings"

// fork: O8 — the InterfaceLanguages option limits which languages the
// frontend language menu offers. Every locale stays loaded; the frontend
// always keeps a user's current language selectable.

// interfaceLanguageCodes maps lowercase input to the frontend interface
// language codes (note the camelCase zh codes). "kr" is the legacy team code.
var interfaceLanguageCodes = map[string]string{
	"en":   "en",
	"ko":   "ko",
	"kr":   "ko",
	"zhcn": "zhCN",
	"zhtw": "zhTW",
	"fr":   "fr",
	"ru":   "ru",
	"ja":   "ja",
	"vi":   "vi",
}

// ParseInterfaceLanguages turns the comma-separated option value into the
// list of interface language codes, in the admin's order. Unknown codes and
// duplicates are dropped; an empty result falls back to English.
func ParseInterfaceLanguages(value string) []string {
	seen := make(map[string]bool)
	languages := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		code, ok := interfaceLanguageCodes[strings.ToLower(strings.TrimSpace(part))]
		if !ok || seen[code] {
			continue
		}
		seen[code] = true
		languages = append(languages, code)
	}
	if len(languages) == 0 {
		return []string{"en"}
	}
	return languages
}
