package service

import (
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := types.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.Equal(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.Equal(t, 1488, chatSummary.Quota)
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         100,
			CachedCreationTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, "anthropic", summary.UsageSemantic)
	require.Equal(t, 1488, summary.Quota)
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.Equal(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheReadFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenRouter OpenAI-format display keeps prompt_tokens as total input,
	// but billing still separates normal input from cache read tokens.
	// quota = (2604 - 2432) + 2432*0.1 + 383 = 798.2 => 798
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummarySeparatesOpenRouterCacheCreationFromPromptBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "openai/gpt-4.1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedCreationTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// prompt_tokens is still logged as total input, but cache creation is billed separately.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryKeepsPrePRClaudeOpenRouterBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 2432,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Pre-PR PostClaudeConsumeQuota behavior for OpenRouter:
	// prompt = 2604 - 2432 = 172
	// quota = 172 + 2432*0.1 + 383 = 798.2 => 798
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 172, summary.PromptTokens)
	require.Equal(t, 798, summary.Quota)
}

func TestCalculateTextQuotaSummaryTreatsDisjointCacheAsClaudeSemantic(t *testing.T) {
	// Reproduces a real under-billing case: a downstream new-api relays a Claude
	// model through an OpenAI-compatible upstream (request_conversion
	// ["Claude Messages", "OpenAI Compatible"]). The upstream reports
	// Anthropic-style disjoint counts — prompt_tokens EXCLUDES cache — but omits
	// usage_semantic="anthropic". Under genuine OpenAI semantics cached tokens are
	// a subset of prompt_tokens, so cache_read > prompt_tokens is impossible.
	// The old code subtracted cache from prompt, underflowed to a negative base,
	// and clamped quota to 1 (severe under-billing).
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		OriginModelName:         "claude-opus-4-8",
		PriceData: types.PriceData{
			ModelRatio:      2.25,
			CompletionRatio: 5,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     19453,
		CompletionTokens: 1095,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 40990,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Disjoint counts must be billed Claude-style (no subtraction):
	// (19453 + 40990*0.1 + 1095*5) * 2.25 = 29027 * 2.25 = 65310.75 => 65311
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 19453, summary.PromptTokens)
	require.Equal(t, 65311, summary.Quota)
}

func TestCalculateTextQuotaSummaryHonorsChannelUsageSemanticOverride(t *testing.T) {
	// Subtle case the magnitude heuristic cannot catch: an OpenAI-compatible
	// upstream reports Anthropic-style disjoint counts (prompt EXCLUDES cache)
	// with cache < prompt and no usage_semantic tag. Numbers are indistinguishable
	// from genuine OpenAI usage, so the admin declares the channel's semantic
	// explicitly via channel settings.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		OriginModelName:         "claude-opus-4-8",
		ChannelMeta:             &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{UsageSemantic: "anthropic"}},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     50000,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10000, // cache < prompt: heuristic does NOT fire
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Forced Claude-style (no subtraction): 50000 + 10000*0.1 = 51000
	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 51000, summary.Quota)
}

func TestCalculateTextQuotaSummaryUpstreamSemanticOverridesChannelSetting(t *testing.T) {
	// An explicit usage_semantic from the upstream must win over the channel
	// override, so a recent upstream that correctly reports OpenAI-style usage
	// (cache included in prompt) is still billed with cache subtraction.
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		OriginModelName:         "claude-opus-4-8",
		ChannelMeta:             &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{UsageSemantic: "anthropic"}},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     50000, // OpenAI-style: prompt INCLUDES cache
		CompletionTokens: 0,
		UsageSemantic:    "openai",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10000,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenAI-style (subtract cache): (50000-10000) + 10000*0.1 = 41000
	require.False(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, 41000, summary.Quota)
}

