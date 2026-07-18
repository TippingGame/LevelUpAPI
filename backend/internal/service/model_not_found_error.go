package service

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// Antigravity must distinguish a missing model from missing endpoints/projects.
func isModelNotFoundError(statusCode int, body []byte) bool {
	if isUpstreamModelNotFoundError(statusCode, body) {
		return true
	}
	// Preserve Antigravity's legacy fallback for bare non-JSON 404 responses;
	// structured 404 payloads still need model-specific evidence.
	return statusCode == http.StatusNotFound && len(bytes.TrimSpace(body)) > 0 && !json.Valid(body)
}
