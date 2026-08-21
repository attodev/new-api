package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestInputTokenDetailsCacheCreationTokensTotal(t *testing.T) {
	cases := []struct {
		name     string
		details  InputTokenDetails
		expected int
	}{
		{
			name:     "claude cache creation only",
			details:  InputTokenDetails{CachedCreationTokens: 50},
			expected: 50,
		},
		{
			name:     "openai gpt-5.6 cache write only",
			details:  InputTokenDetails{CacheWriteTokens: 80},
			expected: 80,
		},
		{
			name:     "both reported, larger wins",
			details:  InputTokenDetails{CachedCreationTokens: 20, CacheWriteTokens: 80},
			expected: 80,
		},
		{
			name:     "neither reported",
			details:  InputTokenDetails{},
			expected: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.details.CacheCreationTokensTotal())
		})
	}
}

func TestUsageUnmarshalsOpenAICacheWriteTokens(t *testing.T) {
	raw := []byte(`{
		"prompt_tokens": 4583,
		"completion_tokens": 15,
		"total_tokens": 4598,
		"prompt_tokens_details": {
			"cached_tokens": 3945,
			"cache_write_tokens": 4580
		}
	}`)

	var usage Usage
	err := common.Unmarshal(raw, &usage)
	require.NoError(t, err)
	require.Equal(t, 3945, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 4580, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 4580, usage.PromptTokensDetails.CacheCreationTokensTotal())
}
