// Package openai provides helpers and types for OpenAI API integration.
package openai

import (
	_ "embed"
	"strings"
)

// Model represents an OpenAI model
type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
}

// DefaultModels OpenAI models list
var DefaultModels = []Model{
	{ID: "gpt-5.6", Object: "model", Created: 1780876800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.6 (Sol)"},
	{ID: "gpt-5.6-sol", Object: "model", Created: 1780876800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.6 Sol"},
	{ID: "gpt-5.6-terra", Object: "model", Created: 1780876800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.6 Terra"},
	{ID: "gpt-5.6-luna", Object: "model", Created: 1780876800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.6 Luna"},
	{ID: "gpt-5.5", Object: "model", Created: 1776873600, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.5"},
	{ID: "gpt-5.5-pro", Object: "model", Created: 1776902400, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.5 Pro"},
	{ID: "gpt-5.4", Object: "model", Created: 1738368000, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.4"},
	{ID: "gpt-5.4-pro", Object: "model", Created: 1772668800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.4 Pro"},
	{ID: "gpt-5.4-2026-03-05", Object: "model", Created: 1772668800, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.4 (2026-03-05)"},
	{ID: "gpt-5.4-mini", Object: "model", Created: 1738368000, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.4 Mini"},
	{ID: "gpt-5.3-codex", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.3 Codex"},
	{ID: "gpt-5.3-codex-spark", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.3 Codex Spark"},
	{ID: "codex-auto-review", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "Codex Auto Review"},
	{ID: "gpt-5.2", Object: "model", Created: 1733875200, OwnedBy: "openai", Type: "model", DisplayName: "GPT-5.2"},
	{ID: "gpt-image-1", Object: "model", Created: 1733875200, OwnedBy: "openai", Type: "model", DisplayName: "GPT Image 1"},
	{ID: "gpt-image-1.5", Object: "model", Created: 1735689600, OwnedBy: "openai", Type: "model", DisplayName: "GPT Image 1.5"},
	{ID: "gpt-image-2", Object: "model", Created: 1738368000, OwnedBy: "openai", Type: "model", DisplayName: "GPT Image 2"},
	{ID: "gpt-realtime-1.5", Object: "model", Created: 1772668800, OwnedBy: "openai", Type: "model", DisplayName: "GPT Realtime 1.5"},
	{ID: "gpt-realtime-2", Object: "model", Created: 1776873600, OwnedBy: "openai", Type: "model", DisplayName: "GPT Realtime 2"},
}

// DefaultModelIDs returns the default model ID list
func DefaultModelIDs() []string {
	ids := make([]string, len(DefaultModels))
	for i, m := range DefaultModels {
		ids[i] = m.ID
	}
	return ids
}

// DefaultTestModel is the default model for ordinary OpenAI connection tests,
// public-share validation, and scheduled background tests.
const DefaultTestModel = "gpt-5.5"

// DefaultPlusVerificationModel is used only to verify that a user-owned OpenAI
// OAuth account can access Plus-only capability before upgrading it to Plus.
const DefaultPlusVerificationModel = "gpt-5.4"

// DefaultInstructions default instructions for non-Codex CLI requests
// Content loaded from instructions.txt at compile time
//
//go:embed instructions.txt
var DefaultInstructions string

// instructionsGPT51 / instructionsGPT52 / instructionsGPT55 are the real
// Codex coding-agent base prompts used by the matching non-Codex model family.
// GPT-5.5 is also the fallback for model families without a dedicated prompt.
//
//go:embed instructions_gpt5_1.txt
var instructionsGPT51 string

//go:embed instructions_gpt5_2.txt
var instructionsGPT52 string

//go:embed instructions_gpt5_5.txt
var instructionsGPT55 string

func latestCodexInstructions() string {
	if instructions := strings.TrimSpace(instructionsGPT55); instructions != "" {
		return instructionsGPT55
	}
	return DefaultInstructions
}

// CodexBaseInstructionsForModel returns the closest real Codex base prompt for
// the requested model while always retaining a non-empty fallback chain.
func CodexBaseInstructionsForModel(model string) string {
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(normalized, "codex"):
		return DefaultInstructions
	case strings.HasPrefix(normalized, "gpt-5.5"):
		return latestCodexInstructions()
	case strings.HasPrefix(normalized, "gpt-5.2"):
		if instructions := strings.TrimSpace(instructionsGPT52); instructions != "" {
			return instructionsGPT52
		}
	case strings.HasPrefix(normalized, "gpt-5.1"):
		if instructions := strings.TrimSpace(instructionsGPT51); instructions != "" {
			return instructionsGPT51
		}
	}
	return latestCodexInstructions()
}
