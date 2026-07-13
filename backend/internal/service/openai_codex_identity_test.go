//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureCodexIdentityHeaders(t *testing.T) {
	headers := make(http.Header)
	ensureCodexIdentityHeaders(headers)
	require.Equal(t, codexCLIUserAgent, headers.Get("User-Agent"))
	require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
	require.Equal(t, codexCLIVersion, headers.Get("Version"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
}

func TestEnforceCodexIdentityHeadersPairsFinalUserAgent(t *testing.T) {
	headers := make(http.Header)
	headers.Set("User-Agent", "codex-tui/0.144.1 (Mac OS X; arm64)")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("Version", "0.125.0")

	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex-tui", headers.Get("Originator"))
	require.Equal(t, "codex-tui/0.144.1 (Mac OS X; arm64)", headers.Get("User-Agent"))
	require.Equal(t, codexCLIVersion, headers.Get("Version"))
}

func TestEnforceCodexIdentityHeadersFallsBackForUnknownClient(t *testing.T) {
	headers := make(http.Header)
	headers.Set("User-Agent", "third-party/1.0")
	headers.Set("Originator", "third-party")

	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
	require.Equal(t, codexCLIUserAgent, headers.Get("User-Agent"))
}
