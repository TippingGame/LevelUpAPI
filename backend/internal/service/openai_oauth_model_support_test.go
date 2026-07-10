//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthEmptyMappingRejectsForeignModelFamilies(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	for _, model := range []string{
		"deepseek-v4",
		"provider/GLM-4.7",
		"kimi-k2",
		"gemini-3.1-pro",
		"grok-4.5",
		"qwen3-max",
		"minimax-m2.5",
	} {
		require.False(t, account.IsModelSupported(model), model)
	}

	for _, model := range []string{"gpt-5.6", "claude-sonnet-4-6", "my-channel-alias"} {
		require.True(t, account.IsModelSupported(model), model)
	}
}

func TestOpenAIOAuthModelFilteringKeepsExplicitRoutingSemantics(t *testing.T) {
	oauth := &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"deepseek-v4": "gpt-5.6-terra"},
		},
	}
	require.True(t, oauth.IsModelSupported("deepseek-v4"))
	require.False(t, oauth.IsModelSupported("glm-4.7"))

	oauth.Credentials = nil
	oauth.Extra = map[string]any{"openai_passthrough": true}
	require.True(t, oauth.IsModelSupported("deepseek-v4"))

	apiKey := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	require.True(t, apiKey.IsModelSupported("deepseek-v4"))
}
