package common

import "testing"

// TestValidateTaskDurationBounds reproduces a real billing-overflow bug:
// video task duration ("seconds"/"duration") is later used as a billing
// multiplier (OtherRatio "seconds"), and was never bounded at request
// validation - a huge or negative value could overflow the quota
// calculation into a negative charge.
func TestValidateTaskDurationBounds(t *testing.T) {
	cases := []struct {
		name      string
		req       TaskSubmitReq
		wantError bool
	}{
		{"typical duration is fine", TaskSubmitReq{Duration: 8}, false},
		{"zero duration is fine (caller applies a default)", TaskSubmitReq{}, false},
		{"at the cap is fine", TaskSubmitReq{Duration: MaxTaskDurationSeconds}, false},
		{"over the cap is rejected", TaskSubmitReq{Duration: MaxTaskDurationSeconds + 1}, true},
		{"negative duration is rejected", TaskSubmitReq{Duration: -1}, true},
		{"huge value via the seconds string field is rejected", TaskSubmitReq{Seconds: "999999999"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskErr := validateTaskDurationBounds(tc.req)
			if tc.wantError && taskErr == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tc.wantError && taskErr != nil {
				t.Fatalf("expected no error, got %v", taskErr)
			}
		})
	}
}
