package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// capGeminiImagePromptTokens keeps Gemini image prompt tokens out of the
// cached and audio portions. Gemini promptTokensDetails include cached tokens,
// and text settlement bills base = prompt - cached - image - audio plus
// cached*cacheRatio + image*imageRatio + audio price, so image tokens that
// overlap the cached or audio portions would be billed twice. The capped-off
// amount returns to TextTokens so the details still sum to PromptTokens.
//
// It is applied only to the canonical usage built from a Gemini billing
// snapshot (UsageSemantic is set by BillingUsage.CanonicalUsage, which returns
// a fresh Usage on every call, so mutating it in place is safe). Any other
// usage is returned unchanged.
// Ported from team af1eddc8e/c20c6d664 into the host (relaykit stays upstream).
func capGeminiImagePromptTokens(usage *dto.Usage) *dto.Usage {
	if usage == nil || usage.UsageSemantic != dto.BillingUsageSemanticGemini {
		return usage
	}
	d := &usage.PromptTokensDetails
	maxImage := max(usage.PromptTokens-d.CachedTokens-d.AudioTokens, 0)
	if d.ImageTokens > maxImage {
		d.TextTokens += d.ImageTokens - maxImage
		d.ImageTokens = maxImage
	}
	return usage
}