func TestUsageSemanticModelPatternMatchesClaudeButNotGPT(t *testing.T) {
	// Channel sets usage_semantic="anthropic" with model pattern "claude-*".
	// A claude-opus-4-8 request should use Anthropic billing (no cache subtraction).
	// A gpt-4o request on the same channel should use OpenAI billing (cache subtracted).
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	priceData := types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 1,
		CacheRatio:      0.1,
		GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
	}
	channelMeta := &relaycommon.ChannelMeta{
		ChannelSetting: dto.ChannelSettings{
			UsageSemantic:       "anthropic",
			UsageSemanticModels: []string{"claude-*"},
		},
	}

	usage := &dto.Usage{
		PromptTokens:     50000,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10000,
		},
	}

	// Claude model: pattern matches → Anthropic billing (no subtraction)
	claudeInfo := &relaycommon.RelayInfo{
		OriginModelName:         "claude-opus-4-8",
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		ChannelMeta:             channelMeta,
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	claudeSummary := calculateTextQuotaSummary(ctx, claudeInfo, usage)
	require.True(t, claudeSummary.IsClaudeUsageSemantic, "claude-opus-4-8 should be Anthropic-semantic")
	require.Equal(t, 51000, claudeSummary.Quota) // 50000 + 10000*0.1 = 51000

	// GPT model: pattern does not match → OpenAI billing (cache subtracted)
	gptInfo := &relaycommon.RelayInfo{
		OriginModelName:         "gpt-4o",
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		ChannelMeta:             channelMeta,
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	gptSummary := calculateTextQuotaSummary(ctx, gptInfo, usage)
	require.False(t, gptSummary.IsClaudeUsageSemantic, "gpt-4o should be OpenAI-semantic")
	require.Equal(t, 41000, gptSummary.Quota) // (50000-10000) + 10000*0.1 = 41000
}

func TestUsageSemanticEmptyModelPatternsAppliesToAll(t *testing.T) {
	// Empty UsageSemanticModels = apply override to ALL models (backward compat).
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName:         "gpt-4o",
		FinalRequestRelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				UsageSemantic:       "anthropic",
				UsageSemanticModels: nil, // empty → all models
			},
		},
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			CacheRatio:      0.1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:        50000,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 10000},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.True(t, summary.IsClaudeUsageSemantic, "empty patterns should apply override to all models")
	require.Equal(t, 51000, summary.Quota)
}

func TestComposeTieredTextQuotaKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{
					CallCount: 1,
				},
				dto.BuildInToolFileSearch: &relaycommon.BuildInToolInfo{
					CallCount: 2,
				},
			},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1000, &billingexpr.TieredResult{
		ActualQuotaBeforeGroup: 1000,
		ActualQuotaAfterGroup:  1000,
	})

	require.Equal(t, int64(13000), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14000, quota)
}

// TestComposeTieredTextQuota_SaturatesCombinedSum reproduces a real
// billing-overflow bug: tieredQuota alone can sit near the int32 bound, and
// adding the tool-call surcharge on top of it (int addition) can wrap the
// combined total into a negative charge instead of saturating.
func TestComposeTieredTextQuota_SaturatesCombinedSum(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("image_generation_call", true)
	ctx.Set("image_generation_call_quality", "low")
	ctx.Set("image_generation_call_size", "1024x1024")

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "o1",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
		},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolFileSearch: {CallCount: 1_000_000_000},
			},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1,
			EstimatedQuotaBeforeGroup: 2_000_000_000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 2_000_000_000, &billingexpr.TieredResult{
		ActualQuotaBeforeGroup: 2_000_000_000,
		ActualQuotaAfterGroup:  2_000_000_000,
	})

	require.GreaterOrEqual(t, quota, 0, "an oversized tieredQuota+surcharge sum must saturate, not go negative")
	require.LessOrEqual(t, int64(quota), int64(math.MaxInt32), "quota is persisted to a 32-bit database column and must saturate at that bound")
}

func TestComposeTieredTextQuotaFallbackKeepsToolCallSurcharges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	quota := composeTieredTextQuota(relayInfo, summary, 1250, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 13750, quota)
}

func TestComposeTieredTextQuotaErrorFallbackUsesPreConsumedQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("claude_web_search_requests", 2)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:      1,
			CompletionRatio: 1,
			GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1.25},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:               "tiered_expr",
			GroupRatio:                1.25,
			EstimatedQuotaBeforeGroup: 1000,
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// tieredResult=nil simulates a settlement error where TryTieredSettle
	// falls back to FinalPreConsumedQuota (2000), which differs from
	// EstimatedQuotaBeforeGroup * GroupRatio (1250).
	preConsumedFallback := 2000
	quota := composeTieredTextQuota(relayInfo, summary, preConsumedFallback, nil)

	require.Equal(t, int64(12500), summary.ToolCallSurchargeQuota.Round(0).IntPart())
	require.Equal(t, 14500, quota)
}

func TestCalculateTextQuotaSummaryBillsOpenAICacheWriteTokensAtCacheCreationRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-terra",
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     2604,
		CompletionTokens: 383,
		PromptTokensDetails: dto.InputTokenDetails{
			CacheWriteTokens: 100,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// OpenAI's native gpt-5.6 field is cache_write_tokens (not the Claude-style
	// cached_creation_tokens), but it must still be billed at CacheCreationRatio
	// instead of silently falling into the base prompt rate.
	// quota = (2604 - 100) + 100*1.25 + 383 = 3012
	require.Equal(t, 2604, summary.PromptTokens)
	require.Equal(t, 3012, summary.Quota)
}

