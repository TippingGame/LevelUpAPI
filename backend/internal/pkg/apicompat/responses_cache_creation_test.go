package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesUsageUnmarshalCacheCreationAliases(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "canonical", body: `{"input_tokens":20,"cache_creation_input_tokens":6}`, want: 6},
		{name: "top level write", body: `{"input_tokens":20,"cache_write_input_tokens":7}`, want: 7},
		{name: "nested responses write", body: `{"input_tokens":20,"input_tokens_details":{"cache_write_tokens":8}}`, want: 8},
		{name: "nested chat creation", body: `{"prompt_tokens":20,"prompt_tokens_details":{"cache_creation_tokens":9}}`, want: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var usage ResponsesUsage
			require.NoError(t, json.Unmarshal([]byte(tt.body), &usage))
			require.Equal(t, tt.want, usage.CacheCreationInputTokens)
		})
	}
}

func TestAnthropicResponsesCacheCreationRoundTrip(t *testing.T) {
	resp := &AnthropicResponse{
		Usage: AnthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     4,
			CacheCreationInputTokens: 6,
		},
	}

	responses := AnthropicToResponsesResponse(resp)
	require.NotNil(t, responses.Usage)
	require.Equal(t, 20, responses.Usage.InputTokens)
	require.Equal(t, 6, responses.Usage.CacheCreationInputTokens)

	usage := anthropicUsageFromResponsesUsage(responses.Usage)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 4, usage.CacheReadInputTokens)
	require.Equal(t, 6, usage.CacheCreationInputTokens)
}

func TestResponsesStreamingCacheCreationReachesAnthropicUsage(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	state.MessageStartSent = true
	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{Status: "completed", Usage: &ResponsesUsage{
			InputTokens:              20,
			OutputTokens:             5,
			CacheCreationInputTokens: 6,
			InputTokensDetails:       &ResponsesInputTokensDetails{CachedTokens: 4},
		}},
	}, state)

	for i := range events {
		if events[i].Type == "message_delta" {
			require.NotNil(t, events[i].Usage)
			require.Equal(t, 10, events[i].Usage.InputTokens)
			require.Equal(t, 4, events[i].Usage.CacheReadInputTokens)
			require.Equal(t, 6, events[i].Usage.CacheCreationInputTokens)
			return
		}
	}
	t.Fatal("message_delta event not found")
}
