//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIUsageFromGJSONReadsCacheWriteAliases(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "canonical", body: `{"input_tokens":20,"cache_creation_input_tokens":6}`, want: 6},
		{name: "top level alias", body: `{"input_tokens":20,"cache_write_input_tokens":7}`, want: 7},
		{name: "responses details", body: `{"input_tokens":20,"input_tokens_details":{"cache_write_tokens":8}}`, want: 8},
		{name: "chat details", body: `{"prompt_tokens":20,"prompt_tokens_details":{"cache_creation_tokens":9}}`, want: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, ok := openAIUsageFromGJSON(gjson.Parse(tt.body))
			require.True(t, ok)
			require.Equal(t, tt.want, usage.CacheCreationInputTokens)
		})
	}
}

func TestOpenAIUsageTokensDoesNotDoubleBillCacheCreation(t *testing.T) {
	usage := OpenAIUsage{
		InputTokens:              100,
		TextInputTokens:          100,
		CacheReadInputTokens:     20,
		CacheCreationInputTokens: 30,
	}

	tokens, actualInput := openAIUsageTokens(usage)
	require.Equal(t, 50, actualInput)
	require.Equal(t, 50, tokens.TextInputTokens)
	require.Zero(t, tokens.InputTokens)
	require.Equal(t, 20, tokens.CacheReadTokens)
	require.Equal(t, 30, tokens.CacheCreationTokens)
}
