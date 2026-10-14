package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// geminiUsage builds a top-level usage carrying a Gemini billing snapshot, the
// shape the Gemini relay hands to settlement.
func geminiUsage(prompt, cached, completion int, details map[string]int) *dto.Usage {
	md := &dto.GeminiUsageMetadata{PromptTokenCount: prompt, CachedContentTokenCount: cached, CandidatesTokenCount: completion, TotalTokenCount: prompt + completion}
	for modality, n := range details {
		md.PromptTokensDetails = append(md.PromptTokensDetails, dto.GeminiPromptTokensDetails{Modality: modality, TokenCount: n})
	}
	u := &dto.Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}
	u.BillingUsage = dto.NewGeminiChatBillingUsage(md)
	return u
}

func TestEffectiveBillingUsageCapsGeminiImageByCachedAndAudio(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 1500, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 500, u.PromptTokensDetails.ImageTokens)
	require.Equal(t, 2000, u.PromptTokensDetails.ImageTokens+u.PromptTokensDetails.TextTokens+u.PromptTokensDetails.AudioTokens)

	u = effectiveBillingUsage(geminiUsage(478, 300, 20, map[string]int{"TEXT": 100, "IMAGE": 258, "AUDIO": 50, "VIDEO": 30, "DOCUMENT": 40}))
	require.Equal(t, 128, u.PromptTokensDetails.ImageTokens) // 478-300-50
	require.Equal(t, 50, u.PromptTokensDetails.AudioTokens)
}

func TestEffectiveBillingUsageLeavesUncappedGeminiAndNonGeminiAlone(t *testing.T) {
	u := effectiveBillingUsage(geminiUsage(2000, 0, 50, map[string]int{"TEXT": 911, "IMAGE": 1089}))
	require.Equal(t, 1089, u.PromptTokensDetails.ImageTokens)

	plain := &dto.Usage{PromptTokens: 100, CompletionTokens: 10}
	plain.PromptTokensDetails.ImageTokens = 90
	plain.PromptTokensDetails.CachedTokens = 50
	require.Same(t, plain, effectiveBillingUsage(plain))
	require.Equal(t, 90, plain.PromptTokensDetails.ImageTokens)
}

// Gemini promptTokensDetails include the cached portion. The cap keeps
// ImageTokens at the uncached remainder (500 of 1089 here) so the cached image
// portion is billed only at the cache rate and never double-counted.
// Ported from team service/text_quota_test.go (af1eddc8e).
func TestCalculateTextQuotaSummaryGeminiCachedImagePromptIsNotOvercharged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const model = "gemini-3-flash-preview"
	newInfo := func(imageRatio float64) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:     types.RelayFormatGemini,
			OriginModelName: model,
			PriceData: hosttypes.PriceData{
				ModelRatio:      1,
				CompletionRatio: 4,
				CacheRatio:      0.25,
				ImageRatio:      imageRatio,
				GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime: time.Now(),
		}
	}
	quota := func(imageRatio float64, usage *dto.Usage) int {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return calculateTextQuotaSummary(ctx, newInfo(imageRatio), effectiveBillingUsage(usage)).Quota
	}

	// P=2000, cached=1500, TEXT 911 + IMAGE 1089.
	mixed := func() *dto.Usage {
		return geminiUsage(2000, 1500, 50, map[string]int{"TEXT": 911, "IMAGE": 1089})
	}
	allText := func() *dto.Usage {
		return geminiUsage(2000, 1500, 50, map[string]int{"TEXT": 2000})
	}

	// All text: (2000-1500) at 1 + 1500*0.25 + 50*4 = 500 + 375 + 200 = 1075.
	require.Equal(t, 1075, quota(1, allText()))
	require.Equal(t, 1075, quota(1, mixed()), "default image ratio must leave quota unchanged")

	// ImageRatio 2: only the 500 uncached image tokens get the 2x ratio.
	// (2000-1500-500)=0 base + 1500*0.25 + 500*2 + 200 = 1575.
	require.Equal(t, 1575, quota(2, mixed()))
}

// Settlement subtracts cached + image + audio from the base (audio only when the
// model has an audio input price, as gemini-2.5-flash does) and clamps at zero,
// so image tokens that overlap cached and audio tokens would be billed twice.
// With the cap at P - cached - audio an image+audio+cached prompt settles
// exactly like the same prompt without image attribution.
// Ported from team service/text_quota_test.go (c20c6d664).
func TestCalculateTextQuotaSummaryGeminiImageAudioCachedPromptIsNotOvercharged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const model = "gemini-2.5-flash"
	require.Greater(t, operation_setting.GetGeminiInputAudioPricePerMillionTokens(model), 0.0,
		"test requires a model with a built-in audio input price")
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:     types.RelayFormatGemini,
			OriginModelName: model,
			PriceData: hosttypes.PriceData{
				ModelRatio:      1,
				CompletionRatio: 4,
				CacheRatio:      0.25,
				ImageRatio:      1,
				GroupRatioInfo:  hosttypes.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime: time.Now(),
		}
	}
	quota := func(usage *dto.Usage) int {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return calculateTextQuotaSummary(ctx, newInfo(), usage).Quota
	}
	build := func(details map[string]int) *dto.Usage {
		return geminiUsage(2000, 1000, 50, details)
	}

	// No image attribution: base 2000-1000-500 = 500, cached 1000*0.25 = 250,
	// audio 500 at the model's audio price, completion 50*4 = 200.
	noImage := quota(effectiveBillingUsage(build(map[string]int{"TEXT": 1500, "AUDIO": 500})))
	require.Equal(t, 1200, noImage)

	// TEXT 700 + IMAGE 800 + AUDIO 500 with 1000 cached: the image is capped at
	// 2000-1000-500 = 500, which settles the same as no attribution.
	imageAudio := map[string]int{"TEXT": 700, "IMAGE": 800, "AUDIO": 500}
	require.Equal(t, noImage, quota(effectiveBillingUsage(build(imageAudio))))

	// Without the cap (the raw canonical usage) the 800 image tokens would be
	// billed on top of the audio and cached tokens they overlap.
	uncapped, ok := usageFromBillingUsage(build(imageAudio))
	require.True(t, ok)
	require.Equal(t, 800, uncapped.PromptTokensDetails.ImageTokens)
	require.Equal(t, 1500, quota(uncapped))
}
