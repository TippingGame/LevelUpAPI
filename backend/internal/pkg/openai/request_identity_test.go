package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPairCodexClientIdentity(t *testing.T) {
	tests := []struct {
		name       string
		ua         string
		originator string
		pairedUA   string
		ok         bool
	}{
		{name: "cli", ua: "codex_cli_rs/0.144.1 (Linux; x86_64)", originator: "codex_cli_rs", pairedUA: "codex_cli_rs/0.144.1 (Linux; x86_64)", ok: true},
		{name: "tui", ua: "codex-tui/0.144.1 (Mac OS X; arm64)", originator: "codex-tui", pairedUA: "codex-tui/0.144.1 (Mac OS X; arm64)", ok: true},
		{name: "desktop family", ua: "Codex Desktop/1.2.3", originator: "Codex Desktop", pairedUA: "Codex Desktop/1.2.3", ok: true},
		{name: "override recovered from trailer", ua: "cccc/0.144.1 (Linux; x86_64) xterm (codex-tui; 0.144.1)", originator: "codex-tui", pairedUA: "codex-tui/0.144.1 (Linux; x86_64) xterm (codex-tui; 0.144.1)", ok: true},
		{name: "third party rejected", ua: "luna/1.2.0", ok: false},
		{name: "missing version separator", ua: "codex_cli_rs", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originator, pairedUA, ok := PairCodexClientIdentity(tt.ua)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.originator, originator)
			require.Equal(t, tt.pairedUA, pairedUA)
		})
	}
}
