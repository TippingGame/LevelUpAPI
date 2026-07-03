package service

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func isAnthropicRequestPolicyError(statusCode int, payload []byte, upstreamMsg string) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity:
	default:
		return false
	}

	if _, permanent := permanentAccountKeywordErrorMessage(
		&Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
		statusCode,
		upstreamMsg,
		payload,
	); permanent {
		return false
	}

	errType, code, msg := anthropicErrorPolicyFields(payload)
	normalizedType := normalizeAnthropicPolicyToken(errType)
	normalizedCode := normalizeAnthropicPolicyToken(code)
	switch normalizedType {
	case "safety_error", "content_filter", "content_policy", "content_policy_error", "policy_violation", "safety_violation":
		return true
	}
	switch normalizedCode {
	case "safety_error", "content_filter", "content_policy", "content_policy_violation", "policy_violation", "safety_violation":
		return true
	}

	combinedMsg := strings.ToLower(strings.TrimSpace(msg + " " + upstreamMsg))
	combinedMsg = strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(combinedMsg)), " ")
	if combinedMsg == "" {
		return false
	}

	for _, marker := range []string{
		"content policy",
		"high risk cyber",
		"high-risk cyber",
		"safety policy",
		"safety system",
		"safety systems",
		"unsafe content",
		"disallowed content",
		"blocked by safety",
		"blocked for safety",
	} {
		if strings.Contains(combinedMsg, marker) {
			return true
		}
	}

	if strings.Contains(combinedMsg, "usage policy") || strings.Contains(combinedMsg, "acceptable use policy") {
		for _, requestMarker := range []string{"request", "prompt", "message", "content", "input"} {
			if strings.Contains(combinedMsg, requestMarker) {
				return true
			}
		}
	}

	if strings.Contains(combinedMsg, "violates") {
		for _, requestMarker := range []string{"request", "prompt", "message", "content", "input"} {
			if strings.Contains(combinedMsg, requestMarker) &&
				(strings.Contains(combinedMsg, "policy") || strings.Contains(combinedMsg, "safety")) {
				return true
			}
		}
	}

	return false
}

func anthropicErrorPolicyFields(payload []byte) (errType string, code string, msg string) {
	if len(payload) == 0 {
		return "", "", ""
	}
	errType = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.type").String(),
		gjson.GetBytes(payload, "response.error.type").String(),
		gjson.GetBytes(payload, "type").String(),
	)
	code = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.code").String(),
		gjson.GetBytes(payload, "response.error.code").String(),
		gjson.GetBytes(payload, "code").String(),
	)
	msg = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.message").String(),
		gjson.GetBytes(payload, "response.error.message").String(),
		gjson.GetBytes(payload, "message").String(),
	)
	return errType, code, msg
}

func normalizeAnthropicPolicyToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	return strings.Trim(value, "_")
}
