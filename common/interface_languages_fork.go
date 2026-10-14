package common

import "strings"

// fork: O8 — the InterfaceLanguages option limits which languages the
// frontend language menu offers. Every locale stays loaded; the frontend
// always keeps a user's current language selectable.

// interfaceLanguageCodes maps lowercase input to the frontend interface
// language codes. The fork keeps only English and Korean (team 66d421d13);
// "kr" is the legacy team code.
var interfaceLanguageCodes = map[string]string{
	"en": "en",
	"ko": "ko",
	"kr": "ko",
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

// NormalizeInterfaceLanguagesOption is the stored form of the option value:
// the parsed codes joined by commas, "en" when nothing known is left.
func NormalizeInterfaceLanguagesOption(value string) string {
	return strings.Join(ParseInterfaceLanguages(value), ",")
}
