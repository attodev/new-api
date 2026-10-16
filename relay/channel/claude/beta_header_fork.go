package claude

import "strings"

// joinAnthropicBetaValues merges every anthropic-beta header line into the
// single comma-separated list the API reads, trimming whitespace and dropping
// empty entries. An empty result means the header should not be sent.
func joinAnthropicBetaValues(values []string) string {
	var betas []string
	for _, value := range values {
		for _, beta := range strings.Split(value, ",") {
			if beta = strings.TrimSpace(beta); beta != "" {
				betas = append(betas, beta)
			}
		}
	}
	return strings.Join(betas, ",")
}