func TestCalculateTextQuotaSummaryClampsBaseTokensWhenCacheCountsExceedPromptTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5.6-terra",
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	// Pathological upstream report: cached + cache-write alone exceed prompt_tokens.
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:     60,
			CacheWriteTokens: 60,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// Without clamping, base = 100 - 60 - 60 = -20, which would silently
	// undercharge the cache portions. Clamped base is 0.
	// quota = 0 + 60*0.1 + 60*1.25 = 81
	require.Equal(t, 81, summary.Quota)
}

// TestCalculateTextQuotaSummary_SaturatesOnOverflow reproduces a real bug:
// this is the primary chat/completion billing calculation - used for
// essentially every ordinary request - but its final
// int(quotaCalculateDecimal.Round(0).IntPart()) conversion had no int32
// saturation, unlike this same file's tiered path (composeTieredTextQuota,
// which already routes through decimalToQuota) and this session's other
// billing-overflow fixes (RecalculateTaskQuotaByTokens, relay_task.go's
// recalcQuotaFromRatios). An oversized ModelRatio/GroupRatio/OtherRatios
// product - a misconfigured custom model ratio, or a poisoned adaptor-
// supplied ratio - would silently overflow the 32-bit quota DB column on
// persistence instead of being clamped.
func TestCalculateTextQuotaSummary_SaturatesOnOverflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-4",
		PriceData: types.PriceData{
			ModelRatio:      1e10,
			CompletionRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 0,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, math.MaxInt32, summary.Quota)
}

// TestCalculateTextQuotaSummary_SaturatesOnOverflow_UsePrice covers the
// UsePrice (fixed-price) branch, which has the identical unguarded
// conversion.
func TestCalculateTextQuotaSummary_SaturatesOnOverflow_UsePrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-4",
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 1e10,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 0,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, math.MaxInt32, summary.Quota)
}

func newCachedUsageTestRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: model,
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    2,
			CacheRatio:         0.25,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
}

func TestCalculateTextQuotaSummaryUsesResponsesInputTokenDetailsCachedTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := newCachedUsageTestRelayInfo("gpt-4o")

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens: 40,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 40, summary.CacheTokens)
	// 60 uncached input + 40*0.25 cached input + 10*2 output = 90.
	require.Equal(t, 90, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesResponsesInputTokenDetailsCacheCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := newCachedUsageTestRelayInfo("gpt-4o")

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		TotalTokens:      110,
		InputTokensDetails: &dto.InputTokenDetails{
			CachedCreationTokens: 20,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 20, summary.CacheCreationTokens)
}

func TestCalculateTextQuotaSummaryFallsBackToPromptCacheHitTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := newCachedUsageTestRelayInfo("deepseek-chat")

	usage := &dto.Usage{
		PromptTokens:         100,
		CompletionTokens:     10,
		TotalTokens:          110,
		PromptCacheHitTokens: 35,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 35, summary.CacheTokens)
	// 65 uncached input + 35*0.25 cached input + 10*2 output = 93.75 => 94.
	require.Equal(t, 94, summary.Quota)
}

func TestNormalizeCachedUsageDoesNotOverwriteCanonicalValues(t *testing.T) {
	usage := &dto.Usage{
		PromptCacheHitTokens: 55,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         8,
			CachedCreationTokens: 3,
			CacheWriteTokens:     6,
		},
		InputTokensDetails: &dto.InputTokenDetails{
			CachedTokens:         40,
			CachedCreationTokens: 5,
		},
	}

	normalizeCachedUsage(usage)

	require.Equal(t, 8, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 3, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	normalizeCachedUsage(nil)
}

// Gemini bills image input at the text rate. With no image ratio configured
// (ratio_setting.GetImageRatio defaults to 1), attributing IMAGE prompt tokens
// to PromptTokensDetails.ImageTokens must not change the settled quota.
func TestCalculateTextQuotaSummaryGeminiImagePromptKeepsTextRateWithoutImageRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const model = "gemini-3-flash-preview"
	imageRatio, configured := ratio_setting.GetImageRatio(model)
	require.False(t, configured, "test requires a Gemini model without a configured image ratio")
	require.Equal(t, 1.0, imageRatio)

	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:     types.RelayFormatGemini,
			OriginModelName: model,
			PriceData: types.PriceData{
				ModelRatio:      1.25,
				CompletionRatio: 4,
				ImageRatio:      imageRatio,
				GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime: time.Now(),
		}
	}

	mixed := &dto.Usage{PromptTokens: 2000, CompletionTokens: 50, TotalTokens: 2050}
	mixed.PromptTokensDetails.TextTokens = 911 // TEXT 800 + VIDEO 100 + DOCUMENT 11
	mixed.PromptTokensDetails.ImageTokens = 1089

	allText := &dto.Usage{PromptTokens: 2000, CompletionTokens: 50, TotalTokens: 2050}
	allText.PromptTokensDetails.TextTokens = 2000

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	mixedSummary := calculateTextQuotaSummary(ctx, newInfo(), mixed)
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	textSummary := calculateTextQuotaSummary(ctx, newInfo(), allText)

	require.Equal(t, 1089, mixedSummary.ImageTokens)
	require.Equal(t, textSummary.Quota, mixedSummary.Quota)
}

