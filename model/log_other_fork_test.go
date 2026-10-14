package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserLogsHideModelRouting(t *testing.T) {
	const other = `{"is_model_mapped":true,"upstream_model_name":"openai/gpt-x","frt":12}`
	user := formatLogOtherJSON(other, logOtherVisibilityUser)
	require.NotContains(t, user, "upstream_model_name")
	require.NotContains(t, user, "is_model_mapped")
	require.Contains(t, user, `"frt":12`)
	admin := formatLogOtherJSON(other, logOtherVisibilityAdmin)
	require.Contains(t, admin, `"upstream_model_name":"openai/gpt-x"`)
}
