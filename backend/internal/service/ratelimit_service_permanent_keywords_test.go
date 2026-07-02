package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type permanentKeywordAccountRepoStub struct {
	AccountRepository
	setErrorCalls  int
	lastErrorMsg   string
	tempCalls      int
	lastTempReason string
}

func (r *permanentKeywordAccountRepoStub) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

func (r *permanentKeywordAccountRepoStub) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.tempCalls++
	r.lastTempReason = reason
	return nil
}

type permanentKeywordOpenAI403CounterStub struct{}

func (s permanentKeywordOpenAI403CounterStub) IncrementOpenAI403Count(context.Context, int64, int) (int64, error) {
	return 1, nil
}

func (s permanentKeywordOpenAI403CounterStub) ResetOpenAI403Count(context.Context, int64) error {
	return nil
}

func TestRateLimitServiceHandleUpstreamErrorAPIKeyPermanentKeywordsDisable(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		statusCode int
		body       []byte
		want       string
	}{
		{
			name:       "openai permission denied",
			platform:   PlatformOpenAI,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"Permission denied","type":"invalid_request_error"}}`),
			want:       "Permission denied",
		},
		{
			name:       "openai quota exhausted",
			platform:   PlatformOpenAI,
			statusCode: http.StatusTooManyRequests,
			body:       []byte(`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","code":"insufficient_quota"}}`),
			want:       "You exceeded your current quota",
		},
		{
			name:       "anthropic credit balance",
			platform:   PlatformAnthropic,
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"Your credit balance is too low"}}`),
			want:       "Your credit balance is too low",
		},
		{
			name:       "security token invalid",
			platform:   PlatformGemini,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"The security token included in the request is invalid"}}`),
			want:       "security token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &permanentKeywordAccountRepoStub{}
			svc := &RateLimitService{accountRepo: repo}
			account := &Account{
				ID:       207,
				Platform: tt.platform,
				Type:     AccountTypeAPIKey,
			}

			shouldDisable := svc.HandleUpstreamError(
				context.Background(),
				account,
				tt.statusCode,
				http.Header{},
				tt.body,
			)

			require.True(t, shouldDisable)
			require.Equal(t, 1, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Contains(t, repo.lastErrorMsg, "Permanent account error")
			require.Contains(t, repo.lastErrorMsg, tt.want)
		})
	}
}

func TestRateLimitServiceHandleUpstreamErrorOpenAIOAuthPermanentKeywordStillUses403Cooldown(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	svc.SetOpenAI403CounterCache(permanentKeywordOpenAI403CounterStub{})
	account := &Account{
		ID:       208,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Permission denied","type":"invalid_request_error"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "Permission denied")
	require.Contains(t, repo.lastTempReason, "(1/3)")
}
