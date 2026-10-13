package claude

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
}

var claudeEffortTable = []claudeEffortCapability{
	{prefix: "claude-fable-5", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	{prefix: "claude-mythos-5", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	{prefix: "claude-mythos-preview", supportsEffort: true, supportsMax: true},
	{prefix: "claude-opus-5-5", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	// Sonnet 5.5 turns thinking off with between_tools, which takes effort
	// high or below only.
	{prefix: "claude-sonnet-5-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true},
	// Opus 5 and Haiku 5.x reject disabled thinking with xhigh or max.
	{prefix: "claude-opus-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true},
	{prefix: "claude-haiku-5", supportsEffort: true, supportsXHigh: true, supportsMax: true, offAtMostHigh: true},
	{prefix: "claude-sonnet-5", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	{prefix: "claude-opus-4-8", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	{prefix: "claude-opus-4-7", supportsEffort: true, supportsXHigh: true, supportsMax: true},
	{prefix: "claude-opus-4-6", supportsEffort: true, supportsMax: true},
	{prefix: "claude-sonnet-4-6", supportsEffort: true, supportsMax: true},
	{prefix: "claude-opus-4-5", supportsEffort: true},
	// Known models that take no effort.
	{prefix: "claude-sonnet-4-5"},
	{prefix: "claude-haiku-4-5"},
	{prefix: "claude-opus-4-1"},
	{prefix: "claude-opus-4-"},
	{prefix: "claude-sonnet-4-"},
	{prefix: "claude-3-7-sonnet"},
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

// ClaudeOutputConfigForEffort builds an output_config carrying the effort the
// model accepts. It returns nil when the model takes none.
func ClaudeOutputConfigForEffort(model, effort string) json.RawMessage {
	effort = ClaudeEffort(model, effort)
	if effort == "" {
		return nil
	}
	raw, err := common.Marshal(map[string]string{"effort": effort})
	if err != nil {
		return nil
	}
	return raw
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
