package model

import "github.com/QuantumNous/new-api/common"

// normalizeForkOptionValue rewrites the value of a fork option into its stored
// form before UpdateOption writes it to the database and the option map.
// fork: O8 — InterfaceLanguages keeps only the known codes (en, ko).
func normalizeForkOptionValue(key, value string) string {
	if key == "InterfaceLanguages" {
		return common.NormalizeInterfaceLanguagesOption(value)
	}
	return value
}
