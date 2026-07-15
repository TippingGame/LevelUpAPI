// Package gemini provides minimal fallback model metadata for Gemini native endpoints.
// It is used when upstream model listing is unavailable (e.g. OAuth token missing AI Studio scopes).
package gemini

import (
	"encoding/json"
	"strings"
)

type Model struct {
	Name                       string   `json:"name"`
	DisplayName                string   `json:"displayName,omitempty"`
	Description                string   `json:"description,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
}

type ModelsListResponse struct {
	Models []Model `json:"models"`
}

func DefaultModels() []Model {
	methods := []string{"generateContent", "streamGenerateContent"}
	return []Model{
		{Name: "models/gemini-2.0-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-2.5-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-2.5-flash-image", SupportedGenerationMethods: methods},
		{Name: "models/gemini-2.5-pro", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.5-flash", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-flash-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-flash-image", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3-pro-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-pro-preview", SupportedGenerationMethods: methods},
		{Name: "models/gemini-3.1-pro-preview-customtools", SupportedGenerationMethods: methods},
	}
}

func HasFallbackModel(model string) bool {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return false
	}
	if !strings.HasPrefix(trimmed, "models/") {
		trimmed = "models/" + trimmed
	}
	for _, model := range DefaultModels() {
		if model.Name == trimmed {
			return true
		}
	}
	return false
}

func FallbackModelsList() ModelsListResponse {
	return ModelsListResponse{Models: DefaultModels()}
}

// MergeFallbackModelsListJSON keeps upstream model metadata intact while
// ensuring models supported locally are visible to clients. Google's model
// catalog can lag behind a newly available model even when the credential can
// already invoke it, so blindly proxying a successful but stale list hides
// supported models from SDK and UI selectors.
func MergeFallbackModelsListJSON(body []byte) ([]byte, bool) {
	var payload struct {
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Models == nil {
		return body, false
	}

	existing := make(map[string]struct{}, len(payload.Models))
	for _, rawModel := range payload.Models {
		var model struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(rawModel, &model); err == nil {
			existing[strings.TrimSpace(model.Name)] = struct{}{}
		}
	}

	changed := false
	for _, fallback := range DefaultModels() {
		if _, ok := existing[fallback.Name]; ok {
			continue
		}
		rawModel, err := json.Marshal(fallback)
		if err != nil {
			return body, false
		}
		payload.Models = append(payload.Models, rawModel)
		existing[fallback.Name] = struct{}{}
		changed = true
	}
	if !changed {
		return body, false
	}

	// Preserve top-level fields such as nextPageToken while replacing only the
	// models array.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, false
	}
	modelsJSON, err := json.Marshal(payload.Models)
	if err != nil {
		return body, false
	}
	root["models"] = modelsJSON
	merged, err := json.Marshal(root)
	if err != nil {
		return body, false
	}
	return merged, true
}

func FallbackModel(model string) Model {
	methods := []string{"generateContent", "streamGenerateContent"}
	if model == "" {
		return Model{Name: "models/unknown", SupportedGenerationMethods: methods}
	}
	if len(model) >= 7 && model[:7] == "models/" {
		return Model{Name: model, SupportedGenerationMethods: methods}
	}
	return Model{Name: "models/" + model, SupportedGenerationMethods: methods}
}
