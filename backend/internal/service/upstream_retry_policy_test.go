package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamReplayUnsafeTimeoutStatusesDoNotFailover(t *testing.T) {
	statuses := []int{http.StatusRequestTimeout, http.StatusGatewayTimeout, cloudflareOriginTimeoutStatus}
	customRetryAccount := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusBadRequest)},
		},
	}

	for _, status := range statuses {
		require.True(t, IsUpstreamReplayUnsafeTimeoutStatus(status))

		require.False(t, (&GatewayService{}).shouldRetryUpstreamError(customRetryAccount, status))
		require.False(t, (&GatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, (&OpenAIGatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(status, "model is at capacity", []byte(`{"error":{"message":"model is at capacity"}}`)))
		require.False(t, shouldFailoverOpenAIPassthroughResponse(customRetryAccount, status, "model is at capacity", []byte(`{"error":{"message":"model is at capacity"}}`)))
		require.False(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, shouldRetryAntigravityError(status))
		require.False(t, (&GeminiMessagesCompatService{}).shouldRetryGeminiUpstreamError(nil, status))
		require.False(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamError(status))
	}
}

func TestTransientUpstreamStatusesStillFailover(t *testing.T) {
	statuses := []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusTooManyRequests, 529}
	customRetryAccount := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusBadRequest)},
		},
	}

	for _, status := range statuses {
		require.False(t, IsUpstreamReplayUnsafeTimeoutStatus(status))

		require.True(t, (&GatewayService{}).shouldRetryUpstreamError(customRetryAccount, status))
		require.True(t, (&GatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(status, "", nil))
		require.True(t, shouldFailoverOpenAIPassthroughResponse(customRetryAccount, status, "", nil))
		require.True(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamError(status))
	}
}

func TestGemini429FailoversWithoutSameAccountRetry(t *testing.T) {
	account := &Account{Type: AccountTypeAPIKey, Platform: PlatformGemini}
	svc := &GeminiMessagesCompatService{}

	require.False(t, svc.shouldRetryGeminiUpstreamError(account, http.StatusTooManyRequests))
	require.True(t, svc.shouldFailoverGeminiUpstreamError(http.StatusTooManyRequests))
	require.True(t, svc.shouldFailoverGeminiUpstreamResponse(
		http.StatusTooManyRequests,
		"rate limit",
		[]byte(`{"error":{"code":429,"message":"rate limit","status":"RESOURCE_EXHAUSTED"}}`),
	))
}

func TestCustomErrorCodeOmittedClientStatusesDoNotRetry(t *testing.T) {
	statuses := []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusMethodNotAllowed,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity,
	}
	customRetryAccount := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusUnauthorized), float64(http.StatusForbidden), float64(http.StatusTooManyRequests)},
		},
	}

	for _, status := range statuses {
		require.True(t, isDeterministicClientRequestStatus(status))
		require.False(t, (&GatewayService{}).shouldRetryUpstreamError(customRetryAccount, status))
	}

	require.False(t, isDeterministicClientRequestStatus(http.StatusTooManyRequests))
	require.True(t, (&GatewayService{}).shouldRetryUpstreamError(customRetryAccount, http.StatusServiceUnavailable))
}

func TestOpenAIRequestStateErrorsDoNotFailoverOrMatchCustomPolicy(t *testing.T) {
	account := &Account{
		ID:       77,
		Type:     AccountTypeAPIKey,
		Platform: PlatformOpenAI,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusBadRequest), float64(http.StatusNotFound)},
		},
	}
	svc := &RateLimitService{}

	cases := []struct {
		name   string
		status int
		body   []byte
	}{
		{
			name:   "invalid encrypted content",
			status: http.StatusBadRequest,
			body:   []byte(`{"error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"The encrypted content could not be verified."}}`),
		},
		{
			name:   "previous response not found",
			status: http.StatusNotFound,
			body:   []byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"previous_response_not_found","type":"invalid_request_error","message":"previous response not found"}}}`),
		},
		{
			name:   "image moderation blocked",
			status: http.StatusBadRequest,
			body:   []byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"moderation_blocked","type":"image_generation_user_error","message":"Prompt was blocked."}}}`),
		},
		{
			name:   "violation fee code",
			status: http.StatusBadRequest,
			body:   []byte(`{"error":{"code":"violation_fee.grok.csam","type":"violation_fee.grok.csam","message":"Failed check: SAFETY_CHECK_TYPE"}}`),
		},
		{
			name:   "grok safety marker",
			status: http.StatusBadRequest,
			body:   []byte(`{"type":"response.failed","response":{"status":"failed","error":{"type":"invalid_request_error","message":"Content violates usage guidelines. Failed check: SAFETY_CHECK_TYPE"}}}`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, isOpenAIRequestPolicyError(tc.body, ""))
			require.Equal(t, ErrorPolicyNone, svc.CheckErrorPolicy(context.Background(), account, tc.status, tc.body))
			require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(tc.status, "", tc.body))
			require.False(t, openAIStreamFailedEventShouldFailover(tc.body, ""))
		})
	}
}

