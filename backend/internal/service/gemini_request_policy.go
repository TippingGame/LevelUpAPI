package service

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func isGeminiRequestPolicyError(statusCode int, payload []byte, upstreamMsg string) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusUnprocessableEntity:
	default:
		return false
	}

	if _, permanent := permanentAccountKeywordErrorMessage(
		&Account{Platform: PlatformGemini, Type: AccountTypeAPIKey},
		statusCode,
		upstreamMsg,
		payload,
	); permanent {
		return false
	}

	if isGoogleProjectConfigError(normalizePolicyText(upstreamMsg + " " + string(payload))) {
		return false
	}

	status, code, msg := geminiErrorPolicyFields(payload)
	tokens := []string{
		normalizeGeminiPolicyToken(status),
		normalizeGeminiPolicyToken(code),
	}
	tokens = append(tokens, geminiErrorPolicyReasonTokens(payload)...)

	for _, token := range tokens {
		switch token {
		case "safety",
			"safety_error",
			"safety_filter",
			"safety_policy",
			"prohibited_content",
			"blocked_reason_safety",
			"blocklist",
			"recitation",
			"responsible_ai",
			"rai",
			"spi",
			"sensitive_personal_information":
			return true
		}
	}

	combinedMsg := normalizePolicyText(msg + " " + upstreamMsg)
	if combinedMsg == "" {
		return false
	}
	for _, marker := range []string{
		"blocked due to safety",
		"blocked by safety",
		"blocked for safety",
		"prompt was blocked",
		"candidate was blocked",
		"response was blocked",
		"safety filter",
		"safety policy",
		"prohibited content",
		"responsible ai",
		"recitation",
		"blocklist",
	} {
		if strings.Contains(combinedMsg, marker) {
			return true
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

func geminiErrorPolicyFields(payload []byte) (status string, code string, msg string) {
	if len(payload) == 0 {
		return "", "", ""
	}
	status = firstNonEmptyTrimmed(
		gjson.GetBytes(payload, "error.status").String(),
		gjson.GetBytes(payload, "response.error.status").String(),
		gjson.GetBytes(payload, "status").String(),
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
	return status, code, msg
}

func geminiErrorPolicyReasonTokens(payload []byte) []string {
	tokens := make([]string, 0, 8)
	for _, path := range []string{"error.details", "response.error.details", "details"} {
		gjson.GetBytes(payload, path).ForEach(func(_, detail gjson.Result) bool {
			for _, reasonPath := range []string{"reason", "metadata.reason", "metadata.blockedReason"} {
				if token := normalizeGeminiPolicyToken(detail.Get(reasonPath).String()); token != "" {
					tokens = append(tokens, token)
				}
			}
			return true
		})
	}
	return tokens
}

func normalizeGeminiPolicyToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	return strings.Trim(value, "_")
}

func normalizePolicyText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", " ", "-", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}
