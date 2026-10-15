package openai

import "encoding/json"

// fork: O11 — an explicit "error": null on an image stream event is not an
// error (team b5d733722). len(raw) alone treats the 4-byte literal as one.
func rawJSONPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}
