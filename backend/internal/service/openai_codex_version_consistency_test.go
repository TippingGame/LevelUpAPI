//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstantsConsistency(t *testing.T) {
	require.Equal(t, codexCLIVersion, openAICodexProbeVersion)
	require.True(t, strings.Contains(codexCLIUserAgent, "codex_cli_rs/"+codexCLIVersion))
}
