package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandle429RetryAfterSecondsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7301,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "17")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.NoError(t, repo.lastRateCtxErr)
	require.False(t, repo.lastRateReset.Before(before.Add(17*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(17*time.Second)))
	require.NotNil(t, account.RateLimitResetAt)
	require.Equal(t, repo.lastRateReset.Unix(), account.RateLimitResetAt.Unix())
}

func TestHandle429RetryAfterHTTPDateSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7302,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	resetAt := time.Now().Add(25 * time.Second).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("Retry-After", resetAt.Format(http.TimeFormat))

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.Equal(t, resetAt.Unix(), repo.lastRateReset.Unix())
}

func TestHandle429AnthropicAPIKeyHonorsRetryAfter(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7303,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "11")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"rate limited"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(11*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(11*time.Second)))
}

func TestHandle429RetryAfterTooLargeFallsBackToDefault(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7304,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "999999")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
}
