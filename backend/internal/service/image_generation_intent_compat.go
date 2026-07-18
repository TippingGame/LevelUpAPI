package service

import "encoding/json"

// cloneRequestMapForImageIntent preserves the map-based LevelUp call path
// while the upstream hot path reads billing metadata directly from raw JSON.
func cloneRequestMapForImageIntent(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func resolveOpenAIResponsesImageBillingConfig(reqBody map[string]any, fallbackModel string) (string, string, error) {
	cfg, err := resolveOpenAIResponsesImageBillingConfigDetailed(reqBody, fallbackModel)
	if err != nil {
		return "", "", err
	}
	return cfg.Model, cfg.SizeTier, nil
}
