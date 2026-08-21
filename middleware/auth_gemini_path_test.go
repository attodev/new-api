package middleware

import "testing"

// TestIsGeminiQueryKeyPath_MatchesBareModelsListing reproduces a real bug:
// the Gemini-style "GET /v1/models" (no trailing model id) listing route
// was not recognized as a query-key-authenticatable path, so a client
// authenticating via "?key=" against that exact route never got its key
// promoted to an Authorization header.
func TestIsGeminiQueryKeyPath_MatchesBareModelsListing(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/v1/models", true},
		{"/v1/models/gemini-3-flash-preview", true},
		{"/v1beta/models", true},
		{"/v1beta/models/gemini-3-flash-preview", true},
		{"/v1beta/openai/models", true},
		{"/v1/chat/completions", false},
	}
	for _, tc := range cases {
		if got := isGeminiQueryKeyPath(tc.path); got != tc.want {
			t.Errorf("isGeminiQueryKeyPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
