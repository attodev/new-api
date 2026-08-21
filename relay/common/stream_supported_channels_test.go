package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
)

// TestStreamSupportedChannels_Tencent reproduces a real gap: Tencent gained
// an OpenAI-compatible protocol path (TokenHub keys), which supports the
// standard stream_options.include_usage field, but the channel was never
// added to streamSupportedChannels - so InitChannelMeta would leave
// SupportStreamOptions false for Tencent even on the OpenAI-compatible path,
// per CLAUDE.md Rule 4 (new channels with StreamOptions support must be
// registered here).
func TestStreamSupportedChannels_Tencent(t *testing.T) {
	if !streamSupportedChannels[constant.ChannelTypeTencent] {
		t.Errorf("streamSupportedChannels[ChannelTypeTencent] = false, want true")
	}
}
