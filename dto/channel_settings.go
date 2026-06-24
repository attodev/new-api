package dto

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
	// UsageSemantic forces how this channel's upstream usage is interpreted for
	// billing when the upstream does not send a usage_semantic tag. Set to
	// "anthropic" for OpenAI-compatible upstreams that report Anthropic-style
	// disjoint token counts (prompt_tokens EXCLUDES cache), e.g. an older new-api
	// relaying a Claude model. Empty = auto-detect. An explicit usage_semantic
	// from the upstream always takes precedence over this override.
	UsageSemantic string `json:"usage_semantic,omitempty"`
	// UsageSemanticModels restricts the UsageSemantic override to models whose
	// names match any of these glob patterns (e.g. "claude-*"). Empty = apply
	// to all models on the channel.
	UsageSemanticModels []string `json:"usage_semantic_models,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk"
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // service_tier
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // inference_geo Claude
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // speed Claude
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // safety_identifier
	DisableStore                          bool          `json:"disable_store,omitempty"`             // store Codex
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // stream_options.include_obfuscation
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
