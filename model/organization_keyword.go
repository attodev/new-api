package model

import "strings"

// fork: O3 — organization search keywords are literals. The escape character
// is "!" (as in sanitizeLikePattern, model/token.go) rather than "\", which
// MySQL also treats as a string escape. Callers wrap the result in "%...%" and
// add ESCAPE '!' to the LIKE clause.
var organizationKeywordEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// SanitizeOrganizationKeyword escapes "!", "%" and "_" so that a keyword
// matches only itself inside a LIKE pattern.
func SanitizeOrganizationKeyword(keyword string) string {
	return organizationKeywordEscaper.Replace(keyword)
}
