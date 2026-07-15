package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModels_ContainsFallbackCatalogModels(t *testing.T) {
	t.Parallel()

	models := DefaultModels()
	byName := make(map[string]Model, len(models))
	for _, model := range models {
		byName[model.Name] = model
	}

	required := []string{
		"models/gemini-3.1-pro-preview",
		"models/gemini-3.1-pro-preview-customtools",
	}
	for _, name := range required {
		model, ok := byName[name]
		if !ok {
			t.Fatalf("expected fallback model %q to exist", name)
		}
		if len(model.SupportedGenerationMethods) == 0 {
			t.Fatalf("expected fallback model %q to advertise generation methods", name)
		}
	}

	imageModels := []string{
		"models/gemini-2.5-flash-image",
		"models/gemini-3.1-flash-image",
	}
	for _, name := range imageModels {
		model, ok := byName[name]
		if !ok {
			t.Fatalf("expected fallback image generation model %q to exist", name)
		}
		if len(model.SupportedGenerationMethods) == 0 {
			t.Fatalf("expected fallback image generation model %q to advertise generation methods", name)
		}
	}
}

func TestHasFallbackModel_RecognizesCustomtoolsModel(t *testing.T) {
	t.Parallel()

	if !HasFallbackModel("gemini-3.1-pro-preview-customtools") {
		t.Fatalf("expected customtools model to exist in fallback catalog")
	}
	if !HasFallbackModel("models/gemini-3.1-pro-preview-customtools") {
		t.Fatalf("expected prefixed customtools model to exist in fallback catalog")
	}
	if HasFallbackModel("gemini-unknown") {
		t.Fatalf("did not expect unknown model to exist in fallback catalog")
	}
}

func TestMergeFallbackModelsListJSON(t *testing.T) {
	t.Parallel()

	body := []byte(`{"models":[{"name":"models/gemini-2.5-pro","displayName":"Upstream metadata"}],"nextPageToken":"next"}`)
	merged, changed := MergeFallbackModelsListJSON(body)
	require.True(t, changed)

	var payload struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
		NextPageToken string `json:"nextPageToken"`
	}
	require.NoError(t, json.Unmarshal(merged, &payload))
	require.Equal(t, "next", payload.NextPageToken)

	seen := make(map[string]int, len(payload.Models))
	for _, model := range payload.Models {
		seen[model.Name]++
		if model.Name == "models/gemini-2.5-pro" {
			require.Equal(t, "Upstream metadata", model.DisplayName)
		}
	}
	require.Equal(t, 1, seen["models/gemini-2.5-pro"])
	require.Equal(t, 1, seen["models/gemini-3.1-pro-preview"])
	require.Equal(t, 1, seen["models/gemini-3.1-pro-preview-customtools"])
}

func TestMergeFallbackModelsListJSONLeavesInvalidOrCompletePayloadAlone(t *testing.T) {
	t.Parallel()

	invalid := []byte(`{"models":`)
	merged, changed := MergeFallbackModelsListJSON(invalid)
	require.False(t, changed)
	require.Equal(t, invalid, merged)

	completeJSON, err := json.Marshal(FallbackModelsList())
	require.NoError(t, err)
	merged, changed = MergeFallbackModelsListJSON(completeJSON)
	require.False(t, changed)
	require.Equal(t, completeJSON, merged)
}
