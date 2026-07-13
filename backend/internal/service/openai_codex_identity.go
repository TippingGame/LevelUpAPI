package service

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const codexUpstreamMinVersion = "0.144.0"

func ensureCodexIdentityHeaders(h http.Header) {
	if h == nil {
		return
	}
	if strings.TrimSpace(h.Get("User-Agent")) == "" {
		h.Set("User-Agent", codexCLIUserAgent)
	}
	if strings.TrimSpace(h.Get("Originator")) == "" {
		h.Set("Originator", "codex_cli_rs")
	}
	if strings.TrimSpace(h.Get("Version")) == "" {
		h.Set("Version", codexCLIVersion)
	}
	h.Set("OpenAI-Beta", "responses=experimental")
}

func enforceCodexIdentityHeaders(h http.Header) {
	if h == nil || strings.TrimSpace(h.Get("Originator")) == "" {
		return
	}
	originator, pairedUA, ok := openai.PairCodexClientIdentity(h.Get("User-Agent"))
	if !ok {
		originator, pairedUA = "codex_cli_rs", codexCLIUserAgent
	}
	h.Set("User-Agent", pairedUA)
	h.Set("Originator", originator)
	if version := strings.TrimSpace(h.Get("Version")); version != "" && CompareVersions(version, codexUpstreamMinVersion) < 0 {
		h.Set("Version", codexCLIVersion)
	}
}
