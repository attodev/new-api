package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Every row error of the import keeps the English line it always had. Each case
// pairs the template and params of one addRowError call with the text the
// former fmt.Sprintf call produced.
func TestAddRowErrorKeepsTheEnglishErrorsText(t *testing.T) {
	nested := errors.New("boom")
	cases := []struct {
		name     string
		line     int
		username string
		key      string
		params   map[string]any
		want     string
	}{
		{"empty username", 2, "", "username is empty", nil,
			"line 2: username is empty"},
		{"invalid role", 3, "u", "invalid organization_role {{role}} (only member or admin can be imported)",
			map[string]any{"role": fmt.Sprintf("%q", "x")},
			`line 3 (u): invalid organization_role "x" (only member or admin can be imported)`},
		{"db error", 4, "u", "db error: {{error}}", rowErrorParams(nested, nil),
			"line 4 (u): db error: boom"},
		{"own row", 5, "u", "an import cannot change the importing admin's own row", nil,
			"line 5 (u): an import cannot change the importing admin's own row"},
		{"platform admin", 6, "u", "platform administrators cannot be managed by an import", nil,
			"line 6 (u): platform administrators cannot be managed by an import"},
		{"owner", 7, "u", "the organization owner cannot be changed by an import", nil,
			"line 7 (u): the organization owner cannot be changed by an import"},
		{"quota with error", 8, "u", "invalid quota {{quota}}: {{error}}",
			rowErrorParams(nested, map[string]any{"quota": fmt.Sprintf("%q", "9")}),
			`line 8 (u): invalid quota "9": boom`},
		{"bare error", 9, "u", "{{error}}", rowErrorParams(nested, nil),
			"line 9 (u): boom"},
		{"failed to update", 10, "u", "failed to update: {{error}}", rowErrorParams(nested, nil),
			"line 10 (u): failed to update: boom"},
		{"failed to set quota", 11, "u", "failed to set quota: {{error}}", rowErrorParams(nested, nil),
			"line 11 (u): failed to set quota: boom"},
		{"other organization", 12, "u", "belongs to another organization", nil,
			"line 12 (u): belongs to another organization"},
		{"member fields", 13, "u", "failed to set member status or note: {{error}}", rowErrorParams(nested, nil),
			"line 13 (u): failed to set member status or note: boom"},
		{"assign", 14, "u", "failed to assign: {{error}}", rowErrorParams(nested, nil),
			"line 14 (u): failed to assign: boom"},
		{"password", 15, "u", "initial_password is required for new users", nil,
			"line 15 (u): initial_password is required for new users"},
		{"quota without error", 16, "u", "invalid quota {{quota}}", map[string]any{"quota": fmt.Sprintf("%q", "x")},
			`line 16 (u): invalid quota "x"`},
		{"invalid user", 17, "u", "invalid user: {{error}}", rowErrorParams(nested, nil),
			"line 17 (u): invalid user: boom"},
		{"remove-absent query", 0, "", "failed to query members: {{error}}", rowErrorParams(nested, nil),
			"remove-absent: failed to query members: boom"},
		{"remove-absent remove", 0, "m", "failed to remove: {{error}}", rowErrorParams(nested, nil),
			"remove-absent (m): failed to remove: boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := &OrgImportResult{Errors: []string{}}
			result.addRowError(c.line, c.username, c.key, c.params)
			require.Equal(t, []string{c.want}, result.Errors)
			require.Equal(t, []OrgImportRowError{{Line: c.line, Username: c.username, MessageKey: c.key, Params: c.params}}, result.ErrorDetails)
		})
	}
}

func TestRowErrorParamsKeepsAMessageKeyAndParams(t *testing.T) {
	message := common.NewMessage("limit is {{limit}}", map[string]any{"limit": 5})
	params := rowErrorParams(message, map[string]any{"quota": `"1"`})
	require.Equal(t, map[string]any{
		"quota":        `"1"`,
		"error":        "limit is 5",
		"error_key":    "limit is {{limit}}",
		"error_params": map[string]any{"limit": 5},
	}, params)

	err := fmt.Errorf("wrapped: %w", message)
	wrapped := rowErrorParams(err, nil)
	require.Equal(t, map[string]any{"error": "wrapped: limit is 5"}, wrapped)

	plain := rowErrorParams(errors.New("boom"), nil)
	require.Equal(t, map[string]any{"error": "boom"}, plain)
}
