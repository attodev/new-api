package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

// Ported from team 2fa3012ec: Tencent's OpenAI-compatible path supports
// stream_options.include_usage, so it must be registered per CLAUDE.md Rule 4.
func TestStreamSupportedChannelsIncludesTencent(t *testing.T) {
	if !streamSupportedChannels[constant.ChannelTypeTencent] {
		t.Errorf("streamSupportedChannels[ChannelTypeTencent] = false, want true")
	}
}
