package claude

import "github.com/QuantumNous/new-api/relaykit/dto"

// fork: O11 — Anthropic defaults adaptive thinking.display to "omitted" from
// Opus 4.7: thinking tokens are billed but the blocks come back empty. Relayed
// clients expect the Opus 4.6 summary, so fill the default when the client
// said nothing. An explicit value always wins; budget thinking is untouched
// (team 9088a5703).
func NormalizeAdaptiveThinkingDisplay(t *dto.Thinking) {
	if t == nil || t.Type != "adaptive" || t.Display != "" {
		return
	}
	t.Display = "summarized"
}

// normalizeConvertedThinkingDisplay applies the rule to a relaykit conversion
// result, which is typed any; other result types are left alone.
func normalizeConvertedThinkingDisplay(converted any) {
	if claudeRequest, ok := converted.(*dto.ClaudeRequest); ok {
		NormalizeAdaptiveThinkingDisplay(claudeRequest.Thinking)
	}
}
