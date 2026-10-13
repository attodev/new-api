package relay

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTaskSubmitStatusAccepted pins the submit-side contract: any 2xx upstream
// status is a successful submission (task APIs may answer 201 Created or 202
// Accepted); everything else must still fail with fail_to_fetch_task.
func TestTaskSubmitStatusAccepted(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{http.StatusOK, true},
		{http.StatusCreated, true},
		{http.StatusAccepted, true},
		{http.StatusNoContent, true},
		{199, false},
		{http.StatusMultipleChoices, false},
		{http.StatusBadRequest, false},
		{http.StatusBadGateway, false},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, taskSubmitStatusAccepted(tc.code), "status %d", tc.code)
	}
}
