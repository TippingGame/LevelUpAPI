//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

func TestCreateTestPayload_IncludesClaudeCodeAttributionBlocks(t *testing.T) {
	t.Parallel()

	payload, err := createTestPayload("claude-opus-4-8")
	require.NoError(t, err)

	system, ok := payload["system"].([]map[string]any)
	require.True(t, ok, "system should be Claude Code-style text blocks")
	require.Len(t, system, 3)

	billingBlock := system[0]
	require.Equal(t, "text", billingBlock["type"])
	billingText, ok := billingBlock["text"].(string)
	require.True(t, ok)
	require.Contains(t, billingText, "x-anthropic-billing-header:")
	require.Contains(t, billingText, "cc_version="+claude.CLICurrentVersion+".")
	require.Contains(t, billingText, "cc_entrypoint=cli")
	require.NotContains(t, billingText, "cch=")
	require.NotContains(t, billingBlock, "cache_control")

	promptBlock := system[1]
	require.Equal(t, "text", promptBlock["type"])
	require.Equal(t, claudeCodeSystemPrompt, promptBlock["text"])

	expansionBlock := system[2]
	require.Equal(t, "text", expansionBlock["type"])
	require.Equal(t, claudeCodeSystemPromptExpansion, expansionBlock["text"])
	cacheControl, ok := expansionBlock["cache_control"].(map[string]any)
	require.True(t, ok, "expansion block should keep cache_control")
	require.Equal(t, "ephemeral", cacheControl["type"])
}
