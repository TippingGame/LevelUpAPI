package domain

import "testing"

func TestDefaultAntigravityModelMapping_IncludesImageCompatibilityAliases(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"gemini-2.5-flash-image":         "gemini-2.5-flash-image",
		"gemini-2.5-flash-image-preview": "gemini-2.5-flash-image",
		"gemini-3.1-flash-image":         "gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
		"gemini-3-pro-image":             "gemini-3.1-flash-image",
		"gemini-3-pro-image-preview":     "gemini-3.1-flash-image",
	}

	for model, want := range expected {
		got, ok := DefaultAntigravityModelMapping[model]
		if !ok {
			t.Fatalf("expected image generation model %q in default mapping", model)
		}
		if got != want {
			t.Fatalf("DefaultAntigravityModelMapping[%q] = %q, want %q", model, got, want)
		}
	}
}

func TestDefaultAntigravityModelMapping_IncludesOpus48(t *testing.T) {
	t.Parallel()

	got, ok := DefaultAntigravityModelMapping["claude-opus-4-8"]
	if !ok {
		t.Fatal("expected claude-opus-4-8 in DefaultAntigravityModelMapping")
	}
	if got != "claude-opus-4-8" {
		t.Fatalf("DefaultAntigravityModelMapping[claude-opus-4-8] = %q, want claude-opus-4-8", got)
	}
}

func TestDefaultAntigravityModelMapping_IncludesFable5(t *testing.T) {
	t.Parallel()

	got, ok := DefaultAntigravityModelMapping["claude-fable-5"]
	if !ok {
		t.Fatal("expected claude-fable-5 in DefaultAntigravityModelMapping")
	}
	if got != "claude-fable-5" {
		t.Fatalf("DefaultAntigravityModelMapping[claude-fable-5] = %q, want claude-fable-5", got)
	}
}

func TestDefaultBedrockModelMapping_IncludesSonnet5(t *testing.T) {
	t.Parallel()

	got, ok := DefaultBedrockModelMapping["claude-sonnet-5"]
	if !ok {
		t.Fatal("expected claude-sonnet-5 in DefaultBedrockModelMapping")
	}
	if got != "us.anthropic.claude-sonnet-5-v1" {
		t.Fatalf("DefaultBedrockModelMapping[claude-sonnet-5] = %q, want us.anthropic.claude-sonnet-5-v1", got)
	}
}
