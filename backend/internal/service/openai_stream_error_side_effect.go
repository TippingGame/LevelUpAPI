package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func (s *OpenAIGatewayService) handleOpenAIResponsesStreamErrorSideEffect(ctx context.Context, account *Account, headers http.Header, payload []byte, fallbackMessage string, includeCapacity bool) bool {
	if s == nil || s.rateLimitService == nil || account == nil || account.Platform != PlatformOpenAI || len(payload) == 0 {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	body := normalizeOpenAIResponsesStreamErrorBody(payload)
	code, errType, msg := parseOpenAIResponsesStreamErrorFields(body)
	if msg == "" {
		msg = strings.TrimSpace(fallbackMessage)
	}
	statusCode := openAIResponsesStreamErrorSideEffectStatus(code, errType, msg, body)
	if statusCode == 0 {
		if includeCapacity && isOpenAIModelCapacityError(http.StatusServiceUnavailable, strings.Join([]string{msg, code, errType}, " "), body) {
			return s.handleOpenAIModelCapacitySignal(ctx, account, http.StatusServiceUnavailable, headers, body, msg)
		}
		return false
	}
	return s.rateLimitService.HandleUpstreamError(ctx, account, statusCode, headers, body)
}

func normalizeOpenAIResponsesStreamErrorBody(payload []byte) []byte {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return payload
	}
	for _, path := range []string{"error", "response.error"} {
		result := gjson.GetBytes(payload, path)
		if !result.Exists() || strings.TrimSpace(result.Raw) == "" {
			continue
		}
		body, err := json.Marshal(map[string]json.RawMessage{
			"error": json.RawMessage(result.Raw),
		})
		if err == nil && len(body) > 0 {
			return body
		}
	}
	return payload
}

func parseOpenAIResponsesStreamErrorFields(body []byte) (code string, errType string, message string) {
	if len(body) == 0 {
		return "", "", ""
	}
	values := gjson.GetManyBytes(body,
		"error.code",
		"error.type",
		"error.message",
		"response.error.code",
		"response.error.type",
		"response.error.message",
	)
	code = firstNonEmptyTrimmed(values[0].String(), values[3].String())
	errType = firstNonEmptyTrimmed(values[1].String(), values[4].String())
	message = firstNonEmptyTrimmed(values[2].String(), values[5].String())
	return code, errType, message
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func openAIResponsesStreamErrorSideEffectStatus(codeRaw, errTypeRaw, msgRaw string, body []byte) int {
	combined := normalizeLooseErrorText(strings.Join([]string{codeRaw, errTypeRaw, msgRaw, string(body)}, " "))
	code := strings.ToLower(strings.TrimSpace(codeRaw))
	errType := strings.ToLower(strings.TrimSpace(errTypeRaw))

	if isOpenAIWSRateLimitError(codeRaw, errTypeRaw, msgRaw) ||
		strings.Contains(combined, "insufficient quota") ||
		(strings.Contains(combined, "usage limit") && strings.Contains(combined, "reached")) {
		return http.StatusTooManyRequests
	}
	if isRecoverableBillingQuotaText(combined) {
		return http.StatusPaymentRequired
	}
	if strings.Contains(errType, "auth") ||
		strings.Contains(code, "invalid_api_key") ||
		strings.Contains(code, "invalid token") ||
		strings.Contains(code, "unauthorized") ||
		strings.Contains(combined, "unauthorized") {
		return http.StatusUnauthorized
	}
	if strings.Contains(errType, "permission") ||
		strings.Contains(errType, "access") ||
		strings.Contains(code, "forbidden") ||
		strings.Contains(code, "access_denied") ||
		strings.Contains(code, "ip_blocked") ||
		strings.Contains(combined, "permission denied") ||
		strings.Contains(combined, "access denied") ||
		strings.Contains(combined, "ip blocked") {
		return http.StatusForbidden
	}
	return 0
}
