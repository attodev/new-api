package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// capGeminiImagePromptTokens keeps Gemini image prompt tokens out of the prompt
// portions that settlement prices separately. Gemini promptTokensDetails
// include the cached and audio tokens, and settlement bills
// base = prompt - cached - image - audio (each subtracted only when that
// portion has its own price) plus the separate prices, so image tokens that
// overlap a separately priced portion would be billed twice. Only the
// portions the caller says are excluded from the base reduce the cap:
//
//   - flat ratio settlement (calculateTextQuotaSummary) always prices cached
//     tokens and prices audio only when the model has a Gemini audio input
//     price;
//   - tiered settlement (BuildTieredTokenParams) excludes cached tokens from p
//     only when the expression uses cr, and audio only when it uses ai
//     (pkg/billingexpr/expr.md, p/c auto-exclusion).
//
// The capped-off amount is moved to TextTokens with team's adjustment
// TextTokens = max(TextTokens, PromptTokens - AudioTokens - ImageTokens).
// The details then sum to PromptTokens in the usual TEXT/IMAGE/AUDIO case, but
// not always: canonicalGeminiUsage sets TextTokens = PromptTokens when Gemini
// reported no TEXT or AUDIO detail (IMAGE only), and drops VIDEO/DOCUMENT
// details. Neither flat nor tiered settlement reads TextTokens.
//
// Only a Gemini canonical usage (UsageSemantic gemini, set by
// BillingUsage.CanonicalUsage) is capped, and a capped value is a shallow copy:
// the caller's usage is never modified, so the flat and tiered paths can each
// cap the same canonical usage by their own rule. Any other usage, or one that
// needs no cap, is returned as is.
// Ported from team af1eddc8e/c20c6d664 into the host (relaykit stays upstream).
func capGeminiImagePromptTokens(usage *dto.Usage, excludeCached, excludeAudio bool) *dto.Usage {
	if usage == nil || usage.UsageSemantic != dto.BillingUsageSemanticGemini {
		return usage
	}
	d := usage.PromptTokensDetails
	maxImage := usage.PromptTokens
	if excludeCached {
		maxImage -= d.CachedTokens
	}
	if excludeAudio {
		maxImage -= d.AudioTokens
	}
	maxImage = max(maxImage, 0)
	if d.ImageTokens <= maxImage {
		return usage
	}
	capped := *usage
	capped.PromptTokensDetails.ImageTokens = maxImage
	capped.PromptTokensDetails.TextTokens = max(d.TextTokens, usage.PromptTokens-d.AudioTokens-maxImage)
	return &capped
}
