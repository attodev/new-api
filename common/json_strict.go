package common

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// fork: O3 — strict JSON decoding for organization request bodies: unknown
// fields are rejected with "unsupported field: <name>".
func DecodeJsonStrict(reader io.Reader, v any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		const unknownFieldPrefix = "json: unknown field "
		if strings.HasPrefix(err.Error(), unknownFieldPrefix) {
			field := strings.Trim(strings.TrimPrefix(err.Error(), unknownFieldPrefix), `"`)
			return fmt.Errorf("unsupported field: %s", field)
		}
		return err
	}
	return nil
}
