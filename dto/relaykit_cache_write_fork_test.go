package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	rdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestRelaykitInputTokenDetailsCacheCreationTokensTotal(t *testing.T) {
	cases := []struct {
		name     string
		details  rdto.InputTokenDetails
		expected int
	}{
		{"claude cache creation only", rdto.InputTokenDetails{CachedCreationTokens: 50}, 50},
		{"openai cache write only", rdto.InputTokenDetails{CacheWriteTokens: 80}, 80},
		{"both reported, larger wins", rdto.InputTokenDetails{CachedCreationTokens: 20, CacheWriteTokens: 80}, 80},
		{"neither reported", rdto.InputTokenDetails{}, 0},
		{"negative values clamp to zero", rdto.InputTokenDetails{CachedCreationTokens: -5, CacheWriteTokens: -9}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.details.CacheCreationTokensTotal())
		})
	}
}

func TestRelaykitUsageUnmarshalsOpenAICacheWriteTokens(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 4583,
		"completion_tokens": 15,
		"total_tokens": 4598,
		"prompt_tokens_details": {
			"cached_tokens": 3945,
			"cache_write_tokens": 4580
		}
	}`)

	var usage rdto.Usage
	require.NoError(t, common.Unmarshal(raw, &usage))
	require.Equal(t, 3945, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 4580, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 4580, usage.PromptTokensDetails.CacheCreationTokensTotal())
}
