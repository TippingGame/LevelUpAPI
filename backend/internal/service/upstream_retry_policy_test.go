package service

import (
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

func TestAnthropicStreamClientErrorStatusesDoNotFailover(t *testing.T) {
	svc := &GatewayService{}
	statuses := []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity,
	}

	for _, status := range statuses {
		require.False(t, svc.shouldFailoverAnthropicStreamError(status))
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
		require.True(t, svc.shouldFailoverAnthropicStreamError(status))
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
	require.False(t, shouldFailoverOpenAIPassthroughResponse(
		customRetryAccount,
		http.StatusForbidden,
		"This request has been flagged for potentially high-risk cyber activity.",
		[]byte(`{"error":{"type":"safety_error","message":"This request has been flagged for potentially high-risk cyber activity."}}`),
	))
	require.False(t, shouldFailoverOpenAIPassthroughResponse(
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
}
