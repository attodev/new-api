package claude

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// Claude models accept different effort levels. A request is never rejected
// for asking for one a model does not take: the effort is lowered to the
// nearest accepted level, or removed when the model takes none.
//
// The table mirrors upstream claudeCapabilitiesFor
// (relaykit/relayconvert/reasoning/claude.go). To add a model, put its row
// above any broader prefix it shares: the first matching prefix wins.

type claudeEffortCapability struct {
	prefix         string
	supportsEffort bool
	supportsXHigh  bool
	supportsMax    bool
	// offAtMostHigh marks models that accept effort high or below only while
	// thinking is off (disabled or between_tools).
	offAtMostHigh bool
	// adaptive marks models that accept thinking.type "adaptive".
	adaptive bool
	// supportsManual marks models that accept thinking.type "enabled" with
	// budget_tokens. The others take adaptive thinking plus an effort only.
	supportsManual bool
	// strictSampling marks models that reject a non-default temperature,
	// top_p or top_k.
	strictSampling bool
	// supportsDisable marks models that accept thinking.type "disabled".
	supportsDisable bool
	// disableAsBetweenTools marks models whose lowest thinking setting is
	// {"type":"between_tools"} instead of {"type":"disabled"}.
	disableAsBetweenTools bool
	// defaultEffort is the effort the model runs at when a request sets none.
	// "" means high, the default on every model but Opus 5.5 and Haiku 5.x
	// (https://platform.claude.com/docs/en/build-with-claude/effort).
	defaultEffort string
}

