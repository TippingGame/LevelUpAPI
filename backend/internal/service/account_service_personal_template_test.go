package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyOwnedPersonalAccountTemplateIncludesOpenAISnapshotModel(t *testing.T) {
	credentials, extra := applyOwnedPersonalAccountTemplateToMaps(
		PlatformOpenAI,
		map[string]any{
			"access_token": "token",
			"compact_model_mapping": map[string]any{
				"gpt-5.4": "gpt-5.4-openai-compact",
			},
		},
		map[string]any{
			"openai_passthrough": true,
		},
	)

	mapping, ok := credentials["model_mapping"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "gpt-5.4-2026-03-05", mapping["gpt-5.4-2026-03-05"])
	require.NotContains(t, credentials, "compact_model_mapping")
	require.Equal(t, false, extra["openai_passthrough"])
}
