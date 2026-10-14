package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeOrganizationKeyword(t *testing.T) {
	for input, want := range map[string]string{
		"alice": "alice",
		"50%_x": "50!%!_x",
		"a!b":   "a!!b",
		"!%_":   "!!!%!_",
		"":      "",
		"한글_이름": "한글!_이름",
	} {
		require.Equal(t, want, SanitizeOrganizationKeyword(input), input)
	}
}
