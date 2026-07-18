package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

func normalizeOpenAIWSTerminalEvent(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "response.completed":
		return "response.completed"
	case "response.done":
		return "response.done"
	case "response.failed":
		return "response.failed"
	case "response.incomplete":
		return "response.incomplete"
	case "response.cancelled", "response.canceled":
		return "response.cancelled"
	default:
		return ""
	}
}

func openAIWSPayloadTransientStatus(payload []byte) int {
	if len(payload) == 0 {
		return 0
	}
	status := int(gjson.GetBytes(payload, "response.error.status_code").Int())
	if status == 0 {
		status = int(gjson.GetBytes(payload, "response.error.status").Int())
	}
	if status == 0 {
		status = int(gjson.GetBytes(payload, "error.status_code").Int())
	}
	if status == 0 {
		status = int(gjson.GetBytes(payload, "error.status").Int())
	}
	if shouldCooldownOpenAITransientUpstreamError(status, payload) {
		return status
	}
	if status != 0 {
		return 0
	}
	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "response.error.code").String()))
	errType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "response.error.type").String()))
	if code == "" {
		code = strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.code").String()))
	}
	if errType == "" {
		errType = strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.type").String()))
	}
	switch {
	case code == "server_is_overloaded", code == "slow_down":
		return http.StatusServiceUnavailable
	case strings.Contains(code, "server_error"),
		strings.Contains(code, "internal_error"),
		strings.Contains(code, "upstream_error"),
		strings.Contains(errType, "server_error"),
		strings.Contains(errType, "internal_error"),
		strings.Contains(errType, "upstream_error"):
		return http.StatusInternalServerError
	default:
		return 0
	}
}

func (s *OpenAIGatewayService) handleOpenAIWSTerminalTransientFailure(ctx context.Context, account *Account, canonicalModel string, headers http.Header, payload []byte) string {
	eventType, _, _ := parseOpenAIWSEventEnvelope(payload)
	terminalEvent := normalizeOpenAIWSTerminalEvent(eventType)
	if terminalEvent != "response.failed" {
		return terminalEvent
	}
	if status := openAIWSPayloadTransientStatus(payload); status != 0 {
		s.handleOpenAIAccountUpstreamError(ctx, account, status, headers, payload, canonicalModel)
	}
	return terminalEvent
}

func (s *OpenAIGatewayService) handleOpenAIWSErrorEventTransientFailure(ctx context.Context, account *Account, canonicalModel string, headers http.Header, payload []byte) {
	eventType, _, _ := parseOpenAIWSEventEnvelope(payload)
	if eventType != "error" {
		return
	}
	if status := openAIWSPayloadTransientStatus(payload); status != 0 {
		s.handleOpenAIAccountUpstreamError(ctx, account, status, headers, payload, canonicalModel)
	}
}

func (s *OpenAIGatewayService) handleOpenAIWSDialTransientFailure(ctx context.Context, account *Account, canonicalModel string, err error) {
	var dialErr *openAIWSDialError
	if !errors.As(err, &dialErr) || dialErr == nil || !shouldCooldownOpenAITransientUpstreamError(dialErr.StatusCode, dialErr.ResponseBody) {
		return
	}
	s.handleOpenAIAccountUpstreamError(ctx, account, dialErr.StatusCode, dialErr.ResponseHeaders, dialErr.ResponseBody, canonicalModel)
}
