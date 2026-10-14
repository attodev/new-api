package model

import "encoding/json"

// fork: O8 — routing details hidden from end users (admins still see them).
var forkUserHiddenLogOtherKeys = []string{"is_model_mapped", "upstream_model_name"}

func deleteLogOtherKeys(values map[string]json.RawMessage, keys []string) bool {
	changed := false
	for _, key := range keys {
		if _, exists := values[key]; exists {
			delete(values, key)
			changed = true
		}
	}
	return changed
}
