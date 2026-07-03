package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func newSSEStreamErrorEventError(rawData string) *sseStreamErrorEventError {
	body := normalizeAnthropicStreamErrorBody(rawData)
	statusCode, message := anthropicStreamErrorStatusAndMessage(body)
	return &sseStreamErrorEventError{
		RawData:      rawData,
		StatusCode:   statusCode,
		ResponseBody: body,
		Message:      message,
	}
}

func normalizeAnthropicStreamErrorBody(rawData string) []byte {
	rawData = strings.TrimSpace(rawData)
	if rawData == "" || !gjson.Valid(rawData) {
		body, err := json.Marshal(map[string]any{
			"type": "error",
			"error": map[string]string{
				"type":    "upstream_error",
				"message": firstNonEmptyTrimmed(rawData, "Anthropic stream error"),
			},
		})
		if err == nil {
			return body
		}
		return []byte(`{"type":"error","error":{"type":"upstream_error","message":"Anthropic stream error"}}`)
	}
	return []byte(rawData)
}

func anthropicStreamErrorStatusAndMessage(body []byte) (int, string) {
	errType := firstNonEmptyTrimmed(
		gjson.GetBytes(body, "error.type").String(),
		gjson.GetBytes(body, "type").String(),
	)
	code := firstNonEmptyTrimmed(
		gjson.GetBytes(body, "error.code").String(),
		gjson.GetBytes(body, "code").String(),
	)
	message := firstNonEmptyTrimmed(
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "message").String(),
	)

	combined := normalizeLooseErrorText(strings.Join([]string{errType, code, message, string(body)}, " "))
	errType = strings.ToLower(strings.TrimSpace(errType))
	code = strings.ToLower(strings.TrimSpace(code))

	switch {
	case errType == "rate_limit_error" ||
		strings.Contains(code, "rate_limit") ||
		strings.Contains(combined, "rate limit"):
		return http.StatusTooManyRequests, message
	case errType == "overloaded_error" ||
		strings.Contains(combined, "overloaded") ||
		strings.Contains(combined, "overload"):
		return 529, message
	case errType == "authentication_error" ||
		strings.Contains(errType, "auth") ||
		strings.Contains(code, "invalid api key") ||
		strings.Contains(code, "invalid token") ||
		strings.Contains(combined, "unauthorized"):
		return http.StatusUnauthorized, message
	case errType == "permission_error" ||
		strings.Contains(errType, "permission") ||
		strings.Contains(code, "forbidden") ||
		strings.Contains(code, "access_denied") ||
		strings.Contains(combined, "permission denied") ||
		strings.Contains(combined, "access denied") ||
		strings.Contains(combined, "forbidden"):
		return http.StatusForbidden, message
	case errType == "billing_error" ||
		errType == "billing error" ||
		isRecoverableBillingQuotaText(combined):
		return http.StatusPaymentRequired, message
	case errType == "not_found_error" ||
		strings.Contains(code, "not_found") ||
		strings.Contains(combined, "not found"):
		return http.StatusNotFound, message
	case errType == "request_too_large" ||
		strings.Contains(combined, "request too large"):
		return http.StatusRequestEntityTooLarge, message
	case errType == "safety_error" ||
		strings.Contains(code, "safety") ||
		strings.Contains(code, "content_policy") ||
		strings.Contains(combined, "content policy") ||
		strings.Contains(combined, "safety policy") ||
		strings.Contains(combined, "high risk cyber"):
		return http.StatusBadRequest, message
	case errType == "timeout_error" ||
		strings.Contains(combined, "timed out") ||
		strings.Contains(combined, "timeout"):
		return http.StatusGatewayTimeout, message
	case errType == "api_error":
		return http.StatusInternalServerError, message
	case errType == "invalid_request_error":
		return http.StatusBadRequest, message
	default:
		return http.StatusBadGateway, message
	}
}