func TestAnthropicStreamClientErrorStatusesDoNotFailover(t *testing.T) {
	svc := &GatewayService{}
	statuses := []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity,
	}

	for _, status := range statuses {
		require.False(t, svc.shouldFailoverAnthropicStreamError(status, "", nil))
	}
}

func TestAnthropicStreamAccountOrTransientStatusesStillFailover(t *testing.T) {
	svc := &GatewayService{}
	statuses := []int{
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		529,
	}

	for _, status := range statuses {
		require.True(t, svc.shouldFailoverAnthropicStreamError(status, "", nil))
	}
}

func TestAuthPaymentPermissionStatusesFailoverWithoutSameAccountRetry(t *testing.T) {
	statuses := []int{http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden}
	customRetryAccount := &Account{
		Type: AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusBadRequest)},
		},
	}

	for _, status := range statuses {
		require.False(t, IsUpstreamReplayUnsafeTimeoutStatus(status))

		require.False(t, (&GatewayService{}).shouldRetryUpstreamError(customRetryAccount, status))
		require.True(t, (&GatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(status, "", nil))
		passthroughBody := []byte(nil)
		passthroughMsg := ""
		if status == http.StatusForbidden {
			passthroughMsg = "This API key has been disabled"
			passthroughBody = []byte(`{"error":{"message":"This API key has been disabled"}}`)
		}
		require.True(t, shouldFailoverOpenAIPassthroughResponse(customRetryAccount, status, passthroughMsg, passthroughBody))
		require.True(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, shouldRetryAntigravityError(status))
		require.False(t, (&GeminiMessagesCompatService{}).shouldRetryGeminiUpstreamError(customRetryAccount, status))
		require.True(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamError(status))
	}

	require.False(t, shouldFailoverOpenAIPassthroughResponse(
		customRetryAccount,
		http.StatusForbidden,
		"This request has been blocked",
		[]byte(`{"error":{"code":"cyber_policy","message":"This request has been blocked"}}`),
	))
	require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(
		http.StatusForbidden,
		"This request has been blocked",
		[]byte(`{"error":{"code":"cyber_policy","message":"This request has been blocked"}}`),
	))
	require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(
		http.StatusForbidden,
		"This request violates the content policy",
		[]byte(`{"error":{"code":"content_policy_violation","type":"invalid_request_error","message":"This request violates the content policy"}}`),
	))
	require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(
		http.StatusForbidden,
		"Your request violates our usage policies.",
		[]byte(`{"error":{"type":"invalid_request_error","message":"Your request violates our usage policies."}}`),
	))
	require.False(t, shouldFailoverOpenAIPassthroughResponse(
		customRetryAccount,
		http.StatusForbidden,
		"The input contains disallowed content.",
		[]byte(`{"error":{"type":"invalid_request_error","message":"The input contains disallowed content."}}`),
	))
	require.False(t, shouldFailoverOpenAIPassthroughResponse(
		customRetryAccount,
		http.StatusForbidden,
		"This request has been flagged for potentially high-risk cyber activity.",
		[]byte(`{"error":{"type":"safety_error","message":"This request has been flagged for potentially high-risk cyber activity."}}`),
	))
	require.True(t, shouldFailoverOpenAIPassthroughResponse(
		customRetryAccount,
		http.StatusForbidden,
		"Permission denied",
		[]byte(`{"error":{"message":"Permission denied","type":"invalid_request_error"}}`),
	))
	require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(
		http.StatusForbidden,
		"This account has been disabled after policy review.",
		[]byte(`{"error":{"code":"content_policy_violation","message":"This account has been disabled after policy review."}}`),
	))

	require.False(t, (&GatewayService{}).shouldFailoverGatewayUpstreamResponse(
		&Account{Platform: PlatformAnthropic},
		http.StatusForbidden,
		"Your request violates Anthropic's Usage Policy.",
		[]byte(`{"type":"error","error":{"type":"safety_error","message":"Your request violates Anthropic's Usage Policy."}}`),
	))
	require.False(t, (&GatewayService{}).shouldFailoverAnthropicStreamError(
		http.StatusBadRequest,
		"This request has been blocked by a safety system.",
		[]byte(`{"type":"error","error":{"type":"safety_error","message":"This request has been blocked by a safety system."}}`),
	))
	require.False(t, (&GatewayService{}).shouldFailoverGatewayUpstreamResponse(
		&Account{Platform: PlatformAnthropic},
		http.StatusForbidden,
		"",
		[]byte(`{"response":{"error":{"type":"safety_error","message":"Your request violates Anthropic's Usage Policy."}}}`),
	))
	require.False(t, (&GatewayService{}).shouldFailoverAnthropicStreamError(
		http.StatusForbidden,
		"",
		[]byte(`{"response":{"error":{"type":"safety_error","message":"Your request violates Anthropic's Usage Policy."}}}`),
	))
	require.True(t, (&GatewayService{}).shouldFailoverGatewayUpstreamResponse(
		&Account{Platform: PlatformAnthropic},
		http.StatusForbidden,
		"Permission denied",
		[]byte(`{"type":"error","error":{"type":"permission_error","message":"Permission denied"}}`),
	))
	require.False(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamResponse(
		http.StatusForbidden,
		"The prompt was blocked due to safety filters.",
		[]byte(`{"error":{"code":403,"message":"The prompt was blocked due to safety filters.","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SAFETY"}]}}`),
	))
	require.False(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamResponse(
		http.StatusForbidden,
		"",
		[]byte(`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`),
	))
	require.False(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamResponse(
		http.StatusForbidden,
		"",
		[]byte(`{"response":{"candidates":[{"finishReason":"SAFETY"}]}}`),
	))
	require.True(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamResponse(
		http.StatusForbidden,
		"Permission denied",
		[]byte(`{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`),
	))
	require.False(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamResponse(
		http.StatusForbidden,
		"The response was blocked due to prohibited content.",
		[]byte(`{"error":{"code":403,"message":"The response was blocked due to prohibited content.","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"PROHIBITED_CONTENT"}]}}`),
	))
	require.False(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamResponse(
		http.StatusForbidden,
		"",
		[]byte(`{"response":{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}}`),
	))
	require.True(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamResponse(
		http.StatusForbidden,
		"Permission denied",
		[]byte(`{"error":{"code":403,"message":"Permission denied","status":"PERMISSION_DENIED"}}`),
	))
}

func TestClaudeCodeClientRestrictionErrorClassification(t *testing.T) {
	require.True(t, isClaudeCodeClientRestrictionError(
		http.StatusServiceUnavailable,
		"",
		[]byte(`{"type":"error","error":{"type":"api_error","message":"No available accounts: this group only allows Claude Code clients"}}`),
	))
	require.True(t, isClaudeCodeClientRestrictionError(
		http.StatusBadRequest,
		"OAuth token is only authorized for use with Claude Code and cannot be used for other API requests",
		nil,
	))
	require.True(t, isClaudeCodeClientRestrictionError(
		http.StatusForbidden,
		"This group is restricted to Claude Code clients (/v1/messages only)",
		nil,
	))
	require.False(t, isClaudeCodeClientRestrictionError(
		http.StatusForbidden,
		"Your request violates Anthropic's Usage Policy.",
		[]byte(`{"type":"error","error":{"type":"safety_error","message":"Your request violates Anthropic's Usage Policy."}}`),
	))
	require.False(t, isClaudeCodeClientRestrictionError(
		http.StatusOK,
		"this group only allows Claude Code clients",
		nil,
	))
}