var claudeEffortTable = []claudeEffortCapability{
	{prefix: "claude-fable-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true},
	{prefix: "claude-mythos-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true},
	// Upstream never clears supportsManual for Mythos Preview.
	{prefix: "claude-mythos-preview", supportsEffort: true, supportsMax: true, adaptive: true, supportsManual: true, strictSampling: true},
	{prefix: "claude-opus-5-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true, defaultEffort: "medium"},
	// Sonnet 5.5 turns thinking off with between_tools, which takes effort
	// high or below only.
	{prefix: "claude-sonnet-5-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true, adaptive: true, strictSampling: true, disableAsBetweenTools: true},
	// Opus 5 and Haiku 5.x reject disabled thinking with xhigh or max.
	{prefix: "claude-opus-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true, adaptive: true, strictSampling: true, supportsDisable: true},
	{prefix: "claude-haiku-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true, adaptive: true, strictSampling: true, defaultEffort: "medium", supportsDisable: true},
	{prefix: "claude-sonnet-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true, supportsDisable: true},
	{prefix: "claude-opus-4-8", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true, supportsDisable: true},
	{prefix: "claude-opus-4-7", supportsEffort: true, supportsXHigh: true, supportsMax: true, adaptive: true, strictSampling: true, supportsDisable: true},
	{prefix: "claude-opus-4-6", supportsEffort: true, supportsMax: true, adaptive: true, supportsManual: true, supportsDisable: true},
	{prefix: "claude-sonnet-4-6", supportsEffort: true, supportsMax: true, adaptive: true, supportsManual: true, supportsDisable: true},
	{prefix: "claude-opus-4-5", supportsEffort: true, supportsManual: true, supportsDisable: true},
	// Known models that take no effort.
	{prefix: "claude-sonnet-4-5", supportsManual: true, supportsDisable: true},
	{prefix: "claude-haiku-4-5", supportsManual: true, supportsDisable: true},
	{prefix: "claude-opus-4-1", supportsManual: true, supportsDisable: true},
	{prefix: "claude-opus-4-", supportsManual: true, supportsDisable: true},
	{prefix: "claude-sonnet-4-", supportsManual: true, supportsDisable: true},
	{prefix: "claude-3-7-sonnet", supportsManual: true, supportsDisable: true},
}

// claudeEffortCapabilityFor returns the table row for a model, or false when
// the model is not in the table.
func claudeEffortCapabilityFor(model string) (claudeEffortCapability, bool) {
	model = strings.ToLower(model)
	for _, row := range claudeEffortTable {
		if strings.HasPrefix(model, row.prefix) {
			return row, true
		}
	}
	return claudeEffortCapability{}, false
}

func claudeSupportsEffort(model string) bool {
	row, ok := claudeEffortCapabilityFor(model)
	return ok && row.supportsEffort
}

// claudeSupportsManualThinking reports whether the model accepts
// thinking.type "enabled" with budget_tokens. Models missing from the table
// keep today's behavior and are assumed to.
func claudeSupportsManualThinking(model string) bool {
	row, ok := claudeEffortCapabilityFor(model)
	return !ok || row.supportsManual
}

// claudeStrictSampling reports whether the model rejects a non-default
// temperature, top_p or top_k. Models missing from the table do not.
func claudeStrictSampling(model string) bool {
	row, ok := claudeEffortCapabilityFor(model)
	return ok && row.strictSampling
}

// claudeDisableCapability reports whether the model accepts
// thinking.type "disabled" and, when it does not, whether between_tools is
// its lowest thinking setting. Models missing from the table accept disabled.
func claudeDisableCapability(model string) (supportsDisable bool, asBetweenTools bool) {
	row, ok := claudeEffortCapabilityFor(model)
	if !ok {
		return true, false
	}
	return row.supportsDisable, row.disableAsBetweenTools
}

// claudeDefaultEffort is the effort the model runs at when a request sets none.
func claudeDefaultEffort(model string) string {
	if row, ok := claudeEffortCapabilityFor(model); ok && row.defaultEffort != "" {
		return row.defaultEffort
	}
	return "high"
}

// ClaudeEffortFromBudget maps a thinking budget to the effort that stands for
// it, as upstream EffortFromBudget
// (relaykit/relayconvert/reasoning/intent.go) does: 0 asks for no thinking and
// a negative (dynamic) budget for high.
func ClaudeEffortFromBudget(budget int) string {
	switch {
	case budget == 0:
		return "none"
	case budget < 0:
		return "high"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	default:
		return "high"
	}
}

// ClaudeEffortSuffix splits an effort suffix ("-high", "-xhigh", ...) off a
// model that takes adaptive thinking with an effort. It returns ok=false for
// every other model, whose name is then left as it is.
func ClaudeEffortSuffix(model string) (baseModel string, effort string, ok bool) {
	baseModel, effort, ok = reasoning.TrimEffortSuffix(model)
	if !ok || effort == "" {
		return model, "", false
	}
	row, known := claudeEffortCapabilityFor(baseModel)
	if !known || !row.supportsEffort || !row.adaptive {
		return model, "", false
	}
	return baseModel, effort, true
}

// RenderClaudeThinking turns a request to think into the form the model
// accepts. A manual-capable model given a budget gets
// {type:"enabled", budget_tokens}. Any other model gets adaptive thinking
// (keeping the client's block_binding and display) plus output_config.effort:
// the requested effort, else the effort the budget stands for, else the
// model's default effort. Other output_config keys are kept. A nil budget on a
// manual-capable model also renders adaptive, so callers pass nil there only
// for models that take adaptive thinking.
func RenderClaudeThinking(req *dto.ClaudeRequest, model string, effort string, budget *int) {
	if budget != nil && claudeSupportsManualThinking(model) {
		budgetTokens := *budget
		req.Thinking = &dto.Thinking{Type: "enabled", BudgetTokens: &budgetTokens}
		return
	}
	accepted := ClaudeEffort(model, effort)
	if accepted == "" && budget != nil {
		accepted = ClaudeEffort(model, ClaudeEffortFromBudget(*budget))
	}
	if accepted == "" {
		accepted = ClaudeEffort(model, claudeDefaultEffort(model))
	}
	req.Thinking = req.Thinking.WithAdaptive()
	if accepted != "" {
		req.OutputConfig = withClaudeOutputConfigEffort(req.OutputConfig, accepted)
	}
}

// RenderClaudeThinkingSuffix renders the "-thinking" model suffix. A
// manual-capable model gets a budget of ThinkingAdapterBudgetTokensPercentage
// of max_tokens (max_tokens raised to 1280 first so the budget reaches the
// 1024 minimum) and the function returns true, so the caller can apply the
// sampling manual thinking needs. Any other model gets adaptive thinking at its
// default effort.
func RenderClaudeThinkingSuffix(req *dto.ClaudeRequest, model string) (manual bool) {
	if !claudeSupportsManualThinking(model) {
		RenderClaudeThinking(req, model, "", nil)
		return false
	}
	if req.MaxTokens == nil || *req.MaxTokens < 1280 {
		req.MaxTokens = common.GetPointer[uint](1280)
	}
	budget := int(float64(*req.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)
	RenderClaudeThinking(req, model, "", &budget)
	return true
}

// RenderClaudeReasoningEffort renders an OpenAI-style reasoning effort.
// Manual-capable models keep the legacy budgets (low 1280, medium 2048,
// high 4096) and ignore other levels; the others get adaptive thinking at the
// effort they accept. "none" asks for thinking off, which only changes models
// that cannot turn it off (see renderClaudeThinkingOff). Unknown levels leave
// thinking alone.
func RenderClaudeReasoningEffort(req *dto.ClaudeRequest, model string, effort string) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	var budget *int
	switch effort {
	case "none":
		renderClaudeThinkingOff(req, model)
		return
	case "low":
		budget = common.GetPointer(1280)
	case "medium":
		budget = common.GetPointer(2048)
	case "high":
		budget = common.GetPointer(4096)
	case "minimal", "xhigh", "max":
	default:
		return
	}
	if budget == nil && claudeSupportsManualThinking(model) {
		return
	}
	RenderClaudeThinking(req, model, effort, budget)
}

// AdaptClaudeClientThinking converts client-sent thinking the model rejects.
// {type:"enabled"} on a model without budget thinking becomes adaptive
// thinking: an effort the client set in output_config wins, otherwise the
// budget picks the effort. {type:"disabled"} on a model that cannot turn
// thinking off becomes its lowest thinking setting (renderClaudeThinkingOff).
func AdaptClaudeClientThinking(req *dto.ClaudeRequest, model string) {
	if req.Thinking == nil {
		return
	}
	switch req.Thinking.Type {
	case "enabled":
		if !claudeSupportsManualThinking(model) {
			RenderClaudeThinking(req, model, req.GetEfforts(), req.Thinking.BudgetTokens)
		}
	case "disabled":
		renderClaudeThinkingOff(req, model)
	}
}

// renderClaudeThinkingOff renders a request to turn thinking off for a model
// that rejects {type:"disabled"}, following upstream RenderClaude
// (relaykit/relayconvert/reasoning/claude.go:166-217). Models whose lowest
// setting is between_tools get {type:"between_tools"}, which takes no other
// thinking field, with any output_config.effort capped at high. The others
// cannot turn thinking off at all and run adaptive thinking at low effort,
// whatever effort the client sent; block_binding and display are kept. Models
// that accept disabled thinking are left alone.
func renderClaudeThinkingOff(req *dto.ClaudeRequest, model string) {
	supportsDisable, asBetweenTools := claudeDisableCapability(model)
	if supportsDisable {
		return
	}
	if asBetweenTools {
		req.Thinking = &dto.Thinking{Type: "between_tools"}
		req.OutputConfig = NormalizeClaudeOutputConfig(model, "between_tools", req.OutputConfig)
		return
	}
	req.Thinking = req.Thinking.WithAdaptive()
	if effort := ClaudeEffort(model, "low"); effort != "" {
		req.OutputConfig = withClaudeOutputConfigEffort(req.OutputConfig, effort)
	}
}

// ApplyClaudeStrictSampling clears temperature, top_p and top_k on models
// that reject non-default sampling.
func ApplyClaudeStrictSampling(req *dto.ClaudeRequest, model string) {
	if !claudeStrictSampling(model) {
		return
	}
	req.Temperature = nil
	req.TopP = nil
	req.TopK = nil
}

// withClaudeOutputConfigEffort sets output_config.effort, keeping every other
// output_config key. An unparsable output_config is replaced.
func withClaudeOutputConfigEffort(outputConfig json.RawMessage, effort string) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(outputConfig) > 0 {
		if err := common.Unmarshal(outputConfig, &fields); err != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
	}
	quoted, err := common.Marshal(effort)
	if err != nil {
		return outputConfig
	}
	fields["effort"] = quoted
	raw, err := common.Marshal(fields)
	if err != nil {
		return outputConfig
	}
	return raw
}

// ClaudeEffort returns the effort level the model accepts in place of the
// requested one: xhigh becomes max on models without xhigh and high on models
// without max, and the effort is removed ("") when the model takes none.
// "" and "none" request no effort. Models missing from the table keep the
// requested effort unchanged.
func ClaudeEffort(model, effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" || effort == "none" {
		return ""
	}
	row, ok := claudeEffortCapabilityFor(model)
	if !ok {
		return effort
	}
	if !row.supportsEffort {
		return ""
	}
	switch effort {
	case "minimal":
		return "low"
	case "xhigh":
		switch {
		case row.supportsXHigh:
			return "xhigh"
		case row.supportsMax:
			return "max"
		default:
			return "high"
		}
	case "max":
		if !row.supportsMax {
			return "high"
		}
	}
	return effort
}

// ClaudeEffortForThinking is ClaudeEffort for a request whose thinking type is
// known: models that accept effort high or below only while thinking is off
// get xhigh and max lowered to high when thinking is "disabled" or
// "between_tools".
func ClaudeEffortForThinking(model, effort, thinkingType string) string {
	effort = ClaudeEffort(model, effort)
	if effort != "xhigh" && effort != "max" {
		return effort
	}
	if thinkingType != "disabled" && thinkingType != "between_tools" {
		return effort
	}
	if row, ok := claudeEffortCapabilityFor(model); ok && row.offAtMostHigh {
		return "high"
	}
	return effort
}

// NormalizeClaudeOutputConfig lowers or removes output_config.effort to what
// the model accepts, keeping every other output_config key. It returns nil
// when nothing is left, and the input unchanged when it carries no effort or
// cannot be parsed.
func NormalizeClaudeOutputConfig(model, thinkingType string, outputConfig json.RawMessage) json.RawMessage {
	if len(outputConfig) == 0 {
		return outputConfig
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(outputConfig, &fields); err != nil {
		return outputConfig
	}
	rawEffort, ok := fields["effort"]
	if !ok {
		return outputConfig
	}
	var requested string
	if err := common.Unmarshal(rawEffort, &requested); err != nil {
		return outputConfig
	}
	effort := ClaudeEffortForThinking(model, requested, thinkingType)
	if effort == requested {
		return outputConfig
	}
	if effort == "" {
		delete(fields, "effort")
	} else {
		quoted, err := common.Marshal(effort)
		if err != nil {
			return outputConfig
		}
		fields["effort"] = quoted
	}
	if len(fields) == 0 {
		return nil
	}
	normalized, err := common.Marshal(fields)
	if err != nil {
		return outputConfig
	}
	return normalized
}
