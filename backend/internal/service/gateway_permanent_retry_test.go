package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayServiceShouldStopRetryForPermanentAccountError(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &GatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := &Account{
		ID:       301,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
		},
	}
	body := []byte(`{"error":{"message":"This API key has been disabled"}}`)

	stopped := svc.shouldStopRetryForPermanentAccountError(context.Background(), account, http.StatusForbidden, body)

	require.True(t, stopped)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Contains(t, repo.lastErrorMsg, "API key has been disabled")
}

func TestGatewayServiceShouldStopRetryForPermanentAccountErrorSkipsPoolMode(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &GatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := &Account{
		ID:       302,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
	body := []byte(`{"error":{"message":"This API key has been disabled"}}`)

	stopped := svc.shouldStopRetryForPermanentAccountError(context.Background(), account, http.StatusForbidden, body)

	require.False(t, stopped)
	require.Equal(t, 0, repo.setErrorCalls)
}

func TestGatewayServiceHandleRetryExhaustedSideEffectsPermanentAccountError(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &GatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := &Account{ID: 303, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	body := []byte(`{"error":{"message":"This API key has been disabled"}}`)
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	svc.handleRetryExhaustedSideEffects(context.Background(), resp, account)

	require.Equal(t, 1, repo.setErrorCalls)
	require.Contains(t, repo.lastErrorMsg, "API key has been disabled")
}
