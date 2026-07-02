package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamReplayUnsafeTimeoutStatusesDoNotFailover(t *testing.T) {
	statuses := []int{http.StatusGatewayTimeout, cloudflareOriginTimeoutStatus}

	for _, status := range statuses {
		require.True(t, IsUpstreamReplayUnsafeTimeoutStatus(status))

		require.False(t, (&GatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, (&OpenAIGatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(status, "model is at capacity", []byte(`{"error":{"message":"model is at capacity"}}`)))
		require.False(t, shouldFailoverOpenAIPassthroughResponse(status, "model is at capacity", []byte(`{"error":{"message":"model is at capacity"}}`)))
		require.False(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamError(status))
		require.False(t, shouldRetryAntigravityError(status))
		require.False(t, (&GeminiMessagesCompatService{}).shouldRetryGeminiUpstreamError(nil, status))
		require.False(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamError(status))
	}
}

func TestTransientUpstreamStatusesStillFailover(t *testing.T) {
	statuses := []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusTooManyRequests, 529}

	for _, status := range statuses {
		require.False(t, IsUpstreamReplayUnsafeTimeoutStatus(status))

		require.True(t, (&GatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&OpenAIGatewayService{}).shouldFailoverOpenAIUpstreamResponse(status, "", nil))
		require.True(t, shouldFailoverOpenAIPassthroughResponse(status, "", nil))
		require.True(t, (&AntigravityGatewayService{}).shouldFailoverUpstreamError(status))
		require.True(t, (&GeminiMessagesCompatService{}).shouldFailoverGeminiUpstreamError(status))
	}
}