// Gemini promptTokensDetails include the cached portion. The handler caps
// ImageTokens at the uncached remainder (500 of 1089 here) so the cached image
// portion is billed only at the cache rate and never double-counted.
func TestCalculateTextQuotaSummaryGeminiCachedImagePromptIsNotOvercharged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const model = "gemini-3-flash-preview"
	newInfo := func(imageRatio float64) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:     types.RelayFormatGemini,
			OriginModelName: model,
			PriceData: types.PriceData{
				ModelRatio:      1,
				CompletionRatio: 4,
				CacheRatio:      0.25,
				ImageRatio:      imageRatio,
				GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime: time.Now(),
		}
	}
	quota := func(imageRatio float64, usage *dto.Usage) int {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return calculateTextQuotaSummary(ctx, newInfo(imageRatio), usage).Quota
	}

	// Usage as produced by buildUsageFromGeminiMetadata for P=2000, cached=1500,
	// TEXT 911 + IMAGE 1089 (image capped to the 500 uncached tokens).
	capped := func() *dto.Usage {
		u := &dto.Usage{PromptTokens: 2000, CompletionTokens: 50, TotalTokens: 2050}
		u.PromptTokensDetails.CachedTokens = 1500
		u.PromptTokensDetails.ImageTokens = 500
		u.PromptTokensDetails.TextTokens = 1500
		return u
	}
	allText := func() *dto.Usage {
		u := &dto.Usage{PromptTokens: 2000, CompletionTokens: 50, TotalTokens: 2050}
		u.PromptTokensDetails.CachedTokens = 1500
		u.PromptTokensDetails.TextTokens = 2000
		return u
	}

	// Old way: (2000-1500) at 1 + 1500*0.25 + 50*4 = 500 + 375 + 200 = 1075.
	require.Equal(t, 1075, quota(1, allText()))
	require.Equal(t, 1075, quota(1, capped()), "default image ratio must leave quota unchanged")

	// ImageRatio 2: only the 500 uncached image tokens get the 2x ratio.
	// (2000-1500-500)=0 base + 1500*0.25 + 500*2 + 200 = 1575.
	require.Equal(t, 1575, quota(2, capped()))
}

// Settlement subtracts cached + image + audio from the base (audio only when the
// model has an audio input price, as gemini-2.5-flash does) and clamps at zero,
// so image tokens that overlap cached and audio tokens are billed twice. The
// handler caps ImageTokens at P - cached - audio; with that cap an
// image+audio+cached prompt settles exactly like the same prompt without
// image attribution.
func TestCalculateTextQuotaSummaryGeminiImageAudioCachedPromptIsNotOvercharged(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const model = "gemini-2.5-flash"
	require.Greater(t, operation_setting.GetGeminiInputAudioPricePerMillionTokens(model), 0.0,
		"test requires a model with a built-in audio input price")
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			RelayFormat:     types.RelayFormatGemini,
			OriginModelName: model,
			PriceData: types.PriceData{
				ModelRatio:      1,
				CompletionRatio: 4,
				CacheRatio:      0.25,
				ImageRatio:      1,
				GroupRatioInfo:  types.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime: time.Now(),
		}
	}
	quota := func(usage *dto.Usage) int {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return calculateTextQuotaSummary(ctx, newInfo(), usage).Quota
	}
	build := func(image, text int) *dto.Usage {
		u := &dto.Usage{PromptTokens: 2000, CompletionTokens: 50, TotalTokens: 2050}
		u.PromptTokensDetails.CachedTokens = 1000
		u.PromptTokensDetails.AudioTokens = 500
		u.PromptTokensDetails.ImageTokens = image
		u.PromptTokensDetails.TextTokens = text
		return u
	}

	// No image attribution: base 2000-1000-500 = 500, cached 1000*0.25 = 250,
	// audio 500 at the model's audio price, completion 50*4 = 200.
	noImage := quota(build(0, 1000))
	require.Equal(t, 1200, noImage)

	// TEXT 700 + IMAGE 800 + AUDIO 500 with 1000 cached: the handler caps the
	// image at 2000-1000-500 = 500, which settles the same as no attribution.
	require.Equal(t, noImage, quota(build(500, 1000)))

	// Without the audio-aware cap the uncapped 800 image tokens would be billed
	// on top of the audio and cached tokens they overlap.
	require.Equal(t, 1500, quota(build(800, 700)))
}
